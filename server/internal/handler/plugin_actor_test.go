package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestPluginConsentHandlersRejectMachineCredentials(t *testing.T) {
	// No dependencies: rejection must precede parsing, database access, local
	// bundle reads, and outbound tool discovery, even without router middleware.
	flags := featureflag.NewStaticProvider()
	flags.Set(featureflags.PluginsV1, featureflag.Rule{Default: true})
	h := &Handler{FeatureFlags: featureflag.NewService(flags)}
	for name, handle := range map[string]http.HandlerFunc{
		"publish":       h.PublishPluginPackage,
		"publish local": h.PublishLocalPluginPackage,
		"install":       h.InstallPlugin,
		"rotate token":  h.RotatePluginToken,
		"approve tools": h.ApprovePluginMCPTools,
	} {
		for _, source := range []string{"task_token", "cloud_pat"} {
			t.Run(name+"/"+source, func(t *testing.T) {
				req := testutil.JSONRequest(http.MethodPost, "/plugins", nil)
				req.Header.Set("X-Actor-Source", source)
				testutil.Call(t, handle, req).Want(http.StatusForbidden)
			})
		}
	}
}
