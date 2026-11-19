package access

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBMemberLookup resolves persisted workspace-member policy. Handled is false
// only when legacy authorization still applies (no policy, or a public foreign
// KB). A handled denial must never fall back to a share or shared-agent grant.
type KBMemberLookup interface {
	CheckKBMemberAccess(context.Context, string, uint64, types.Caller) (types.OrgMemberRole, bool, error)
}

// MemberLookup retains the optional member-policy surface on an access adapter.
func MemberLookup(lookup KBShareLookup) KBMemberLookup {
	members, _ := lookup.(KBMemberLookup)
	return members
}

func checkKBMemberAccess(
	ctx context.Context, lookup KBMemberLookup, kbID string, tenantID uint64, caller types.Caller,
) (types.OrgMemberRole, bool, error) {
	if lookup == nil {
		return "", false, nil
	}
	if captured, ok := ctx.Value(types.CallerContextKey).(types.Caller); ok && captured != caller.Normalize() {
		return "", true, ErrForbidden
	}
	return lookup.CheckKBMemberAccess(ctx, kbID, tenantID, caller)
}

// ErrInvalidKBAccessPolicy denotes malformed replacement input.
var ErrInvalidKBAccessPolicy = errors.New("invalid knowledge base access policy")

// ValidateKBAccessPolicy validates replacement input without trusting member IDs
// or roles supplied by a browser. Active membership is checked by persistence.
func ValidateKBAccessPolicy(update types.KBAccessPolicyUpdate) error {
	if !update.Mode.IsValid() || len(update.Members) > types.MaxKBMemberGrants {
		return ErrInvalidKBAccessPolicy
	}
	seen := make(map[string]bool, len(update.Members))
	for _, member := range update.Members {
		if member.UserID == "" || len(member.UserID) > 36 || strings.TrimSpace(member.UserID) != member.UserID ||
			!member.Permission.IsValid() || seen[member.UserID] {
			return ErrInvalidKBAccessPolicy
		}
		seen[member.UserID] = true
	}
	return nil
}

// WorkspaceKBPermission evaluates server-loaded facts for a human member.
// It does not authorize machine principals, foreign tenants, org shares, or
// trusted worker continuation. Invalid policy state fails closed, even for an
// administrator. Callers must not replace a missing membership with caller.Role.
func WorkspaceKBPermission(
	caller types.Caller,
	kb *types.KnowledgeBase,
	policy *types.KnowledgeBaseAccessPolicy,
	member *types.TenantMember,
	grant *types.KnowledgeBaseMemberGrant,
) (types.OrgMemberRole, bool) {
	if kb == nil || kb.ID == "" || kb.TenantID == 0 || caller.UserID == "" || caller.TenantID != kb.TenantID ||
		member == nil || member.ID == 0 || member.UserID != caller.UserID || member.TenantID != kb.TenantID ||
		member.DeletedAt.Valid || member.Status != types.TenantMemberStatusActive || !member.Role.IsValid() {
		return "", false
	}
	mode := types.KBAccessInherit
	if policy != nil {
		if policy.KnowledgeBaseID != kb.ID || policy.TenantID != kb.TenantID ||
			!policy.Mode.IsValid() || policy.Version == 0 {
			return "", false
		}
		mode = policy.Mode
	}
	if member.Role.HasPermission(types.TenantRoleAdmin) {
		return types.OrgRoleAdmin, true
	}
	var permission types.OrgMemberRole
	if mode == types.KBAccessInherit {
		permission = types.OrgRoleViewer
		if kb.CreatorID == caller.UserID && member.Role.HasPermission(types.TenantRoleContributor) {
			permission = types.OrgRoleAdmin
		}
	}
	if policy != nil && grant != nil && grant.KnowledgeBaseID == kb.ID && grant.TenantID == kb.TenantID &&
		grant.UserID == caller.UserID && grant.MembershipID == member.ID && grant.Permission.IsValid() &&
		(permission == "" || grant.Permission.HasPermission(permission)) {
		permission = grant.Permission
	}
	if member.Role == types.TenantRoleViewer && permission != "" {
		permission = types.OrgRoleViewer
	}
	return permission, permission.IsValid()
}
