package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Background-shell liveness regression for #7833 (PUCK-147).
//
// The shape being bounded is: Cursor launches a background shell, its top-level
// execution then stops making semantic progress, and its terminal `result` never
// arrives — so the task holds a runtime slot until the daemon-wide watchdog.
//
// A background shell is a lifecycle state that needs supervision, not a failure:
// these tests pin both halves of that distinction. The pathological run must end
// inside a bounded window, and a legitimate run that starts a server, keeps
// working, and reports a result must be left alone.
//
// These run on every platform, so the fake is the test binary re-executed as
// cursor-agent (dispatched from the package TestMain via CURSOR_FAKE_MODE)
// rather than the /bin/sh fixtures in cursor_execute_unix_test.go.

// cursorFakeModeEnv selects the fake cursor-agent behaviour when this test
// binary is re-executed as the child. Mirrors CLAUDE_FAKE_MODE.
const cursorFakeModeEnv = "CURSOR_FAKE_MODE"

const (
	cursorFakeStall              = "background_shell_stall"
	cursorFakeWorkThenResult     = "background_shell_work_then_result"
	cursorFakePulses             = "background_shell_progress_pulses"
	cursorFakeResultWins         = "background_shell_result_wins"
	cursorFakeMalformedStall     = "background_shell_malformed_stall"
	cursorFakeUnverifiedActivity = "background_shell_unverified_activity"
	cursorFakeNonShellStall      = "nonshell_background_flag_stall"
)

// cursorBGSessionID is emitted by every fake below; the guard must fail closed
// without losing it.
const cursorBGSessionID = "sess-cursor-bg"

const (
	cursorBGInit = `{"type":"system","subtype":"init","session_id":"` + cursorBGSessionID + `"}`

	cursorBGShellStarted = `{"type":"tool_call","subtype":"started","call_id":"call-bg",` +
		`"tool_call":{"shellToolCall":{"args":{"command":"python3 -m http.server 8000"},` +
		`"toolCallId":"call-bg"}}}`

	// cursorBGShellCompleted is the authoritative structural observation: the
	// `result` object's root carries `"isBackground": true`.
	cursorBGShellCompleted = `{"type":"tool_call","subtype":"completed","call_id":"call-bg",` +
		`"tool_call":{"shellToolCall":{"args":{"command":"python3 -m http.server 8000"},` +
		`"result":{"success":{"exitCode":0},"isBackground":true},"toolCallId":"call-bg"}}}`

	// cursorBGMalformedCompleted carries the same text where the result is a
	// JSON string rather than an object. It is not a lifecycle observation and
	// must not arm the guard.
	cursorBGMalformedCompleted = `{"type":"tool_call","subtype":"completed","call_id":"call-bg",` +
		`"tool_call":{"shellToolCall":{"args":{"command":"python3 -m http.server 8000"},` +
		`"result":"{\"isBackground\":true}","toolCallId":"call-bg"}}}`

	cursorBGThinking    = `{"type":"thinking","subtype":"delta","text":"the server is answering"}`
	cursorBGReadStarted = `{"type":"tool_call","subtype":"started","call_id":"call-read",` +
		`"tool_call":{"readToolCall":{"args":{"path":"server.log"},"toolCallId":"call-read"}}}`
	cursorBGReadCompleted = `{"type":"tool_call","subtype":"completed","call_id":"call-read",` +
		`"tool_call":{"readToolCall":{"args":{"path":"server.log"},` +
		`"result":{"content":"GET / 200"},"toolCallId":"call-read"}}}`
	cursorBGStepFinish = `{"type":"step_finish","model":"cursor",` +
		`"part":{"tokens":{"input":120,"output":8,"cache":{"read":0}}}}`

	cursorBGResult = `{"type":"result","subtype":"success","is_error":false,` +
		`"result":"dev server checked and stopped","session_id":"` + cursorBGSessionID + `"}`

	// cursorNonShellBGCompleted carries the same root flag on a *read* payload.
	// It is not a launched shell, so it must not put the run under the guard.
	cursorNonShellBGCompleted = `{"type":"tool_call","subtype":"completed","call_id":"call-read",` +
		`"tool_call":{"readToolCall":{"args":{"path":"server.log"},` +
		`"result":{"content":"listening on 8000","isBackground":true},"toolCallId":"call-read"}}}`
)

// cursorFakeStaysAlive reports the fakes that hang instead of exiting, so only
// the daemon can end the run. A fake that finished normally exits before the
// test framework can print PASS/ok into the same stdout.
func cursorFakeStaysAlive(mode string) bool {
	switch mode {
	case cursorFakeStall, cursorFakeMalformedStall,
		cursorFakeUnverifiedActivity, cursorFakeNonShellStall:
		return true
	default:
		return false
	}
}

