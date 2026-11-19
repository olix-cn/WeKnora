package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrKBAccessConflict prevents stale saves from overwriting newer policies.
	ErrKBAccessConflict = errors.New("knowledge base access policy changed; reload before saving")
	// ErrKBAccessMember rejects unavailable or foreign grant recipients.
	ErrKBAccessMember = errors.New("knowledge base grant requires an active workspace member")
)

// KBAccessState contains persisted authorization facts, never a client's role.
type KBAccessState struct {
	KB      *types.KnowledgeBase
	Policy  *types.KnowledgeBaseAccessPolicy
	Actor   *types.TenantMember
	Members []types.KnowledgeBaseMemberGrant
}

// KnowledgeBaseAccessRepository persists member policies for management and
// live access adapters. It never creates tables or silently falls back on a
// missing migration.
type KnowledgeBaseAccessRepository struct{ db *gorm.DB }

// NewKnowledgeBaseAccessRepository constructs an internal persistence adapter.
func NewKnowledgeBaseAccessRepository(db *gorm.DB) *KnowledgeBaseAccessRepository {
	return &KnowledgeBaseAccessRepository{db: db}
}

// CheckKBTransferPolicy prevents source contents from being declassified by a
// clone/move into a new or less restricted knowledge base. This first version
// deliberately does not guess whether two explicit audiences are equivalent.
func (r *knowledgeBaseRepository) CheckKBTransferPolicy(ctx context.Context, kbID string, tenantID uint64) error {
	var policy types.KnowledgeBaseAccessPolicy
	err := r.db.WithContext(ctx).Where("knowledge_base_id = ?", kbID).Take(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if policy.TenantID != tenantID || policy.Mode != types.KBAccessInherit || policy.Version == 0 {
		return access.ErrForbidden
	}
	return nil
}

// CheckKBMemberAccess applies a policy, if present, before any legacy own-tenant
// or shared-agent shortcut. No policy rows are cached across operations.
func (r *knowledgeBaseRepository) CheckKBMemberAccess(
	ctx context.Context, kbID string, ownerTenantID uint64, caller types.Caller,
) (types.OrgMemberRole, bool, error) {
	var policy types.KnowledgeBaseAccessPolicy
	err := r.db.WithContext(ctx).Where("knowledge_base_id = ?", kbID).Take(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	if policy.TenantID != ownerTenantID || !policy.Mode.IsValid() || policy.Version == 0 {
		return "", true, nil
	}
	if caller.TenantID != ownerTenantID {
		return "", policy.Mode == types.KBAccessRestricted, nil
	}
	if _, machine := types.TenantAPIKeyScopeFromContext(ctx); machine || caller.UserID == "" {
		return "", policy.Mode == types.KBAccessRestricted, nil
	}
	if principal, ok := types.PrincipalFromContext(ctx); ok && principal.Type != types.PrincipalWebUser {
		return "", policy.Mode == types.KBAccessRestricted, nil
	}
	if types.IsSyntheticUserID(caller.UserID) {
		return "", policy.Mode == types.KBAccessRestricted, nil
	}
	state, err := NewKnowledgeBaseAccessRepository(r.db).Read(ctx, caller, kbID)
	if err != nil {
		return "", true, err
	}
	var own *types.KnowledgeBaseMemberGrant
	for i := range state.Members {
		if state.Members[i].UserID == caller.UserID {
			own = &state.Members[i]
			break
		}
	}
	permission, _ := access.WorkspaceKBPermission(caller, state.KB, state.Policy, state.Actor, own)
	return permission, true, nil
}

func loadKBAccessState(db *gorm.DB, caller types.Caller, kbID string) (*KBAccessState, error) {
	state := &KBAccessState{}
	var kb types.KnowledgeBase
	if err := db.Where("id = ? AND tenant_id = ?", kbID, caller.TenantID).First(&kb).Error; err != nil {
		return nil, err
	}
	state.KB = &kb
	var policy types.KnowledgeBaseAccessPolicy
	err := db.Where("knowledge_base_id = ? AND tenant_id = ?", kbID, caller.TenantID).Take(&policy).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil {
		state.Policy = &policy
	}
	var actor types.TenantMember
	err = db.Where("tenant_id = ? AND user_id = ? AND status = ?", caller.TenantID, caller.UserID,
		types.TenantMemberStatusActive).Take(&actor).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil {
		state.Actor = &actor
		var count int64
		if err := db.Model(&types.User{}).Where("id = ? AND is_active = ?", caller.UserID, true).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 1 {
			state.Actor = nil
		}
	}
	// Do not echo stale grants back to a browser: saving an unchanged roster
	// must not silently reauthorize a removed/rejoined or disabled member.
	if err := db.Model(&types.KnowledgeBaseMemberGrant{}).
		Select("knowledge_base_member_grants.*").
		Joins("JOIN tenant_members m ON m.id = knowledge_base_member_grants.membership_id "+
			"AND m.user_id = knowledge_base_member_grants.user_id "+
			"AND m.tenant_id = knowledge_base_member_grants.tenant_id AND m.deleted_at IS NULL").
		Joins("JOIN users u ON u.id = m.user_id AND u.deleted_at IS NULL AND u.is_active = ?", true).
		Where("knowledge_base_member_grants.knowledge_base_id = ? AND knowledge_base_member_grants.tenant_id = ? "+
			"AND m.status = ?", kbID, caller.TenantID, types.TenantMemberStatusActive).
		Order("knowledge_base_member_grants.user_id ASC").Find(&state.Members).Error; err != nil {
		return nil, err
	}
	return state, nil
}

// Read uses a transaction so mode, version and member list form one snapshot.
// It returns internal facts; a service must authorize before exposing them.
func (r *KnowledgeBaseAccessRepository) Read(
	ctx context.Context, caller types.Caller, kbID string,
) (*KBAccessState, error) {
	var state *KBAccessState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize with Replace even under PostgreSQL's READ COMMITTED.
		var kb types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("id = ? AND tenant_id = ?", kbID, caller.TenantID).First(&kb).Error; err != nil {
			return err
		}
		var err error
		state, err = loadKBAccessState(tx, caller, kbID)
		return err
	})
	return state, err
}

