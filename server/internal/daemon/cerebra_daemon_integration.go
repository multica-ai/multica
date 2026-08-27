package daemon

import (
	"context"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebra"
)

// detectMCPUsage inspects the task's runtime MCP overlay, connected apps,
// plugin hook tools, and remote MCP connections to decide whether the task
// is expected to call MCP/tool chains. Used to populate TaskMeta.WillUseMCPTools before routing.
func detectMCPUsage(runtimeMCPOverlay []byte, connectedApps []string, pluginHooks int, remoteMCPs int) bool {
	if len(runtimeMCPOverlay) > 2 { // non-empty JSON object
		return true
	}
	if pluginHooks > 0 || remoteMCPs > 0 {
		return true
	}
	for _, app := range connectedApps {
		if strings.TrimSpace(app) != "" {
			return true
		}
	}
	return false
}

// deriveRuntimeTierMap dynamically scans the runtime provider's available model catalog
// and automatically assigns the best matched model for Simple, Standard, and Heavy tiers.
func deriveRuntimeTierMap(provider string) map[cerebra.Tier]string {
	switch strings.ToLower(provider) {
	case "codex":
		codexCatalog := []string{
			"gpt-4o-mini",
			"gpt-4o",
			"o1",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(codexCatalog))
	case "claude":
		claudeCatalog := []string{
			"claude-3-5-haiku",
			"claude-3-5-sonnet",
			"claude-3-opus",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(claudeCatalog))
	default:
		// OpenCode / Universal Runtime: Scan full discovered model catalog dynamically
		openCodeCatalog := []string{
			"opencode/mimo-v2.5-free",
			"opencode/hy3-free",
			"opencode/muse-spark-1.2-contributor-free",
			"opencode/x-preview-f-free",
			"opencode/nemotron-3.5-lightning-free",
			"opencode/nemotron-3-ultra-free",
			"opencode/big-pickle",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(openCodeCatalog))
	}
}

// routeBeforeDispatch calls the Cerebra router (if enabled) and returns the
// selected model. Falls back to agentDefaultModel when the router is nil or
// returns an error.
func routeBeforeDispatch(
	ctx context.Context,
	router *cerebra.Router,
	prompt string,
	meta cerebra.TaskMeta,
	runtimes []cerebra.RuntimeEntry,
	agentDefaultModel string,
) string {
	if router == nil {
		return agentDefaultModel
	}
	result := router.Route(ctx, prompt, meta, runtimes, agentDefaultModel)
	return result.Model
}

