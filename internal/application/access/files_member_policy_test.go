package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type memberDeniedFileCatalog struct{ fileCatalog }

func (memberDeniedFileCatalog) AuthorizeResourceMemberAccess(context.Context, uint64, string, bool) error {
	return ErrForbidden
}

func TestMemberPolicyDeniesSameWorkspaceMessageAndArtifactFiles(t *testing.T) {
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "user"})
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	catalog := memberDeniedFileCatalog{fileCatalog{resource: &types.StoredResource{
		TenantID: 7, PhysicalPath: "local://7/private.txt",
	}}}
	message := &types.Message{
		ID: "message", Role: "assistant", AgentTenantID: 7, Content: ref,
		Artifacts: []types.MessageArtifact{{URL: ref, FileName: "private.txt"}},
	}
	_, err := AuthorizeMessageFile(ctx, message, ref, nil, catalog, MessageKBShareAuthorizer{})
	require.ErrorIs(t, err, ErrForbidden)
	_, err = ResolveMessageArtifact(ctx, message, 0, nil, catalog, MessageKBShareAuthorizer{})
	require.ErrorIs(t, err, ErrForbidden, "same-tenant artifact shortcut must not declassify a restricted KB file")
	grant := &KBAccess{
		Caller: types.CallerFromContext(ctx), KnowledgeBase: &types.KnowledgeBase{ID: "kb", TenantID: 7},
		EffectiveTenantID: 7, Permission: types.OrgRoleViewer,
	}
	_, err = ResolveKBFile(ctx, grant, "kb", ref, catalog, &fileBinding{allowed: true})
	require.ErrorIs(t, err, ErrForbidden)
}
