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
			result, messages := collectAntigravityACP(t, newSession)
			if result.Status != "completed" || result.SessionID != "session-1" || result.Output != "Final\n\nanswer answer answer" {
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
			if text.String() != "CheckingFinal\n\nanswer answer answer" || thoughts.String() != "Thinking\nnextReviewing" ||
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
		{"thinking-unconfirmed", "did not confirm thinking level", false, false},
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
			if tc.mode == "thinking-unconfirmed" {
				opts.ThinkingLevel = "max"
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

func waitForAntigravityACPMessage(t *testing.T, session *Session, match func(Message) bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case msg, ok := <-session.Messages:
			if !ok {
				t.Fatalf("ACP ended before expected message: %+v", <-session.Result)
			}
			if match(msg) {
				return
			}
		case <-timer.C:
			t.Fatal("ACP did not emit the expected message")
		}
	}
}

func TestAntigravityACPBackgroundWork(t *testing.T) {
	opts := ExecOptions{Cwd: t.TempDir(), Timeout: 12 * time.Second}
	session := startFakeAntigravityACP(t, t.Context(), "background", opts)
	waitForAntigravityACPMessage(t, session, func(msg Message) bool { return msg.Content == "background pending" })
	if session.TerminalObserved() {
		t.Fatal("first prompt ended while a background tool was running")
	}
	result, messages := collectAntigravityACP(t, session)
	if result.Status != "completed" || result.Output != "Background answer" {
		t.Fatalf("background result = %+v, stream = %+v", result, messages)
	}
	if !slices.ContainsFunc(messages, func(msg Message) bool {
		return msg.Type == MessageToolResult && msg.CallID == "background-1" && msg.Output == "work complete"
	}) {
		t.Fatalf("background tool result missing: %+v", messages)
	}
}

