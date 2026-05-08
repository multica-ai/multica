package runtime

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	FirtalGatewayProvider = "firtal-gateway"

	defaultFirtalGatewayModel          = "claude-sonnet-4-6"
	defaultFirtalGatewayRuntimeName    = "Firtal Gateway"
	defaultFirtalGatewayPollInterval   = 2 * time.Second
	defaultFirtalGatewaySyncInterval   = 30 * time.Second
	defaultFirtalGatewayTaskTimeout    = 10 * time.Minute
	defaultFirtalGatewayHistoryLimit   = 30
	defaultFirtalGatewayMaxConcurrency = 4
)

// FirtalGatewayRuntimeConfig controls the server-owned HTTPS runtime backed by
// the Data Registry AI Gateway. It deliberately uses server env only; agent
// custom_env is not allowed to override central gateway credentials.
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
// runtime auto-enables only when both URL and key are present.
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
		enabled = baseURL != "" && apiKey != ""
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

	if !cfg.Enabled {
		return cfg, nil
	}
	if cfg.BaseURL == "" {
		return FirtalGatewayRuntimeConfig{}, fmt.Errorf("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL is required when server firtal gateway runtime is enabled")
	}
	if cfg.APIKey == "" {
		return FirtalGatewayRuntimeConfig{}, fmt.Errorf("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY is required when server firtal gateway runtime is enabled")
	}
	if parsed, err := url.Parse(cfg.BaseURL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return FirtalGatewayRuntimeConfig{}, fmt.Errorf("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL must be an absolute URL")
	}

	return cfg, nil
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
