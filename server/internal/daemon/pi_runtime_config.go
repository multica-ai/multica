package daemon

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/piagent"
)

// decodePiRuntimeConfig validates the non-secret Pi model configuration. A
// malformed or partial blob fails soft: it is ignored and Pi keeps its native
// configuration, matching the behavior of the other provider-specific runtime
// config decoders.
func decodePiRuntimeConfig(raw json.RawMessage, logger *slog.Logger) (piagent.Config, bool) {
	cfg, configured, err := piagent.Decode(raw)
	if err != nil {
		logger.Warn("pi runtime_config: invalid; using native Pi configuration", "error", err)
		return piagent.Config{}, false
	}
	return cfg, configured
}

func applyPiAgentModelOverride(cfg piagent.Config, modelOverride string, logger *slog.Logger) piagent.Config {
	modelOverride = strings.TrimSpace(modelOverride)
	if modelOverride == "" {
		return cfg
	}
	if provider, model, ok := strings.Cut(modelOverride, "/"); ok {
		provider = strings.TrimSpace(provider)
		model = strings.TrimSpace(model)
		if provider == "" || model == "" {
			return cfg
		}
		if !strings.EqualFold(provider, cfg.Provider) {
			logger.Warn(
				"pi runtime_config: model override provider differs from runtime provider; using runtime default",
				"runtime_provider", cfg.Provider,
				"model_provider", provider,
			)
			return cfg
		}
		cfg.Model = model
		return cfg
	}
	cfg.Model = modelOverride
	return cfg
}
