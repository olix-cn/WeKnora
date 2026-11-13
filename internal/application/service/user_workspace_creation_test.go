package service

import (
	"context"
	"errors"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type workspaceCreateRepo struct {
	*adminCreateUserRepo
	role  types.TenantRole
	actor string
	err   error
	calls int
}

func (r *workspaceCreateRepo) CreateUserWithMembership(_ context.Context, user *types.User, role types.TenantRole, actor string) error {
	r.calls++
	r.role, r.actor = role, actor
	if r.err != nil {
		return r.err
	}
	r.created = user
	return nil
}

type workspaceMemberService struct {
	interfaces.TenantMemberService
	member *types.TenantMember
}

func (s *workspaceMemberService) GetMembership(context.Context, string, uint64) (*types.TenantMember, error) {
	return s.member, nil
}

func TestAdminCreateUserInExistingWorkspace(t *testing.T) {
	for _, role := range []types.TenantRole{types.TenantRoleViewer, types.TenantRoleContributor, types.TenantRoleAdmin, types.TenantRoleOwner} {
		t.Run(string(role), func(t *testing.T) {
			repo := &workspaceCreateRepo{adminCreateUserRepo: &adminCreateUserRepo{}}
			svc := &userService{userRepo: repo}
			ctx := context.WithValue(context.Background(), types.UserIDContextKey, "system-admin")
			user, generated, err := svc.AdminCreateUser(ctx, &types.AdminCreateUserRequest{
				Username: "alice", DefaultTenantID: 42, Role: role,
			}, types.TenantProvisioningCreatePersonal)
			require.NoError(t, err)
			require.NotEmpty(t, generated)
			require.NoError(t, bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(generated)))
			require.Equal(t, uint64(42), user.TenantID)
			require.Equal(t, role, repo.role)
			require.Equal(t, "system-admin", repo.actor)
			require.False(t, user.IsSystemAdmin)
			require.False(t, user.CanAccessAllTenants)
		})
	}
}

func TestAdminWorkspaceCreationValidatesBeforeWritingAndReturnsNoPasswordOnFailure(t *testing.T) {
	repo := &workspaceCreateRepo{adminCreateUserRepo: &adminCreateUserRepo{}}
	svc := &userService{userRepo: repo}
	for _, req := range []*types.AdminCreateUserRequest{
		{Username: "alice", DefaultTenantID: 42},
		{Username: "alice", Role: types.TenantRoleViewer},
		{Username: "alice", DefaultTenantID: 42, Role: "system_admin"},
	} {
		_, password, err := svc.AdminCreateUser(context.Background(), req, types.TenantProvisioningTenantless)
		require.ErrorIs(t, err, ErrAdminUserWorkspace)
		require.Empty(t, password)
	}
	require.Zero(t, repo.calls)
	for _, err := range []error{apprepo.ErrProvisioningTenantUnavailable, errors.New("database failure")} {
		repo.err = err
		_, password, gotErr := svc.AdminCreateUser(context.Background(), &types.AdminCreateUserRequest{
			Username: "alice", DefaultTenantID: 42, Role: types.TenantRoleViewer,
		}, types.TenantProvisioningTenantless)
		require.Error(t, gotErr)
		require.Empty(t, password)
		require.Nil(t, repo.created)
	}
}

func TestAdminWorkspaceCreationRetryDoesNotChangeExistingMembership(t *testing.T) {
	existing := &types.User{ID: "alice", Username: "alice", TenantID: 42, IsActive: true}
	repo := &workspaceCreateRepo{adminCreateUserRepo: &adminCreateUserRepo{existingByUsername: existing}}
	members := &workspaceMemberService{member: &types.TenantMember{Role: types.TenantRoleViewer, Status: types.TenantMemberStatusActive}}
	svc := &userService{userRepo: repo, memberService: members}
	req := &types.AdminCreateUserRequest{Username: "alice", DefaultTenantID: 42, Role: types.TenantRoleViewer}
	user, password, err := svc.AdminCreateUser(context.Background(), req, types.TenantProvisioningTenantless)
	require.ErrorIs(t, err, ErrUserUsernameExists)
	require.Equal(t, existing, user)
	require.Empty(t, password)
	require.Zero(t, repo.calls)
	for _, role := range []types.TenantRole{types.TenantRoleAdmin, types.TenantRoleOwner} {
		req.Role = role
		_, _, err = svc.AdminCreateUser(context.Background(), req, types.TenantProvisioningTenantless)
		require.ErrorIs(t, err, ErrUserIdentityConflict)
	}
	req.Role, req.DefaultTenantID = types.TenantRoleViewer, 99
	_, _, err = svc.AdminCreateUser(context.Background(), req, types.TenantProvisioningTenantless)
	require.ErrorIs(t, err, ErrUserIdentityConflict)
	require.Zero(t, repo.calls)
}
