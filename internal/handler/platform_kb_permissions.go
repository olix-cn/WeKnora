package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// PlatformKBPermissionHandler exposes policy control only, not resource access.
type PlatformKBPermissionHandler struct {
	repo *repository.KnowledgeBaseAccessRepository
}

// NewPlatformKBPermissionHandler constructs the system administration adapter.
func NewPlatformKBPermissionHandler(repo *repository.KnowledgeBaseAccessRepository) *PlatformKBPermissionHandler {
	return &PlatformKBPermissionHandler{repo: repo}
}

// Get implements the transport's narrow policy service with fresh system authority.
// @Summary Get a knowledge-base policy as a current system administrator
// @Tags System Admin
// @Produce json
// @Param id path string true "Knowledge-base ID"
// @Success 200 {object} handler.DataResponse{data=types.KBAccessPolicyView}
// @Security Bearer
// @Router /system/admin/knowledge-base-permissions/{id} [get]
func (h *PlatformKBPermissionHandler) Get(ctx context.Context, id string) (*types.KBAccessPolicyView, error) {
	state, err := h.repo.ReadPlatformPolicy(ctx, id)
	if err != nil {
		return nil, err
	}
	return platformPolicyView(state), nil
}

// Replace reuses the same strict transport decoder and protected repository save.
// @Summary Replace knowledge-base permissions as a current system administrator
// @Tags System Admin
// @Accept json
// @Produce json
// @Param id path string true "Knowledge-base ID"
// @Param body body types.KBAccessPolicyUpdate true "Complete policy with expected_version"
// @Success 200 {object} handler.DataResponse{data=types.KBAccessPolicyView}
// @Security Bearer
// @Router /system/admin/knowledge-base-permissions/{id} [put]
func (h *PlatformKBPermissionHandler) Replace(
	ctx context.Context, id string, input types.KBAccessPolicyUpdate,
) (*types.KBAccessPolicyView, error) {
	state, err := h.repo.ReplacePlatformPolicy(ctx, id, input)
	if err != nil {
		return nil, err
	}
	return platformPolicyView(state), nil
}

// MemberCandidates searches only the library's own workspace.
// @Summary Search grant candidates in the target knowledge-base workspace
// @Tags System Admin
// @Produce json
// @Param id path string true "Knowledge-base ID"
// @Param q query string false "Username search"
// @Param page query int false "Page"
// @Param page_size query int false "Page size 1-100"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Router /system/admin/knowledge-base-permissions/{id}/candidates [get]
func (h *PlatformKBPermissionHandler) MemberCandidates(
	ctx context.Context, id, q string, page, size int,
) ([]types.KBAccessMemberCandidate, int64, error) {
	return h.repo.PlatformCandidates(ctx, id, q, page, size)
}

func platformPolicyView(s *repository.KBAccessState) *types.KBAccessPolicyView {
	v := &types.KBAccessPolicyView{
		MemberDetails: s.MemberDetails, Mode: types.KBAccessInherit, Members: []types.KBMemberGrantInput{},
	}
	if s.Policy != nil {
		v.Mode = s.Policy.Mode
		v.Version = s.Policy.Version
		v.UpdatedAt = &s.Policy.UpdatedAt
	}
	for _, g := range s.Members {
		v.Members = append(v.Members, types.KBMemberGrantInput{UserID: g.UserID, Permission: g.Permission})
	}
	return v
}

// List returns all nondeleted library metadata across workspaces, paginated.
// @Summary List knowledge-base permission metadata across all workspaces
// @Tags System Admin
// @Produce json
// @Param q query string false "Knowledge-base or workspace name"
// @Param tenant_id query int false "Optional workspace filter"
// @Param offset query int false "Offset"
// @Param limit query int false "Page size 1-100"
// @Success 200 {object} map[string]interface{}
// @Security Bearer
// @Router /system/admin/knowledge-base-permissions [get]
func (h *PlatformKBPermissionHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	offset, e1 := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, e2 := strconv.Atoi(c.DefaultQuery("limit", "20"))
	tenant, e3 := strconv.ParseUint(c.DefaultQuery("tenant_id", "0"), 10, 64)
	if e1 != nil || e2 != nil || e3 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pagination"})
		return
	}
	rows, total, err := h.repo.ListPlatformPolicies(c.Request.Context(), c.Query("q"), tenant, offset, limit)
	if memberPolicyHTTPError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total})
}
