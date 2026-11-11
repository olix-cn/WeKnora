package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

type loginAccountRepo struct {
	*stubUserRepoForAuth
}

func (r *loginAccountRepo) GetUserByUsername(_ context.Context, username string) (*types.User, error) {
	for _, user := range r.users {
		if user.Username == username {
			return user, nil
		}
	}
	return nil, nil
}

func TestLoginWithUsernameOrEmail(t *testing.T) {
	for _, tt := range []struct {
		name, account, password string
		inactive                bool
		want                    bool
	}{
		{"email", "alice@example.com", "Secure9!", false, true},
		{"username", "alice", "Secure9!", false, true},
		{"trim username", " alice ", "Secure9!", false, true},
		{"trim email", " alice@example.com ", "Secure9!", false, true},
		{"unknown username", "missing", "Secure9!", false, false},
		{"unknown email", "missing@example.com", "Secure9!", false, false},
		{"wrong password", "alice", "incorrect", false, false},
		{"empty account", "  ", "Secure9!", false, false},
		{"disabled account", "alice", "Secure9!", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tokenRepo := &stubAuthTokenRepo{tokens: map[string]*types.AuthToken{}}
			svc := newAuthTestUserService(tokenRepo)
			user := persistedTestUser(t, 0, "alice@example.com", "Secure9!")
			user.Username = "alice"
			user.IsActive = !tt.inactive
			svc.userRepo = &loginAccountRepo{&stubUserRepoForAuth{users: map[string]*types.User{user.ID: user}}}
			resp, err := svc.Login(context.Background(), &types.LoginRequest{Email: tt.account, Password: tt.password})
			require.NoError(t, err)
			require.Equal(t, tt.want, resp.Success)
			if tt.want {
				require.Equal(t, user.ID, resp.User.ID)
				require.NotEmpty(t, resp.Token)
				require.Equal(t, 2, tokenRepo.createCalls)
			} else {
				require.Empty(t, resp.Token)
				require.Zero(t, tokenRepo.createCalls)
				if !tt.inactive {
					require.Equal(t, "Invalid account or password", resp.Message)
				}
			}
		})
	}
}
