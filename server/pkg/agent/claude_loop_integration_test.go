//go:build agentintegration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercises the installed CLI's /loop expansion and timers through Execute.
// Only the Messages API is mocked; no authenticated model account is used.
func TestClaudeNativeLoopIntegration(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to run the installed Claude CLI against a local mock API")
	}
	executable := os.Getenv("MULTICA_CLAUDE_EXECUTABLE")
	if executable == "" {
		executable = "claude"
	}
	executable, err := exec.LookPath(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"dynamic", "cron"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			config := filepath.Join(root, "config")
			if err := os.Mkdir(config, 0700); err != nil {
				t.Fatal(err)
			}
			cached := fmt.Sprintf(`{"cachedGrowthBookFeatures":{"tengu_kairos_loop_dynamic":true,"tengu_kairos_loop_prompt":true,"tengu_kairos_cron":true},"cachedGrowthBookFeaturesAt":%d}`, time.Now().UnixMilli())
			if err := os.WriteFile(filepath.Join(config, ".claude.json"), []byte(cached), 0600); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, "/messages") {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{}`)
					return
				}
				var req struct {
					Model    string          `json:"model"`
					Stream   bool            `json:"stream"`
					Messages json.RawMessage `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					http.Error(w, "bad request", 400)
					return
				}
				n := calls.Add(1)
				var block map[string]any
				switch n {
				case 1:
					name, input := "ScheduleWakeup", map[string]any{"delaySeconds": 60, "reason": "bounded native loop smoke test", "prompt": "Stop the loop and say second tick finished.", "noop": false}
					if kind == "cron" {
						name = "CronCreate"
						input = map[string]any{"cron": "* * * * *", "recurring": true, "prompt": "Delete this cron and say second tick finished."}
					}
					block = map[string]any{"type": "tool_use", "id": "call-arm", "name": name, "input": input}
				case 3:
					name, input := "ScheduleWakeup", map[string]any{"stop": true}
					if kind == "cron" {
						match := regexp.MustCompile(`Scheduled recurring job ([a-z0-9]+)`).FindSubmatch(req.Messages)
						if len(match) != 2 {
							t.Error("native CronCreate receipt missing from conversation")
							http.Error(w, "missing cron", 500)
							return
						}
						name = "CronDelete"
						input = map[string]any{"id": string(match[1])}
					}
					block = map[string]any{"type": "tool_use", "id": "call-stop", "name": name, "input": input}
				default:
					value := "first tick finished"
					if n > 2 {
						value = "second tick finished"
					}
					block = map[string]any{"type": "text", "text": value}
				}
				stop := "end_turn"
				if block["type"] == "tool_use" {
					stop = "tool_use"
				}
				message := map[string]any{"id": fmt.Sprintf("msg-%d", n), "type": "message", "role": "assistant", "model": req.Model, "content": []any{block}, "stop_reason": stop, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 5}}
				if !req.Stream {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(message)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				emit := func(event string, data map[string]any) {
					data["type"] = event
					b, _ := json.Marshal(data)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
					w.(http.Flusher).Flush()
				}
				message["content"] = []any{}
				message["stop_reason"] = nil
				message["usage"] = map[string]int{"input_tokens": 10, "output_tokens": 0}
				emit("message_start", map[string]any{"message": message})
				start, delta := map[string]any{"type": "text", "text": ""}, map[string]any{"type": "text_delta", "text": block["text"]}
				if stop == "tool_use" {
					b, _ := json.Marshal(block["input"])
					start = map[string]any{"type": "tool_use", "id": block["id"], "name": block["name"], "input": map[string]any{}}
					delta = map[string]any{"type": "input_json_delta", "partial_json": string(b)}
				}
				emit("content_block_start", map[string]any{"index": 0, "content_block": start})
				emit("content_block_delta", map[string]any{"index": 0, "delta": delta})
				emit("content_block_stop", map[string]any{"index": 0})
				emit("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 5}})
				emit("message_stop", map[string]any{})
			}))
			defer server.Close()
			// Override inherited credentials and feature opt-outs in this subprocess.
			// The isolated config and dummy key never use the developer's account.
			env := map[string]string{}
			for _, item := range os.Environ() {
				key, _, _ := strings.Cut(item, "=")
				if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") {
					env[key] = ""
				}
			}
			for _, key := range []string{"DISABLE_TELEMETRY", "DO_NOT_TRACK", "DISABLE_ERROR_REPORTING"} {
				env[key] = ""
			}
			env["ANTHROPIC_BASE_URL"] = server.URL
			env["ANTHROPIC_API_KEY"] = "local-test-key"
			env["CLAUDE_CONFIG_DIR"] = config
			env["IS_SANDBOX"] = "1"
			backend, err := New("claude", Config{ExecutablePath: executable, Env: env, Logger: slog.Default()})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			prompt := "/loop Run the bounded native loop smoke check and stop after the second tick."
			if kind == "cron" {
				prompt = "/loop 1m Run the bounded native loop smoke check and delete the cron after the second tick."
			}
			start := time.Now()
			session, err := backend.Execute(ctx, prompt, ExecOptions{Cwd: root, Model: "claude-sonnet-4-6", Timeout: 145 * time.Second, ThinkingLevel: "low", McpConfig: json.RawMessage(`{"mcpServers":{}}`), CustomArgs: []string{"--strict-mcp-config", "--setting-sources", ""}})
			if err != nil {
				t.Fatal(err)
			}
			waited := false
			sessionID := ""
			for msg := range session.Messages {
				if msg.SessionID != "" {
					if sessionID != "" && sessionID != msg.SessionID {
						t.Error("native loop changed session")
					}
					sessionID = msg.SessionID
				}
				if !msg.WaitingUntil.IsZero() {
					waited = true
					t.Logf("native wait until %s", msg.WaitingUntil)
				}
			}
			result := <-session.Result
			if !waited || calls.Load() != 4 || result.Status != "completed" || result.Output != "second tick finished" {
				t.Fatalf("waited=%v requests=%d result=%+v", waited, calls.Load(), result)
			}
			if kind == "dynamic" && time.Since(start) < time.Minute {
				t.Fatal("second turn did not wait for the native timer")
			}
			t.Logf("native %s completed two turns and stopped in %s; session=%s", kind, time.Since(start), sessionID)
		})
	}
}
