package handler

// CEREBRO-PATCH(firtal-registry-credentials): shared workspace credential loader for FDR admin proxy.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

type fdrRegistrySettings struct {
	DataRegistry *struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	} `json:"data_registry"`
	FirtalGateway *struct {
		GatewayURL string `json:"gateway_url"`
		APIKey     string `json:"api_key"`
	} `json:"firtal_gateway"`
}

func (h *Handler) loadFirtalRegistryCredentials(ctx context.Context, workspaceID pgtype.UUID) (string, string, error) {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return "", "", fmt.Errorf("load workspace: %w", err)
	}
	var envelope fdrRegistrySettings
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &envelope)
	}
	var baseURL, apiKey string
	if envelope.DataRegistry != nil {
		baseURL = strings.TrimRight(envelope.DataRegistry.BaseURL, "/")
		apiKey = envelope.DataRegistry.APIKey
	}
	if baseURL == "" && envelope.FirtalGateway != nil {
		baseURL = strings.TrimRight(envelope.FirtalGateway.GatewayURL, "/")
		apiKey = envelope.FirtalGateway.APIKey
	}
	if baseURL == "" || apiKey == "" {
		return "", "", fmt.Errorf("data registry URL not configured in workspace settings")
	}
	return baseURL, apiKey, nil
}
