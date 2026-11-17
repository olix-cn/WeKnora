package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TemplatesReady checks schema only, never creates it at runtime.
func (r *DocumentGenerationRepository) TemplatesReady(ctx context.Context) bool {
	return r.db.WithContext(ctx).Migrator().HasTable(&types.DocumentTemplateVersion{})
}

// CreateTemplateTask serializes reservation against disabling the published version.
// A task reserved before disable may finish; no task can be reserved after it.
func (r *DocumentGenerationRepository) CreateTemplateTask(
	ctx context.Context, row *types.DocumentGeneration,
) (bool, error) {
	var created bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var template types.DocumentTemplateVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ? AND status = ?", row.TemplateID, row.TenantID, "published").
			Take(&template).Error; err != nil {
			return err
		}
		var err error
		created, err = (&DocumentGenerationRepository{db: tx}).CreateTask(ctx, row)
		return err
	})
	return created, err
}

// CreateTemplate persists a private draft. A new version uses a fresh immutable ID.
func (r *DocumentGenerationRepository) CreateTemplate(ctx context.Context, row *types.DocumentTemplateVersion) error {
	return r.db.WithContext(ctx).Create(row).Error
}

// Template reads a tenant-local version; service code decides draft/published visibility.
func (r *DocumentGenerationRepository) Template(ctx context.Context, tenantID uint64, id string) (
	*types.DocumentTemplateVersion, error,
) {
	var row types.DocumentTemplateVersion
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).Take(&row).Error
	return &row, err
}

// Templates returns all published templates and the requesting manager's drafts.
func (r *DocumentGenerationRepository) Templates(ctx context.Context, caller types.Caller) (
	[]types.DocumentTemplateVersion, error,
) {
	q := r.db.WithContext(ctx).Where("tenant_id = ?", caller.TenantID)
	if caller.Role.HasPermission(types.TenantRoleAdmin) {
		q = q.Where("status = ? OR created_by = ?", "published", caller.UserID)
	} else {
		q = q.Where("status = ?", "published")
	}
	rows := []types.DocumentTemplateVersion{}
	err := q.Order("created_at DESC, id DESC").Limit(200).Find(&rows).Error
	return rows, err
}

// ChangeTemplate is an optimistic, owner-scoped state transition.
func (r *DocumentGenerationRepository) ChangeTemplate(ctx context.Context, row *types.DocumentTemplateVersion,
	fromStatus, fromHash string,
) (bool, error) {
	res := r.db.WithContext(ctx).Model(&types.DocumentTemplateVersion{}).
		Where("id = ? AND tenant_id = ? AND created_by = ? AND status = ? AND test_hash = ?",
			row.ID, row.TenantID, row.CreatedBy, fromStatus, fromHash).
		Where("updated_at = ?", row.UpdatedAt).
		Updates(map[string]any{
			"layout": row.Layout, "status": row.Status, "test_hash": row.TestHash, "updated_at": time.Now().UTC(),
		})
	return res.RowsAffected == 1, res.Error
}

// NextTemplateVersion obtains the next family number; the unique index resolves concurrent uploads.
func (r *DocumentGenerationRepository) NextTemplateVersion(
	ctx context.Context, tenantID uint64, family string,
) (int, error) {
	var maximum int
	err := r.db.WithContext(ctx).Model(&types.DocumentTemplateVersion{}).
		Where("tenant_id = ? AND family_id = ?", tenantID, family).
		Select("COALESCE(MAX(version), 0)").Scan(&maximum).Error
	return maximum + 1, err
}
