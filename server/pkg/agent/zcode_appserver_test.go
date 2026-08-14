package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── test helpers ──────────────────────────────────────────────────────────

// writeFakeZcodeAppServer writes an executable shell fixture that emulates the
// `zcode app-server` NDJSON protocol (POSIX-only; see codex_test.go for the
// same pattern). The body is a sequence of `read line` / `echo` statements.
func writeFakeZcodeAppServer(t *testing.T, body string) string {
	t.Helper()
	fakePath := filepath.Join(t.TempDir(), "zcode")
	script := "#!/bin/sh\n" + body
	writeTestExecutable(t, fakePath, []byte(script))
	return fakePath
}

// cleanupZcodeProc closes the registered app-server process for runtimeID and
// removes it from the package-level registry, so tests do not leak processes.
func cleanupZcodeProc(t *testing.T, runtimeID string) {
	t.Helper()
	t.Cleanup(func() {
		key := "runtime:" + runtimeID
		if v, ok := zcodeProcRegistry.Load(key); ok {
			c := v.(*zcodeClient)
			c.close()
			zcodeProcRegistry.Delete(key)
		}
	})
}

// zcodeFakeStdin adds the Close method the client's io.WriteCloser field needs;
// fakeStdin (shared with codex tests) only implements Write.
type zcodeFakeStdin struct {
	*fakeStdin
}

func (zcodeFakeStdin) Close() error { return nil }

func newTestZcodeClient(t *testing.T) (*zcodeClient, *fakeStdin) {
	t.Helper()
	fs := &fakeStdin{}
	return &zcodeClient{
		cfg:         Config{Logger: slog.Default()},
		stdin:       zcodeFakeStdin{fs},
		pending:     make(map[int]*zcodePendingRPC),
		processDone: make(chan struct{}),
		handlers:    make(map[string]*zcodeSessionHandler),
	}, fs
}

