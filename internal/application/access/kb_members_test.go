package access

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWorkspaceKBPermission(t *testing.T) {
	for _, tt := range []struct {
		name       string
		mode       types.KBAccessMode
		role       types.TenantRole
		creator    bool
		grant      types.OrgMemberRole
		membership uint64
		want       types.OrgMemberRole
	}{
		{"legacy viewer", "", types.TenantRoleViewer, false, "", 1, types.OrgRoleViewer},
		{"legacy contributor", "", types.TenantRoleContributor, false, "", 1, types.OrgRoleViewer},
		{"legacy creator", "", types.TenantRoleContributor, true, "", 1, types.OrgRoleAdmin},
		{"demoted creator", "", types.TenantRoleViewer, true, "", 1, types.OrgRoleViewer},
		{
			"explicit editor", types.KBAccessInherit, types.TenantRoleContributor, false,
			types.OrgRoleEditor, 1, types.OrgRoleEditor,
		},
		{
			"explicit manager", types.KBAccessRestricted, types.TenantRoleContributor, false,
			types.OrgRoleAdmin, 1, types.OrgRoleAdmin,
		},
		{"restricted unlisted", types.KBAccessRestricted, types.TenantRoleContributor, false, "", 1, ""},
		{"restricted creator unlisted", types.KBAccessRestricted, types.TenantRoleContributor, true, "", 1, ""},
		{
			"restricted viewer", types.KBAccessRestricted, types.TenantRoleViewer, false,
			types.OrgRoleViewer, 1, types.OrgRoleViewer,
		},
		{
			"viewer write cap", types.KBAccessRestricted, types.TenantRoleViewer, false,
			types.OrgRoleAdmin, 1, types.OrgRoleViewer,
		},
		{"admin recovery", types.KBAccessRestricted, types.TenantRoleAdmin, false, "", 1, types.OrgRoleAdmin},
		{"owner recovery", types.KBAccessRestricted, types.TenantRoleOwner, false, "", 1, types.OrgRoleAdmin},
		{"rejoined member", types.KBAccessRestricted, types.TenantRoleContributor, false, types.OrgRoleAdmin, 2, ""},
		{"invalid grant", types.KBAccessRestricted, types.TenantRoleContributor, false, "owner", 1, ""},
		{"invalid mode", "unknown", types.TenantRoleAdmin, false, "", 1, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			caller := types.Caller{TenantID: 7, UserID: "user", Role: types.TenantRoleOwner}
			kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
			if tt.creator {
				kb.CreatorID = caller.UserID
			}
			member := &types.TenantMember{
				ID: tt.membership, TenantID: 7, UserID: "user", Role: tt.role, Status: types.TenantMemberStatusActive,
			}
			var policy *types.KnowledgeBaseAccessPolicy
			if tt.mode != "" {
				policy = &types.KnowledgeBaseAccessPolicy{KnowledgeBaseID: "kb", TenantID: 7, Mode: tt.mode, Version: 1}
			}
			grant := &types.KnowledgeBaseMemberGrant{
				KnowledgeBaseID: "kb", TenantID: 7, UserID: "user", MembershipID: 1, Permission: tt.grant,
			}
			permission, allowed := WorkspaceKBPermission(caller, kb, policy, member, grant)
			require.Equal(t, tt.want, permission)
			require.Equal(t, tt.want != "", allowed)
		})
	}
}

func TestWorkspaceKBPermissionRejectsUntrustedFacts(t *testing.T) {
	type mutation func(*types.Caller, *types.KnowledgeBase, *types.TenantMember, *types.KnowledgeBaseAccessPolicy)
	for _, mutate := range []mutation{
		func(c *types.Caller, _ *types.KnowledgeBase, _ *types.TenantMember, _ *types.KnowledgeBaseAccessPolicy) {
			c.UserID = ""
		},
		func(c *types.Caller, _ *types.KnowledgeBase, _ *types.TenantMember, _ *types.KnowledgeBaseAccessPolicy) {
			c.TenantID = 8
		},
		func(_ *types.Caller, k *types.KnowledgeBase, _ *types.TenantMember, _ *types.KnowledgeBaseAccessPolicy) {
			k.ID = ""
		},
		func(_ *types.Caller, _ *types.KnowledgeBase, m *types.TenantMember, _ *types.KnowledgeBaseAccessPolicy) {
			m.UserID = "other"
		},
		func(_ *types.Caller, _ *types.KnowledgeBase, m *types.TenantMember, _ *types.KnowledgeBaseAccessPolicy) {
			m.TenantID = 8
		},
		func(_ *types.Caller, _ *types.KnowledgeBase, m *types.TenantMember, _ *types.KnowledgeBaseAccessPolicy) {
			m.Status = types.TenantMemberStatusSuspended
		},
		func(_ *types.Caller, _ *types.KnowledgeBase, m *types.TenantMember, _ *types.KnowledgeBaseAccessPolicy) {
			m.DeletedAt = gorm.DeletedAt{Valid: true}
		},
		func(_ *types.Caller, _ *types.KnowledgeBase, m *types.TenantMember, _ *types.KnowledgeBaseAccessPolicy) {
			m.Role = "invalid"
		},
		func(_ *types.Caller, _ *types.KnowledgeBase, _ *types.TenantMember, p *types.KnowledgeBaseAccessPolicy) {
			p.TenantID = 8
		},
		func(_ *types.Caller, _ *types.KnowledgeBase, _ *types.TenantMember, p *types.KnowledgeBaseAccessPolicy) {
			p.KnowledgeBaseID = "other"
		},
		func(_ *types.Caller, _ *types.KnowledgeBase, _ *types.TenantMember, p *types.KnowledgeBaseAccessPolicy) {
			p.Version = 0
		},
	} {
		caller := types.Caller{TenantID: 7, UserID: "user", Role: types.TenantRoleOwner}
		kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
		member := &types.TenantMember{
			ID: 1, TenantID: 7, UserID: "user", Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive,
		}
		policy := &types.KnowledgeBaseAccessPolicy{
			KnowledgeBaseID: "kb", TenantID: 7, Mode: types.KBAccessRestricted, Version: 1,
		}
		mutate(&caller, kb, member, policy)
		_, allowed := WorkspaceKBPermission(caller, kb, policy, member, nil)
		require.False(t, allowed)
	}
	_, allowed := WorkspaceKBPermission(types.Caller{}, nil, nil, nil, nil)
	require.False(t, allowed)
}

func TestValidateKBAccessPolicy(t *testing.T) {
	require.NoError(t, ValidateKBAccessPolicy(types.KBAccessPolicyUpdate{Mode: types.KBAccessInherit}))
	require.NoError(t, ValidateKBAccessPolicy(types.KBAccessPolicyUpdate{Mode: types.KBAccessRestricted}))
	for _, input := range []types.KBAccessPolicyUpdate{
		{},
		{Mode: "unknown"},
		{Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{
			{UserID: "", Permission: types.OrgRoleViewer},
		}},
		{Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{
			{UserID: " user ", Permission: types.OrgRoleViewer},
		}},
		{Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{{UserID: "user", Permission: "owner"}}},
		{Mode: types.KBAccessRestricted, Members: []types.KBMemberGrantInput{
			{UserID: "user", Permission: types.OrgRoleViewer}, {UserID: "user", Permission: types.OrgRoleEditor},
		}},
		{Mode: types.KBAccessRestricted, Members: make([]types.KBMemberGrantInput, types.MaxKBMemberGrants+1)},
	} {
		require.ErrorIs(t, ValidateKBAccessPolicy(input), ErrInvalidKBAccessPolicy)
	}
}
