//go:build unix

package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// primeCancelFakeScript returns a POSIX-sh script that impersonates a
// long-running `prime-agent --mode acp`: it spawns a background grandchild
// (standing in for Prime's IPython kernel or a bash tool subprocess),
// answers initialize/session/new normally, then hangs on session/prompt
// (never responding) so the test can cancel mid-turn — publishing its own
// (process-group-leader) pid and the grandchild's only once that turn is in
// flight, which is what orders the cancellation after the handshake rather
// than racing it. When ignoreTerm is
// true the whole group ignores SIGTERM, forcing the SIGKILL escalation path.
func primeCancelFakeScript(ignoreTerm bool) string {
	trap := "trap 'exit 0' TERM\n"
	if ignoreTerm {
		trap = "trap '' TERM\n"
	}
	return "#!/bin/sh\n" + trap +
		`# Background grandchild so the test can assert the *whole* group is
# terminated on cancellation, not just the direct child.
( sleep 300 ) &
child=$!
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_cancel"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      # The pid file doubles as the test's synchronisation point, so publish it
      # only now: written before the read loop it orders against nothing, and
      # the cancellation could land during the handshake instead of mid-turn.
      if [ -n "$PRIME_PID_FILE" ]; then
        printf '%s %s\n' "$$" "$child" > "$PRIME_PID_FILE"
      fi
      # Never respond — simulates a turn still in flight when cancelled.
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
}

// primeMixedSignalFakeScript returns a fake `prime-agent` whose leader
// RESPECTS SIGTERM (so it exits and cmd.Wait() returns the instant the group
// is signalled) while a background grandchild IGNORES SIGTERM and detaches
// its stdio, so it holds neither the leader alive nor prime-agent's stdout
// pipe. This is the mixed case that leaks when escalation keys off the
// leader's exit (procDone) instead of the whole process group.
func primeMixedSignalFakeScript() string {
	return "#!/bin/sh\n" + "trap 'exit 0' TERM\n" +
		`# Grandchild ignores TERM and redirects its stdio away from the pipe so
# it does not keep prime-agent's stdout open after the leader exits.
( trap '' TERM; sleep 300 ) </dev/null >/dev/null 2>&1 &
child=$!
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_cancel"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      # Published mid-turn on purpose; see primeCancelFakeScript.
      if [ -n "$PRIME_PID_FILE" ]; then
        printf '%s %s\n' "$$" "$child" > "$PRIME_PID_FILE"
      fi
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
}

// primeGracefulExitFakeScript returns a fake `prime-agent` that models the
// path Prime's real ACP mode takes on stdin EOF: it exits 0 on its own,
// leaving NO descendant behind, and records that it got there gracefully.
// It also records any SIGTERM it receives, so a test can assert the daemon
// never had to reach for a signal at all. Nothing here holds prime-agent's
// stdout open after the leader exits, so the whole process group really is
// empty by the time the cancellation handler re-checks it.
func primeGracefulExitFakeScript() string {
	return "#!/bin/sh\n" +
		`trap 'printf "term\n" >> "$PRIME_SIGNAL_FILE"' TERM
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_cancel"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      # Never respond, and announce that the turn is now in flight so the
      # test cancels mid-turn rather than during the handshake.
      printf 'ready\n' >> "$PRIME_SIGNAL_FILE"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
# stdin EOF: this is the graceful shutdown path Prime uses to run
# connection.dispose() -> complete_owned_session before exiting.
printf 'graceful\n' >> "$PRIME_SIGNAL_FILE"
exit 0
`
}

// primeHangingHandshakeFakeScript returns a fake `prime-agent` that receives
// the initialize request, announces it, and then never answers — pinning the
// run inside the ACP handshake so a test can cancel or time out there
// specifically, rather than racing whichever RPC happens to be in flight.
func primeHangingHandshakeFakeScript() string {
	return "#!/bin/sh\n" +
		`while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      # Announce, then never respond: the run stays in the handshake.
      printf 'initialize-received\n' >> "$PRIME_SIGNAL_FILE"
      ;;
  esac
done
`
}

