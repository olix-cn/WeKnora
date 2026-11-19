package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// KnowledgeBaseMemberPolicyService is the authorized management boundary; the
// handler never reads membership/policy tables or infers permissions itself.
type KnowledgeBaseMemberPolicyService interface {
	Get(context.Context, string) (*types.KBAccessPolicyView, error)
	Replace(context.Context, string, types.KBAccessPolicyUpdate) (*types.KBAccessPolicyView, error)
}

// KnowledgeBaseMemberHandler serves human-only policy management routes.
type KnowledgeBaseMemberHandler struct {
	service KnowledgeBaseMemberPolicyService
}

// NewKnowledgeBaseMemberHandler constructs the management transport adapter.
func NewKnowledgeBaseMemberHandler(service KnowledgeBaseMemberPolicyService) *KnowledgeBaseMemberHandler {
	return &KnowledgeBaseMemberHandler{service: service}
}

// Get returns only the current policy's public management projection.
// @Summary 获取知识库成员访问策略
// @Tags 知识库成员权限
// @Produce json
// @Param id path string true "知识库 ID"
// @Success 200 {object} handler.DataResponse{data=types.KBAccessPolicyView}
// @Failure 403 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Security Bearer
// @Router /knowledge-bases/{id}/member-access [get]
func (h *KnowledgeBaseMemberHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	view, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if memberPolicyHTTPError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// Replace accepts one bounded replacement, with a required version token.
// Omission is not equivalent to expected_version=0: old clients must not
// overwrite an existing policy accidentally. Unknown actor/role fields reject.
// @Summary 原子替换知识库成员访问策略
// @Tags 知识库成员权限
// @Accept json
// @Produce json
// @Param id path string true "知识库 ID"
// @Param request body types.KBAccessPolicyUpdate true "完整名单与 expected_version"
// @Success 200 {object} handler.DataResponse{data=types.KBAccessPolicyView}
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security Bearer
// @Router /knowledge-bases/{id}/member-access [put]
func (h *KnowledgeBaseMemberHandler) Replace(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input struct {
		Mode            types.KBAccessMode          `json:"mode"`
		ExpectedVersion *uint64                     `json:"expected_version"`
		Members         *[]types.KBMemberGrantInput `json:"members"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid knowledge base member policy request"})
		return
	}
	if decoder.Decode(new(any)) != io.EOF || input.ExpectedVersion == nil || input.Members == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode, expected_version and members are required"})
		return
	}
	update := types.KBAccessPolicyUpdate{
		Mode: input.Mode, ExpectedVersion: *input.ExpectedVersion, Members: *input.Members,
	}
	if err := access.ValidateKBAccessPolicy(update); err != nil {
		memberPolicyHTTPError(c, err)
		return
	}
	view, err := h.service.Replace(c.Request.Context(), c.Param("id"), update)
	if memberPolicyHTTPError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// MemberCandidates searches a minimal active-member directory after manager
// authorization.
// @Summary 搜索可授权的有效空间成员
// @Tags 知识库成员权限
// @Produce json
// @Param id path string true "知识库 ID"
// @Param q query string false "用户名搜索"
// @Param page query int false "页码"
// @Param page_size query int false "每页 1–100"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Router /knowledge-bases/{id}/member-access/candidates [get]
func (h *KnowledgeBaseMemberHandler) MemberCandidates(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		memberPolicyHTTPError(c, access.ErrInvalidKBAccessPolicy)
		return
	}
	size, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || page < 1 || page > 1000000 || size < 1 || size > 100 || len(c.Query("q")) > 200 {
		memberPolicyHTTPError(c, access.ErrInvalidKBAccessPolicy)
		return
	}
	service, ok := h.service.(interface {
		MemberCandidates(context.Context, string, string, int, int) ([]types.KBAccessMemberCandidate, int64, error)
	})
	if !ok {
		memberPolicyHTTPError(c, access.ErrForbidden)
		return
	}
	rows, total, err := service.MemberCandidates(c.Request.Context(), c.Param("id"), c.Query("q"), page, size)
	if memberPolicyHTTPError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "total": total})
}

// RecoverTransfer delegates current workspace-admin checks to the service/store.
// @Summary 确认恢复已停止且核对完成的失败转存尝试
// @Tags 知识库成员权限
// @Accept json
// @Produce json
// @Param request body types.KBTransferRecovery true "精确任务坐标与人工核对确认"
// @Success 200 {object} map[string]interface{}
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security Bearer
// @Router /knowledge-base-transfers/recover [post]
func (h *KnowledgeBaseMemberHandler) RecoverTransfer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input types.KBTransferRecovery
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF {
		memberPolicyHTTPError(c, access.ErrInvalidKBAccessPolicy)
		return
	}
	service, ok := h.service.(interface {
		RecoverFailedTransfer(context.Context, types.KBTransferRecovery) error
	})
	if !ok {
		memberPolicyHTTPError(c, access.ErrForbidden)
		return
	}
	if memberPolicyHTTPError(c, service.RecoverFailedTransfer(c.Request.Context(), input)) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListTransfers returns at most 100 outstanding attempt coordinates.
// @Summary 查看本空间未解决的转存尝试
// @Tags 知识库成员权限
// @Produce json
// @Param limit query int false "最大 100"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Router /knowledge-base-transfers [get]
func (h *KnowledgeBaseMemberHandler) ListTransfers(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit < 1 || limit > 100 {
		memberPolicyHTTPError(c, access.ErrInvalidKBAccessPolicy)
		return
	}
	s, ok := h.service.(interface {
		ListTransferFences(context.Context, int) ([]types.KBTransferStatus, error)
	})
	if !ok {
		memberPolicyHTTPError(c, access.ErrForbidden)
		return
	}
	rows, err := s.ListTransferFences(c.Request.Context(), limit)
	if memberPolicyHTTPError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func memberPolicyHTTPError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	status, code := http.StatusServiceUnavailable, "policy_unavailable"
	message := "cannot verify knowledge base permissions"
	switch {
	case errors.Is(err, access.ErrForbidden), errors.Is(err, access.ErrUnauthorized):
		status, code = http.StatusForbidden, "policy_forbidden"
		message = "knowledge base management permission required"
	case errors.Is(err, repository.ErrKBAccessConflict):
		status, code, message = http.StatusConflict, "policy_conflict", "permissions changed; reload before saving"
	case errors.Is(err, repository.ErrKBAccessShared):
		status, code = http.StatusConflict, "policy_shared"
		message = "remove existing knowledge base or agent shares first"
	case errors.Is(err, repository.ErrKBAccessTransferBusy):
		status, code = http.StatusConflict, "policy_transfer_busy"
		message = "a content transfer is active or unresolved; complete or recover it before changing permissions"
	case errors.Is(err, access.ErrInvalidKBAccessPolicy), errors.Is(err, repository.ErrKBAccessMember):
		status, code = http.StatusBadRequest, "policy_invalid"
		message = "invalid policy or unavailable workspace member"
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, repository.ErrKnowledgeBaseNotFound):
		status, code, message = http.StatusNotFound, "policy_not_found", "knowledge base not found"
	}
	c.JSON(status, gin.H{"error": message, "code": code})
	return true
}
