package agent

// CEREBRO-PATCH(agent-managed-mcp-project-config-tests): FIR-1459 - verify local runtime MCP config materialization.

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnsureCursorProjectMCPConfigWritesMCPAndPermissionDeny(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	cursorDir := filepath.Join(cwd, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o700); err != nil {
		t.Fatalf("mkdir .cursor: %v", err)
	}
	originalCLI := []byte(`{"permissions":{"allow":["Shell(ls)"],"deny":["Shell(rm)"]},"theme":"dark"}`)
	cliPath := filepath.Join(cursorDir, "cli.json")
	if err := os.WriteFile(cliPath, originalCLI, 0o644); err != nil {
		t.Fatalf("seed cli config: %v", err)
	}

	cleanup, err := ensureCursorProjectMCPConfig(
		cwd,
		json.RawMessage(`{"mcpServers":{"shopify":{"url":"https://mcp.example/mcp","headers":{"Authorization":"Bearer secret"}}}}`),
		[]string{"mcp__shopify__delete_order", "mcp__shopify__delete_order", "mcp__shopify__refund_order"},
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("ensure cursor config: %v", err)
	}

	mcpData, err := os.ReadFile(filepath.Join(cursorDir, "mcp.json"))
	if err != nil {
		t.Fatalf("read mcp config: %v", err)
	}
	if !strings.Contains(string(mcpData), `"shopify"`) || !strings.Contains(string(mcpData), `"Bearer secret"`) {
		t.Fatalf("managed mcp config missing expected server/headers:\n%s", mcpData)
	}

	var cli map[string]any
	if err := json.Unmarshal(mustReadFile(t, cliPath), &cli); err != nil {
		t.Fatalf("parse cli config: %v", err)
	}
	permissions := cli["permissions"].(map[string]any)
	deny := stringSliceFromAny(permissions["deny"])
	wantDeny := []string{"Mcp(shopify:delete_order)", "Mcp(shopify:refund_order)", "Shell(rm)"}
	if !reflect.DeepEqual(deny, wantDeny) {
		t.Fatalf("deny rules = %v, want %v", deny, wantDeny)
	}
	if allow := stringSliceFromAny(permissions["allow"]); !reflect.DeepEqual(allow, []string{"Shell(ls)"}) {
		t.Fatalf("allow rules should be preserved, got %v", allow)
	}

	cleanup()
	if _, err := os.Stat(filepath.Join(cursorDir, "mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("mcp.json should be removed after cleanup, stat err=%v", err)
	}
	if got := string(mustReadFile(t, cliPath)); got != string(originalCLI) {
		t.Fatalf("cli config should be restored\n got: %s\nwant: %s", got, originalCLI)
	}
}

func TestEnsureGeminiProjectMCPConfigWritesSettingsAndRestores(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	geminiDir := filepath.Join(cwd, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		t.Fatalf("mkdir .gemini: %v", err)
	}
	originalSettings := []byte(`{"theme":"Dracula","mcp":{"serverCommand":"custom","excluded":["old"]}}`)
	settingsPath := filepath.Join(geminiDir, "settings.json")
	if err := os.WriteFile(settingsPath, originalSettings, 0o644); err != nil {
		t.Fatalf("seed gemini settings: %v", err)
	}

	cleanup, err := ensureGeminiProjectMCPConfig(
		cwd,
		json.RawMessage(`{"mcpServers":{"beta":{"command":"uvx","args":["server"]},"alpha":{"url":"https://mcp.example/mcp","headers":{"X-API-Key":"secret"}}}}`),
		// CEREBRO-PATCH(daemon-connection-tool-deny): TECH-3156 assert Gemini receives per-tool MCP denies.
		[]string{"mcp__alpha__delete_order", "mcp__alpha__refund_order"},
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("ensure gemini config: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(mustReadFile(t, settingsPath), &settings); err != nil {
		t.Fatalf("parse gemini settings: %v", err)
	}
	if settings["theme"] != "Dracula" {
		t.Fatalf("existing settings should be preserved, got %v", settings["theme"])
	}
	mcp := settings["mcp"].(map[string]any)
	if got := stringSliceFromAny(mcp["allowed"]); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("mcp.allowed = %v, want [alpha beta]", got)
	}
	if _, ok := mcp["excluded"]; ok {
		t.Fatalf("mcp.excluded should be removed so managed servers are not shadowed: %v", mcp)
	}
	if mcp["serverCommand"] != "custom" {
		t.Fatalf("unrelated mcp settings should be preserved, got %v", mcp)
	}
	servers := settings["mcpServers"].(map[string]any)
	alpha := servers["alpha"].(map[string]any)
	if alpha["httpUrl"] != "https://mcp.example/mcp" {
		t.Fatalf("alpha.httpUrl = %v, want streamable HTTP URL", alpha["httpUrl"])
	}
	if _, ok := alpha["url"]; ok {
		t.Fatalf("alpha.url should be translated away for Gemini streamable HTTP config: %v", alpha)
	}
	if got := stringSliceFromAny(alpha["excludeTools"]); !reflect.DeepEqual(got, []string{"delete_order", "refund_order"}) {
		t.Fatalf("alpha.excludeTools = %v, want [delete_order refund_order]", got)
	}
	beta := servers["beta"].(map[string]any)
	if beta["command"] != "uvx" {
		t.Fatalf("stdio server should pass through unchanged, got %v", beta)
	}

	cleanup()
	if got := string(mustReadFile(t, settingsPath)); got != string(originalSettings) {
		t.Fatalf("settings should be restored\n got: %s\nwant: %s", got, originalSettings)
	}
}

func TestManagedProjectMCPConfigRejectsBadShapes(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	if _, err := ensureCursorProjectMCPConfig(cwd, json.RawMessage(`not json`), nil, slog.Default()); err == nil {
		t.Fatal("expected malformed cursor mcp_config to fail closed")
	}
	if _, err := ensureGeminiProjectMCPConfig(cwd, json.RawMessage(`{"mcpServers":{"bad":"not-object"}}`), nil, slog.Default()); err == nil {
		t.Fatal("expected malformed gemini mcp_config server entry to fail closed")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func stringSliceFromAny(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
