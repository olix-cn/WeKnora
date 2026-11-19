package repository

import (
	"context"
	"sort"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RegisterSessionKnowledgeSources records admitted sources BEFORE retrieval or
// model output. Registration never grants access; the caller's owner scope and
// each KB's current policy must already have been verified by the runtime.
func (r *sessionRepository) RegisterSessionKnowledgeSources(
	ctx context.Context, sessionID string, targets types.SearchTargets,
) error {
	if len(targets) == 0 {
		return nil
	}
	caller := types.CallerFromContext(ctx)
	targets = append(types.SearchTargets(nil), targets...)
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i] == nil {
			return false
		}
		if targets[j] == nil {
			return true
		}
		return targets[i].KnowledgeBaseID < targets[j].KnowledgeBaseID
	})
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var session types.Session
		if err := tx.Select("id", "tenant_id", "user_id").Where("id = ? AND tenant_id = ?", sessionID,
			caller.TenantID).First(&session).Error; err != nil {
			return err
		}
		if session.UserID != types.SessionOwnerIDFromContext(ctx) {
			return access.ErrForbidden
		}
		// Order KB locks consistently across concurrent turns. Read() uses a
		// nested savepoint on this transaction; its SHARE lock remains held until
		// the outer commit, so a policy save cannot pass source registration.
		var sources []sessionKnowledgeSource
		seen := map[string]bool{}
		for _, target := range targets {
			if target == nil || target.KnowledgeBaseID == "" || seen[target.KnowledgeBaseID] {
				continue
			}
			seen[target.KnowledgeBaseID] = true
			var kb types.KnowledgeBase
			if err := tx.Select("id", "tenant_id").Where("id = ?", target.KnowledgeBaseID).
				First(&kb).Error; err != nil {
				return err
			}
			if kb.TenantID != target.TenantID {
				return access.ErrForbidden
			}
			permission, handled, err := (&knowledgeBaseRepository{db: tx}).CheckKBMemberAccess(
				ctx, target.KnowledgeBaseID, target.TenantID, caller)
			if err != nil {
				return err
			}
			if handled && (!permission.IsValid() || !permission.HasPermission(types.OrgRoleViewer)) {
				return access.ErrForbidden
			}
			sources = append(sources, sessionKnowledgeSource{
				SessionID: sessionID, KnowledgeBaseID: target.KnowledgeBaseID,
			})
		}
		if len(sources) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&sources).Error
	})
}
