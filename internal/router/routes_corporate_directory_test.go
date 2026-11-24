package router

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCorporateDirectoryRoutesRequireRealCurrentSystemAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "routes.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.AuditLog{}, &types.KnowledgeBase{}, &types.TenantMember{},
		&types.KnowledgeBaseAccessPolicy{}, &types.KnowledgeBaseMemberGrant{}))
	up, err := os.ReadFile("../../migrations/sqlite/000058_corporate_directory.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	require.NoError(t, db.Create(&types.User{
		ID: "admin", Username: "admin", IsActive: true, IsSystemAdmin: true,
	}).Error)
	require.NoError(t, db.Create(&types.User{ID: "regular", Username: "regular", IsActive: true}).Error)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.UserIDContextKey, c.GetHeader("X-User"))
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, c.GetHeader("X-Admin") == "true")
		if c.GetHeader("X-Key") == "true" {
			ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true})
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	RegisterPlatformKBPermissionRoutes(r.Group("/api/v1"),
		handler.NewPlatformKBPermissionHandler(repository.NewKnowledgeBaseAccessRepository(db)),
		&rbacGuards{cfg: &config.Config{}})
	off := false
	h := handler.NewCorporateDirectoryHandler(repository.NewCorporateDirectoryRepository(db))
	RegisterCorporateDirectoryRoutes(r.Group("/api/v1"), h,
		&rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &off}}})
	call := func(method, path, user, body string, key bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/system/admin/directory"+path, bytes.NewBufferString(body))
		req.Header.Set("X-User", user)
		req.Header.Set("X-Admin", "true")
		if key {
			req.Header.Set("X-Key", "true")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, user := range []string{"admin", "regular"} {
		req := httptest.NewRequest("GET", "/api/v1/system/admin/knowledge-base-permissions", nil)
		req.Header.Set("X-User", user)
		req.Header.Set("X-Admin", "true")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if user == "admin" {
			require.Equal(t, 200, w.Code, w.Body.String())
		} else {
			require.Equal(t, 403, w.Code)
		}
	}
	require.Equal(t, 200, call("GET", "", "admin", "", false).Code)
	require.Equal(t, 403, call("GET", "", "regular", "", false).Code)
	require.Equal(t, 403, call("GET", "", "admin", "", true).Code)
	require.Equal(t, 400, call("POST", "/departments", "admin", `{"name":"test","actor":"admin"}`, false).Code)
	require.Equal(t, 200, call("POST", "/departments", "admin", `{"name":"Test","expected_version":0}`, false).Code)
	require.Equal(t, 400, call("GET", "/members?limit=999", "admin", "", false).Code)
	require.Equal(t, 200, call("GET", "/members?keyword=regular", "admin", "", false).Code)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "admin").Update("is_system_admin", false).Error)
	require.Equal(t, 403, call("GET", "", "admin", "", false).Code)
}
