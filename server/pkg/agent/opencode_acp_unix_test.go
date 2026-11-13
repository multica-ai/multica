//go:build unix

package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeOpencodeACPScript speaks enough of `opencode acp` to drive executeACP,
// with the frame shapes recorded from opencode-ai 1.19.4 and @opencode/cli
// 2.2.0. FAKE_MODE=cancel holds the prompt open until session/cancel arrives.
// `export` / `session export` print a transcript whose old message (created 1)
// must not be billed to this turn.
func fakeOpencodeACPScript() string {
	return `#!/bin/sh
if [ "$1" = "export" ] || [ "$1" = "session" ]; then
  printf '%s\n' "$*" >> "$FAKE_LOG.export"
  printf '%s' '{"info":{"id":"ses_fake"},"messages":[{"info":{"role":"assistant","time":{"created":1},"tokens":{"input":999,"output":999,"cache":{"read":0,"write":0}}}},{"info":{"role":"assistant","time":{"created":9999999999999},"tokens":{"input":40,"output":7,"cache":{"read":3,"write":0}}}}]}'
  exit 0
fi
prompt_id=""
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$FAKE_LOG"
  id=$(printf '%s' "$line" | sed -n 's/^{"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      if [ "$FAKE_MODE" = "stall-setup" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
        exec sleep 60
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":false}}}}\n' "$id" ;;
    *'"method":"session/new"'*|*'"method":"session/load"'*)
      if [ "$FAKE_MODE" = "stall" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
        exec sleep 60
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake","configOptions":[{"id":"model","category":"model","type":"select","currentValue":"opencode/default","options":[{"value":"opencode/default"},{"value":"opencode/picked"}]}]}}\n' "$id" ;;
    *'"method":"session/set_config_option"'*'"configId":"model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"model","category":"model","currentValue":"opencode/picked","options":[{"value":"opencode/picked"}]},{"id":"effort","category":"thought_level","currentValue":"default","options":[{"value":"high"},{"value":"default"}]}]}}\n' "$id" ;;
    *'"method":"session/set_config_option"'*'"configId":"effort"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"effort","category":"thought_level","currentValue":"high","options":[{"value":"high"},{"value":"default"}]}]}}\n' "$id" ;;
    *'"method":"session/prompt"'*)
      prompt_id=$id
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake","update":{"sessionUpdate":"tool_call","toolCallId":"call-1","name":"bash","title":"bash","kind":"execute","status":"pending","rawInput":{"cwd":"/w"}}}}'
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake","update":{"sessionUpdate":"tool_call_update","toolCallId":"call-1","status":"in_progress","kind":"execute","title":"sleep 1","rawInput":{"command":"sleep 1","cwd":"/w"}}}}'
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake","update":{"sessionUpdate":"tool_call_update","toolCallId":"call-1","status":"in_progress","kind":"execute","title":"sleep 1","rawInput":{"command":"sleep 1","cwd":"/w"}}}}'
      if [ "$FAKE_MODE" = "cancel" ]; then continue; fi
      if [ "$FAKE_MODE" = "ignore-cancel" ]; then exec sleep 60; fi
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"CHILD-TEXT"},"_meta":{"opencode/child-session":{"id":"ses_child","parentID":"ses_fake"}}}}}'
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake","update":{"sessionUpdate":"tool_call_update","toolCallId":"call-1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"slept"}}],"rawOutput":{"metadata":{"exit":0}}}}}'
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"parent answer"}}}}'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":1}}}\n' "$id" ;;
    *'"method":"session/cancel"'*)
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake","update":{"sessionUpdate":"tool_call_update","toolCallId":"call-1","status":"failed","content":[{"type":"content","content":{"type":"text","text":"Tool execution interrupted"}}]}}}'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"cancelled"}}\n' "$prompt_id" ;;
  esac
done
`
}

func newFakeOpencodeACP(t *testing.T, version, mode string) (Backend, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "opencode")
	writeTestExecutable(t, bin, []byte(fakeOpencodeACPScript()))
	logPath := filepath.Join(dir, "requests.log")
	backend, err := New("opencode", Config{
		ExecutablePath: bin,
		CLIVersion:     version,
		BuiltinRuntime: true,
		Logger:         slog.Default(),
		Env:            map[string]string{"FAKE_LOG": logPath, "FAKE_MODE": mode},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}
	return backend, logPath
}

