package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type deniedMemberResourceCatalog struct{ stubResourceCatalog }

func (*deniedMemberResourceCatalog) AuthorizeResourceMemberAccess(context.Context, uint64, string, bool) error {
	return access.ErrForbidden
}

func TestRawFileRouteCannotUseTenantOwnershipToBypassMemberPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantInfoContextKey, &types.Tenant{ID: 7})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET("/files", newFileServeHandler(&stubFileService{}, nil, &deniedMemberResourceCatalog{}))
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/files?file_path=local://7/private.txt", nil))
	require.Equal(t, http.StatusForbidden, response.Code)
}

func TestSignedFileRouteRechecksMemberPolicyForGETAndHEAD(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
	r := gin.New()
	servePresignedFiles(r, &stubTenantService{}, nil, &deniedMemberResourceCatalog{})
	signed, err := secutils.SignFileURL("https://example.test", "local://7/private.txt", 7, time.Minute)
	require.NoError(t, err)
	parsed, err := url.Parse(signed)
	require.NoError(t, err)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest(method, "/api/v1/files/presigned?"+parsed.RawQuery, nil))
		require.Equal(t, http.StatusForbidden, response.Code)
	}
}
