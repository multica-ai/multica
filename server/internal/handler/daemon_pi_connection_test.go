package handler

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/piagent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var validRuntimePiConfig = json.RawMessage(`{
	"provider":"deepseek",
	"api":"openai-completions",
	"base_url":"https://api.deepseek.com",
	"model":"deepseek-v4-flash"
}`)

func TestResolvePiRuntimeModelConnectionInheritsCompleteRuntimeDefault(t *testing.T) {
	originalEnv := map[string]string{
		"OTHER":           "preserved",
		piagent.APIKeyEnv: "stale-agent-secret",
	}
	runtime := db.AgentRuntime{
		DefaultModelConfig: validRuntimePiConfig,
		DefaultModelApiKey: pgtype.Text{String: "runtime-secret", Valid: true},
	}

	config, env, inherited, agentErr, runtimeErr :=
		resolvePiRuntimeModelConnection(nil, originalEnv, runtime)

	if agentErr != nil || runtimeErr != nil {
		t.Fatalf("unexpected decode errors: agent=%v runtime=%v", agentErr, runtimeErr)
	}
	if !inherited {
		t.Fatal("expected runtime default to be inherited")
	}
	if string(config) != string(validRuntimePiConfig) {
		t.Fatalf("config = %s, want runtime default", config)
	}
	if env[piagent.APIKeyEnv] != "runtime-secret" || env["OTHER"] != "preserved" {
		t.Fatalf("custom env = %#v, want inherited key and preserved values", env)
	}
	if originalEnv[piagent.APIKeyEnv] != "stale-agent-secret" {
		t.Fatal("resolver mutated the original custom env map")
	}
}

func TestResolvePiRuntimeModelConnectionKeepsAgentOverrideIsolated(t *testing.T) {
	agentConfig := json.RawMessage(`{
		"provider":"openai",
		"api":"openai-responses",
		"base_url":"https://api.openai.com/v1",
		"model":"gpt-5"
	}`)
	agentEnv := map[string]string{piagent.APIKeyEnv: "agent-secret"}
	runtime := db.AgentRuntime{
		DefaultModelConfig: validRuntimePiConfig,
		DefaultModelApiKey: pgtype.Text{String: "runtime-secret", Valid: true},
	}

	config, env, inherited, agentErr, runtimeErr :=
		resolvePiRuntimeModelConnection(agentConfig, agentEnv, runtime)

	if agentErr != nil || runtimeErr != nil {
		t.Fatalf("unexpected decode errors: agent=%v runtime=%v", agentErr, runtimeErr)
	}
	if inherited {
		t.Fatal("agent override must win over the runtime default")
	}
	if string(config) != string(agentConfig) {
		t.Fatalf("config = %s, want agent override", config)
	}
	if env[piagent.APIKeyEnv] != "agent-secret" {
		t.Fatal("runtime key replaced the agent override key")
	}
}

func TestResolvePiRuntimeModelConnectionSharesKeyForModelOnlyOverride(t *testing.T) {
	agentConfig := json.RawMessage(`{
		"provider":"deepseek",
		"api":"openai-completions",
		"base_url":"https://api.deepseek.com/",
		"model":"deepseek-v4-pro"
	}`)
	runtime := db.AgentRuntime{
		DefaultModelConfig: validRuntimePiConfig,
		DefaultModelApiKey: pgtype.Text{String: "runtime-secret", Valid: true},
	}

	config, env, inherited, agentErr, runtimeErr :=
		resolvePiRuntimeModelConnection(agentConfig, nil, runtime)

	if agentErr != nil || runtimeErr != nil {
		t.Fatalf("unexpected decode errors: agent=%v runtime=%v", agentErr, runtimeErr)
	}
	if !inherited {
		t.Fatal("same-endpoint model override should inherit the runtime key")
	}
	if string(config) != string(agentConfig) {
		t.Fatalf("config = %s, want agent model override", config)
	}
	if env[piagent.APIKeyEnv] != "runtime-secret" {
		t.Fatal("model-only override did not inherit the runtime key")
	}
}

func TestResolvePiRuntimeModelConnectionDoesNotShareKeyAcrossEndpoints(t *testing.T) {
	agentConfig := json.RawMessage(`{
		"provider":"openai",
		"api":"openai-responses",
		"base_url":"https://api.openai.com/v1",
		"model":"gpt-5"
	}`)
	runtime := db.AgentRuntime{
		DefaultModelConfig: validRuntimePiConfig,
		DefaultModelApiKey: pgtype.Text{String: "runtime-secret", Valid: true},
	}

	_, env, inherited, _, _ :=
		resolvePiRuntimeModelConnection(agentConfig, nil, runtime)

	if inherited {
		t.Fatal("different provider endpoint must not inherit the runtime key")
	}
	if env[piagent.APIKeyEnv] != "" {
		t.Fatal("runtime key leaked across provider endpoints")
	}
}

func TestResolvePiRuntimeModelConnectionRejectsIncompleteDefaults(t *testing.T) {
	tests := []struct {
		name    string
		runtime db.AgentRuntime
	}{
		{
			name: "missing key",
			runtime: db.AgentRuntime{
				DefaultModelConfig: validRuntimePiConfig,
			},
		},
		{
			name: "invalid config",
			runtime: db.AgentRuntime{
				DefaultModelConfig: json.RawMessage(`{"provider":"deepseek"}`),
				DefaultModelApiKey: pgtype.Text{String: "runtime-secret", Valid: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, env, inherited, _, _ := resolvePiRuntimeModelConnection(
				json.RawMessage(`{"provider":"broken"}`),
				map[string]string{"OTHER": "preserved"},
				tt.runtime,
			)
			if inherited {
				t.Fatal("incomplete runtime connection must not be inherited")
			}
			if config != nil {
				t.Fatalf("invalid agent config should be discarded, got %s", config)
			}
			if env["OTHER"] != "preserved" || env[piagent.APIKeyEnv] != "" {
				t.Fatalf("custom env unexpectedly changed: %#v", env)
			}
		})
	}
}
