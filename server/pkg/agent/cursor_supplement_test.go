package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testCursorHookCommand = []string{"/opt/multica tools/multica", CursorHookHelperArg}

func startTestCursorSupplements(t *testing.T, ctx context.Context) (*cursorSupplementSession, string) {
	t.Helper()
	cwd := t.TempDir()
	s, err := startCursorSupplementSession(ctx, cwd, testCursorHookCommand, slog.Default())
	if err != nil {
		t.Fatalf("startCursorSupplementSession: %v", err)
	}
	t.Cleanup(s.close)
	return s, cwd
}

func (s *cursorSupplementSession) testEnv(name string) string {
	for _, kv := range s.env() {
		if key, value, ok := strings.Cut(kv, "="); ok && key == name {
			return value
		}
	}
	return ""
}

func relayCursorHook(t *testing.T, getenv func(string) string, payload map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	RunCursorHookRelay(strings.NewReader(string(data)), &out, getenv)
	var reply map[string]any
	if err := json.Unmarshal([]byte(out.String()), &reply); err != nil {
		t.Fatalf("relay printed %q, want one JSON object: %v", out.String(), err)
	}
	return reply
}

func postToolUse(conversationID string) map[string]any {
	return map[string]any{
		"hook_event_name": "postToolUse",
		"conversation_id": conversationID,
		"tool_name":       "Shell",
		"tool_output":     strings.Repeat("x", 1<<20),
	}
}

