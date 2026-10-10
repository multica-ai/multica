package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fakes below are minimal `zcode app-server` implementations written as
// shell scripts (same pattern as the zeroclaw/ACP fakes): they read NDJSON
// frames from stdin, dispatch on the method, and write NDJSON frames to
// stdout. Nothing here resolves or executes a user-installed zcode CLI —
// ExecutablePath always points at a test-created script, per the package's
// testing contract.

const fakeZcodeSessionID = "ses_zcode_fake"

func zcodeTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// writeFakeZcodeScript materialises script as an executable named "zcode" in
// a temp dir and returns its path.
func writeFakeZcodeScript(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "zcode")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake zcode: %v", err)
	}
	return bin
}

func newZcodeTestBackend(t *testing.T, bin string) Backend {
	t.Helper()
	b, err := New("zcode", Config{
		ExecutablePath: bin,
		Logger:         zcodeTestLogger(),
		TaskID:         "task-1",
	})
	if err != nil {
		t.Fatalf("New(zcode) error: %v", err)
	}
	return b
}

// collectZcodeSession drains Messages concurrently and returns them together
// with the final Result.
func collectZcodeSession(t *testing.T, s *Session) ([]Message, Result) {
	t.Helper()
	var (
		mu   sync.Mutex
		msgs []Message
		wg   sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for msg := range s.Messages {
			mu.Lock()
			msgs = append(msgs, msg)
			mu.Unlock()
		}
	}()
	res := <-s.Result
	wg.Wait()
	return msgs, res
}

func zcodeTextMessages(msgs []Message) []string {
	var out []string
	for _, m := range msgs {
		if m.Type == MessageText {
			out = append(out, m.Content)
		}
	}
	return out
}

// zcodeEventFrame renders one scripted `printf` line that pushes a
// session/event notification with the given seq/type/payload.
func zcodeEventFrame(seq int, evType, payload string) string {
	return fmt.Sprintf("      printf '{\"method\":\"session/event\",\"params\":{\"type\":\"%s\",\"seq\":%d,\"sessionId\":\"%s\",\"payload\":%s}}\\n'\n", evType, seq, fakeZcodeSessionID, payload)
}

// fakeZcodeHappyScript answers session/create, session/subscribe and
// session/send; before send it replays events with seqs below the subscribe
// baseline (replay-gate fixture); after the send acknowledgement it streams
// the scripted `postSend` frames verbatim (each must already be a full NDJSON
// line, usually from zcodeEventFrame). Unknown requests get -32603.
func fakeZcodeHappyScript(postSend string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"session/create"'*)
      printf '{"id":%s,"result":{"protocol":{"name":"ZCode Protocol","version":1},"session":{"sessionId":"` + fakeZcodeSessionID + `"}}}\n' "$id"
      # Replay frames pushed before the client subscribed: seqs below the
      # subscribe baseline, dropped by the gate.
      printf '{"method":"session/event","params":{"type":"turn.started","seq":1,"sessionId":"` + fakeZcodeSessionID + `","payload":{"turnNumber":0}}}\n'
      printf '{"method":"session/event","params":{"type":"model.streaming","seq":2,"sessionId":"` + fakeZcodeSessionID + `","payload":{"kind":"text_delta","delta":"REPLAY","assistantMessageId":"m_replay","partId":"p1"}}}\n'
      printf '{"method":"session/event","params":{"type":"message.upserted","seq":3,"sessionId":"` + fakeZcodeSessionID + `","payload":{"content":"REPLAY ANSWER","toolCalls":[]}}}\n'
      ;;
    *'"method":"session/subscribe"'*)
      printf '{"id":%s,"result":{"sessionId":"` + fakeZcodeSessionID + `","eventSeq":5,"events":[]}}\n' "$id"
      ;;
    *'"method":"session/send"'*)
      printf '{"id":%s,"result":{"sessionId":"` + fakeZcodeSessionID + `","accepted":true,"stateRevision":7}}\n' "$id"
` + postSend + `      ;;
    *'"method":"session/stop"'*)
      printf '{"id":%s,"result":{}}\n' "$id"
      ;;
    *)
      if [ -n "$id" ]; then
        printf '{"id":%s,"error":{"code":-32603,"message":"internal error"}}\n' "$id"
      fi
      ;;
  esac
