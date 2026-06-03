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
	// answer. 8 rounds allows complex multi-step agent work; the forced final
	// call lets the model emit its confirmation text. Overridable via
	// MULTICA_SERVER_FIRTAL_GATEWAY_MAX_TOOL_ITERATIONS env var.
	firtalGatewayMaxToolRounds = 8
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

	// ToolsEnabledAgentIDs is loaded from MULTICA_SERVER_FIRTAL_GATEWAY_TOOLS_AGENTS
	// for backward compatibility only. Tool-loop gating uses the runtime tools
	// cascade (see FirtalGatewayExecutor.agentHasCallableTools); this list is
	// no longer consulted at execution time (FIR-2761).
	ToolsEnabledAgentIDs []pgtype.UUID

	// MaxToolRounds overrides firtalGatewayMaxToolRounds when > 0. Loaded from
	// MULTICA_SERVER_FIRTAL_GATEWAY_MAX_TOOL_ITERATIONS.
	MaxToolRounds int

	// CheapModel is the cheaper model the model_routing cost saving (FIR-2325)
	// routes to when it is turned "on" for a workspace. Empty means no routing
	// target is configured, so routing is a no-op and is not measured — the
	// runtime never invents an alternative model to compare against.
	CheapModel string

	// CostSavingHoldoutPct is the share (0-100) of "on" model_routing runs kept
	// on the requested model as a holdout control arm (FIR-2325 phase 5). nil
	// means unconfigured, which resolves to defaultCostSavingHoldoutPct via
	// costSavingHoldoutPct(). Loaded from
	// MULTICA_SERVER_FIRTAL_GATEWAY_COST_HOLDOUT_PCT.
	CostSavingHoldoutPct *int
}

// costSavingHoldoutPct resolves the configured holdout share, applying the
// default when unset and clamping to [0,100].
func (c FirtalGatewayRuntimeConfig) costSavingHoldoutPct() int {
	if c.CostSavingHoldoutPct == nil {
		return defaultCostSavingHoldoutPct
	}
	pct := *c.CostSavingHoldoutPct
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// ToolsEnabledForAgent reports whether agentID appears in ToolsEnabledAgentIDs.
// Deprecated for execution: kept for config parsing tests only.
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

	if maxIter := positiveIntFromEnv(0, "MULTICA_SERVER_FIRTAL_GATEWAY_MAX_TOOL_ITERATIONS"); maxIter > 0 {
		cfg.MaxToolRounds = maxIter
	}

	// FIR-2325: cheap model + holdout share for the model_routing cost saving.
	cfg.CheapModel = strings.TrimSpace(firstNonEmptyEnv("MULTICA_SERVER_FIRTAL_GATEWAY_CHEAP_MODEL", "FIRTAL_DATA_REGISTRY_AI_CHEAP_MODEL"))
	if raw := strings.TrimSpace(os.Getenv("MULTICA_SERVER_FIRTAL_GATEWAY_COST_HOLDOUT_PCT")); raw != "" {
		pct, err := strconv.Atoi(raw)
		if err != nil {
			return FirtalGatewayRuntimeConfig{}, fmt.Errorf("invalid MULTICA_SERVER_FIRTAL_GATEWAY_COST_HOLDOUT_PCT value %q: %w", raw, err)
		}
		cfg.CostSavingHoldoutPct = &pct
	}

	if cfg.Enabled && cfg.BaseURL != "" {
		// Operator-trusted server env URL: honors the internal-address opt-in.
		normalizedURL, err := firtalgateway.ValidateTrustedBaseURL(cfg.BaseURL)
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

	workspaceProvidedURL := false
	if envelope.FirtalGateway != nil {
		settings := envelope.FirtalGateway
		if !settings.Enabled {
			return cfg, false, nil
		}
		if gatewayURL := strings.TrimSpace(settings.GatewayURL); gatewayURL != "" {
			cfg.BaseURL = strings.TrimRight(gatewayURL, "/")
			workspaceProvidedURL = true
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
	// A workspace-supplied URL is untrusted input and is always validated
	// strictly (public HTTPS). When the workspace inherits the operator's env
	// URL, validate it with the trusted policy so an opted-in internal address
	// is accepted.
	validate := firtalgateway.ValidateTrustedBaseURL
	if workspaceProvidedURL {
		validate = firtalgateway.ValidateBaseURL
	}
	normalizedURL, err := validate(cfg.BaseURL)
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
