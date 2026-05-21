package daemon

import (
	"github.com/multica-ai/multica/server/internal/cerebro/capabilities"
)

func providerCapabilities(providerType string) map[string]any {
	return capabilities.LegacyProviderMap(providerType) // CEREBRO-PATCH(capabilities-shim): keep upstream daemon hook tiny; JEH-1916.
}
