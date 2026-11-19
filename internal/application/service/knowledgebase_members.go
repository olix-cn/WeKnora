package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

// KnowledgeBaseMemberPolicyStore is a narrow seam for management authorization
// tests. ReplaceManaged must serialize policy changes with share/scope writes.
type KnowledgeBaseMemberPolicyStore interface {
	Read(context.Context, types.Caller, string) (*repository.KBAccessState, error)
	ReplaceManaged(context.Context, types.Caller, string, types.KBAccessPolicyUpdate,
		func(*repository.KBAccessState) error) (*repository.KBAccessState, error)
}

// KnowledgeBaseMemberService owns member-policy management.
// Its inputs contain neither actor IDs nor workspace roles from request JSON.
type KnowledgeBaseMemberService struct {
	store KnowledgeBaseMemberPolicyStore
}

// ListTransferFences requires a current workspace administrator at the store.
func (s *KnowledgeBaseMemberService) ListTransferFences(
	ctx context.Context, limit int,
) ([]types.KBTransferStatus, error) {
	caller, err := kbPolicyCaller(ctx)
	if err != nil {
		return nil, err
	}
	store, ok := s.store.(interface {
		ListTransferFences(context.Context, types.Caller, int) ([]types.KBTransferStatus, error)
	})
	if !ok {
		return nil, access.ErrForbidden
	}
	return store.ListTransferFences(ctx, caller, limit)
}

// NewKnowledgeBaseMemberService constructs the internal policy service.
func NewKnowledgeBaseMemberService(store KnowledgeBaseMemberPolicyStore) *KnowledgeBaseMemberService {
	return &KnowledgeBaseMemberService{store: store}
}

func kbPolicyManager(caller types.Caller, state *repository.KBAccessState) error {
	if state == nil {
		return access.ErrForbidden
	}
	var grant *types.KnowledgeBaseMemberGrant
	for i := range state.Members {
		if state.Members[i].UserID == caller.UserID {
			grant = &state.Members[i]
			break
		}
	}
	permission, allowed := access.WorkspaceKBPermission(caller, state.KB, state.Policy, state.Actor, grant)
	if !allowed || permission != types.OrgRoleAdmin {
		return access.ErrForbidden
	}
	return nil
}

func kbPolicyView(state *repository.KBAccessState) *types.KBAccessPolicyView {
	view := &types.KBAccessPolicyView{Mode: types.KBAccessInherit, Members: []types.KBMemberGrantInput{}}
	if state.Policy != nil {
		view.Mode, view.Version = state.Policy.Mode, state.Policy.Version
		updatedAt := state.Policy.UpdatedAt
		view.UpdatedAt = &updatedAt
	}
	for _, grant := range state.Members {
		view.Members = append(view.Members, types.KBMemberGrantInput{
			UserID: grant.UserID, Permission: grant.Permission,
		})
	}
	return view
}

func kbPolicyCaller(ctx context.Context) (types.Caller, error) {
	caller := types.CallerFromContext(ctx)
	if caller.TenantID == 0 || caller.UserID == "" || types.IsSyntheticUserID(caller.UserID) {
		return caller, access.ErrForbidden
	}
	if _, key := types.TenantAPIKeyScopeFromContext(ctx); key {
		return caller, access.ErrForbidden
	}
	if principal, ok := types.PrincipalFromContext(ctx); ok && principal.Type != types.PrincipalWebUser {
		return caller, access.ErrForbidden
	}
	return caller, nil
}

// Get exposes the roster only to a manager determined from current persisted
// membership and policy. Caller.Role alone never grants management authority.
func (s *KnowledgeBaseMemberService) Get(ctx context.Context, kbID string) (*types.KBAccessPolicyView, error) {
	caller, err := kbPolicyCaller(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.store.Read(ctx, caller, kbID)
	if err != nil {
		return nil, err
	}
	if err := kbPolicyManager(caller, state); err != nil {
		return nil, err
	}
	return kbPolicyView(state), nil
}

// Replace uses the store's authorization callback inside its locked transaction
// rather than a Get/check/save sequence vulnerable to concurrent revocation.
func (s *KnowledgeBaseMemberService) Replace(
	ctx context.Context, kbID string, update types.KBAccessPolicyUpdate,
) (*types.KBAccessPolicyView, error) {
	caller, err := kbPolicyCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := access.ValidateKBAccessPolicy(update); err != nil {
		return nil, err
	}
	state, err := s.store.ReplaceManaged(ctx, caller, kbID, update, func(current *repository.KBAccessState) error {
		return kbPolicyManager(caller, current)
	})
	if err != nil {
		return nil, err
	}
	return kbPolicyView(state), nil
}

// MemberCandidates exposes only active users in the current KB's workspace and
// only to current KB managers. No platform-wide account directory is queried.
func (s *KnowledgeBaseMemberService) MemberCandidates(
	ctx context.Context, kbID, search string, page, size int,
) ([]types.KBAccessMemberCandidate, int64, error) {
	caller, err := kbPolicyCaller(ctx)
	if err != nil {
		return nil, 0, err
	}
	search = strings.TrimSpace(search)
	if page < 1 || page > 1000000 || size < 1 || size > 100 || len(search) > 200 {
		return nil, 0, access.ErrInvalidKBAccessPolicy
	}
	store, ok := s.store.(interface {
		MemberCandidates(context.Context, types.Caller, string, string, int, int,
			func(*repository.KBAccessState) error) ([]types.KBAccessMemberCandidate, int64, error)
	})
	if !ok {
		return nil, 0, access.ErrForbidden
	}
	return store.MemberCandidates(ctx, caller, kbID, search, page, size,
		func(state *repository.KBAccessState) error { return kbPolicyManager(caller, state) })
}

// RecoverFailedTransfer is intentionally not a generic "unlock" operation.
// The store checks current workspace-admin membership and refuses active fences.
func (s *KnowledgeBaseMemberService) RecoverFailedTransfer(ctx context.Context, input types.KBTransferRecovery) error {
	caller, err := kbPolicyCaller(ctx)
	if err != nil {
		return err
	}
	store, ok := s.store.(interface {
		RecoverFailedTransfer(context.Context, types.Caller, types.KBTransferRecovery) error
	})
	if !ok {
		return access.ErrForbidden
	}
	return store.RecoverFailedTransfer(ctx, caller, input)
}