// executeFakeZcode runs one zcode turn against a fake app-server fixture.
func executeFakeZcode(t *testing.T, fakePath string, cfg Config, opts ExecOptions, budget time.Duration) (Result, []Message) {
	t.Helper()
	cfg.ExecutablePath = fakePath
	if cfg.RuntimeID == "" {
		cfg.RuntimeID = "rt-execute-test"
	}
	cleanupZcodeProc(t, cfg.RuntimeID)
	backend, err := New("zcode", cfg)
	if err != nil {
		t.Fatalf("new zcode backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var mu sync.Mutex
	var messages []Message
	go func() {
		for msg := range session.Messages {
			mu.Lock()
			messages = append(messages, msg)
			mu.Unlock()
		}
	}()
	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		mu.Lock()
		collected := append([]Message(nil), messages...)
		mu.Unlock()
		return result, collected
	case <-time.After(budget):
		t.Fatal("timeout waiting for result")
		return Result{}, nil
	}
}

func hasMessageType(msgs []Message, typ MessageType) bool {
	for _, m := range msgs {
		if m.Type == typ {
			return true
		}
	}
	return false
}

// ── client dispatch ───────────────────────────────────────────────────────

func TestZcodeHandleLineRoutesResponse(t *testing.T) {
	c, _ := newTestZcodeClient(t)
	pr := &zcodePendingRPC{ch: make(chan rpcResult, 1), method: "session/create"}
	c.mu.Lock()
	c.pending[1] = pr
	c.mu.Unlock()

	c.handleLine(`{"id":1,"result":{"session":{"sessionId":"sess_test"}}}`)

	select {
	case res := <-pr.ch:
		if res.err != nil {
			t.Fatalf("unexpected error: %v", res.err)
		}
		if got := zcodeResultSessionID(res.result); got != "sess_test" {
			t.Fatalf("session id = %q, want sess_test", got)
		}
	case <-time.After(time.Second):
		t.Fatal("pending RPC was not resolved")
	}
}

func TestZcodeHandleLineRoutesError(t *testing.T) {
	c, _ := newTestZcodeClient(t)
	pr := &zcodePendingRPC{ch: make(chan rpcResult, 1), method: "session/resume"}
	c.mu.Lock()
	c.pending[2] = pr
	c.mu.Unlock()

	c.handleLine(`{"id":2,"error":{"code":-32004,"message":"Session not found"}}`)

	select {
	case res := <-pr.ch:
		var rpcErr *zcodeRPCError
		if !errors.As(res.err, &rpcErr) {
			t.Fatalf("expected zcodeRPCError, got %v", res.err)
		}
		if rpcErr.Code != -32004 || !strings.Contains(rpcErr.Message, "Session not found") {
			t.Fatalf("unexpected rpc error: %+v", rpcErr)
		}
	case <-time.After(time.Second):
		t.Fatal("pending RPC was not resolved")
	}
}

func TestZcodeHandleServerRequestAutoResponds(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantSub string
	}{
		{
			name:    "runtime preferences",
			line:    `{"id":"server-1","method":"session/requestRuntimePreferences","params":{}}`,
			wantSub: `"modelContextBudgetStrategy":"preflight-v1"`,
		},
		{
			name:    "permission",
			line:    `{"id":"server-2","method":"interaction/requestPermission","params":{}}`,
			wantSub: `"decision":"allow"`,
		},
		{
			name:    "runtime headers",
			line:    `{"id":"server-3","method":"interaction/requestProviderRuntimeHeaders","params":{}}`,
			wantSub: `"headersApplied":true`,
		},
		{
			name:    "unknown",
			line:    `{"id":"server-4","method":"some/unknownMethod","params":{}}`,
			wantSub: `"error"`,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, fs := newTestZcodeClient(t)
			c.handleLine(tc.line)
			lines := fs.Lines()
			if len(lines) != 1 {
				t.Fatalf("expected one response line, got %d: %v", len(lines), lines)
			}
			if !strings.Contains(lines[0], tc.wantSub) {
				t.Fatalf("response %q does not contain %q", lines[0], tc.wantSub)
			}
			// The echoed id must be the string server id, not a number.
			var resp struct {
				ID json.RawMessage `json:"id"`
			}
			if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
				t.Fatalf("parse response: %v", err)
			}
			var id string
			if err := json.Unmarshal(resp.ID, &id); err != nil {
				t.Fatalf("server request id must round-trip as a string, got %s", resp.ID)
			}
		})
	}
}

func TestZcodeHandleNotificationRoutesToSession(t *testing.T) {
	c, _ := newTestZcodeClient(t)
	h := c.attachSession("sess_test")
	defer c.detachSession("sess_test")

	c.handleLine(`{"method":"session/event","params":{"type":"model.streaming","sessionId":"sess_test","payload":{"kind":"text_delta","delta":"hi"}}}`)

	select {
	case ev := <-h.ch:
		if ev.method != "session/event" || ev.session != "sess_test" {
			t.Fatalf("unexpected event %+v", ev)
		}
		etype, _ := ev.params["type"].(string)
		if etype != "model.streaming" {
			t.Fatalf("event type = %q", etype)
		}
	case <-time.After(time.Second):
		t.Fatal("notification not routed to handler")
	}
}

func TestZcodeNotificationForOtherSessionDropped(t *testing.T) {
	c, _ := newTestZcodeClient(t)
	h := c.attachSession("sess_test")
	defer c.detachSession("sess_test")

	// No handler for sess_other — must not block or route.
	c.handleLine(`{"method":"session/event","params":{"type":"model.streaming","sessionId":"sess_other","payload":{}}}`)

	select {
	case ev := <-h.ch:
		t.Fatalf("unexpected event routed: %+v", ev)
	default:
	}
}

// ── event conversion ──────────────────────────────────────────────────────

