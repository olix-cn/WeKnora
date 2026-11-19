package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type restrictedTaskTransferShares struct{ interfaces.KBShareService }

func (*restrictedTaskTransferShares) CheckKBTransferPolicy(context.Context, string, uint64) error {
	return access.ErrForbidden
}

func TestQueuedTransferRechecksRestrictedSourceBeforeAnyMutation(t *testing.T) {
	f := transferFixture(t, access.KBTransferMove)
	f.svc.kbShareService = &restrictedTaskTransferShares{}
	err := f.svc.ProcessKnowledgeMove(context.Background(), moveTask(t, []string{"doc"}, "reuse_vectors"))
	require.ErrorIs(t, err, asynq.SkipRetry)
	row, err := f.repo.GetKnowledgeByID(f.ctx, 7, "doc")
	require.NoError(t, err)
	require.Equal(t, "kb", row.KnowledgeBaseID)
	require.Zero(t, f.chunkRepo.writes)
	require.Zero(t, f.graph.calls)
	payload, err := json.Marshal(types.KBClonePayload{
		TenantID: 7, SourceID: "kb", TargetID: "new", TaskID: "clone", CreateTarget: true,
	})
	require.NoError(t, err)
	err = f.svc.ProcessKBClone(context.Background(), asynq.NewTask(types.TypeKBClone, payload))
	require.ErrorIs(t, err, asynq.SkipRetry)
}

type failedTargetBatchKBs struct {
	interfaces.KnowledgeBaseService
}

func (failedTargetBatchKBs) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return nil, access.ErrForbidden
}

func TestSearchTargetsNeverAssumeTenantAccessWhenKBMetadataIsMissing(t *testing.T) {
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "user"})
	for _, kb := range []interfaces.KnowledgeBaseService{
		failedTargetBatchKBs{}, &tagTargetKnowledgeBaseService{kbs: map[string]*types.KnowledgeBase{}},
	} {
		s := &sessionService{knowledgeBaseService: kb}
		targets, err := s.buildSearchTargets(ctx, 7, []string{"restricted"}, nil, nil)
		require.NoError(t, err)
		require.Empty(t, targets)
	}
}
