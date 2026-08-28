package daemon

import (
	"context"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebra"
	"github.com/multica-ai/multica/server/pkg/agent"
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

// deriveDynamicRuntimeTierMap automatically probes the local runtime machine's
// installed model catalog (using agent.ListModels) and dynamically builds a
// machine-specific TierMap (Simple, Standard, Heavy).
// If dynamic discovery returns models, it derives the tiers directly from the live models.
// If discovery returns empty or errors, it falls back to known provider defaults.
func deriveDynamicRuntimeTierMap(ctx context.Context, provider string, runtimeCmd agent.Command) map[cerebra.Tier]string {
	if listModels != nil {
		cat, err := listModels(ctx, provider, runtimeCmd)
		if err == nil && len(cat.Models) > 0 {
			var modelIDs []string
			for _, m := range cat.Models {
				if m.ID != "" {
					modelIDs = append(modelIDs, m.ID)
				}
			}
			if len(modelIDs) > 0 {
				tierMap := cerebra.BuildTierMapFromCatalog(modelIDs)
				if len(tierMap) > 0 {
					return map[cerebra.Tier]string(tierMap)
				}
			}
		}
	}
	return deriveRuntimeTierMap(provider)
}

// deriveRuntimeTierMap provides static fallback catalogs for known providers
// when dynamic CLI model discovery is not supported or returns empty.
func deriveRuntimeTierMap(provider string) map[cerebra.Tier]string {
	switch strings.ToLower(provider) {
	case "codex", "openai":
		codexCatalog := []string{
			"gpt-4o-mini",
			"gpt-4o",
			"o1",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(codexCatalog))
	case "claude", "anthropic":
		claudeCatalog := []string{
			"claude-3-5-haiku",
			"claude-3-5-sonnet",
			"claude-3-opus",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(claudeCatalog))
	case "gemini", "google":
		geminiCatalog := []string{
			"gemini-2.5-flash",
			"gemini-2.5-pro",
			"gemini-ultra",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(geminiCatalog))
	case "ollama", "qwen", "llama":
		localCatalog := []string{
			"llama3.2:3b",
			"qwen2.5-coder:7b",
			"deepseek-r1:14b",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(localCatalog))
	case "kimi":
		kimiCatalog := []string{
			"moonshot-v1-8k",
			"moonshot-v1-32k",
			"moonshot-v1-128k",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(kimiCatalog))
	case "hermes":
		hermesCatalog := []string{
			"hermes-3-llama-3.1-8b",
			"hermes-3-llama-3.1-70b",
			"hermes-3-llama-3.1-405b",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(hermesCatalog))
	default:
		// OpenCode / Universal Runtime default catalog
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

