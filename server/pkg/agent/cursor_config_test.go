package agent

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// Trimmed from the cli-config.json attached to #8701 after picking
// "Grok 4.7 500K High" in Cursor's /model, with the selection moved to another
// model so the rewrite has something to change.
const cursorTestCLIConfig = `{
  "permissions": {"allow": ["Shell(ls)"], "deny": []},
  "version": 1,
  "editor": {"vimMode": false},
  "model": {"modelId": "claude-opus-5", "displayName": "Claude Opus 5 300K", "maxMode": false},
  "maxMode": false,
  "modelParameters": {
    "grok-4.7": [{"id": "context", "value": "256k"}, {"id": "reasoning_effort", "value": "high"}, {"id": "fast", "value": "false"}],
    "claude-opus-5": [{"id": "context", "value": "300k"}]
  },
  "selectedModel": {"modelId": "claude-opus-5", "parameters": [{"id": "context", "value": "300k"}]},
  "authInfo": {"email": "user@example.com", "authId": "auth-123"},
  "privacyCache": {"ghostMode": true, "privacyMode": 2}
}`

func TestSplitCursorContextModel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		model, id, ctx string
		ok             bool
	}{
		{"grok-4.7[500k]", "grok-4.7", "500k", true},
		{"gpt-5.6-luna[1m]", "gpt-5.6-luna", "1m", true},
		{"grok-4.7-high", "", "", false},
		{"grok-4.7[high]", "", "", false},
		{"[500k]", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		id, ctx, ok := splitCursorContextModel(tc.model)
		if id != tc.id || ctx != tc.ctx || ok != tc.ok {
			t.Errorf("splitCursorContextModel(%q) = (%q, %q, %v), want (%q, %q, %v)", tc.model, id, ctx, ok, tc.id, tc.ctx, tc.ok)
		}
	}
}

func TestRewriteCursorCLIConfigSelectsContextAndKeepsOtherKeys(t *testing.T) {
	t.Parallel()

	out, err := rewriteCursorCLIConfig([]byte(cursorTestCLIConfig), "grok-4.7", "500k")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}

	wantParams := []any{
		map[string]any{"id": "context", "value": "500k"},
		map[string]any{"id": "reasoning_effort", "value": "high"},
		map[string]any{"id": "fast", "value": "false"},
	}
	assertJSONEqual(t, "selectedModel", got["selectedModel"], map[string]any{"modelId": "grok-4.7", "parameters": wantParams})
	params := got["modelParameters"].(map[string]any)
	assertJSONEqual(t, "modelParameters[grok-4.7]", params["grok-4.7"], wantParams)
	assertJSONEqual(t, "modelParameters[claude-opus-5]", params["claude-opus-5"], []any{map[string]any{"id": "context", "value": "300k"}})
	if got["maxMode"] != true {
		t.Errorf("maxMode = %v, want true", got["maxMode"])
	}
	model := got["model"].(map[string]any)
	if model["modelId"] != "grok-4.7" || model["maxMode"] != true || model["displayName"] != "grok-4.7" {
		t.Errorf("model = %v, want grok-4.7 with maxMode and no stale display name", model)
	}
	// Auth and unrelated settings survive untouched.
	assertJSONEqual(t, "authInfo", got["authInfo"], map[string]any{"email": "user@example.com", "authId": "auth-123"})
	assertJSONEqual(t, "privacyCache", got["privacyCache"], map[string]any{"ghostMode": true, "privacyMode": float64(2)})
	assertJSONEqual(t, "permissions", got["permissions"], map[string]any{"allow": []any{"Shell(ls)"}, "deny": []any{}})
	if got["version"] != float64(1) {
		t.Errorf("version = %v, want 1", got["version"])
	}
}

func TestRewriteCursorCLIConfigAddsMissingModelParameters(t *testing.T) {
	t.Parallel()

	out, err := rewriteCursorCLIConfig([]byte(`{"version":1}`), "grok-4.7", "500k")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, "modelParameters", got["modelParameters"], map[string]any{"grok-4.7": []any{map[string]any{"id": "context", "value": "500k"}}})

	if _, err := rewriteCursorCLIConfig([]byte(`[]`), "grok-4.7", "500k"); err == nil {
		t.Error("non-object config: want error")
	}
}

func TestPrepareCursorContextConfigDirLeavesUserConfigAlone(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs Developer Mode or admin rights on Windows")
	}

	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, cursorCLIConfigFile), []byte(cursorTestCLIConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := cursorSourceConfigDir(map[string]string{"CURSOR_CONFIG_DIR": source})
	if err != nil || resolved != source {
		t.Fatalf("cursorSourceConfigDir = %q, %v; want %q", resolved, err, source)
	}

	dir, err := prepareCursorContextConfigDir(t.TempDir(), source, "grok-4.7", "500k")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for _, name := range []string{"mcp.json", "skills"} {
		target, err := os.Readlink(filepath.Join(dir, name))
		if err != nil || target != filepath.Join(source, name) {
			t.Errorf("%s link = %q, %v; want %q", name, target, err, filepath.Join(source, name))
		}
	}
	info, err := os.Lstat(filepath.Join(dir, cursorCLIConfigFile))
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("cli-config.json must be a private copy, got mode %v, err %v", info, err)
	}
	copied, _ := os.ReadFile(filepath.Join(dir, cursorCLIConfigFile))
	var cfg map[string]any
	if err := json.Unmarshal(copied, &cfg); err != nil || cfg["maxMode"] != true {
		t.Fatalf("copied config not rewritten: %s", copied)
	}
	original, _ := os.ReadFile(filepath.Join(source, cursorCLIConfigFile))
	if string(original) != cursorTestCLIConfig {
		t.Fatal("user's cli-config.json was modified")
	}
}

func TestBuildCursorArgsContextTaggedModelOmitsModelFlag(t *testing.T) {
	t.Parallel()

	args := buildCursorArgs(ExecOptions{Model: "grok-4.7[500k]"}, slog.Default())
	if slices.Contains(args, "--model") {
		t.Fatalf("args = %v; a context-tagged model is selected via cli-config.json, not --model", args)
	}
	args = buildCursorArgs(ExecOptions{Model: "grok-4.7-high"}, slog.Default())
	if i := slices.Index(args, "--model"); i < 0 || args[i+1] != "grok-4.7-high" {
		t.Fatalf("args = %v; an untagged model keeps --model", args)
	}
}

func assertJSONEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("%s = %s, want %s", label, g, w)
	}
}
