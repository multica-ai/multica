package firtalgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const Provider = "firtal-gateway"

type ModelListResponse struct {
	ID        string    `json:"id"`
	RuntimeID string    `json:"runtime_id"`
	Status    string    `json:"status"`
	Models    []Model   `json:"models,omitempty"`
	Supported bool      `json:"supported"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Model struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider,omitempty"`
	Default  bool   `json:"default,omitempty"`
}

type ModelDiscoveryConfig struct {
	BaseURL string
	APIKey  string
}

type workspaceSettingsEnvelope struct {
	FirtalGateway *struct {
		Enabled    bool   `json:"enabled"`
		GatewayURL string `json:"gateway_url"`
		APIKey     string `json:"api_key"`
	} `json:"firtal_gateway"`
}

func ResolveModelList(ctx context.Context, requestID, runtimeID string, rawSettings []byte, workspaceErr error) ModelListResponse {
	now := time.Now()
	resp := ModelListResponse{
		ID:        requestID,
		RuntimeID: runtimeID,
		Status:    "running",
		Supported: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	fail := func(message string) ModelListResponse {
		resp.Status = "failed"
		resp.Error = message
		resp.UpdatedAt = time.Now()
		return resp
	}

	if workspaceErr != nil {
		return fail("failed to load workspace gateway settings")
	}
	cfg, ok, err := DiscoveryConfig(rawSettings)
	if err != nil {
		return fail(err.Error())
	}
	if !ok {
		return fail("firtal gateway runtime is not configured for this workspace")
	}

	models, err := agent.DiscoverFirtalGatewayModels(ctx, map[string]string{
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL": cfg.BaseURL,
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY": cfg.APIKey,
	})
	if err != nil {
		return fail(err.Error())
	}

	resp.Status = "completed"
	resp.Models = make([]Model, 0, len(models))
	for _, model := range models {
		resp.Models = append(resp.Models, Model{
			ID:       model.ID,
			Label:    model.Label,
			Provider: model.Provider,
			Default:  model.Default,
		})
	}
	resp.UpdatedAt = time.Now()
	return resp
}

func DiscoveryConfig(rawSettings []byte) (ModelDiscoveryConfig, bool, error) {
	cfg := ModelDiscoveryConfig{
		BaseURL: strings.TrimRight(firstNonEmptyOS("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", "FIRTAL_AE_GATEWAY_URL", "FIRTAL_DATA_REGISTRY_URL"), "/"),
		APIKey:  firstNonEmptyOS("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "FIRTAL_AE_GATEWAY_KEY", "FIRTAL_DATA_REGISTRY_API_KEY"),
	}

	var envelope workspaceSettingsEnvelope
	if len(rawSettings) > 0 {
		if err := json.Unmarshal(rawSettings, &envelope); err != nil {
			return cfg, false, fmt.Errorf("parse workspace gateway settings: %w", err)
		}
	}
	if envelope.FirtalGateway != nil {
		if !envelope.FirtalGateway.Enabled {
			return cfg, false, nil
		}
		if gatewayURL := strings.TrimSpace(envelope.FirtalGateway.GatewayURL); gatewayURL != "" {
			cfg.BaseURL = strings.TrimRight(gatewayURL, "/")
		}
		if key := strings.TrimSpace(envelope.FirtalGateway.APIKey); key != "" {
			cfg.APIKey = key
		}
	}
	if cfg.BaseURL == "" || cfg.APIKey == "" {
		return cfg, false, nil
	}
	normalizedURL, err := ValidateBaseURL(cfg.BaseURL)
	if err != nil {
		return cfg, false, fmt.Errorf("workspace firtal gateway URL %w", err)
	}
	cfg.BaseURL = normalizedURL
	return cfg, true, nil
}

func firstNonEmptyOS(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}
