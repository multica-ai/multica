// Package plugintest exposes production bundled Plugin releases to lifecycle
// and execution-chain tests. Production code never depends on this package.
package plugintest

import (
	"fmt"

	"github.com/multica-ai/multica/server/internal/pluginbundled"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

const (
	ReviewReadinessPluginKey = "ai.multica.software-delivery"
	ReviewReadinessVersion   = "1.0.0"
)

// ReviewReadinessRelease returns the same signed, validated artifact shipped
// by the production bundled catalog.
func ReviewReadinessRelease() (plugincontract.ValidatedRelease, error) {
	entry, ok := pluginbundled.Load().Find(ReviewReadinessPluginKey, ReviewReadinessVersion)
	if !ok {
		return plugincontract.ValidatedRelease{}, fmt.Errorf("bundled review-readiness release is unavailable")
	}
	return entry.Release, nil
}
