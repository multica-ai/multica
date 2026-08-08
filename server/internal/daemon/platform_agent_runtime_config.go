package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

type platformAgentRuntimeConfig struct {
	PlatformAgent *json.RawMessage `json:"platform_agent"`
}

type platformAgentContextWire struct {
	SchemaVersion string                                `json:"schema_version"`
	Extension     execenv.PlatformAgentExtensionForEnv  `json:"extension"`
	Agent         execenv.PlatformAgentIdentityForEnv   `json:"agent"`
	Commands      *[]execenv.PlatformAgentCommandForEnv `json:"commands"`
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
	if err := execenv.ValidateNoDuplicateJSONKeys(trimmed); err != nil {
		return nil, fmt.Errorf("decode platform agent runtime_config: %w", err)
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
	platformRaw := bytes.TrimSpace(*config.PlatformAgent)
	if len(platformRaw) == 0 || bytes.Equal(platformRaw, []byte("null")) {
		return nil, fmt.Errorf("platform agent runtime_config.platform_agent is required")
	}
	if platformRaw[0] != '{' {
		return nil, fmt.Errorf("platform agent runtime_config.platform_agent must be an object")
	}
	var wire platformAgentContextWire
	platformDecoder := json.NewDecoder(bytes.NewReader(platformRaw))
	platformDecoder.DisallowUnknownFields()
	if err := platformDecoder.Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode platform agent runtime_config.platform_agent: %w", err)
	}
	if err := platformDecoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("platform agent runtime_config.platform_agent must contain one JSON value")
	}
	if wire.Commands == nil {
		return nil, fmt.Errorf("platform agent runtime_config.platform_agent.commands must be an array")
	}
	platformContext := &execenv.PlatformAgentContextForEnv{
		SchemaVersion: wire.SchemaVersion,
		Extension:     wire.Extension,
		Agent:         wire.Agent,
		Commands:      *wire.Commands,
	}
	if err := execenv.ValidatePlatformAgentContext(platformContext); err != nil {
		return nil, fmt.Errorf("invalid platform agent runtime_config.platform_agent: %w", err)
	}
	return platformContext, nil
}
