package repository

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// PlatformKBPermissionRow is metadata only: no document content, model or storage secrets.
type PlatformKBPermissionRow struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	TenantID    uint64             `json:"tenant_id"`
	TenantName  string             `json:"tenant_name"`
	CreatorID   string             `json:"creator_id"`
	Mode        types.KBAccessMode `json:"mode"`
	Version     uint64             `json:"version"`
	MemberCount int64              `json:"member_count"`
}

// ListPlatformPolicies is a distinct, current-system-admin-only control-plane surface.
func (r *KnowledgeBaseAccessRepository) ListPlatformPolicies(
	ctx context.Context, keyword string, tenant uint64, offset, limit int,
) ([]PlatformKBPermissionRow, int64, error) {
	if len(keyword) > 200 || offset < 0 || offset > 1000000 || limit < 1 || limit > 100 {
		return nil, 0, access.ErrInvalidKBAccessPolicy
	}
	rows := []PlatformKBPermissionRow{}
	var total int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := directoryActor(ctx, tx); err != nil {
			return err
		}
		q := tx.Table("knowledge_bases k").Joins("JOIN tenants t ON t.id=k.tenant_id AND t.deleted_at IS NULL").
			Joins("LEFT JOIN knowledge_base_access_policies p ON p.knowledge_base_id=k.id").
			Where("k.deleted_at IS NULL")
		if tenant != 0 {
			q = q.Where("k.tenant_id = ?", tenant)
		}
		if keyword = strings.TrimSpace(keyword); keyword != "" {
			pattern := "%" + escapeLikePattern(keyword) + "%"
			q = q.Where("(LOWER(k.name) LIKE LOWER(?) ESCAPE ? OR LOWER(t.name) LIKE LOWER(?) ESCAPE ?)",
				pattern, likeEscapeChar, pattern, likeEscapeChar)
		}
		if err := q.Count(&total).Error; err != nil {
			return err
		}
		return q.Select("k.id,k.name,k.tenant_id,t.name AS tenant_name,k.creator_id," +
			"COALESCE(p.mode,'inherit') AS mode,COALESCE(p.version,0) AS version," +
			"(SELECT COUNT(*) FROM knowledge_base_member_grants g JOIN tenant_members m ON m.id=g.membership_id " +
			"AND m.user_id=g.user_id AND m.tenant_id=g.tenant_id AND m.deleted_at IS NULL " +
			"JOIN users u ON u.id=g.user_id AND u.deleted_at IS NULL AND u.is_active = true " +
			"WHERE g.knowledge_base_id=k.id AND m.status='active') AS member_count").
			Order("t.name ASC,k.name ASC,k.id ASC").Offset(offset).Limit(limit).Scan(&rows).Error
	})
	return rows, total, err
}

// platformScope resolves resource tenancy only after platform authority is proven.
// It never changes the authenticated request's tenant or grants content access.
func (r *KnowledgeBaseAccessRepository) platformScope(ctx context.Context, kbID string) (types.Caller, error) {
	var caller types.Caller
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		id, err := directoryActor(ctx, tx)
		if err != nil {
			return err
		}
		var kb types.KnowledgeBase
		if err := tx.Select("id", "tenant_id").First(&kb, "id = ?", kbID).Error; err != nil {
			return err
		}
		caller = types.Caller{TenantID: kb.TenantID, UserID: id}
		return nil
	})
	return caller, err
}

// ReadPlatformPolicy rechecks authority within the locked policy read transaction.
func (r *KnowledgeBaseAccessRepository) ReadPlatformPolicy(ctx context.Context, kbID string) (*KBAccessState, error) {
	caller, err := r.platformScope(ctx, kbID)
	if err != nil {
		return nil, err
	}
	var state *KBAccessState
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		state, err = NewKnowledgeBaseAccessRepository(tx).Read(ctx, caller, kbID)
		if err != nil {
			return err
		}
		if _, err = directoryActor(ctx, tx); err != nil {
			return err
		}
		ids := make([]string, 0, len(state.Members))
		for _, g := range state.Members {
			ids = append(ids, g.UserID)
		}
		if len(ids) == 0 {
			return nil
		}
		return tx.Table("tenant_members m").Select("m.user_id,u.username,m.role").
			Joins("JOIN users u ON u.id=m.user_id AND u.deleted_at IS NULL AND u.is_active = ?", true).
			Where("m.tenant_id = ? AND m.user_id IN ? AND m.status = ? AND m.deleted_at IS NULL", caller.TenantID, ids,
				types.TenantMemberStatusActive).Scan(&state.MemberDetails).Error
	})
	return state, err
}

// ReplacePlatformPolicy preserves all workspace sharing/fence/version/member rules.
func (r *KnowledgeBaseAccessRepository) ReplacePlatformPolicy(
	ctx context.Context, kbID string, input types.KBAccessPolicyUpdate,
) (*KBAccessState, error) {
	caller, err := r.platformScope(ctx, kbID)
	if err != nil {
		return nil, err
	}
	var state *KBAccessState
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		state, err = NewKnowledgeBaseAccessRepository(tx).replaceManaged(ctx, caller, kbID, input,
			func(*KBAccessState) error { _, err := directoryActor(ctx, tx); return err }, true)
		return err
	})
	return state, err
}

// PlatformCandidates only returns active members of the target KB's real workspace.
func (r *KnowledgeBaseAccessRepository) PlatformCandidates(
	ctx context.Context, kbID, search string, page, size int,
) ([]types.KBAccessMemberCandidate, int64, error) {
	caller, err := r.platformScope(ctx, kbID)
	if err != nil {
		return nil, 0, err
	}
	var rows []types.KBAccessMemberCandidate
	var total int64
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		rows, total, err = NewKnowledgeBaseAccessRepository(tx).MemberCandidates(ctx, caller, kbID, search, page, size,
			func(*KBAccessState) error { _, err := directoryActor(ctx, tx); return err })
		return err
	})
	return rows, total, err
}
