package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// CheckResourceMemberAccess adds live KB policy to raw-file and message-file
// reads. A file bound to multiple restricted KBs must satisfy every such policy;
// deduplication or attaching it as a message artifact cannot declassify it.
func (r *resourceRepository) CheckResourceMemberAccess(
	ctx context.Context, tenantID uint64, resourceID, physicalPath string, anonymous bool,
) error {
	var policies []types.KnowledgeBaseAccessPolicy
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&policies).Error; err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}
	var kbIDs []string
	if resourceID != "" {
		bindings, err := r.GetMessageFileBindings(ctx, tenantID, resourceID, "")
		if err != nil {
			return err
		}
		kbIDs = bindings.KnowledgeBaseIDs
	}
	if len(kbIDs) == 0 {
		if err := r.db.WithContext(ctx).Model(&types.Knowledge{}).
			Where("tenant_id = ? AND file_path = ?", tenantID, physicalPath).
			Distinct("knowledge_base_id").Pluck("knowledge_base_id", &kbIDs).Error; err != nil {
			return err
		}
		// Legacy derived images and markdown attachments may have no catalog
		// binding or source-file column. Without authoritative ownership proof
		// they cannot use a tenant-wide path to bypass a restricted KB. The
		// KB-scoped route remains available with its explicit binding check.
		if len(kbIDs) == 0 && resourceID == "" {
			for _, policy := range policies {
				if policy.Mode != types.KBAccessInherit {
					return access.ErrForbidden
				}
			}
		}
	}
	bound := make(map[string]bool, len(kbIDs))
	for _, id := range kbIDs {
		bound[id] = true
	}
	caller := types.CallerFromContext(ctx)
	lookup := &knowledgeBaseRepository{db: r.db}
	for _, policy := range policies {
		if !bound[policy.KnowledgeBaseID] {
			continue
		}
		if !policy.Mode.IsValid() || policy.Version == 0 {
			return access.ErrForbidden
		}
		if anonymous && policy.Mode != types.KBAccessInherit {
			return access.ErrForbidden
		}
		if anonymous {
			continue
		}
		permission, handled, err := lookup.CheckKBMemberAccess(ctx, policy.KnowledgeBaseID, tenantID, caller)
		if err != nil {
			return err
		}
		if handled && (!permission.IsValid() || !permission.HasPermission(types.OrgRoleViewer)) {
			return access.ErrForbidden
		}
	}
	return nil
}
