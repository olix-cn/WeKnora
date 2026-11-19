package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrKBAccessShared requires explicit removal of existing shares before a KB
// becomes member-restricted. Share creation also uses this error for a scope
// that already contains a restricted KB.
var ErrKBAccessShared = errors.New("knowledge base member restrictions conflict with external sharing")

// ErrKBAccessTransferBusy requires transfer completion or explicit recovery
// before any policy/audience change of either participating knowledge base.
var ErrKBAccessTransferBusy = errors.New("knowledge base has an active or unresolved content transfer")

// withKBSharingLock serializes policy changes, KB/agent share creation and
// shared-agent scope changes in one workspace. Always acquire this lock BEFORE
// KB, agent or membership locks. There is no process-local mutex: PostgreSQL
// workers and separate app instances must observe the same serialization.
func withKBSharingLock(ctx context.Context, db *gorm.DB, tenantID uint64, run func(*gorm.DB) error) error {
	if tenantID == 0 {
		return errors.New("workspace is required for knowledge base sharing")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tenant types.Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
			Where("id = ? AND status = ?", tenantID, "active").First(&tenant).Error; err != nil {
			return err
		}
		return run(tx)
	})
}

func requirePublicKBPolicy(tx *gorm.DB, kbID string, tenantID uint64) error {
	var policy types.KnowledgeBaseAccessPolicy
	err := tx.Where("knowledge_base_id = ?", kbID).Take(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if policy.TenantID != tenantID || policy.Mode != types.KBAccessInherit || policy.Version == 0 {
		return ErrKBAccessShared
	}
	return nil
}

func requirePublicAgentScope(tx *gorm.DB, agent *types.CustomAgent) error {
	scope := types.NewSharedAgentKBScope(agent)
	if scope.IsEmpty() {
		return nil
	}
	var policies []types.KnowledgeBaseAccessPolicy
	query := tx.Where("tenant_id = ?", agent.TenantID)
	if !scope.IsAll() {
		query = query.Where("knowledge_base_id IN ?", scope.IDs())
	}
	if err := query.Find(&policies).Error; err != nil {
		return err
	}
	for _, policy := range policies {
		if policy.Mode != types.KBAccessInherit || policy.Version == 0 {
			return ErrKBAccessShared
		}
	}
	return nil
}

func requireUnsharedKB(tx *gorm.DB, kb *types.KnowledgeBase) error {
	var count int64
	if err := tx.Model(&types.KnowledgeBaseShare{}).Where("knowledge_base_id = ?", kb.ID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrKBAccessShared
	}
	var agents []types.CustomAgent
	if err := tx.Model(&types.CustomAgent{}).
		Where("tenant_id = ?", kb.TenantID).
		Where(`EXISTS (SELECT 1 FROM agent_shares s WHERE s.agent_id = custom_agents.id
			AND s.source_tenant_id = custom_agents.tenant_id AND s.deleted_at IS NULL)`).
		Select("id", "tenant_id", "config").Find(&agents).Error; err != nil {
		return err
	}
	for _, agent := range agents {
		if types.SharedAgentIncludesKB(&agent, kb) {
			return ErrKBAccessShared
		}
	}
	return nil
}

// ReplaceManaged adds external-sharing exclusion to the lower-level replace
// primitive. The authorization callback is still evaluated against fresh facts
// under the KB/membership locks, before checking or disclosing share conflicts.
func (r *KnowledgeBaseAccessRepository) ReplaceManaged(
	ctx context.Context, caller types.Caller, kbID string, update types.KBAccessPolicyUpdate,
	authorize func(*KBAccessState) error,
) (*KBAccessState, error) {
	if authorize == nil {
		return nil, errors.New("knowledge base policy authorizer is required")
	}
	var state *KBAccessState
	err := withKBSharingLock(ctx, r.db, caller.TenantID, func(tx *gorm.DB) error {
		var err error
		state, err = NewKnowledgeBaseAccessRepository(tx).Replace(ctx, caller, kbID, update,
			func(current *KBAccessState) error {
				if current.Actor == nil {
					return ErrKBAccessMember
				}
				if err := authorize(current); err != nil {
					return err
				}
				var transfers int64
				if err := tx.Model(&types.KBTransferFence{}).
					Where("tenant_id = ? AND (source_id = ? OR target_id = ?)", caller.TenantID, kbID, kbID).
					Count(&transfers).Error; err != nil {
					return err
				}
				if transfers > 0 {
					return ErrKBAccessTransferBusy
				}
				if update.Mode == types.KBAccessRestricted {
					return requireUnsharedKB(tx, current.KB)
				}
				return nil
			})
		if err != nil {
			return err
		}
		// Commit the policy and its minimal audit event together. Do not put
		// names, emails, full rosters or document content in the audit details.
		details, err := json.Marshal(map[string]any{
			"mode": state.Policy.Mode, "version": state.Policy.Version, "member_count": len(state.Members),
		})
		if err != nil {
			return err
		}
		return tx.Create(&types.AuditLog{
			TenantID: caller.TenantID, ActorUserID: caller.UserID, ActorRole: string(state.Actor.Role),
			Action: types.AuditActionKBMemberAccessChanged, ScopeType: "knowledge_base", ScopeID: kbID,
			TargetType: "knowledge_base", TargetID: kbID, Outcome: types.AuditOutcomeSuccess,
			Details: types.JSON(details), CreatedAt: time.Now().UTC(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return state, nil
}

// MemberCandidates performs manager authorization before searching a bounded
// set of active members. It holds a shared KB lock through the query so policy
// writes cannot revoke the roster access between authorization and disclosure.
func (r *KnowledgeBaseAccessRepository) MemberCandidates(
	ctx context.Context, caller types.Caller, kbID, search string, page, size int,
	authorize func(*KBAccessState) error,
) ([]types.KBAccessMemberCandidate, int64, error) {
	if authorize == nil || page < 1 || page > 1000000 || size < 1 || size > 100 || len(search) > 200 {
		return nil, 0, errors.New("invalid knowledge base member search")
	}
	var rows []types.KBAccessMemberCandidate
	var total int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var kb types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").
			Where("id = ? AND tenant_id = ?", kbID, caller.TenantID).First(&kb).Error; err != nil {
			return err
		}
		state, err := loadKBAccessState(tx, caller, kbID)
		if err != nil {
			return err
		}
		if err := authorize(state); err != nil {
			return err
		}
		query := tx.Model(&types.TenantMember{}).
			Joins("JOIN users u ON u.id = tenant_members.user_id AND u.deleted_at IS NULL AND u.is_active = ?", true).
			Where("tenant_members.tenant_id = ? AND tenant_members.status = ? "+
				"AND tenant_members.deleted_at IS NULL", caller.TenantID,
				types.TenantMemberStatusActive)
		if search != "" {
			query = query.Where("LOWER(u.username) LIKE LOWER(?) ESCAPE ?",
				"%"+escapeLikePattern(search)+"%", likeEscapeChar)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Select("tenant_members.user_id, u.username, tenant_members.role").
			Order("u.username ASC, tenant_members.user_id ASC").Offset((page - 1) * size).Limit(size).Scan(&rows).Error
	})
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
