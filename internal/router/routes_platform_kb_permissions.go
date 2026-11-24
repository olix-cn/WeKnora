package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterPlatformKBPermissionRoutes separates global policy control from KB reads.
func RegisterPlatformKBPermissionRoutes(r *gin.RouterGroup, h *handler.PlatformKBPermissionHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	group := r.Group("/system/admin/knowledge-base-permissions", middleware.DenyAPIKeyPrincipal(), g.SystemAdmin())
	group.GET("", h.List)
	policy := handler.NewKnowledgeBaseMemberHandler(h)
	group.GET("/:id", policy.Get)
	group.PUT("/:id", policy.Replace)
	group.GET("/:id/candidates", policy.MemberCandidates)
}
