package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

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

type ollamaTagsResponse struct {
	Models []struct {
		Name         string   `json:"name"`
		Capabilities []string `json:"capabilities"`
	} `json:"models"`
}

func hasToolCapability(caps []string) bool {
	if len(caps) == 0 {
		return true // If capabilities not reported, assume compatible
	}
	for _, c := range caps {
		if strings.ToLower(c) == "tools" {
			return true
		}
	}
	return false
}

func autoSyncOpenCodeOllama(models []string) {
	home, err := os.UserHomeDir()
	if err != nil || len(models) == 0 {
		return
	}
	configDir := home + "/.config/opencode"
	_ = os.MkdirAll(configDir, 0755)
	configFile := configDir + "/opencode.jsonc"

	modelsMap := make(map[string]map[string]string)
	for _, m := range models {
		cleanName := strings.TrimPrefix(m, "ollama/")
		modelsMap[cleanName] = map[string]string{"name": cleanName}
	}

	cfg := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			"ollama": map[string]any{
				"npm": "@ai-sdk/openai-compatible",
				"options": map[string]string{
					"baseURL": "http://127.0.0.1:11434/v1",
					"apiKey":  "ollama",
				},
				"models": modelsMap,
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err == nil {
		_ = os.WriteFile(configFile, data, 0644)
	}
}

func fetchLocalOllamaModels(ctx context.Context) []string {
	reqCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://127.0.0.1:11434/api/tags", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var tagResp ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagResp); err != nil {
		return nil
	}
	var res []string
	for _, m := range tagResp.Models {
		if strings.TrimSpace(m.Name) != "" && hasToolCapability(m.Capabilities) {
			res = append(res, "ollama/"+strings.TrimSpace(m.Name))
		}
	}
	if len(res) > 0 {
		autoSyncOpenCodeOllama(res)
	}
	return res
}

// deriveDynamicRuntimeTierMap automatically probes the local runtime machine's
// installed model catalog (using agent.ListModels and local Ollama APIs) and dynamically builds a
// machine-specific TierMap (Simple, Standard, Heavy).
// If dynamic discovery returns models, it derives the tiers directly from the live models.
// If discovery returns empty or errors, it falls back to known provider defaults.
func deriveDynamicRuntimeTierMap(ctx context.Context, provider string, runtimeCmd agent.Command) map[cerebra.Tier]string {
	var modelIDs []string

	// 1. Proactively query local Ollama engine (instant zero-config auto-discovery for all locally downloaded models)
	ollamaModels := fetchLocalOllamaModels(ctx)
	modelIDs = append(modelIDs, ollamaModels...)

	// 2. Discover provider models via agent.ListModels
	if listModels != nil {
		cat, err := listModels(ctx, provider, runtimeCmd)
		if err == nil && len(cat.Models) > 0 {
			for _, m := range cat.Models {
				if m.ID != "" {
					modelIDs = append(modelIDs, m.ID)
				}
			}
		}
	}

	if len(modelIDs) > 0 {
		tierMap := cerebra.BuildTierMapFromCatalog(modelIDs)
		if len(tierMap) > 0 {
			return map[cerebra.Tier]string(tierMap)
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

