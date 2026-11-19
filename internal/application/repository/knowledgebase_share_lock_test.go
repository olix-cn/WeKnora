package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func memberSharingFixture(t *testing.T) (*gorm.DB, *KnowledgeBaseAccessRepository, types.Caller) {
	t.Helper()
	db := newKBAccessTestDB(t)
	// Avoid AutoMigrate's association traversal altering the minimal KB table.
	require.NoError(t, db.Exec(`CREATE TABLE kb_shares (id TEXT PRIMARY KEY, knowledge_base_id TEXT,
		organization_id TEXT, source_tenant_id INTEGER, shared_by_user_id TEXT, permission TEXT,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE agent_shares (id TEXT PRIMARY KEY, agent_id TEXT,
		organization_id TEXT, source_tenant_id INTEGER, shared_by_user_id TEXT, permission TEXT,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`).Error)
	require.NoError(t, db.AutoMigrate(&types.CustomAgent{}))
	require.NoError(t, db.AutoMigrate(&types.AuditLog{}))
	return db, NewKnowledgeBaseAccessRepository(db), types.Caller{TenantID: 7, UserID: "owner"}
}

func TestManagedKBPolicyRejectsExistingSharesWithoutMutatingThem(t *testing.T) {
	for _, kind := range []string{"direct", "selected-agent", "all-agent"} {
		t.Run(kind, func(t *testing.T) {
			db, repo, caller := memberSharingFixture(t)
			ctx := context.Background()
			if kind == "direct" {
				require.NoError(t, NewKBShareRepository(db).Create(ctx, &types.KnowledgeBaseShare{
					ID: "share", KnowledgeBaseID: "kb", SourceTenantID: 7, OrganizationID: "org",
				}))
			} else {
				config := types.CustomAgentConfig{KBSelectionMode: "selected", KnowledgeBases: []string{"kb"}}
				if kind == "all-agent" {
					config.KBSelectionMode = "all"
				}
				require.NoError(t, db.Create(&types.CustomAgent{ID: "agent", TenantID: 7, Config: config}).Error)
				require.NoError(t, NewAgentShareRepository(db).Create(ctx, &types.AgentShare{
					ID: "share", AgentID: "agent", SourceTenantID: 7, OrganizationID: "org",
				}))
			}
			update := types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}
			_, err := repo.ReplaceManaged(ctx, caller, "kb", update, allowKBAccessManager(caller))
			require.ErrorIs(t, err, ErrKBAccessShared)
			state, err := repo.Read(ctx, caller, "kb")
			require.NoError(t, err)
			require.Nil(t, state.Policy)
			if kind == "direct" {
				require.NoError(t, NewKBShareRepository(db).Delete(ctx, "share"))
			} else {
				require.NoError(t, NewAgentShareRepository(db).Delete(ctx, "share"))
			}
			_, err = repo.ReplaceManaged(ctx, caller, "kb", update, allowKBAccessManager(caller))
			require.NoError(t, err, "explicit removal is required; never automatically delete an existing share")
		})
	}
}

func TestRestrictedKBRejectsLaterShareAndSharedAgentScopeExpansion(t *testing.T) {
	db, repo, caller := memberSharingFixture(t)
	ctx := context.Background()
	publicAgent := &types.CustomAgent{
		ID: "agent", TenantID: 7,
		Config: types.CustomAgentConfig{KBSelectionMode: "selected", KnowledgeBases: []string{"public"}},
	}
	require.NoError(t, db.Create(publicAgent).Error)
	require.NoError(t, NewAgentShareRepository(db).Create(ctx, &types.AgentShare{
		ID: "agent-share", AgentID: "agent", SourceTenantID: 7, OrganizationID: "org",
	}))
	_, err := repo.ReplaceManaged(ctx, caller, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted},
		allowKBAccessManager(caller))
	require.NoError(t, err)
	err = NewKBShareRepository(db).Create(ctx, &types.KnowledgeBaseShare{
		ID: "later", KnowledgeBaseID: "kb", SourceTenantID: 7, OrganizationID: "org",
	})
	require.ErrorIs(t, err, ErrKBAccessShared)
	for _, mode := range []string{"selected", "all"} {
		publicAgent.Config.KBSelectionMode, publicAgent.Config.KnowledgeBases = mode, []string{"kb"}
		err = NewCustomAgentRepository(db).UpdateAgent(ctx, publicAgent)
		require.ErrorIs(t, err, ErrKBAccessShared)
		stored, err := NewCustomAgentRepository(db).GetAgentByID(ctx, "agent", 7)
		require.NoError(t, err)
		require.Equal(t, []string{"public"}, stored.Config.KnowledgeBases)
	}
	require.NoError(t, db.Create(&types.CustomAgent{
		ID: "private-agent", TenantID: 7, Config: types.CustomAgentConfig{KBSelectionMode: "all"},
	}).Error)
	err = NewAgentShareRepository(db).Create(ctx, &types.AgentShare{
		ID: "later-agent", AgentID: "private-agent", SourceTenantID: 7, OrganizationID: "org",
	})
	require.ErrorIs(t, err, ErrKBAccessShared)
}

