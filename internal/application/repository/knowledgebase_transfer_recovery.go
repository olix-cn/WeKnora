package repository

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ListTransferFences is an administrator-only, bounded status view. It does not
// expose queue payloads, documents or an unsafe automatic unlock operation.
func (r *KnowledgeBaseAccessRepository) ListTransferFences(
	ctx context.Context, caller types.Caller, limit int,
) ([]types.KBTransferStatus, error) {
	if limit < 1 || limit > 100 || caller.UserID == "" || types.IsSyntheticUserID(caller.UserID) {
		return nil, access.ErrForbidden
	}
	if _, machine := types.TenantAPIKeyScopeFromContext(ctx); machine {
		return nil, access.ErrForbidden
	}
	if p, ok := types.PrincipalFromContext(ctx); ok && p.Type != types.PrincipalWebUser {
		return nil, access.ErrForbidden
	}
	var rows []types.KBTransferStatus
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var member types.TenantMember
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
			"tenant_id = ? AND user_id = ? AND status = ?", caller.TenantID, caller.UserID,
			types.TenantMemberStatusActive).Take(&member).Error; err != nil {
			return access.ErrForbidden
		}
		if !member.Role.HasPermission(types.TenantRoleAdmin) {
			return access.ErrForbidden
		}
		var user types.User
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where(
			"id = ? AND is_active = ?", caller.UserID, true).Take(&user).Error; err != nil {
			return access.ErrForbidden
		}
		return tx.Model(&types.KBTransferFence{}).
			Select("id AS fence_id, task_id, source_id, target_id, state, created_at").
			Where("tenant_id = ?", caller.TenantID).Order("created_at ASC, id ASC").Limit(limit).Scan(&rows).Error
	})
	if rows == nil {
		rows = []types.KBTransferStatus{}
	}
	return rows, err
}

// RecoverFailedTransfer removes only a failed, stopped and reconciled attempt,
// for a CURRENT workspace administrator. Delegated KB management is insufficient.
// Crashed/active attempts require offline recovery after workers are stopped;
// there is deliberately no "force" flag and no age-based unlock.
func (r *KnowledgeBaseAccessRepository) RecoverFailedTransfer(
	ctx context.Context, caller types.Caller, input types.KBTransferRecovery,
) error {
	if caller.UserID == "" || types.IsSyntheticUserID(caller.UserID) ||
		!input.WorkerStopped || !input.OutcomeReconciled ||
		input.FenceID == "" || input.TaskID == "" || input.SourceID == "" || input.TargetID == "" ||
		len(strings.TrimSpace(input.Reason)) < 8 || len(input.Reason) > 500 {
		return access.ErrForbidden
	}
	if _, machine := types.TenantAPIKeyScopeFromContext(ctx); machine {
		return access.ErrForbidden
	}
	if principal, ok := types.PrincipalFromContext(ctx); ok && principal.Type != types.PrincipalWebUser {
		return access.ErrForbidden
	}
	return withKBSharingLock(ctx, r.db, caller.TenantID, func(tx *gorm.DB) error {
		var member types.TenantMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND user_id = ? AND status = ?", caller.TenantID, caller.UserID,
				types.TenantMemberStatusActive).Take(&member).Error; err != nil {
			return access.ErrForbidden
		}
		if !member.Role.HasPermission(types.TenantRoleAdmin) {
			return access.ErrForbidden
		}
		var user types.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
			Where("id = ? AND is_active = ?", caller.UserID, true).Take(&user).Error; err != nil {
			return access.ErrForbidden
		}
		var fence types.KBTransferFence
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id = ? AND tenant_id = ? AND task_id = ? AND source_id = ? AND target_id = ?",
			input.FenceID, caller.TenantID, input.TaskID, input.SourceID, input.TargetID).
			Take(&fence).Error; err != nil {
			return err
		}
		if fence.State != "failed" {
			return ErrKBAccessTransferBusy
		}
		var active int64
		if err := tx.Model(&types.KBTransferFence{}).
			Where("tenant_id = ? AND task_id = ? AND state <> 'failed'", caller.TenantID, input.TaskID).
			Count(&active).Error; err != nil {
			return err
		}
		if active != 0 {
			return ErrKBAccessTransferBusy
		}
		details, err := json.Marshal(map[string]any{
			"fence_id": fence.ID, "task_id": fence.TaskID, "source_id": fence.SourceID, "target_id": fence.TargetID,
			"worker_stopped": true, "outcome_reconciled": true, "reason": strings.TrimSpace(input.Reason),
		})
		if err != nil {
			return err
		}
		if err := tx.Create(&types.AuditLog{
			TenantID: caller.TenantID, ActorUserID: caller.UserID, ActorRole: string(member.Role),
			Action: types.AuditActionKBTransferRecovered, ScopeType: "knowledge_base", ScopeID: fence.SourceID,
			TargetType: "knowledge_base_transfer", TargetID: fence.ID, Outcome: types.AuditOutcomeSuccess,
			Details: types.JSON(details), CreatedAt: time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ? AND state = ?", fence.ID, caller.TenantID, "failed").
			Delete(&types.KBTransferFence{}).Error
	})
}
