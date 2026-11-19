package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type revokedTranscriptSessions struct{ interfaces.SessionRepository }

func (*revokedTranscriptSessions) Get(context.Context, uint64, string, string) (*types.Session, error) {
	return &types.Session{ID: "session", TenantID: 7, UserID: "user"}, nil
}

func (*revokedTranscriptSessions) GetIMPlatform(context.Context, uint64, string) (string, error) {
	return "", nil
}

func (*revokedTranscriptSessions) CheckSessionKBMemberAccess(context.Context, string, types.Caller) error {
	return access.ErrForbidden
}

func TestRevokedTranscriptStopsAllHistoryReadsBeforeLoadingContent(t *testing.T) {
	s := &messageService{sessionRepo: &revokedTranscriptSessions{}}
	ctx := types.WithExecutionTenant(types.WithCaller(context.Background(), types.Caller{
		TenantID: 7, UserID: "user", Role: types.TenantRoleViewer,
	}), 7)
	// messageRepo is deliberately nil: no plaintext/history must be loaded
	// after the session member guard rejects this transcript.
	_, err := s.GetMessage(ctx, "session", "message")
	require.ErrorIs(t, err, access.ErrForbidden)
	_, err = s.GetMessagesBySession(ctx, "session", 1, 20)
	require.ErrorIs(t, err, access.ErrForbidden)
	_, err = s.GetRecentMessagesBySession(ctx, "session", 20)
	require.ErrorIs(t, err, access.ErrForbidden)
	_, err = s.GetMessagesBySessionBeforeTime(ctx, "session", time.Now(), 20)
	require.ErrorIs(t, err, access.ErrForbidden)
	_, err = s.GetRecentUserMessages(ctx, "session", 20)
	require.ErrorIs(t, err, access.ErrForbidden)
	_, err = (&sessionService{sessionRepo: s.sessionRepo}).GetOwnedSession(ctx, "session")
	require.ErrorIs(t, err, access.ErrForbidden, "continuation/resume must not reuse a revoked transcript")
}

func TestFollowUpCacheCannotBypassCurrentTranscriptPermission(t *testing.T) {
	s := &messageSuggestionService{messageService: &messageService{sessionRepo: &revokedTranscriptSessions{}}}
	ctx := types.WithExecutionTenant(types.WithCaller(context.Background(), types.Caller{
		TenantID: 7, UserID: "user", Role: types.TenantRoleViewer,
	}), 7)
	// Suggestion repository/model are nil: cached answers and regeneration
	// must both stop at the live transcript check before accessing either.
	_, err := s.GetFollowUps(ctx, "session", "message")
	require.ErrorIs(t, err, access.ErrForbidden)
	_, err = s.EnsureFollowUps(ctx, "session", "message", false)
	require.ErrorIs(t, err, access.ErrForbidden)
}
