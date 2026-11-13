package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ListUserCreationWorkspaces is SystemAdmin-only, not cross-tenant-superuser-only.
// Return names/IDs only, never tenant credentials or operational configuration.
// @Summary List workspace choices for administrator user creation
// @Tags System Admin
// @Produce json
// @Param keyword query string false "Workspace name search"
// @Param page query int false "Page (100 entries per page)"
// @Success 200 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Router /system/admin/users/workspaces [get]
func (h *SystemHandler) ListUserCreationWorkspaces(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	items, total, err := h.tenantSvc.SearchTenants(c.Request.Context(), strings.TrimSpace(c.Query("keyword")), 0, page, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load workspaces"})
		return
	}
	type option struct {
		ID   uint64 `json:"id"`
		Name string `json:"name"`
	}
	options := make([]option, 0, len(items))
	for _, item := range items {
		if item != nil && item.Status == "active" {
			options = append(options, option{item.ID, item.Name})
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": options, "total": total, "page": page, "page_size": 100})
}
