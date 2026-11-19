package access

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type memberPolicyLookup struct {
	permission types.OrgMemberRole
	handled    bool
	err        error
	calls      int
	shareCalls int
}

func (s *memberPolicyLookup) CheckKBMemberAccess(
	_ context.Context, _ string, _ uint64, _ types.Caller,
) (types.OrgMemberRole, bool, error) {
	s.calls++
	return s.permission, s.handled, s.err
}

func (s *memberPolicyLookup) CheckTenantKBPermission(
	_ context.Context, _ string, _ uint64, _ types.TenantRole,
) (types.OrgMemberRole, bool, error) {
	s.shareCalls++
	return types.OrgRoleAdmin, true, nil
}

func TestResolveKBMemberPolicyPrecedesOwnershipAndShareFallback(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		for _, required := range []types.OrgMemberRole{types.OrgRoleViewer, types.OrgRoleEditor, types.OrgRoleAdmin} {
			caller := types.Caller{TenantID: 7, UserID: "user", Role: types.TenantRoleAdmin}
			ctx := types.WithCaller(context.Background(), caller)
			kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
			if foreign {
				kb.TenantID = 8
			}
			lookup := &memberPolicyLookup{handled: true}
			_, err := ResolveKB(ctx, KBRequest{Caller: caller}, kb, required, lookup, nil)
			require.ErrorIs(t, err, ErrForbidden)
			require.Zero(t, lookup.shareCalls, "a member-policy denial cannot fall back to organization sharing")
			lookup.permission = types.OrgRoleViewer
			grant, err := ResolveKB(ctx, KBRequest{Caller: caller}, kb, required, lookup, nil)
			if required == types.OrgRoleViewer {
				require.NoError(t, err)
				require.Equal(t, types.OrgRoleViewer, grant.Permission)
			} else {
				require.ErrorIs(t, err, ErrForbidden)
			}
		}
	}
}

func TestKBPermissionsMemberDenialOverridesCapturedAgentAndReadGrants(t *testing.T) {
	caller := types.Caller{TenantID: 7, UserID: "user", Role: types.TenantRoleContributor}
	base := types.WithCaller(context.Background(), caller)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
	grant, err := ResolveKB(base, KBRequest{Caller: caller}, kb, types.OrgRoleViewer, nil, nil)
	require.NoError(t, err)
	lookup := &memberPolicyLookup{handled: true}
	permissions := NewKBPermissions(grant.Context(base), lookup)
	for range 3 {
		allowed, err := permissions.Check("kb", 7, types.OrgRoleViewer)
		require.NoError(t, err)
		require.False(t, allowed)
	}
	require.Equal(t, 1, lookup.calls, "one decision per KB/owner in one operation")
	lookup.permission = types.OrgRoleViewer
	allowed, err := NewKBPermissions(base, lookup).Check("kb", 7, types.OrgRoleViewer)
	require.NoError(t, err)
	require.True(t, allowed, "a new operation sees the updated policy")
	lookup.permission = ""
	ctx := WithSharedAgent(base, &types.CustomAgent{
		ID: "agent", TenantID: 8,
		Config: types.CustomAgentConfig{KBSelectionMode: "selected", KnowledgeBases: []string{"kb"}},
	})
	allowed, err = NewKBPermissions(ctx, lookup).Check("kb", 8, types.OrgRoleViewer)
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestMemberPolicyErrorsNeverGrantLegacyAccess(t *testing.T) {
	caller := types.Caller{TenantID: 7, UserID: "user", Role: types.TenantRoleOwner}
	ctx := types.WithCaller(context.Background(), caller)
	lookup := &memberPolicyLookup{err: errors.New("synthetic database failure")}
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
	_, err := ResolveKB(ctx, KBRequest{Caller: caller}, kb, types.OrgRoleViewer, lookup, nil)
	require.ErrorIs(t, err, lookup.err)
	allowed, err := NewKBPermissions(ctx, lookup).Check("kb", 7, types.OrgRoleViewer)
	require.ErrorIs(t, err, lookup.err)
	require.False(t, allowed)
	lookup.err, lookup.handled = nil, true
	require.Error(t, AuthorizeKBAccess(ctx, lookup, []*types.KnowledgeBase{kb}))
}

func TestOnlyExactTrustedTaskGrantCanContinueAdmittedRead(t *testing.T) {
	caller := types.Caller{TenantID: 7, UserID: "user", Role: types.TenantRoleContributor}
	base := types.WithCaller(context.Background(), caller)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
	ctx, err := WithKBTaskWrite(base, kb, 7)
	require.NoError(t, err)
	lookup := &memberPolicyLookup{handled: true}
	allowed, err := NewKBPermissions(ctx, lookup).Check("kb", 7, types.OrgRoleViewer)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Zero(t, lookup.calls)
	allowed, err = NewKBPermissions(ctx, lookup).Check("other", 7, types.OrgRoleViewer)
	require.NoError(t, err)
	require.False(t, allowed)
	require.False(t, HasKBTaskGrant(ctx, "kb", 7, types.OrgRoleAdmin))
	changed := types.WithCaller(ctx, types.Caller{TenantID: 7, UserID: "another"})
	require.False(t, HasKBTaskGrant(changed, "kb", 7, types.OrgRoleViewer))
	require.False(t, HasKBTaskGrant(base, "kb", 7, types.OrgRoleViewer))
}