func TestZcodeProcessSessionEventTextAndTurnCompleted(t *testing.T) {
	b := &zcodeBackend{cfg: Config{Logger: slog.Default()}}
	state := &zcodeTurnState{usage: map[string]TokenUsage{}}
	var msgs []Message
	ch := make(chan Message, 16)
	msgClosed := make(chan struct{})
	go func() {
		for m := range ch {
			msgs = append(msgs, m)
		}
		close(msgClosed)
	}()

	if term, _, _, _ := b.processEvent(zcodeSessionEvent{method: "session/event", params: map[string]any{
		"type": "turn.started", "payload": map[string]any{},
	}}, ch, state); term {
		t.Fatal("turn.started must not be terminal")
	}
	if term, _, _, _ := b.processEvent(zcodeSessionEvent{method: "session/event", params: map[string]any{
		"type": "model.streaming", "payload": map[string]any{"kind": "reasoning_delta", "delta": "think"},
	}}, ch, state); term {
		t.Fatal("streaming must not be terminal")
	}
	if term, _, _, _ := b.processEvent(zcodeSessionEvent{method: "session/event", params: map[string]any{
		"type": "model.streaming", "payload": map[string]any{"kind": "text_delta", "delta": "hello"},
	}}, ch, state); term {
		t.Fatal("streaming must not be terminal")
	}
	term, status, out, _ := b.processEvent(zcodeSessionEvent{method: "session/event", params: map[string]any{
		"type": "turn.completed",
		"payload": map[string]any{
			"response": "hello world",
			"usage":    map[string]any{"inputTokens": float64(100), "outputTokens": float64(5)},
		},
	}}, ch, state)
	if !term || status != "completed" || out != "hello world" {
		t.Fatalf("turn.completed: done=%v status=%q out=%q", term, status, out)
	}
	close(ch)
	<-msgClosed

	if state.lastText != "hello" {
		t.Fatalf("lastText = %q, want hello", state.lastText)
	}
	u, ok := state.usage["zcode"]
	if !ok || u.InputTokens != 100 || u.OutputTokens != 5 {
		t.Fatalf("usage = %+v, want input=100 output=5", state.usage)
	}
	if !hasMessageType(msgs, MessageThinking) || !hasMessageType(msgs, MessageText) {
		t.Fatalf("expected thinking and text messages, got %+v", msgs)
	}
}

func TestZcodeProcessSessionEventTurnFailed(t *testing.T) {
	b := &zcodeBackend{cfg: Config{Logger: slog.Default()}}
	state := &zcodeTurnState{usage: map[string]TokenUsage{}}
	ch := make(chan Message, 4)
	done, status, _, errMsg := b.processEvent(zcodeSessionEvent{method: "session/event", params: map[string]any{
		"type": "turn.failed", "payload": map[string]any{"error": map[string]any{"message": "provider not configured"}},
	}}, ch, state)
	if !done || status != "failed" || !strings.Contains(errMsg, "provider not configured") {
		t.Fatalf("turn.failed: done=%v status=%q err=%q", done, status, errMsg)
	}
}

func TestZcodeProcessStateUpdatedPromptStarted(t *testing.T) {
	b := &zcodeBackend{cfg: Config{Logger: slog.Default()}}
	state := &zcodeTurnState{usage: map[string]TokenUsage{}}
	done, _, _, _ := b.processEvent(zcodeSessionEvent{method: "state.updated", params: map[string]any{
		"reason": "prompt_started",
	}}, make(chan Message, 4), state)
	if done || !state.turnStarted {
		t.Fatalf("prompt_started: done=%v turnStarted=%v", done, state.turnStarted)
	}
}

// ── full Execute flow against a fake app-server ───────────────────────────

