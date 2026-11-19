package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type revokedStreamSessions struct{ interfaces.SessionService }

func (*revokedStreamSessions) GetSession(context.Context, string) (*types.Session, error) {
	return nil, access.ErrForbidden
}

func TestContinueStreamRejectsRevokedTranscriptBeforeReadingCachedEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &Handler{sessionService: &revokedStreamSessions{}}
	// No stream manager or message service: touching them before checking
	// current history permissions would panic or leak cached output.
	r.GET("/sessions/continue-stream/:session_id", h.ContinueStream)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/sessions/continue-stream/session?message_id=message", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.NotContains(t, w.Header().Get("Content-Type"), "text/event-stream")
	require.NotContains(t, w.Body.String(), "diagram")
}

type changingStreamSessions struct {
	stubSessionService
	checks int
}

func (s *changingStreamSessions) CheckSessionStreamAccess(context.Context, string) error {
	s.checks++
	if s.checks > 1 {
		return access.ErrForbidden
	}
	return nil
}

func TestLiveSSERevocationStopsNextBatchWithoutFlushingPrivateContent(t *testing.T) {
	sessions := &changingStreamSessions{}
	stream := &titleBatchStream{batches: [][]interfaces.StreamEvent{
		{{ID: "first", Type: types.ResponseTypeAnswer, Content: "allowed prefix"}},
		{
			{ID: "private", Type: types.ResponseTypeAnswer, Content: "revoked private suffix"},
			{ID: "complete", Type: types.ResponseTypeComplete, Done: true},
		},
	}}
	h := &Handler{sessionService: sessions, streamManager: stream}
	c, response := newTestGinContext(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	h.handleAgentEventsForSSE(ctx, c, "session", "message", "request", nil, false, nil)
	require.NoError(t, ctx.Err())
	require.Equal(t, 2, sessions.checks)
	require.Contains(t, response.Body.String(), "allowed prefix")
	require.Contains(t, response.Body.String(), "knowledge_access_revoked")
	require.NotContains(t, response.Body.String(), "revoked private suffix")
	require.NotContains(t, response.Body.String(), `"response_type":"complete"`)
}
