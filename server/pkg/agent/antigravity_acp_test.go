package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func startFakeAntigravityACP(t *testing.T, ctx context.Context, mode string, opts ExecOptions) *Session {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := New("antigravity", Config{
		ExecutablePath: executable,
		LaunchPrefix:   []string{"-test.run=^TestAntigravityACPFakeProcess$", "--"},
		Logger:         quietAntigravityLogger(),
		CLIVersion:     "1.5.15",
		Env:            map[string]string{"MULTICA_FAKE_ANTIGRAVITY_ACP_MODE": mode},
	})
	if err != nil {
		t.Fatal(err)
	}
	opts.CustomArgs = []string{antigravityACPArg, "--disable-telemetry"}
	opts.EnableTaskSupplement = true
	session, err := backend.Execute(ctx, "task prompt", opts)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func collectAntigravityACP(t *testing.T, session *Session) (Result, []Message) {
	t.Helper()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	var messages []Message
	for {
		select {
		case msg, ok := <-session.Messages:
			if ok {
				messages = append(messages, msg)
				continue
			}
			select {
			case result := <-session.Result:
				return result, messages
			case <-timer.C:
				t.Fatal("ACP result did not settle")
			}
		case <-timer.C:
			t.Fatal("ACP message stream did not close")
		}
	}
}

func TestAntigravityACPStreamAndResume(t *testing.T) {
	for _, dirMode := range []string{"managed clone", "local directory", "local worktree"} {
		t.Run(dirMode, func(t *testing.T) {
			cwd := filepath.Join(t.TempDir(), dirMode)
			if err := os.MkdirAll(filepath.Join(cwd, ".agents"), 0o755); err != nil {
				t.Fatal(err)
			}
			hooks := filepath.Join(cwd, ".agents", "hooks.json")
			if err := os.WriteFile(hooks, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			opts := ExecOptions{Cwd: cwd, Model: "claude-sonnet-test", Timeout: 5 * time.Second}
			newSession := startFakeAntigravityACP(t, t.Context(), "new", opts)
			if newSession.Supplement != nil || newSession.SupplementReady != nil {
				t.Fatal("concurrent prompts have no in-turn receipt")
			}
			result, messages := collectAntigravityACP(t, newSession)
			if result.Status != "completed" || result.SessionID != "session-1" || result.Output != "Final\n\nanswer answer" {
				t.Fatalf("new result = %+v", result)
			}
			if got := result.Usage["claude-sonnet-test"]; got.InputTokens != 17 || got.OutputTokens != 4 || got.CostUSDTicks != 0 {
				t.Fatalf("usage = %+v", result.Usage)
			}
			var kinds []MessageType
			var text, thoughts strings.Builder
			for _, msg := range messages {
				switch msg.Type {
				case MessageText:
					text.WriteString(msg.Content)
				case MessageThinking:
					thoughts.WriteString(msg.Content)
				case MessageToolUse:
					if msg.CallID != "call-1" || msg.Tool != "run_command" || msg.Input["command"] != "pwd" {
						t.Fatalf("tool invocation = %+v", msg)
					}
					kinds = append(kinds, msg.Type)
				case MessageToolResult:
					if msg.CallID != "call-1" || msg.Output != "ok" || msg.Status != "completed" {
						t.Fatalf("tool result = %+v", msg)
					}
					kinds = append(kinds, msg.Type)
				}
			}
			if text.String() != "CheckingFinal\n\nanswer answer" || thoughts.String() != "Thinking\nnext" ||
				!slices.Equal(kinds, []MessageType{MessageToolUse, MessageToolResult}) {
				t.Fatalf("unexpected stream: %+v", messages)
			}
			opts.ResumeSessionID = result.SessionID
			for _, mode := range []string{"resume", "load"} {
				resumed, _ := collectAntigravityACP(t, startFakeAntigravityACP(t, t.Context(), mode, opts))
				if resumed.Status != "completed" || resumed.SessionID != "session-1" || resumed.Output != "Continued" || resumed.Usage != nil {
					t.Fatalf("%s result = %+v", mode, resumed)
				}
			}
			if content, err := os.ReadFile(hooks); err != nil || string(content) != "{}" {
				t.Fatalf("workspace hook file changed: %q, %v", content, err)
			}
		})
	}
}

func TestAntigravityACPFailures(t *testing.T) {
	for _, tc := range []struct {
		mode, want       string
		resume, rejected bool
	}{
		{"auth", "agy acp --login", true, false},
		{"rejected", "session not found", true, true},
		{"missing-trajectory", "trajectory not found", true, true},
		{"changed", "different session", true, true},
		{"no-resume", "does not advertise", true, false},
		{"disconnect", "connection closed", true, false},
		{"bad-protocol", "protocol version 1", false, false},
		{"model-error", "model unavailable", false, false},
		{"queued-result", "queued", false, false},
		{"setup-timeout", "deadline exceeded", false, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			opts := ExecOptions{Cwd: t.TempDir(), Model: "claude-sonnet-test", Timeout: 5 * time.Second}
			if tc.resume {
				opts.ResumeSessionID = "session-1"
			}
			if tc.mode == "setup-timeout" {
				opts.HandshakeTimeout = 750 * time.Millisecond
			}
			result, _ := collectAntigravityACP(t, startFakeAntigravityACP(t, t.Context(), tc.mode, opts))
			if result.Status != "failed" || result.ResumeRejected != tc.rejected || !strings.Contains(result.Error, tc.want) {
				t.Fatalf("result = %+v, want %q rejection=%v", result, tc.want, tc.rejected)
			}
			if tc.resume && result.SessionID != opts.ResumeSessionID {
				t.Fatalf("existing session pointer lost: %+v", result)
			}
		})
	}
}

func TestAntigravityACPCancel(t *testing.T) {
	for _, status := range []string{"aborted", "timeout"} {
		t.Run(status, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cwd := t.TempDir()
			opts := ExecOptions{Cwd: cwd, Timeout: 5 * time.Second}
			if status == "timeout" {
				opts.Timeout = time.Second
			}
			session := startFakeAntigravityACP(t, ctx, "cancel", opts)
			timer := time.NewTimer(4 * time.Second)
			defer timer.Stop()
		waitPrompt:
			for {
				select {
				case msg, ok := <-session.Messages:
					if !ok {
						t.Fatal("session ended before active prompt")
					}
					if msg.Type == MessageThinking && msg.Content == "active" {
						break waitPrompt
					}
				case <-timer.C:
					t.Fatal("session did not start")
				}
			}
			if status == "aborted" {
				cancel()
			}
			result, _ := collectAntigravityACP(t, session)
			if result.Status != status || result.SessionID != "session-1" || result.ResumeRejected {
				t.Fatalf("result = %+v, want %s", result, status)
			}
			if _, err := os.Stat(filepath.Join(cwd, "cancel-received")); err != nil {
				t.Fatalf("fake did not receive a session/cancel notification: %v", err)
			}
		})
	}
}