// supplementAsync starts Supplement and waits until the input is queued, so a
// following hook observes it deterministically.
func supplementAsync(t *testing.T, s *cursorSupplementSession, ctx context.Context, text string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.supplement(ctx, text) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		queued := len(s.pending)
		s.mu.Unlock()
		if queued > 0 {
			return done
		}
		select {
		case err := <-done:
			t.Fatalf("supplement returned before a hook: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("supplement was never queued")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCursorSupplementDeliversThroughHookRelay(t *testing.T) {
	s, cwd := startTestCursorSupplements(t, t.Context())

	data, err := os.ReadFile(filepath.Join(cwd, ".cursor", "hooks.json"))
	if err != nil {
		t.Fatalf("hooks.json not written: %v", err)
	}
	var hooks cursorHooksFile
	if err := json.Unmarshal(data, &hooks); err != nil {
		t.Fatal(err)
	}
	wantCommand := `'/opt/multica tools/multica' '` + CursorHookHelperArg + `'`
	for _, event := range []string{"postToolUse", "postToolUseFailure"} {
		if got := hooks.Hooks[event]; len(got) != 1 || got[0].Command != wantCommand {
			t.Fatalf("%s hooks = %#v, want command %q", event, got, wantCommand)
		}
	}

	if s.ready() {
		t.Fatal("ready before the CLI reported its conversation")
	}
	if err := s.supplement(t.Context(), "early"); err == nil || !strings.Contains(err.Error(), "turn has not started") {
		t.Fatalf("supplement before start = %v, want turn-not-started", err)
	}
	s.activate("conv-main")
	if !s.ready() {
		t.Fatal("not ready after activation")
	}

	done := supplementAsync(t, s, t.Context(), "Also update the changelog.")
	reply := relayCursorHook(t, s.testEnv, postToolUse("conv-main"))
	guidance, _ := reply["additional_context"].(string)
	if !strings.HasPrefix(guidance, s.tag()+"\n") || !strings.HasSuffix(guidance, "Also update the changelog.") {
		t.Fatalf("additional_context = %q", guidance)
	}
	if !strings.Contains(s.promptNote(), s.tag()) {
		t.Fatal("prompt note does not declare the delivery tag")
	}
	assertSupplementWaiting(t, done)
	if reply := relayCursorHook(t, s.testEnv, postToolUse("conv-main")); len(reply) != 0 {
		t.Fatalf("second hook replayed claimed guidance: %#v", reply)
	}

	s.receipt("call-other", []string{"unrelated hook context"})
	assertSupplementWaiting(t, done)
	s.receipt("call-1", []string{guidance})
	if err := <-done; err != nil {
		t.Fatalf("supplement = %v, want delivered", err)
	}
}

func assertSupplementWaiting(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("supplement settled (%v) before Cursor echoed the guidance", err)
	case <-time.After(50 * time.Millisecond):
	}
}

type closedHookStdout struct{}

func (closedHookStdout) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCursorSupplementRelayFailureIsNotDelivery(t *testing.T) {
	s, _ := startTestCursorSupplements(t, t.Context())
	s.activate("conv-main")
	done := supplementAsync(t, s, t.Context(), "Stop before making the next change.")
	payload := postToolUse("conv-main")
	payload["tool_use_id"] = "call-1\nfc_1"
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	RunCursorHookRelay(strings.NewReader(string(data)), closedHookStdout{}, s.testEnv)
	assertSupplementWaiting(t, done)

	// Cursor reports the call without the guidance, so it never reached the
	// model and goes out again at the next tool boundary.
	s.receipt("call-1", nil)
	assertSupplementWaiting(t, done)
	reply := relayCursorHook(t, s.testEnv, postToolUse("conv-main"))
	guidance, _ := reply["additional_context"].(string)
	if !strings.HasSuffix(guidance, "Stop before making the next change.") {
		t.Fatalf("retried additional_context = %q", guidance)
	}
	s.receipt("call-2", []string{guidance})
	if err := <-done; err != nil {
		t.Fatalf("supplement = %v, want delivered on retry", err)
	}
}

func TestCursorSupplementRetriesDroppedGuidanceFirst(t *testing.T) {
	s, _ := startTestCursorSupplements(t, t.Context())
	s.activate("conv-main")
	input := func(text string) *cursorSupplementInput {
		return &cursorSupplementInput{ctx: t.Context(), text: text, done: make(chan error, 1)}
	}
	first, second := input("first"), input("second")
	s.pending = []*cursorSupplementInput{first}
	s.prepareHook(cursorHookRequest{Event: "postToolUseFailure", ConversationID: "conv-main", ToolUseID: "call-read"})
	s.pending = append(s.pending, second)

	s.receipt("call-read", []string{})
	output := s.prepareHook(cursorHookRequest{Event: "postToolUse", ConversationID: "conv-main", ToolUseID: "call-next"})
	if output["additional_context"] != "first\n\nsecond" {
		t.Fatalf("retry hook = %q, want dropped guidance ahead of newer guidance", output["additional_context"])
	}
	s.receipt("call-next", []string{"first\n\nsecond"})
	for _, in := range []*cursorSupplementInput{first, second} {
		if err := <-in.done; err != nil {
			t.Fatalf("%s = %v, want delivered", in.text, err)
		}
	}
}

func TestCursorSupplementWithoutReceiptFailsAtTurnEnd(t *testing.T) {
	s, _ := startTestCursorSupplements(t, t.Context())
	s.activate("conv-main")
	ctx, cancel := context.WithCancel(t.Context())
	done := supplementAsync(t, s, ctx, "no receipt")
	if reply := relayCursorHook(t, s.testEnv, postToolUse("conv-main")); len(reply) == 0 {
		t.Fatal("hook received no guidance")
	}
	// Cancelling cannot withdraw guidance Cursor may already hold.
	cancel()
	assertSupplementWaiting(t, done)
	s.end()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("unconfirmed supplement at turn end = %v, want context.Canceled", err)
	}
}

func TestCursorSupplementIgnoresHooksItDoesNotOwn(t *testing.T) {
	s, _ := startTestCursorSupplements(t, t.Context())
	s.activate("conv-main")
	ctx, cancel := context.WithCancel(t.Context())
	done := supplementAsync(t, s, ctx, "main loop only")

	subagent := postToolUse("conv-main")
	subagent["parent_tool_call_id"] = "toolu_task"
	preTool := postToolUse("conv-main")
	preTool["hook_event_name"] = "preToolUse"
	for name, payload := range map[string]map[string]any{
		"subagent":           subagent,
		"other conversation": postToolUse("conv-other"),
		"other event":        preTool,
	} {
		if reply := relayCursorHook(t, s.testEnv, payload); len(reply) != 0 {
			t.Fatalf("%s hook received guidance: %#v", name, reply)
		}
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled supplement = %v, want context.Canceled", err)
	}
	if reply := relayCursorHook(t, s.testEnv, postToolUse("conv-main")); len(reply) != 0 {
		t.Fatalf("withdrawn guidance was delivered later: %#v", reply)
	}
}

