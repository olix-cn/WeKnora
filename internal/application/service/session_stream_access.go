package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// CheckSessionStreamAccess rechecks provenance/member access while an already
// authenticated and owner-scoped SSE connection remains open. It never creates
// a new session grant or changes the background task's execution identity.
func (s *sessionService) CheckSessionStreamAccess(ctx context.Context, sessionID string) error {
	if guard, ok := s.sessionRepo.(interface {
		CheckSessionKBMemberAccess(context.Context, string, types.Caller) error
	}); ok {
		return guard.CheckSessionKBMemberAccess(ctx, sessionID, types.CallerFromContext(ctx))
	}
	return nil
}

func (s *sessionService) registerTurnKnowledgeSources(ctx context.Context, req *types.QARequest,
	targets types.SearchTargets,
) error {
	if req == nil || req.Session == nil || len(targets) == 0 {
		return nil
	}
	if recorder, ok := s.sessionRepo.(interface {
		RegisterSessionKnowledgeSources(context.Context, string, types.SearchTargets) error
	}); ok {
		return recorder.RegisterSessionKnowledgeSources(ctx, req.Session.ID, targets)
	}
	return nil
}
