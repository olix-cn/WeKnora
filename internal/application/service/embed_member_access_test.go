package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type restrictedEmbedChunks struct{ interfaces.ChunkService }

func (*restrictedEmbedChunks) GetChunkByIDOnly(context.Context, string) (*types.Chunk, error) {
	return &types.Chunk{ID: "chunk", KnowledgeBaseID: "private", TenantID: 7, Content: "synthetic private"}, nil
}

func (*restrictedEmbedChunks) CheckChunkMemberAccess(context.Context, string, uint64) error {
	return access.ErrForbidden
}

func TestEmbedDirectChunkCannotBypassMemberPolicy(t *testing.T) {
	s := &embedChannelService{chunkService: &restrictedEmbedChunks{}}
	// agentService deliberately nil: direct chunk policy must reject before
	// even considering an agent's all/selected scope.
	chunk, err := s.EmbedChunk(context.Background(), &types.EmbedChannel{TenantID: 7}, "chunk")
	require.ErrorIs(t, err, ErrEmbedChunkForbidden)
	require.Nil(t, chunk)
}
