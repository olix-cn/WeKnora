package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type creationWorkspaceTenants struct {
	interfaces.TenantService
	keyword string
}

func (s *creationWorkspaceTenants) SearchTenants(_ context.Context, keyword string, _ uint64, page, pageSize int) ([]*types.Tenant, int64, error) {
	s.keyword = keyword
	return []*types.Tenant{{ID: 42, Name: "Engineering", Status: "active"}, {ID: 99, Name: "Disabled", Status: "inactive"}}, 2, nil
}

func TestUserCreationWorkspacesReturnOnlySafeActiveOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenants := &creationWorkspaceTenants{}
	h := &SystemHandler{tenantSvc: tenants}
	r := gin.New()
	r.GET("/workspaces", h.ListUserCreationWorkspaces)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/workspaces?keyword=Engineering", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, "Engineering", tenants.keyword)
	var body struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	require.Len(t, body.Items[0], 2)
	require.Equal(t, "Engineering", body.Items[0]["name"])
}

func TestCreateUserPassesDefaultWorkspaceAndRoleWithoutInvitations(t *testing.T) {
	users := &createUserService{createdUser: &types.User{ID: "created", Username: "alice", TenantID: 42}}
	h := &SystemHandler{userSvc: users}
	w := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/system/admin/users/create", bytes.NewBufferString(`{"username":"alice","password":"1","default_tenant_id":42,"role":"viewer"}`))
	request.Header.Set("Content-Type", "application/json")
	createSystemUserRouter(h, "admin").ServeHTTP(w, request)
	require.Equal(t, 201, w.Code, w.Body.String())
	require.Equal(t, uint64(42), users.gotReq.DefaultTenantID)
	require.Equal(t, types.TenantRoleViewer, users.gotReq.Role)
}

func TestCreateUserRejectsIncompleteWorkspaceAssignment(t *testing.T) {
	for _, body := range []string{
		`{"username":"alice","default_tenant_id":42}`,
		`{"username":"alice","role":"viewer"}`,
		`{"username":"alice","default_tenant_id":42,"role":"system_admin"}`,
	} {
		h := &SystemHandler{}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/system/admin/users/create", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		createSystemUserRouter(h, "admin").ServeHTTP(w, req)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
}