func TestZcodeExecuteTurnCompleted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := writeFakeZcodeAppServer(t, ""+
		`read line`+"\n"+ // session/create
		`echo '{"id":1,"result":{"session":{"sessionId":"sess_test"}}}'`+"\n"+
		`read line`+"\n"+ // session/subscribe
		`echo '{"id":2,"result":{"eventSeq":0,"events":[]}}'`+"\n"+
		`read line`+"\n"+ // session/send
		`echo '{"id":3,"result":{"accepted":true,"sessionId":"sess_test"}}'`+"\n"+
		`echo '{"method":"session/event","params":{"type":"turn.started","sessionId":"sess_test","payload":{}}}'`+"\n"+
		`echo '{"method":"session/event","params":{"type":"model.streaming","sessionId":"sess_test","payload":{"kind":"reasoning_delta","delta":"think"}}}'`+"\n"+
		`echo '{"method":"session/event","params":{"type":"model.streaming","sessionId":"sess_test","payload":{"kind":"text_delta","delta":"hello"}}}'`+"\n"+
		`echo '{"method":"session/event","params":{"type":"turn.completed","sessionId":"sess_test","payload":{"response":"hello world","usage":{"inputTokens":100,"outputTokens":5}}}}'`+"\n"+
		`while read line; do :; done`+"\n")

	result, msgs := executeFakeZcode(t, fakePath, Config{Logger: slog.Default(), RuntimeID: "rt-exec-completed"},
		ExecOptions{Model: "bigmodel/GLM-5.2"}, 10*time.Second)

	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if result.Output != "hello world" {
		t.Fatalf("output = %q, want hello world", result.Output)
	}
	if result.SessionID != "sess_test" {
		t.Fatalf("session id = %q, want sess_test", result.SessionID)
	}
	if u, ok := result.Usage["zcode"]; !ok || u.InputTokens != 100 || u.OutputTokens != 5 {
		t.Fatalf("usage = %+v, want input=100 output=5", result.Usage)
	}
	if !hasMessageType(msgs, MessageThinking) || !hasMessageType(msgs, MessageText) {
		t.Fatalf("expected thinking/text messages, got %+v", msgs)
	}
}

func TestZcodeExecuteFreshSessionOnResumeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := writeFakeZcodeAppServer(t, ""+
		`read line`+"\n"+ // session/resume -> rejected
		`echo '{"id":1,"error":{"code":-32004,"message":"Session not found"}}'`+"\n"+
		`read line`+"\n"+ // session/create
		`echo '{"id":2,"result":{"session":{"sessionId":"sess_fresh"}}}'`+"\n"+
		`read line`+"\n"+ // session/subscribe
		`echo '{"id":3,"result":{"eventSeq":0,"events":[]}}'`+"\n"+
		`read line`+"\n"+ // session/send -> capture the request
		`echo "$line" > "$0.send"`+"\n"+
		`echo '{"id":4,"result":{"accepted":true,"sessionId":"sess_fresh"}}'`+"\n"+
		`echo '{"method":"session/event","params":{"type":"turn.started","sessionId":"sess_fresh","payload":{}}}'`+"\n"+
		`echo '{"method":"session/event","params":{"type":"turn.completed","sessionId":"sess_fresh","payload":{"response":"ok","usage":{}}}}'`+"\n"+
		`while read line; do :; done`+"\n")

	result, _ := executeFakeZcode(t, fakePath, Config{Logger: slog.Default(), RuntimeID: "rt-exec-fresh"},
		ExecOptions{
			Model:                  "bigmodel/GLM-5.2",
			ResumeSessionID:        "sess_gone",
			ResumeExpected:         true,
			ResumeContinuityNotice: "[prior context lost] ",
		}, 10*time.Second)

	if result.Status != "completed" || result.SessionID != "sess_fresh" {
		t.Fatalf("expected fresh completed session, got status=%q session=%q error=%q", result.Status, result.SessionID, result.Error)
	}

	// The send request must have carried the continuity notice + prompt.
	data, err := os.ReadFile(fakePath + ".send")
	if err != nil {
		t.Fatalf("read captured send line: %v", err)
	}
	var req struct {
		Params struct {
			Content string `json:"content"`
		} `json:"params"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("parse send line %q: %v", data, err)
	}
	if !strings.HasPrefix(req.Params.Content, "[prior context lost] ") {
		t.Fatalf("send content %q missing continuity notice", req.Params.Content)
	}
}

func TestZcodeExecuteTurnFailed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := writeFakeZcodeAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"id":1,"result":{"session":{"sessionId":"sess_test"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"id":2,"result":{"eventSeq":0,"events":[]}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"id":3,"result":{"accepted":true,"sessionId":"sess_test"}}'`+"\n"+
		`echo '{"method":"session/event","params":{"type":"turn.started","sessionId":"sess_test","payload":{}}}'`+"\n"+
		`echo '{"method":"session/event","params":{"type":"turn.failed","sessionId":"sess_test","payload":{"error":{"message":"provider not configured"}}}}'`+"\n"+
		`while read line; do :; done`+"\n")

	result, _ := executeFakeZcode(t, fakePath, Config{Logger: slog.Default(), RuntimeID: "rt-exec-failed"},
		ExecOptions{Model: "bigmodel/GLM-5.2"}, 10*time.Second)

	if result.Status != "failed" || !strings.Contains(result.Error, "provider not configured") {
		t.Fatalf("expected failed with provider error, got status=%q error=%q", result.Status, result.Error)
	}
}

