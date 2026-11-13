package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserCreationWorkspaceRoutesRequireSystemAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	g := &rbacGuards{cfg: &config.Config{}}
	r := gin.New()
	RegisterSystemAdminRoutes(r.Group("/api/v1"), &handler.SystemHandler{}, nil, g)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/system/admin/users/workspaces"},
		{http.MethodPost, "/api/v1/system/admin/users/create"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusForbidden, w.Code)
		_, declared := g.apiKeyAuthorizer.Lookup(tc.method, tc.path)
		require.False(t, declared, "API keys cannot provision users or list admin-only workspace choices")
	}
}