// cursorFakeEvent is one stream-json line, written after the given delay.
type cursorFakeEvent struct {
	delay time.Duration
	line  string
}

// runFakeCursorStream is the fake cursor-agent, dispatched from the package
// TestMain before the testing package parses the CLI's argv.
//
// It honours the real CLI's stdin contract first (read to EOF, the prompt is
// delivered on stdin — see buildCursorArgs and the drainStdin note in
// cursor_execute_unix_test.go), because a fake that never reads it races EPIPE
// against the daemon's prompt write, and that write outranks most failures when
// the error is classified.
func runFakeCursorStream(mode string) {
	var events []cursorFakeEvent
	// emitUnverifiedActivity is the "loud but meaningless" stream: unknown event
	// types, malformed JSON and stderr noise, none of which is semantic progress.
	var unverified bool

	switch mode {
	case cursorFakeStall:
		// The #7833 shape: background shell confirmed, then silence forever.
		events = []cursorFakeEvent{
			{line: cursorBGInit},
			{line: cursorBGShellStarted},
			{line: cursorBGShellCompleted},
		}
	case cursorFakeWorkThenResult:
		// Legitimate workflow: start the server, inspect it, report normally.
		events = []cursorFakeEvent{
			{line: cursorBGInit},
			{line: cursorBGShellStarted},
			{line: cursorBGShellCompleted},
			{delay: 150 * time.Millisecond, line: cursorBGThinking},
			{delay: 150 * time.Millisecond, line: cursorBGReadStarted},
			{delay: 150 * time.Millisecond, line: cursorBGReadCompleted},
			{delay: 150 * time.Millisecond, line: cursorBGStepFinish},
			{delay: 150 * time.Millisecond, line: cursorBGResult},
		}
	case cursorFakePulses:
		// Progress keeps landing well inside each guard interval, so the total
		// run spans several intervals without ever going quiet long enough.
		events = []cursorFakeEvent{{line: cursorBGInit}, {line: cursorBGShellStarted}, {line: cursorBGShellCompleted}}
		for i := 0; i < 5; i++ {
			events = append(events,
				cursorFakeEvent{delay: 500 * time.Millisecond, line: cursorBGThinking},
				cursorFakeEvent{delay: 0, line: cursorBGStepFinish},
			)
		}
		events = append(events, cursorFakeEvent{delay: 200 * time.Millisecond, line: cursorBGResult})
	case cursorFakeResultWins:
		// Terminal result arrives right after the background observation.
		events = []cursorFakeEvent{
			{line: cursorBGInit},
			{line: cursorBGShellStarted},
			{line: cursorBGShellCompleted},
			{delay: 150 * time.Millisecond, line: cursorBGResult},
		}
	case cursorFakeMalformedStall:
		// Same stall, but the lifecycle flag only exists as text inside a
		// non-object result: there is nothing to supervise here.
		events = []cursorFakeEvent{
			{line: cursorBGInit},
			{line: cursorBGShellStarted},
			{line: cursorBGMalformedCompleted},
		}
	case cursorFakeUnverifiedActivity:
		unverified = true
		events = []cursorFakeEvent{
			{line: cursorBGInit},
			{line: cursorBGShellStarted},
			{line: cursorBGShellCompleted},
		}
	case cursorFakeNonShellStall:
		// The same stall, but the flag was reported by a read tool: there is no
		// launched shell to supervise, so the guard must never arm.
		events = []cursorFakeEvent{
			{line: cursorBGInit},
			{line: cursorNonShellBGCompleted},
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown CURSOR_FAKE_MODE: %q\n", mode)
		os.Exit(2)
	}

	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "fake cursor-agent: read prompt: %v\n", err)
		os.Exit(21)
	}

	if unverified {
		go func() {
			// Noise of every kind the guard is told not to believe: a background
			// server's own log lines on stderr, an unknown top-level event type,
			// malformed JSON, and the CLI echoing our prompt back.
			for i := 0; i < 200; i++ {
				fmt.Fprintf(os.Stderr, "GET / 200 OK - %d bytes\n", i)
				fmt.Printf("%s\n", `{"type":"background_log","line":"still serving"}`)
				fmt.Printf("%s\n", `{"type":"result","subtype":"success","result":"trunc`)
				fmt.Printf("%s\n", `{"type":"user","message":{"role":"user","content":"still serving"}}`)
				time.Sleep(50 * time.Millisecond)
			}
		}()
	}

	for _, evt := range events {
		if evt.delay > 0 {
			time.Sleep(evt.delay)
		}
		if _, err := fmt.Fprintln(os.Stdout, evt.line); err != nil {
			os.Exit(22)
		}
	}

	if !cursorFakeStaysAlive(mode) {
		// The fake completed normally: exit before the test framework can print
		// PASS/ok into the JSON stream.
		os.Exit(0)
	}

	// Stay alive with stdout open, so only the daemon can end this run. This is
	// the state that held a runtime slot in #7833. A sleep, not a bare
	// select{}: the runtime deadlock detector would crash the child instead.
	time.Sleep(10 * time.Minute)
}

