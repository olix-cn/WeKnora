package handler

import (
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

// CorporateDirectoryHandler exposes authorized platform personnel operations.
type CorporateDirectoryHandler struct {
	repo *repository.CorporateDirectoryRepository
}

// NewCorporateDirectoryHandler constructs the directory HTTP adapter.
func NewCorporateDirectoryHandler(repo *repository.CorporateDirectoryRepository) *CorporateDirectoryHandler {
	return &CorporateDirectoryHandler{repo: repo}
}

func directoryError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	status, code := http.StatusServiceUnavailable, "directory_unavailable"
	switch {
	case errors.Is(err, access.ErrForbidden):
		status, code = http.StatusForbidden, "directory_forbidden"
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusNotFound, "directory_not_found"
	case errors.Is(err, repository.ErrDirectoryInvalid):
		status, code = http.StatusBadRequest, "directory_invalid"
	case errors.Is(err, repository.ErrDirectoryConflict):
		status, code = http.StatusConflict, "directory_conflict"
	case errors.Is(err, repository.ErrDepartmentOccupied):
		status, code = http.StatusConflict, "department_occupied"
	case errors.Is(err, repository.ErrDepartmentLeader):
		status, code = http.StatusConflict, "department_leader"
	}
	c.JSON(status, gin.H{"error": code, "code": code})
	return true
}

func directoryBody(c *gin.Context, out any) bool {
	c.Header("Cache-Control", "no-store")
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil || dec.Decode(new(any)) != io.EOF {
		directoryError(c, repository.ErrDirectoryInvalid)
		return false
	}
	return true
}

// Tree returns the personnel hierarchy, unrelated to cross-workspace sharing.
// @Summary Read corporate directory and department tree
// @Tags System Admin
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /system/admin/directory [get]
func (h *CorporateDirectoryHandler) Tree(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	root, rows, err := h.repo.Tree(c.Request.Context())
	if directoryError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"directory": root, "departments": rows})
}

// SaveDepartment creates or version-replaces a department.
// @Summary Create or update corporate department
// @Tags System Admin
// @Accept json
// @Produce json
// @Param department_id path string false "Department ID for update"
// @Param body body types.DepartmentUpdate true "Complete department input"
// @Success 200 {object} types.Department
// @Router /system/admin/directory/departments [post]
// @Router /system/admin/directory/departments/{department_id} [put]
func (h *CorporateDirectoryHandler) SaveDepartment(c *gin.Context) {
	var input types.DepartmentUpdate
	if !directoryBody(c, &input) {
		return
	}
	row, err := h.repo.SaveDepartment(c.Request.Context(), c.Param("department_id"), input)
	if directoryError(c, err) {
		return
	}
	c.JSON(http.StatusOK, row)
}

// DeleteDepartment refuses occupied departments and stale versions.
// @Summary Delete empty department
// @Tags System Admin
// @Accept json
// @Produce json
// @Param department_id path string true "Department ID"
// @Param body body map[string]interface{} true "expected_version required"
// @Success 200 {object} map[string]interface{}
// @Router /system/admin/directory/departments/{department_id} [delete]
func (h *CorporateDirectoryHandler) DeleteDepartment(c *gin.Context) {
	var input struct {
		ExpectedVersion uint64 `json:"expected_version"`
	}
	if !directoryBody(c, &input) {
		return
	}
	err := h.repo.DeleteDepartment(c.Request.Context(), c.Param("department_id"), input.ExpectedVersion)
	if directoryError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Members searches the complete account directory before count and pagination.
// @Summary Search corporate personnel
// @Tags System Admin
// @Produce json
// @Param keyword query string false "Username, email or display name"
// @Param department_id query string false "Department ID; unassigned or omitted"
// @Param include_descendants query boolean false "Include children"
// @Param status query string false "active or inactive"
// @Param offset query int false "Offset"
// @Param limit query int false "Page size 1-100"
// @Success 200 {object} map[string]interface{}
// @Router /system/admin/directory/members [get]
func (h *CorporateDirectoryHandler) Members(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	offset, e1 := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, e2 := strconv.Atoi(c.DefaultQuery("limit", "20"))
	desc, e3 := strconv.ParseBool(c.DefaultQuery("include_descendants", "false"))
	if e1 != nil || e2 != nil || e3 != nil {
		directoryError(c, repository.ErrDirectoryInvalid)
		return
	}
	rows, total, err := h.repo.Members(c.Request.Context(), types.PersonnelQuery{
		Keyword: c.Query("keyword"), DepartmentID: c.Query("department_id"), IncludeDescendants: desc,
		Status: c.Query("status"), Offset: offset, Limit: limit,
	})
	if directoryError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"users": rows, "total": total, "offset": offset, "limit": limit, "has_more": int64(offset+len(rows)) < total,
	})
}

// MoveMembers atomically assigns a versioned batch, without changing resource grants.
// @Summary Move personnel to a department
// @Tags System Admin
// @Accept json
// @Produce json
// @Param body body map[string]interface{} true "department_id (nullable), members with expected_version"
// @Success 200 {object} map[string]interface{}
// @Router /system/admin/directory/members/department [put]
func (h *CorporateDirectoryHandler) MoveMembers(c *gin.Context) {
	var input struct {
		DepartmentID json.RawMessage       `json:"department_id"`
		Members      []types.PersonnelMove `json:"members"`
	}
	if !directoryBody(c, &input) {
		return
	}
	var departmentID *string
	if len(input.DepartmentID) == 0 {
		directoryError(c, repository.ErrDirectoryInvalid)
		return
	}
	if err := json.Unmarshal(input.DepartmentID, &departmentID); err != nil {
		directoryError(c, repository.ErrDirectoryInvalid)
		return
	}
	if directoryError(c, h.repo.MoveMembers(c.Request.Context(), departmentID, input.Members)) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// UpdatePersonnel edits display name only, never login identity or roles.
// @Summary Edit personnel display name
// @Tags System Admin
// @Accept json
// @Produce json
// @Param user_id path string true "User ID"
// @Param body body map[string]interface{} true "display_name, expected_version"
// @Success 200 {object} map[string]interface{}
// @Router /system/admin/directory/members/{user_id} [put]
func (h *CorporateDirectoryHandler) UpdatePersonnel(c *gin.Context) {
	var input struct {
		DisplayName     string `json:"display_name"`
		ExpectedVersion uint64 `json:"expected_version"`
	}
	if !directoryBody(c, &input) {
		return
	}
	err := h.repo.UpdatePersonnel(c.Request.Context(), c.Param("user_id"), input.DisplayName, input.ExpectedVersion)
	if directoryError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
