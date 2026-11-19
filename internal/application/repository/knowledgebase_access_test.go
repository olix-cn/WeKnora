package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newKBAccessTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "kb-access.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Exec("PRAGMA foreign_keys=ON").Error)
	// Minimal pre-existing schema; use the actual migration for access tables.
	require.NoError(t, db.Exec(`CREATE TABLE knowledge_bases
		(id TEXT PRIMARY KEY, tenant_id INTEGER, creator_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.TenantMember{}))
	require.NoError(t, db.Exec("INSERT INTO tenants (id, name) VALUES (7, 'test-space'), (8, 'other-space')").Error)
	up, err := os.ReadFile("../../../migrations/sqlite/000056_knowledge_base_member_access.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	for _, id := range []string{"owner", "editor", "viewer", "outsider"} {
		require.NoError(t, db.Create(&types.User{ID: id, Username: id, TenantID: 7}).Error)
	}
	for _, member := range []types.TenantMember{
		{ID: 1, UserID: "owner", TenantID: 7, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive},
		{
			ID: 2, UserID: "editor", TenantID: 7, Role: types.TenantRoleContributor,
			Status: types.TenantMemberStatusActive,
		},
		{ID: 3, UserID: "viewer", TenantID: 7, Role: types.TenantRoleViewer, Status: types.TenantMemberStatusActive},
		{ID: 4, UserID: "outsider", TenantID: 8, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive},
	} {
		require.NoError(t, db.Create(&member).Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases (id,tenant_id,creator_id)
		VALUES ('kb',7,'owner'),('foreign',8,'outsider')`).Error)
	return db
}

func allowKBAccessManager(caller types.Caller) func(*KBAccessState) error {
	return func(state *KBAccessState) error {
		var grant *types.KnowledgeBaseMemberGrant
		for i := range state.Members {
			if state.Members[i].UserID == caller.UserID {
				grant = &state.Members[i]
			}
		}
		permission, ok := access.WorkspaceKBPermission(caller, state.KB, state.Policy, state.Actor, grant)
		if !ok || permission != types.OrgRoleAdmin {
			return access.ErrForbidden
		}
		return nil
	}
}

