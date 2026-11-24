package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestNavigationSettingsAreDefaultOffAndBooleanOnly(t *testing.T) {
	for _, key := range []string{"ui.navigation.automations_visible", "ui.navigation.toolbox_visible", "ui.navigation.workspace_switcher_visible"} {
		t.Run(key, func(t *testing.T) {
			spec, ok := registry[key]
			require.True(t, ok)
			require.Equal(t, false, spec.Default)
			require.Empty(t, spec.EnvName)
			require.False(t, spec.RequiresRestart)
			svc := &systemSettingService{repo: &registrationSettingRepo{}, cache: make(map[string]*types.SystemSetting)}
			ctx := context.WithValue(context.Background(), types.UserIDContextKey, "system-admin")
			require.False(t, svc.GetBool(ctx, key, "", false))
			_, err := svc.Update(ctx, key, "true")
			require.Error(t, err)
			_, err = svc.Update(ctx, key, true)
			require.NoError(t, err)
			require.True(t, svc.GetBool(ctx, key, "", false))
			_, err = svc.Update(ctx, key, false)
			require.NoError(t, err)
			require.False(t, svc.GetBool(ctx, key, "", false))
		})
	}
}