done
`
}

// TestZcodeHappyPath covers T1: create → subscribe → send → streamed deltas,
// tool frames and a successful terminal event with usage.
func TestZcodeHappyPath(t *testing.T) {
	t.Parallel()
	postSend := zcodeEventFrame(6, "turn.started", `{"turnNumber":1}`) +
		zcodeEventFrame(7, "model.streaming", `{"kind":"text_delta","delta":"Hello ","assistantMessageId":"m1","partId":"p1"}`) +
		zcodeEventFrame(8, "model.streaming", `{"kind":"text_delta","delta":"world","assistantMessageId":"m1","partId":"p1"}`) +
		zcodeEventFrame(9, "tool.updated", `{"kind":"scheduled","toolCallId":"t1","toolName":"bash","input":{"cmd":"ls"}}`) +
		zcodeEventFrame(10, "tool.updated", `{"kind":"result","toolCallId":"t1","toolName":"bash","result":{"content":"file.txt"}}`) +
		zcodeEventFrame(11, "turn.completed", `{"response":"Final answer","resultType":"success","tokenCount":0,"toolCallCount":1,"duration":12,"usage":{"inputTokens":100,"outputTokens":40,"cacheReadTokens":20,"cacheWriteTokens":10,"reasoningTokens":15}}`)
	bin := writeFakeZcodeScript(t, fakeZcodeHappyScript(postSend))

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "test prompt", ExecOptions{
		Cwd:   t.TempDir(),
		Model: "zai/glm-5.3",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	msgs, res := collectZcodeSession(t, session)

	if res.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", res.Status, res.Error)
	}
	if res.Output != "Final answer" {
		t.Fatalf("expected output %q, got %q", "Final answer", res.Output)
	}
	if res.SessionID != fakeZcodeSessionID {
		t.Fatalf("expected session id %q, got %q", fakeZcodeSessionID, res.SessionID)
	}
	// TerminalObserved must be published by the time Result is in hand.
	if session.TerminalObserved == nil || !session.TerminalObserved() {
		t.Fatal("expected TerminalObserved to be true before Result was delivered")
	}
	// Usage mapping per plan §1.5: input excludes cache (100-20-10), output
	// includes reasoning (40+15).
	usage, ok := res.Usage["zai/glm-5.3"]
	if !ok {
		t.Fatalf("expected usage keyed by model, got %+v", res.Usage)
	}
	if usage.InputTokens != 70 || usage.OutputTokens != 55 || usage.CacheReadTokens != 20 || usage.CacheWriteTokens != 10 {
		t.Fatalf("unexpected usage mapping: %+v", usage)
	}

	texts := zcodeTextMessages(msgs)
	if len(texts) != 2 || texts[0] != "Hello " || texts[1] != "world" {
		t.Fatalf("expected the two streamed deltas, got %q", texts)
	}
	var sawToolUse, sawToolResult bool
	for _, m := range msgs {
		if m.Type == MessageToolUse && m.CallID == "t1" && m.Tool == "bash" {
			sawToolUse = true
		}
		if m.Type == MessageToolResult && m.CallID == "t1" && m.Output == "file.txt" {
			sawToolResult = true
		}
	}
	if !sawToolUse || !sawToolResult {
		t.Fatalf("expected tool use and result messages, got %+v", msgs)
	}
}

// TestZcodeReplayFramesDropped covers T16: events pushed before the send (and
// with seqs below the subscribe baseline) never reach Messages.
func TestZcodeReplayFramesDropped(t *testing.T) {
	t.Parallel()
	postSend := zcodeEventFrame(6, "turn.completed", `{"response":"done","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}`)
	bin := writeFakeZcodeScript(t, fakeZcodeHappyScript(postSend))

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	msgs, res := collectZcodeSession(t, session)
	if res.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", res.Status, res.Error)
	}
	for _, m := range msgs {
		if m.Type == MessageText && (m.Content == "REPLAY" || m.Content == "REPLAY ANSWER") {
			t.Fatalf("replay frame leaked into transcript: %+v", m)
		}
	}
}

// TestZcodeUpsertedReconcile covers T2: deltas for one assistant message
// followed by the authoritative upserted snapshot produce no duplicate text.
func TestZcodeUpsertedReconcile(t *testing.T) {
	t.Parallel()
	postSend := zcodeEventFrame(6, "model.streaming", `{"kind":"text_delta","delta":"Hello ","assistantMessageId":"m1","partId":"p1"}`) +
		zcodeEventFrame(7, "model.streaming", `{"kind":"text_delta","delta":"world","assistantMessageId":"m1","partId":"p1"}`) +
		zcodeEventFrame(8, "message.upserted", `{"content":"Hello world","toolCalls":[]}`) +
		zcodeEventFrame(9, "turn.completed", `{"response":"Hello world","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}`)
	bin := writeFakeZcodeScript(t, fakeZcodeHappyScript(postSend))

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	msgs, res := collectZcodeSession(t, session)
	if res.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", res.Status, res.Error)
	}
	texts := zcodeTextMessages(msgs)
	if len(texts) != 2 || texts[0] != "Hello " || texts[1] != "world" {
		t.Fatalf("expected exactly the two deltas without duplication, got %q", texts)
	}
}

// TestZcodeUpsertedSuffixAndDivergence covers plan §2.5's other reconcile
// branches: a prefix-matched authoritative snapshot emits only the missing
// suffix; a divergent snapshot falls back to a corrective full text.
func TestZcodeUpsertedSuffixAndDivergence(t *testing.T) {
	t.Parallel()
	postSend := zcodeEventFrame(6, "model.streaming", `{"kind":"text_delta","delta":"Hello","assistantMessageId":"m1","partId":"p1"}`) +
		zcodeEventFrame(7, "message.upserted", `{"content":"Hello world","toolCalls":[]}`) +
		// New assistant message: deltas diverge from the next snapshot.
		zcodeEventFrame(8, "model.streaming", `{"kind":"text_delta","delta":"drift","assistantMessageId":"m2","partId":"p1"}`) +
		zcodeEventFrame(9, "message.upserted", `{"content":"authoritative","toolCalls":[]}`) +
		zcodeEventFrame(10, "turn.completed", `{"response":"authoritative","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}`)
	bin := writeFakeZcodeScript(t, fakeZcodeHappyScript(postSend))

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	msgs, res := collectZcodeSession(t, session)
	if res.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", res.Status, res.Error)
	}
	texts := zcodeTextMessages(msgs)
	// The transcript is append-only: the "Hello" delta, the " world" suffix
	// reconciled from the prefix-matched snapshot, then the divergent "drift"
	// delta stays (it cannot be retracted) and the corrective authoritative
	// full text follows — mirroring codex's delta-then-reconcile behaviour.
	want := []string{"Hello", " world", "drift", "authoritative"}
	if len(texts) != len(want) {
		t.Fatalf("expected %q, got %q", want, texts)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Fatalf("expected %q, got %q", want, texts)
		}
	}
	if res.Output != "authoritative" {
		t.Fatalf("expected authoritative Output, got %q", res.Output)
	}
}

// TestZcodeResumeSuccess covers T3.
func TestZcodeResumeSuccess(t *testing.T) {
	t.Parallel()
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"session/resume"'*)
      printf '{"id":%s,"result":{"session":{"sessionId":"ses_old"}}}\n' "$id"
      printf '{"method":"session/event","params":{"type":"session.resumed","seq":2,"sessionId":"ses_old","payload":{"directory":"/tmp","messageCount":3}}}\n'
      ;;
    *'"method":"session/subscribe"'*)
      printf '{"id":%s,"result":{"sessionId":"ses_old","eventSeq":5,"events":[]}}\n' "$id"
      ;;
    *'"method":"session/send"'*)
      printf '{"id":%s,"result":{"sessionId":"ses_old","accepted":true,"stateRevision":2}}\n' "$id"
` + zcodeEventFrame(6, "turn.completed", `{"response":"resumed answer","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":2}`) + `      ;;
    *'"method":"session/stop"'*)
      printf '{"id":%s,"result":{}}\n' "$id"
      ;;
  esac
