package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testAntigravityHookCommand = []string{"/opt/multica tools/multica", AntigravityHookHelperArg}

func startTestAntigravitySupplements(t *testing.T, ctx context.Context) (*antigravitySupplementSession, string) {
	t.Helper()
	cwd := t.TempDir()
	s, err := startAntigravitySupplementSession(ctx, cwd, testAntigravityHookCommand, slog.Default())
	if err != nil {
		t.Fatalf("startAntigravitySupplementSession: %v", err)
	}
	t.Cleanup(s.close)
	return s, cwd
}

func (s *antigravitySupplementSession) testEnv(name string) string {
	for _, kv := range s.env() {
		if key, value, ok := strings.Cut(kv, "="); ok && key == name {
			return value
		}
	}
	return ""
}

func preInvocation(conversationID string) map[string]any {
	return map[string]any{
		"conversationId":  conversationID,
		"transcriptPath":  "/tmp/transcript.jsonl",
		"invocationNum":   2,
		"initialNumSteps": 1,
		"lastUserInput":   strings.Repeat("x", 1<<20),
	}
}

func relayAntigravityHook(t *testing.T, getenv func(string) string, payload map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	RunAntigravityHookRelay(strings.NewReader(string(data)), &out, getenv)
	var reply map[string]any
	if err := json.Unmarshal([]byte(out.String()), &reply); err != nil {
		t.Fatalf("relay printed %q, want one JSON object: %v", out.String(), err)
	}
	return reply
}

func injectedUserMessages(t *testing.T, reply map[string]any) []string {
	t.Helper()
	steps, _ := reply["injectSteps"].([]any)
	var messages []string
	for _, raw := range steps {
		step, _ := raw.(map[string]any)
		if len(step) != 1 {
			t.Fatalf("inject step %#v must carry exactly one field", raw)
		}
		message, ok := step["userMessage"].(string)
		if !ok {
			t.Fatalf("inject step %#v is not a userMessage", raw)
		}
		messages = append(messages, message)
	}
	return messages
}

// antigravitySupplementAsync starts Supplement and waits until the input is
// queued, so a following hook observes it deterministically.
func antigravitySupplementAsync(t *testing.T, s *antigravitySupplementSession, ctx context.Context, text string) <-chan error {
	t.Helper()
	before := func() int {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.pending)
	}()
	done := make(chan error, 1)
	go func() { done <- s.supplement(ctx, text) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		queued := len(s.pending)
		s.mu.Unlock()
		if queued > before {
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

func assertAntigravitySupplementWaiting(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("supplement settled (%v) before agy reported the user_input step", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAntigravitySupplementDeliversThroughHookRelay(t *testing.T) {
	s, cwd := startTestAntigravitySupplements(t, t.Context())

	data, err := os.ReadFile(filepath.Join(cwd, ".agents", "hooks.json"))
	if err != nil {
		t.Fatalf("hooks.json not written: %v", err)
	}
	var hooks map[string]antigravityHookConfig
	if err := json.Unmarshal(data, &hooks); err != nil {
		t.Fatal(err)
	}
	wantCommand := `'/opt/multica tools/multica' '` + AntigravityHookHelperArg + `'`
	if got := hooks[antigravityHookName].PreInvocation; len(got) != 1 || got[0].Command != wantCommand || got[0].Type != "command" {
		t.Fatalf("PreInvocation hooks = %#v, want command %q", got, wantCommand)
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

	done := antigravitySupplementAsync(t, s, t.Context(), "Also update the changelog.")
	reply := relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))
	if got := injectedUserMessages(t, reply); len(got) != 1 || got[0] != "Also update the changelog." {
		t.Fatalf("injected = %q", got)
	}
	assertAntigravitySupplementWaiting(t, done)
	if reply := relayAntigravityHook(t, s.testEnv, preInvocation("conv-main")); len(reply) != 0 {
		t.Fatalf("second hook replayed claimed guidance: %#v", reply)
	}

	if !s.receipt() {
		t.Fatal("receipt found no injected guidance")
	}
	if err := <-done; err != nil {
		t.Fatalf("supplement = %v, want delivered", err)
	}
	if s.receipt() {
		t.Fatal("a second user_input step settled guidance that was never injected")
	}
}

func TestAntigravitySupplementIgnoresOtherConversations(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	done := antigravitySupplementAsync(t, s, t.Context(), "Use the smaller fix.")
	for _, conversationID := range []string{"", "conv-subagent"} {
		if reply := relayAntigravityHook(t, s.testEnv, preInvocation(conversationID)); len(reply) != 0 {
			t.Fatalf("conversation %q consumed guidance: %#v", conversationID, reply)
		}
	}
	if got := injectedUserMessages(t, relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))); len(got) != 1 {
		t.Fatalf("main conversation injected %q", got)
	}
	s.receipt()
	if err := <-done; err != nil {
		t.Fatalf("supplement = %v", err)
	}
}

func TestAntigravitySupplementSettlesInInjectionOrder(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	first := antigravitySupplementAsync(t, s, t.Context(), "first")
	second := antigravitySupplementAsync(t, s, t.Context(), "second")
	if got := injectedUserMessages(t, relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))); strings.Join(got, ",") != "first,second" {
		t.Fatalf("injected = %q, want comment order", got)
	}
	s.receipt()
	if err := <-first; err != nil {
		t.Fatalf("first = %v", err)
	}
	assertAntigravitySupplementWaiting(t, second)
	s.receipt()
	if err := <-second; err != nil {
		t.Fatalf("second = %v", err)
	}
}

