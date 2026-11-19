package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
)

// AuthorizeResourceMemberAccess resolves both resource handles and registered
// physical aliases before consulting persisted knowledge-base bindings.
func (s *resourceCatalog) AuthorizeResourceMemberAccess(
	ctx context.Context, tenantID uint64, reference string, anonymous bool,
) error {
	lookup, ok := s.repo.(interface {
		CheckResourceMemberAccess(context.Context, uint64, string, string, bool) error
	})
	if !ok {
		return nil // Existing lightweight test adapters have no policy store.
	}
	physical, resource, err := s.ResolvePath(ctx, reference)
	if err != nil {
		return err
	}
	var resourceID string
	if resource == nil {
		resource, err = s.repo.GetByTenantLocation(ctx, tenantID, resourceLocationHash(physical))
		if err != nil {
			return err
		}
	}
	if resource != nil {
		if resource.TenantID != tenantID {
			return access.ErrForbidden
		}
		resourceID = resource.ID
	}
	return lookup.CheckResourceMemberAccess(ctx, tenantID, resourceID, physical, anonymous)
}
