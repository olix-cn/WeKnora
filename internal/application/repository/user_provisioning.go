package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

var ErrProvisioningTenantUnavailable = errors.New("default workspace is unavailable")

// CreateUserWithMembership commits the identity, home workspace and active role
// together. No half-created identity is visible and no invitation is issued.
func (r *userRepository) CreateUserWithMembership(ctx context.Context, user *types.User, role types.TenantRole, actorID string) error {
	if user == nil || user.TenantID == 0 || !role.IsValid() {
		return errors.New("workspace and valid role are required")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tenant types.Tenant
		if err := tx.Clauses(forUpdateClause()).Where("id = ? AND status = ?", user.TenantID, "active").First(&tenant).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProvisioningTenantUnavailable
			}
			return err
		}
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		member := &types.TenantMember{UserID: user.ID, TenantID: user.TenantID, Role: role,
			Status: types.TenantMemberStatusActive, JoinedAt: time.Now()}
		if actorID != "" {
			member.InvitedBy = &actorID
		}
		return tx.Create(member).Error
	})
}
