package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This opt-in suite only accepts an explicitly supplied disposable database.
// Each invocation creates and removes its own random schema, using synthetic
// identities only. Never point this at a deployment/business database.
func kbMemberPostgresFixture(t *testing.T) (*gorm.DB, *KnowledgeBaseAccessRepository, types.Caller) {
	t.Helper()
	dsn := os.Getenv("WEKNORA_KB_MEMBER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set WEKNORA_KB_MEMBER_TEST_POSTGRES_DSN to a disposable PostgreSQL database")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	schema := "kb_member_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() {
		require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
		require.NoError(t, adminSQL.Close())
	})
	separator := " "
	if strings.Contains(dsn, "://") {
		separator = "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
	}
	db, err := gorm.Open(postgres.Open(dsn+separator+"search_path="+schema),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(10)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	// Only existing model tables; the new policy tables come from the real SQL.
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.TenantMember{}, &types.KnowledgeBase{},
		&types.CustomAgent{}, &types.KnowledgeBaseShare{}, &types.AgentShare{}, &types.AuditLog{}))
	up, err := os.ReadFile("../../../migrations/versioned/000140_knowledge_base_member_access.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	require.NoError(t, db.Create(&types.Tenant{ID: 7, Name: "Synthetic PG workspace"}).Error)
	for i, user := range []string{"owner", "editor"} {
		require.NoError(t, db.Create(&types.User{ID: user, Username: user, TenantID: 7}).Error)
		role := types.TenantRoleContributor
		if user == "owner" {
			role = types.TenantRoleOwner
		}
		require.NoError(t, db.Create(&types.TenantMember{
			ID: uint64(i + 1), TenantID: 7, UserID: user, Role: role, Status: types.TenantMemberStatusActive,
		}).Error)
	}
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", TenantID: 7, CreatorID: "owner"}).Error)
	require.NoError(t, db.Create(&types.Organization{
		ID: "org", Name: "Synthetic org", OwnerID: "owner", OwnerTenantID: 7,
	}).Error)
	return db, NewKnowledgeBaseAccessRepository(db), types.Caller{TenantID: 7, UserID: "owner"}
}

func TestKBMemberPostgresConcurrentPolicyAndSharing(t *testing.T) {
	for _, operation := range []string{"second-save", "kb-share", "agent-share", "agent-scope"} {
		t.Run(operation, func(t *testing.T) {
			db, repo, caller := kbMemberPostgresFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			agent := &types.CustomAgent{
				ID: "agent", TenantID: 7, Config: types.CustomAgentConfig{KBSelectionMode: "none"},
			}
			require.NoError(t, db.Create(agent).Error)
			if operation == "agent-scope" {
				require.NoError(t, NewAgentShareRepository(db).Create(ctx, &types.AgentShare{
					ID: "existing-share", AgentID: "agent", SourceTenantID: 7, OrganizationID: "org",
				}))
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, err := repo.ReplaceManaged(ctx, caller, "kb",
					types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(caller))
				results <- err
			}()
			go func() {
				defer wg.Done()
				<-start
				var err error
				switch operation {
				case "second-save":
					_, err = repo.ReplaceManaged(ctx, caller, "kb",
						types.KBAccessPolicyUpdate{Mode: types.KBAccessInherit}, allowKBAccessManager(caller))
				case "kb-share":
					err = NewKBShareRepository(db).Create(ctx, &types.KnowledgeBaseShare{
						ID: "share", KnowledgeBaseID: "kb", SourceTenantID: 7, OrganizationID: "org",
					})
				case "agent-share":
					// Persist the scope before the race; share creation must reload it.
					agent.Config = types.CustomAgentConfig{KBSelectionMode: "all"}
					if err = NewCustomAgentRepository(db).UpdateAgent(ctx, agent); err == nil {
						err = NewAgentShareRepository(db).Create(ctx, &types.AgentShare{
							ID: "share", AgentID: "agent", SourceTenantID: 7, OrganizationID: "org",
						})
					}
				case "agent-scope":
					agent.Config = types.CustomAgentConfig{KBSelectionMode: "selected", KnowledgeBases: []string{"kb"}}
					err = NewCustomAgentRepository(db).UpdateAgent(ctx, agent)
				}
				results <- err
			}()
			close(start)
			wg.Wait()
			close(results)
			successes, failures := 0, 0
			for err := range results {
				if err == nil {
					successes++
				} else {
					failures++
					if operation == "second-save" {
						require.ErrorIs(t, err, ErrKBAccessConflict)
					} else {
						require.ErrorIs(t, err, ErrKBAccessShared)
					}
				}
			}
			require.Equal(t, 1, successes)
			require.Equal(t, 1, failures)
		})
	}
}