// TestZcodeExecuteRecoversCompletedFromPoll exercises the reliability fallback:
// notifications are dropped but the turn completed internally; the session/read
// poll recovers the terminal state and the response text.
// TestZcodeExecuteFallsBackToFreshOnModelUnavailable covers ZCode's -32031
// restore-warning rejection: a resumed session whose stored model is no longer
// in the runtime's model catalog fails at session/send. The backend must
// abandon that session, create a fresh one, and retry the turn once.
func TestZcodeExecuteFallsBackToFreshOnModelUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := writeFakeZcodeAppServer(t, ""+
		`read line`+"\n"+ // session/resume -> sess_bad
		`echo '{"id":1,"result":{"session":{"sessionId":"sess_bad"}}}'`+"\n"+
		`read line`+"\n"+ // session/subscribe (sess_bad)
		`echo '{"id":2,"result":{"eventSeq":0,"events":[]}}'`+"\n"+
		`read line`+"\n"+ // session/send (sess_bad) -> -32031
		`echo '{"id":3,"error":{"code":-32031,"message":"历史任务使用的模型已不可用"}}'`+"\n"+
		`read line`+"\n"+ // session/close (sess_bad)
		`echo '{"id":4,"result":{"closed":true}}'`+"\n"+
		`read line`+"\n"+ // session/create -> sess_fresh
		`echo '{"id":5,"result":{"session":{"sessionId":"sess_fresh"}}}'`+"\n"+
		`read line`+"\n"+ // session/subscribe (sess_fresh)
		`echo '{"id":6,"result":{"eventSeq":0,"events":[]}}'`+"\n"+
		`read line`+"\n"+ // session/send (sess_fresh) -> capture
		`echo "$line" > "$0.send"`+"\n"+
		`echo '{"id":7,"result":{"accepted":true,"sessionId":"sess_fresh"}}'`+"\n"+
		`echo '{"method":"session/event","params":{"type":"turn.started","sessionId":"sess_fresh","payload":{}}}'`+"\n"+
		`echo '{"method":"session/event","params":{"type":"turn.completed","sessionId":"sess_fresh","payload":{"response":"ok","usage":{}}}}'`+"\n"+
		`while read line; do :; done`+"\n")

	result, _ := executeFakeZcode(t, fakePath, Config{Logger: slog.Default(), RuntimeID: "rt-exec-32031"},
		ExecOptions{
			Model:                  "bigmodel/GLM-5.2",
			ResumeSessionID:        "sess_bad",
			ResumeExpected:         true,
			ResumeContinuityNotice: "[continuity lost] ",
		}, 10*time.Second)

	if result.Status != "completed" {
		t.Fatalf("expected completed via fresh fallback, got status=%q error=%q", result.Status, result.Error)
	}
	if result.SessionID != "sess_fresh" {
		t.Fatalf("expected fresh session id sess_fresh, got %q", result.SessionID)
	}

	// The fresh turn must carry the continuity notice since the resume was
	// rejected by -32031.
	data, err := os.ReadFile(fakePath + ".send")
	if err != nil {
		t.Fatalf("read captured send line: %v", err)
	}
	var req struct {
		Params struct {
			SessionID string `json:"sessionId"`
			Content   string `json:"content"`
		} `json:"params"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("parse send line %q: %v", data, err)
	}
	if req.Params.SessionID != "sess_fresh" {
		t.Fatalf("fresh send targeted %q, want sess_fresh", req.Params.SessionID)
	}
	if !strings.HasPrefix(req.Params.Content, "[continuity lost] ") {
		t.Fatalf("fresh send content %q missing continuity notice", req.Params.Content)
	}
}

func TestZcodeExecuteRecoversCompletedFromPoll(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	zcodeTurnPollIntervalNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { zcodeTurnPollIntervalNanos.Store(0) })

	fakePath := writeFakeZcodeAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"id":1,"result":{"session":{"sessionId":"sess_poll"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"id":2,"result":{"eventSeq":0,"events":[]}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"id":3,"result":{"accepted":true,"sessionId":"sess_poll"}}'`+"\n"+
		// Only turn.started is notified; turn.completed is silently dropped.
		`echo '{"method":"session/event","params":{"type":"turn.started","sessionId":"sess_poll","payload":{}}}'`+"\n"+
		`read line`+"\n"+ // poll session/read
		`echo '{"id":4,"result":{"projection":{"status":"idle"},"messages":[{"info":{"role":"assistant"},"parts":[{"type":"text","text":"poll result"}]}]}}'`+"\n"+
		`read line`+"\n"+ // readSessionResponse session/read
		`echo '{"id":5,"result":{"projection":{"status":"idle"},"messages":[{"info":{"role":"assistant"},"parts":[{"type":"text","text":"poll result"}]}]}}'`+"\n"+
		`while read line; do :; done`+"\n")

	result, _ := executeFakeZcode(t, fakePath, Config{Logger: slog.Default(), RuntimeID: "rt-exec-poll"},
		ExecOptions{Model: "bigmodel/GLM-5.2"}, 10*time.Second)

	if result.Status != "completed" {
		t.Fatalf("expected completed via poll, got status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "poll result" {
		t.Fatalf("output = %q, want poll result", result.Output)
	}
}