done
`
	bin := writeFakeZcodeScript(t, script)

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "next prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_old",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	msgs, res := collectZcodeSession(t, session)
	if res.Status != "completed" || res.Output != "resumed answer" {
		t.Fatalf("expected completed resumed answer, got status=%q output=%q error=%q", res.Status, res.Output, res.Error)
	}
	if res.SessionID != "ses_old" {
		t.Fatalf("expected session id preserved, got %q", res.SessionID)
	}
	if res.ResumeRejected {
		t.Fatal("expected ResumeRejected=false on a successful resume")
	}
	if got := zcodeTextMessages(msgs); len(got) != 0 {
		t.Fatalf("expected no message.upserted content from resume, got %+v", msgs)
	}
}

// TestZcodeResumeRejected covers T4: -32004 on resume is positive evidence of
// a rejected resume, with the session id preserved.
func TestZcodeResumeRejected(t *testing.T) {
	t.Parallel()
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"session/resume"'*)
      printf '{"id":%s,"error":{"code":-32004,"message":"Session not found: ses_gone"}}\n' "$id"
      ;;
  esac
done
`
	bin := writeFakeZcodeScript(t, script)

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_gone",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	_, res := collectZcodeSession(t, session)
	if res.Status != "failed" {
		t.Fatalf("expected failed, got %q", res.Status)
	}
	if !res.ResumeRejected {
		t.Fatal("expected ResumeRejected=true for -32004")
	}
	if res.SessionID != "ses_gone" {
		t.Fatalf("expected session id preserved for daemon gating, got %q", res.SessionID)
	}
}

// TestZcodeResumeInternalErrorNotRejection covers T5: a -32603 on resume must
// NOT set ResumeRejected — only the structured rejection code may.
func TestZcodeResumeInternalErrorNotRejection(t *testing.T) {
	t.Parallel()
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"session/resume"'*)
      printf '{"id":%s,"error":{"code":-32603,"message":"internal error"}}\n' "$id"
      ;;
  esac
done
`
	bin := writeFakeZcodeScript(t, script)

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_old",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	_, res := collectZcodeSession(t, session)
	if res.Status != "failed" {
		t.Fatalf("expected failed, got %q", res.Status)
	}
	if res.ResumeRejected {
		t.Fatal("expected ResumeRejected=false for a non-rejection failure")
	}
}

// TestZcodeCancelViaStop covers T6: daemon cancellation sends session/stop
// and maps the cancelled completion to aborted.
func TestZcodeCancelViaStop(t *testing.T) {
	t.Parallel()
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"session/create"'*)
      printf '{"id":%s,"result":{"session":{"sessionId":"` + fakeZcodeSessionID + `"}}}\n' "$id"
      ;;
    *'"method":"session/subscribe"'*)
      printf '{"id":%s,"result":{"sessionId":"` + fakeZcodeSessionID + `","eventSeq":5,"events":[]}}\n' "$id"
      ;;
    *'"method":"session/send"'*)
      printf '{"id":%s,"result":{"sessionId":"` + fakeZcodeSessionID + `","accepted":true,"stateRevision":7}}\n' "$id"
      # Stay silent: the turn hangs until the daemon cancels.
      ;;
    *'"method":"session/stop"'*)
      printf '{"id":%s,"result":{}}\n' "$id"
` + zcodeEventFrame(6, "turn.completed", `{"response":"","resultType":"cancelled","tokenCount":0,"toolCallCount":0,"duration":5}`) + `      ;;
  esac
done
`
	bin := writeFakeZcodeScript(t, script)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := newZcodeTestBackend(t, bin).Execute(ctx, "prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	done := make(chan Result, 1)
	go func() {
		for range session.Messages {
		}
		done <- <-session.Result
	}()
	time.Sleep(200 * time.Millisecond) // let the send land
	cancel()
	select {
	case res := <-done:
		if res.Status != "aborted" {
			t.Fatalf("expected aborted, got status=%q error=%q", res.Status, res.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the cancelled result")
	}
}

// TestZcodeHardTimeout covers T7: a silent turn dies with status timeout.
func TestZcodeHardTimeout(t *testing.T) {
	t.Parallel()
	bin := writeFakeZcodeScript(t, fakeZcodeHappyScript(""))

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "prompt", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 300 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	_, res := collectZcodeSession(t, session)
	if res.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", res.Status, res.Error)
	}
}

// TestZcodeTurnFailed covers T8.
func TestZcodeTurnFailed(t *testing.T) {
	t.Parallel()
	postSend := zcodeEventFrame(6, "turn.failed", `{"error":{"type":"provider","message":"provider exploded","retryable":false},"turnPhase":"executing"}`)
	bin := writeFakeZcodeScript(t, fakeZcodeHappyScript(postSend))

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	_, res := collectZcodeSession(t, session)
	if res.Status != "failed" {
		t.Fatalf("expected failed, got %q", res.Status)
	}
	if !strings.Contains(res.Error, "provider exploded") {
		t.Fatalf("expected error to carry the failure message, got %q", res.Error)
	}
}

// TestZcodeTurnFailedRetryableHint covers plan §2.5's retryable surfacing.
func TestZcodeTurnFailedRetryableHint(t *testing.T) {
	t.Parallel()
	postSend := zcodeEventFrame(6, "turn.failed", `{"error":{"type":"provider","message":"boom","attribution":{"source":"network"}},"turnPhase":"executing"}`)
	bin := writeFakeZcodeScript(t, fakeZcodeHappyScript(postSend))

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	_, res := collectZcodeSession(t, session)
	if !strings.Contains(res.Error, "retryable") {
		t.Fatalf("expected a retryable hint in the error, got %q", res.Error)
	}
}

// TestZcodePermissionAndUnknownReverseRequests covers T9 + T10 at the
// transport level: interaction/requestPermission is denied fail-closed and an
// unknown server-originated request is answered with -32601.
func TestZcodePermissionAndUnknownReverseRequests(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	c := &zcodeClient{stdin: &buf, pending: make(map[int]*pendingRPC), processDone: make(chan struct{})}

	c.handleLine(`{"id":"server-1","method":"interaction/requestPermission","params":{"toolCallId":"t1"}}`)
	if !strings.Contains(buf.String(), `"decision":"deny"`) {
		t.Fatalf("expected a deny response, got %q", buf.String())
	}

	// A real app-server asks this during session/create; an unanswered
	// request fails the create with -32022 after its 15s timeout, so the
	// client must answer with the schema defaults.
	buf.Reset()
	c.handleLine(`{"id":"server-2","method":"session/requestRuntimePreferences","params":{"sessionId":"ses","scope":"runtime-materialization"}}`)
	if !strings.Contains(buf.String(), `"nativeSearchEnhancementsEnabled":false`) ||
		!strings.Contains(buf.String(), `"modelContextBudgetStrategy":"preflight-v1"`) {
		t.Fatalf("expected runtime-preferences defaults, got %q", buf.String())
	}

	buf.Reset()
	c.handleLine(`{"id":"server-3","method":"workspace/generateText","params":{}}`)
	if !strings.Contains(buf.String(), "-32601") {
		t.Fatalf("expected -32601 for an unsupported reverse request, got %q", buf.String())
	}

	// Notifications other than session/event are ignored without effect.
	buf.Reset()
	c.handleLine(`{"method":"startup/storageState","params":{}}`)
	c.handleLine(`{"method":"state.updated","params":{}}`)
	if buf.String() != "" {
		t.Fatalf("expected no response to plain notifications, got %q", buf.String())
	}
}

// TestZcodeUnknownNotificationDoesNotBreakFlow covers T10 end-to-end.
func TestZcodeUnknownNotificationDoesNotBreakFlow(t *testing.T) {
	t.Parallel()
	postSend := `      printf '{"method":"mcpResourceSamples","params":{}}\n'
      printf '{"id":"srv-9","method":"interaction/browserList","params":{}}\n'
` + zcodeEventFrame(6, "turn.completed", `{"response":"ok","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}`)
	bin := writeFakeZcodeScript(t, fakeZcodeHappyScript(postSend))

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	_, res := collectZcodeSession(t, session)
	if res.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", res.Status, res.Error)
	}
}

// TestZcodeStderrFailurePromotion covers T15: a CLI that dies before the
// handshake completes fails the run with the stderr tail attached.
func TestZcodeStderrFailurePromotion(t *testing.T) {
	t.Parallel()
	bin := writeFakeZcodeScript(t, `#!/bin/sh
echo "zcode: authentication failed: invalid api key" >&2
exit 1
`)

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	_, res := collectZcodeSession(t, session)
	if res.Status != "failed" {
		t.Fatalf("expected failed, got %q", res.Status)
	}
	if !strings.Contains(res.Error, "authentication failed") {
		t.Fatalf("expected the stderr tail in the error, got %q", res.Error)
	}
}

// TestZcodeThinkingDeltas covers T19: reasoning deltas map to MessageThinking.
func TestZcodeThinkingDeltas(t *testing.T) {
	t.Parallel()
	postSend := zcodeEventFrame(6, "model.streaming", `{"kind":"reasoning_delta","delta":"thinking...","assistantMessageId":"m1","partId":"r1"}`) +
		zcodeEventFrame(7, "turn.completed", `{"response":"done","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}`)
	bin := writeFakeZcodeScript(t, fakeZcodeHappyScript(postSend))

	session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	msgs, res := collectZcodeSession(t, session)
	if res.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", res.Status, res.Error)
	}
	sawThinking := false
	for _, m := range msgs {
		if m.Type == MessageThinking && m.Content == "thinking..." {
			sawThinking = true
		}
		if m.Type == MessageText {
			t.Fatalf("expected no MessageText without text deltas, got %+v", m)
		}
	}
	if !sawThinking {
		t.Fatalf("expected a thinking message, got %+v", msgs)
	}
}

