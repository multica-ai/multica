package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

type platformAgentRuntimeConfig struct {
	PlatformAgent *execenv.PlatformAgentContextForEnv `json:"platform_agent"`
}

// decodePlatformAgentRuntimeConfig strictly extracts the platform-owned
// runtime context. Unlike optional provider tuning, this payload is required
// for platform-agent-cli; running without it would silently drop the imported
// Extension identity and Command registry.
func decodePlatformAgentRuntimeConfig(raw json.RawMessage) (*execenv.PlatformAgentContextForEnv, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("platform agent runtime_config is required")
	}
	if trimmed[0] != '{' {
		return nil, fmt.Errorf("platform agent runtime_config must be an object")
	}
	var config platformAgentRuntimeConfig
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode platform agent runtime_config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("platform agent runtime_config must contain one JSON value")
	}
	if config.PlatformAgent == nil {
		return nil, fmt.Errorf("platform agent runtime_config.platform_agent is required")
	}
	if err := execenv.ValidatePlatformAgentContext(config.PlatformAgent); err != nil {
		return nil, fmt.Errorf("invalid platform agent runtime_config.platform_agent: %w", err)
	}
	return config.PlatformAgent, nil
}
