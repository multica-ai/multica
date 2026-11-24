package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
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
		marker, body, ok := strings.Cut(message, "\n")
		if !ok || !strings.HasPrefix(marker, cursorGuidanceTagPrefix) {
			t.Fatalf("injected message %.80q does not open with a guidance marker", message)
		}
		messages = append(messages, body)
	}
	return messages
}

// onlyInflightMarker returns the marker of the single guidance a hook injected.
func onlyInflightMarker(t *testing.T, s *antigravitySupplementSession) string {
	t.Helper()
	markers := s.inflightMarkers()
	if len(markers) != 1 {
		t.Fatalf("in-flight markers = %q, want one", markers)
	}
	return markers[0]
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

// assertAntigravityDelivered expects a receipt the caller has just processed
// to have settled the supplement.
func assertAntigravityDelivered(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supplement = %v, want delivered", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("supplement was not delivered")
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

	s.receipt(onlyInflightMarker(t, s))
	if err := <-done; err != nil {
		t.Fatalf("supplement = %v, want delivered", err)
	}
	if s.awaitingReceipt() {
		t.Fatal("delivered guidance still awaits a receipt")
	}
}

// Global and plugin hooks can inject user input into the same conversation.
// Their user_input step must not count as Multica's delivery.
func TestAntigravitySupplementIgnoresForeignUserInput(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	done := antigravitySupplementAsync(t, s, t.Context(), "Prefer the smaller fix.")
	relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))

	s.receipt(cursorGuidanceTagPrefix + "0000000000000000]")
	assertAntigravitySupplementWaiting(t, done)
	s.end()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("supplement = %v, want canceled at turn end", err)
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
	s.receipt(onlyInflightMarker(t, s))
	if err := <-done; err != nil {
		t.Fatalf("supplement = %v", err)
	}
}

func TestAntigravitySupplementInjectsOneMessagePerHookInOrder(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	first := antigravitySupplementAsync(t, s, t.Context(), "first")
	second := antigravitySupplementAsync(t, s, t.Context(), "second")
	for _, want := range []string{"first", "second"} {
		if got := injectedUserMessages(t, relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))); len(got) != 1 || got[0] != want {
			t.Fatalf("injected = %q, want %q", got, want)
		}
	}
	markers := s.inflightMarkers()
	if len(markers) != 2 || markers[0] == markers[1] {
		t.Fatalf("in-flight markers = %q, want one per message", markers)
	}
	s.receipt(markers[1])
	if err := <-second; err != nil {
		t.Fatalf("second = %v", err)
	}
	assertAntigravitySupplementWaiting(t, first)
	s.receipt(markers[0])
	if err := <-first; err != nil {
		t.Fatalf("first = %v", err)
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

	for _, want := range []string{"first", "second"} {
		if got := injectedUserMessages(t, relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))); len(got) != 1 || got[0] != want {
			t.Fatalf("injected = %q, want the unsent guidance first", got)
		}
		s.receipt(onlyInflightMarker(t, s))
	}
	for _, done := range []<-chan error{first, second} {
		if err := <-done; err != nil {
			t.Fatalf("supplement = %v", err)
		}
	}
}

