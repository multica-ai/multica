package agent

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestBuildACPSessionParamsMinimum verifies the params object contains only
// `cwd` when no capabilities are enabled and opts carries no extra context.
// This is the safest default and matches what we want to send to a runtime
// that opted out of every advanced feature.
func TestBuildACPSessionParamsMinimum(t *testing.T) {
	t.Parallel()
	params := buildACPSessionParams(ExecOptions{Cwd: "/tmp/wd"}, ConfigCapabilities{})
	if len(params) != 1 {
		t.Fatalf("expected 1 param (cwd), got %d: %v", len(params), params)
	}
	if got := params["cwd"]; got != "/tmp/wd" {
		t.Errorf("cwd = %v, want /tmp/wd", got)
	}
}

// TestBuildACPSessionParamsForwardsModelOnlyWhenCapabilityEnabled documents
// the contract: opts.Model alone is not enough; the manifest must opt in to
// model_selection or the daemon won't forward the field. This stops a
// runtime that doesn't accept `model` from receiving a noisy param it might
// reject during session/new.
func TestBuildACPSessionParamsForwardsModelOnlyWhenCapabilityEnabled(t *testing.T) {
	t.Parallel()
	t.Run("disabled", func(t *testing.T) {
		params := buildACPSessionParams(
			ExecOptions{Cwd: "/wd", Model: "claude-sonnet-4"},
			ConfigCapabilities{ModelSelection: false},
		)
		if _, ok := params["model"]; ok {
			t.Errorf("model was forwarded despite capability disabled: %v", params)
		}
	})
	t.Run("enabled", func(t *testing.T) {
		params := buildACPSessionParams(
			ExecOptions{Cwd: "/wd", Model: "claude-sonnet-4"},
			ConfigCapabilities{ModelSelection: true},
		)
		if got := params["model"]; got != "claude-sonnet-4" {
			t.Errorf("model = %v, want claude-sonnet-4", got)
		}
	})
}

// TestBuildACPSessionParamsForwardsThinkingMaxTurnsAndResume covers the
// trio of optional-but-common params on a runtime that opts in to all
// three. Each capability flag gates exactly one field; flipping one off
// must not affect the others.
func TestBuildACPSessionParamsForwardsThinkingMaxTurnsAndResume(t *testing.T) {
	t.Parallel()
	caps := ConfigCapabilities{
		Thinking:      true,
		MaxTurns:      true,
		SessionResume: true,
	}
	opts := ExecOptions{
		Cwd:             "/wd",
		ThinkingLevel:   "high",
		MaxTurns:        25,
		ResumeSessionID: "sess-1234",
	}
	params := buildACPSessionParams(opts, caps)
	if got := params["thinkingLevel"]; got != "high" {
		t.Errorf("thinkingLevel = %v, want high", got)
	}
	if got := params["maxTurns"]; got != 25 {
		t.Errorf("maxTurns = %v, want 25", got)
	}
	if got := params["sessionId"]; got != "sess-1234" {
		t.Errorf("sessionId = %v, want sess-1234", got)
	}
}

// TestBuildACPSessionParamsZeroMaxTurnsIsNotForwarded ensures we don't send
// a 0-valued maxTurns just because the capability flag is on. Zero means
// "unset" in ExecOptions, and a runtime might treat literal 0 as "no
// turns allowed" and refuse to do anything.
func TestBuildACPSessionParamsZeroMaxTurnsIsNotForwarded(t *testing.T) {
	t.Parallel()
	params := buildACPSessionParams(
		ExecOptions{Cwd: "/wd", MaxTurns: 0},
		ConfigCapabilities{MaxTurns: true},
	)
	if _, ok := params["maxTurns"]; ok {
		t.Errorf("zero maxTurns was forwarded: %v", params)
	}
}