// cursorFakeRun is the observable outcome of one fake cursor-agent run.
type cursorFakeRun struct {
	result   Result
	messages []Message
	elapsed  time.Duration
}

// runCursorFake executes the cursor backend against the re-executed fake.
//
// The guard duration is set on the backend instance rather than through a
// package-level knob: this package's cursor tests run in parallel under -race,
// and a shared variable would be read by every concurrent Cursor execution.
//
// Waiting for the message channel to close (not just for Result) matters: the
// reader closes it after it has disarmed the guard and waited for its watcher,
// so a guard goroutine that outlived the run would hang this instead of
// silently leaking.
func runCursorFake(t *testing.T, mode string, guard, execTimeout time.Duration) cursorFakeRun {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	backend, err := New("cursor", Config{
		ExecutablePath: self,
		Env:            map[string]string{cursorFakeModeEnv: mode},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	cb, ok := backend.(*cursorBackend)
	if !ok {
		t.Fatalf("cursor backend is %T", backend)
	}
	cb.backgroundGuardTimeout = guard

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout+10*time.Second)
	defer cancel()

	start := time.Now()
	session, err := cb.Execute(ctx, "start a dev server and check it", ExecOptions{Timeout: execTimeout})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var messages []Message
	msgsDone := make(chan struct{})
	go func() {
		defer close(msgsDone)
		for msg := range session.Messages {
			messages = append(messages, msg)
		}
	}()

	var result Result
	select {
	case res, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		result = res
	case <-time.After(execTimeout + 5*time.Second):
		t.Fatal("cursor backend never reported a result")
	}
	elapsed := time.Since(start)

	select {
	case <-msgsDone:
	case <-time.After(10 * time.Second):
		t.Fatal("message channel never closed: the run's reader or its liveness watcher outlived the run")
	}

	return cursorFakeRun{result: result, messages: messages, elapsed: elapsed}
}

// assertGuardFailure pins the fail-closed outcome: failed, for the dedicated
// background-liveness reason, with no partial transcript promoted to output.
func (r cursorFakeRun) assertGuardFailure(t *testing.T) {
	t.Helper()

	if r.result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", r.result.Status, r.result.Error)
	}
	if r.result.Error != cursorBackgroundLivenessGuardError {
		t.Errorf("error = %q, want only the content-free background-liveness reason", r.result.Error)
	}
	// The guard's own cancellation must not be rewritten into the generic one.
	if strings.Contains(r.result.Error, "execution cancelled") {
		t.Errorf("error = %q, must not be reported as generic cancellation", r.result.Error)
	}
	if r.result.Status == "timeout" || r.result.Status == "aborted" {
		t.Errorf("status = %q, want the guard's own classification", r.result.Status)
	}
	if r.result.Output != "" {
		t.Errorf("output = %q, want the partial transcript withheld", r.result.Output)
	}
}

// TestCursorBackgroundShellStallFailsWithinGuardWindow is Test A: the #7833
// pathological run. It must be ended by the guard, long before the execution
// deadline, and must fail closed.
//
// RED on unpatched main: main ignores `isBackground` entirely, so the same fake
// stream holds the slot until ExecOptions.Timeout and reports
// status="timeout" — see the RED probe evidence in the PUCK-147 report.
func TestCursorBackgroundShellStallFailsWithinGuardWindow(t *testing.T) {
	t.Parallel()

	const (
		guard       = 500 * time.Millisecond
		execTimeout = 12 * time.Second
	)

	run := runCursorFake(t, cursorFakeStall, guard, execTimeout)

	// Well inside the 12s execution deadline: the guard, not the timeout, ended
	// the run.
	if run.elapsed > 6*time.Second {
		t.Errorf("elapsed = %s, want the guard to bound the run well before the 12s deadline", run.elapsed)
	}
	run.assertGuardFailure(t)

	if run.result.SessionID != cursorBGSessionID {
		t.Errorf("session id = %q, want %q", run.result.SessionID, cursorBGSessionID)
	}
	// Detection must not cost the transcript anything: the raw tool result is
	// still forwarded verbatim.
	var sawRawResult bool
	for _, msg := range run.messages {
		if msg.Type == MessageToolResult && strings.Contains(msg.Output, `"isBackground":true`) {
			sawRawResult = true
		}
	}
	if !sawRawResult {
		t.Errorf("background shell tool result was not forwarded unchanged: %+v", run.messages)
	}
}