func TestKBMemberPostgresMigrationRollbackGuard(t *testing.T) {
	db, repo, caller := kbMemberPostgresFixture(t)
	ctx := context.Background()
	down, err := os.ReadFile("../../../migrations/versioned/000140_knowledge_base_member_access.down.sql")
	require.NoError(t, err)
	_, err = repo.ReplaceManaged(ctx, caller, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted},
		allowKBAccessManager(caller))
	require.NoError(t, err)
	err = db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error })
	require.ErrorContains(t, err, "permissions are in use")
	require.True(t, db.Migrator().HasTable(&types.KnowledgeBaseAccessPolicy{}))
	_, err = repo.ReplaceManaged(ctx, caller, "kb", types.KBAccessPolicyUpdate{
		Mode: types.KBAccessInherit, ExpectedVersion: 1,
		Members: []types.KBMemberGrantInput{{UserID: "editor", Permission: types.OrgRoleViewer}},
	},
		allowKBAccessManager(caller))
	require.NoError(t, err)
	err = db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error })
	require.Error(t, err, "explicit grants also prevent a lossy rollback")
	_, err = repo.ReplaceManaged(ctx, caller, "kb",
		types.KBAccessPolicyUpdate{Mode: types.KBAccessInherit, ExpectedVersion: 2}, allowKBAccessManager(caller))
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }))
	require.False(t, db.Migrator().HasTable(&types.KnowledgeBaseAccessPolicy{}))
}

func TestKBMemberPostgresMembershipRevocationIsRecheckedUnderLock(t *testing.T) {
	db, repo, caller := kbMemberPostgresFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	locked := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&types.TenantMember{}).Where("id = ?", 1).
				Update("status", types.TenantMemberStatusSuspended).Error; err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-locked:
	case err := <-finished:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	result := make(chan error, 1)
	go func() {
		_, err := repo.ReplaceManaged(ctx, caller, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted},
			allowKBAccessManager(caller))
		result <- err
	}()
	close(release)
	require.NoError(t, <-finished)
	err := <-result
	require.True(t, errors.Is(err, ErrKBAccessMember) || errors.Is(err, access.ErrForbidden), "%v", err)
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeBaseAccessPolicy{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestKBMemberPostgresManagedSaveActuallyWaitsForWorkspaceRowLock(t *testing.T) {
	db, repo, caller := kbMemberPostgresFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	holder := db.WithContext(ctx).Begin()
	require.NoError(t, holder.Error)
	t.Cleanup(func() { _ = holder.Rollback().Error })
	require.NoError(t, holder.Exec("SELECT id FROM tenants WHERE id = 7 FOR UPDATE").Error)
	err := db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("SET lock_timeout = '150ms'").Error; err != nil {
			return err
		}
		defer conn.Exec("RESET lock_timeout")
		_, err := NewKnowledgeBaseAccessRepository(conn).ReplaceManaged(ctx, caller, "kb",
			types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(caller))
		return err
	})
	require.ErrorContains(t, err, "55P03", "a separate connection must block on the database lock")
	require.NoError(t, holder.Rollback().Error)
	_, err = repo.ReplaceManaged(ctx, caller, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted},
		allowKBAccessManager(caller))
	require.NoError(t, err)
}

