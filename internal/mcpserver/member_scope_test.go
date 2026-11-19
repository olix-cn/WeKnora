package mcpserver

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type memberRestrictedMCPShare struct{ stubKBShareService }

func (*memberRestrictedMCPShare) CheckKBMemberAccess(
	context.Context, string, uint64, types.Caller,
) (types.OrgMemberRole, bool, error) {
	return "", true, nil
}

func TestMCPEndpointCannotReadMemberRestrictedKBByExplicitID(t *testing.T) {
	s := newScopeTestServer(&types.KnowledgeBase{ID: "restricted", TenantID: 7})
	s.kbShareService = &memberRestrictedMCPShare{}
	ep := &types.MCPEndpoint{ID: "endpoint", TenantID: 7, KnowledgeBaseIDs: types.StringArray{"restricted"}}
	ctx := mcpCallContext(7, ep)
	rows, err := s.allowedKnowledgeBases(ctx, ep)
	require.NoError(t, err)
	require.Empty(t, rows)
	_, err = s.authorizedKnowledgeBase(ctx, "restricted", types.OrgRoleViewer)
	require.Error(t, err)
	_, err = s.authorizedKnowledgeBase(ctx, "restricted", types.OrgRoleEditor)
	require.Error(t, err)
}
