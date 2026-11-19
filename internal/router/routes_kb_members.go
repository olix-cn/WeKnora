package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterKnowledgeBaseMemberRoutes keeps human policy management separate
// from machine capabilities. Service/store authorization is always enforced,
// including when workspace RBAC rollout is set to log-only.
func RegisterKnowledgeBaseMemberRoutes(r *gin.RouterGroup, h *handler.KnowledgeBaseMemberHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	members := r.Group("/knowledge-bases/:id/member-access", middleware.DenyAPIKeyPrincipal(), g.Viewer())
	members.GET("", h.Get)
	members.PUT("", h.Replace)
	members.GET("/candidates", h.MemberCandidates)
	r.GET("/knowledge-base-transfers", middleware.DenyAPIKeyPrincipal(), g.Admin(), h.ListTransfers)
	r.POST("/knowledge-base-transfers/recover", middleware.DenyAPIKeyPrincipal(), g.Admin(), h.RecoverTransfer)
}
