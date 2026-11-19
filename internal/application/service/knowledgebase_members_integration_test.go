package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKBMemberServiceRealRepositoryLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "policy-service.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.TenantMember{},
		&types.KnowledgeBase{}, &types.KnowledgeBaseAccessPolicy{}, &types.KnowledgeBaseMemberGrant{},
		&types.KnowledgeBaseShare{}, &types.CustomAgent{}, &types.AgentShare{}, &types.AuditLog{},
		&types.KBTransferFence{}))
	require.NoError(t, db.Create(&types.Tenant{ID: 7, Name: "Synthetic workspace"}).Error)
	for _, user := range []string{"creator", "delegate", "reader"} {
		require.NoError(t, db.Create(&types.User{ID: user, Username: user, TenantID: 7}).Error)
		require.NoError(t, db.Create(&types.TenantMember{
			TenantID: 7, UserID: user, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive,
		}).Error)
	}
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", TenantID: 7, CreatorID: "creator"}).Error)
	store := repository.NewKnowledgeBaseAccessRepository(db)
	svc := NewKnowledgeBaseMemberService(store)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "creator"})
	view, err := svc.Get(ctx, "kb")
	require.NoError(t, err)
	require.Zero(t, view.Version)
	view, err = svc.Replace(ctx, "kb", types.KBAccessPolicyUpdate{
		Mode: types.KBAccessRestricted,
		Members: []types.KBMemberGrantInput{
			{UserID: "delegate", Permission: types.OrgRoleAdmin},
			{UserID: "reader", Permission: types.OrgRoleViewer},
		},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, view.Version)
	_, err = svc.Get(ctx, "kb")
	require.ErrorIs(t, err, access.ErrForbidden, "creator removed from restricted roster loses management")
	delegate := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "delegate"})
	view, err = svc.Get(delegate, "kb")
	require.NoError(t, err)
	serialized, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "membership_id")
	require.NotContains(t, string(serialized), "embedding_model")
	rows, total, err := svc.MemberCandidates(delegate, "kb", "read", 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "reader", rows[0].Username)
	_, err = svc.Replace(delegate, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessInherit})
	require.ErrorIs(t, err, repository.ErrKBAccessConflict)
	reader := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "reader"})
	_, _, err = svc.MemberCandidates(reader, "kb", "", 1, 20)
	require.ErrorIs(t, err, access.ErrForbidden)
	view, err = svc.Replace(delegate, "kb", types.KBAccessPolicyUpdate{
		Mode: types.KBAccessInherit, ExpectedVersion: 1, Members: nil,
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, view.Version)
	_, err = svc.Get(delegate, "kb")
	require.ErrorIs(t, err, access.ErrForbidden, "delegation does not survive its removal")
	_, err = svc.Get(ctx, "kb")
	require.NoError(t, err, "original creator regains inherited authority")
}
