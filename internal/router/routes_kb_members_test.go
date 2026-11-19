package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Use the production route registration, service and SQL persistence together.
// Authentication is a synthetic test boundary, never a live login or account.
func TestKBMemberRoutesRealRepository(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "routes.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.TenantMember{}, &types.KnowledgeBase{},
		&types.KnowledgeBaseShare{}, &types.CustomAgent{}, &types.AgentShare{}, &types.AuditLog{}))
	up, err := os.ReadFile("../../migrations/sqlite/000056_knowledge_base_member_access.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	require.NoError(t, db.Create(&types.Tenant{ID: 7, Name: "Synthetic route workspace"}).Error)
	for i, id := range []string{"owner", "delegate", "reader"} {
		require.NoError(t, db.Create(&types.User{ID: id, Username: id, TenantID: 7}).Error)
		role := types.TenantRoleContributor
		if id == "owner" {
			role = types.TenantRoleOwner
		}
		require.NoError(t, db.Create(&types.TenantMember{
			ID: uint64(i + 1), TenantID: 7, UserID: id,
			Role: role, Status: types.TenantMemberStatusActive,
		}).Error)
	}
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", TenantID: 7, CreatorID: "owner"}).Error)
	svc := service.NewKnowledgeBaseMemberService(repository.NewKnowledgeBaseAccessRepository(db))
	r := gin.New()
	r.Use(func(c *gin.Context) {
		id := c.GetHeader("X-Test-User")
		ctx := context.WithValue(c.Request.Context(), types.UserIDContextKey, id)
		ctx = types.WithCaller(ctx, types.Caller{TenantID: 7, UserID: id, Role: types.TenantRoleOwner})
		if c.GetHeader("X-Test-Machine") != "" {
			ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true})
			ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "7"})
		}
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantRoleContextKey.String(), types.TenantRoleOwner)
		c.Next()
	})
	// Log-only workspace RBAC must not weaken persisted policy authorization.
	off := false
	RegisterKnowledgeBaseMemberRoutes(r.Group("/api/v1"), handler.NewKnowledgeBaseMemberHandler(svc),
		&rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &off}}})
	request := func(method, path, user string, input any, machine bool) *httptest.ResponseRecorder {
		body, err := json.Marshal(input)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", user)
		if machine {
			req.Header.Set("X-Test-Machine", "1")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	path := "/api/v1/knowledge-bases/kb/member-access"
	require.Equal(t, http.StatusOK, request("GET", path, "owner", nil, false).Code)
	for _, user := range []string{"delegate", "reader"} {
		require.Equal(t, http.StatusForbidden, request("GET", path, user, nil, false).Code)
	}
	require.Equal(t, http.StatusForbidden, request("GET", path, "owner", nil, true).Code)
	update := map[string]any{
		"mode": "restricted", "expected_version": 0,
		"members": []map[string]string{{"user_id": "delegate", "permission": "admin"}},
	}
	w := request("PUT", path, "owner", update, false)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, http.StatusConflict, request("PUT", path, "owner", update, false).Code)
	require.Equal(t, http.StatusOK, request("GET", path, "delegate", nil, false).Code)
	require.Equal(t, http.StatusOK, request("GET", path+"/candidates?q=reader", "delegate", nil, false).Code)
	require.Equal(t, http.StatusForbidden, request("GET", path+"/candidates", "reader", nil, false).Code)
	require.Equal(t, http.StatusForbidden,
		request("GET", "/api/v1/knowledge-base-transfers", "delegate", nil, false).Code)
	require.Equal(t, http.StatusOK, request("GET", "/api/v1/knowledge-base-transfers", "owner", nil, false).Code)
	require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id = ?", "delegate").
		Update("status", "inactive").Error)
	require.Equal(t, http.StatusForbidden, request("GET", path, "delegate", nil, false).Code)
	if python := os.Getenv("WEKNORA_KB_BROWSER_PYTHON"); python != "" {
		require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id = ?", "delegate").
			Update("status", types.TenantMemberStatusActive).Error)
		// Opt-in real HTTP browser loop; all accounts/database are disposable.
		server := httptest.NewServer(r)
		defer server.Close()
		cmd := exec.Command(python, "../../tests/browser/kb_member_access.py", server.URL)
		cmd.Env = os.Environ()
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
		t.Log(string(output))
	}
}
