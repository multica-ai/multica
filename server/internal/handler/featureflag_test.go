package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestRequireDocsV1(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		h := &Handler{}
		withFeatureFlag(t, h, featureflags.DocsV1, enabled)
		reached := false
		w := httptest.NewRecorder()
		h.RequireDocsV1(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			reached = true
		})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/docs", nil))
		if reached != enabled {
			t.Fatalf("docs_v1=%v: next handler reached = %v", enabled, reached)
		}
		if !enabled && (w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "docs_disabled")) {
			t.Fatalf("docs_v1=false: want 403 docs_disabled, got %d %s", w.Code, w.Body.String())
		}
	}
	// No flag service at all (tests, misconfigured boot) must fail closed.
	if (&Handler{}).docsV1Enabled(context.Background()) {
		t.Fatal("docs_v1 must default to off")
	}
}
