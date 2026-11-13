package types

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRegistrationEmailIsOptionalButValidatedWhenProvided(t *testing.T) {
	v := validator.New()
	v.SetTagName("binding")
	for _, email := range []string{"", "alice@example.com"} {
		require.NoError(t, v.Struct(RegisterRequest{Username: "alice", Email: email, Password: "Secure9!"}))
		require.NoError(t, v.Struct(AdminCreateUserRequest{Username: "alice", Email: email}))
	}
	require.Error(t, v.Struct(RegisterRequest{Username: "alice", Email: "invalid", Password: "Secure9!"}))
	require.Error(t, v.Struct(AdminCreateUserRequest{Username: "alice", Email: "invalid"}))
}
