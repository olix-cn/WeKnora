package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BackfillSessionProvenance rebuilds one explicit, bounded maintenance batch.
// It grants no history access, returns no content and never runs automatically
// at server startup. A caller must own maintenance authorization separately.
func BackfillSessionProvenance(ctx context.Context, db *gorm.DB, after string, limit int) (string, int, error) {
	if limit < 1 || limit > 500 || len(after) > 36 {
		return "", 0, fmt.Errorf("invalid provenance backfill batch")
	}
	var ids []string
	if err := db.WithContext(ctx).Table("session_knowledge_revisions").
		Where("session_id > ? AND revision <> indexed_revision", after).
		Order("session_id ASC").Limit(limit).Pluck("session_id", &ids).Error; err != nil {
		return "", 0, err
	}
	r := &sessionRepository{db: db}
	last, count := after, 0
	for _, id := range ids {
		if _, err := r.sessionSourceIDs(ctx, id); err != nil {
			return last, count, err
		}
		last = id
		count++
	}
	return last, count, nil
}

type sessionKnowledgeRevision struct {
	SessionID        string `gorm:"primaryKey"`
	Revision         int64
	IndexedRevision  int64
	LegacyUnverified bool
}

func (sessionKnowledgeRevision) TableName() string { return "session_knowledge_revisions" }

type sessionKnowledgeSource struct {
	SessionID       string `gorm:"primaryKey"`
	KnowledgeBaseID string `gorm:"primaryKey"`
}

func (sessionKnowledgeSource) TableName() string { return "session_knowledge_sources" }

// sessionSourceIDs refreshes derived provenance only after message writes.
// Database triggers invalidate direct writes too; no process-local TTL can
// preserve a stale allow decision. Sources are monotonic: removing an old
// citation does not prove later answers/summaries stopped depending on it.
func (r *sessionRepository) sessionSourceIDs(ctx context.Context, sessionID string) (map[string]bool, error) {
	var result map[string]bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&sessionKnowledgeRevision{
			SessionID: sessionID, Revision: 1,
		}).Error; err != nil {
			return err
		}
		var revision sessionKnowledgeRevision
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", sessionID).
			Take(&revision).Error; err != nil {
			return err
		}
		if revision.Revision != revision.IndexedRevision {
			ids, err := (&sessionRepository{db: tx}).scanSessionSourceIDs(ctx, sessionID)
			if err != nil {
				return err
			}
			rows := make([]sessionKnowledgeSource, 0, len(ids))
			for id := range ids {
				if id != "" {
					rows = append(rows, sessionKnowledgeSource{SessionID: sessionID, KnowledgeBaseID: id})
				}
			}
			if len(rows) > 0 {
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 100).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&sessionKnowledgeRevision{}).Where("session_id = ?", sessionID).
				Update("indexed_revision", revision.Revision).Error; err != nil {
				return err
			}
			// This transaction holds the revision lock. Concurrent message
			// mutations cannot commit their trigger/invalidation until it ends.
			// Evidence is compacted only after the monotonic index is durable.
			if err := tx.Exec("DELETE FROM session_knowledge_evidence WHERE session_id = ?",
				sessionID).Error; err != nil {
				return err
			}
		}
		var sources []sessionKnowledgeSource
		if err := tx.Where("session_id = ?", sessionID).Find(&sources).Error; err != nil {
			return err
		}
		result = make(map[string]bool, len(sources))
		for _, source := range sources {
			result[source.KnowledgeBaseID] = true
		}
		return nil
	})
	return result, err
}
