package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A test-owned executable supports both discovery and ACP, so the daemon's
// production command binding and model precedence remain in the tested path.
func TestTomcatDaemonModelsHelperProcess(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	args := os.Args[separator+1:]
	if len(args) < 3 || args[1] != "profile with spaces" {
		os.Exit(91)
	}
	root := args[0]
	trace, err := os.OpenFile(filepath.Join(root, "trace.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(92)
	}
	log := json.NewEncoder(trace)
	_ = log.Encode(map[string]any{"args": args})
	if slices.Equal(args[2:], []string{"serve", "models"}) {
		if _, err := os.Stat(filepath.Join(root, "discovery-error")); err == nil {
			fmt.Fprintln(os.Stdout, "PRIVATE_STDOUT_SENTINEL")
			fmt.Fprintln(os.Stderr, "error: unrecognized subcommand 'models'")
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, `{"defaultModelId":"team/alias","models":[{"id":"team/alias","provider":"team","keyPresent":true,"modelName":"wire-model","baseUrl":"PRIVATE_LOCAL_METADATA"},{"id":"missing-key","provider":"other","keyPresent":false}]}`)
		os.Exit(0)
	}
	if !slices.Equal(args[2:], []string{"acp"}) {
		os.Exit(93)
	}
	effort := "default"
	resumeOptions := func() []any {
		return []any{
			map[string]any{"id": "model", "currentValue": "saved-session-model"},
			map[string]any{"id": "effort", "type": "select", "currentValue": effort, "options": []any{
				map[string]any{"value": "default", "name": "Default"},
				map[string]any{"value": "high", "name": "High"},
			}},
		}
	}
	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(94)
		}
		_ = log.Encode(req)
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"mcpCapabilities": map[string]any{"http": true, "sse": true}}}
		case "session/new":
			result = map[string]any{"sessionId": "test-session", "configOptions": []any{map[string]any{"id": "model", "currentValue": "tomcat-default"}}}
		case "session/load":
			result = map[string]any{"configOptions": resumeOptions()}
		case "session/set_config_option":
			if req.Params["configId"] == "effort" {
				effort = fmt.Sprint(req.Params["value"])
				result = map[string]any{"configOptions": resumeOptions()}
			} else {
				result = map[string]any{"configOptions": []any{map[string]any{"id": "model", "currentValue": req.Params["value"]}}}
			}
		case "session/prompt":
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "test-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "done"}}}})
			result = map[string]any{"stopReason": "end_turn"}
		default:
			os.Exit(95)
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}

func tomcatDaemonModelCommand(t *testing.T) (profileLaunchSpec, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	return profileLaunchSpec{path: self, version: "0.1.97", fixedArgs: []string{"-test.run=^TestTomcatDaemonModelsHelperProcess$", "--", root, "profile with spaces"}}, root
}

func TestTomcatTaskModelPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, selected, fallback, want string
		resume                         bool
	}{
		{name: "agent wins", selected: "manual-not-in-catalog", fallback: "daemon-default", want: "manual-not-in-catalog"},
		{name: "daemon default", fallback: "daemon-default", want: "daemon-default"},
		{name: "Tomcat default"},
		{name: "resumed effort uses session model", resume: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirectTestHome(t, t.TempDir())
			t.Setenv("MULTICA_AGENT_TEMP_BASE", t.TempDir())
			spec, root := tomcatDaemonModelCommand(t)
			// A legacy CLI's failed discovery must not clear a saved manual ID or
			// prevent the existing default/ACP path from running.
			if !tc.resume {
				if err := os.WriteFile(filepath.Join(root, "discovery-error"), []byte("old CLI"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
			defer srv.Close()
			d := &Daemon{
				client: NewClient(srv.URL), logger: logger,
				workspaces: make(map[string]*workspaceState), activeEnvRoots: make(map[string]int),
				runtimeIndex:       map[string]Runtime{"test-rt": {ID: "test-rt", Provider: "tomcat", ProfileID: "test-profile"}},
				profileLaunchSpecs: map[string]profileLaunchSpec{"test-profile": spec},
				cfg: Config{WorkspacesRoot: t.TempDir(), AgentTimeout: 10 * time.Second, ServerBaseURL: srv.URL,
					Agents: map[string]AgentEntry{"tomcat": {Path: filepath.Join(root, "must-not-run-builtin"), Model: tc.fallback}}},
			}
			task := Task{ID: "test-task", WorkspaceID: "test-ws", RuntimeID: "test-rt", IssueID: "test-issue", AgentID: "test-agent", AuthToken: "mat_fixture",
				Agent: &AgentData{ID: "test-agent", Name: "test", Model: tc.selected}}
			if tc.resume {
				// Keep the real resume reachability gate: this test owns the local directory.
				task.PriorSessionID = "test-session"
				task.PriorWorkDir = t.TempDir()
				task.Agent.ThinkingLevel = "high"
				d.cfg.DaemonID = "test-daemon"
				d.localPathLocks = NewLocalPathLocker()
				ref, err := json.Marshal(localDirectoryRef{LocalPath: task.PriorWorkDir, DaemonID: d.cfg.DaemonID})
				if err != nil {
					t.Fatal(err)
				}
				task.ProjectResources = []ProjectResourceData{{ID: "test-dir", ResourceType: localDirectoryResourceType, ResourceRef: ref}}
			}
			result, err := d.runTask(t.Context(), task, "tomcat", 0, logger)
			if err != nil || result.Status != "completed" || result.Comment != "done" {
				t.Fatalf("runTask: %+v, %v", result, err)
			}
			trace, err := os.ReadFile(filepath.Join(root, "trace.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			var overrides, calls []string
			for _, line := range strings.Split(strings.TrimSpace(string(trace)), "\n") {
				var record struct {
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatal(err)
				}
				if record.Method != "" {
					calls = append(calls, record.Method)
				}
				if record.Method == "session/load" && record.Params["sessionId"] != task.PriorSessionID {
					t.Fatalf("loaded the wrong session: %+v", record)
				}
				if record.Method == "session/set_config_option" {
					if tc.resume {
						if record.Params["configId"] != "effort" || record.Params["value"] != "high" || record.Params["sessionId"] != task.PriorSessionID {
							t.Fatalf("resumed effort was changed using the global default: %+v", record)
						}
					} else {
						if record.Params["configId"] != "model" {
							t.Fatalf("wrong config: %+v", record)
						}
						overrides = append(overrides, fmt.Sprint(record.Params["value"]))
					}
				}
			}
			if tc.resume {
				if strings.Contains(string(trace), `"serve","models"`) {
					t.Fatal("a resumed session's model must not be inferred from the global catalog")
				}
				if !slices.Equal(calls, []string{"initialize", "session/load", "session/set_config_option", "session/prompt"}) {
					t.Fatalf("resume setup order = %v", calls)
				}
			}
			if (tc.want == "" && len(overrides) != 0) || (tc.want != "" && !slices.Equal(overrides, []string{tc.want})) {
				t.Fatalf("ACP overrides=%v, want %q (blank means no override)", overrides, tc.want)
			}
		})
	}
}