func TestAntigravitySupplementWithoutReceiptFailsAtTurnEnd(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := antigravitySupplementAsync(t, s, ctx, "Run the tests again.")
	relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))

	// The caller giving up does not withdraw guidance a hook already carried.
	cancel()
	assertAntigravitySupplementWaiting(t, done)

	s.end()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("supplement = %v, want canceled at turn end", err)
	}
	if s.ready() {
		t.Fatal("ready after the turn ended")
	}
	if err := s.supplement(t.Context(), "late"); !errors.Is(err, context.Canceled) {
		t.Fatalf("late supplement = %v, want canceled", err)
	}
}

func TestAntigravitySupplementCancelWithdrawsUnclaimedGuidance(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	ctx, cancel := context.WithCancel(t.Context())
	done := antigravitySupplementAsync(t, s, ctx, "Never mind.")
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("supplement = %v, want canceled", err)
	}
	if reply := relayAntigravityHook(t, s.testEnv, preInvocation("conv-main")); len(reply) != 0 {
		t.Fatalf("withdrawn guidance was injected: %#v", reply)
	}
}

func TestAntigravitySupplementUnsentReplyReturnsGuidanceToQueue(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	first := antigravitySupplementAsync(t, s, t.Context(), "first")
	claimed, _ := s.prepareHook(antigravityHookRequest{ConversationID: "conv-main"})
	second := antigravitySupplementAsync(t, s, t.Context(), "second")
	s.unclaim(claimed)

	if got := injectedUserMessages(t, relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))); strings.Join(got, ",") != "first,second" {
		t.Fatalf("injected = %q, want the unsent guidance first", got)
	}
	s.receipt()
	s.receipt()
	for _, done := range []<-chan error{first, second} {
		if err := <-done; err != nil {
			t.Fatalf("supplement = %v", err)
		}
	}
}

func TestAntigravitySupplementBatchesWithinLimit(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	big := strings.Repeat("a", antigravityHookGuidanceLimit-10)
	first := antigravitySupplementAsync(t, s, t.Context(), big)
	second := antigravitySupplementAsync(t, s, t.Context(), "second guidance")
	if got := injectedUserMessages(t, relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))); len(got) != 1 || got[0] != big {
		t.Fatalf("first hook injected %d messages, want only the one that fits", len(got))
	}
	if got := injectedUserMessages(t, relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))); len(got) != 1 || got[0] != "second guidance" {
		t.Fatalf("second hook injected %q", got)
	}
	s.receipt()
	s.receipt()
	for _, done := range []<-chan error{first, second} {
		if err := <-done; err != nil {
			t.Fatalf("supplement = %v", err)
		}
	}

	if err := s.supplement(t.Context(), strings.Repeat("b", antigravityHookGuidanceLimit+1)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized supplement = %v, want limit error", err)
	}
}

