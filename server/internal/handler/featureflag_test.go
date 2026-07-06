package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func withComposioMCPAppsFlag(t *testing.T, h *Handler, enabled bool) {
	t.Helper()
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.ComposioMCPApps, featureflag.Rule{Default: enabled})
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
