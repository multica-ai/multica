package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/firtalgateway"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	FirtalGatewayProvider    = "firtal-gateway"
	FirtalGatewaySettingsKey = "firtal_gateway"

	defaultFirtalGatewayModel          = "claude-sonnet-4-6"
	defaultFirtalGatewayRuntimeName    = "Firtal Gateway"
	defaultFirtalGatewayPollInterval   = 2 * time.Second
	defaultFirtalGatewaySyncInterval   = 30 * time.Second
	defaultFirtalGatewayTaskTimeout    = 10 * time.Minute
	defaultFirtalGatewayHistoryLimit   = 30
	defaultFirtalGatewayMaxConcurrency = 4

	// firtalGatewayMaxToolRounds caps how many tool-call rounds the model may
	// use before the loop forces a final no-tool gateway call to extract the
	// answer. Three rounds covers the POC chain (get_issue → list_comments →
	// add_comment); the forced final call on top of that lets the model emit
	// its confirmation text. Going higher needs the budget + transparency-log
	// work scheduled for W2/W3.
	firtalGatewayMaxToolRounds = 3
)

// FirtalGatewayRuntimeConfig controls the server-owned HTTPS runtime backed by
// the Data Registry AI Gateway. Server env is a fallback/bootstrap layer;
// workspace owners can override credentials in workspace settings.
type FirtalGatewayRuntimeConfig struct {
	Enabled        bool
	BaseURL        string
	APIKey         string
	Model          string
	MaxTokens      int
	Temperature    *float64
	RuntimeName    string
	PollInterval   time.Duration
	SyncInterval   time.Duration
	TaskTimeout    time.Duration
	HistoryLimit   int
	MaxConcurrency int
	WorkspaceIDs   []pgtype.UUID

	// ToolsEnabledAgentIDs is the per-agent allowlist for the POC tool-loop.
	// Populated from MULTICA_SERVER_FIRTAL_GATEWAY_TOOLS_AGENTS. Agents not in
	// this list see the unchanged chat-only behaviour — no `tools` parameter is
	// sent and no tool_calls are dispatched.
	ToolsEnabledAgentIDs []pgtype.UUID
}

// ToolsEnabledForAgent reports whether the per-agent allowlist includes
// agentID. An empty list means tools are disabled for everyone.
func (c FirtalGatewayRuntimeConfig) ToolsEnabledForAgent(agentID pgtype.UUID) bool {
	if !agentID.Valid {
		return false
	}
	for _, id := range c.ToolsEnabledAgentIDs {
		if id.Valid && id.Bytes == agentID.Bytes {
			return true
		}
	}
	return false
}

