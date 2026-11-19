package repository

import (
	"context"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRegisterSessionSourcesBeforeFirstAnswerAndRevoke(t *testing.T) {
	db := newKBAccessTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.Message{}))
	up, err := os.ReadFile("../../../migrations/sqlite/000057_session_knowledge_provenance.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	require.NoError(t, db.Exec(`CREATE TABLE sessions
		(id TEXT PRIMARY KEY, tenant_id INTEGER, user_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec("INSERT INTO sessions VALUES ('turn',7,'editor',NULL)").Error)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "editor")
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 7, UserID: "editor"})
	r := &sessionRepository{db: db}
	targets := types.SearchTargets{{KnowledgeBaseID: "kb", TenantID: 7}}
	// Registration precedes the first message, while the KB still inherits.
	require.NoError(t, r.RegisterSessionKnowledgeSources(ctx, "turn", targets))
	require.NoError(t, r.RegisterSessionKnowledgeSources(ctx, "turn", targets))
	var count int64
	require.NoError(t, db.Model(&sessionKnowledgeSource{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	owner := types.Caller{TenantID: 7, UserID: "owner"}
	_, err = NewKnowledgeBaseAccessRepository(db).Replace(context.Background(), owner, "kb",
		types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(owner))
	require.NoError(t, err)
	require.ErrorIs(t, r.CheckSessionKBMemberAccess(ctx, "turn", types.CallerFromContext(ctx)), access.ErrForbidden)
	require.ErrorIs(t, r.RegisterSessionKnowledgeSources(ctx, "turn", targets), access.ErrForbidden)
	// A forged target tenant cannot turn a same-space restriction into an
	// unmanaged cross-space grant, nor may a caller register another owner.
	targets[0].TenantID = 8
	require.ErrorIs(t, r.RegisterSessionKnowledgeSources(ctx, "turn", targets), access.ErrForbidden)
	wrong := context.WithValue(ctx, types.UserIDContextKey, "owner")
	require.ErrorIs(t, r.RegisterSessionKnowledgeSources(wrong, "turn", targets), access.ErrForbidden)
	require.NoError(t, db.Exec("UPDATE sessions SET deleted_at = CURRENT_TIMESTAMP WHERE id = 'turn'").Error)
	require.Error(t, r.RegisterSessionKnowledgeSources(ctx, "turn", targets))
}

func TestRegisterSessionSourcesSharedAgentAndMachineOwners(t *testing.T) {
	db := newKBAccessTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE sessions
		(id TEXT PRIMARY KEY, tenant_id INTEGER, user_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.AutoMigrate(&sessionKnowledgeSource{}))
	require.NoError(t, db.Exec(`INSERT INTO sessions VALUES
		('shared',7,'editor',NULL),('machine',7,'mcp_endpoint:7:endpoint',NULL)`).Error)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "editor")
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 7, UserID: "editor"})
	r := &sessionRepository{db: db}
	// The actual retrieval authorization is upstream. Registration preserves
	// the original caller and session tenant even for a foreign agent source.
	require.NoError(t, r.RegisterSessionKnowledgeSources(types.WithExecutionTenant(ctx, 8), "shared",
		types.SearchTargets{{KnowledgeBaseID: "foreign", TenantID: 8}}))
	machine := types.WithPrincipal(ctx, types.MCPEndpointPrincipal(7, "endpoint"))
	require.NoError(t, r.RegisterSessionKnowledgeSources(machine, "machine",
		types.SearchTargets{{KnowledgeBaseID: "kb", TenantID: 7}}))
}
