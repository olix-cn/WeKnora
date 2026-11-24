package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPlatformKBPermissionsManageWithoutWorkspaceMembershipOrContentGrant(t *testing.T) {
	db, repo, _ := memberSharingFixture(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.Exec("ALTER TABLE knowledge_bases ADD COLUMN name TEXT").Error)
	require.NoError(t, db.Exec("UPDATE knowledge_bases SET name=id").Error)
	require.NoError(t, db.Create(&types.User{
		ID: "platform", Username: "platform", TenantID: 7, IsSystemAdmin: true,
	}).Error)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "platform")
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 8, UserID: "platform"})
	rows, total, err := repo.ListPlatformPolicies(ctx, "", 0, 0, 20)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, rows, 2)
	state, err := repo.ReadPlatformPolicy(ctx, "kb")
	require.NoError(t, err)
	require.Nil(t, state.Actor)
	saved, err := repo.ReplacePlatformPolicy(ctx, "kb", types.KBAccessPolicyUpdate{
		Mode:    types.KBAccessRestricted,
		Members: []types.KBMemberGrantInput{{UserID: "editor", Permission: types.OrgRoleEditor}},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.Policy.Version)
	candidates, _, err := repo.PlatformCandidates(ctx, "kb", "editor", 1, 20)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	state, err = repo.ReadPlatformPolicy(ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, "editor", state.MemberDetails[0].Username)
	// Global policy authority must not turn into a content-access shortcut.
	permission, handled, err := (&knowledgeBaseRepository{db: db}).CheckKBMemberAccess(
		ctx, "kb", 7, types.Caller{TenantID: 8, UserID: "platform"})
	require.NoError(t, err)
	require.True(t, handled)
	require.Empty(t, permission)
	_, err = repo.ReplacePlatformPolicy(ctx, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessInherit})
	require.ErrorIs(t, err, ErrKBAccessConflict)
	require.NoError(t, db.Create(&types.KBTransferFence{
		ID: "busy", TenantID: 7, SourceID: "kb", TargetID: "target", TaskID: "task", State: "active",
	}).Error)
	_, err = repo.ReplacePlatformPolicy(ctx, "kb", types.KBAccessPolicyUpdate{
		Mode: types.KBAccessInherit, ExpectedVersion: 1,
	})
	require.ErrorIs(t, err, ErrKBAccessTransferBusy)
	require.NoError(t, db.Where("id = ?", "busy").Delete(&types.KBTransferFence{}).Error)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "platform").Update("is_system_admin", false).Error)
	_, err = repo.ReadPlatformPolicy(ctx, "kb")
	require.ErrorIs(t, err, access.ErrForbidden)
}

func TestPlatformKBPermissionsRejectMachineAndForeignGrants(t *testing.T) {
	db, repo, _ := memberSharingFixture(t)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "owner").Update("is_system_admin", true).Error)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "owner")
	_, err := repo.ReplacePlatformPolicy(ctx, "kb", types.KBAccessPolicyUpdate{
		Mode:    types.KBAccessRestricted,
		Members: []types.KBMemberGrantInput{{UserID: "outsider", Permission: types.OrgRoleAdmin}},
	})
	require.ErrorIs(t, err, ErrKBAccessMember)
	key := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true})
	_, err = repo.ReadPlatformPolicy(key, "kb")
	require.ErrorIs(t, err, access.ErrForbidden)
	_, err = repo.ReplacePlatformPolicy(key, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessInherit})
	require.ErrorIs(t, err, access.ErrForbidden)
}

func TestPlatformKBPolicyStillRejectsSharingAndRollsBackAudit(t *testing.T) {
	db, repo, _ := memberSharingFixture(t)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "owner").Update("is_system_admin", true).Error)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "owner")
	require.NoError(t, NewKBShareRepository(db).Create(ctx, &types.KnowledgeBaseShare{
		ID: "share", KnowledgeBaseID: "kb", SourceTenantID: 7, OrganizationID: "org",
	}))
	_, err := repo.ReplacePlatformPolicy(ctx, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted})
	require.ErrorIs(t, err, ErrKBAccessShared)
	require.NoError(t, NewKBShareRepository(db).Delete(ctx, "share"))
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_platform_audit BEFORE INSERT ON audit_logs
  BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`).Error)
	_, err = repo.ReplacePlatformPolicy(ctx, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted})
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeBaseAccessPolicy{}).Count(&count).Error)
	require.Zero(t, count)
}
