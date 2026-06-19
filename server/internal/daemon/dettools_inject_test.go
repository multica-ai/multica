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

func testAllowed() []string { return []string{"repo_facts", "policy_check"} }

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
	out, err := buildEffectiveMcpConfig(nil, "/usr/local/bin/multica", "/work/dir", "", testDetToolsCfg(), testAllowed())
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

func TestBuildEffectiveMcpConfig_OmitsWorkdirWhenEmpty(t *testing.T) {
	out, err := buildEffectiveMcpConfig(nil, "/bin/multica", "", "", testDetToolsCfg(), testAllowed())
	if err != nil {
		t.Fatalf("buildEffectiveMcpConfig: %v", err)
	}
	var e struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(parseServers(t, out)[dettoolsServerName], &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := e.Env["MULTICA_DETTOOLS_WORKDIR"]; ok {
		t.Error("WORKDIR env should be omitted when workDir is empty (OpenClaw cwd fallback)")
	}
}

func TestBuildEffectiveMcpConfig_AdditivePreservesUserServers(t *testing.T) {
	user := json.RawMessage(`{"mcpServers":{"github":{"command":"gh-mcp","args":["serve"]}}}`)
	out, err := buildEffectiveMcpConfig(user, "/bin/multica", "/w", "", testDetToolsCfg(), testAllowed())
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
	out, err := buildEffectiveMcpConfig(user, "/bin/multica", "/w", "", testDetToolsCfg(), testAllowed())
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
	out, err := buildEffectiveMcpConfig(user, "/bin/multica", "/w", "", testDetToolsCfg(), testAllowed())
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
	_, err := buildEffectiveMcpConfig(json.RawMessage(`{not json`), "/bin/multica", "/w", "", testDetToolsCfg(), testAllowed())
	if err == nil {
		t.Fatal("expected error on malformed config")
	}
}

func TestComputeEffectiveAllowed(t *testing.T) {
	cfg := DetToolsConfig{
		AllowedTools: []string{"repo_facts", "policy_check", "test_gate"},
		DeniedTools:  []string{"test_gate"}, // daemon-wide denylist
	}

	// No agent profile: daemon allowlist minus daemon denylist.
	got := computeEffectiveAllowed(cfg, nil)
	if want := "repo_facts,policy_check"; join(got) != want {
		t.Errorf("no profile: got %v, want %s", got, want)
	}

	// Agent narrows to a subset; it cannot widen beyond the daemon allowlist.
	rc := json.RawMessage(`{"deterministic_tools":{"allowed_tools":["repo_facts","build_probe"]}}`)
	got = computeEffectiveAllowed(cfg, rc)
	if want := "repo_facts"; join(got) != want {
		t.Errorf("agent narrow: got %v, want %s (build_probe not in daemon allowlist)", got, want)
	}

	// Agent denylist removes an otherwise-allowed tool.
	rc = json.RawMessage(`{"deterministic_tools":{"denied_tools":["policy_check"]}}`)
	got = computeEffectiveAllowed(cfg, rc)
	if want := "repo_facts"; join(got) != want {
		t.Errorf("agent deny: got %v, want %s", got, want)
	}
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

func TestInjectExecOptionsTools_Gating(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orig := json.RawMessage(`{"mcpServers":{}}`)

	// Disabled → returns original untouched.
	dDisabled := &Daemon{cfg: Config{DetTools: DetToolsConfig{Enabled: false}}}
	if got := dDisabled.injectExecOptionsTools(orig, "claude", "/w", nil, nil, logger); string(got) != string(orig) {
		t.Errorf("disabled: config changed: %s", got)
	}

	dEnabled := &Daemon{cfg: Config{DetTools: testDetToolsCfg()}}

	// Non-MCP provider → untouched.
	if got := dEnabled.injectExecOptionsTools(orig, "gemini", "/w", nil, nil, logger); string(got) != string(orig) {
		t.Errorf("non-injecting provider: config changed: %s", got)
	}

	// Every ExecOptions MCP provider gets the server injected.
	for _, provider := range []string{"claude", "codex", "opencode", "hermes", "kimi", "kiro"} {
		got := dEnabled.injectExecOptionsTools(orig, provider, "/w", nil, nil, logger)
		if _, ok := parseServers(t, got)[dettoolsServerName]; !ok {
			t.Errorf("%s: deterministic server not injected", provider)
		}
	}

	// An agent profile that allows no daemon tool → injection skipped.
	rc := json.RawMessage(`{"deterministic_tools":{"allowed_tools":["nonexistent"]}}`)
	if got := dEnabled.injectExecOptionsTools(orig, "claude", "/w", rc, nil, logger); string(got) != string(orig) {
		t.Errorf("empty effective allowlist should skip injection, got %s", got)
	}
}

func TestInjectExecenvTools_OpenclawOnly(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orig := json.RawMessage(`{"mcpServers":{}}`)
	dEnabled := &Daemon{cfg: Config{DetTools: testDetToolsCfg()}}

	// openclaw gets injected through the execenv path.
	if _, ok := parseServers(t, dEnabled.injectExecenvTools(orig, "openclaw", nil, nil, logger))[dettoolsServerName]; !ok {
		t.Error("openclaw: deterministic server not injected via execenv path")
	}
	// claude does NOT go through the execenv path (it uses ExecOptions).
	if got := dEnabled.injectExecenvTools(orig, "claude", nil, nil, logger); string(got) != string(orig) {
		t.Errorf("claude should not be injected via execenv path, got %s", got)
	}
}
