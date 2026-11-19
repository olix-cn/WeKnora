package session

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// streamMemberAccess verifies the next outgoing batch against current member
// policy. Revocation/errors stop delivery without flushing held private text.
// Already transmitted bytes cannot be withdrawn. This does not cancel other
// clients or pretend to undo a background tool's committed side effects.
func (h *Handler) streamMemberAccess(c *gin.Context, sessionID string) bool {
	guard, ok := h.sessionService.(interface {
		CheckSessionStreamAccess(context.Context, string) error
	})
	if !ok {
		return true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := guard.CheckSessionStreamAccess(ctx, sessionID); err != nil {
		c.SSEvent("message", gin.H{
			"response_type": "error", "code": "knowledge_access_revoked", "done": true,
			"content": "Conversation knowledge access has been revoked or cannot be verified.",
		})
		c.Writer.Flush()
		return false
	}
	return true
}
