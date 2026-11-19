package repository

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLegacyUnknownSourceTranscriptsFailClosedWithoutDeletingContent(t *testing.T) {
	db := newKBAccessTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.Message{}))
	require.NoError(t, db.Exec(`CREATE TABLE sessions
		(id TEXT PRIMARY KEY, tenant_id INTEGER, user_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec("INSERT INTO sessions VALUES ('legacy',7,'editor',NULL),('new',7,'editor',NULL)").Error)
	require.NoError(t, db.Create(&types.Message{
		SessionID: "legacy", Role: "assistant", Content: "synthetic old paraphrase",
	}).Error)
	up, err := os.ReadFile("../../../migrations/sqlite/000057_session_knowledge_provenance.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	require.NoError(t, db.Create(&types.Message{
		SessionID: "new", Role: "assistant", Content: "synthetic new plain chat",
	}).Error)
	r := &sessionRepository{db: db}
	caller := types.Caller{TenantID: 7, UserID: "editor"}
	ctx := types.WithCaller(context.Background(), caller)
	require.NoError(t, r.CheckSessionKBMemberAccess(ctx, "legacy", caller),
		"no restriction preserves historical access")
	owner := types.Caller{TenantID: 7, UserID: "owner"}
	_, err = NewKnowledgeBaseAccessRepository(db).Replace(context.Background(), owner, "kb",
		types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(owner))
	require.NoError(t, err)
	require.ErrorIs(t, r.CheckSessionKBMemberAccess(ctx, "legacy", caller), access.ErrForbidden)
	require.NoError(t, r.CheckSessionKBMemberAccess(ctx, "new", caller))
	denied, err := r.DeniedSessionIDs(ctx, 7, "editor")
	require.NoError(t, err)
	require.Equal(t, []string{"legacy"}, denied)
	require.NoError(t, r.CheckSessionKBMemberAccess(context.Background(), "legacy", owner))
	var rows []types.Message
	require.NoError(t, db.Where("session_id = ?", "legacy").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "synthetic old paraphrase", rows[0].Content)
}

func TestSessionProvenanceTriggersInvalidateRawWritesAndRetainEarlierSources(t *testing.T) {
	db := newArtifactTestDB(t)
	ctx := context.Background()
	// Treat a pre-migration transcript exactly like an old deployed session.
	message := &types.Message{
		SessionID: "session", Role: "assistant",
		ExecutionContext: types.MessageExecutionContext{KnowledgeBaseIDs: []string{"first"}},
	}
	require.NoError(t, db.Create(message).Error)
	up, err := os.ReadFile("../../../migrations/sqlite/000057_session_knowledge_provenance.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	r := &sessionRepository{db: db}
	ids, err := r.sessionSourceIDs(ctx, "session")
	require.NoError(t, err)
	require.True(t, ids["first"])
	// Metadata written and replaced without any intervening history read is
	// still evidence; invalidation alone cannot recover an overwritten value.
	require.NoError(t, db.Exec("UPDATE messages SET execution_context = ? WHERE id = ?",
		`{"knowledge_base_ids":["unread-source"]}`, message.ID).Error)
	require.NoError(t, db.Exec("UPDATE messages SET execution_context = ? WHERE id = ?",
		`{"knowledge_base_ids":["first"]}`, message.ID).Error)
	ids, err = r.sessionSourceIDs(ctx, "session")
	require.NoError(t, err)
	require.True(t, ids["unread-source"])
	var revision sessionKnowledgeRevision
	require.NoError(t, db.First(&revision).Error)
	require.Equal(t, revision.Revision, revision.IndexedRevision)
	// Rendering/content-only updates invalidate the revision but must not
	// duplicate large tool traces in the temporary evidence buffer.
	require.NoError(t, db.Exec("UPDATE messages SET content = ? WHERE id = ?", "synthetic answer", message.ID).Error)
	var evidence int64
	require.NoError(t, db.Table("session_knowledge_evidence").Count(&evidence).Error)
	require.Zero(t, evidence)
	// Raw SQL, not a repository hook: every persistence path invalidates.
	require.NoError(t, db.Exec("UPDATE messages SET execution_context = ? WHERE id = ?",
		`{"knowledge_base_ids":["second"]}`, message.ID).Error)
	require.NoError(t, db.First(&revision).Error)
	require.Greater(t, revision.Revision, revision.IndexedRevision)
	ids, err = r.sessionSourceIDs(ctx, "session")
	require.NoError(t, err)
	require.True(t, ids["first"], "later paraphrases can depend on an earlier source even if its citation was replaced")
	require.True(t, ids["second"])
	require.NoError(t, db.Exec("DELETE FROM messages WHERE id = ?", message.ID).Error)
	ids, err = r.sessionSourceIDs(ctx, "session")
	require.NoError(t, err)
	require.True(t, ids["first"])
	require.True(t, ids["second"], "deleting a citation is not evidence of declassification")
	down, err := os.ReadFile("../../../migrations/sqlite/000057_session_knowledge_provenance.down.sql")
	require.NoError(t, err)
	require.Error(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }),
		"rollback must not silently forget a dependency whose citation was deleted")
	require.NoError(t, db.Exec("DELETE FROM session_knowledge_sources").Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }))
	_, err = r.sessionSourceIDs(ctx, "session")
	require.Error(t, err, "missing provenance migration must not silently authorize legacy reads")
}

func TestIndexedSessionListDoesNotRescanThousandsOfUnchangedTranscripts(t *testing.T) {
	db := newKBAccessTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.Message{}))
	up, err := os.ReadFile("../../../migrations/sqlite/000057_session_knowledge_provenance.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	require.NoError(t, db.Exec(`CREATE TABLE sessions
		(id TEXT PRIMARY KEY, tenant_id INTEGER, user_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<2100)
		INSERT INTO sessions(id,tenant_id,user_id) SELECT 's-'||x,7,'viewer' FROM n`).Error)
	require.NoError(t, db.Exec(`INSERT INTO session_knowledge_revisions(session_id,revision,indexed_revision)
		SELECT id,1,1 FROM sessions`).Error)
	require.NoError(t, db.Create(&types.KnowledgeBaseAccessPolicy{
		KnowledgeBaseID: "kb", TenantID: 7, Mode: types.KBAccessRestricted, Version: 1, UpdatedBy: "owner",
	}).Error)
	require.NoError(t, db.Exec(`INSERT INTO session_knowledge_sources(session_id,knowledge_base_id)
		VALUES ('s-1','kb'), ('s-2100','kb')`).Error)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "viewer"})
	denied, err := (&sessionRepository{db: db}).DeniedSessionIDs(ctx, 7, "viewer")
	require.NoError(t, err, "the cold-build budget must not limit already indexed histories")
	require.ElementsMatch(t, []string{"s-1", "s-2100"}, denied)
	require.NoError(t, db.Exec("UPDATE session_knowledge_revisions SET indexed_revision=0").Error)
	denied, err = (&sessionRepository{db: db}).DeniedSessionIDs(ctx, 7, "viewer")
	require.ErrorContains(t, err, "backfill")
	require.Nil(t, denied, "a partial cold rebuild must never authorize a partially checked list")
}

func TestExplicitProvenanceBackfillIsBoundedAndResumable(t *testing.T) {
	db := newArtifactTestDB(t)
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Create(&types.Message{
			SessionID: fmt.Sprintf("session-%d", i), Role: "assistant",
			ExecutionContext: types.MessageExecutionContext{KnowledgeBaseIDs: []string{"source"}},
		}).Error)
	}
	up, err := os.ReadFile("../../../migrations/sqlite/000057_session_knowledge_provenance.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	cursor, count, err := BackfillSessionProvenance(context.Background(), db, "", 2)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Equal(t, "session-1", cursor)
	cursor, count, err = BackfillSessionProvenance(context.Background(), db, cursor, 2)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, "session-2", cursor)
	_, count, err = BackfillSessionProvenance(context.Background(), db, "", 2)
	require.NoError(t, err)
	require.Zero(t, count)
	_, _, err = BackfillSessionProvenance(context.Background(), db, "", 501)
	require.Error(t, err)
}

func TestUnindexedSoftDeletedMessageRetainsSourceEvidence(t *testing.T) {
	db := newArtifactTestDB(t)
	up, err := os.ReadFile("../../../migrations/sqlite/000057_session_knowledge_provenance.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	message := &types.Message{
		SessionID: "legacy", Role: "assistant",
		ExecutionContext: types.MessageExecutionContext{KnowledgeBaseIDs: []string{"sensitive"}},
	}
	require.NoError(t, db.Create(message).Error)
	require.NoError(t, db.Delete(message).Error)
	ids, err := (&sessionRepository{db: db}).sessionSourceIDs(context.Background(), "legacy")
	require.NoError(t, err)
	require.True(t, ids["sensitive"], "soft deletion before the first index read must not drop the source")
}
