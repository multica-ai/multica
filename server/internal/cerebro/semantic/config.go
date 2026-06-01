package semantic

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// Config is the env-driven configuration for the semantic search subsystem.
// A self-host install that does not set CEREBRO_SEARCH_SEMANTIC_PROVIDER
// gets the disabled provider and search degrades to keyword-only.
type Config struct {
	// Enabled toggles BOTH the worker and the query path. When false, the
	// queue still fills (triggers are unconditional) but nothing drains it
	// and the handler never asks for a query embedding.
	Enabled bool

	// Provider is one of: "openai", "deterministic", "" (disabled).
	Provider string

	// Endpoint + APIKey + Model are read for the openai provider.
	Endpoint string
	APIKey   string
	Model    string

	// WorkerInterval is the queue-drain cadence. 5s is brisk enough for live
	// search to surface new issues quickly without hammering the model.
	WorkerInterval time.Duration

	// BatchSize caps how many queue entries the worker pulls per tick. Each
	// entry is one provider call.
	BatchSize int

	// QueryTimeout bounds Embed calls on the search request path so a slow
	// model never blocks an HTTP response — the handler treats a timeout as
	// "fall back to keyword-only" rather than erroring.
	QueryTimeout time.Duration
}

// LoadConfig pulls the configuration from the environment with sensible
// defaults. Returns Config{Enabled:false} when no provider is configured.
func LoadConfig() Config {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("CEREBRO_SEARCH_SEMANTIC_PROVIDER")))
	if provider == "" {
		return Config{}
	}
	cfg := Config{
		Enabled:        true,
		Provider:       provider,
		Endpoint:       strings.TrimSpace(os.Getenv("CEREBRO_SEARCH_SEMANTIC_ENDPOINT")),
		APIKey:         strings.TrimSpace(os.Getenv("CEREBRO_SEARCH_SEMANTIC_API_KEY")),
		Model:          strings.TrimSpace(os.Getenv("CEREBRO_SEARCH_SEMANTIC_MODEL")),
		WorkerInterval: 5 * time.Second,
		BatchSize:      16,
		QueryTimeout:   2 * time.Second,
	}
	if v := strings.TrimSpace(os.Getenv("CEREBRO_SEARCH_SEMANTIC_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.WorkerInterval = d
		} else {
			slog.Warn("semantic search: invalid CEREBRO_SEARCH_SEMANTIC_INTERVAL, using default",
				"value", v, "default", cfg.WorkerInterval.String())
		}
	}
	if v := strings.TrimSpace(os.Getenv("CEREBRO_SEARCH_SEMANTIC_QUERY_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.QueryTimeout = d
		}
	}
	return cfg
}

// BuildProvider instantiates the Provider described by cfg. An invalid /
// unknown provider name returns Disabled() and logs — we never crash a server
// boot over a misconfigured opt-in feature.
func (cfg Config) BuildProvider() Provider {
	if !cfg.Enabled {
		return Disabled()
	}
	switch cfg.Provider {
	case "openai":
		if cfg.Endpoint == "" || cfg.Model == "" {
			slog.Warn("semantic search: openai provider missing endpoint/model, disabling")
			return Disabled()
		}
		return NewOpenAIProvider(cfg.Endpoint, cfg.APIKey, cfg.Model)
	case "deterministic":
		return NewDeterministicProvider()
	default:
		slog.Warn("semantic search: unknown provider, disabling", "provider", cfg.Provider)
		return Disabled()
	}
}