type WorkspaceFirtalGatewaySettings struct {
	Enabled    bool   `json:"enabled"`
	GatewayURL string `json:"gateway_url"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
}

type workspaceSettingsEnvelope struct {
	FirtalGateway *WorkspaceFirtalGatewaySettings `json:"firtal_gateway"`
}

func withFirtalGatewayDefaults(cfg FirtalGatewayRuntimeConfig) FirtalGatewayRuntimeConfig {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.Model == "" {
		cfg.Model = defaultFirtalGatewayModel
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	cfg.RuntimeName = strings.TrimSpace(cfg.RuntimeName)
	if cfg.RuntimeName == "" {
		cfg.RuntimeName = defaultFirtalGatewayRuntimeName
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultFirtalGatewayPollInterval
	}
	if cfg.SyncInterval <= 0 {
		cfg.SyncInterval = defaultFirtalGatewaySyncInterval
	}
	if cfg.TaskTimeout <= 0 {
		cfg.TaskTimeout = defaultFirtalGatewayTaskTimeout
	}
	if cfg.HistoryLimit <= 0 {
		cfg.HistoryLimit = defaultFirtalGatewayHistoryLimit
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = defaultFirtalGatewayMaxConcurrency
	}
	return cfg
}

// LoadFirtalGatewayRuntimeConfig reads the server-side gateway runtime config
// from environment variables. If the explicit enable flag is absent, the
// worker is enabled so workspace-level settings can activate the runtime
// without another server deploy.
func LoadFirtalGatewayRuntimeConfig() (FirtalGatewayRuntimeConfig, error) {
	baseURL := strings.TrimRight(firstNonEmptyEnv(
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL",
		"FIRTAL_AE_GATEWAY_URL",
		"FIRTAL_DATA_REGISTRY_URL",
	), "/")
	apiKey := firstNonEmptyEnv(
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY",
		"FIRTAL_AE_GATEWAY_KEY",
		"FIRTAL_DATA_REGISTRY_API_KEY",
	)

	enabled, explicit, err := boolFromEnv(
		"MULTICA_SERVER_FIRTAL_GATEWAY_ENABLED",
		"MULTICA_FIRTAL_GATEWAY_CLOUD_ENABLED",
	)
	if err != nil {
		return FirtalGatewayRuntimeConfig{}, err
	}
	if !explicit {
		enabled = true
	}

	cfg := withFirtalGatewayDefaults(FirtalGatewayRuntimeConfig{
		Enabled:        enabled,
		BaseURL:        baseURL,
		APIKey:         apiKey,
		Model:          firstNonEmptyEnv("FIRTAL_DATA_REGISTRY_AI_MODEL", "FIRTAL_AE_GATEWAY_MODEL"),
		MaxTokens:      positiveIntFromEnv(4096, "FIRTAL_DATA_REGISTRY_AI_MAX_TOKENS", "FIRTAL_AE_GATEWAY_MAX_TOKENS"),
		RuntimeName:    stringFromEnv(defaultFirtalGatewayRuntimeName, "MULTICA_SERVER_FIRTAL_GATEWAY_RUNTIME_NAME"),
		PollInterval:   durationFromEnv(defaultFirtalGatewayPollInterval, "MULTICA_SERVER_FIRTAL_GATEWAY_POLL_INTERVAL"),
		SyncInterval:   durationFromEnv(defaultFirtalGatewaySyncInterval, "MULTICA_SERVER_FIRTAL_GATEWAY_SYNC_INTERVAL"),
		TaskTimeout:    durationFromEnv(defaultFirtalGatewayTaskTimeout, "MULTICA_SERVER_FIRTAL_GATEWAY_TASK_TIMEOUT"),
		HistoryLimit:   positiveIntFromEnv(defaultFirtalGatewayHistoryLimit, "MULTICA_SERVER_FIRTAL_GATEWAY_HISTORY_LIMIT"),
		MaxConcurrency: positiveIntFromEnv(defaultFirtalGatewayMaxConcurrency, "MULTICA_SERVER_FIRTAL_GATEWAY_MAX_CONCURRENCY"),
	})

	if raw := firstNonEmptyEnv("FIRTAL_DATA_REGISTRY_AI_TEMPERATURE", "FIRTAL_AE_GATEWAY_TEMPERATURE"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return FirtalGatewayRuntimeConfig{}, fmt.Errorf("invalid FIRTAL_DATA_REGISTRY_AI_TEMPERATURE: %w", err)
		}
		cfg.Temperature = &parsed
	}

	if raw := strings.TrimSpace(os.Getenv("MULTICA_SERVER_FIRTAL_GATEWAY_WORKSPACE_IDS")); raw != "" {
		for _, part := range splitCSV(raw) {
			id, err := util.ParseUUID(part)
			if err != nil {
				return FirtalGatewayRuntimeConfig{}, fmt.Errorf("invalid MULTICA_SERVER_FIRTAL_GATEWAY_WORKSPACE_IDS value %q: %w", part, err)
			}
			cfg.WorkspaceIDs = append(cfg.WorkspaceIDs, id)
		}
	}

	if raw := strings.TrimSpace(os.Getenv("MULTICA_SERVER_FIRTAL_GATEWAY_TOOLS_AGENTS")); raw != "" {
		for _, part := range splitCSV(raw) {
			id, err := util.ParseUUID(part)
			if err != nil {
				return FirtalGatewayRuntimeConfig{}, fmt.Errorf("invalid MULTICA_SERVER_FIRTAL_GATEWAY_TOOLS_AGENTS value %q: %w", part, err)
			}
			cfg.ToolsEnabledAgentIDs = append(cfg.ToolsEnabledAgentIDs, id)
		}
	}

	if cfg.Enabled && cfg.BaseURL != "" {
		normalizedURL, err := firtalgateway.ValidateBaseURL(cfg.BaseURL)
		if err != nil {
			return FirtalGatewayRuntimeConfig{}, fmt.Errorf("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL %w", err)
		}
		cfg.BaseURL = normalizedURL
	}

	return cfg, nil
}

func FirtalGatewayConfigFromWorkspaceSettings(raw []byte, fallback FirtalGatewayRuntimeConfig) (FirtalGatewayRuntimeConfig, bool, error) {
	cfg := withFirtalGatewayDefaults(fallback)
	if !cfg.Enabled {
		return cfg, false, nil
	}

	var envelope workspaceSettingsEnvelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return cfg, false, fmt.Errorf("parse workspace gateway settings: %w", err)
		}
	}

	if envelope.FirtalGateway != nil {
		settings := envelope.FirtalGateway
		if !settings.Enabled {
			return cfg, false, nil
		}
		if gatewayURL := strings.TrimSpace(settings.GatewayURL); gatewayURL != "" {
			cfg.BaseURL = strings.TrimRight(gatewayURL, "/")
		}
		if key := strings.TrimSpace(settings.APIKey); key != "" {
			cfg.APIKey = key
		}
		if model := strings.TrimSpace(settings.Model); model != "" {
			cfg.Model = model
		}
		cfg = withFirtalGatewayDefaults(cfg)
	}

	if cfg.BaseURL == "" || cfg.APIKey == "" {
		return cfg, false, nil
	}
	normalizedURL, err := firtalgateway.ValidateBaseURL(cfg.BaseURL)
	if err != nil {
		return cfg, false, fmt.Errorf("workspace firtal gateway URL %w", err)
	}
	cfg.BaseURL = normalizedURL

	return cfg, true, nil
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func stringFromEnv(def string, keys ...string) string {
	if v := firstNonEmptyEnv(keys...); v != "" {
		return v
	}
	return def
}

func positiveIntFromEnv(def int, keys ...string) int {
	raw := firstNonEmptyEnv(keys...)
	if raw == "" {
		return def
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return def
	}
	return parsed
}

func durationFromEnv(def time.Duration, keys ...string) time.Duration {
	raw := firstNonEmptyEnv(keys...)
	if raw == "" {
		return def
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return def
	}
	return parsed
}

func boolFromEnv(keys ...string) (value bool, explicit bool, err error) {
	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		switch strings.ToLower(raw) {
		case "true", "1", "yes", "on":
			return true, true, nil
		case "false", "0", "no", "off":
			return false, true, nil
		default:
			return false, true, fmt.Errorf("invalid %s value %q", key, raw)
		}
	}
	return false, false, nil
}
