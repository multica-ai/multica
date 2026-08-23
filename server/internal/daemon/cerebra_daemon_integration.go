package daemon

import (
	"context"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebra"
)

// detectMCPUsage inspects the task's runtime MCP overlay and connected apps
// to decide whether the task is expected to call MCP/tool chains. Used to
// populate TaskMeta.WillUseMCPTools before routing.
func detectMCPUsage(runtimeMCPOverlay []byte, connectedApps []string) bool {
	if len(runtimeMCPOverlay) > 2 { // non-empty JSON object
		return true
	}
	for _, app := range connectedApps {
		if strings.TrimSpace(app) != "" {
			return true
		}
	}
	return false
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
