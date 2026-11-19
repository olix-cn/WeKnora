package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEmbedAuthenticationDoesNotGrantRestrictedKnowledgeBaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channels := &fakeEmbedChannelService{
		channels: map[string]*types.EmbedChannel{"channel": {
			ID: "channel", TenantID: 7, Enabled: true, AllowedOrigins: types.JSON(`["https://example.test"]`),
		}},
		sessions: map[string]string{"ems_valid": "channel"},
	}
	r := gin.New()
	r.Use(ErrorHandler())
	r.GET("/api/v1/embed/:channel_id/kbs/:id", EmbedAuth(channels,
		&fakeTenantService{tenant: &types.Tenant{ID: 7}}, nil),
		RequireKBAccess(KBIDFromParam("id"), types.OrgRoleViewer,
			&stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb": {ID: "kb", TenantID: 7}}},
			&memberKBShareForGuard{}, nil, &config.Config{}),
		func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/api/v1/embed/channel/kbs/kb", nil)
	req.Header.Set("Authorization", "Embed ems_valid")
	req.Header.Set("Origin", "https://example.test")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}