// TestCursorBackgroundShellAllowsLegitimateFollowOnWork is Test B: a background
// shell followed by real work and a terminal result must complete normally.
// #8042's immediate cancel() fails this — the run is killed at the first step.
func TestCursorBackgroundShellAllowsLegitimateFollowOnWork(t *testing.T) {
	t.Parallel()

	run := runCursorFake(t, cursorFakeWorkThenResult, 600*time.Millisecond, 12*time.Second)

	if run.result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", run.result.Status, run.result.Error)
	}
	if run.result.Output != "dev server checked and stopped" {
		t.Errorf("output = %q, want the Cursor terminal result preserved", run.result.Output)
	}
	if strings.Contains(run.result.Error, "background shell") {
		t.Errorf("error = %q, want no background-liveness failure on a normal run", run.result.Error)
	}
	if run.result.SessionID != cursorBGSessionID {
		t.Errorf("session id = %q, want %q", run.result.SessionID, cursorBGSessionID)
	}
}

// TestCursorBackgroundShellProgressExtendsGuard is Test C: the guard measures
// inactivity, not wall-clock time since the shell started. Five progress pulses
// land 500ms apart inside a 1s deadline, so the run spans several guard
// intervals and still completes. A deadline that never moved would fire ~1s in.
func TestCursorBackgroundShellProgressExtendsGuard(t *testing.T) {
	t.Parallel()

	run := runCursorFake(t, cursorFakePulses, time.Second, 12*time.Second)

	if run.elapsed < 2*time.Second {
		t.Errorf("elapsed = %s, want the run to outlive at least one guard interval", run.elapsed)
	}
	if run.result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", run.result.Status, run.result.Error)
	}
	if run.result.Output != "dev server checked and stopped" {
		t.Errorf("output = %q, want the Cursor terminal result preserved", run.result.Output)
	}
}

// TestCursorBackgroundOutputTextCannotFakeLifecycleIsStructural is Test D: the
// observation comes from the shell payload's result root boolean and nowhere
// else.
func TestCursorBackgroundOutputTextCannotFakeLifecycleIsStructural(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		event          string
		wantName       string
		wantBackground bool
		wantResult     string
	}{
		{
			name: "root boolean true is the observation",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c1",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},"result":{"success":{"exitCode":0},"isBackground":true},"toolCallId":"c1"}}}`,
			wantName:       "shell",
			wantBackground: true,
			wantResult:     `{"success":{"exitCode":0},"isBackground":true}`,
		},
		{
			name: "isBackground on another tool is not a launched shell",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c9",` +
				`"tool_call":{"readToolCall":{"args":{"path":"server.log"},` +
				`"result":{"content":"ok","isBackground":true},"toolCallId":"c9"}}}`,
			wantName:   "read",
			wantResult: `{"content":"ok","isBackground":true}`,
		},
		{
			name: "explicit false is foreground",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c2",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"ls"},"result":{"success":{"exitCode":0},"isBackground":false},"toolCallId":"c2"}}}`,
			wantName:   "shell",
			wantResult: `{"success":{"exitCode":0},"isBackground":false}`,
		},
		{
			name: "isBackground only inside captured stdout is not an observation",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c3",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"grep -r isBackground ."},` +
				`"result":{"stdout":"{\"isBackground\":true}","exitCode":0},"toolCallId":"c3"}}}`,
			wantName:   "shell",
			wantResult: `{"stdout":"{\"isBackground\":true}","exitCode":0}`,
		},
		{
			name: "a nested isBackground is not a root field",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c4",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},` +
				`"result":{"meta":{"isBackground":true},"exitCode":0},"toolCallId":"c4"}}}`,
			wantName:   "shell",
			wantResult: `{"meta":{"isBackground":true},"exitCode":0}`,
		},
		{
			name: "a string where the boolean belongs is not an observation",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c5",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},` +
				`"result":{"isBackground":"true"},"toolCallId":"c5"}}}`,
			wantName:   "shell",
			wantResult: `{"isBackground":"true"}`,
		},
		{
			name: "non-object result is not an observation",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c6",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},"result":[{"isBackground":true}],"toolCallId":"c6"}}}`,
			wantName:   "shell",
			wantResult: `[{"isBackground":true}]`,
		},
		{
			name: "missing result stays foreground",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c7",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},"toolCallId":"c7"}}}`,
			wantName: "shell",
		},
		{
			name: "null result stays foreground",
			event: `{"type":"tool_call","subtype":"completed","call_id":"c8",` +
				`"tool_call":{"shellToolCall":{"args":{"command":"serve"},"result":null,"toolCallId":"c8"}}}`,
			wantName:   "shell",
			wantResult: `null`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var evt cursorStreamEvent
			if err := json.Unmarshal([]byte(tt.event), &evt); err != nil {
				t.Fatalf("unmarshal event: %v", err)
			}
			call := parseCursorToolCall(&evt)
			if call.Background != tt.wantBackground {
				t.Errorf("Background = %v, want %v", call.Background, tt.wantBackground)
			}
			if call.Result != tt.wantResult {
				t.Errorf("Result = %q, want %q (raw result must be preserved verbatim)", call.Result, tt.wantResult)
			}
			if call.Name != tt.wantName || call.CallID == "" {
				t.Errorf("tool identity lost: name=%q want %q, callID=%q", call.Name, tt.wantName, call.CallID)
			}
		})
	}
}