func TestKBAccessRepositoryAtomicReplacementAndVersion(t *testing.T) {
	db := newKBAccessTestDB(t)
	repo := NewKnowledgeBaseAccessRepository(db)
	ctx := context.Background()
	caller := types.Caller{TenantID: 7, UserID: "owner"}
	before, err := repo.Read(ctx, caller, "kb")
	require.NoError(t, err)
	require.Nil(t, before.Policy, "legacy KB has no policy row")
	require.Empty(t, before.Members)
	update := types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{
		{UserID: "editor", Permission: types.OrgRoleEditor},
	}}
	saved, err := repo.Replace(ctx, caller, "kb", update, allowKBAccessManager(caller))
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.Policy.Version)
	require.EqualValues(t, 2, saved.Members[0].MembershipID)
	_, err = repo.Replace(ctx, caller, "kb", update, allowKBAccessManager(caller))
	require.ErrorIs(t, err, ErrKBAccessConflict)
	update.ExpectedVersion = 1
	update.Members = []types.KBMemberGrantInput{{UserID: "outsider", Permission: types.OrgRoleAdmin}}
	_, err = repo.Replace(ctx, caller, "kb", update, allowKBAccessManager(caller))
	require.ErrorIs(t, err, ErrKBAccessMember)
	unchanged, err := repo.Read(ctx, caller, "kb")
	require.NoError(t, err)
	require.Equal(t, saved.Policy.Version, unchanged.Policy.Version)
	require.Equal(t, saved.Members, unchanged.Members)
	update.Members = nil
	saved, err = repo.Replace(ctx, caller, "kb", update, allowKBAccessManager(caller))
	require.NoError(t, err)
	require.EqualValues(t, 2, saved.Policy.Version)
	require.Empty(t, saved.Members)
	_, err = repo.Read(ctx, caller, "foreign")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestKBAccessRepositoryReauthorizesAndFailsClosed(t *testing.T) {
	db := newKBAccessTestDB(t)
	repo := NewKnowledgeBaseAccessRepository(db)
	ctx := context.Background()
	owner := types.Caller{TenantID: 7, UserID: "owner"}
	update := types.KBAccessPolicyUpdate{Mode: types.KBAccessInherit}
	_, err := repo.Replace(ctx, owner, "kb", update, nil)
	require.Error(t, err)
	_, err = repo.Replace(ctx, owner, "kb", types.KBAccessPolicyUpdate{Mode: "invalid"}, allowKBAccessManager(owner))
	require.ErrorIs(t, err, access.ErrInvalidKBAccessPolicy)
	// A forged stale owner role cannot replace the current membership role.
	caller := types.Caller{TenantID: 7, UserID: "editor", Role: types.TenantRoleOwner}
	_, err = repo.Replace(ctx, caller, "kb", update, allowKBAccessManager(caller))
	require.ErrorIs(t, err, access.ErrForbidden)
	require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id = ?", "owner").
		Update("status", types.TenantMemberStatusSuspended).Error)
	_, err = repo.Replace(ctx, owner, "kb", update, allowKBAccessManager(owner))
	require.ErrorIs(t, err, access.ErrForbidden)
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeBaseAccessPolicy{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestKBAccessMembershipRemovalDoesNotResurrectGrant(t *testing.T) {
	db := newKBAccessTestDB(t)
	repo := NewKnowledgeBaseAccessRepository(db)
	ctx := context.Background()
	owner := types.Caller{TenantID: 7, UserID: "owner"}
	update := types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{
		{UserID: "editor", Permission: types.OrgRoleAdmin},
	}}
	_, err := repo.Replace(ctx, owner, "kb", update, allowKBAccessManager(owner))
	require.NoError(t, err)
	editor := types.Caller{TenantID: 7, UserID: "editor"}
	state, err := repo.Read(ctx, editor, "kb")
	require.NoError(t, err)
	require.NoError(t, allowKBAccessManager(editor)(state))
	require.NoError(t, db.Where("id = ?", 2).Delete(&types.TenantMember{}).Error)
	require.NoError(t, db.Create(&types.TenantMember{
		ID: 5, TenantID: 7, UserID: "editor", Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive,
	}).Error)
	state, err = repo.Read(ctx, editor, "kb")
	require.NoError(t, err)
	require.ErrorIs(t, allowKBAccessManager(editor)(state), access.ErrForbidden)
	update.ExpectedVersion = 1
	_, err = repo.Replace(ctx, editor, "kb", update, allowKBAccessManager(editor))
	require.ErrorIs(t, err, access.ErrForbidden)
	// A deleted user with a surviving membership is also not a manager.
	require.NoError(t, db.Where("id = ?", "owner").Delete(&types.User{}).Error)
	state, err = repo.Read(ctx, owner, "kb")
	require.NoError(t, err)
	require.Nil(t, state.Actor)
}

