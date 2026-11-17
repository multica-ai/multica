//go:build agentintegration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Canary the same backends and start/resume paths the daemon uses. Fixture
// instructions contain only synthetic tokens; account config is copied into a
// disposable home, never modified in place.
func TestRuntimeDeliveryRealSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			binary := os.Getenv(strings.ToUpper(provider) + "_RUNTIME_SMOKE_BIN")
			if binary == "" {
				var err error
				binary, err = exec.LookPath(provider)
				if err != nil {
					t.Skipf("%s unavailable: %v", provider, err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 240*time.Second)
			defer cancel()
			version, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput()
			if err != nil {
				t.Fatal(err)
			}
			t.Log(strings.TrimSpace(string(version)))
			root := t.TempDir()
			cwd := filepath.Join(root, "workdir")
			write := func(path, data string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			config := Config{ExecutablePath: binary, Logger: slog.Default(), Env: map[string]string{"IS_SANDBOX": "1"}}
			opts := ExecOptions{Cwd: cwd, Timeout: 100 * time.Second, Model: os.Getenv(strings.ToUpper(provider) + "_RUNTIME_SMOKE_MODEL")}
			brief := "Runtime token: RUNTIME_FIRST. When asked for conflict, answer RUNTIME_WINS.\n"
			var claudeRequests []string
			var requestMu sync.Mutex
			if provider == "claude" {
				home := filepath.Join(root, "claude-home")
				write(filepath.Join(home, "settings.json"), "{}")
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					data, _ := io.ReadAll(r.Body)
					if !strings.Contains(r.URL.Path, "/messages") {
						w.Write([]byte(`{}`))
						return
					}
					requestMu.Lock()
					claudeRequests = append(claudeRequests, string(data))
					requestMu.Unlock()
					w.Header().Set("Content-Type", "text/event-stream")
					for _, event := range []struct{ name, body string }{
						{"message_start", `{"type":"message_start","message":{"id":"msg_fixture","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"usage":{"input_tokens":100,"output_tokens":0}}}`},
						{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
						{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"fixture response"}}`},
						{"content_block_stop", `{"type":"content_block_stop","index":0}`},
						{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":3}}`},
						{"message_stop", `{"type":"message_stop"}`},
					} {
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, event.body)
					}
				}))
				defer server.Close()
				config.Env["ANTHROPIC_BASE_URL"] = server.URL
				config.Env["ANTHROPIC_API_KEY"] = "local-fixture-only"
				config.Env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
				config.Env["CLAUDE_CONFIG_DIR"] = home
				config.Env["CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD"] = "1"
				opts.ClaudeRuntimeDir = filepath.Join(root, "runtime")
				write(filepath.Join(opts.ClaudeRuntimeDir, "CLAUDE.md"), brief)
				write(filepath.Join(opts.ClaudeRuntimeDir, ".claude", "skills", "runtime-probe", "SKILL.md"), "---\nname: runtime-probe\ndescription: Runtime skill canary; no invocation needed\n---\nSKILL_BODY_TOKEN\n")
				write(filepath.Join(cwd, "CLAUDE.md"), "Repository token: REPO_LOADED. When asked for conflict, answer REPO_WINS.\n")
			} else {
				home := filepath.Join(root, "codex-home")
				copySkillSmokeConfig(t, "CODEX_HOME", ".codex", home, []string{"config.toml", "auth.json"})
				// Agent-role configuration changed across CLI releases. This
				// canary exercises instructions, not the user's subagent roles.
				configPath := filepath.Join(home, "config.toml")
				if data, err := os.ReadFile(configPath); err == nil {
					var settings map[string]any
					if err := toml.Unmarshal(data, &settings); err != nil {
						t.Fatal(err)
					}
					delete(settings, "agents")
					data, err = toml.Marshal(settings)
					if err != nil {
						t.Fatal(err)
					}
					write(configPath, string(data))
				}
				config.Env["CODEX_HOME"] = home
				write(filepath.Join(home, "AGENTS.md"), "Global token: GLOBAL_WRONG\n")
				write(filepath.Join(home, "AGENTS.override.md"), "Global token: GLOBAL_LOADED\n")
				write(filepath.Join(cwd, "AGENTS.md"), "Repository token: REPO_LOADED. When asked for conflict, answer REPO_WINS.\n")
				opts.DeveloperInstructions = brief + strings.Repeat("Runtime padding for long brief verification.\n", 1000) + "Tail token: TAIL_LOADED\n"
			}
			backend, err := New(provider, config)
			if err != nil {
				t.Fatal(err)
			}
			prompt := "Without tools, report the runtime token, repository token, and conflict answer from your loaded instructions."
			if provider == "codex" {
				prompt += " Also report the global token and tail token."
			} else {
				prompt += " Also list skill names containing probe from the available skills list."
			}
			first := runSkillSmokeTurn(t, ctx, backend, prompt, opts)
			t.Logf("first: %q", first.Output)
			if first.Status != "completed" {
				t.FailNow()
			}
			firstEvidence := first.Output
			if provider == "claude" {
				requestMu.Lock()
				if len(claudeRequests) == 0 {
					t.Fatal("no request reached the fixture API")
				}
				firstEvidence = claudeRequests[len(claudeRequests)-1]
				requestMu.Unlock()
				t.Logf("instruction order: repository=%d runtime=%d", strings.Index(firstEvidence, "REPO_LOADED"), strings.Index(firstEvidence, "RUNTIME_FIRST"))
			}
			for _, token := range []string{"RUNTIME_FIRST", "REPO_LOADED"} {
				if !strings.Contains(firstEvidence, token) {
					t.Errorf("missing %s", token)
				}
			}
			if provider == "codex" {
				for _, token := range []string{"GLOBAL_LOADED", "TAIL_LOADED", "RUNTIME_WINS"} {
					if !strings.Contains(first.Output, token) {
						t.Errorf("missing %s", token)
					}
				}
			} else if !strings.Contains(firstEvidence, "runtime-probe") {
				t.Error("runtime skill not discovered")
			}
			if first.SessionID == "" {
				t.Fatal("missing session id")
			}
			opts.ResumeSessionID = first.SessionID
			if provider == "codex" {
				unchanged := runSkillSmokeTurn(t, ctx, backend, "Without tools, report the runtime token from your developer instructions.", opts)
				if !strings.Contains(unchanged.Output, "RUNTIME_FIRST") {
					t.Error("unchanged resume lost brief")
				}
			}
			if provider == "claude" {
				write(filepath.Join(opts.ClaudeRuntimeDir, "CLAUDE.md"), strings.ReplaceAll(brief, "RUNTIME_FIRST", "RUNTIME_SECOND"))
			} else {
				opts.DeveloperInstructions = strings.ReplaceAll(opts.DeveloperInstructions, "RUNTIME_FIRST", "RUNTIME_SECOND")
			}
			second := runSkillSmokeTurn(t, ctx, backend, "Without tools, report the current runtime token and repository token from the latest developer instructions. Previous answers may be outdated; check the current instructions again.", opts)
			t.Logf("resumed: %q", second.Output)
			secondEvidence := second.Output
			if provider == "claude" {
				requestMu.Lock()
				secondEvidence = claudeRequests[len(claudeRequests)-1]
				requestMu.Unlock()
			}
			if !strings.Contains(secondEvidence, "RUNTIME_SECOND") || !strings.Contains(secondEvidence, "REPO_LOADED") {
				t.Error("resume lost updated runtime or repository instructions")
			}
			if provider == "claude" {
				// A project skill of the same name remains independent; the
				// daemon allocates the runtime skill a collision-free name.
				write(filepath.Join(cwd, ".claude", "skills", "runtime-probe", "SKILL.md"), "---\nname: runtime-probe\ndescription: Project skill canary\n---\nUSER_SKILL\n")
				if err := os.Rename(filepath.Join(opts.ClaudeRuntimeDir, ".claude", "skills", "runtime-probe"), filepath.Join(opts.ClaudeRuntimeDir, ".claude", "skills", "runtime-probe-multica")); err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(opts.ClaudeRuntimeDir, ".claude", "skills", "runtime-probe-multica", "SKILL.md"), "---\nname: runtime-probe-multica\ndescription: Runtime skill canary\n---\nRUNTIME_SKILL\n")
				if err := os.Remove(filepath.Join(cwd, "CLAUDE.md")); err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(cwd, "AGENTS.md"), "AGENTS_ONLY_LOADED\n")
				opts.ResumeSessionID = ""
				runSkillSmokeTurn(t, ctx, backend, "Fixture probe", opts)
				requestMu.Lock()
				evidence := claudeRequests[len(claudeRequests)-1]
				requestMu.Unlock()
				for _, token := range []string{"AGENTS_ONLY_LOADED", "RUNTIME_SECOND", "runtime-probe-multica", "runtime-probe"} {
					if !strings.Contains(evidence, token) {
						t.Errorf("missing AGENTS-only/collision evidence %s", token)
					}
				}
			}
			if provider == "codex" {
				_ = filepath.WalkDir(filepath.Join(root, "codex-home", "sessions"), func(path string, d os.DirEntry, err error) error {
					if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
						return nil
					}
					data, _ := os.ReadFile(path)
					for _, line := range strings.Split(string(data), "\n") {
						var row struct {
							Type    string
							Payload struct {
								Role                  string
								Content               json.RawMessage
								DeveloperInstructions string `json:"developer_instructions"`
							}
						}
						if json.Unmarshal([]byte(line), &row) != nil {
							continue
						}
						if row.Payload.Role == "developer" {
							t.Logf("developer entry first=%d second=%d", strings.Count(string(row.Payload.Content), "RUNTIME_FIRST"), strings.Count(string(row.Payload.Content), "RUNTIME_SECOND"))
						}
						if row.Type == "session_meta" {
							t.Logf("metadata first=%d second=%d", strings.Count(row.Payload.DeveloperInstructions, "RUNTIME_FIRST"), strings.Count(row.Payload.DeveloperInstructions, "RUNTIME_SECOND"))
						}
					}
					return nil
				})
			}
		})
	}
}