// runPrimeHandshakeInterruptTest drives a fake prime-agent that hangs during
// initialize, waits until the handshake is provably in flight, then either
// cancels the context or lets the supplied timeout fire, and asserts the
// reported status.
//
// Unlike runPrimeCancellationTest this synchronises on a marker the fake writes
// from inside the initialize branch, so the interruption cannot land before the
// handshake has started.
func runPrimeHandshakeInterruptTest(t *testing.T, timeout time.Duration, want string) {
	t.Helper()

	primeGracefulExitGraceNanos.Store(int64(300 * time.Millisecond))
	primeTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() {
		primeGracefulExitGraceNanos.Store(0)
		primeTerminateGraceNanos.Store(0)
	})

	tempDir := t.TempDir()
	signalFile := filepath.Join(tempDir, "signals")
	fakePath := filepath.Join(tempDir, "prime-agent")
	writeTestExecutable(t, fakePath, []byte(primeHangingHandshakeFakeScript()))

	backend, err := New("prime", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"PRIME_SIGNAL_FILE": signalFile},
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	opts := ExecOptions{Cwd: tempDir}
	if timeout > 0 {
		opts.Timeout = timeout
	} else {
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
	}

	session, err := backend.Execute(ctx, "prompt-ignored", opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	// The handshake is provably in flight before the interruption lands.
	waitForFileContaining(t, signalFile, "initialize-received")
	if cancel != nil {
		cancel()
	}

	select {
	case res := <-session.Result:
		if res.Status != want {
			t.Errorf("status = %q, want %q — a run interrupted during the ACP handshake "+
				"must not be reported as a provider failure (error: %q)", res.Status, want, res.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after the handshake was interrupted")
	}
}

// TestPrimeCancelDuringHandshakeReportsAborted: cancelling while initialize is
// still in flight is a user abort, not a provider fault. The initialize error
// path used to hardcode "failed", so a task the user cancelled during startup
// surfaced as a Prime defect.
func TestPrimeCancelDuringHandshakeReportsAborted(t *testing.T) {
	runPrimeHandshakeInterruptTest(t, 0, "aborted")
}

// TestPrimeTimeoutDuringHandshakeReportsTimeout is the deadline half of the
// same gap: a handshake that never completes within the run's timeout is a
// timeout, and is retried/reported differently from a failure.
func TestPrimeTimeoutDuringHandshakeReportsTimeout(t *testing.T) {
	runPrimeHandshakeInterruptTest(t, 700*time.Millisecond, "timeout")
}

// waitForFileContaining polls path until it contains want, failing the test if
// that never happens before the deadline.
func waitForFileContaining(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(path); err == nil && strings.Contains(string(raw), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s never contained %q", path, want)
}

// readFileString returns path's contents, failing the test if it cannot be read.
func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// primeLeaderExitsOnEOFFakeScript returns a fake `prime-agent` whose leader
// exits on stdin EOF WITHOUT needing a signal, while a SIGTERM-ignoring,
// stdio-detached grandchild keeps running. This is the regression case for
// the graceful-exit window: the leader exiting closes procDone early, so an
// implementation that treats procDone as proof the group is gone skips
// escalation entirely and orphans the grandchild.
func primeLeaderExitsOnEOFFakeScript() string {
	return "#!/bin/sh\n" +
		`( trap '' TERM; sleep 300 ) </dev/null >/dev/null 2>&1 &
child=$!
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_cancel"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      # Published mid-turn on purpose; see primeCancelFakeScript.
      if [ -n "$PRIME_PID_FILE" ]; then
        printf '%s %s\n' "$$" "$child" > "$PRIME_PID_FILE"
      fi
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
exit 0
`
}

// The scenarios below assert an exact status. They previously also accepted
// "failed", on the theory that hermesClient.request's select races the RPC's
// own error (closeAllPending, "prime-agent process exited") against runCtx's
// Canceled/DeadlineExceeded, so either could win.
//
// That race is real for the error hermesClient.request RETURNS, but it cannot
// change the status these scenarios observe: Execute classifies session/new
// and session/prompt failures on runCtx.Err(), not on the returned error, and
// context.CancelFunc sets Err() before it unblocks Done(). Once the test has
// cancelled, runCtx.Err() is deterministic no matter which error won.
//
// The one path that really did produce "failed" was initialize, which set the
// status unconditionally; that is fixed, and covered by
// runPrimeHandshakeInterruptTest. Accepting "failed" everywhere had been
// masking it, so these now pin the exact status — Execute not hanging and the
// process group being reaped are still asserted, but they are no longer the
// only thing asserted.

// TestPrimeCancellationTerminatesProcessGroupGraceful verifies that
// cancelling a run terminates a SIGTERM-respecting prime-agent and its whole
// process group, returns without hanging, and leaves no orphaned descendant.
func TestPrimeCancellationTerminatesProcessGroupGraceful(t *testing.T) {
	primeGracefulExitGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() { primeGracefulExitGraceNanos.Store(0) })
	runPrimeCancellationTest(t, primeCancelFakeScript(false), nil, "aborted")
}

// TestPrimeCancellationSkipsSignalWhenPrimeExitsOnEOF pins the graceful-exit
// window itself: when prime-agent exits on its own from the stdin EOF and
// leaves nothing behind, cancellation must NOT signal it at all. That window
// is what lets Prime's ACP shutdown hook (handle.closed ->
// connection.dispose() -> complete_owned_session) run to completion, which is
// the only supported way to stop the detached daemon worker the session
// actually runs in. Signalling here would cut that hook short.
func TestPrimeCancellationSkipsSignalWhenPrimeExitsOnEOF(t *testing.T) {
	tempDir := t.TempDir()
	signalFile := filepath.Join(tempDir, "signals")
	fakePath := filepath.Join(tempDir, "prime-agent")
	writeTestExecutable(t, fakePath, []byte(primeGracefulExitFakeScript()))

	backend, err := New("prime", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"PRIME_SIGNAL_FILE": signalFile},
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Cwd: tempDir})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	// Wait until the fake has received session/prompt, so the run is genuinely
	// mid-turn (that prompt is never answered) when the cancel lands.
	waitForFileContaining(t, signalFile, "ready")
	cancel()

	select {
	case <-session.Result:
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after cancellation")
	}

	got := readFileString(t, signalFile)
	if !strings.Contains(got, "graceful") {
		t.Fatalf("prime-agent did not take the graceful stdin-EOF exit path; signal file = %q", got)
	}
	if strings.Contains(got, "term") {
		t.Fatalf("cancellation signalled a prime-agent that was already exiting on its own — "+
			"this cuts Prime's ACP shutdown hook short and strands its detached worker; signal file = %q", got)
	}
}

