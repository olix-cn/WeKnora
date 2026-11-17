package handler

import (
	"io"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/documentgen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func (h *DocumentCenterHandler) templatesAvailable(c *gin.Context, manager bool) bool {
	if h.generation == nil || !h.generation.TemplatesReady(c.Request.Context()) {
		c.JSON(503, gin.H{"message": "Template storage is unavailable"})
		return false
	}
	if manager && !types.CallerFromContext(c.Request.Context()).Role.HasPermission(types.TenantRoleAdmin) {
		c.JSON(403, gin.H{"message": "Template management requires workspace admin"})
		return false
	}
	return true
}

// CustomTemplates lists published versions and the manager's own drafts.
func (h *DocumentCenterHandler) CustomTemplates(c *gin.Context) {
	if !h.templatesAvailable(c, false) {
		return
	}
	rows, err := h.generation.TemplateList(c.Request.Context())
	if err != nil {
		documentError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": rows})
}

// UploadTemplate accepts bounded xlsx files, never executable templates or file paths.
func (h *DocumentCenterHandler) UploadTemplate(c *gin.Context) {
	if !h.templatesAvailable(c, true) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (10<<20)+(64<<10))
	if err := c.Request.ParseMultipartForm(64 << 10); err != nil {
		c.JSON(400, gin.H{"message": "Invalid or oversized template upload"})
		return
	}
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	file, err := c.FormFile("file")
	if err != nil || file.Size > 10<<20 || !strings.HasSuffix(strings.ToLower(file.Filename), ".xlsx") {
		c.JSON(400, gin.H{"message": "Upload an xlsx under 10 MiB"})
		return
	}
	r, err := file.Open()
	if err != nil {
		c.JSON(400, gin.H{"message": "Cannot read template"})
		return
	}
	defer func() { _ = r.Close() }()
	payload, err := io.ReadAll(io.LimitReader(r, (10<<20)+1))
	if err != nil {
		c.JSON(400, gin.H{"message": "Cannot read template"})
		return
	}
	row, inspection, err := h.generation.UploadTemplate(
		c.Request.Context(), c.PostForm("name"), c.PostForm("base_version_id"), payload)
	if err != nil {
		documentError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(201, gin.H{"success": true, "data": row, "inspection": inspection})
}

// InspectTemplate supplies private literal header choices for guided configuration.
func (h *DocumentCenterHandler) InspectTemplate(c *gin.Context) {
	if !h.templatesAvailable(c, true) {
		return
	}
	inspection, err := h.generation.InspectManagedTemplate(c.Request.Context(), c.Param("id"))
	if err != nil {
		documentError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"success": true, "data": inspection})
}

// PreviewCustomTemplate returns authenticated bytes for the private preview drawer.
func (h *DocumentCenterHandler) PreviewCustomTemplate(c *gin.Context) {
	if !h.templatesAvailable(c, false) {
		return
	}
	payload, _, err := h.generation.PreviewTemplate(c.Request.Context(), c.Param("id"))
	if err != nil {
		documentError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", payload)
}

// ConfigureTemplate stores exact field coordinates; published versions are immutable.
func (h *DocumentCenterHandler) ConfigureTemplate(c *gin.Context) {
	if !h.templatesAvailable(c, true) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var layout documentgen.TemplateLayout
	if err := c.ShouldBindJSON(&layout); err != nil {
		c.JSON(400, gin.H{"message": "Invalid template layout"})
		return
	}
	if err := h.generation.ConfigureTemplate(c.Request.Context(), c.Param("id"), layout); err != nil {
		documentError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// TemplateLayout supplies published field requirements or an owner's draft configuration.
func (h *DocumentCenterHandler) TemplateLayout(c *gin.Context) {
	if !h.templatesAvailable(c, false) {
		return
	}
	layout, err := h.generation.TemplateLayout(c.Request.Context(), c.Param("id"))
	if err != nil {
		documentError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": layout})
}

// TestTemplate generates a private synthetic test; it does not publish the version.
func (h *DocumentCenterHandler) TestTemplate(c *gin.Context) {
	if !h.templatesAvailable(c, true) {
		return
	}
	output, err := h.generation.TestTemplate(c.Request.Context(), c.Param("id"))
	if err != nil {
		documentError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="template-synthetic-test.xlsx"`)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", output)
}

// PublishTemplate requires explicit confirmation that sample data have been removed.
func (h *DocumentCenterHandler) PublishTemplate(c *gin.Context) {
	if !h.templatesAvailable(c, true) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req struct {
		SampleCleared bool `json:"sample_cleared"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"message": "Invalid publish request"})
		return
	}
	if err := h.generation.PublishTemplate(c.Request.Context(), c.Param("id"), req.SampleCleared); err != nil {
		documentError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// DisableTemplate stops new generations without deleting previous files.
func (h *DocumentCenterHandler) DisableTemplate(c *gin.Context) {
	if !h.templatesAvailable(c, true) {
		return
	}
	if err := h.generation.DisableTemplate(c.Request.Context(), c.Param("id")); err != nil {
		documentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
