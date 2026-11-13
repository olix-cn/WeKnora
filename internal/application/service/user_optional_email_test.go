package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRegisterAndAdminCreateWithoutEmail(t *testing.T) {
	repo := &adminCreateUserRepo{existingByEmail: &types.User{ID: "empty-email-user"}}
	svc := newAdminCreateUserService(repo)
	for _, name := range []string{"first", "second"} {
		user, err := svc.Register(context.Background(), &types.RegisterRequest{
			Username: name, Password: "Secure9!", TenantProvisioning: types.TenantProvisioningTenantless,
		})
		require.NoError(t, err)
		require.Empty(t, user.Email)
		require.Equal(t, name, user.Username)
	}
	user, _, err := svc.AdminCreateUser(context.Background(), &types.AdminCreateUserRequest{
		Username: "admin-created", Password: new("Secure9!"),
	}, types.TenantProvisioningTenantless)
	require.NoError(t, err)
	require.Empty(t, user.Email)
	_, err = svc.Register(context.Background(), &types.RegisterRequest{
		Username: "third", Email: "duplicate@example.com", Password: "Secure9!", TenantProvisioning: types.TenantProvisioningTenantless,
	})
	require.ErrorIs(t, err, ErrUserEmailExists)
}
