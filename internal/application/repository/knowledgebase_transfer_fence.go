package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RunKBTransferFenced closes the policy-check/write gap without holding a long
// SQL transaction across file/vector operations. Admission and policy changes
// share the workspace row lock. Each attempt has an independent fence; a
// sibling success cannot remove a crashed or concurrently running attempt.
func (r *knowledgeBaseRepository) RunKBTransferFenced(
	ctx context.Context, tenantID uint64, sourceID, targetID, taskID string, run func() error,
) error {
	if sourceID == "" || targetID == "" || sourceID == targetID || taskID == "" || len(taskID) > 128 || run == nil {
		return errors.New("invalid knowledge base transfer fence")
	}
	fence := types.KBTransferFence{
		ID: uuid.NewString(), TenantID: tenantID, SourceID: sourceID, TargetID: targetID,
		TaskID: taskID, CreatedAt: time.Now().UTC(),
	}
	if err := withKBSharingLock(ctx, r.db, tenantID, func(tx *gorm.DB) error {
		var kb types.KnowledgeBase
		if err := tx.Where("id = ? AND tenant_id = ?", sourceID, tenantID).First(&kb).Error; err != nil {
			return err
		}
		if err := requirePublicKBPolicy(tx, sourceID, tenantID); err != nil {
			return err
		}
		return tx.Create(&fence).Error
	}); err != nil {
		return err
	}
	if err := run(); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		// Mark only after the execution callback has returned. Panic/process
		// death stays active and is never eligible for online recovery.
		if markErr := r.db.WithContext(cleanup).Model(&types.KBTransferFence{}).
			Where("id = ? AND tenant_id = ?", fence.ID, tenantID).Update("state", "failed").Error; markErr != nil {
			return errors.Join(err, markErr)
		}
		return err // Preserve fence: external partial writes may have committed.
	}
	// Cancellation after successful writes must not turn a release into a silent
	// success. A failed release keeps a safe, visible recovery blocker.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return r.db.WithContext(cleanup).Where("id = ? AND tenant_id = ?", fence.ID, tenantID).
		Delete(&types.KBTransferFence{}).Error
}