func TestAntigravityACPSteering(t *testing.T) {
	for _, mode := range []string{"steer", "steer-boundary", "steer-rejected"} {
		t.Run(mode, func(t *testing.T) {
			opts := ExecOptions{Cwd: t.TempDir(), Timeout: 12 * time.Second}
			session := startFakeAntigravityACP(t, t.Context(), mode, opts)
			waitForAntigravityACPMessage(t, session, func(msg Message) bool { return msg.Content == "ready for guidance" })
			for deadline := time.Now().Add(time.Second); !session.SupplementReady() && time.Now().Before(deadline); {
				time.Sleep(10 * time.Millisecond)
			}
			if session.TerminalObserved() || !session.SupplementReady() {
				t.Fatal("session not available for steering")
			}
			sendCtx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := session.Supplement(sendCtx, "additional guidance"); err != nil {
				t.Fatalf("transport receipt: %v", err)
			}
			// The fake withholds the latest response until it sees this file,
			// so returning from Supplement proves it only waited for the write.
			if err := os.WriteFile(filepath.Join(opts.Cwd, "finish-steering"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			result, messages := collectAntigravityACP(t, session)
			if mode == "steer-rejected" {
				if result.Status != "failed" || !strings.Contains(result.Error, "prompt rejected") || result.Output != "Original work completed" {
					t.Fatalf("rejected steer result = %+v", result)
				}
			} else {
				if result.Status != "completed" || result.SessionID != "session-1" || result.Output != "Answer using guidance" {
					t.Fatalf("steered result = %+v, stream = %+v", result, messages)
				}
				if got := result.Usage["gemini-test"]; got.InputTokens != 12 || got.OutputTokens != 5 {
					t.Fatalf("steered turn usage = %+v", result.Usage)
				}
			}
			if session.SupplementReady() || session.Supplement(t.Context(), "too late") == nil {
				t.Fatal("steering remained available after task completion")
			}
		})
	}
}

func TestAntigravityACPThinkingAndLiveUsage(t *testing.T) {
	snapshots := make(chan map[string]TokenUsage, 8)
	cwd := t.TempDir()
	opts := ExecOptions{
		Cwd: cwd, Timeout: 12 * time.Second, Model: "claude-sonnet-test", ThinkingLevel: "max",
		OnUsage: func(usage map[string]TokenUsage) {
			select {
			case snapshots <- usage:
			default:
			}
		},
	}
	session := startFakeAntigravityACP(t, t.Context(), "thinking-and-usage", opts)
	waitForAntigravityACPMessage(t, session, func(msg Message) bool { return msg.Content == "max applied" })
	select {
	case usage := <-snapshots:
		if got := usage[opts.Model]; got.InputTokens != 17 || got.OutputTokens != 4 {
			t.Fatalf("live usage = %+v", usage)
		}
	case <-time.After(time.Second):
		t.Fatal("live usage did not arrive before prompt settlement")
	}
	if err := os.WriteFile(filepath.Join(cwd, "finish-steering"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	result, _ := collectAntigravityACP(t, session)
	if got := result.Usage[opts.Model]; result.Status != "completed" || got.InputTokens != 17 || got.OutputTokens != 4 {
		t.Fatalf("terminal result = %+v", result)
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
	initialized, attached, modelSelected, thinkingSelected := false, false, false, false
	startTool := func() {
		update("session-1", "tool_call", map[string]any{"toolCallId": "background-1", "kind": "execute", "title": "Run command", "status": "in_progress"})
	}
	finishTool := func() {
		update("session-1", "tool_call_update", map[string]any{"toolCallId": "background-1", "status": "completed", "rawOutput": "work complete"})
	}
	promptEnd := func(id json.RawMessage, input, output int) {
		send(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
			"stopReason": "end_turn", "usage": map[string]int{"inputTokens": input, "outputTokens": output},
		}})
	}
	waitForTest := func() {
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat("finish-steering"); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("test did not release the prompt")
	}
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
			if !attached || req.Params.SessionID != "session-1" {
				t.Fatalf("invalid model config: %+v", req.Params)
			}
			if req.Params.ConfigID == "thought_level" {
				if !modelSelected || req.Params.Value != "max" {
					t.Fatalf("invalid thinking config: %+v", req.Params)
				}
				current := "max"
				if mode == "thinking-unconfirmed" {
					current = "low"
				}
				thinkingSelected = current == "max"
				reply(map[string]any{"configOptions": []map[string]string{{"id": "thought_level", "currentValue": current}}})
				continue
			}
			if req.Params.ConfigID != "model" || req.Params.Value != "claude-sonnet-test" {
				t.Fatalf("invalid model config: %+v", req.Params)
			}
			if mode == "model-error" {
				rpcError(-32602, "model unavailable")
				continue
			}
			modelSelected = true
			reply(map[string]any{"configOptions": []any{
				map[string]string{"id": "model", "currentValue": req.Params.Value},
				map[string]any{"id": "thought_level", "category": "thought_level", "currentValue": "low",
					"options": []map[string]string{{"value": "low", "name": "Low"}, {"value": "max", "name": "Max"}}},
			}})
		case "session/prompt":
			if !attached || req.Params.SessionID != "session-1" || len(req.Params.Prompt) != 1 {
				t.Fatalf("invalid prompt: %+v", req.Params)
			}
			prompt := req.Params.Prompt[0]["text"]
			if strings.HasPrefix(mode, "steer") && prompt == "additional guidance" && promptID != nil {
				if mode == "steer-rejected" {
					rpcError(-32602, "prompt rejected")
					waitForTest()
					finishTool()
					chunk("session-1", text, "Original work completed")
					promptEnd(promptID, 5, 2)
					continue
				}
				if mode == "steer" {
					promptEnd(promptID, 5, 2)
				}
				waitForTest()
				finishTool()
				time.Sleep(250 * time.Millisecond)
				chunk("session-1", text, "Answer using guidance")
				promptEnd(req.ID, 7, 3)
				continue
			}
			if prompt != "task prompt" || (mode == "new" && !modelSelected) {
				t.Fatalf("invalid prompt: %+v", req.Params)
			}
			switch mode {
			case "steer", "steer-boundary", "steer-rejected":
				if promptID != nil {
					t.Fatal("received another initial prompt")
				}
				promptID = req.ID
				startTool()
				chunk("session-1", text, "Original draft")
				if mode == "steer-boundary" {
					promptEnd(promptID, 5, 2)
				}
				chunk("session-1", thought, "ready for guidance")
			case "background":
				startTool()
				chunk("session-1", text, "Work continues in the background")
				promptEnd(req.ID, 0, 0)
				chunk("session-1", thought, "background pending")
				time.Sleep(4 * time.Second)
				finishTool()
				chunk("session-1", text, "Background ")
				time.Sleep(250 * time.Millisecond)
				chunk("session-1", text, "answer")
				update("session-1", "usage_update", map[string]any{"used": 10, "size": 100})
			case "thinking-and-usage":
				if !thinkingSelected {
					t.Fatal("thinking level not applied before prompt")
				}
				update("session-1", "usage_update", map[string]any{"usage": map[string]int{"inputTokens": 17, "outputTokens": 4}})
				chunk("session-1", thought, "max applied")
				waitForTest()
				chunk("session-1", text, "Done")
				promptEnd(req.ID, 17, 4)
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
			chunk("session-1", thought, "Reviewing\n")
			chunk("session-1", text, "\nanswer")
			chunk("session-1", text, " answer")
			chunk("session-1", text, " answer")
			chunk("session-1", text, "\n\n")
			if err := os.WriteFile("session-1.saved", nil, 0o600); err != nil {
				t.Fatal(err)
			}
			send(map[string]any{"jsonrpc": "2.0", "id": promptID, "result": map[string]any{
				"stopReason": "end_turn", "usage": map[string]int{"inputTokens": 17, "outputTokens": 4},
			}})
			chunk("another-session", text, "detached follow-up")
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
