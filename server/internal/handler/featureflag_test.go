package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func withComposioMCPAppsFlag(t *testing.T, h *Handler, enabled bool) {
	withFeatureFlag(t, h, featureflags.ComposioMCPApps, enabled)
}

func withPluginsV1Flag(t *testing.T, h *Handler, enabled bool) {
	withFeatureFlag(t, h, featureflags.PluginsV1, enabled)
}

func withPrivatePluginsV1Flag(t *testing.T, h *Handler, pluginsEnabled, privateEnabled bool) {
	t.Helper()
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.PluginsV1, featureflag.Rule{Default: pluginsEnabled})
	provider.Set(featureflags.PrivatePluginsV1, featureflag.Rule{Default: privateEnabled})
	flags := featureflag.NewService(provider)
	original := h.FeatureFlags
	h.FeatureFlags = flags
	t.Cleanup(func() { h.FeatureFlags = original })
}

func withFeatureFlag(t *testing.T, h *Handler, key string, enabled bool) {
	t.Helper()
	provider := featureflag.NewStaticProvider()
	provider.Set(key, featureflag.Rule{Default: enabled})
	flags := featureflag.NewService(provider)

	origHandlerFlags := h.FeatureFlags
	h.FeatureFlags = flags
	var origTaskFlags *featureflag.Service
	if h.TaskService != nil {
		origTaskFlags = h.TaskService.FeatureFlags
		h.TaskService.FeatureFlags = flags
	}
	t.Cleanup(func() {
		h.FeatureFlags = origHandlerFlags
		if h.TaskService != nil {
			h.TaskService.FeatureFlags = origTaskFlags
		}
	})
}
