package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// CheckKnowledgeMemberAccess rechecks current policy when an Agent's previously
// captured search scope is reused. It does not expand that scope.
func (s *knowledgeService) CheckKnowledgeMemberAccess(
	ctx context.Context, kbID string, ownerTenantID uint64,
) error {
	lookup := access.MemberLookup(s.kbShareService)
	if lookup == nil {
		return nil
	}
	permission, handled, err := lookup.CheckKBMemberAccess(ctx, kbID, ownerTenantID, types.CallerFromContext(ctx))
	if err != nil {
		return err
	}
	if handled && (!permission.IsValid() || !permission.HasPermission(types.OrgRoleViewer)) {
		return access.ErrForbidden
	}
	return nil
}

// CheckChunkMemberAccess guards direct embed chunk reads that do not pass
// through the ordinary KB HTTP middleware.
func (s *chunkService) CheckChunkMemberAccess(ctx context.Context, kbID string, tenantID uint64) error {
	if lookup, ok := s.kbRepository.(access.KBMemberLookup); ok {
		permission, handled, err := lookup.CheckKBMemberAccess(ctx, kbID, tenantID, types.CallerFromContext(ctx))
		if err != nil {
			return err
		}
		if handled && (!permission.IsValid() || !permission.HasPermission(types.OrgRoleViewer)) {
			return access.ErrForbidden
		}
	}
	return nil
}