// TestCursorBackgroundShellMalformedResultDoesNotArmGuard is Test E: metadata
// that cannot be decoded must not panic, must keep the raw result, and must not
// put the run under the guard — so this stalled run is still ended by the
// execution deadline it always had, not by a background-liveness failure.
func TestCursorBackgroundShellMalformedResultDoesNotArmGuard(t *testing.T) {
	t.Parallel()

	const (
		guard       = 500 * time.Millisecond
		execTimeout = 3 * time.Second
	)

	// A guard armed on this stream would fire ~0.5s in; the deadline is 3s, so
	// the outcome below can only come from the guard having stayed disarmed.
	run := runCursorFake(t, cursorFakeMalformedStall, guard, execTimeout)

	if run.result.Status != "timeout" {
		t.Errorf("status = %q, want timeout: the malformed result is not an observation, so only the execution deadline may end this run; error=%q",
			run.result.Status, run.result.Error)
	}
	if strings.Contains(run.result.Error, "background shell") {
		t.Errorf("error = %q, must not claim a background-liveness failure", run.result.Error)
	}
	if run.result.Output != "" {
		t.Errorf("output = %q, want the partial transcript withheld", run.result.Output)
	}
	if run.result.SessionID != cursorBGSessionID {
		t.Errorf("session id = %q, want %q", run.result.SessionID, cursorBGSessionID)
	}
	// Requirement 1: raw result metadata preserved, whole parse intact.
	var sawRawResult bool
	for _, msg := range run.messages {
		if msg.Type == MessageToolResult && strings.Contains(msg.Output, "isBackground") {
			sawRawResult = true
		}
	}
	if !sawRawResult {
		t.Errorf("malformed tool result was not forwarded unchanged: %+v", run.messages)
	}
}

// TestCursorBackgroundFlagOutsideShellDoesNotArmGuard is the companion of Test E
// for the other half of "structural": the flag has to sit on the *shell* result.
// A read tool reporting `isBackground` at its result root is not a launched
// process, so arming on it would put ordinary runs under a deadline they were
// never meant to have. The run must still be ended only by the execution
// timeout, exactly like a run with no background observation at all.
func TestCursorBackgroundFlagOutsideShellDoesNotArmGuard(t *testing.T) {
	t.Parallel()

	run := runCursorFake(t, cursorFakeNonShellStall, 500*time.Millisecond, 3*time.Second)

	if run.result.Status != "timeout" {
		t.Errorf("status = %q, want %q: a non-shell isBackground must not arm the guard",
			run.result.Status, "timeout")
	}
	if !strings.Contains(run.result.Error, "timed out after 3s") {
		t.Errorf("error = %q, want the execution deadline's own wording", run.result.Error)
	}
	if strings.Contains(run.result.Error, "background shell") {
		t.Errorf("error = %q, must not claim a background shell that was never launched", run.result.Error)
	}
	if run.elapsed < 2*time.Second || run.elapsed > 9*time.Second {
		t.Errorf("elapsed = %s, want the 3s execution deadline to be what ended the run", run.elapsed)
	}
}

// TestCursorBackgroundShellTerminalResultBeatsObservation is Test F: once a
// valid terminal result arrives, Cursor's native protocol owns the outcome. A
// latched background observation must not override it during finalization.
func TestCursorBackgroundShellTerminalResultBeatsObservation(t *testing.T) {
	t.Parallel()

	// The guard is deliberately longer than the run: the result must win on its
	// own authority, not because nothing had time to fire.
	run := runCursorFake(t, cursorFakeResultWins, 30*time.Second, 12*time.Second)

	if run.result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", run.result.Status, run.result.Error)
	}
	if run.result.Output != "dev server checked and stopped" {
		t.Errorf("output = %q, want the Cursor terminal result preserved", run.result.Output)
	}
	if run.result.DurationMs <= 0 {
		t.Errorf("duration = %d, want a normal completed run", run.result.DurationMs)
	}
}

