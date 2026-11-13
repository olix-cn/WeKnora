package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOptionalEmailUsersCanCoexistAndCannotBeLookedUpByEmptyEmail(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.User{}))
	repo := NewUserRepository(db)
	ctx := context.Background()
	for _, id := range []string{"one", "two"} {
		require.NoError(t, repo.CreateUser(ctx, &types.User{ID: id, Username: id, PasswordHash: "hash", IsActive: true}))
	}
	_, err = repo.GetUserByEmail(ctx, "")
	require.ErrorIs(t, err, ErrUserNotFound)
	_, err = repo.GetUserByEmail(ctx, "  ")
	require.ErrorIs(t, err, ErrUserNotFound)
	user, err := repo.GetUserByUsername(ctx, "two")
	require.NoError(t, err)
	require.Empty(t, user.Email)
	user.Email = "two@example.com"
	require.NoError(t, repo.UpdateUser(ctx, user))
	require.Error(t, repo.CreateUser(ctx, &types.User{ID: "duplicate", Username: "other", Email: user.Email, PasswordHash: "hash"}))
	user.Email = ""
	require.NoError(t, repo.UpdateUser(ctx, user))
}