func TestAntigravitySupplementCloseCleansUpOnlyItsHooks(t *testing.T) {
	s, cwd := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	done := antigravitySupplementAsync(t, s, t.Context(), "pending")
	s.close()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("pending supplement = %v, want canceled", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".agents", "hooks.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hooks.json after close: %v, want removed", err)
	}

	edited, cwd2 := startTestAntigravitySupplements(t, t.Context())
	path := filepath.Join(cwd2, ".agents", "hooks.json")
	if err := os.WriteFile(path, []byte(`{"user":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	edited.close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("close removed a hooks.json it no longer owns: %v", err)
	}
}

func TestAntigravitySupplementKeepsUnmanagedHooksFile(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, ".agents", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const userHooks = `{"lint":{"PreInvocation":[{"command":"./lint.sh"}]}}`
	if err := os.WriteFile(path, []byte(userHooks), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AntigravityHooksInstallable(cwd); err == nil {
		t.Fatal("an unmanaged hooks.json was reported installable")
	}
	if _, err := startAntigravitySupplementSession(t.Context(), cwd, testAntigravityHookCommand, slog.Default()); err == nil {
		t.Fatal("started over an unmanaged hooks.json")
	}
	if data, _ := os.ReadFile(path); string(data) != userHooks {
		t.Fatalf("user hooks.json changed: %s", data)
	}

	stale, err := antigravityHooksFileData(hookShellCommand([]string{"/old/multica", AntigravityHookHelperArg}))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := startAntigravitySupplementSession(t.Context(), cwd, testAntigravityHookCommand, slog.Default())
	if err != nil {
		t.Fatalf("stale managed hooks.json blocked start: %v", err)
	}
	s.close()

	agentsFile := t.TempDir()
	if err := os.WriteFile(filepath.Join(agentsFile, ".agents"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AntigravityHooksInstallable(agentsFile); err == nil {
		t.Fatal(".agents as a file was reported installable")
	}
}

func TestAntigravityHookRelayFailsOpen(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	env := func(url, token string) func(string) string {
		return func(name string) string {
			switch name {
			case antigravityHookURLEnv:
				return url
			case antigravityHookTokenEnv:
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
		if reply := relayAntigravityHook(t, getenv, preInvocation("conv-main")); len(reply) != 0 {
			t.Fatalf("%s: relay replied %#v, want {}", name, reply)
		}
	}
	var out strings.Builder
	RunAntigravityHookRelay(strings.NewReader("not json"), &out, s.testEnv)
	if out.String() != "{}\n" {
		t.Fatalf("invalid payload: relay printed %q", out.String())
	}
}

func TestAntigravityTurnAcceptsOnlyInjectedUserInputs(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	done := antigravitySupplementAsync(t, s, t.Context(), "steer")
	relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))

	turn := antigravityTurn{steps: map[int]antigravityStepSummary{}}
	turn.observeUserInput(0, s)
	turn.observeUserInput(0, s) // a re-emitted snapshot of the opening step
	turn.observeUserInput(3, s) // the injected guidance
	turn.observeUserInput(3, s) // its re-emitted snapshot
	if turn.invalid || *turn.start != 0 || !turn.injected[3] {
		t.Fatalf("turn = %+v, want start 0 with step 3 injected", turn)
	}
	if err := <-done; err != nil {
		t.Fatalf("supplement = %v", err)
	}
	turn.observeUserInput(5, s)
	if !turn.invalid {
		t.Fatal("a user_input step nothing injected was accepted")
	}

	var unsteered antigravityTurn
	unsteered.observeUserInput(0, nil)
	unsteered.observeUserInput(1, nil)
	if !unsteered.invalid {
		t.Fatal("a second boundary without steering was accepted")
	}
}
