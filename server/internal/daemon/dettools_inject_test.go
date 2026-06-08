package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"
)

func testDetToolsCfg() DetToolsConfig {
	return DetToolsConfig{
		Enabled:      true,
		AllowedTools: []string{"repo_facts", "policy_check"},
		Timeout:      90 * time.Second,
		AllowNetwork: false,
		ArtifactDir:  ".multica/artifacts",
	}
}

// parseServers unmarshals an mcp_config and returns its mcpServers map.
func parseServers(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	servers := map[string]json.RawMessage{}
	if v, ok := root["mcpServers"]; ok {
		if err := json.Unmarshal(v, &servers); err != nil {
			t.Fatalf("unmarshal mcpServers: %v", err)
		}
	}
	return servers
}

func TestBuildEffectiveMcpConfig_EmptyInput(t *testing.T) {
	out, err := buildEffectiveMcpConfig(nil, "/usr/local/bin/multica", "/work/dir", testDetToolsCfg())
	if err != nil {
		t.Fatalf("buildEffectiveMcpConfig: %v", err)
	}
	servers := parseServers(t, out)
	entry, ok := servers[dettoolsServerName]
	if !ok {
		t.Fatalf("expected %q server to be injected", dettoolsServerName)
	}

	var e struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(entry, &e); err != nil {
		t.Fatalf("unmarshal entry: %v", err)
	}
	if e.Command != "/usr/local/bin/multica" {
		t.Errorf("command = %q", e.Command)
	}
	if len(e.Args) != 2 || e.Args[0] != "mcp-tools" || e.Args[1] != "serve" {
		t.Errorf("args = %v, want [mcp-tools serve]", e.Args)
	}
	if e.Env["MULTICA_DETTOOLS_WORKDIR"] != "/work/dir" {
		t.Errorf("workdir env = %q", e.Env["MULTICA_DETTOOLS_WORKDIR"])
	}
	if e.Env["MULTICA_DETTOOLS_ALLOWED"] != "repo_facts,policy_check" {
		t.Errorf("allowed env = %q", e.Env["MULTICA_DETTOOLS_ALLOWED"])
	}
	if e.Env["MULTICA_DETTOOLS_ALLOW_NETWORK"] != "false" {
		t.Errorf("allow_network env = %q", e.Env["MULTICA_DETTOOLS_ALLOW_NETWORK"])
	}
}

func TestBuildEffectiveMcpConfig_AdditivePreservesUserServers(t *testing.T) {
	user := json.RawMessage(`{"mcpServers":{"github":{"command":"gh-mcp","args":["serve"]}}}`)
	out, err := buildEffectiveMcpConfig(user, "/bin/multica", "/w", testDetToolsCfg())
	if err != nil {
		t.Fatalf("buildEffectiveMcpConfig: %v", err)
	}
	servers := parseServers(t, out)
	if _, ok := servers["github"]; !ok {
		t.Error("user-defined github server was dropped")
	}
	if _, ok := servers[dettoolsServerName]; !ok {
		t.Error("deterministic server was not added")
	}
	if len(servers) != 2 {
		t.Errorf("server count = %d, want 2", len(servers))
	}
}

func TestBuildEffectiveMcpConfig_DoesNotOverrideUserNameCollision(t *testing.T) {
	user := json.RawMessage(`{"mcpServers":{"multica-tools":{"command":"custom"}}}`)
	out, err := buildEffectiveMcpConfig(user, "/bin/multica", "/w", testDetToolsCfg())
	if err != nil {
		t.Fatalf("buildEffectiveMcpConfig: %v", err)
	}
	servers := parseServers(t, out)
	var e struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(servers[dettoolsServerName], &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.Command != "custom" {
		t.Errorf("user server was overwritten: command = %q, want custom", e.Command)
	}
}

func TestBuildEffectiveMcpConfig_PreservesOtherTopLevelKeys(t *testing.T) {
	user := json.RawMessage(`{"mcpServers":{},"someOtherKey":{"a":1}}`)
	out, err := buildEffectiveMcpConfig(user, "/bin/multica", "/w", testDetToolsCfg())
	if err != nil {
		t.Fatalf("buildEffectiveMcpConfig: %v", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := root["someOtherKey"]; !ok {
		t.Error("unrelated top-level key was dropped")
	}
}

func TestBuildEffectiveMcpConfig_InvalidJSON(t *testing.T) {
	_, err := buildEffectiveMcpConfig(json.RawMessage(`{not json`), "/bin/multica", "/w", testDetToolsCfg())
	if err == nil {
		t.Fatal("expected error on malformed config")
	}
}

func TestInjectDeterministicTools_Gating(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orig := json.RawMessage(`{"mcpServers":{}}`)

	// Disabled → returns original untouched.
	dDisabled := &Daemon{cfg: Config{DetTools: DetToolsConfig{Enabled: false}}}
	if got := dDisabled.injectDeterministicTools(orig, "claude", "/w", logger); string(got) != string(orig) {
		t.Errorf("disabled: config changed: %s", got)
	}

	// Enabled but non-injecting provider → returns original untouched.
	dEnabled := &Daemon{cfg: Config{DetTools: testDetToolsCfg()}}
	if got := dEnabled.injectDeterministicTools(orig, "gemini", "/w", logger); string(got) != string(orig) {
		t.Errorf("non-injecting provider: config changed: %s", got)
	}

	// Enabled + claude → injects the server.
	got := dEnabled.injectDeterministicTools(orig, "claude", "/w", logger)
	servers := parseServers(t, got)
	if _, ok := servers[dettoolsServerName]; !ok {
		t.Error("claude: deterministic server not injected")
	}
}