func TestManagedPolicyRejectsMissingShareSchemaRatherThanAssumingUnshared(t *testing.T) {
	db := newKBAccessTestDB(t)
	caller := types.Caller{TenantID: 7, UserID: "owner"}
	_, err := NewKnowledgeBaseAccessRepository(db).ReplaceManaged(context.Background(), caller, "kb",
		types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(caller))
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeBaseAccessPolicy{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestManagedPolicyConcurrentSavesNeverSilentlyOverwrite(t *testing.T) {
	db, repo, caller := memberSharingFixture(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// SQLite serializes writes; a single connection models that explicit
	// scheduling without pretending to test PostgreSQL SELECT FOR UPDATE.
	sqlDB.SetMaxOpenConns(1)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, mode := range []types.KBAccessMode{types.KBAccessInherit, types.KBAccessRestricted} {
		workers.Add(1)
		go func(mode types.KBAccessMode) {
			defer workers.Done()
			<-start
			_, err := repo.ReplaceManaged(context.Background(), caller, "kb",
				types.KBAccessPolicyUpdate{Mode: mode}, allowKBAccessManager(caller))
			results <- err
		}(mode)
	}
	close(start)
	workers.Wait()
	close(results)
	winners, conflicts := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, ErrKBAccessConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, winners)
	require.Equal(t, 1, conflicts)
	state, err := repo.Read(context.Background(), caller, "kb")
	require.NoError(t, err)
	require.EqualValues(t, 1, state.Policy.Version)
}

func TestManagedPolicyAndShareCreationCannotBothSucceed(t *testing.T) {
	for _, kind := range []string{"kb", "agent"} {
		t.Run(kind, func(t *testing.T) {
			db, repo, caller := memberSharingFixture(t)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			require.NoError(t, db.Create(&types.CustomAgent{
				ID: "agent", TenantID: 7, Config: types.CustomAgentConfig{KBSelectionMode: "all"},
			}).Error)
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				_, err := repo.ReplaceManaged(context.Background(), caller, "kb",
					types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(caller))
				results <- err
			}()
			go func() {
				<-start
				if kind == "kb" {
					results <- NewKBShareRepository(db).Create(context.Background(), &types.KnowledgeBaseShare{
						ID: "share", KnowledgeBaseID: "kb", SourceTenantID: 7, OrganizationID: "org",
					})
				} else {
					results <- NewAgentShareRepository(db).Create(context.Background(), &types.AgentShare{
						ID: "share", AgentID: "agent", SourceTenantID: 7, OrganizationID: "org",
					})
				}
			}()
			close(start)
			first, second := <-results, <-results
			if first == nil {
				require.ErrorIs(t, second, ErrKBAccessShared)
			} else {
				require.ErrorIs(t, first, ErrKBAccessShared)
				require.NoError(t, second)
			}
		})
	}
}

func TestShareUpdatesCannotRetargetRestrictedResources(t *testing.T) {
	db, repo, caller := memberSharingFixture(t)
	ctx := context.Background()
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id,tenant_id) VALUES ('public',7)").Error)
	shares := NewKBShareRepository(db)
	require.NoError(t, shares.Create(ctx, &types.KnowledgeBaseShare{
		ID: "share", KnowledgeBaseID: "public", SourceTenantID: 7, OrganizationID: "org",
	}))
	_, err := repo.ReplaceManaged(ctx, caller, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted},
		allowKBAccessManager(caller))
	require.NoError(t, err)
	require.NoError(t, shares.Update(ctx, &types.KnowledgeBaseShare{
		ID: "share", KnowledgeBaseID: "kb", SourceTenantID: 8, Permission: types.OrgRoleViewer,
	}))
	stored, err := shares.GetByID(ctx, "share")
	require.NoError(t, err)
	require.Equal(t, "public", stored.KnowledgeBaseID)
	require.EqualValues(t, 7, stored.SourceTenantID)
}

func TestKBPolicyCandidateSearchFiltersBeforePagination(t *testing.T) {
	db, repo, caller := memberSharingFixture(t)
	ctx := context.Background()
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "viewer").Update("is_active", false).Error)
	rows, total, err := repo.MemberCandidates(ctx, caller, "kb", "", 1, 1, allowKBAccessManager(caller))
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, rows, 1)
	require.Equal(t, "editor", rows[0].UserID)
	rows, total, err = repo.MemberCandidates(ctx, caller, "kb", "%", 1, 20, allowKBAccessManager(caller))
	require.NoError(t, err)
	require.Zero(t, total, "search wildcards are literal, not permission-directory expansion")
	require.Empty(t, rows)
	editor := types.Caller{TenantID: 7, UserID: "editor", Role: types.TenantRoleOwner}
	rows, _, err = repo.MemberCandidates(ctx, editor, "kb", "", 1, 20, allowKBAccessManager(editor))
	require.Error(t, err)
	require.Nil(t, rows, "unprivileged members must not receive the management directory")
}

func TestKBPolicyAuditIsAtomicAndContainsOnlyPolicySummary(t *testing.T) {
	db, repo, caller := memberSharingFixture(t)
	ctx := context.Background()
	update := types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}
	_, err := repo.ReplaceManaged(ctx, caller, "kb", update, allowKBAccessManager(caller))
	require.NoError(t, err)
	var events []types.AuditLog
	require.NoError(t, db.Find(&events).Error)
	require.Len(t, events, 1)
	require.Equal(t, types.AuditActionKBMemberAccessChanged, events[0].Action)
	require.Equal(t, "owner", events[0].ActorUserID)
	require.JSONEq(t, `{"mode":"restricted","version":1,"member_count":0}`, string(events[0].Details))
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_policy_audit BEFORE INSERT ON audit_logs
		BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`).Error)
	update.ExpectedVersion, update.Mode = 1, types.KBAccessInherit
	_, err = repo.ReplaceManaged(ctx, caller, "kb", update, allowKBAccessManager(caller))
	require.Error(t, err)
	state, err := repo.Read(ctx, caller, "kb")
	require.NoError(t, err)
	require.EqualValues(t, 1, state.Policy.Version)
	require.Equal(t, types.KBAccessRestricted, state.Policy.Mode)
}
