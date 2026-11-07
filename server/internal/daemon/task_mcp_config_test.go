package daemon

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeTaskMcpConfigPreservesRuntimeServers(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			home := t.TempDir()
			redirectTestHome(t, home)
			t.Setenv("CODEX_HOME", "")
			configPath := filepath.Join(home, ".claude.json")
			config := `{"mcpServers":{"multica_developer_logs":{"command":"fake-logs-server"}}}`
			if provider == "codex" {
				configPath = filepath.Join(home, ".codex", "config.toml")
				if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
					t.Fatal(err)
				}
				config = "[mcp_servers.multica_developer_logs]\ncommand = \"fake-logs-server\"\n"
			}
			if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name   string
				config json.RawMessage
			}{
				{"absent", nil},
				{"null", json.RawMessage(" null ")},
				{"empty-object", json.RawMessage(`{}`)},
				{"empty-managed", json.RawMessage(`{"mcpServers":{}}`)},
			} {
				t.Run(tc.name, func(t *testing.T) {
					for _, withTask := range []bool{false, true} {
						var taskConfig json.RawMessage
						if withTask {
							taskConfig = json.RawMessage(`{"mcpServers":{"task_plugin":{"command":"fake-plugin-server"}}}`)
						}
						merged, err := mergeTaskMcpConfig(provider, tc.config, taskConfig, slog.Default())
						if err != nil {
							t.Fatal(err)
						}
						if !withTask && (tc.name == "absent" || tc.name == "null") {
							if !bytes.Equal(merged, tc.config) {
								t.Fatalf("without task MCP, native inheritance changed: %s", merged)
							}
							continue
						}
						servers := taskMcpServers(t, merged)
						if got := servers["multica_developer_logs"]["command"]; got != "fake-logs-server" {
							t.Errorf("withTask=%v: runtime logs server lost: %s", withTask, merged)
						}
						_, hasTask := servers["task_plugin"]
						if hasTask != withTask {
							t.Errorf("withTask=%v: task server presence = %v", withTask, hasTask)
						}
					}
				})
			}
		})
	}
}

func TestMergeTaskMcpConfigPreservesPrecedenceAndDisabledServer(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	t.Setenv("CODEX_HOME", home)
	config := `[mcp_servers.multica_developer_logs]
command = "fake-logs-server"
enabled = false
[mcp_servers.shared]
command = "runtime-server"
[mcp_servers.task_shared]
command = "runtime-server"
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	task := json.RawMessage(`{"mcpServers":{"task_shared":{"command":"task-server"}}}`)
	for _, tc := range []struct {
		name   string
		agent  json.RawMessage
		shared string
	}{
		{"absent", nil, "runtime-server"},
		{"managed", json.RawMessage(`{"mcpServers":{"shared":{"command":"agent-server"},"task_shared":{"command":"agent-server"}}}`), "agent-server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			merged, err := mergeTaskMcpConfig("codex", tc.agent, task, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			servers := taskMcpServers(t, merged)
			if got := servers["multica_developer_logs"]["enabled"]; got != false {
				t.Errorf("disabled runtime server changed: %s", merged)
			}
			for name, want := range map[string]string{"shared": tc.shared, "task_shared": "task-server"} {
				if got := servers[name]["command"]; got != want {
					t.Errorf("%s command = %v, want %s", name, got, want)
				}
			}
		})
	}
}

func TestMergeTaskMcpConfigKeepsContributionsWhenRuntimeConfigInvalid(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	agent := json.RawMessage(`{"mcpServers":{"agent":{"command":"agent-server"}}}`)
	task := json.RawMessage(`{"mcpServers":{"task":{"command":"task-server"}}}`)
	merged, err := mergeTaskMcpConfig("codex", agent, task, logger)
	if err != nil {
		t.Fatal(err)
	}
	servers := taskMcpServers(t, merged)
	if len(servers) != 2 || servers["agent"] == nil || servers["task"] == nil {
		t.Fatalf("lost task contributions: %s", merged)
	}
	if !strings.Contains(logs.String(), "runtime merge failed") {
		t.Fatalf("missing runtime diagnostic: %s", logs.String())
	}
}

func TestMergeTaskMcpConfigRejectsInvalidTaskConfig(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	t.Setenv("CODEX_HOME", "")
	if _, err := mergeTaskMcpConfig("codex", nil, json.RawMessage("invalid"), slog.Default()); err == nil {
		t.Fatal("malformed task configuration was accepted")
	}
}

func TestMergeTaskMcpConfigNormalizesLegacyOpenCodeBeforeTaskOverlay(t *testing.T) {
	writeOpencodeRuntimeConfig(t, `{"runtime":{"command":"runtime-server"}}`)
	agent := json.RawMessage(`{"mcp":{"private":{"command":"agent-server"}}}`)
	task := json.RawMessage(`{"mcpServers":{"task":{"command":"task-server"}}}`)
	merged, err := mergeTaskMcpConfig("opencode", agent, task, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	servers := taskMcpServers(t, merged)
	if len(servers) != 3 || servers["runtime"] == nil || servers["private"] == nil || servers["task"] == nil {
		t.Fatalf("lost a configuration layer: %s", merged)
	}
}

func taskMcpServers(t *testing.T, raw json.RawMessage) map[string]map[string]any {
	t.Helper()
	var document struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return document.Servers
}
