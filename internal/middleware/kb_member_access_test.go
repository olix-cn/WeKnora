package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type memberKBShareForGuard struct {
	stubKBShareForGuard
	permission types.OrgMemberRole
}

func (s *memberKBShareForGuard) CheckKBMemberAccess(
	context.Context, string, uint64, types.Caller,
) (types.OrgMemberRole, bool, error) {
	return s.permission, true, nil
}

func TestKBGuardEnforcesMemberPolicyEvenWhenWorkspaceRBACIsOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, permission := range []types.OrgMemberRole{"", types.OrgRoleViewer, types.OrgRoleEditor} {
		for _, required := range []types.OrgMemberRole{types.OrgRoleViewer, types.OrgRoleEditor} {
			r := gin.New()
			r.Use(ErrorHandler())
			r.Use(func(c *gin.Context) {
				caller := types.Caller{TenantID: 7, UserID: "member", Role: types.TenantRoleAdmin}
				c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(), caller))
				c.Next()
			})
			lookup := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb": {ID: "kb", TenantID: 7}}}
			shares := &memberKBShareForGuard{permission: permission}
			r.GET("/kbs/:id", RequireKBAccess(KBIDFromParam("id"), required, lookup, shares, nil, &config.Config{}),
				func(c *gin.Context) { c.Status(http.StatusNoContent) })
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/kbs/kb", nil))
			if permission.IsValid() && permission.HasPermission(required) {
				require.Equal(t, http.StatusNoContent, response.Code)
			} else {
				require.Equal(t, http.StatusForbidden, response.Code)
			}
		}
	}
}

func TestKBMemberWriteGateDistinguishesContentFromSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, permission := range []types.OrgMemberRole{"", types.OrgRoleViewer, types.OrgRoleEditor, types.OrgRoleAdmin} {
		for _, required := range []types.OrgMemberRole{types.OrgRoleEditor, types.OrgRoleAdmin} {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(), types.Caller{
					TenantID: 7, UserID: "delegate", Role: types.TenantRoleContributor,
				}))
				c.Next()
			})
			lookup := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb": {ID: "kb", TenantID: 7}}}
			shares := &memberKBShareForGuard{permission: permission}
			legacy := func(*gin.Context) { t.Fatal("explicit policy must not use legacy ownership") }
			r.POST("/kbs/:id", RequireKBMemberOrOwnership(KBIDFromParam("id"), required, lookup, shares, legacy),
				func(c *gin.Context) { c.Status(http.StatusNoContent) })
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/kbs/kb", nil))
			if permission.IsValid() && permission.HasPermission(required) {
				require.Equal(t, http.StatusNoContent, response.Code)
			} else {
				require.Equal(t, http.StatusForbidden, response.Code)
			}
		}
	}
}
