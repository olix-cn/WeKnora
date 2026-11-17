package router

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestDocumentCenterRequiresWorkspaceCallerAndDoesNotNeedSandbox(t *testing.T) {
	enforce := true
	g := &rbacGuards{
		cfg:              &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enforce}},
		apiKeyAuthorizer: middleware.NewAPIKeyRouteAuthorizer(),
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	RegisterDocumentCenterRoutes(r.Group("/api/v1", g.apiKeyAuthorizer.Middleware()),
		handler.NewDocumentCenterHandler(), g)
	request := func(ctx context.Context, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
		return w
	}
	path := "/api/v1/document-center/templates"
	w := request(context.Background(), path)
	require.NotEqual(t, http.StatusOK, w.Code)
	ctx := types.WithCaller(context.Background(), types.Caller{
		TenantID: 1, UserID: "viewer", Role: types.TenantRoleViewer,
	})
	w = request(ctx, path)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"generation_available":false`)
	w = request(ctx, path+"/engineering_bom_v1/preview")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	value, err := f.GetCellValue("BOM明细", "A2")
	require.NoError(t, err)
	require.Equal(t, "000001", value)
	w = request(ctx, path+"/../../private/preview")
	require.Equal(t, http.StatusNotFound, w.Code)
	w = request(ctx, path+"/unknown/preview")
	require.Equal(t, http.StatusNotFound, w.Code)
	keyCtx := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{})
	w = request(keyCtx, path)
	require.Equal(t, http.StatusForbidden, w.Code)
	for _, endpoint := range []string{"/custom-templates", "/custom-templates/id/layout"} {
		w = request(keyCtx, "/api/v1/document-center"+endpoint)
		require.Equal(t, http.StatusForbidden, w.Code)
	}
	w = request(ctx, "/api/v1/document-center/custom-templates/id/inspection")
	require.Equal(t, http.StatusForbidden, w.Code)
	w = request(keyCtx, "/api/v1/document-center/custom-templates/id/inspection")
	require.Equal(t, http.StatusForbidden, w.Code)
	w = request(keyCtx, "/api/v1/document-center/custom-templates/id/preview")
	require.Equal(t, http.StatusForbidden, w.Code)
	for _, endpoint := range []string{
		"/custom-templates", "/custom-templates/id/test",
		"/custom-templates/id/publish", "/custom-templates/id/disable",
	} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/document-center"+endpoint, nil).WithContext(ctx))
		require.Equal(t, http.StatusForbidden, w.Code)
	}
}