// TestCursorBackgroundShellNoiseDoesNotExtendGuard covers the other half of
// requirement 4: the deadline measures *semantic* progress. A run whose child
// keeps producing stderr output, unknown event types and malformed lines is
// still stalled, and must not be allowed to hold its slot indefinitely.
func TestCursorBackgroundShellNoiseDoesNotExtendGuard(t *testing.T) {
	t.Parallel()

	const (
		guard       = 800 * time.Millisecond
		execTimeout = 12 * time.Second
	)

	run := runCursorFake(t, cursorFakeUnverifiedActivity, guard, execTimeout)

	if run.elapsed > 6*time.Second {
		t.Errorf("elapsed = %s, want noisy-but-meaningless output to leave the deadline untouched", run.elapsed)
	}
	run.assertGuardFailure(t)
}

// TestCursorSemanticProgressEvent pins which events may refresh the deadline.
// Everything unrecognized is excluded by construction: believing it would let
// the protocol drift MUL-5231 and MUL-5434 already fixed extend a stalled run
// forever.
func TestCursorSemanticProgressEvent(t *testing.T) {
	t.Parallel()

	progress := [][2]string{
		{"assistant", ""},
		{"thinking", "delta"},
		{"thinking", "completed"},
		{"tool_call", "started"},
		{"tool_call", "completed"},
		{"tool_use", ""},
		{"tool_result", ""},
		{"text", ""},
		{"step_finish", ""},
	}
	for _, evt := range progress {
		if !cursorSemanticProgressEvent(evt[0], evt[1]) {
			t.Errorf("(%s,%s) must count as semantic progress", evt[0], evt[1])
		}
	}

	notProgress := [][2]string{
		{"system", "init"},
		{"system", "error"},
		{"error", ""},
		{"result", "success"},
		{"thinking", "progress"}, // the non-terminal noise MUL-5231/MUL-5434 exclude
		{"thinking", ""},
		// Execute handles exactly started/completed and counts everything else as
		// unhandled, so a subtype it drops cannot be evidence of progress.
		{"tool_call", "progress"},
		{"tool_call", "delta"},
		{"tool_call", ""},
		{"background_log", ""}, // an unhandled type is not evidence of progress
		{"user", ""},           // a CLI echoing our own prompt proves nothing
		{"connection", ""},
		{"", ""},
	}
	for _, evt := range notProgress {
		if cursorSemanticProgressEvent(evt[0], evt[1]) {
			t.Errorf("(%s,%s) must NOT count as semantic progress", evt[0], evt[1])
		}
	}
}

// TestCursorBackgroundGuardFiresOnceThroughCancellationPath exercises the guard
// directly: one observation, one cancellation, a dedicated cause, and a watcher
// that is gone once Stop returns.
func TestCursorBackgroundGuardFiresOnceThroughCancellationPath(t *testing.T) {
	t.Parallel()

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g := newCursorBackgroundGuard(context.Background(), runCtx, cancel, 40*time.Millisecond)
	if got := g.Cause(); got != "" {
		t.Fatalf("Cause = %q before any background observation, want empty", got)
	}

	for i := 0; i < 20; i++ {
		g.ObserveBackground()
	}

	if !waitForGuardFire(g, 2*time.Second) {
		t.Fatal("guard never fired for a stalled background run")
	}
	if got := g.Cause(); got != cursorBackgroundLivenessGuardError {
		t.Errorf("Cause = %q, want the dedicated background-liveness reason", got)
	}
	if runCtx.Err() == nil {
		t.Error("the guard must end the run through the existing cancellation path")
	}

	stopGuardWithin(t, g, 2*time.Second)
	g.Stop() // idempotent: the result path and the deferred teardown both call it
}

// TestCursorBackgroundGuardProgressExtendsDeadline proves the deadline rolls
// forward on semantic progress and only expires once the stream goes quiet.
func TestCursorBackgroundGuardProgressExtendsDeadline(t *testing.T) {
	t.Parallel()

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const guard = 200 * time.Millisecond
	g := newCursorBackgroundGuard(context.Background(), runCtx, cancel, guard)
	g.ObserveBackground()

	// Six pulses at a third of the interval each: 600ms of supervised time,
	// three times the deadline.
	for i := 0; i < 6; i++ {
		time.Sleep(guard / 3)
		g.ObserveProgress()
	}
	if got := g.Cause(); got != "" {
		t.Fatalf("Cause = %q while progress kept landing, want empty", got)
	}
	if !waitForGuardFire(g, 2*time.Second) {
		t.Fatal("guard never fired once the progress stopped")
	}
	stopGuardWithin(t, g, 2*time.Second)
}