func TestOpencodeACPStreamsToolStartAndSumsTurnUsage(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"1.19.4", "opencode v2.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			backend, logPath := newFakeOpencodeACP(t, version, "")
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			mcp := `{"mcpServers":{"probe":{"command":"node","args":["s.js"]}}}`
			session, err := backend.Execute(ctx, "do it", ExecOptions{
				Cwd:           t.TempDir(),
				Model:         "opencode/picked",
				ThinkingLevel: "high",
				McpConfig:     []byte(mcp),
				Timeout:       15 * time.Second,
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			var msgs []Message
			for m := range session.Messages {
				msgs = append(msgs, m)
			}
			res := <-session.Result
			if res.Status != "completed" || res.SessionID != "ses_fake" {
				t.Fatalf("result = %+v", res)
			}
			if res.Output != "parent answer" {
				t.Fatalf("output = %q: a subagent's text must not reach the parent's output", res.Output)
			}
			// The tool is announced once, from the in_progress frame, with the
			// real input rather than the start frame's placeholder.
			var uses, results []Message
			for _, m := range msgs {
				switch m.Type {
				case MessageToolUse:
					uses = append(uses, m)
				case MessageToolResult:
					results = append(results, m)
				}
			}
			if len(uses) != 1 || uses[0].Tool != "bash" || uses[0].CallID != "call-1" || uses[0].Input["command"] != "sleep 1" {
				t.Fatalf("tool uses = %+v", uses)
			}
			if len(results) != 1 || results[0].CallID != "call-1" || results[0].Output != "slept" {
				t.Fatalf("tool results = %+v", results)
			}
			// Billed from the export, this turn's messages only.
			if u := res.Usage["opencode/picked"]; u.InputTokens != 40 || u.OutputTokens != 7 || u.CacheReadTokens != 3 {
				t.Fatalf("usage = %+v", res.Usage)
			}

			raw, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			requests := string(raw)
			for _, want := range []string{`"configId":"model","sessionId":"ses_fake","value":"opencode/picked"`, `"configId":"effort","sessionId":"ses_fake","value":"high"`} {
				if !strings.Contains(requests, want) {
					t.Fatalf("missing request %s in:\n%s", want, requests)
				}
			}
			// 2.x receives MCP in session/new; 1.x keeps OPENCODE_CONFIG_CONTENT.
			newHasMCP := strings.Contains(requests, `"name":"probe"`)
			if newHasMCP != (version != "1.19.4") {
				t.Fatalf("session/new carried MCP = %v for %s:\n%s", newHasMCP, version, requests)
			}
			export, _ := os.ReadFile(logPath + ".export")
			if version == "1.19.4" && !strings.HasPrefix(string(export), "export ses_fake") ||
				version != "1.19.4" && !strings.Contains(string(export), "session export ses_fake --standalone") {
				t.Fatalf("export argv = %q", export)
			}
		})
	}
}

func TestOpencodeACPCancelSendsSessionCancel(t *testing.T) {
	t.Parallel()
	backend, logPath := newFakeOpencodeACP(t, "opencode v2.2.0", "cancel")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "do it", ExecOptions{Cwd: t.TempDir(), Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for m := range session.Messages {
			if m.Type == MessageToolUse {
				cancel()
			}
		}
	}()
	select {
	case res := <-session.Result:
		if res.Status != "aborted" {
			t.Fatalf("result = %+v", res)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("cancelled run did not finish")
	}
	raw, _ := os.ReadFile(logPath)
	if !strings.Contains(string(raw), `"method":"session/cancel"`) {
		t.Fatalf("session/cancel was not sent:\n%s", raw)
	}
}

// A runtime that stops reading stdin mid-prompt must not outlive the run's
// timeout: the blocked prompt write holds the client's write lock, so the
// teardown cannot wait on a session/cancel write (review of #9619).
func TestOpencodeACPTimeoutTearsDownStalledPromptWrite(t *testing.T) {
	t.Parallel()
	backend, _ := newFakeOpencodeACP(t, "1.19.4", "stall")
	started := time.Now()
	session, err := backend.Execute(context.Background(), strings.Repeat("x", 1<<20), ExecOptions{Cwd: t.TempDir(), Timeout: time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	res := <-session.Result
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("teardown took %s; want it bounded by the timeout, not the runtime's own exit", elapsed)
	}
	if res.Status != "timeout" {
		t.Fatalf("result = %+v", res)
	}
}

// A runtime that received the prompt but ignores session/cancel is forced
// down after opencodeACPCancelGrace.
func TestOpencodeACPCancelIgnoredIsForced(t *testing.T) {
	t.Parallel()
	backend, _ := newFakeOpencodeACP(t, "opencode v2.2.0", "ignore-cancel")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "do it", ExecOptions{Cwd: t.TempDir(), Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var cancelledAt time.Time
	go func() {
		for m := range session.Messages {
			if m.Type == MessageToolUse && cancelledAt.IsZero() {
				cancelledAt = time.Now()
				cancel()
			}
		}
	}()
	select {
	case res := <-session.Result:
		if res.Status != "aborted" {
			t.Fatalf("result = %+v", res)
		}
	case <-time.After(opencodeACPCancelGrace + 6*time.Second):
		t.Fatal("cancelled run whose runtime ignored session/cancel was not torn down")
	}
}

// The same bound must hold before the prompt: a runtime that stops reading
// after initialize leaves session/new blocked in its stdin write (a large MCP
// payload fills the pipe), and the request checks its context only after the
// write returns (second review of #9619).
func TestOpencodeACPTimeoutTearsDownStalledSetupWrite(t *testing.T) {
	t.Parallel()
	backend, _ := newFakeOpencodeACP(t, "opencode v2.2.0", "stall-setup")
	mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"probe": map[string]any{
		"command": "node", "args": []string{"probe.js"}, "env": map[string]string{"BIG": strings.Repeat("x", 256<<10)},
	}}})
	started := time.Now()
	session, err := backend.Execute(context.Background(), "hello", ExecOptions{Cwd: t.TempDir(), McpConfig: mcp, Timeout: time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	res := <-session.Result
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("teardown took %s; want it bounded by the timeout, not the runtime's own exit", elapsed)
	}
	if res.Status != "timeout" {
		t.Fatalf("result = %+v", res)
	}
}
