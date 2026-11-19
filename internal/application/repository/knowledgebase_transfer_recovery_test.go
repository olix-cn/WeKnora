package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestTransferRecoveryRequiresAdminStoppedWorkerAndReconciledOutcome(t *testing.T) {
	db, policies, owner := memberSharingFixture(t)
	ctx := context.Background()
	fences := &knowledgeBaseRepository{db: db}
	err := fences.RunKBTransferFenced(ctx, 7, "kb", "target", "task", func() error {
		return errors.New("synthetic partial result")
	})
	require.Error(t, err)
	var row types.KBTransferFence
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, "failed", row.State)
	statuses, err := policies.ListTransferFences(ctx, owner, 100)
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	require.Equal(t, row.ID, statuses[0].FenceID)
	require.Equal(t, "failed", statuses[0].State)
	_, err = policies.ListTransferFences(ctx,
		types.Caller{TenantID: 7, UserID: "editor", Role: types.TenantRoleOwner}, 100)
	require.ErrorIs(t, err, access.ErrForbidden, "a forged context role cannot read recovery coordinates")
	_, err = policies.ListTransferFences(types.WithPrincipal(ctx,
		types.Principal{Type: types.PrincipalIMUser, ID: "owner"}), owner, 100)
	require.ErrorIs(t, err, access.ErrForbidden, "machine principal cannot borrow owner grants")
	input := types.KBTransferRecovery{
		FenceID: row.ID, TaskID: row.TaskID, SourceID: row.SourceID, TargetID: row.TargetID,
		WorkerStopped: true, OutcomeReconciled: true, Reason: "Synthetic result verified and worker stopped",
	}
	for _, mutate := range []func(*types.KBTransferRecovery){
		func(i *types.KBTransferRecovery) { i.WorkerStopped = false },
		func(i *types.KBTransferRecovery) { i.OutcomeReconciled = false },
		func(i *types.KBTransferRecovery) { i.Reason = "short" },
		func(i *types.KBTransferRecovery) { i.TargetID = "other" },
	} {
		invalid := input
		mutate(&invalid)
		require.Error(t, policies.RecoverFailedTransfer(ctx, owner, invalid))
	}
	editor := types.Caller{TenantID: 7, UserID: "editor", Role: types.TenantRoleOwner}
	require.ErrorIs(t, policies.RecoverFailedTransfer(ctx, editor, input), access.ErrForbidden)
	machine := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true})
	require.ErrorIs(t, policies.RecoverFailedTransfer(machine, owner, input), access.ErrForbidden)
	require.NoError(t, policies.RecoverFailedTransfer(ctx, owner, input))
	var count int64
	require.NoError(t, db.Model(&types.KBTransferFence{}).Count(&count).Error)
	require.Zero(t, count)
	var audit types.AuditLog
	require.NoError(t, db.Where("action = ?", "kb.transfer_recovered").First(&audit).Error)
	require.Equal(t, "owner", audit.ActorUserID)
	_, err = policies.ReplaceManaged(ctx, owner, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted},
		allowKBAccessManager(owner))
	require.NoError(t, err)
}

func TestTransferRecoveryNeverClearsLiveOrCrashedAttempt(t *testing.T) {
	db, policies, owner := memberSharingFixture(t)
	require.NoError(t, db.Create(&types.KBTransferFence{
		ID: "live", TenantID: 7, SourceID: "kb", TargetID: "target", TaskID: "task", State: "active",
	}).Error)
	input := types.KBTransferRecovery{
		FenceID: "live", TaskID: "task", SourceID: "kb", TargetID: "target",
		WorkerStopped: true, OutcomeReconciled: true,
		Reason: "Operator assertion is not evidence of callback completion",
	}
	require.ErrorIs(t, policies.RecoverFailedTransfer(context.Background(), owner, input), ErrKBAccessTransferBusy)
	var count int64
	require.NoError(t, db.Model(&types.KBTransferFence{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestTransferRecoveryAuditFailureRetainsSafetyFence(t *testing.T) {
	db, policies, owner := memberSharingFixture(t)
	ctx := context.Background()
	require.NoError(t, db.Create(&types.KBTransferFence{
		ID: "failed", TenantID: 7, SourceID: "kb", TargetID: "target", TaskID: "task", State: "failed",
	}).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_recovery_audit BEFORE INSERT ON audit_logs
		BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`).Error)
	input := types.KBTransferRecovery{
		FenceID: "failed", TaskID: "task", SourceID: "kb", TargetID: "target",
		WorkerStopped: true, OutcomeReconciled: true, Reason: "Synthetic stopped worker with reconciled outcome",
	}
	require.Error(t, policies.RecoverFailedTransfer(ctx, owner, input))
	var fence types.KBTransferFence
	require.NoError(t, db.First(&fence, "id = ?", "failed").Error)
	require.Equal(t, "failed", fence.State)
}

func TestTransferRecoveryRefusesFailedAttemptWithLiveSibling(t *testing.T) {
	db, policies, owner := memberSharingFixture(t)
	for _, state := range []string{"active", "failed"} {
		require.NoError(t, db.Create(&types.KBTransferFence{
			ID: state, TenantID: 7, SourceID: "kb", TargetID: "target", TaskID: "task", State: state,
		}).Error)
	}
	input := types.KBTransferRecovery{
		FenceID: "failed", TaskID: "task", SourceID: "kb", TargetID: "target",
		WorkerStopped: true, OutcomeReconciled: true, Reason: "Operator cannot override a live sibling attempt",
	}
	require.ErrorIs(t, policies.RecoverFailedTransfer(context.Background(), owner, input), ErrKBAccessTransferBusy)
	var count int64
	require.NoError(t, db.Model(&types.KBTransferFence{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}
