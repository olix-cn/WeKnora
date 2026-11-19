package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type memberPolicyHandlerService struct {
	err        error
	writes     int
	update     types.KBAccessPolicyUpdate
	searches   int
	recoveries int
}

func (s *memberPolicyHandlerService) Get(context.Context, string) (*types.KBAccessPolicyView, error) {
	return &types.KBAccessPolicyView{Mode: types.KBAccessInherit, Members: []types.KBMemberGrantInput{}}, s.err
}

func (s *memberPolicyHandlerService) Replace(
	_ context.Context, _ string, update types.KBAccessPolicyUpdate,
) (*types.KBAccessPolicyView, error) {
	s.writes++
	s.update = update
	return &types.KBAccessPolicyView{
		Mode: update.Mode, Version: update.ExpectedVersion + 1, Members: update.Members,
	}, s.err
}

func memberPolicyTestRouter(s *memberPolicyHandlerService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewKnowledgeBaseMemberHandler(s)
	r.GET("/kbs/:id/access", h.Get)
	r.PUT("/kbs/:id/access", h.Replace)
	r.GET("/kbs/:id/access/candidates", h.MemberCandidates)
	r.POST("/transfers/recover", h.RecoverTransfer)
	return r
}

func (s *memberPolicyHandlerService) RecoverFailedTransfer(context.Context, types.KBTransferRecovery) error {
	s.recoveries++
	return s.err
}

func TestRecoveryHandlerRejectsUnknownForceAndTrailingPayload(t *testing.T) {
	for _, body := range []string{`{"force":true}`, `{} {}`, strings.Repeat(" ", 4097) + `{}`} {
		s := &memberPolicyHandlerService{}
		w := httptest.NewRecorder()
		memberPolicyTestRouter(s).ServeHTTP(w,
			httptest.NewRequest(http.MethodPost, "/transfers/recover", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Zero(t, s.recoveries)
	}
	for _, tt := range []struct {
		err    error
		status int
	}{
		{access.ErrForbidden, http.StatusForbidden},
		{repository.ErrKBAccessTransferBusy, http.StatusConflict},
		{nil, http.StatusOK},
	} {
		s := &memberPolicyHandlerService{err: tt.err}
		w := httptest.NewRecorder()
		memberPolicyTestRouter(s).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/transfers/recover",
			strings.NewReader(`{"fence_id":"attempt","task_id":"task","source_id":"source","target_id":"target",
			"worker_stopped":true,"outcome_reconciled":true,"reason":"synthetic reviewed outcome"}`)))
		require.Equal(t, tt.status, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
}

func (s *memberPolicyHandlerService) MemberCandidates(
	context.Context, string, string, int, int,
) ([]types.KBAccessMemberCandidate, int64, error) {
	s.searches++
	return []types.KBAccessMemberCandidate{{UserID: "user", Username: "User", Role: types.TenantRoleViewer}}, 1, s.err
}

func TestKBMemberPolicyCandidateHandlerBoundsQueriesAndRequiresAuthorization(t *testing.T) {
	for _, query := range []string{
		"page=0", "page=-1", "page_size=101", "page=invalid", "q=" + strings.Repeat("x", 201),
	} {
		s := &memberPolicyHandlerService{}
		w := httptest.NewRecorder()
		memberPolicyTestRouter(s).ServeHTTP(w,
			httptest.NewRequest(http.MethodGet, "/kbs/kb/access/candidates?"+query, nil))
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Zero(t, s.searches)
	}
	s := &memberPolicyHandlerService{err: access.ErrForbidden}
	w := httptest.NewRecorder()
	memberPolicyTestRouter(s).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/kbs/kb/access/candidates", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.NotContains(t, w.Body.String(), "username")
	s.err = nil
	w = httptest.NewRecorder()
	memberPolicyTestRouter(s).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/kbs/kb/access/candidates", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "username")
	require.NotContains(t, w.Body.String(), "email")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestKBMemberPolicyHandlerRejectsUnboundedOrAmbiguousInput(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `{"mode":"inherit","members":[]}`,
		`{"mode":"inherit","expected_version":0}`, `{"mode":"inherit","expected_version":0,"members":null}`,
		`{"mode":"inherit","expected_version":0,"members":[],"actor_id":"owner"}`,
		`{"mode":"inherit","expected_version":0,"members":[{"user_id":"user","permission":"viewer","role":"owner"}]}`,
		`{"mode":"inherit","expected_version":0,"members":[]} {}`,
		`{"mode":"inherit","expected_version":-1,"members":[]}`,
		`{"mode":"unknown","expected_version":0,"members":[]}`,
		`{"mode":"inherit","expected_version":0,"members":[{"user_id":"user","permission":"owner"}]}`,
		strings.Repeat(" ", 128<<10) + `{}`,
	} {
		s := &memberPolicyHandlerService{}
		w := httptest.NewRecorder()
		memberPolicyTestRouter(s).ServeHTTP(w,
			httptest.NewRequest(http.MethodPut, "/kbs/kb/access", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, w.Code, body[:min(len(body), 150)])
		require.Zero(t, s.writes)
	}
}

func TestKBMemberPolicyHandlerMapsConflictsAndDoesNotLeakDatabaseErrors(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		code   string
	}{
		{nil, http.StatusOK, ""},
		{access.ErrForbidden, http.StatusForbidden, "policy_forbidden"},
		{repository.ErrKBAccessConflict, http.StatusConflict, "policy_conflict"},
		{repository.ErrKBAccessShared, http.StatusConflict, "policy_shared"},
		{repository.ErrKBAccessMember, http.StatusBadRequest, "policy_invalid"},
		{errors.New("database private connection detail"), http.StatusServiceUnavailable, "policy_unavailable"},
	} {
		s := &memberPolicyHandlerService{err: tt.err}
		w := httptest.NewRecorder()
		memberPolicyTestRouter(s).ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/kbs/kb/access",
			strings.NewReader(`{"mode":"restricted","expected_version":2,"members":[]}`)))
		require.Equal(t, tt.status, w.Code)
		require.Contains(t, w.Body.String(), tt.code)
		require.NotContains(t, w.Body.String(), "private connection")
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.EqualValues(t, 2, s.update.ExpectedVersion)
	}
}
