// CEREBRO-PATCH(capabilities): persona integration changes.
// CEREBRO-PATCH(capabilities-shim): delegate to internal/cerebro/capabilities (JEH-978/JEH-1873).
package daemon

import (
	"github.com/multica-ai/multica/server/internal/cerebro/capabilities"
)

// providerCapabilities returns the capability snapshot the daemon reports
// for a given provider type at register time. It is a thin shim over the
// canonical registry in internal/cerebro/capabilities — see that package
// for the per-provider declarations and the JEH-1873 design notes.
//
// The legacy map[string]any return type is preserved so /api/runtimes/
// register and persona's actor-attrs push read the same wire shape they
// always have. New fields (skills, hooks, secret_bindings, cli_binaries,
// discovery_method) are added by capabilities.AsMap when populated.
//
// Unknown providers receive an empty-but-typed payload with
// discovery_method="unmapped", so persona's UI shows the runtime as
// "uncatalogued" instead of fabricating tool names.
func providerCapabilities(providerType string) map[string]any {
	return capabilities.AsMap(capabilities.For(providerType))
}
