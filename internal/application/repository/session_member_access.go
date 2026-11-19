package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// CheckSessionKBMemberAccess rejects replay of a transcript whose persisted KB
// provenance is no longer readable. Redacting only citations is insufficient:
// answers, tool output and summaries can all contain the original evidence.
// The stored transcript is never rewritten or deleted.
func (r *sessionRepository) CheckSessionKBMemberAccess(
	ctx context.Context, sessionID string, caller types.Caller,
) error {
	var policies []types.KnowledgeBaseAccessPolicy
	if err := r.db.WithContext(ctx).Where("mode <> ?", types.KBAccessInherit).Find(&policies).Error; err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}
	kbIDs, err := r.sessionSourceIDs(ctx, sessionID)
	if err != nil {
		return err
	}
	var revision sessionKnowledgeRevision
	if err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Take(&revision).Error; err != nil {
		return err
	}
	lookup := &knowledgeBaseRepository{db: r.db}
	for _, policy := range policies {
		if !kbIDs[policy.KnowledgeBaseID] && (!revision.LegacyUnverified || policy.TenantID != caller.TenantID) {
			continue
		}
		permission, _, err := lookup.CheckKBMemberAccess(ctx, policy.KnowledgeBaseID, policy.TenantID, caller)
		if err != nil {
			return err
		}
		if !permission.IsValid() || !permission.HasPermission(types.OrgRoleViewer) {
			return access.ErrForbidden
		}
	}
	return nil
}

func (r *sessionRepository) scanSessionSourceIDs(ctx context.Context, sessionID string) (map[string]bool, error) {
	var rows []types.Message
	// A bounded scan protects API reads from unbounded transcript decoding.
	// More than the budget fails closed, rather than checking only the last page.
	if err := r.db.WithContext(ctx).Model(&types.Message{}).
		Select("knowledge_references", "execution_context", "mentioned_items", "agent_steps").
		Where("session_id = ?", sessionID).Limit(10001).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 10000 {
		return nil, fmt.Errorf("transcript exceeds member-policy verification budget")
	}
	var evidence []types.Message
	if err := r.db.WithContext(ctx).Unscoped().Table("session_knowledge_evidence").
		Select("knowledge_references", "execution_context", "mentioned_items", "agent_steps").
		Where("session_id = ?", sessionID).Limit(10001).Find(&evidence).Error; err != nil {
		return nil, err
	}
	if len(evidence) > 10000 {
		return nil, fmt.Errorf("transcript provenance evidence exceeds verification budget")
	}
	rows = append(rows, evidence...)
	kbIDs, knowledgeIDs := make(map[string]bool), make(map[string]bool)
	for _, row := range rows {
		data, err := json.Marshal(struct {
			References types.References              `json:"references"`
			Execution  types.MessageExecutionContext `json:"execution"`
			Mentions   types.MentionedItems          `json:"mentions"`
			Steps      types.AgentSteps              `json:"steps"`
		}{row.KnowledgeReferences, row.ExecutionContext, row.MentionedItems, row.AgentSteps})
		if err != nil {
			return nil, err
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, err
		}
		collectTranscriptKBIDs(value, kbIDs, knowledgeIDs, 0)
		for _, mention := range row.MentionedItems {
			if mention.Type == "kb" {
				kbIDs[mention.ID] = true
			}
			if mention.Type == "file" {
				knowledgeIDs[mention.ID] = true
			}
		}
	}
	if len(knowledgeIDs) > 0 {
		ids := make([]string, 0, len(knowledgeIDs))
		for id := range knowledgeIDs {
			ids = append(ids, id)
		}
		var docs []types.Knowledge
		if err := r.db.WithContext(ctx).Unscoped().Model(&types.Knowledge{}).
			Select("knowledge_base_id").Where("id IN ?", ids).Find(&docs).Error; err != nil {
			return nil, err
		}
		for _, doc := range docs {
			kbIDs[doc.KnowledgeBaseID] = true
		}
	}
	return kbIDs, nil
}