func TestAntigravityACPFakeProcess(t *testing.T) {
	mode := os.Getenv("MULTICA_FAKE_ANTIGRAVITY_ACP_MODE")
	if mode == "" {
		return
	}
	if !slices.Equal(os.Args[3:], []string{"acp", "--disable-telemetry"}) {
		t.Fatalf("fake argv = %v", os.Args)
	}
	send := func(value any) {
		t.Helper()
		if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	update := func(id string, kind string, fields map[string]any) {
		fields["sessionUpdate"] = kind
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": id, "update": fields}})
	}
	chunk := func(id, kind, text string) {
		update(id, kind, map[string]any{"content": map[string]string{"type": "text", "text": text}})
	}
	const text = "agent_message_chunk"
	const thought = "agent_thought_chunk"
	requests := newAgentStreamScanner(os.Stdin)
	var promptID json.RawMessage
	initialized, attached, modelSelected := false, false, false
	for requests.Scan() {
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
			Params struct {
				Cwd       string              `json:"cwd"`
				SessionID string              `json:"sessionId"`
				ConfigID  string              `json:"configId"`
				Value     string              `json:"value"`
				Prompt    []map[string]string `json:"prompt"`
			} `json:"params"`
			Result struct {
				Outcome struct {
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			} `json:"result"`
		}
		if err := json.Unmarshal(requests.Bytes(), &req); err != nil {
			t.Fatal(err)
		}
		reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) }
		rpcError := func(code int, message string) {
			send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": message}})
		}
		switch req.Method {
		case "initialize":
			initialized = true
			version := 1
			if mode == "bad-protocol" {
				version = 2
			}
			if mode == "setup-timeout" {
				continue
			}
			capabilities := map[string]any{"loadSession": true, "sessionCapabilities": map[string]any{"resume": map[string]any{}}}
			if mode == "no-resume" || mode == "load" {
				capabilities = map[string]any{"loadSession": mode == "load"}
			}
			reply(map[string]any{"protocolVersion": version, "agentCapabilities": capabilities, "agentInfo": map[string]string{"name": "Antigravity ACP Server", "version": "1.5.15"}})
		case "session/new", "session/resume", "session/load":
			actual, err := os.Stat(".")
			requested, reqErr := os.Stat(req.Params.Cwd)
			if err != nil || reqErr != nil || !initialized || !filepath.IsAbs(req.Params.Cwd) || !os.SameFile(actual, requested) {
				t.Fatalf("invalid session setup: cwd=%q initialized=%v, %v, %v", req.Params.Cwd, initialized, err, reqErr)
			}
			if req.Method != "session/new" && req.Params.SessionID != "session-1" {
				t.Fatalf("wrong resume ID: %s", req.Params.SessionID)
			}
			switch mode {
			case "auth":
				rpcError(-32000, "Authentication required")
				continue
			case "rejected":
				rpcError(-32603, "session not found")
				continue
			case "missing-trajectory":
				rpcError(-32603, "trajectory not found")
				continue
			case "changed":
				reply(map[string]string{"sessionId": "fresh-id"})
				continue
			case "disconnect":
				os.Exit(9)
			case "load", "resume":
				if req.Method != "session/"+mode {
					t.Fatalf("expected session/%s, got %s", mode, req.Method)
				}
				if _, err := os.Stat("session-1.saved"); err != nil {
					t.Fatalf("resume used another workdir or skipped the prior turn: %v", err)
				}
			}
			attached = true
			chunk("session-1", text, "history")
			model := "gemini-test"
			if req.Method != "session/new" {
				model = "claude-sonnet-test"
			}
			result := map[string]any{"configOptions": []map[string]string{{"id": "model", "category": "model", "currentValue": model}}}
			if req.Method == "session/new" {
				result["sessionId"] = "session-1"
			}
			reply(result)
		case "session/set_config_option":
			if !attached || req.Params.SessionID != "session-1" || req.Params.ConfigID != "model" || req.Params.Value != "claude-sonnet-test" {
				t.Fatalf("invalid model config: %+v", req.Params)
			}
			if mode == "model-error" {
				rpcError(-32602, "model unavailable")
				continue
			}
			modelSelected = true
			reply(map[string]any{"configOptions": []map[string]string{{"id": "model", "currentValue": req.Params.Value}}})
		case "session/prompt":
			if !attached || req.Params.SessionID != "session-1" || len(req.Params.Prompt) != 1 ||
				req.Params.Prompt[0]["text"] != "task prompt" || (mode == "new" && !modelSelected) {
				t.Fatalf("invalid prompt: %+v", req.Params)
			}
			switch mode {
			case "cancel":
				promptID = req.ID
				chunk("session-1", thought, "active")
			case "queued-result":
				reply(map[string]string{"stopReason": "queued"})
			case "resume", "load":
				chunk("session-1", text, "Continued")
				reply(map[string]string{"stopReason": "end_turn"})
			case "new":
				chunk("another-session", text, "unrelated")
				chunk("session-1", thought, "Thinking\n")
				chunk("session-1", thought, "next\n\n")
				chunk("session-1", text, "Checking\n\n")
				for range 2 {
					update("session-1", "tool_call", map[string]any{"toolCallId": "call-1", "title": "Run run_command", "status": "in_progress", "rawInput": map[string]string{"CommandLine": "pwd"}})
				}
				promptID = req.ID
				send(map[string]any{
					"jsonrpc": "2.0", "id": "permission", "method": "session/request_permission",
					"params": map[string]any{
						"sessionId": "session-1", "toolCall": map[string]string{"toolCallId": "call-1"},
						"options": []map[string]string{{"optionId": "always", "kind": "allow_always"}, {"optionId": "once", "kind": "allow_once"}},
					},
				})
			default:
				t.Fatalf("unexpected prompt in mode %s", mode)
			}
		case "":
			if string(req.ID) != `"permission"` || req.Result.Outcome.OptionID != "once" {
				t.Fatalf("expected one-time permission approval, got %s", requests.Text())
			}
			for range 2 {
				update("session-1", "tool_call_update", map[string]any{"toolCallId": "call-1", "status": "completed", "rawOutput": "ok"})
			}
			chunk("session-1", text, "Final\n")
			chunk("session-1", text, "\nanswer")
			chunk("session-1", text, " answer\n\n")
			if err := os.WriteFile("session-1.saved", nil, 0o600); err != nil {
				t.Fatal(err)
			}
			send(map[string]any{"jsonrpc": "2.0", "id": promptID, "result": map[string]any{
				"stopReason": "end_turn", "usage": map[string]int{"inputTokens": 17, "outputTokens": 4},
			}})
			chunk("session-1", text, "detached follow-up")
		case "session/cancel":
			if req.ID != nil || req.Params.SessionID != "session-1" || promptID == nil {
				t.Fatalf("invalid cancellation: %+v", req)
			}
			if err := os.WriteFile("cancel-received", nil, 0o600); err != nil {
				t.Fatal(err)
			}
			send(map[string]any{"jsonrpc": "2.0", "id": promptID, "result": map[string]string{"stopReason": "cancelled"}})
		default:
			t.Fatalf("unexpected ACP method %s", req.Method)
		}
	}
	os.Exit(0)
}