func TestKBMemberPostgresMachineAndForeignRolesCannotUseRestrictedGrant(t *testing.T) {
	db, repo, owner := kbMemberPostgresFixture(t)
	ctx := context.Background()
	_, err := repo.ReplaceManaged(ctx, owner, "kb", types.KBAccessPolicyUpdate{
		Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{
			{UserID: "editor", Permission: types.OrgRoleEditor},
		},
	}, allowKBAccessManager(owner))
	require.NoError(t, err)
	lookup := NewKnowledgeBaseRepository(db).(access.KBMemberLookup)
	for _, caller := range []types.Caller{
		{TenantID: 7},
		{TenantID: 8, UserID: "editor", Role: types.TenantRoleOwner},
		{TenantID: 7, UserID: "im-synthetic", Role: types.TenantRoleAdmin},
	} {
		permission, handled, err := lookup.CheckKBMemberAccess(ctx, "kb", 7, caller)
		require.NoError(t, err)
		require.True(t, handled)
		require.Empty(t, permission)
	}
	machine := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true})
	permission, handled, err := lookup.CheckKBMemberAccess(machine, "kb", 7, owner)
	require.NoError(t, err)
	require.True(t, handled)
	require.Empty(t, permission, "even a key carrying a real owner's ID cannot reuse that user's grant")
}

