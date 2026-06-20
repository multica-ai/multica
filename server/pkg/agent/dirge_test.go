package agent

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func quietDirgeLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildDirgeArgsBasic(t *testing.T) {
	t.Parallel()

	args := buildDirgeArgs(
		"fix the bug",
		"multica-session",
		ExecOptions{Model: "dirge-model", MaxTurns: 7},
		quietDirgeLogger(),
	)

	want := []string{
		"--print",
		"--yolo",
		"--auto-confirm", "yes",
		"--session", "multica-session",
		"--output-format", "json",
		"--model", "dirge-model",
		"--max-agent-turns", "7",
		"--", "fix the bug",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("buildDirgeArgs mismatch\n got: %v\nwant: %v", args, want)
	}
}

func TestBuildDirgeArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildDirgeArgs(
		"real prompt",
		"real-session",
		ExecOptions{
			CustomArgs: []string{
				"--output-format", "text",
				"--session", "evil-session",
				"--model", "evil-model",
				"--max-agent-turns", "99",
				"--auto-confirm", "no",
				"--yolo",
				"--verbose",
			},
		},
		quietDirgeLogger(),
	)

	joined := strings.Join(args, " ")
	for _, banned := range []string{"text", "evil-session", "evil-model", "99", "--auto-confirm no"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("blocked custom arg leaked through: %q in %v", banned, args)
		}
	}
	if !slices.Contains(args, "--verbose") {
		t.Fatalf("non-blocked custom arg should pass through, got %v", args)
	}
	if args[len(args)-2] != "--" || args[len(args)-1] != "real prompt" {
		t.Fatalf("prompt must remain the final positional arg, got %v", args)
	}
}

func TestParseDirgeResult(t *testing.T) {
	t.Parallel()

	stdout := strings.Join([]string{
		"debug log line",
		`{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"inner","duration_ms":10}`,
	}, "\n")
	got, err := parseDirgeResult(stdout)
	if err != nil {
		t.Fatalf("parseDirgeResult: %v", err)
	}
	if got.Result != "done" || got.Subtype != "success" || got.IsError {
		t.Fatalf("unexpected parsed result: %#v", got)
	}
}

func TestPrepareDirgeConfigWritesManagedMcpServers(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baseDir, "config.json"), []byte(`{
  "theme": "dark",
  "mcp_servers": {
    "global": {"command": "old"}
  }
}`), 0o600); err != nil {
		t.Fatalf("seed base config: %v", err)
	}

	raw := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"],"env":{"TOKEN":"x"}}}}`)
	dir, cleanup, err := prepareDirgeConfig(raw, map[string]string{"DIRGE_CONFIG_DIR": baseDir})
	if err != nil {
		t.Fatalf("prepareDirgeConfig: %v", err)
	}
	defer cleanup()
	if dir == "" || dir == baseDir {
		t.Fatalf("expected a managed temp config dir, got %q", dir)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read managed config: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse managed config: %v\n%s", err, data)
	}
	if string(got["theme"]) != `"dark"` {
		t.Fatalf("non-MCP base config key was not preserved: %s", data)
	}
	if _, ok := got["mcpServers"]; ok {
		t.Fatalf("Dirge config must use mcp_servers, not mcpServers: %s", data)
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(got["mcp_servers"], &servers); err != nil {
		t.Fatalf("parse mcp_servers: %v", err)
	}
	if _, ok := servers["fetch"]; !ok {
		t.Fatalf("managed server missing from mcp_servers: %s", data)
	}
	if _, ok := servers["global"]; ok {
		t.Fatalf("managed mcp_config must replace inherited mcp_servers: %s", data)
	}
}

func TestPrepareDirgeConfigEmptyManagedSetClearsInheritedMcpServers(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baseDir, "config.json"), []byte(`{"mcp_servers":{"global":{"command":"old"}}}`), 0o600); err != nil {
		t.Fatalf("seed base config: %v", err)
	}

	dir, cleanup, err := prepareDirgeConfig(json.RawMessage(`{}`), map[string]string{"DIRGE_CONFIG_DIR": baseDir})
	if err != nil {
		t.Fatalf("prepareDirgeConfig: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read managed config: %v", err)
	}
	var got struct {
		McpServers map[string]json.RawMessage `json:"mcp_servers"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse managed config: %v", err)
	}
	if len(got.McpServers) != 0 {
		t.Fatalf("expected empty managed mcp_servers, got %s", data)
	}
}

func TestPrepareDirgeConfigNilDoesNotTakeOwnership(t *testing.T) {
	t.Parallel()

	dir, cleanup, err := prepareDirgeConfig(nil, nil)
	if err != nil {
		t.Fatalf("prepareDirgeConfig(nil): %v", err)
	}
	defer cleanup()
	if dir != "" {
		t.Fatalf("nil mcp_config should not create managed Dirge config, got %q", dir)
	}
}

func TestExtractDirgeMcpServersRejectsBadShapes(t *testing.T) {
	t.Parallel()

	for _, raw := range []json.RawMessage{
		json.RawMessage(`not json`),
		json.RawMessage(`{"mcpServers":[]}`),
		json.RawMessage(`{"mcpServers":{"bad":"not an object"}}`),
		json.RawMessage(`{"mcpServers":{"bad":{}}}`),
	} {
		if _, err := extractDirgeMcpServers(raw); err == nil {
			t.Fatalf("extractDirgeMcpServers(%s) succeeded, want error", raw)
		}
	}
}