// TestPrimeCancellationStillKillsGroupWhenLeaderExitsOnEOF is the companion
// regression to the test above, and the one that fails if the graceful-exit
// window is allowed to short-circuit the teardown: the leader exits on stdin
// EOF (closing procDone early) while a SIGTERM-ignoring, stdio-detached
// grandchild keeps running. procDone proves only that the LEADER was reaped,
// so escalation must stay gated on the whole process group or the grandchild
// is orphaned.
func TestPrimeCancellationStillKillsGroupWhenLeaderExitsOnEOF(t *testing.T) {
	primeGracefulExitGraceNanos.Store(int64(300 * time.Millisecond))
	primeTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() {
		primeGracefulExitGraceNanos.Store(0)
		primeTerminateGraceNanos.Store(0)
	})
	runPrimeCancellationTest(t, primeLeaderExitsOnEOFFakeScript(), nil, "aborted")
}

// TestPrimeCancellationEscalatesToSIGKILL verifies the worst case: prime-agent
// (and the descendants it spawned, e.g. its IPython kernel) ignore SIGTERM
// and keep running. Cancellation must escalate to a group SIGKILL, still
// return promptly, and still reap the whole group.
func TestPrimeCancellationEscalatesToSIGKILL(t *testing.T) {
	primeGracefulExitGraceNanos.Store(int64(300 * time.Millisecond))
	primeTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() {
		primeGracefulExitGraceNanos.Store(0)
		primeTerminateGraceNanos.Store(0)
	})
	runPrimeCancellationTest(t, primeCancelFakeScript(true), nil, "aborted")
}