// The API accepts 64 KiB comments and the daemon frames them before delivery.
// JSON escapes '<' as six bytes, so the hook reply is several times larger than
// the message; it must still reach agy intact.
func TestAntigravitySupplementCarriesLargestCommentThroughRelay(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	big := strings.Repeat("x", 1024) + "\n\nHuman message:\n" + strings.Repeat("<", 64<<10)
	done := antigravitySupplementAsync(t, s, t.Context(), big)
	if got := injectedUserMessages(t, relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))); len(got) != 1 || got[0] != big {
		t.Fatalf("relay injected %d messages, want the full comment", len(got))
	}
	s.receipt(onlyInflightMarker(t, s))
	if err := <-done; err != nil {
		t.Fatalf("supplement = %v", err)
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

	// A hook the user added to the generated file makes the file theirs, even
	// though it still names the relay: a later run must not overwrite it.
	var extended map[string]any
	if err := json.Unmarshal(stale, &extended); err != nil {
		t.Fatal(err)
	}
	extended["lint"] = map[string]any{"PreInvocation": []any{map[string]any{"type": "command", "command": "./lint.sh"}}}
	userEdited, err := json.MarshalIndent(extended, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, userEdited, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AntigravityHooksInstallable(cwd); err == nil {
		t.Fatal("a managed hooks.json with a user hook was reported installable")
	}
	if _, err := startAntigravitySupplementSession(t.Context(), cwd, testAntigravityHookCommand, slog.Default()); err == nil {
		t.Fatal("started over a user-extended hooks.json")
	}
	if data, _ := os.ReadFile(path); string(data) != string(userEdited) {
		t.Fatalf("user-extended hooks.json changed: %s", data)
	}

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

	marker := onlyInflightMarker(t, s)
	transcript, appDataDir := newTestAntigravityTranscript(t)

	turn := antigravityTurn{steps: map[int]antigravityStepSummary{}}
	turn.observeUserInput(0, s)
	turn.observeUserInput(0, s) // a re-emitted snapshot of the opening step
	turn.observeUserInput(2, s) // another hook's input while guidance is in flight
	turn.observeUserInput(3, s) // the injected guidance
	turn.observeUserInput(3, s) // its re-emitted snapshot
	if turn.invalid || *turn.start != 0 || !turn.injected[2] || !turn.injected[3] {
		t.Fatalf("turn = %+v, want start 0 with steps 2 and 3 injected", turn)
	}

	// Until the transcript shows the marker, nothing is delivered; the foreign
	// input settles nothing.
	turn.confirmInjected(s, transcript, "conv-main")
	seedAntigravityTranscript(t, appDataDir, "conv-main", []string{
		`{"type":"USER_INPUT","step_index":0,"content":"goal"}`,
		`{"type":"USER_INPUT","step_index":2,"content":"lint: ok"}`,
	})
	turn.confirmInjected(s, transcript, "conv-main")
	assertAntigravitySupplementWaiting(t, done)
	if !turn.checkTranscript {
		t.Fatal("stopped checking the transcript with guidance still in flight")
	}
	appendAntigravityTranscript(t, appDataDir, "conv-main", antigravityUserInputRecord(t, 3, marker+"\nsteer"))
	turn.confirmInjected(s, transcript, "conv-main")
	if err := <-done; err != nil {
		t.Fatalf("supplement = %v", err)
	}
	if turn.checkTranscript {
		t.Fatal("still checking the transcript with nothing in flight")
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

func appendAntigravityTranscript(t *testing.T, appDataDir, conversationID, record string) {
	t.Helper()
	path := filepath.Join(appDataDir, "brain", conversationID, ".system_generated", "logs", "transcript.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(record + "\n"); err != nil {
		t.Fatal(err)
	}
}

func newTestAntigravityTranscript(t *testing.T) (*antigravityTranscript, string) {
	t.Helper()
	appDataDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "agy.log")
	if err := os.WriteFile(logPath, []byte("I0630 1 common.go:156] CLI app data directory: "+appDataDir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &antigravityTranscript{logPath: logPath}, appDataDir
}

func antigravityUserInputRecord(t *testing.T, step int, content string, extra ...string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"type": "USER_INPUT", "source": "USER_EXPLICIT", "step_index": step, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	if len(extra) > 0 {
		return strings.TrimSuffix(string(data), "}") + "," + strings.Join(extra, ",") + "}"
	}
	return string(data)
}

// agy rewrites the transcript when it compacts the conversation. A receipt
// written after that must still be found.
func TestAntigravityTranscriptFollowsCompactionRewrite(t *testing.T) {
	transcript, appDataDir := newTestAntigravityTranscript(t)
	const first, second = "[MULTICA-GUIDANCE nonce=1111111111111111]", "[MULTICA-GUIDANCE nonce=2222222222222222]"
	history := []string{antigravityUserInputRecord(t, 0, "goal")}
	for i := 1; i < 200; i++ {
		history = append(history, `{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":`+strconv.Itoa(i)+`,"content":"`+strings.Repeat("work ", 30)+`"}`)
	}
	history = append(history, antigravityUserInputRecord(t, 200, first+"\nfirst"))
	seedAntigravityTranscript(t, appDataDir, "conv-main", history)

	var found []string
	collect := func(marker string) { found = append(found, marker) }
	transcript.scan("conv-main", []string{first}, collect)
	if len(found) != 1 || found[0] != first {
		t.Fatalf("found = %q, want the first receipt", found)
	}

	seedAntigravityTranscript(t, appDataDir, "conv-main", []string{
		`{"type":"CONVERSATION_HISTORY","content":"summary"}`,
		antigravityUserInputRecord(t, 4, second+"\nsecond"),
	})
	found = nil
	transcript.scan("conv-main", []string{second}, collect)
	if len(found) != 1 || found[0] != second {
		t.Fatalf("found = %q, want the receipt in the rewritten transcript", found)
	}
}

// The compact transcript keeps only a prefix of long content. The marker
// leads the message, so the receipt survives the truncation.
func TestAntigravityTranscriptMatchesTruncatedContent(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	long := strings.Repeat("x", 64<<10)
	done := antigravitySupplementAsync(t, s, t.Context(), long)
	relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))
	marker := onlyInflightMarker(t, s)

	transcript, appDataDir := newTestAntigravityTranscript(t)
	seedAntigravityTranscript(t, appDataDir, "conv-main", []string{
		antigravityUserInputRecord(t, 0, "goal"),
		antigravityUserInputRecord(t, 3, (marker + "\n" + long)[:4096], `"truncated_fields":["content"]`),
	})
	turn := antigravityTurn{steps: map[int]antigravityStepSummary{}}
	turn.observeUserInput(0, s)
	turn.observeUserInput(3, s)
	turn.confirmInjected(s, transcript, "conv-main")
	assertAntigravityDelivered(t, done)
}

// A compaction can rewrite the transcript in place and grow it past the
// remembered offset before the next check, so the offset lands inside the
// receipt's line. A new injected step must read the file from the start.
func TestAntigravityTranscriptFindsReceiptAfterInPlaceRewrite(t *testing.T) {
	s, _ := startTestAntigravitySupplements(t, t.Context())
	s.activate("conv-main")
	transcript, appDataDir := newTestAntigravityTranscript(t)
	turn := antigravityTurn{steps: map[int]antigravityStepSummary{}}
	turn.observeUserInput(0, s)

	first := antigravitySupplementAsync(t, s, t.Context(), "first")
	relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))
	history := []string{antigravityUserInputRecord(t, 0, "goal")}
	for i := 1; i < 200; i++ {
		history = append(history, `{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":`+strconv.Itoa(i)+`,"content":"`+strings.Repeat("work ", 30)+`"}`)
	}
	history = append(history, antigravityUserInputRecord(t, 200, onlyInflightMarker(t, s)+"\nfirst"))
	seedAntigravityTranscript(t, appDataDir, "conv-main", history)
	turn.observeUserInput(200, s)
	turn.confirmInjected(s, transcript, "conv-main")
	if err := <-first; err != nil {
		t.Fatalf("first = %v", err)
	}
	oldOffset := int(transcript.offset)

	second := antigravitySupplementAsync(t, s, t.Context(), "second")
	relayAntigravityHook(t, s.testEnv, preInvocation("conv-main"))
	receipt := antigravityUserInputRecord(t, 4, onlyInflightMarker(t, s)+"\nsecond "+strings.Repeat("y", 2000))
	padding := `{"type":"CONVERSATION_HISTORY","content":"` + strings.Repeat("s", oldOffset-500) + `"}`
	if len(padding)+1 >= oldOffset || len(padding)+1+len(receipt)+1 <= oldOffset {
		t.Fatal("fixture: the receipt line must straddle the old offset")
	}
	path := filepath.Join(appDataDir, "brain", "conv-main", ".system_generated", "logs", "transcript.jsonl")
	if err := os.WriteFile(path, []byte(padding+"\n"+receipt+"\n"), 0o644); err != nil { // same inode
		t.Fatal(err)
	}
	turn.observeUserInput(4, s)
	turn.confirmInjected(s, transcript, "conv-main")
	assertAntigravityDelivered(t, second)
}
