package types

import (
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"
)

func TestLoginRequestAcceptsUsernameAndEmail(t *testing.T) {
	v := validator.New()
	v.SetTagName("binding")
	for _, account := range []string{"alice", "alice@example.com", "用户名"} {
		require.NoError(t, v.Struct(LoginRequest{Email: account, Password: "Secure9!"}))
	}
	require.Error(t, v.Struct(LoginRequest{Password: "Secure9!"}))
	require.Error(t, v.Struct(LoginRequest{Email: "alice"}))
}