// A timer notification selected before a progress reset is processed must be
// checked against the observation time, not treated as proof of inactivity.
func TestCursorBackgroundGuardStaleTimerAfterProgress(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		g := newCursorBackgroundGuard(context.Background(), runCtx, cancel, time.Minute)
		defer g.Stop()
		g.ObserveBackground()
		synctest.Wait()

		time.Sleep(59 * time.Second)
		g.ObserveProgress()
		time.Sleep(time.Second)
		// Model the old timer's expiry reaching fire after progress was
		// observed. Calling the expiry gate directly makes this ordering
		// deterministic instead of depending on select choosing timer.C.
		g.fire()
		if runCtx.Err() != nil || g.Cause() != "" {
			t.Fatalf("stale timer cancelled a progressing run: context=%v cause=%q", runCtx.Err(), g.Cause())
		}

		// The deadline is exactly one interval after the observation, not
		// one interval after the stale timer was handled.
		time.Sleep(58 * time.Second)
		if runCtx.Err() != nil {
			t.Fatal("guard cancelled before a full interval without progress")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if runCtx.Err() == nil || g.Cause() != cursorBackgroundLivenessGuardError {
			t.Fatalf("guard did not expire after progress stopped: context=%v cause=%q", runCtx.Err(), g.Cause())
		}
	})
}

// TestCursorBackgroundGuardYieldsToRealCauses covers precedence: an execution
// deadline or a caller cancellation that was already in effect is not the
// guard's to claim, and the guard must not cancel on top of it.
func TestCursorBackgroundGuardYieldsToRealCauses(t *testing.T) {
	t.Parallel()

	t.Run("execution deadline", func(t *testing.T) {
		t.Parallel()

		parent := context.Background()
		runCtx, cancel := context.WithTimeout(parent, 20*time.Millisecond)
		defer cancel()
		time.Sleep(80 * time.Millisecond) // let the deadline be the real cause

		g := newCursorBackgroundGuard(parent, runCtx, cancel, 20*time.Millisecond)
		g.ObserveBackground()
		time.Sleep(200 * time.Millisecond)

		if got := g.Cause(); got != "" {
			t.Errorf("Cause = %q, want the timeout to keep its own classification", got)
		}
		stopGuardWithin(t, g, 2*time.Second)
	})

	t.Run("external cancellation", func(t *testing.T) {
		t.Parallel()

		parent, cancelParent := context.WithCancel(context.Background())
		runCtx, cancel := runContext(parent, 0)
		cancelParent() // the caller ended this run, not the guard
		time.Sleep(20 * time.Millisecond)

		g := newCursorBackgroundGuard(parent, runCtx, cancel, 20*time.Millisecond)
		g.ObserveBackground()
		time.Sleep(200 * time.Millisecond)

		if got := g.Cause(); got != "" {
			t.Errorf("Cause = %q, want external cancellation to keep its own classification", got)
		}
		if runCtx.Err() == nil {
			t.Error("runCtx should already be cancelled by the parent")
		}
		stopGuardWithin(t, g, 2*time.Second)
	})
}

// TestCursorBackgroundGuardWithoutObservationOwnsNoTimer documents that the
// overwhelmingly common foreground run pays nothing: Stop on a guard that never
// armed must still return, and there is no watcher to leak.
func TestCursorBackgroundGuardWithoutObservationOwnsNoTimer(t *testing.T) {
	t.Parallel()

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g := newCursorBackgroundGuard(context.Background(), runCtx, cancel, 20*time.Millisecond)
	g.ObserveProgress() // progress before any background shell is inert
	time.Sleep(100 * time.Millisecond)

	if got := g.Cause(); got != "" {
		t.Errorf("Cause = %q, want empty: no background shell was ever observed", got)
	}
	if runCtx.Err() != nil {
		t.Error("an unobserved guard must not cancel the run")
	}
	stopGuardWithin(t, g, 2*time.Second)
}

