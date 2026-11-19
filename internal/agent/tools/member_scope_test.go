package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type revokedScopeKnowledgeService struct{ scopeKnowledgeService }

func (*revokedScopeKnowledgeService) CheckKnowledgeMemberAccess(context.Context, string, uint64) error {
	return errors.New("synthetic revoked knowledge base membership")
}

func TestCapturedAgentScopeCannotOverrideRevokedKBMembership(t *testing.T) {
	service := &revokedScopeKnowledgeService{scopeKnowledgeService{knowledge: &types.Knowledge{
		ID: "doc", TenantID: 7, KnowledgeBaseID: "kb",
	}}}
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb", TenantID: 7}}
	_, err := authorizeKnowledgeInSearchTargets(context.Background(), targets, "doc", service)
	require.ErrorContains(t, err, "revoked")
	chunks := &scopeChunkService{chunk: &types.Chunk{
		ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", TenantID: 7, IsEnabled: true,
	}}
	_, err = authorizeChunkInSearchTargets(context.Background(), targets, "chunk", chunks, service)
	require.ErrorContains(t, err, "revoked")
}
