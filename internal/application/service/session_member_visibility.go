package service

import "context"

func deniedSessionIDs(ctx context.Context, repo any, tenantID uint64, ownerID string) ([]string, error) {
	if guard, ok := repo.(interface {
		DeniedSessionIDs(context.Context, uint64, string) ([]string, error)
	}); ok {
		return guard.DeniedSessionIDs(ctx, tenantID, ownerID)
	}
	return nil, nil
}