// TestPrimeCancellationEscalatesWhenDescendantIgnoresTERM is the mixed-signal
// regression: a SIGTERM-respecting leader plus a SIGTERM-ignoring,
// stdio-detached descendant. Cancellation must still reap the descendant,
// which only holds when the SIGKILL escalation is gated on the whole process
// group (not the leader's exit).
func TestPrimeCancellationEscalatesWhenDescendantIgnoresTERM(t *testing.T) {
	primeGracefulExitGraceNanos.Store(int64(300 * time.Millisecond))
	primeTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() {
		primeGracefulExitGraceNanos.Store(0)
		primeTerminateGraceNanos.Store(0)
	})
	runPrimeCancellationTest(t, primeMixedSignalFakeScript(), nil, "aborted")
}

// TestPrimeTimeoutTerminatesProcessGroupWithDescendant proves the timeout
// path — not just manual cancellation — also reaps a live descendant.
// runContext() unifies both under the same runCtx.Done(), but the maintainer
// explicitly asked for timeout to be covered as its own scenario, matching
// the precedent in codex_cleanup_unix_test.go.
func TestPrimeTimeoutTerminatesProcessGroupWithDescendant(t *testing.T) {
	primeGracefulExitGraceNanos.Store(int64(300 * time.Millisecond))
	primeTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() {
		primeGracefulExitGraceNanos.Store(0)
		primeTerminateGraceNanos.Store(0)
	})
	runPrimeCancellationTest(t, primeCancelFakeScript(false), &ExecOptions{Timeout: 500 * time.Millisecond}, "timeout")
}

// runPrimeCancellationTest drives a fake prime-agent through initialize +
// session/new, waits for it to record its process-group pids, then either
// cancels the context (optsOverride == nil) or lets the supplied
// ExecOptions.Timeout fire on its own. Either way it asserts the run reports
// one of wantStatuses without hanging and that both the leader and the
// grandchild are gone afterward.
func runPrimeCancellationTest(t *testing.T, script string, optsOverride *ExecOptions, wantStatuses ...string) {
	t.Helper()

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "prime-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("prime", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"PRIME_PID_FILE": pidFile},
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	opts := ExecOptions{Cwd: tempDir}
	if optsOverride != nil {
		opts.Timeout = optsOverride.Timeout
	} else {
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
	}

	session, err := backend.Execute(ctx, "prompt-ignored", opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Drain streamed messages so the reader never blocks on a full channel.
	go func() {
		for range session.Messages {
		}
	}()

	pids := waitForPids(t, pidFile)

	if cancel != nil {
		cancel() // user cancels the task
	}
	// When optsOverride is set, the context's own timeout does the cancelling.

	select {
	case res := <-session.Result:
		ok := false
		for _, want := range wantStatuses {
			if res.Status == want {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("status = %q, want one of %v", res.Status, wantStatuses)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after cancellation (possible scanner deadlock or unkilled process)")
	}

	// The leader and the grandchild must both be gone — cancellation reaped
	// the whole group, leaving no orphan spinning.
	for _, pid := range pids {
		waitProcessGone(t, pid)
	}
}
