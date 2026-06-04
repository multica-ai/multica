package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// PluginInstall mirrors the install metadata from the external plugin API.
type PluginInstall struct {
	Method             string `json:"method"`
	Marketplace        string `json:"marketplace"`
	PluginName         string `json:"plugin_name"`
	MarketplaceName    string `json:"marketplace_name"`
	MarketplaceRepo    string `json:"marketplace_repo"`
	MarketplaceVerified bool  `json:"marketplace_verified"`
}

// PluginInfo is the subset of plugin data passed to the daemon for task execution.
type PluginInfo struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Install PluginInstall `json:"install"`
}

// builtinPluginItem mirrors a single item in the external plugin API list response.
type builtinPluginItem struct {
	ID       string                    `json:"id"`
	Name     string                    `json:"name"`
	Content  string                    `json:"content"`
	Metadata builtinPluginItemMetadata `json:"metadata"`
}

type builtinPluginItemMetadata struct {
	Install PluginInstall `json:"install"`
}

// builtinPluginListResponse mirrors the external plugin API list response.
type builtinPluginListResponse struct {
	Items   []builtinPluginItem `json:"items"`
	HasMore bool               `json:"hasMore"`
	Page    int                `json:"page"`
}

// pluginResult bundles plugin metadata and content from a single external API call.
type pluginResult struct {
	Info    *PluginInfo
	Content string
}

// fetchPluginData fetches the plugin's metadata and content from the external
// catalog API in a single request. Returns nil when the API is unreachable,
// the plugin is not found, or the base URL is unconfigured — best-effort and
// must never block task startup.
func fetchPluginData(ctx context.Context, baseURL string, pluginID string) *pluginResult {
	list, ok := fetchPluginList(ctx, baseURL)
	if !ok {
		return nil
	}
	for _, p := range list.Items {
		if p.ID == pluginID {
			return &pluginResult{
				Info: &PluginInfo{
					ID:      p.ID,
					Name:    p.Name,
					Install: p.Metadata.Install,
				},
				Content: p.Content,
			}
		}
	}
	slog.Debug("plugin: plugin not found in catalog", "plugin_id", pluginID)
	return nil
}

func fetchPluginList(ctx context.Context, baseURL string) (*builtinPluginListResponse, bool) {
	if baseURL == "" {
		return nil, false
	}

	url := fmt.Sprintf("%s/api/plugins/builtin?page=1&pageSize=100", baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		slog.Warn("plugin: failed to build request", "error", err)
		return nil, false
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("plugin: API unreachable", "url", url, "error", err)
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("plugin: API returned non-200", "status", resp.StatusCode)
		return nil, false
	}

	var list builtinPluginListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		slog.Warn("plugin: failed to decode response", "error", err)
		return nil, false
	}

	return &list, true
}