// ── registry / model resolution ───────────────────────────────────────────

func TestZcodeProcRegistryReusesLiveProcess(t *testing.T) {
	fakePath := writeFakeZcodeAppServer(t, `while read line; do :; done`+"\n")
	cfg := Config{ExecutablePath: fakePath, RuntimeID: "rt-reuse", Logger: slog.Default()}
	cleanupZcodeProc(t, "rt-reuse")

	c1, err := getZcodeClient(cfg, fakePath, t.TempDir())
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	c2, err := getZcodeClient(cfg, fakePath, t.TempDir())
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if c1 != c2 {
		t.Fatal("expected the same live client to be reused")
	}
}

func TestZcodeResolveModelUsesOptsFirst(t *testing.T) {
	pid, mid, err := resolveZcodeModel(context.Background(), ExecOptions{Model: "bigmodel/GLM-5.2"}, "zcode", slog.Default())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pid != "bigmodel" || mid != "GLM-5.2" {
		t.Fatalf("resolved %q/%q, want bigmodel/GLM-5.2", pid, mid)
	}
}

func TestZcodeResultHelpers(t *testing.T) {
	if got := zcodeResultSessionID(json.RawMessage(`{"session":{"sessionId":"sess_x"}}`)); got != "sess_x" {
		t.Fatalf("session id = %q", got)
	}
	if got := zcodeProjectionStatus(json.RawMessage(`{"projection":{"status":"running"}}`)); got != "running" {
		t.Fatalf("projection status = %q", got)
	}
	read := json.RawMessage(`{"messages":[
		{"info":{"role":"user"},"parts":[{"type":"text","text":"q"}]},
		{"info":{"role":"assistant"},"parts":[{"type":"reasoning","text":"r"},{"type":"text","text":"final answer"}]}
	]}`)
	if got := zcodeFinalResponse(read); got != "final answer" {
		t.Fatalf("final response = %q", got)
	}
}
