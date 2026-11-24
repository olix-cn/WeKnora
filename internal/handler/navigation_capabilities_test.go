package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type navigationSettingService struct {
	interfaces.SystemSettingService
	values map[string]bool
}

func (s *navigationSettingService) GetBool(_ context.Context, key, _ string, fallback bool) bool {
	if value, ok := s.values[key]; ok {
		return value
	}
	return fallback
}

func TestNavigationCapabilitiesDefaultOffAndFollowLiveSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &navigationSettingService{values: map[string]bool{}}
	h := &SystemHandler{systemSettingSvc: settings}
	h.BindDeploymentCapabilities(BuildDeploymentCapabilities("standard", DeploymentFeatureAvailability{}))
	read := func() DeploymentCapabilitiesData {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("GET", "/capabilities", nil)
		h.GetDeploymentCapabilities(ctx)
		var body struct {
			Data DeploymentCapabilitiesData `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
		return body.Data
	}
	for _, key := range []string{"navigation.automations", "navigation.toolbox", "navigation.workspace_switcher"} {
		require.False(t, read().Capabilities[key].Supported)
	}
	settings.values["ui.navigation.automations_visible"] = true
	require.True(t, read().Capabilities["navigation.automations"].Supported)
	require.False(t, read().Capabilities["navigation.toolbox"].Supported)
	settings.values["ui.navigation.toolbox_visible"] = true
	require.True(t, read().Capabilities["navigation.toolbox"].Supported)
	settings.values["ui.navigation.workspace_switcher_visible"] = true
	require.True(t, read().Capabilities["navigation.workspace_switcher"].Supported)
	settings.values["ui.navigation.workspace_switcher_visible"] = false
	require.False(t, read().Capabilities["navigation.workspace_switcher"].Supported)
	settings.values["ui.navigation.automations_visible"] = false
	require.False(t, read().Capabilities["navigation.automations"].Supported)
	// The shared startup snapshot must never be mutated by per-request overlays.
	require.False(t, h.deploymentCapabilities.Capabilities["navigation.toolbox"].Supported)
}

func TestNavigationCapabilitiesWithoutSettingServiceStayHidden(t *testing.T) {
	h := &SystemHandler{}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/capabilities", nil)
	h.GetDeploymentCapabilities(ctx)
	var body struct {
		Data DeploymentCapabilitiesData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.False(t, body.Data.Capabilities["navigation.toolbox"].Supported)
	require.False(t, body.Data.Capabilities["navigation.automations"].Supported)
	require.False(t, body.Data.Capabilities["navigation.workspace_switcher"].Supported)
}
