package router

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// RegisterDocumentCenterRoutes mounts the implemented template sample endpoints.
func RegisterDocumentCenterRoutes(r *gin.RouterGroup, h *handler.DocumentCenterHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	// JWT workspace viewers only. API keys remain denied by the default policy;
	// no new endpoint may become an unscoped way to read private generated files.
	documents := r.Group("/document-center", func(c *gin.Context) {
		caller := types.CallerFromContext(c.Request.Context())
		if caller.TenantID == 0 || caller.UserID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Workspace authentication required"})
			return
		}
		c.Next()
	}, g.Viewer())
	documents.GET("/templates", h.Catalog)
	documents.GET("/templates/:id/preview", h.Preview)
	documents.POST("/datasets/import", h.ImportDataset)
	documents.GET("/sources", h.Sources)
	documents.POST("/tasks", h.Generate)
	documents.GET("/tasks", h.Tasks)
	documents.GET("/tasks/:id/file", h.DownloadTask)
	documents.DELETE("/tasks/:id/file", h.DeleteTaskFile)
	documents.GET("/custom-templates", h.CustomTemplates)
	documents.POST("/custom-templates", g.Admin(), h.UploadTemplate)
	documents.GET("/custom-templates/:id/inspection", g.Admin(), h.InspectTemplate)
	documents.GET("/custom-templates/:id/preview", h.PreviewCustomTemplate)
	documents.GET("/custom-templates/:id/layout", h.TemplateLayout)
	documents.PUT("/custom-templates/:id/layout", g.Admin(), h.ConfigureTemplate)
	documents.POST("/custom-templates/:id/test", g.Admin(), h.TestTemplate)
	documents.POST("/custom-templates/:id/publish", g.Admin(), h.PublishTemplate)
	documents.POST("/custom-templates/:id/disable", g.Admin(), h.DisableTemplate)
}
