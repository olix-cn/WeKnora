package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterCorporateDirectoryRoutes mounts human system-admin-only personnel management.
func RegisterCorporateDirectoryRoutes(r *gin.RouterGroup, h *handler.CorporateDirectoryHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	group := r.Group("/system/admin/directory", middleware.DenyAPIKeyPrincipal(), g.SystemAdmin())
	group.GET("", h.Tree)
	group.GET("/members", h.Members)
	group.PUT("/members/:user_id", h.UpdatePersonnel)
	group.PUT("/members/department", h.MoveMembers)
	group.POST("/departments", h.SaveDepartment)
	group.PUT("/departments/:department_id", h.SaveDepartment)
	group.DELETE("/departments/:department_id", h.DeleteDepartment)
}