// TestZcodeModelSelectionSplit covers T14.
func TestZcodeModelSelectionSplit(t *testing.T) {
	t.Parallel()
	provider, model, ok := splitZcodeModelSelection("zai/glm-5.3")
	if !ok || provider != "zai" || model != "glm-5.3" {
		t.Fatalf("expected zai/glm-5.3 split, got %q/%q ok=%v", provider, model, ok)
	}
	if _, _, ok := splitZcodeModelSelection("glm-5.3"); ok {
		t.Fatal("expected a bare id to be rejected (no fabricated providerId)")
	}
	if _, _, ok := splitZcodeModelSelection(""); ok {
		t.Fatal("expected an empty model to be rejected")
	}
	if _, _, ok := splitZcodeModelSelection("/glm"); ok {
		t.Fatal("expected an empty provider to be rejected")
	}
}

// TestZcodeBlockedArgs covers T12.
func TestZcodeBlockedArgs(t *testing.T) {
	t.Parallel()
	got := filterCustomArgs([]string{
		"--flag", "app-server", "--surface", "cli", "-p", "hi", "tui", "--prompt=x", "version",
		"--version", "-v", "--keep-me",
	}, zcodeBlockedArgs, zcodeTestLogger())
	want := []string{"--flag", "--keep-me"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// TestZcodeMcpServerConversion covers T11: canonical and alt-key configs both
// map to the {name,value} pair-array shape, and invalid entries are skipped.
func TestZcodeMcpServerConversion(t *testing.T) {
	t.Parallel()
	t.Run("canonical stdio and remote", func(t *testing.T) {
		out, err := buildZcodeMcpServers(json.RawMessage(`{"mcpServers":{"fs":{"command":"npx","args":["-y","fs"],"env":{"TOKEN":"secret"}},"web":{"type":"sse","url":"https://mcp.example/sse","headers":{"X-Key":"k"}}}}`), zcodeTestLogger())
		if err != nil {
			t.Fatalf("buildZcodeMcpServers error: %v", err)
		}
		if len(out) != 2 {
			t.Fatalf("expected 2 entries, got %d: %+v", len(out), out)
		}
		stdio, ok := out[0].(map[string]any)
		if !ok || stdio["command"] != "npx" {
			t.Fatalf("expected stdio entry first, got %+v", out[0])
		}
		env, ok := stdio["env"].([]map[string]any)
		if !ok || len(env) != 1 || env[0]["name"] != "TOKEN" || env[0]["value"] != "secret" {
			t.Fatalf("expected {name,value} env pairs, got %+v", stdio["env"])
		}
		remote, ok := out[1].(map[string]any)
		if !ok || remote["type"] != "sse" || remote["url"] != "https://mcp.example/sse" {
			t.Fatalf("expected remote entry second, got %+v", out[1])
		}
	})
	t.Run("alt-key servers", func(t *testing.T) {
		out, err := buildZcodeMcpServers(json.RawMessage(`{"servers":{"fs":{"command":"npx"}}}`), zcodeTestLogger())
		if err != nil || len(out) != 1 {
			t.Fatalf("expected 1 entry via alt-key tolerance, got %d entries err=%v", len(out), err)
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		if _, err := buildZcodeMcpServers(json.RawMessage(`{`), zcodeTestLogger()); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("entry without command or url skipped", func(t *testing.T) {
		out, err := buildZcodeMcpServers(json.RawMessage(`{"mcpServers":{"bad":{},"good":{"command":"run"}}}`), zcodeTestLogger())
		if err != nil || len(out) != 1 {
			t.Fatalf("expected the bad entry skipped, got %d entries err=%v", len(out), err)
		}
	})
}

// TestZcodeVersionGate covers T13.
func TestZcodeVersionGate(t *testing.T) {
	t.Parallel()
	if err := CheckMinVersion("zcode", "0.16.9"); err != nil {
		t.Fatalf("expected 0.16.9 to pass, got %v", err)
	}
	if err := CheckMinVersion("zcode", "0.15.2"); err == nil {
		t.Fatal("expected 0.15.2 to be rejected below the 0.16.0 floor")
	}
}

// TestZcodeRoleFilter covers T17/T18 discrimination rules.
func TestZcodeRoleFilter(t *testing.T) {
	t.Parallel()
	f := newZcodeRoleFilter("the full prompt text")

	// ① user echo (exact full sent input): dropped, first occurrence only.
	if f.deliver(zcodeUpsertedPayload{Content: "the full prompt text"}) {
		t.Fatal("expected the user echo to be dropped")
	}
	// ① assistant re-quoting the prompt verbatim: delivered (second occurrence).
	if !f.deliver(zcodeUpsertedPayload{Content: "the full prompt text"}) {
		t.Fatal("expected a second occurrence of the same content to be delivered")
	}
	// ② system payloads: dropped by type or compactBoundary.
	if f.deliver(zcodeUpsertedPayload{Type: "init", Content: "hi"}) {
		t.Fatal("expected a system init message to be dropped")
	}
	if f.deliver(zcodeUpsertedPayload{Type: "compact_boundary", Content: "c"}) {
		t.Fatal("expected a compact boundary message to be dropped")
	}
	if f.deliver(zcodeUpsertedPayload{CompactBoundary: json.RawMessage(`{}`), Content: "c"}) {
		t.Fatal("expected a compactBoundary payload to be dropped")
	}
	// ③ toolCalls presence marks assistant (even an empty array).
	if !f.deliver(zcodeUpsertedPayload{Content: "answer", ToolCalls: []json.RawMessage{}}) {
		t.Fatal("expected an assistant payload with toolCalls to be delivered")
	}
	// ④ fallback: bare content is delivered.
	if !f.deliver(zcodeUpsertedPayload{Content: "bare answer"}) {
		t.Fatal("expected a bare content payload to be delivered")
	}
	// Exact matching only: a string that merely extends the input is not an echo.
	f2 := newZcodeRoleFilter("the full prompt")
	if !f2.deliver(zcodeUpsertedPayload{Content: "the full prompt text"}) {
		t.Fatal("expected exact matching to leave a longer assistant text alone")
	}
}

// TestZcodeNewConstructionLockstep proves New can construct the zcode backend
// and that Execute with a missing executable fails fast without spawning.
func TestZcodeNewConstructionLockstep(t *testing.T) {
	t.Parallel()
	b, err := New("zcode", Config{ExecutablePath: missingAgentExecutable(t, "zcode"), Logger: zcodeTestLogger()})
	if err != nil {
		t.Fatalf("New(zcode) error: %v", err)
	}
	if _, ok := b.(*zcodeBackend); !ok {
		t.Fatalf("expected *zcodeBackend, got %T", b)
	}
	if _, err := b.Execute(context.Background(), "p", ExecOptions{}); err == nil {
		t.Fatal("expected Execute to fail for a missing executable")
	} else if !errors.Is(err, exec.ErrNotFound) && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error shape: %v", err)
	}
}

// TestZcodeWorkspaceKeyFallback covers the real-binary smoke finding: the
// strict workspace ref schema rejects an empty workspaceKey with -32602, so a
// backend without a TaskID must still send a non-empty key on session/create.
func TestZcodeWorkspaceKeyFallback(t *testing.T) {
	t.Parallel()
	if got := zcodeWorkspaceKey("task-9", "/tmp/w"); got != "task-9" {
		t.Fatalf("expected the task id to win, got %q", got)
	}
	got := zcodeWorkspaceKey("", "relative/dir")
	if !filepath.IsAbs(got) {
		t.Fatalf("expected an absolute fallback key, got %q", got)
	}
	if !strings.HasSuffix(got, "relative/dir") {
		t.Fatalf("expected the fallback key to resolve the cwd, got %q", got)
	}
}

// TestZcodeCreateSendsNonEmptyWorkspaceKey drives a fake server that captures
// the raw session/create frame and asserts the workspace key is non-empty —
// the exact validation the real CLI enforced in the first smoke run.
func TestZcodeCreateSendsNonEmptyWorkspaceKey(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "create.txt")
	t.Setenv("ZCODE_CAPTURE", capture)
	bin := writeFakeZcodeScript(t, `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"session/create"'*)
      printf '%s\n' "$line" >> "$ZCODE_CAPTURE"
      printf '{"id":%s,"result":{"protocol":{"name":"ZCode Protocol","version":1},"session":{"sessionId":"`+fakeZcodeSessionID+`"}}}\n' "$id"
      ;;
    *'"method":"session/subscribe"'*)
      printf '{"id":%s,"result":{"sessionId":"`+fakeZcodeSessionID+`","eventSeq":5,"events":[]}}\n' "$id"
      ;;
    *'"method":"session/send"'*)
      printf '{"id":%s,"result":{"sessionId":"`+fakeZcodeSessionID+`","accepted":true,"stateRevision":7}}\n' "$id"
`+zcodeEventFrame(6, "turn.completed", `{"response":"done","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}`)+`      ;;
  esac
done
`)
	b, err := New("zcode", Config{
		ExecutablePath: bin,
		Logger:         zcodeTestLogger(),
		// Deliberately no TaskID: the fallback path is what the smoke test hit.
	})
	if err != nil {
		t.Fatalf("New(zcode) error: %v", err)
	}
	cwd := t.TempDir()
	session, err := b.Execute(context.Background(), "p", ExecOptions{Cwd: cwd})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, res := collectZcodeSession(t, session); res.Status != "completed" {
		t.Fatalf("expected completed, got %q error=%q", res.Status, res.Error)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("session/create frame not captured: %v", err)
	}
	var frame struct {
		Params struct {
			Workspace struct {
				WorkspaceKey string `json:"workspaceKey"`
			} `json:"workspace"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("parse captured frame: %v", err)
	}
	if frame.Params.Workspace.WorkspaceKey == "" {
		t.Fatal("session/create carried an empty workspaceKey")
	}
}

// TestZcodeModelSelectionReasoningLevel covers the real-binary smoke finding:
// reasoning-capable models reject session/create with "Reasoning level is
// required" unless the selection carries options.reasoningLevel, so a
// non-empty thinking level must ride inside both the create `model` and the
// send `modelSelection` payloads.
func TestZcodeModelSelectionReasoningLevel(t *testing.T) {
	t.Parallel()
	with := zcodeModelSelection("zai-api", "GLM-5.3-Flash", "low")
	opts, ok := with["options"].(map[string]any)
	if !ok || opts["reasoningLevel"] != "low" {
		t.Fatalf("expected options.reasoningLevel=low, got %+v", with["options"])
	}
	without := zcodeModelSelection("zai-api", "GLM-5.3", "")
	if _, ok := without["options"]; ok {
		t.Fatalf("expected no options for an empty thinking level, got %+v", without)
	}
}

// TestZcodeCreateSendCarryReasoningLevel drives a fake server that captures
// the raw session/create and session/send frames and asserts both carry
// options.reasoningLevel — the exact validation the real CLI enforced when the
// smoke run selected GLM-5.3-Flash without a reasoning level.
func TestZcodeCreateSendCarryReasoningLevel(t *testing.T) {
	captureDir := t.TempDir()
	createCapture := filepath.Join(captureDir, "create.txt")
	sendCapture := filepath.Join(captureDir, "send.txt")
	t.Setenv("ZCODE_CREATE_CAPTURE", createCapture)
	t.Setenv("ZCODE_SEND_CAPTURE", sendCapture)
	bin := writeFakeZcodeScript(t, `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"session/create"'*)
      printf '%s\n' "$line" >> "$ZCODE_CREATE_CAPTURE"
      printf '{"id":%s,"result":{"protocol":{"name":"ZCode Protocol","version":1},"session":{"sessionId":"`+fakeZcodeSessionID+`"}}}\n' "$id"
      ;;
    *'"method":"session/subscribe"'*)
      printf '{"id":%s,"result":{"sessionId":"`+fakeZcodeSessionID+`","eventSeq":5,"events":[]}}\n' "$id"
      ;;
    *'"method":"session/send"'*)
      printf '%s\n' "$line" >> "$ZCODE_SEND_CAPTURE"
      printf '{"id":%s,"result":{"sessionId":"`+fakeZcodeSessionID+`","accepted":true,"stateRevision":7}}\n' "$id"
`+zcodeEventFrame(6, "turn.completed", `{"response":"done","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}`)+`      ;;
  esac
done
`)
	b, err := New("zcode", Config{
		ExecutablePath: bin,
		Logger:         zcodeTestLogger(),
		TaskID:         "task-1",
	})
	if err != nil {
		t.Fatalf("New(zcode) error: %v", err)
	}
	session, err := b.Execute(context.Background(), "p", ExecOptions{
		Cwd:           t.TempDir(),
		Model:         "zai-api/GLM-5.3-Flash",
		ThinkingLevel: "low",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, res := collectZcodeSession(t, session); res.Status != "completed" {
		t.Fatalf("expected completed, got %q error=%q", res.Status, res.Error)
	}

	assertReasoningLevel := func(path string) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("captured frame missing (%s): %v", path, err)
		}
		var frame struct {
			Params struct {
				Model   zcodeSelectionJSON `json:"model"`
				SendSel zcodeSelectionJSON `json:"modelSelection"`
			} `json:"params"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("parse captured frame: %v", err)
		}
		sel := frame.Params.Model
		if sel.ProviderID == "" {
			sel = frame.Params.SendSel
		}
		if sel.ProviderID == "" {
			t.Fatalf("%s carried neither model nor modelSelection: %s", path, raw)
		}
		if sel.ProviderID != "zai-api" || sel.ModelID != "GLM-5.3-Flash" {
			t.Fatalf("%s unexpected selection: %+v", path, sel)
		}
		if sel.Options.ReasoningLevel != "low" {
			t.Fatalf("%s missing options.reasoningLevel=low: %+v", path, sel.Options)
		}
	}
	assertReasoningLevel(createCapture)
	assertReasoningLevel(sendCapture)
}

// zcodeSelectionJSON mirrors the wire shape of the create `model` / send
// `modelSelection` payload for capture-based assertions.
type zcodeSelectionJSON struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
	Options    struct {
		ReasoningLevel string `json:"reasoningLevel"`
	} `json:"options"`
}

// fakeZcodeDiscoveryScript answers one session/create with a snapshot whose
// settings.model.available carries a reasoning-capable current model, a bare
// second model and a disabled third. Before the response it pushes the
// control-frame notifications a real server emits and — like the real 0.16.9
// app-server, verified live — a session/requestRuntimePreferences reverse
// request that must be answered or the create fails with -32022 after its
// 15s timeout. Every received frame is appended to $ZCODE_DISCOVERY_CAPTURE
// for wire assertions.
const fakeZcodeDiscoveryScript = `#!/bin/sh
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$ZCODE_DISCOVERY_CAPTURE"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"session/create"'*)
      printf '{"method":"startup/storagePath","params":{"path":"/tmp/storage"}}\n'
      printf '{"method":"state.updated","params":{"state":"ready"}}\n'
      printf '{"id":101,"method":"session/requestRuntimePreferences","params":{"sessionId":"ses_pending","scope":"runtime-materialization"}}\n'
      # A real server materializes the runtime from the preferences answer
      # before completing the create, so wait for it — this also pins the
      # client's answer into the capture before the response can end the run.
      IFS= read -r prefs_answer
      printf '%s\n' "$prefs_answer" >> "$ZCODE_DISCOVERY_CAPTURE"
      printf '{"id":%s,"result":{"protocol":{"name":"ZCode Protocol","version":1},"session":{"sessionId":"ses_disc"},"settings":{"model":{"current":{"providerId":"zai-api","modelId":"GLM-5.3-Flash"},"available":[{"ref":{"providerId":"zai-api","modelId":"GLM-5.3-Flash"},"label":"GLM-5.3-Flash","contextWindow":200000,"reasoning":{"levels":[{"value":"low","label":"Low"},{"value":"high","label":"High","description":"Deeper reasoning"}],"defaultLevel":"low"}},{"ref":{"providerId":"zai-api","modelId":"GLM-5.3"},"label":"GLM-5.3"},{"ref":{"providerId":"zai-api","modelId":"GLM-5.3-Air"},"label":"GLM-5.3-Air","disabledReason":"requires a coding plan"}]}},"projection":{},"runtime":{},"messages":[]}}\n' "$id"
      ;;
    *)
      if [ -n "$id" ]; then
        printf '{"id":%s,"error":{"code":-32601,"message":"method not supported"}}\n' "$id"
      fi
      ;;
  esac
done
`

// TestZcodeModelDiscovery covers the discovery happy path: the catalog maps
// settings.model.available onto composite `providerId/modelId` ids, the
// registry's current selection badges Default, per-model reasoning levels
// become the thinking catalog (which is what makes the UI render the thinking
// picker for zcode), and disabled entries land in Unavailable with the
// runtime's own reason. The captured create frame pins the wire contract:
// no `jsonrpc` member (the strict schema rejects one) and a deferred draft
// session so discovery leaves nothing in the session store.
func TestZcodeModelDiscovery(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "create.txt")
	t.Setenv("ZCODE_DISCOVERY_CAPTURE", capture)
	bin := writeFakeZcodeScript(t, fakeZcodeDiscoveryScript)

	catalog, err := discoverZcodeModels(context.Background(), NewCommand(bin, nil))
	if err != nil {
		t.Fatalf("discoverZcodeModels error: %v", err)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("expected 2 runnable models, got %d: %+v", len(catalog.Models), catalog.Models)
	}
	flash := catalog.Models[0]
	if flash.ID != "zai-api/GLM-5.3-Flash" || flash.Provider != "zai-api" || flash.Label != "GLM-5.3-Flash" {
		t.Fatalf("unexpected first model: %+v", flash)
	}
	if !flash.Default {
		t.Fatal("expected the registry's current selection to carry the Default badge")
	}
	if flash.Thinking == nil || len(flash.Thinking.SupportedLevels) != 2 ||
		flash.Thinking.DefaultLevel != "low" ||
		flash.Thinking.SupportedLevels[0].Value != "low" ||
		flash.Thinking.SupportedLevels[1].Value != "high" ||
		flash.Thinking.SupportedLevels[1].Description != "Deeper reasoning" {
		t.Fatalf("unexpected thinking catalog: %+v", flash.Thinking)
	}
	if second := catalog.Models[1]; second.ID != "zai-api/GLM-5.3" || second.Default || second.Thinking != nil {
		t.Fatalf("unexpected second model: %+v", second)
	}
	if len(catalog.Unavailable) != 1 ||
		catalog.Unavailable[0].ID != "zai-api/GLM-5.3-Air" ||
		catalog.Unavailable[0].Reason != "requires a coding plan" {
		t.Fatalf("unexpected unavailable list: %+v", catalog.Unavailable)
	}
	if !catalog.Verified() {
		t.Fatal("a discovered catalog must be Verified")
	}

	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("captured create frame missing: %v", err)
	}
	if strings.Contains(string(raw), `"jsonrpc"`) {
		t.Fatalf("zcode wire frames must not carry a jsonrpc member: %s", raw)
	}
	if !strings.Contains(string(raw), `"persistence":"deferred"`) {
		t.Fatalf("discovery must create a deferred draft session: %s", raw)
	}
	// The capture also records the client's answer to the server's
	// requestRuntimePreferences reverse request: it must carry the schema
	// defaults, or a real app-server fails the create with -32022.
	var prefsAnswer struct {
		ID     int `json:"id"`
		Result struct {
			NativeSearchEnhancementsEnabled      bool   `json:"nativeSearchEnhancementsEnabled"`
			MemoryEnabled                        bool   `json:"memoryEnabled"`
			AskUserQuestionAutoResolutionEnabled bool   `json:"askUserQuestionAutoResolutionEnabled"`
			ModelContextBudgetStrategy           string `json:"modelContextBudgetStrategy"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.SplitN(string(raw), "\n", 2)[1]), &prefsAnswer); err != nil {
		t.Fatalf("expected the runtime-preferences answer as the second captured frame: %v\n%s", err, raw)
	}
	if prefsAnswer.ID != 101 ||
		prefsAnswer.Result.NativeSearchEnhancementsEnabled ||
		prefsAnswer.Result.MemoryEnabled ||
		!prefsAnswer.Result.AskUserQuestionAutoResolutionEnabled ||
		prefsAnswer.Result.ModelContextBudgetStrategy != "preflight-v1" {
		t.Fatalf("unexpected runtime-preferences answer: %+v", prefsAnswer)
	}
}

// TestZcodeModelDiscoveryDegradesToEmpty covers the failure paths: a missing
// binary, a server that dies mid-conversation, and a create rejection (an
// unauthenticated account) all yield the empty catalog without an error, so
// the UI keeps manual model entry instead of surfacing a discovery failure.
func TestZcodeModelDiscoveryDegradesToEmpty(t *testing.T) {
	cases := map[string]struct{ bin, script string }{
		"missing binary": {filepath.Join(t.TempDir(), "missing-zcode"), ""},
		"garbage then exit": {"", `#!/bin/sh
read -r line
printf 'not json at all\n'
exit 0
`},
		"create rejected": {"", `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  printf '{"id":%s,"error":{"code":-32603,"message":"not logged in"}}\n' "$id"
done
`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			bin := tc.bin
			if bin == "" {
				bin = writeFakeZcodeScript(t, tc.script)
			}
			catalog, err := discoverZcodeModels(context.Background(), NewCommand(bin, nil))
			if err != nil {
				t.Fatalf("discovery failures must degrade, got error: %v", err)
			}
			if len(catalog.Models) != 0 || catalog.Verified() {
				t.Fatalf("expected an empty unverified catalog, got %+v", catalog)
			}
		})
	}
}

// TestZcodeCatalogFromSnapshotAbsentSettings pins the drift signal: a
// snapshot whose settings.model is missing must report ok=false so the
// caller treats it as schema drift, not as an empty registry.
func TestZcodeCatalogFromSnapshotAbsentSettings(t *testing.T) {
	if _, ok := zcodeCatalogFromSnapshot(json.RawMessage(`{"session":{"sessionId":"ses_x"}}`)); ok {
		t.Fatal("a snapshot without settings.model must not parse as a catalog")
	}
	if _, ok := zcodeCatalogFromSnapshot(json.RawMessage(`not json`)); ok {
		t.Fatal("malformed snapshot must not parse as a catalog")
	}
	empty, ok := zcodeCatalogFromSnapshot(json.RawMessage(`{"settings":{"model":{"available":[]}}}`))
	if !ok || len(empty.Models) != 0 {
		t.Fatalf("an empty registry is a valid empty catalog: ok=%v models=%d", ok, len(empty.Models))
	}
}

// zcodeThinkingDefaultScript serves both halves of an empty-thinking-level
// launch: the discovery create (persistence deferred) answers a catalog whose
// single GLM entry carries the given reasoning fields, and the execution
// create is captured for assertion. resume/subscribe/send complete the run.
// The reasoning JSON is spliced as its own single-quoted shell region
// (” at both seams) so its double quotes survive the parser.
func zcodeThinkingDefaultScript(reasoning string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"session/create"'*)
      case "$line" in
        *'"persistence":"deferred"'*)
          printf '{"id":%s,"result":{"session":{"sessionId":"ses_disc"},"settings":{"model":{"current":{"providerId":"zai-api","modelId":"GLM-5.3"},"available":[{"ref":{"providerId":"zai-api","modelId":"GLM-5.3"},"label":"GLM-5.3","reasoning":''` + reasoning + `''}]}}}}\n' "$id"
          ;;
        *)
          printf '%s\n' "$line" >> "$ZCODE_EXEC_CREATE_CAPTURE"
          printf '{"id":%s,"result":{"protocol":{"name":"ZCode Protocol","version":1},"session":{"sessionId":"ses_run"}}}\n' "$id"
          ;;
      esac
      ;;
    *'"method":"session/subscribe"'*)
      printf '{"id":%s,"result":{"sessionId":"ses_run","eventSeq":5,"events":[]}}\n' "$id"
      ;;
    *'"method":"session/send"'*)
      printf '{"id":%s,"result":{"sessionId":"ses_run","accepted":true,"stateRevision":7}}\n' "$id"
      printf '{"method":"session/event","params":{"type":"turn.completed","seq":6,"sessionId":"ses_run","payload":{"response":"done","resultType":"success","tokenCount":0,"toolCallCount":0,"duration":1}}}\n'
      ;;
    *)
      if [ -n "$id" ]; then
        printf '{"id":%s,"error":{"code":-32601,"message":"method not supported"}}\n' "$id"
      fi
      ;;
  esac
done
`
}

// TestZcodeEmptyThinkingLevelResolvesDefault is the regression for the
// saved-with-"Default" launch failure: an agent with a zcode model and an
// empty thinking_level died at session/create with -32603 "Reasoning level is
// required" — the empty level means "let the runtime decide", and zcode has
// no implicit default, so the decision must be resolved from the model's own
// catalog the same way its client does: defaultLevel first, else the first
// advertised level.
func TestZcodeEmptyThinkingLevelResolvesDefault(t *testing.T) {
	// Registry shape observed live: levels low/high, defaultLevel max.
	withDefault := `{"levels":[{"value":"low","label":"low"},{"value":"max","label":"max"}],"defaultLevel":"max"}`
	// Same levels without a defaultLevel: zcode's own client falls back to
	// the first available level.
	withoutDefault := `{"levels":[{"value":"low","label":"low"},{"value":"max","label":"max"}]}`

	for name, tc := range map[string]struct {
		reasoning string
		want      string
	}{
		"catalog default level":  {withDefault, "max"},
		"first advertised level": {withoutDefault, "low"},
	} {
		t.Run(name, func(t *testing.T) {
			createCapture := filepath.Join(t.TempDir(), "exec-create.txt")
			t.Setenv("ZCODE_EXEC_CREATE_CAPTURE", createCapture)
			bin := writeFakeZcodeScript(t, zcodeThinkingDefaultScript(tc.reasoning))

			session, err := newZcodeTestBackend(t, bin).Execute(context.Background(), "p", ExecOptions{
				Cwd:   t.TempDir(),
				Model: "zai-api/GLM-5.3",
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if _, res := collectZcodeSession(t, session); res.Status != "completed" {
				t.Fatalf("expected completed, got %q error=%q", res.Status, res.Error)
			}

			raw, err := os.ReadFile(createCapture)
			if err != nil {
				t.Fatalf("captured execution create missing: %v", err)
			}
			var frame struct {
				Params struct {
					Model zcodeSelectionJSON `json:"model"`
				} `json:"params"`
			}
			if err := json.Unmarshal(raw, &frame); err != nil {
				t.Fatalf("parse captured create: %v\n%s", err, raw)
			}
			if frame.Params.Model.Options.ReasoningLevel != tc.want {
				t.Fatalf("expected reasoningLevel=%q on the create, got %+v\n%s", tc.want, frame.Params.Model, raw)
			}
		})
	}
}
