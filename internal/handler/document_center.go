package handler

import (
	"errors"
	"mime"
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/documentgen"
	"github.com/gin-gonic/gin"
)

// DocumentCenterHandler serves built-in templates and private stored-result generation.
// Custom templates are enabled only after their migration; live MCP acquisition is not supported.
type DocumentCenterHandler struct {
	generation *service.DocumentGenerationService
}

// NewDocumentCenterHandler creates the built-in template catalog handler.
func NewDocumentCenterHandler() *DocumentCenterHandler { return &DocumentCenterHandler{} }

// NewDocumentCenterHandlerWithGeneration enables persistence after migrations are available.
func NewDocumentCenterHandlerWithGeneration(s *service.DocumentGenerationService) *DocumentCenterHandler {
	return &DocumentCenterHandler{generation: s}
}

// Catalog advertises only supported template operations, not planned features.
func (h *DocumentCenterHandler) Catalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"generation_available":       h.generation != nil && h.generation.Ready(c.Request.Context()),
			"custom_templates_available": h.generation != nil && h.generation.TemplatesReady(c.Request.Context()),
			// Legacy sample download remains addressable for old clients, but
			// only published workspace templates belong in the current library.
			"templates": []gin.H{},
		},
	})
}

func (h *DocumentCenterHandler) available(c *gin.Context) bool {
	if h.generation == nil || !h.generation.Ready(c.Request.Context()) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Generation storage is not available"})
		return false
	}
	return true
}

func documentError(c *gin.Context, err error) {
	code := http.StatusInternalServerError
	message := "Document operation failed"
	switch {
	case errors.Is(err, service.ErrDocumentUnavailable):
		code = http.StatusNotFound
		message = "Document is unavailable"
	case errors.Is(err, service.ErrDocumentConflict):
		code = http.StatusConflict
		message = "Request key conflict"
	case errors.Is(err, service.ErrDocumentInvalid):
		code = http.StatusBadRequest
		message = err.Error()
	}
	c.JSON(code, gin.H{"success": false, "message": message})
}

// ImportDataset accepts message coordinates only, never browser-submitted BOM cells.
func (h *DocumentCenterHandler) ImportDataset(c *gin.Context) {
	if !h.available(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req struct {
		SessionID string `json:"session_id" binding:"required,max=36"`
		MessageID string `json:"message_id" binding:"required,max=36"`
		ResultID  string `json:"result_id" binding:"required,max=128"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"message": "Invalid import request"})
		return
	}
	row, err := h.generation.Import(c.Request.Context(), req.SessionID, req.MessageID, req.ResultID)
	if err != nil {
		documentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": row})
}

// Sources discovers importable saved results; expired remote snapshots need not be reconnected.
func (h *DocumentCenterHandler) Sources(c *gin.Context) {
	if !h.available(c) {
		return
	}
	id := c.Query("session_id")
	if len(id) != 36 {
		c.JSON(400, gin.H{"message": "Invalid session"})
		return
	}
	rows, err := h.generation.Sources(c.Request.Context(), id)
	if err != nil {
		documentError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": rows})
}

// Generate renders an owned imported dataset using an immutable built-in template.
func (h *DocumentCenterHandler) Generate(c *gin.Context) {
	if !h.available(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req struct {
		DatasetID     string            `json:"dataset_id" binding:"required,max=36"`
		TemplateID    string            `json:"template_id" binding:"required,max=64"`
		RequestKey    string            `json:"request_key" binding:"required,max=128"`
		AcceptPartial bool              `json:"accept_partial"`
		Bindings      map[string]string `json:"bindings"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"message": "Invalid generation request"})
		return
	}
	row, err := h.generation.GenerateWithBindings(
		c.Request.Context(), req.DatasetID, req.TemplateID, req.RequestKey, req.AcceptPartial, req.Bindings)
	if err != nil {
		documentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}

// Tasks lists private durable task metadata, without storage locators.
func (h *DocumentCenterHandler) Tasks(c *gin.Context) {
	if !h.available(c) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "30"))
	if page < 1 || page > 10000 || size < 1 || size > 100 {
		c.JSON(400, gin.H{"message": "Invalid page"})
		return
	}
	rows, total, err := h.generation.List(c.Request.Context(), page, size)
	if err != nil {
		documentError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": rows, "total": total, "page": page, "page_size": size})
}

// DownloadTask authorizes the source and generation before opening the stored file.
func (h *DocumentCenterHandler) DownloadTask(c *gin.Context) {
	if !h.available(c) {
		return
	}
	reader, row, err := h.generation.Download(c.Request.Context(), c.Param("id"))
	if err != nil {
		documentError(c, err)
		return
	}
	defer func() { _ = reader.Close() }()
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": row.FileName}))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(200, row.FileSize,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", reader, nil)
}

// DeleteTaskFile removes a file while retaining the task tombstone.
func (h *DocumentCenterHandler) DeleteTaskFile(c *gin.Context) {
	if !h.available(c) {
		return
	}
	if err := h.generation.Delete(c.Request.Context(), c.Param("id")); err != nil {
		documentError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// Preview downloads a workbook rendered solely from synthetic data.
func (h *DocumentCenterHandler) Preview(c *gin.Context) {
	if c.Param("id") != documentgen.BOMTemplateID {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Template not found"})
		return
	}
	// Synthetic data only; never consult a user file or external MCP endpoint.
	data := documentgen.Dataset{
		Columns: []documentgen.Column{
			{ID: "part_number", Title: "图号"},
			{ID: "name", Title: "名称"},
			{ID: "quantity", Title: "数量"},
		},
		Rows: []documentgen.Row{{Source: "synthetic-example.dwg", Values: []documentgen.Cell{
			{Value: "000001", State: "value"},
			{Value: "合成示例零件", State: "value"},
			{Value: "2", State: "value"},
		}}},
		Issues: []string{"仅为合成样例，未读取任何业务图纸。"},
	}
	output, err := documentgen.RenderBOM(c.Request.Context(), data, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Preview generation failed"})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="engineering-bom-sample.xlsx"`)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", output)
}
