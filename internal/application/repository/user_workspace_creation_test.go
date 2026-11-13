package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateUserWithMembershipIsAtomic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.TenantMember{}))
	require.NoError(t, db.Create(&types.Tenant{ID: 42, Name: "Engineering", Status: "active"}).Error)
	repo := &userRepository{db: db}
	ctx := context.Background()
	user := &types.User{ID: "created", Username: "created", PasswordHash: "hash", TenantID: 42, IsActive: true}
	require.NoError(t, repo.CreateUserWithMembership(ctx, user, types.TenantRoleViewer, "admin"))
	var membership types.TenantMember
	require.NoError(t, db.Where("user_id = ?", user.ID).First(&membership).Error)
	require.Equal(t, types.TenantRoleViewer, membership.Role)
	require.Equal(t, types.TenantMemberStatusActive, membership.Status)
	require.Equal(t, "admin", *membership.InvitedBy)
	var stored types.User
	require.NoError(t, db.First(&stored, "id = ?", user.ID).Error)
	require.Equal(t, uint64(42), stored.TenantID)
	require.False(t, stored.IsSystemAdmin)

	// Force membership persistence failure after the user INSERT.
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_member BEFORE INSERT ON tenant_members BEGIN SELECT RAISE(ABORT, 'injected failure'); END`).Error)
	failing := &types.User{ID: "failed", Username: "failed", TenantID: 42, PasswordHash: "hash"}
	require.Error(t, repo.CreateUserWithMembership(ctx, failing, types.TenantRoleAdmin, "admin"))
	var count int64
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", failing.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id = ?", failing.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestCreateUserWithMembershipRejectsUnavailableWorkspace(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.TenantMember{}))
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Disabled", Status: "inactive"}).Error)
	repo := &userRepository{db: db}
	for _, id := range []uint64{1, 999} {
		user := &types.User{ID: "new", Username: "new", TenantID: id, PasswordHash: "hash"}
		require.ErrorIs(t, repo.CreateUserWithMembership(context.Background(), user, types.TenantRoleViewer, ""), ErrProvisioningTenantUnavailable)
	}
	var count int64
	require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
	require.Zero(t, count)
}
