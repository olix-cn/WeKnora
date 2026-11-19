package repository

import (
	"context"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestTranscriptReplayRechecksRestrictedProvenanceAcrossAllPages(t *testing.T) {
	db := newKBAccessTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.Message{}))
	provenance, err := os.ReadFile("../../../migrations/sqlite/000057_session_knowledge_provenance.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(provenance)).Error)
	require.NoError(t, db.Exec(`CREATE TABLE sessions
		(id TEXT PRIMARY KEY, tenant_id INTEGER, user_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec("INSERT INTO sessions(id,tenant_id,user_id) VALUES ('conversation',7,'viewer')").Error)
	ctx := context.Background()
	owner := types.Caller{TenantID: 7, UserID: "owner"}
	_, err = NewKnowledgeBaseAccessRepository(db).Replace(ctx, owner, "kb", types.KBAccessPolicyUpdate{
		Mode:    types.KBAccessRestricted,
		Members: []types.KBMemberGrantInput{{UserID: "editor", Permission: types.OrgRoleViewer}},
	}, allowKBAccessManager(owner))
	require.NoError(t, err)
	old := &types.Message{
		ID: "old", SessionID: "conversation", Role: "assistant", Content: "synthetic confidential answer",
		ExecutionContext: types.MessageExecutionContext{KnowledgeBaseIDs: []string{"kb"}},
	}
	require.NoError(t, db.Create(old).Error)
	require.NoError(t, db.Create(&types.Message{
		ID: "recent", SessionID: "conversation", Role: "assistant",
		Content: "summary that could paraphrase the previous evidence",
	}).Error)
	sessions := &sessionRepository{db: db}
	err = sessions.CheckSessionKBMemberAccess(ctx, "conversation", types.Caller{TenantID: 7, UserID: "viewer"})
	require.ErrorIs(t, err, access.ErrForbidden)
	denied, err := sessions.DeniedSessionIDs(
		types.WithCaller(ctx, types.Caller{TenantID: 7, UserID: "viewer"}), 7, "viewer")
	require.NoError(t, err)
	require.Equal(t, []string{"conversation"}, denied)
	require.NoError(t, sessions.CheckSessionKBMemberAccess(ctx, "conversation",
		types.Caller{TenantID: 7, UserID: "editor"}))
	require.NoError(t, sessions.CheckSessionKBMemberAccess(ctx, "conversation", owner))
	var stored types.Message
	require.NoError(t, db.First(&stored, "id = ?", old.ID).Error)
	require.Equal(t, "synthetic confidential answer", stored.Content, "do not delete or rewrite the saved transcript")
	require.NoError(t, db.Where("id = ?", 2).Delete(&types.TenantMember{}).Error)
	err = sessions.CheckSessionKBMemberAccess(ctx, "conversation", types.Caller{TenantID: 7, UserID: "editor"})
	require.ErrorIs(t, err, access.ErrForbidden, "historical citations cannot be retained as an access grant")
}
