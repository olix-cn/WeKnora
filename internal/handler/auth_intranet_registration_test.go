package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIntranetRegistrationAllowsNoEmailAndSingleCharacterPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, invited := range []bool{false, true} {
		users := &invitedRegistrationUserService{}
		h := &AuthHandler{userService: users, tenantService: &invitedRegistrationTenantService{}, invitationSvc: &invitedRegistrationInvitationService{}}
		r := gin.New()
		r.Use(errorCapture())
		r.POST("/register", h.Register)
		r.POST("/invite", h.RegisterByInvite)
		path, body := "/register", `{"username":"alice","password":"1"}`
		if invited {
			path, body = "/invite", `{"token":"valid","username":"alice","password":"密"}`
		}
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	}
}

func TestAdminCreateSupportsNoEmailAndSimplePassword(t *testing.T) {
	users := &createUserService{createdUser: &types.User{ID: "intranet", Username: "alice"}}
	h := &SystemHandler{userSvc: users}
	w := performCreateSystemUser(t, createSystemUserRouter(h, "admin"), map[string]string{
		"username": "alice", "password": "1",
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Empty(t, users.gotReq.Email)
	require.Equal(t, "1", *users.gotReq.Password)
}