// Replace is a transaction primitive, not a public permission-management API.
// authorize MUST check the current state while the KB row is locked. Nil
// authorization fails closed. The caller validates request syntax first.
func (r *KnowledgeBaseAccessRepository) Replace(
	ctx context.Context, caller types.Caller, kbID string, update types.KBAccessPolicyUpdate,
	authorize func(*KBAccessState) error,
) (*KBAccessState, error) {
	if err := access.ValidateKBAccessPolicy(update); err != nil {
		return nil, err
	}
	if authorize == nil {
		return nil, errors.New("knowledge base policy authorizer is required")
	}
	var saved *KBAccessState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var kb types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", kbID, caller.TenantID).First(&kb).Error; err != nil {
			return err
		}
		// Lock memberships in a consistent order, including the actor. Changes
		// to role/status or soft deletion cannot race this authorization.
		ids := []string{caller.UserID}
		for _, input := range update.Members {
			ids = append(ids, input.UserID)
		}
		var members []types.TenantMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND user_id IN ? AND status = ?", caller.TenantID, ids,
				types.TenantMemberStatusActive).Order("user_id ASC").Find(&members).Error; err != nil {
			return err
		}
		// User deactivation/deletion must not race the active-member checks.
		// Select only IDs so password hashes and private settings are not loaded.
		var users []types.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
			Where("id IN ? AND is_active = ?", ids, true).Order("id ASC").Find(&users).Error; err != nil {
			return err
		}
		state, err := loadKBAccessState(tx, caller, kbID)
		if err != nil {
			return err
		}
		if err := authorize(state); err != nil {
			return err
		}
		var version uint64
		if state.Policy != nil {
			version = state.Policy.Version
		}
		if update.ExpectedVersion != version || version >= 9223372036854775807 {
			return ErrKBAccessConflict
		}
		byUser := make(map[string]types.TenantMember, len(members))
		for _, member := range members {
			byUser[member.UserID] = member
		}
		var oldGrants []types.KnowledgeBaseMemberGrant
		if err := tx.Where("knowledge_base_id = ? AND tenant_id = ?", kbID, caller.TenantID).
			Find(&oldGrants).Error; err != nil {
			return err
		}
		oldMemberships := make(map[string]uint64, len(oldGrants))
		for _, grant := range oldGrants {
			oldMemberships[grant.UserID] = grant.MembershipID
		}
		activeUsers := make(map[string]bool, len(users))
		for _, user := range users {
			activeUsers[user.ID] = true
		}
		grants := make([]types.KnowledgeBaseMemberGrant, 0, len(update.Members))
		for _, input := range update.Members {
			member, ok := byUser[input.UserID]
			if !ok || !member.Role.IsValid() {
				return ErrKBAccessMember
			}
			if oldID, exists := oldMemberships[input.UserID]; exists && oldID != member.ID {
				// A stale, already-open form must not grant the new membership
				// merely because it still contains the same account ID. Remove
				// the old grant in a separate save before deliberately adding it.
				return ErrKBAccessMember
			}
			if !activeUsers[input.UserID] {
				return ErrKBAccessMember
			}
			grants = append(grants, types.KnowledgeBaseMemberGrant{
				KnowledgeBaseID: kbID, TenantID: caller.TenantID, UserID: input.UserID,
				MembershipID: member.ID, Permission: input.Permission,
			})
		}
		policy := &types.KnowledgeBaseAccessPolicy{
			KnowledgeBaseID: kbID, TenantID: caller.TenantID, Mode: update.Mode,
			Version: version + 1, UpdatedBy: caller.UserID, UpdatedAt: time.Now().UTC(),
		}
		if err := tx.Save(policy).Error; err != nil {
			return err
		}
		if err := tx.Where("knowledge_base_id = ? AND tenant_id = ?", kbID, caller.TenantID).
			Delete(&types.KnowledgeBaseMemberGrant{}).Error; err != nil {
			return err
		}
		if len(grants) > 0 {
			if err := tx.Create(&grants).Error; err != nil {
				return err
			}
		}
		saved = &KBAccessState{KB: state.KB, Actor: state.Actor, Policy: policy, Members: grants}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}
