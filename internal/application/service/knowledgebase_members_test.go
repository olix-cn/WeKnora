package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type memberPolicyStore struct {
	state      *repository.KBAccessState
	beforeSave func()
	reads      int
	writes     int
}

func (s *memberPolicyStore) Read(context.Context, types.Caller, string) (*repository.KBAccessState, error) {
	s.reads++
	return s.state, nil
}

func (s *memberPolicyStore) ReplaceManaged(
	_ context.Context, _ types.Caller, _ string, update types.KBAccessPolicyUpdate,
	authorize func(*repository.KBAccessState) error,
) (*repository.KBAccessState, error) {
	if s.beforeSave != nil {
		s.beforeSave()
	}
	if err := authorize(s.state); err != nil {
		return nil, err
	}
	s.writes++
	s.state.Policy = &types.KnowledgeBaseAccessPolicy{
		KnowledgeBaseID: "kb", TenantID: 7, Mode: update.Mode, Version: 1,
	}
	return s.state, nil
}

func memberManagementState() *repository.KBAccessState {
	return &repository.KBAccessState{
		KB: &types.KnowledgeBase{ID: "kb", TenantID: 7, CreatorID: "member"},
		Actor: &types.TenantMember{
			ID: 1, TenantID: 7, UserID: "member", Role: types.TenantRoleContributor,
			Status: types.TenantMemberStatusActive,
		},
	}
}

func TestKBMemberManagementRejectsMachineAndAnonymousPrincipalsBeforeRead(t *testing.T) {
	store := &memberPolicyStore{state: memberManagementState()}
	svc := NewKnowledgeBaseMemberService(store)
	human := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "member"})
	for _, ctx := range []context.Context{
		context.Background(), types.WithCaller(context.Background(), types.Caller{TenantID: 7}),
		types.WithTenantAPIKeyScope(human, types.TenantAPIKeyScope{FullAccess: true}),
		types.WithPrincipal(human, types.Principal{Type: types.PrincipalIMUser, ID: "member"}),
		types.WithPrincipal(human, types.Principal{Type: types.PrincipalEmbedSession, ID: "member"}),
	} {
		_, err := svc.Get(ctx, "kb")
		require.ErrorIs(t, err, access.ErrForbidden)
		_, err = svc.Replace(ctx, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessInherit})
		require.ErrorIs(t, err, access.ErrForbidden)
	}
	require.Zero(t, store.reads)
	require.Zero(t, store.writes)
}

func TestKBMemberManagementReauthorizesAtSaveAndHidesInternalModels(t *testing.T) {
	store := &memberPolicyStore{state: memberManagementState()}
	svc := NewKnowledgeBaseMemberService(store)
	ctx := types.WithCaller(context.Background(), types.Caller{
		TenantID: 7, UserID: "member", Role: types.TenantRoleOwner,
	})
	view, err := svc.Get(ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, types.KBAccessInherit, view.Mode)
	require.Zero(t, view.Version)
	store.beforeSave = func() { store.state.Actor.Role = types.TenantRoleViewer }
	_, err = svc.Replace(ctx, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted})
	require.ErrorIs(t, err, access.ErrForbidden)
	require.Zero(t, store.writes, "a stale caller owner role must not override current membership")
	store.beforeSave = nil
	store.state.Actor.Role = types.TenantRoleContributor
	view, err = svc.Replace(ctx, "kb", types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted})
	require.NoError(t, err)
	require.Equal(t, types.KBAccessRestricted, view.Mode)
}

func TestKBMemberManagementDelegatedEditorCannotManageRoster(t *testing.T) {
	state := memberManagementState()
	state.KB.CreatorID = "someone-else"
	state.Policy = &types.KnowledgeBaseAccessPolicy{
		KnowledgeBaseID: "kb", TenantID: 7, Mode: types.KBAccessRestricted, Version: 1,
	}
	state.Members = []types.KnowledgeBaseMemberGrant{{
		KnowledgeBaseID: "kb", TenantID: 7, UserID: "member", MembershipID: 1, Permission: types.OrgRoleEditor,
	}}
	store := &memberPolicyStore{state: state}
	svc := NewKnowledgeBaseMemberService(store)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "member"})
	_, err := svc.Get(ctx, "kb")
	require.ErrorIs(t, err, access.ErrForbidden)
	state.Members[0].Permission = types.OrgRoleAdmin
	_, err = svc.Get(ctx, "kb")
	require.NoError(t, err)
	state.Actor.Role = types.TenantRoleViewer
	_, err = svc.Get(ctx, "kb")
	require.ErrorIs(t, err, access.ErrForbidden)
}
