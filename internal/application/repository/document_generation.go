package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DocumentGenerationRepository scopes every read/write by authenticated ownership.
type DocumentGenerationRepository struct{ db *gorm.DB }

// NewDocumentGenerationRepository creates the document persistence adapter.
func NewDocumentGenerationRepository(db *gorm.DB) *DocumentGenerationRepository {
	return &DocumentGenerationRepository{db: db}
}

// Ready reports whether migrations have been applied, without creating tables.
func (r *DocumentGenerationRepository) Ready(ctx context.Context) bool {
	db := r.db.WithContext(ctx)
	return db.Migrator().HasTable(&types.DocumentDataset{}) && db.Migrator().HasTable(&types.DocumentGeneration{})
}

// SaveDataset writes an immutable private import.
func (r *DocumentGenerationRepository) SaveDataset(ctx context.Context, row *types.DocumentDataset) error {
	return r.db.WithContext(ctx).Create(row).Error
}

// SaveDatasetOnce retains the first immutable deterministic chat import.
func (r *DocumentGenerationRepository) SaveDatasetOnce(ctx context.Context, row *types.DocumentDataset) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
}

// Dataset loads only an owned dataset.
func (r *DocumentGenerationRepository) Dataset(ctx context.Context, caller types.Caller, id string) (
	*types.DocumentDataset, error,
) {
	var row types.DocumentDataset
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND user_id = ?",
		id, caller.TenantID, caller.UserID).Take(&row).Error
	return &row, err
}

// CreateTask reserves an idempotency key atomically. Different users can use the same key.
func (r *DocumentGenerationRepository) CreateTask(ctx context.Context, row *types.DocumentGeneration) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "user_id"}, {Name: "request_key"}}, DoNothing: true,
	}).Create(row)
	return res.RowsAffected == 1, res.Error
}

// Task loads an owned generation.
func (r *DocumentGenerationRepository) Task(ctx context.Context, caller types.Caller, id string) (
	*types.DocumentGeneration, error,
) {
	var row types.DocumentGeneration
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND user_id = ?",
		id, caller.TenantID, caller.UserID).Take(&row).Error
	return &row, err
}

// ByKey looks up a previously reserved request.
func (r *DocumentGenerationRepository) ByKey(ctx context.Context, caller types.Caller, key string) (
	*types.DocumentGeneration, error,
) {
	var row types.DocumentGeneration
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND request_key = ?",
		caller.TenantID, caller.UserID, key).Take(&row).Error
	return &row, err
}

// Finish only transitions a still-running task, never a deleted or completed one.
func (r *DocumentGenerationRepository) Finish(ctx context.Context, row *types.DocumentGeneration) error {
	res := r.db.WithContext(ctx).Model(&types.DocumentGeneration{}).
		Where("id = ? AND tenant_id = ? AND user_id = ? AND status = ?",
			row.ID, row.TenantID, row.UserID, "generating").Updates(map[string]any{
		"status": row.Status, "file_ref": row.FileRef, "file_size": row.FileSize,
		"content_hash": row.ContentHash, "error_code": row.ErrorCode, "updated_at": time.Now().UTC(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// List returns only the caller's tasks, newest first.
func (r *DocumentGenerationRepository) List(ctx context.Context, caller types.Caller, page, size int) (
	[]types.DocumentGeneration, int64, error,
) {
	var total int64
	q := r.db.WithContext(ctx).Model(&types.DocumentGeneration{}).
		Where("tenant_id = ? AND user_id = ?", caller.TenantID, caller.UserID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []types.DocumentGeneration{}
	err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

// DeleteFile tombstones an owned output once. The reference remains for cleanup/audit.
func (r *DocumentGenerationRepository) DeleteFile(ctx context.Context, caller types.Caller, id string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&types.DocumentGeneration{}).
		Where("id = ? AND tenant_id = ? AND user_id = ? AND status = ?",
			id, caller.TenantID, caller.UserID, "completed").
		Updates(map[string]any{"status": "file_deleted", "updated_at": time.Now().UTC()})
	return res.RowsAffected == 1, res.Error
}
