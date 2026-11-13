package service

import (
	"context"
	"errors"
	"strings"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var ErrAdminUserWorkspace = errors.New("select an active default workspace and a valid workspace role")

type adminUserProvisioningRepository interface {
	CreateUserWithMembership(context.Context, *types.User, types.TenantRole, string) error
}

func (s *userService) adminCreateInWorkspace(ctx context.Context, req *types.AdminCreateUserRequest, password string) (*types.User, error) {
	username, email := strings.TrimSpace(req.Username), strings.TrimSpace(req.Email)
	checkExisting := func(user *types.User) (*types.User, error) {
		if !adminCreateIdentityMatches(user, username, email) || user.TenantID != req.DefaultTenantID || !user.IsActive {
			return nil, ErrUserIdentityConflict
		}
		if s.memberService == nil {
			return nil, ErrUserIdentityConflict
		}
		membership, err := s.memberService.GetMembership(ctx, user.ID, req.DefaultTenantID)
		if err != nil {
			return nil, err
		}
		if membership == nil || membership.Status != types.TenantMemberStatusActive || membership.Role != req.Role {
			return nil, ErrUserIdentityConflict
		}
		return user, ErrUserUsernameExists
	}
	existing, err := s.userRepo.GetUserByUsername(ctx, username)
	if err != nil && !errors.Is(err, apprepo.ErrUserNotFound) {
		return nil, err
	}
	if existing != nil {
		return checkExisting(existing)
	}
	if email != "" {
		existing, err := s.userRepo.GetUserByEmail(ctx, email)
		if err != nil && !errors.Is(err, apprepo.ErrUserNotFound) {
			return nil, err
		}
		if existing != nil {
			return checkExisting(existing)
		}
	}
	repo, ok := s.userRepo.(adminUserProvisioningRepository)
	if !ok {
		return nil, errors.New("atomic user provisioning is unavailable")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &types.User{ID: uuid.NewString(), Username: username, Email: email, PasswordHash: string(hash),
		TenantID: req.DefaultTenantID, IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	actor, _ := types.UserIDFromContext(ctx)
	if err := repo.CreateUserWithMembership(ctx, user, req.Role, actor); err != nil {
		if errors.Is(err, apprepo.ErrProvisioningTenantUnavailable) {
			return nil, ErrAdminUserWorkspace
		}
		return nil, err
	}
	return user, nil
}
