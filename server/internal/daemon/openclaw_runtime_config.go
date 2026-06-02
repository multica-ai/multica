package daemon

import (
	"encoding/json"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// openclawRuntimeConfig is the schema the daemon expects under an openclaw
// agent's `runtime_config` JSONB column. All fields are optional; absence
// (or the agent's whole runtime_config being null/empty) keeps the
// historical embedded behaviour so existing agents are unaffected.
//
// Schema (issue #3260):
//
//	{
//	  "mode": "local" | "gateway",     // default: "local"
//	  "gateway": {
//	    "host":  "<hostname>",         // remote OpenClaw gateway host
//	    "port":  18789,                // gateway port
//	    "token": "<bearer>",           // gateway auth token (masked in API responses)
//	    "tls":   false                 // dial https if true
//	  }
//	}
//
// Other providers' runtime_config payloads pass through untouched — this
// decoder only reads keys that have meaning for the openclaw backend.
type openclawRuntimeConfig struct {
	Mode    string                       `json:"mode"`
	Gateway openclawRuntimeGatewayConfig `json:"gateway"`
}

type openclawRuntimeGatewayConfig struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Token string `json:"token"`
	TLS   bool   `json:"tls"`
}

// decodeOpenclawRuntimeConfig extracts the openclaw-specific knobs from an
// agent's runtime_config payload. Returns the routing mode plus the gateway
// pin shaped for execenv. A malformed payload logs a warning and degrades
// to local mode (mode="", zero gateway) rather than failing dispatch — the
// alternative would let one bad save block every task that agent runs.
func decodeOpenclawRuntimeConfig(raw json.RawMessage, logger *slog.Logger) (string, execenv.OpenclawGatewayPin) {
	if len(raw) == 0 {
		return "", execenv.OpenclawGatewayPin{}
	}
	var cfg openclawRuntimeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		logger.Warn("openclaw runtime_config: parse failed; falling back to local mode", "error", err)
		return "", execenv.OpenclawGatewayPin{}
	}
	return cfg.Mode, execenv.OpenclawGatewayPin{
		Host:  cfg.Gateway.Host,
		Port:  cfg.Gateway.Port,
		Token: cfg.Gateway.Token,
		TLS:   cfg.Gateway.TLS,
	}
}
