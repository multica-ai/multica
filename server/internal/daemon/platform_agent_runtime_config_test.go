package daemon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodePlatformAgentRuntimeConfigValid(t *testing.T) {
	raw := json.RawMessage(`{
  "platform_agent": {
    "schema_version": "platform-agent.runtime-context/v1",
    "extension": {
      "key": "research-team",
      "version": "1.0.0",
      "release_id": "release-1",
      "digest": "sha256:abc"
    },
    "agent": {"source_key": "lead-researcher"},
    "commands": [{
      "name": "summarize",
      "description": "Summary command.",
      "content": "Summarize findings.",
      "metadata": {"owner": "platform"}
    }]
  }
}`)

	got, err := decodePlatformAgentRuntimeConfig(raw)
	if err != nil {
		t.Fatalf("decodePlatformAgentRuntimeConfig() error = %v", err)
	}
	if got.Extension.Key != "research-team" || got.Agent.SourceKey != "lead-researcher" || len(got.Commands) != 1 {
		t.Fatalf("decoded context = %+v", got)
	}
	if string(got.Commands[0].Metadata) != `{"owner": "platform"}` {
		t.Fatalf("metadata bytes = %q, want original JSON bytes", got.Commands[0].Metadata)
	}
}

func TestDecodePlatformAgentRuntimeConfigFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "missing config", raw: ``, want: "runtime_config is required"},
		{name: "null config", raw: `null`, want: "runtime_config must be an object"},
		{name: "missing platform key", raw: `{}`, want: "platform_agent is required"},
		{name: "unknown outer field", raw: `{"platform_agent":` + validPlatformAgentPayload() + `,"other":true}`, want: "unknown field"},
		{name: "trailing value", raw: `{"platform_agent":` + validPlatformAgentPayload() + `} {}`, want: "one JSON value"},
		{name: "wrong schema", raw: `{"platform_agent":` + strings.Replace(validPlatformAgentPayload(), "platform-agent.runtime-context/v1", "wrong/v1", 1) + `}`, want: "unsupported schema"},
		{name: "unknown context field", raw: `{"platform_agent":` + strings.TrimSuffix(validPlatformAgentPayload(), `}`) + `,"unknown":true}}`, want: "unknown field"},
		{name: "missing extension identity", raw: `{"platform_agent":{"schema_version":"platform-agent.runtime-context/v1","extension":{},"agent":{"source_key":"lead"},"commands":[]}}`, want: "extension identity"},
		{name: "missing agent identity", raw: `{"platform_agent":{"schema_version":"platform-agent.runtime-context/v1","extension":{"key":"x","version":"1","release_id":"r","digest":"d"},"agent":{},"commands":[]}}`, want: "source_key"},
		{name: "duplicate command", raw: `{"platform_agent":{"schema_version":"platform-agent.runtime-context/v1","extension":{"key":"x","version":"1","release_id":"r","digest":"d"},"agent":{"source_key":"lead"},"commands":[{"name":"same","metadata":{}},{"name":"same","metadata":{}}]}}`, want: "duplicate command"},
		{name: "missing metadata", raw: `{"platform_agent":{"schema_version":"platform-agent.runtime-context/v1","extension":{"key":"x","version":"1","release_id":"r","digest":"d"},"agent":{"source_key":"lead"},"commands":[{"name":"same"}]}}`, want: "metadata"},
		{name: "unknown command field", raw: `{"platform_agent":{"schema_version":"platform-agent.runtime-context/v1","extension":{"key":"x","version":"1","release_id":"r","digest":"d"},"agent":{"source_key":"lead"},"commands":[{"name":"same","metadata":{},"tool":true}]}}`, want: "unknown field"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodePlatformAgentRuntimeConfig(json.RawMessage(tt.raw))
			if err == nil {
				t.Fatalf("decodePlatformAgentRuntimeConfig() = %+v, want error", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestDecodePlatformAgentRuntimeConfigRejectsContextLargerThanCLIInputLimit(t *testing.T) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(validPlatformAgentPayload()), &payload); err != nil {
		t.Fatal(err)
	}
	commands := payload["commands"].([]any)
	commands[0].(map[string]any)["content"] = strings.Repeat("x", 1024*1024)
	raw, err := json.Marshal(map[string]any{"platform_agent": payload})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decodePlatformAgentRuntimeConfig(raw); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("decodePlatformAgentRuntimeConfig() = %+v, %v; want size error", got, err)
	}
}

func validPlatformAgentPayload() string {
	return `{
  "schema_version":"platform-agent.runtime-context/v1",
  "extension":{"key":"research-team","version":"1.0.0","release_id":"release-1","digest":"sha256:abc"},
  "agent":{"source_key":"lead-researcher"},
  "commands":[{"name":"summarize","description":"Summary command.","content":"Summarize findings.","metadata":{}}]
}`
}
