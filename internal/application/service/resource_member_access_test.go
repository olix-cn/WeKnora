package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func TestRestrictedResourceRevokesPreviouslyMintedAnonymousGrant(t *testing.T) {
	catalog, db := newResourceCatalogForTest(t)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}))
	ctx := context.Background()
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "private", TenantID: 7}).Error)
	require.NoError(t, db.Create(&types.Knowledge{ID: "doc", TenantID: 7, KnowledgeBaseID: "private"}).Error)
	ref, err := catalog.Register(ctx, 7, "local://7/file.txt", interfaces.ResourceRegistration{})
	require.NoError(t, err)
	require.NoError(t, catalog.Bind(ctx, ref, types.ResourceOwnerKnowledge, "doc", types.ResourceRelationSourceFile))
	token, err := catalog.CreateAccessGrant(ctx, ref, time.Minute)
	require.NoError(t, err)
	_, err = catalog.ResolveAccessGrant(ctx, token)
	require.NoError(t, err)
	require.NoError(t, db.Create(&types.KnowledgeBaseAccessPolicy{
		KnowledgeBaseID: "private", TenantID: 7, Mode: types.KBAccessRestricted, Version: 1, UpdatedBy: "owner",
	}).Error)
	_, err = catalog.ResolveAccessGrant(ctx, token)
	require.ErrorIs(t, err, access.ErrForbidden, "old anonymous capabilities must observe the current restriction")
	_, err = catalog.CreateAccessGrant(ctx, ref, time.Minute)
	require.ErrorIs(t, err, access.ErrForbidden)
	guard := catalog.(interfaces.ResourceMemberAccessGuard)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 7, UserID: "unlisted"})
	// Do not require membership tables for an anonymous capability check.
	require.ErrorIs(t, guard.AuthorizeResourceMemberAccess(ctx, 7, ref, true), access.ErrForbidden)
	require.ErrorIs(t, guard.AuthorizeResourceMemberAccess(ctx, 7, "local://7/file.txt", true), access.ErrForbidden,
		"registered physical aliases must use the resource's authoritative bindings")
}
