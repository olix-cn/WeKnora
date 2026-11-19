package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type memberFilterLookup struct{ fail bool }

func (s *memberFilterLookup) CheckKBMemberAccess(
	_ context.Context, id string, _ uint64, _ types.Caller,
) (types.OrgMemberRole, bool, error) {
	if s.fail {
		return "", false, errors.New("synthetic policy lookup failure")
	}
	switch id {
	case "denied":
		return "", true, nil
	case "reader":
		return types.OrgRoleViewer, true, nil
	case "editor":
		return types.OrgRoleEditor, true, nil
	}
	return "", false, nil
}

func (*memberFilterLookup) CheckTenantKBPermission(
	context.Context, string, uint64, types.TenantRole,
) (types.OrgMemberRole, bool, error) {
	return types.OrgRoleAdmin, true, nil
}

func TestMemberListFilterOmitsDeniedBeforeEnrichment(t *testing.T) {
	lookup := &memberFilterLookup{}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "member"})
	kbs := []*types.KnowledgeBase{
		{ID: "legacy", TenantID: 7},
		{ID: "denied", TenantID: 7},
		nil,
		{ID: "reader", TenantID: 7},
		{ID: "editor", TenantID: 7},
	}
	visible, err := filterMemberKnowledgeBases(ctx, lookup, kbs)
	require.NoError(t, err)
	require.Len(t, visible, 3)
	require.Equal(t, "legacy", visible[0].ID)
	require.Equal(t, "reader", visible[1].ID)
	require.Equal(t, "editor", visible[2].ID)
	require.Len(t, kbs, 5, "filtering must not mutate the original list")
	lookup.fail = true
	visible, err = filterMemberKnowledgeBases(ctx, lookup, kbs)
	require.Error(t, err)
	require.Nil(t, visible)
}

func TestReadPermissionsKeepMemberChecksWithoutHumanShareExpansion(t *testing.T) {
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7})
	permissions := kbReadPermissions(ctx, &memberFilterLookup{})
	allowed, err := permissions.Check("denied", 7, types.OrgRoleViewer)
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = permissions.Check("legacy", 8, types.OrgRoleViewer)
	require.NoError(t, err)
	require.False(t, allowed, "no human identity must not expand organization shares")
	require.NotNil(t, access.MemberLookup(&memberFilterLookup{}))
}

type memberFilterKBRepository struct {
	interfaces.KnowledgeBaseRepository
	memberFilterLookup
}

func (*memberFilterKBRepository) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "denied", TenantID: 7}, nil
}

func TestRestrictedKnowledgeBaseCannotBeSharedEvenByWorkspaceOwner(t *testing.T) {
	s := &kbShareService{kbRepo: &memberFilterKBRepository{}}
	ctx := types.WithCaller(context.Background(), types.Caller{
		TenantID: 7, UserID: "owner", Role: types.TenantRoleOwner,
	})
	_, err := s.ShareKnowledgeBase(ctx, "denied", "organization", "owner", 7, types.OrgRoleViewer)
	require.ErrorIs(t, err, ErrSharePermissionDenied)
}

func TestKnowledgeMemberAccessRechecksLivePolicy(t *testing.T) {
	lookup := &memberFilterLookup{}
	s := &knowledgeService{kbShareService: &fakeMemberFilterShareService{memberFilterLookup: lookup}}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "member"})
	require.ErrorIs(t, s.CheckKnowledgeMemberAccess(ctx, "denied", 7), access.ErrForbidden)
	require.NoError(t, s.CheckKnowledgeMemberAccess(ctx, "reader", 7))
	lookup.fail = true
	require.Error(t, s.CheckKnowledgeMemberAccess(ctx, "reader", 7))
}

type fakeMemberFilterShareService struct {
	interfaces.KBShareService
	*memberFilterLookup
}

func (s *fakeMemberFilterShareService) CheckTenantKBPermission(
	ctx context.Context, kbID string, tenantID uint64, role types.TenantRole,
) (types.OrgMemberRole, bool, error) {
	return s.memberFilterLookup.CheckTenantKBPermission(ctx, kbID, tenantID, role)
}