func TestKBAccessRepositoryRollsBackFailedMemberInsert(t *testing.T) {
	db := newKBAccessTestDB(t)
	repo := NewKnowledgeBaseAccessRepository(db)
	ctx := context.Background()
	owner := types.Caller{TenantID: 7, UserID: "owner"}
	update := types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}
	_, err := repo.Replace(ctx, owner, "kb", update, allowKBAccessManager(owner))
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_kb_grant BEFORE INSERT ON knowledge_base_member_grants
		BEGIN SELECT RAISE(ABORT,'synthetic insert failure'); END`).Error)
	update.ExpectedVersion = 1
	update.Members = []types.KBMemberGrantInput{{UserID: "editor", Permission: types.OrgRoleViewer}}
	_, err = repo.Replace(ctx, owner, "kb", update, allowKBAccessManager(owner))
	require.Error(t, err)
	state, err := repo.Read(ctx, owner, "kb")
	require.NoError(t, err)
	require.EqualValues(t, 1, state.Policy.Version)
	require.Empty(t, state.Members)
}

func TestKBAccessSQLiteRollbackGuard(t *testing.T) {
	for _, explicitGrant := range []bool{false, true} {
		t.Run(map[bool]string{false: "restricted", true: "inherit-with-grant"}[explicitGrant], func(t *testing.T) {
			db := newKBAccessTestDB(t)
			repo := NewKnowledgeBaseAccessRepository(db)
			owner := types.Caller{TenantID: 7, UserID: "owner"}
			update := types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}
			if explicitGrant {
				update.Mode = types.KBAccessInherit
				update.Members = []types.KBMemberGrantInput{{UserID: "editor", Permission: types.OrgRoleEditor}}
			}
			_, err := repo.Replace(context.Background(), owner, "kb", update, allowKBAccessManager(owner))
			require.NoError(t, err)
			down, err := os.ReadFile("../../../migrations/sqlite/000056_knowledge_base_member_access.down.sql")
			require.NoError(t, err)
			err = db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error })
			require.Error(t, err)
			require.True(t, db.Migrator().HasTable(&types.KnowledgeBaseAccessPolicy{}))
			require.True(t, db.Migrator().HasTable(&types.KnowledgeBaseMemberGrant{}))
			update.Mode, update.ExpectedVersion, update.Members = types.KBAccessInherit, 1, nil
			_, err = repo.Replace(context.Background(), owner, "kb", update, allowKBAccessManager(owner))
			require.NoError(t, err)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }))
			require.False(t, db.Migrator().HasTable(&types.KnowledgeBaseAccessPolicy{}))
		})
	}
}

func TestKBAccessRepositoryMissingMigrationIsNotInherit(t *testing.T) {
	db := newKBAccessTestDB(t)
	require.NoError(t, db.Exec("DROP TABLE knowledge_base_member_grants").Error)
	_, err := NewKnowledgeBaseAccessRepository(db).Read(
		context.Background(), types.Caller{TenantID: 7, UserID: "owner"}, "kb")
	require.Error(t, err)
	require.False(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestKBAccessRepositoryRejectsUnavailableGrantTargets(t *testing.T) {
	for _, unavailable := range []string{"suspended", "deleted-membership", "inactive-user", "deleted-user"} {
		t.Run(unavailable, func(t *testing.T) {
			db := newKBAccessTestDB(t)
			switch unavailable {
			case "suspended":
				require.NoError(t, db.Model(&types.TenantMember{}).Where("id = ?", 2).
					Update("status", types.TenantMemberStatusSuspended).Error)
			case "deleted-membership":
				require.NoError(t, db.Where("id = ?", 2).Delete(&types.TenantMember{}).Error)
			case "inactive-user":
				require.NoError(t, db.Model(&types.User{}).Where("id = ?", "editor").Update("is_active", false).Error)
			case "deleted-user":
				require.NoError(t, db.Where("id = ?", "editor").Delete(&types.User{}).Error)
			}
			owner := types.Caller{TenantID: 7, UserID: "owner"}
			_, err := NewKnowledgeBaseAccessRepository(db).Replace(context.Background(), owner, "kb",
				types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{
					{UserID: "editor", Permission: types.OrgRoleViewer},
				}}, allowKBAccessManager(owner))
			require.ErrorIs(t, err, ErrKBAccessMember)
			var count int64
			require.NoError(t, db.Model(&types.KnowledgeBaseAccessPolicy{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestKBAccessSQLiteSchemaRejectsInvalidPolicyAndOrphanGrant(t *testing.T) {
	db := newKBAccessTestDB(t)
	require.Error(t, db.Exec(`INSERT INTO knowledge_base_access_policies
		(knowledge_base_id,tenant_id,mode,version,updated_by,updated_at)
		VALUES ('kb',7,'unknown',1,'owner',CURRENT_TIMESTAMP)`).Error)
	require.Error(t, db.Exec(`INSERT INTO knowledge_base_access_policies
		(knowledge_base_id,tenant_id,mode,version,updated_by,updated_at)
		VALUES ('kb',7,'inherit',0,'owner',CURRENT_TIMESTAMP)`).Error)
	require.Error(t, db.Create(&types.KnowledgeBaseMemberGrant{
		KnowledgeBaseID: "kb", TenantID: 7, UserID: "editor", MembershipID: 2, Permission: types.OrgRoleViewer,
	}).Error)
}

func TestRuntimeKBMemberLookupUsesLivePolicyAndMembership(t *testing.T) {
	db := newKBAccessTestDB(t)
	ctx := context.Background()
	runtime := NewKnowledgeBaseRepository(db).(access.KBMemberLookup)
	policies := NewKnowledgeBaseAccessRepository(db)
	owner := types.Caller{TenantID: 7, UserID: "owner", Role: types.TenantRoleOwner}
	editor := types.Caller{TenantID: 7, UserID: "editor", Role: types.TenantRoleOwner}
	_, handled, err := runtime.CheckKBMemberAccess(ctx, "kb", 7, editor)
	require.NoError(t, err)
	require.False(t, handled, "no policy leaves legacy behavior unchanged")
	update := types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{
		{UserID: "editor", Permission: types.OrgRoleEditor},
	}}
	_, err = policies.Replace(ctx, owner, "kb", update, allowKBAccessManager(owner))
	require.NoError(t, err)
	permission, handled, err := runtime.CheckKBMemberAccess(ctx, "kb", 7, editor)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, types.OrgRoleEditor, permission, "stale caller role must not grant admin")
	for _, caller := range []types.Caller{
		{TenantID: 7, UserID: "viewer"}, {TenantID: 7}, {TenantID: 8, UserID: "outsider"},
	} {
		permission, handled, err = runtime.CheckKBMemberAccess(ctx, "kb", 7, caller)
		require.NoError(t, err)
		require.True(t, handled)
		require.Empty(t, permission)
	}
	_, handled, err = runtime.CheckKBMemberAccess(ctx, "kb", 8, owner)
	require.NoError(t, err)
	require.True(t, handled, "mismatched persisted tenant must never trigger legacy fallback")
	require.NoError(t, db.Where("id = ?", 2).Delete(&types.TenantMember{}).Error)
	permission, handled, err = runtime.CheckKBMemberAccess(ctx, "kb", 7, editor)
	require.NoError(t, err)
	require.True(t, handled)
	require.Empty(t, permission, "no cross-operation authorization cache")
}

func TestRejoinedMemberIsNotReauthorizedByStaleRosterSave(t *testing.T) {
	db := newKBAccessTestDB(t)
	repo := NewKnowledgeBaseAccessRepository(db)
	ctx := context.Background()
	owner := types.Caller{TenantID: 7, UserID: "owner"}
	update := types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{
		{UserID: "editor", Permission: types.OrgRoleAdmin},
	}}
	_, err := repo.Replace(ctx, owner, "kb", update, allowKBAccessManager(owner))
	require.NoError(t, err)
	require.NoError(t, db.Where("id = ?", 2).Delete(&types.TenantMember{}).Error)
	require.NoError(t, db.Create(&types.TenantMember{
		ID: 5, TenantID: 7, UserID: "editor", Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive,
	}).Error)
	state, err := repo.Read(ctx, owner, "kb")
	require.NoError(t, err)
	require.Empty(t, state.Members, "a freshly loaded roster excludes stale grants")
	update.ExpectedVersion = 1
	_, err = repo.Replace(ctx, owner, "kb", update, allowKBAccessManager(owner))
	require.ErrorIs(t, err, ErrKBAccessMember, "an already-open form cannot resurrect the stale grant")
	update.Members = nil
	_, err = repo.Replace(ctx, owner, "kb", update, allowKBAccessManager(owner))
	require.NoError(t, err)
	update.ExpectedVersion = 2
	update.Members = []types.KBMemberGrantInput{{UserID: "editor", Permission: types.OrgRoleViewer}}
	_, err = repo.Replace(ctx, owner, "kb", update, allowKBAccessManager(owner))
	require.NoError(t, err, "explicit remove then re-add may authorize the new membership")
}

func TestRestrictedKBTransferPolicyFailsClosedIndependentlyOfReadGrants(t *testing.T) {
	db := newKBAccessTestDB(t)
	ctx := context.Background()
	lookup := NewKnowledgeBaseRepository(db).(access.KBTransferPolicyLookup)
	require.NoError(t, lookup.CheckKBTransferPolicy(ctx, "kb", 7))
	owner := types.Caller{TenantID: 7, UserID: "owner"}
	_, err := NewKnowledgeBaseAccessRepository(db).Replace(ctx, owner, "kb",
		types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(owner))
	require.NoError(t, err)
	require.ErrorIs(t, lookup.CheckKBTransferPolicy(ctx, "kb", 7), access.ErrForbidden)
	require.ErrorIs(t, lookup.CheckKBTransferPolicy(ctx, "kb", 8), access.ErrForbidden)
	require.NoError(t, db.Exec("DROP TABLE knowledge_base_member_grants").Error)
	require.NoError(t, db.Exec("DROP TABLE knowledge_base_access_policies").Error)
	require.Error(t, lookup.CheckKBTransferPolicy(ctx, "kb", 7), "missing migration is never a public source")
}
