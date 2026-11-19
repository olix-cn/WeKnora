package interfaces

import "context"

// ResourceMemberAccessGuard checks current KB membership independently of a
// file's workspace ownership. Anonymous capability URLs must never expose a
// resource bound to a restricted knowledge base.
type ResourceMemberAccessGuard interface {
	AuthorizeResourceMemberAccess(ctx context.Context, tenantID uint64, reference string, anonymous bool) error
}
