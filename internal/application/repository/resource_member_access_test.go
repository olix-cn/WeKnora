package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestResourceMemberPolicyAppliesToRegisteredAndLegacyFiles(t *testing.T) {
	db := newKBAccessTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE knowledges
		(id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, file_path TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.AutoMigrate(&types.ResourceBinding{}))
	require.NoError(t, db.Exec(`INSERT INTO knowledges (id,tenant_id,knowledge_base_id,file_path)
		VALUES ('doc',7,'kb','local://7/private.txt')`).Error)
	require.NoError(t, db.Create(&types.ResourceBinding{
		ID: "binding", ResourceID: "resource", TenantID: 7,
		OwnerType: types.ResourceOwnerKnowledge, OwnerID: "doc", Relation: types.ResourceRelationSourceFile,
	}).Error)
	owner := types.Caller{TenantID: 7, UserID: "owner"}
	_, err := NewKnowledgeBaseAccessRepository(db).Replace(context.Background(), owner, "kb",
		types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{
			{UserID: "editor", Permission: types.OrgRoleViewer},
		}}, allowKBAccessManager(owner))
	require.NoError(t, err)
	resources := &resourceRepository{db: db}
	for _, resourceID := range []string{"resource", ""} {
		ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "viewer"})
		err = resources.CheckResourceMemberAccess(ctx, 7, resourceID, "local://7/private.txt", false)
		require.ErrorIs(t, err, access.ErrForbidden)
		ctx = types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "editor"})
		require.NoError(t, resources.CheckResourceMemberAccess(ctx, 7, resourceID, "local://7/private.txt", false))
		require.ErrorIs(t, resources.CheckResourceMemberAccess(ctx, 7, resourceID, "local://7/private.txt", true),
			access.ErrForbidden, "an authorized human cannot mint an anonymous URL for restricted content")
		ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true})
		require.ErrorIs(t, resources.CheckResourceMemberAccess(ctx, 7, resourceID, "local://7/private.txt", false),
			access.ErrForbidden, "full-access machine key must not inherit human membership")
	}
}
