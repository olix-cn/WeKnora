package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type authDepartmentLookup func(context.Context, string) (string, error)

func (f authDepartmentLookup) DepartmentNameForUser(ctx context.Context, id string) (string, error) {
	return f(ctx, id)
}

func TestAuthMeReturnsOnlyOwnDepartmentForHumanUsers(t *testing.T) {
	for _, tc := range []struct {
		name      string
		principal types.Principal
		resultErr error
		want      string
		lookup    bool
	}{
		{name: "self", principal: types.Principal{Type: types.PrincipalWebUser, ID: "self"}, want: "技术部", lookup: true},
		{name: "lookup failure", principal: types.Principal{Type: types.PrincipalWebUser, ID: "self"}, resultErr: errors.New("directory unavailable"), lookup: true},
		{name: "other user", principal: types.Principal{Type: types.PrincipalWebUser, ID: "other"}},
		{name: "machine", principal: types.Principal{Type: types.PrincipalAPIPlatform, ID: "self"}},
		{name: "no principal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := NewAuthHandler(&config.Config{Auth: &config.AuthConfig{}, Tenant: &config.TenantConfig{}},
				&tenantPolicyUserService{user: &types.User{ID: "self"}}, nil, nil, nil).
				WithDepartmentLookup(authDepartmentLookup(func(_ context.Context, id string) (string, error) {
					calls++
					require.Equal(t, "self", id)
					return "技术部", tc.resultErr
				}))
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest("GET", "/auth/me?user_id=another", nil)
			c.Request = req.WithContext(types.WithPrincipal(req.Context(), tc.principal))
			h.GetCurrentUser(c)
			require.Equal(t, 200, w.Code)
			var response struct {
				Data struct {
					DepartmentName string `json:"department_name"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Equal(t, tc.want, response.Data.DepartmentName)
			require.Equal(t, tc.lookup, calls == 1)
		})
	}
}