func TestCursorSupplementEndFailsPendingAndCleansUp(t *testing.T) {
	s, cwd := startTestCursorSupplements(t, t.Context())
	s.activate("conv-main")
	done := supplementAsync(t, s, t.Context(), "too late")

	s.end()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("pending supplement at turn end = %v, want context.Canceled", err)
	}
	if s.ready() {
		t.Fatal("ready after the turn ended")
	}
	if err := s.supplement(t.Context(), "after"); !errors.Is(err, context.Canceled) {
		t.Fatalf("supplement after end = %v, want context.Canceled", err)
	}

	getenv := s.testEnv
	s.close()
	if _, err := os.Stat(filepath.Join(cwd, ".cursor", "hooks.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hooks.json after close: %v, want removed", err)
	}
	if reply := relayCursorHook(t, getenv, postToolUse("conv-main")); len(reply) != 0 {
		t.Fatalf("closed endpoint replied %#v", reply)
	}
}

func TestCursorSupplementRejectsOversizedGuidance(t *testing.T) {
	s, _ := startTestCursorSupplements(t, t.Context())
	s.activate("conv-main")
	err := s.supplement(t.Context(), strings.Repeat("界", cursorHookContextLimit))
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized supplement = %v, want limit error", err)
	}
	if len(s.pending) != 0 {
		t.Fatal("oversized guidance was queued")
	}
}

func TestCursorSupplementBatchesInCommentOrder(t *testing.T) {
	s, _ := startTestCursorSupplements(t, t.Context())
	s.activate("conv-main")
	input := func(text string) *cursorSupplementInput {
		return &cursorSupplementInput{ctx: t.Context(), text: text, done: make(chan error, 1)}
	}
	first := input(strings.Repeat("a", 6000))
	second := input(strings.Repeat("b", 6000))
	third := input("c")
	s.pending = []*cursorSupplementInput{first, second, third}

	req := cursorHookRequest{Event: "postToolUse", ConversationID: "conv-main"}
	output := s.prepareHook(req)
	if len(s.inflight) != 1 || s.inflight[0] != first || output["additional_context"] != first.text {
		t.Fatalf("first hook claimed %d inputs, want only the first", len(s.inflight))
	}
	output = s.prepareHook(req)
	if len(s.inflight) != 3 || s.inflight[1] != second || s.inflight[2] != third ||
		output["additional_context"] != second.text+"\n\n"+third.text {
		t.Fatalf("second hook claimed %d inputs in total, want second then third", len(s.inflight))
	}
}

func TestCursorSupplementKeepsUnmanagedHooksFile(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	own := []byte(`{"version":1,"hooks":{"afterFileEdit":[{"command":"./format.sh"}]}}`)
	if err := os.WriteFile(path, own, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := startCursorSupplementSession(t.Context(), cwd, testCursorHookCommand, slog.Default()); err == nil {
		t.Fatal("started over an unmanaged hooks.json")
	}
	if got, _ := os.ReadFile(path); string(got) != string(own) {
		t.Fatalf("unmanaged hooks.json changed to %q", got)
	}

	stale := []byte(`{"hooks":{"postToolUse":[{"command":"'/old/multica' '` + CursorHookHelperArg + `'"}]}}`)
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := startCursorSupplementSession(t.Context(), cwd, testCursorHookCommand, slog.Default())
	if err != nil {
		t.Fatalf("stale managed hooks.json blocked start: %v", err)
	}
	s.close()
}

func TestCursorHookRelayFailsOpen(t *testing.T) {
	s, _ := startTestCursorSupplements(t, t.Context())
	s.activate("conv-main")
	env := func(url, token string) func(string) string {
		return func(name string) string {
			switch name {
			case cursorHookURLEnv:
				return url
			case cursorHookTokenEnv:
				return token
			}
			return ""
		}
	}
	for name, getenv := range map[string]func(string) string{
		"no env":       env("", ""),
		"wrong token":  env(s.url, "nope"),
		"non-loopback": env(strings.Replace(s.url, "127.0.0.1", "192.0.2.1", 1), s.token),
		"unreachable":  env("http://127.0.0.1:1/hook", s.token),
	} {
		if reply := relayCursorHook(t, getenv, postToolUse("conv-main")); len(reply) != 0 {
			t.Fatalf("%s: relay replied %#v, want {}", name, reply)
		}
	}
	var out strings.Builder
	RunCursorHookRelay(strings.NewReader("not json"), &out, s.testEnv)
	if out.String() != "{}\n" {
		t.Fatalf("invalid payload: relay printed %q", out.String())
	}
}