// TestCursorTerminalResultObservedDecidesByObservationOrder pins the precedence
// rule the Execute result path applies through cursorTerminalResultObserved: a
// native terminal result outranks the guard only when it arrives first.
//
// The scenarios are the three orderings that can happen, each set up directly on
// the guard rather than by racing a child process, because "the result bytes were
// already buffered when the guard fired" is not something a subprocess test can
// force deterministically.
func TestCursorTerminalResultObservedDecidesByObservationOrder(t *testing.T) {
	t.Parallel()

	t.Run("result observed before the guard fires is authoritative", func(t *testing.T) {
		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		g := newCursorBackgroundGuard(context.Background(), runCtx, cancel, time.Hour)
		g.ObserveBackground()

		if !cursorTerminalResultObserved(g) {
			t.Fatal("a terminal result that lands inside the window must own the outcome")
		}
	})

	t.Run("result observed after the guard fires is not authoritative", func(t *testing.T) {
		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		g := newCursorBackgroundGuard(context.Background(), runCtx, cancel, 20*time.Millisecond)
		g.ObserveBackground()
		if !waitForGuardFire(g, 2*time.Second) {
			t.Fatal("guard never fired")
		}

		if cursorTerminalResultObserved(g) {
			t.Fatal("a result that lands after the guard ended the run must not revive it as completed")
		}
	})

	// Precedence 2 is untouched: the guard never claims a run the deadline or the
	// caller had already ended, so a buffered result in such a run is still
	// authoritative rather than being stolen by the guard.
	t.Run("a cause the guard declined to claim leaves the result authoritative", func(t *testing.T) {
		parent, cancelParent := context.WithCancel(context.Background())
		defer cancelParent()

		runCtx, runCancel := context.WithDeadline(parent, time.Now().Add(-time.Second))
		defer runCancel()

		g := newCursorBackgroundGuard(parent, runCtx, runCancel, time.Millisecond)
		g.ObserveBackground()
		if !cursorTerminalResultObserved(g) {
			t.Fatal("the guard must not demote a result in a run it never claimed")
		}
		if got := g.Cause(); got != "" {
			t.Fatalf("Cause = %q, want the guard to have yielded to the real deadline", got)
		}
	})
}

func waitForGuardFire(g *cursorBackgroundGuard, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if g.Cause() != "" {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// stopGuardWithin asserts the guard's watcher goroutine is gone, because Stop
// only returns once the monitor has closed `done`.
func stopGuardWithin(t *testing.T, g *cursorBackgroundGuard, timeout time.Duration) {
	t.Helper()

	stopped := make(chan struct{})
	go func() {
		g.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(timeout):
		t.Fatal("guard.Stop did not return: the background-liveness watcher outlived the run")
	}
}

// TestCursorBackgroundGuardStopReportsWhetherTheRunWasAlreadyEnded pins the
// ordering contract behind the late-result boundary: Stop reports the guard's
// cause as of the moment it is called, which is the only way the result handler
// can tell an authoritative terminal result from one that arrived after the
// guard had ended the run.
func TestCursorBackgroundGuardStopReportsWhetherTheRunWasAlreadyEnded(t *testing.T) {
	t.Parallel()

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	live := newCursorBackgroundGuard(context.Background(), runCtx, cancel, time.Hour)
	live.ObserveBackground()
	if got := live.Stop(); got != "" {
		t.Errorf("Stop before the deadline = %q, want empty so the terminal result owns the outcome", got)
	}
	if runCtx.Err() != nil {
		t.Error("Stop must only disarm the guard, never cancel the run")
	}

	endedCtx, endedCancel := context.WithCancel(context.Background())
	defer endedCancel()
	fired := newCursorBackgroundGuard(context.Background(), endedCtx, endedCancel, 20*time.Millisecond)
	fired.ObserveBackground()
	if !waitForGuardFire(fired, 2*time.Second) {
		t.Fatal("guard never fired")
	}
	if got := fired.Stop(); got != cursorBackgroundLivenessGuardError {
		t.Errorf("Stop after the guard fired = %q, want the latched cause so a late result cannot win", got)
	}
}

// TestCursorBackgroundGuardConcurrentStopDoesNotPanic covers the idempotency the
// guard documents: the terminal-result path and the deferred teardown can reach
// Stop at the same moment, and a second close of either channel must not panic
// or lose the latched cause.
func TestCursorBackgroundGuardConcurrentStopDoesNotPanic(t *testing.T) {
	t.Parallel()

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := newCursorBackgroundGuard(context.Background(), runCtx, cancel, 20*time.Millisecond)
	g.ObserveBackground()
	if !waitForGuardFire(g, 2*time.Second) {
		t.Fatal("guard never fired")
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = g.Stop()
		}()
	}

	all := make(chan struct{})
	go func() {
		wg.Wait()
		close(all)
	}()
	select {
	case <-all:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Stop did not return")
	}
	if g.Cause() != cursorBackgroundLivenessGuardError {
		t.Errorf("Cause after concurrent Stop = %q, want the latched cause preserved", g.Cause())
	}
}

// TestCursorBackgroundGuardStopIsIdempotentBeforeArm checks the same close
// discipline for a guard that never saw a background shell: no watcher exists to
// close `done`, so repeated Stop calls must still return.
func TestCursorBackgroundGuardStopIsIdempotentBeforeArm(t *testing.T) {
	t.Parallel()

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := newCursorBackgroundGuard(context.Background(), runCtx, cancel, time.Hour)

	for i := 0; i < 3; i++ {
		if got := g.Stop(); got != "" {
			t.Fatalf("Stop #%d = %q, want empty", i+1, got)
		}
	}
	if runCtx.Err() != nil {
		t.Error("an unarmed guard must not cancel the run")
	}
}