// TestBuildACPSessionParamsMcpConfigClaudeShape verifies the daemon
// translates Claude's `{"name": {...}}` MCP config into the ACP-style
// array of server descriptors. This is the shape Hermes/Kimi/Kiro consume,
// so external runtimes inherit the same translation for free.
func TestBuildACPSessionParamsMcpConfigClaudeShape(t *testing.T) {
	t.Parallel()
	mcp := json.RawMessage(`{
		"linear": {"command": "linear-mcp", "args": ["serve"]},
		"github": {"command": "gh-mcp"}
	}`)
	params := buildACPSessionParams(
		ExecOptions{Cwd: "/wd", McpConfig: mcp},
		ConfigCapabilities{McpConfig: true},
	)
	servers, ok := params["mcpServers"].([]map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing or wrong type: %T %v", params["mcpServers"], params["mcpServers"])
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	names := map[string]bool{}
	for _, s := range servers {
		if n, _ := s["name"].(string); n != "" {
			names[n] = true
		}
	}
	if !names["linear"] || !names["github"] {
		t.Errorf("expected names linear+github, got %v", names)
	}
}

// TestBuildACPSessionParamsMcpConfigACPShape verifies the daemon passes
// already-ACP-shaped MCP configs through without translation. Manifest
// authors who already speak ACP shouldn't be forced to round-trip through
// the legacy Claude shape.
func TestBuildACPSessionParamsMcpConfigACPShape(t *testing.T) {
	t.Parallel()
	mcp := json.RawMessage(`{
		"mcpServers": [
			{"name": "linear", "command": "linear-mcp"},
			{"name": "github", "command": "gh-mcp"}
		]
	}`)
	params := buildACPSessionParams(
		ExecOptions{Cwd: "/wd", McpConfig: mcp},
		ConfigCapabilities{McpConfig: true},
	)
	servers, ok := params["mcpServers"].([]map[string]any)
	if !ok {
		t.Fatalf("mcpServers wrong type: %T", params["mcpServers"])
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
}

// TestBuildACPPromptParamsUsesSessionIDAndTextBlocks pins the ACP
// session/prompt contract for external runtime extensions. The runtime
// returns sessionId from session/new; every prompt turn must send that id
// plus text-block prompt content rather than a bare string payload.
func TestBuildACPPromptParamsUsesSessionIDAndTextBlocks(t *testing.T) {
	t.Parallel()
	params := buildACPPromptParams("ses-ext-1", "hello runtime")
	if got := params["sessionId"]; got != "ses-ext-1" {
		t.Fatalf("sessionId = %v, want ses-ext-1", got)
	}
	for _, key := range []string{"prompt", "content"} {
		blocks, ok := params[key].([]map[string]any)
		if !ok {
			t.Fatalf("%s = %T, want []map[string]any", key, params[key])
		}
		if len(blocks) != 1 {
			t.Fatalf("%s len = %d, want 1", key, len(blocks))
		}
		if blocks[0]["type"] != "text" || blocks[0]["text"] != "hello runtime" {
			t.Fatalf("%s[0] = %v, want text block", key, blocks[0])
		}
	}
}

// TestManifestBlockedArgsTranslation pins the wire format ("flag" vs
// "value") to the internal blockedArgMode constants the filter consumes.
// A regression here would cause a manifest-declared blocked flag to
// silently mis-handle its argument, either dropping a real positional or
// keeping a critical override that should have been stripped.
func TestManifestBlockedArgsTranslation(t *testing.T) {
	t.Parallel()
	in := map[string]string{
		"--output-format": "value",
		"--dangerous":     "flag",
		"--legacy":        "boolean", // alias for flag
		"--unknown":       "?",       // unknown defaults to value (safer)
		"--mode-explicit": "VALUE",   // case-insensitive
	}
	out := manifestBlockedArgs(in)

	expectMode := func(t *testing.T, flag string, want blockedArgMode) {
		t.Helper()
		got, ok := out[flag]
		if !ok {
			t.Fatalf("flag %s missing from translated map", flag)
		}
		if got != want {
			t.Errorf("flag %s = %d, want %d", flag, got, want)
		}
	}
	expectMode(t, "--output-format", blockedWithValue)
	expectMode(t, "--dangerous", blockedStandalone)
	expectMode(t, "--legacy", blockedStandalone)
	expectMode(t, "--unknown", blockedWithValue)
	expectMode(t, "--mode-explicit", blockedWithValue)
}

// TestManifestBlockedArgsActuallyFilters does an end-to-end check via
// filterCustomArgs to make sure the manifest-declared blocked set takes
// effect. Without this the unit test for the translator could pass while
// the integration into the ACP/stream-json backends silently drops the
// map.
func TestManifestBlockedArgsActuallyFilters(t *testing.T) {
	t.Parallel()
	blocked := manifestBlockedArgs(map[string]string{
		"--output-format": "value",
		"--yolo":          "flag",
	})
	args := []string{"--output-format", "stream-json", "--yolo", "--model", "x"}
	got := filterCustomArgs(args, blocked, slog.Default())
	if strings.Join(got, " ") != "--model x" {
		t.Errorf("filtered args = %v, want [--model x]", got)
	}
}

// TestMergeRuntimeEnvKeepsExistingKeys verifies that a manifest cannot
// clobber daemon-managed env vars: `MULTICA_AGENT_SKILLS_ROOT` is only
// injected when not already set so a future per-task override can win.
func TestMergeRuntimeEnvKeepsExistingKeys(t *testing.T) {
	t.Parallel()
	t.Run("injects when absent", func(t *testing.T) {
		out := mergeRuntimeEnv(map[string]string{"OTHER": "v"}, "/skills")
		if out["MULTICA_AGENT_SKILLS_ROOT"] != "/skills" {
			t.Errorf("skills root not injected: %v", out)
		}
		if out["OTHER"] != "v" {
			t.Errorf("existing key lost: %v", out)
		}
	})
	t.Run("respects existing key", func(t *testing.T) {
		out := mergeRuntimeEnv(map[string]string{"MULTICA_AGENT_SKILLS_ROOT": "/already"}, "/should-not-win")
		if out["MULTICA_AGENT_SKILLS_ROOT"] != "/already" {
			t.Errorf("manifest skills root clobbered: %v", out)
		}
	})
	t.Run("empty skills root is no-op", func(t *testing.T) {
		out := mergeRuntimeEnv(map[string]string{"X": "y"}, "")
		if _, ok := out["MULTICA_AGENT_SKILLS_ROOT"]; ok {
			t.Errorf("empty skills root injected anyway: %v", out)
		}
	})
}

// TestNormalizeMCPServersUnparseable ensures we don't panic on garbage and
// fall through to the raw-pass-through path the ACP backend takes.
func TestNormalizeMCPServersUnparseable(t *testing.T) {
	t.Parallel()
	servers, ok := normalizeMCPServers(json.RawMessage(`null`))
	if ok || servers != nil {
		t.Errorf("null payload should not normalise: ok=%v servers=%v", ok, servers)
	}
	servers, ok = normalizeMCPServers(json.RawMessage(``))
	if ok || servers != nil {
		t.Errorf("empty payload should not normalise: ok=%v servers=%v", ok, servers)
	}
}
