package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Service reads accept exact upstream grants. Otherwise cross-tenant reads
// require a user and organization permission resolved for the original caller.
func kbReadPermissions(ctx context.Context, shares access.KBShareLookup) *access.KBPermissions {
	members := access.MemberLookup(shares)
	if types.CallerFromContext(ctx).UserID == "" {
		shares = nil
	}
	return access.NewKBPermissionsWithMembers(ctx, shares, members)
}

// filterMemberKnowledgeBases runs before list metadata enrichment, so denied
// KBs never cause document counts, processing state or pin metadata to surface.
// Legacy foreign lists keep their existing shared-agent selection upstream.
func filterMemberKnowledgeBases(
	ctx context.Context, lookup access.KBMemberLookup, kbs []*types.KnowledgeBase,
) ([]*types.KnowledgeBase, error) {
	if lookup == nil {
		return kbs, nil
	}
	caller := types.CallerFromContext(ctx)
	visible := make([]*types.KnowledgeBase, 0, len(kbs))
	for _, kb := range kbs {
		if kb == nil {
			continue
		}
		permission, handled, err := lookup.CheckKBMemberAccess(ctx, kb.ID, kb.TenantID, caller)
		if err != nil {
			return nil, err
		}
		if !handled || (permission.IsValid() && permission.HasPermission(types.OrgRoleViewer)) {
			visible = append(visible, kb)
		}
	}
	return visible, nil
}

// kbWritableIDs returns the target KBs the caller may modify: those of its own
// workspace, and those shared to it with at least editor permission (capped by
// the caller's tenant role). A read grant — a viewer share or a shared agent's
// scope — never makes a KB writable.
//
// The caller must also be allowed to write KB content at all, as on the HTTP
// write routes: Contributor+ (a tenant Viewer, and the IM, embed and MCP
// endpoint principals that run as Viewer, stay read-only), or for a scoped API
// key the ingest capability, as in access.RequireKBWrite. roleEnforced mirrors
// the RBAC rollout switch, under which role checks only log.
func kbWritableIDs(
	ctx context.Context, shares access.KBShareLookup, targets types.SearchTargets, roleEnforced bool,
) []string {
	caller := types.CallerFromContext(ctx)
	if scope, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		if !scope.FullAccess && !scope.HasCapability(types.APIKeyCapabilityIngest) {
			return nil
		}
	} else if roleEnforced && !caller.Role.HasPermission(types.TenantRoleContributor) {
		return nil
	}
	members := access.MemberLookup(shares)
	if caller.UserID == "" {
		shares = nil
	}
	permissions := access.NewKBSharePermissions(ctx, shares, caller.TenantID, caller.Role)
	seen := make(map[string]bool, len(targets))
	var ids []string
	for _, target := range targets {
		if target == nil || target.KnowledgeBaseID == "" || seen[target.KnowledgeBaseID] {
			continue
		}
		seen[target.KnowledgeBaseID] = true
		if members != nil {
			permission, handled, err := members.CheckKBMemberAccess(
				ctx, target.KnowledgeBaseID, target.TenantID, caller)
			if err != nil || (handled && (!permission.IsValid() || !permission.HasPermission(types.OrgRoleEditor))) {
				continue
			}
			if handled {
				ids = append(ids, target.KnowledgeBaseID)
				continue
			}
		}
		writable := caller.TenantID != 0 && target.TenantID == caller.TenantID
		if !writable {
			writable, _ = permissions.Check(target.KnowledgeBaseID, types.OrgRoleEditor)
		}
		if writable {
			ids = append(ids, target.KnowledgeBaseID)
		}
	}
	return ids
}

func resolveKBReadTenant(ctx context.Context, kb *types.KnowledgeBase, shares access.KBShareLookup) (uint64, error) {
	if kb != nil {
		allowed, err := kbReadPermissions(ctx, shares).Check(kb.ID, kb.TenantID, types.OrgRoleViewer)
		if err == nil && allowed {
			return kb.TenantID, nil
		}
	}
	return 0, apperrors.NewForbiddenError("无权访问该知识库")
}

func requireKBWrite(ctx context.Context, kb *types.KnowledgeBase) (context.Context, error) {
	if err := access.RequireKBWrite(ctx, kb); err != nil {
		return ctx, apperrors.NewForbiddenError("无权修改该知识库")
	}
	return types.WithExecutionTenant(ctx, kb.TenantID), nil
}

func (s *knowledgeService) writableFAQKnowledgeBase(
	ctx context.Context,
	kbID string,
) (*types.KnowledgeBase, context.Context, error) {
	kb, err := s.validateFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, ctx, err
	}
	ctx, err = requireKBWrite(ctx, kb)
	if err == nil {
		ctx, err = withKBWriteTenantInfo(ctx, kb, s.tenantRepo)
	}
	return kb, ctx, err
}

// A shared KB mutation must also resolve models/index backends against its
// owner. The authenticated caller remains unchanged when TenantInfo changes.
func withKBWriteTenantInfo(
	ctx context.Context,
	kb *types.KnowledgeBase,
	tenants interfaces.TenantRepository,
) (context.Context, error) {
	if tenant, ok := types.TenantInfoFromContext(ctx); ok && tenant != nil && tenant.ID == kb.TenantID {
		return ctx, nil
	}
	if tenants == nil {
		return ctx, apperrors.NewServiceUnavailableError("无法获取知识库所属空间")
	}
	tenant, err := tenants.GetTenantByID(ctx, kb.TenantID)
	if err != nil {
		return ctx, err
	}
	if tenant == nil || tenant.ID != kb.TenantID {
		return ctx, apperrors.NewNotFoundError("知识库所属空间不存在")
	}
	return context.WithValue(ctx, types.TenantInfoContextKey, tenant), nil
}
