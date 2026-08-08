package handler

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/internal/piagent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// resolvePiRuntimeModelConnection applies the Pi configuration precedence used
// at claim time: a complete per-agent override wins, otherwise a complete
// runtime default (including its secret) is inherited. Invalid partial agent
// configuration is discarded so Pi can still use its native configuration
// when no runtime default is available.
func resolvePiRuntimeModelConnection(
	agentConfig json.RawMessage,
	customEnv map[string]string,
	runtime db.AgentRuntime,
) (json.RawMessage, map[string]string, bool, error, error) {
	agentModel, hasAgentConfig, agentErr := piagent.Decode(agentConfig)
	if hasAgentConfig {
		if strings.TrimSpace(customEnv[piagent.APIKeyEnv]) != "" {
			return agentConfig, customEnv, false, agentErr, nil
		}
		runtimeModel, hasRuntimeConfig, runtimeErr := piagent.Decode(
			json.RawMessage(runtime.DefaultModelConfig),
		)
		hasRuntimeKey := runtime.DefaultModelApiKey.Valid &&
			strings.TrimSpace(runtime.DefaultModelApiKey.String) != ""
		if !hasRuntimeConfig || !hasRuntimeKey ||
			!piagent.SameEndpoint(agentModel, runtimeModel) {
			return agentConfig, customEnv, false, agentErr, runtimeErr
		}
		return agentConfig, withPiRuntimeAPIKey(customEnv, runtime.DefaultModelApiKey.String), true, agentErr, runtimeErr
	}

	_, hasRuntimeConfig, runtimeErr := piagent.Decode(
		json.RawMessage(runtime.DefaultModelConfig),
	)
	hasRuntimeKey := runtime.DefaultModelApiKey.Valid &&
		strings.TrimSpace(runtime.DefaultModelApiKey.String) != ""
	if !hasRuntimeConfig || !hasRuntimeKey {
		return nil, customEnv, false, agentErr, runtimeErr
	}

	return json.RawMessage(runtime.DefaultModelConfig), withPiRuntimeAPIKey(customEnv, runtime.DefaultModelApiKey.String), true, agentErr, runtimeErr
}

func withPiRuntimeAPIKey(customEnv map[string]string, apiKey string) map[string]string {
	nextEnv := make(map[string]string, len(customEnv)+1)
	for key, value := range customEnv {
		nextEnv[key] = value
	}
	nextEnv[piagent.APIKeyEnv] = apiKey
	return nextEnv
}
