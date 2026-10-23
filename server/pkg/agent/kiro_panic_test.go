package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// fakeKiroACPPanicScript reproduces MUL-8188: session/load succeeds, then the
// first session/prompt makes Kiro's agent loop panic. The panic is written to
// stderr and — the part that used to wedge tasks for hours — the process keeps
// running without ever answering the prompt, unless KIRO_PANIC_EXIT is set.
// KIRO_CHUNK_BEFORE_PANIC emits one agent message before panicking.
func fakeKiroACPPanicScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fresh"}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      if [ -n "$KIRO_CHUNK_BEFORE_PANIC" ]; then
        printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_broken","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"working on it"}}}}\n'
        sleep 0.3
      fi
      printf '\033[31mThe application panicked (crashed).\033[0m\n' >&2
      printf 'Message:  first agent loop request should never fail: AgentLoopResponse(Custom("invalid conversation history received"))\n' >&2
      printf 'Location: crates/agent/src/agent/mod.rs:3744\n' >&2
      if [ -n "$KIRO_PANIC_EXIT" ]; then
        exit 101
      fi
      ;;
  esac
done
`
}

func runKiroPanicScript(t *testing.T, env map[string]string, opts ExecOptions) (Result, time.Duration) {
	t.Helper()
	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPPanicScript()))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default(), Env: env})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}
	// A long task timeout: the run must end because of the panic, not
	// because a deadline happened to expire.
	opts.Timeout = 60 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	session, err := backend.Execute(ctx, "prompt", opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		return result, time.Since(start)
	case <-time.After(20 * time.Second):
		t.Fatal("kiro run did not finish after the runtime panicked (stuck waiting for session/prompt)")
		return Result{}, 0
	}
}

// TestKiroResumedPanicFailsFastAndRejectsResume is the MUL-8188 regression:
// a resumed session whose history Kiro cannot replay panics on the first
// prompt while the process stays alive. The run must fail within seconds,
// surface the panic, and signal ResumeRejected so the daemon retries once on a
// fresh session and retires the broken one.
func TestKiroResumedPanicFailsFastAndRejectsResume(t *testing.T) {
	t.Parallel()

	result, elapsed := runKiroPanicScript(t, nil, ExecOptions{ResumeSessionID: "ses_broken"})

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed (error=%q)", result.Status, result.Error)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("run took %s; a live panic must end it promptly", elapsed)
	}
	if !result.ResumeRejected {
		t.Fatalf("ResumeRejected = false, want true for a resumed session that panicked before its first message (error=%q)", result.Error)
	}
	for _, want := range []string{"kiro runtime panicked", "The application panicked", "invalid conversation history received", "Location: crates/agent/src/agent/mod.rs:3744"} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error %q does not contain %q", result.Error, want)
		}
	}
	if strings.Contains(result.Error, "\x1b[") {
		t.Errorf("error still carries ANSI escapes: %q", result.Error)
	}
	// The resume guards (daemon fresh-session retry, poisoned classifier,
	// GetLastTaskSession text guard) all key off this predicate.
	if !taskfailure.UnresumableHistory(result.Error) {
		t.Errorf("UnresumableHistory(%q) = false, want true", result.Error)
	}
}

// TestKiroPanicThenExitIsStillClassifiedAsPanic covers the other shape: the
// panic takes the process down, so stdout closes ("kiro process exited")
// possibly before stderr is read. The panic must still be the reported cause.
func TestKiroPanicThenExitIsStillClassifiedAsPanic(t *testing.T) {
	t.Parallel()

	result, _ := runKiroPanicScript(t, map[string]string{"KIRO_PANIC_EXIT": "1"}, ExecOptions{ResumeSessionID: "ses_broken"})

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed (error=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "invalid conversation history received") {
		t.Fatalf("error %q does not surface the panic message", result.Error)
	}
	if !result.ResumeRejected {
		t.Fatalf("ResumeRejected = false, want true (error=%q)", result.Error)
	}
}

// TestKiroFreshSessionPanicDoesNotRejectResume: a cold session cannot be
// "resume rejected". It must still fail fast instead of hanging.
func TestKiroFreshSessionPanicDoesNotRejectResume(t *testing.T) {
	t.Parallel()

	result, elapsed := runKiroPanicScript(t, nil, ExecOptions{})

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed (error=%q)", result.Status, result.Error)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("run took %s; a live panic must end it promptly", elapsed)
	}
	if result.ResumeRejected {
		t.Fatal("ResumeRejected = true on a fresh session, want false")
	}
	if !strings.Contains(result.Error, "kiro runtime panicked") {
		t.Fatalf("error %q does not surface the panic", result.Error)
	}
}

// TestKiroResumedPanicAfterProgressKeepsSession: a resumed turn that produced
// output before panicking proved the session loads and runs, so the panic is
// not evidence against it and ResumeRejected stays false.
func TestKiroResumedPanicAfterProgressKeepsSession(t *testing.T) {
	t.Parallel()

	result, _ := runKiroPanicScript(t, map[string]string{"KIRO_CHUNK_BEFORE_PANIC": "1"}, ExecOptions{ResumeSessionID: "ses_broken"})

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed (error=%q)", result.Status, result.Error)
	}
	if result.ResumeRejected {
		t.Fatalf("ResumeRejected = true after the turn made progress, want false (error=%q)", result.Error)
	}
}

func TestACPRuntimePanicDetector(t *testing.T) {
	t.Parallel()

	t.Run("color-eyre header split across writes", func(t *testing.T) {
		d := newACPRuntimePanicDetector()
		_, _ = d.Write([]byte("INFO starting\n\x1b[31mThe application pan"))
		if d.Fired() {
			t.Fatal("fired on a partial line")
		}
		_, _ = d.Write([]byte("icked (crashed).\x1b[0m\nMessage:  boom\nLocation: x.rs:1\n"))
		select {
		case <-d.Detected():
		default:
			t.Fatal("Detected() not closed after the header")
		}
		if got, want := d.Detail(), "The application panicked (crashed). | Message:  boom | Location: x.rs:1"; got != want {
			t.Fatalf("Detail() = %q, want %q", got, want)
		}
	})

	t.Run("rust default hook", func(t *testing.T) {
		d := newACPRuntimePanicDetector()
		_, _ = d.Write([]byte("thread 'tokio-runtime-worker' panicked at crates/agent/src/lib.rs:10:5:\nindex out of bounds\n"))
		if !d.Fired() {
			t.Fatal("did not fire on the Rust default panic header")
		}
		if !strings.Contains(d.Detail(), "index out of bounds") {
			t.Fatalf("Detail() = %q, want the panic message", d.Detail())
		}
	})

	t.Run("mid-line mention is not a panic", func(t *testing.T) {
		d := newACPRuntimePanicDetector()
		_, _ = d.Write([]byte("WARN tool output: the application panicked earlier, retrying\nDEBUG thread 'x' panicked at nothing\n"))
		if d.Fired() {
			t.Fatal("fired on a log line that only mentions a panic")
		}
	})

	t.Run("detail is bounded", func(t *testing.T) {
		d := newACPRuntimePanicDetector()
		_, _ = d.Write([]byte("The application panicked (crashed).\nMessage: " + strings.Repeat("x", 2000) + "\na\nb\nc\nd\n"))
		detail := d.Detail()
		if len(detail) > acpPanicDetailMaxLen+len("…") {
			t.Fatalf("Detail() length %d exceeds the cap", len(detail))
		}
		if strings.Contains(detail, "| d") {
			t.Fatalf("Detail() kept more than %d trailing lines: %q", acpPanicDetailLines, detail)
		}
	})
}