func TestKBMemberPostgresTransferFenceClosesPolicyWriteWindow(t *testing.T) {
	db, policies, caller := kbMemberPostgresFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "target", TenantID: 7, CreatorID: "owner"}).Error)
	started, finish := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- (&knowledgeBaseRepository{db: db}).RunKBTransferFenced(ctx, 7, "kb", "target", "task", func() error {
			close(started)
			select {
			case <-finish:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for _, id := range []string{"kb", "target"} {
		_, err := policies.ReplaceManaged(ctx, caller, id,
			types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(caller))
		require.ErrorIs(t, err, ErrKBAccessTransferBusy)
	}
	close(finish)
	require.NoError(t, <-done)
	_, err := policies.ReplaceManaged(ctx, caller, "kb",
		types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(caller))
	require.NoError(t, err)
}

func TestKBMemberPostgresFailedTransferRecoveryIsAtomic(t *testing.T) {
	db, policies, caller := kbMemberPostgresFixture(t)
	ctx := context.Background()
	err := (&knowledgeBaseRepository{db: db}).RunKBTransferFenced(ctx, 7, "kb", "target", "task", func() error {
		return errors.New("synthetic failed write")
	})
	require.Error(t, err)
	var fence types.KBTransferFence
	require.NoError(t, db.First(&fence).Error)
	require.Equal(t, "failed", fence.State)
	input := types.KBTransferRecovery{
		FenceID: fence.ID, TaskID: fence.TaskID, SourceID: fence.SourceID, TargetID: fence.TargetID,
		WorkerStopped: true, OutcomeReconciled: true, Reason: "Synthetic PG outcome reconciled with stopped worker",
	}
	require.NoError(t, policies.RecoverFailedTransfer(ctx, caller, input))
	var count int64
	require.NoError(t, db.Model(&types.KBTransferFence{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.AuditLog{}).Where("action = ?", types.AuditActionKBTransferRecovered).
		Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestKBMemberPostgresProvenanceMigrationRetainsOverwrittenEvidence(t *testing.T) {
	db, _, _ := kbMemberPostgresFixture(t)
	require.NoError(t, db.Exec(`CREATE TABLE sessions
		(id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT, user_id VARCHAR(36), deleted_at TIMESTAMPTZ)`).Error)
	require.NoError(t, db.AutoMigrate(&types.Message{}))
	ctx := context.Background()
	message := &types.Message{
		SessionID: "session", Role: "assistant",
		ExecutionContext: types.MessageExecutionContext{KnowledgeBaseIDs: []string{"before-migration"}},
	}
	require.NoError(t, db.Create(message).Error)
	up, err := os.ReadFile("../../../migrations/versioned/000141_session_knowledge_provenance.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	// Replace a legacy source before the first indexed read. The trigger
	// must preserve the old evidence, not merely mark a revision dirty.
	require.NoError(t, db.Exec("UPDATE messages SET execution_context = ? WHERE id = ?",
		`{"knowledge_base_ids":["after-migration"]}`, message.ID).Error)
	r := &sessionRepository{db: db}
	ids, err := r.sessionSourceIDs(ctx, "session")
	require.NoError(t, err)
	require.True(t, ids["before-migration"])
	require.True(t, ids["after-migration"])
	require.NoError(t, db.Exec("INSERT INTO sessions(id,tenant_id,user_id) VALUES ('session',7,'editor')").Error)
	require.NoError(t, db.Create(&types.KnowledgeBaseAccessPolicy{
		KnowledgeBaseID: "kb", TenantID: 7, Mode: types.KBAccessRestricted, Version: 1, UpdatedBy: "owner",
	}).Error)
	require.NoError(t, db.Exec(`INSERT INTO session_knowledge_sources(session_id,knowledge_base_id)
		VALUES ('session','kb')`).Error)
	denied, err := r.DeniedSessionIDs(types.WithCaller(ctx, types.Caller{TenantID: 7, UserID: "editor"}), 7, "editor")
	require.NoError(t, err)
	require.Equal(t, []string{"session"}, denied, "PostgreSQL reverse-index exclusions must match SQLite")
	var evidence int64
	require.NoError(t, db.Table("session_knowledge_evidence").Count(&evidence).Error)
	require.Zero(t, evidence, "evidence is compacted atomically after indexing")
	require.NoError(t, db.Exec("DELETE FROM messages WHERE id = ?", message.ID).Error)
	ids, err = r.sessionSourceIDs(ctx, "session")
	require.NoError(t, err)
	require.True(t, ids["before-migration"])
	down, err := os.ReadFile("../../../migrations/versioned/000141_session_knowledge_provenance.down.sql")
	require.NoError(t, err)
	require.Error(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }))
	require.True(t, db.Migrator().HasTable(&sessionKnowledgeSource{}))
	require.NoError(t, db.Exec("DELETE FROM session_knowledge_sources").Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }))
}

func TestKBMemberPostgresConcurrentSourceRefreshCannotMarkNewWritesClean(t *testing.T) {
	db, _, _ := kbMemberPostgresFixture(t)
	require.NoError(t, db.AutoMigrate(&types.Message{}))
	up, err := os.ReadFile("../../../migrations/versioned/000141_session_knowledge_provenance.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	message := &types.Message{
		SessionID: "session", Role: "assistant",
		ExecutionContext: types.MessageExecutionContext{KnowledgeBaseIDs: []string{"initial"}},
	}
	require.NoError(t, db.Create(message).Error)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := &sessionRepository{db: db}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := r.sessionSourceIDs(ctx, "session")
		results <- err
	}()
	go func() {
		<-start
		results <- db.WithContext(ctx).Exec("UPDATE messages SET execution_context = ? WHERE id = ?",
			`{"knowledge_base_ids":["concurrent-source"]}`, message.ID).Error
	}()
	close(start)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	ids, err := r.sessionSourceIDs(ctx, "session")
	require.NoError(t, err)
	require.True(t, ids["initial"])
	require.True(t, ids["concurrent-source"], "a concurrently committed write must invalidate or be included")
	var revision sessionKnowledgeRevision
	require.NoError(t, db.First(&revision, "session_id = ?", "session").Error)
	require.Equal(t, revision.Revision, revision.IndexedRevision)
}