func collectTranscriptKBIDs(value any, kbIDs, knowledgeIDs map[string]bool, depth int) {
	if depth > 32 {
		return
	}
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			var target map[string]bool
			switch key {
			case "knowledge_base_id", "knowledge_base_ids", "knowledge_bases", "kb_id":
				target = kbIDs
			case "knowledge_id", "knowledge_ids":
				target = knowledgeIDs
			}
			if target != nil {
				if id, ok := child.(string); ok {
					target[id] = true
				}
				if ids, ok := child.([]any); ok {
					for _, item := range ids {
						if id, ok := item.(string); ok {
							target[id] = true
						}
					}
				}
			}
			collectTranscriptKBIDs(child, kbIDs, knowledgeIDs, depth+1)
		}
	case []any:
		for _, child := range value {
			collectTranscriptKBIDs(child, kbIDs, knowledgeIDs, depth+1)
		}
	case string:
		// Legacy tool output often persists its structured result as a JSON
		// string. Parse bounded objects/arrays, never arbitrary prose as IDs.
		if len(value) > 0 && len(value) <= 1<<20 && (value[0] == '{' || value[0] == '[') {
			var decoded any
			if json.Unmarshal([]byte(value), &decoded) == nil {
				collectTranscriptKBIDs(decoded, kbIDs, knowledgeIDs, depth+1)
			}
		}
	}
}

// DeniedSessionIDs checks current policy once per restricted KB and uses the
// reverse source index for list exclusions. Only dirty/legacy sessions require
// decoding; a large fully indexed history does not hit the cold-scan budget.
func (r *sessionRepository) DeniedSessionIDs(
	ctx context.Context, tenantID uint64, ownerID string,
) ([]string, error) {
	var policies []types.KnowledgeBaseAccessPolicy
	if err := r.db.WithContext(ctx).Where("mode <> ?", types.KBAccessInherit).Find(&policies).Error; err != nil {
		return nil, err
	}
	lookup := &knowledgeBaseRepository{db: r.db}
	var deniedKBs []string
	legacyDenied := false
	for _, policy := range policies {
		permission, _, err := lookup.CheckKBMemberAccess(ctx, policy.KnowledgeBaseID, policy.TenantID,
			types.CallerFromContext(ctx))
		if err != nil {
			return nil, err
		}
		if !permission.IsValid() || !permission.HasPermission(types.OrgRoleViewer) {
			deniedKBs = append(deniedKBs, policy.KnowledgeBaseID)
			legacyDenied = legacyDenied || policy.TenantID == tenantID
		}
	}
	if len(deniedKBs) == 0 {
		return nil, nil
	}
	scope := func(query *gorm.DB) *gorm.DB {
		query = query.Where("s.tenant_id = ? AND s.deleted_at IS NULL", tenantID)
		if ownerID != "" {
			query = query.Where("(s.user_id = ? OR s.user_id IS NULL OR s.user_id = '')", ownerID)
		}
		return query
	}
	var dirty []string
	if err := scope(r.db.WithContext(ctx).Table("sessions s").
		Joins("LEFT JOIN session_knowledge_revisions r ON r.session_id = s.id")).
		Where("r.session_id IS NULL OR r.revision <> r.indexed_revision").
		Limit(2001).Pluck("s.id", &dirty).Error; err != nil {
		return nil, err
	}
	if len(dirty) > 2000 {
		return nil, fmt.Errorf("cold session provenance exceeds verification budget; backfill before serving this list")
	}
	for _, id := range dirty {
		if _, err := r.sessionSourceIDs(ctx, id); err != nil {
			return nil, err
		}
	}
	var denied []string
	const sourceMatch = "EXISTS (SELECT 1 FROM session_knowledge_sources p " +
		"WHERE p.session_id = s.id AND p.knowledge_base_id IN ?)"
	query := scope(r.db.WithContext(ctx).Table("sessions s")).Where(sourceMatch, deniedKBs)
	if legacyDenied {
		query = scope(r.db.WithContext(ctx).Table("sessions s")).Where(
			sourceMatch+" OR EXISTS (SELECT 1 FROM session_knowledge_revisions r "+
				"WHERE r.session_id = s.id AND r.legacy_unverified = ?)", deniedKBs, true)
	}
	if err := query.Limit(10001).Pluck("s.id", &denied).Error; err != nil {
		return nil, err
	}
	if len(denied) > 10000 {
		return nil, fmt.Errorf("session visibility exclusion exceeds query budget")
	}
	return denied, nil
}
