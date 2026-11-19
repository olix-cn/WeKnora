package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestTransferFenceBlocksSourceAndDestinationPolicyUntilSuccess(t *testing.T) {
	db, policies, caller := memberSharingFixture(t)
	ctx := context.Background()
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases (id,tenant_id,creator_id)
		VALUES ('target',7,'owner')`).Error)
	fences := &knowledgeBaseRepository{db: db}
	err := fences.RunKBTransferFenced(ctx, 7, "kb", "target", "task", func() error {
		for _, id := range []string{"kb", "target"} {
			_, err := policies.ReplaceManaged(ctx, caller, id,
				types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(caller))
			require.ErrorIs(t, err, ErrKBAccessTransferBusy)
		}
		return nil
	})
	require.NoError(t, err)
	_, err = policies.ReplaceManaged(ctx, caller, "kb",
		types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}, allowKBAccessManager(caller))
	require.NoError(t, err, "successful attempt releases only its own fence")
	called := false
	err = fences.RunKBTransferFenced(ctx, 7, "kb", "target", "later", func() error { called = true; return nil })
	require.ErrorIs(t, err, ErrKBAccessShared)
	require.False(t, called, "restriction is checked atomically before execution admission")
}

func TestFailedTransferFenceCannotBeClearedBySuccessfulRetry(t *testing.T) {
	db, policies, caller := memberSharingFixture(t)
	ctx := context.Background()
	fences := &knowledgeBaseRepository{db: db}
	failure := errors.New("synthetic external write outcome unknown")
	require.ErrorIs(t, fences.RunKBTransferFenced(ctx, 7, "kb", "target", "task", func() error {
		return failure
	}), failure)
	require.NoError(t, fences.RunKBTransferFenced(ctx, 7, "kb", "target", "task", func() error { return nil }))
	var rows []types.KBTransferFence
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1, "a successful sibling/retry cannot resolve an earlier partial or crashed attempt")
	_, err := policies.ReplaceManaged(ctx, caller, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted},
		allowKBAccessManager(caller))
	require.ErrorIs(t, err, ErrKBAccessTransferBusy)
}
