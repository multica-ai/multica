package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func discoverOmniRouteModels(ctx context.Context) ([]Model, error) {
	cfg, err := resolveOmniRouteConfig(map[string]string{omniRouteBaseURLKey: strings.TrimSpace(getenv(omniRouteBaseURLKey)), omniRouteAPIKeyKey: strings.TrimSpace(getenv(omniRouteAPIKeyKey))})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("omniroute models: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("omniroute models: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("omniroute models: upstream HTTP %d: %s", resp.StatusCode, sanitizedHTTPError(resp.Body))
	}
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("omniroute models: decode response: %w", err)
	}
	models := make([]Model, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.ID == "" {
			continue
		}
		provider := item.OwnedBy
		if provider == "" && strings.Contains(item.ID, "/") {
			provider = strings.SplitN(item.ID, "/", 2)[0]
		}
		models = append(models, Model{ID: item.ID, Label: item.ID, Provider: provider})
	}
	return models, nil
}

var getenv = func(key string) string { return os.Getenv(key) }
