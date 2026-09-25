//go:build unix

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// hermesEscapedHolderEnv carries the path the re-executed helper records its
// identity in. Empty means this process is the ordinary test run.
const hermesEscapedHolderEnv = "MULTICA_FAKE_HERMES_ESCAPED_HOLDER"

// hermesEscapedHolderBound is what the daemon's shutdown may cost: the drain
// grace, then a forced shutdown whose own cost is one cmd.Wait() on an
// already-exited process, plus slack for a loaded CI runner.
var hermesEscapedHolderBound = hermesReaderDrainGrace + 6*time.Second

// hermesEscapedHolderReady covers everything between Execute returning and the
// holder's identity being readable: the protocol advancing to the case that
// starts it, then a process spawn and a Go runtime start, since the holder is a
// re-executed test binary. On a loaded runner that is seconds rather than
// milliseconds. The fake agent and the test wait on the same budget on purpose:
// with the test's the shorter of the two, it can give up while the agent is
// still legitimately waiting, and the run fails with a missing state file
// instead of an assertion about the daemon.
var hermesEscapedHolderReady = hermesEscapedHolderBound

// hermesEscapedHolderLifetime outlives every wait these tests make — the ready
// budget and then the shutdown bound, twice over — so a passing run can only
// mean the daemon stopped waiting for the holder, never that the holder
// happened to exit first. The fixture kills it long before this elapses.
var hermesEscapedHolderLifetime = 2 * (hermesEscapedHolderReady + hermesEscapedHolderBound)

// TestHermesEscapedPipeHolderHelper is the fixture's pipe holder, not a test of
// its own: the tests below re-execute this binary so the child can call
// setsid(2) and leave the process group the daemon kills, the one thing a shell
// fixture cannot express portably. It inherits the fake agent's stdout and
// stderr — the daemon's read pipes — and keeps them open while it sleeps.
func TestHermesEscapedPipeHolderHelper(t *testing.T) {
	statePath := os.Getenv(hermesEscapedHolderEnv)
	if statePath == "" {
		t.Skip("pipe holder for the hermes escaped-descendant tests")
	}
	// Without its own session this process would die with the group and the
	// tests would pass for the wrong reason, so leave no state file behind:
	// they fail on its absence.
	if _, err := syscall.Setsid(); err != nil {
		t.Fatalf("setsid: %v", err)
	}
	if err := os.WriteFile(statePath, []byte(fmt.Sprintf("%d %d", os.Getpid(), syscall.Getpgrp())), 0o600); err != nil {
		t.Fatalf("record holder identity: %v", err)
	}
	time.Sleep(hermesEscapedHolderLifetime)
}

// TestHermesBackendReportsTurnWhenEscapedDescendantHoldsPipes covers the case
// owning the process tree cannot: the agent leaves a descendant that inherited
// stdout/stderr and then left the process group, so the group-wide kill in
// cmd.Cancel never reaches it and the daemon's readers never see EOF. Joining
// them unconditionally wedged the turn until the user cancelled it by hand,
// which is the MUL-5241 report. This fixture is the POSIX form of that — a
// setsid descendant; on Windows the equivalent is a child running unowned
// because startOwnedProcessTree failed open, since the Job Object this daemon
// creates does not permit breakaway.
//
// The turn must reach a terminal Result, and the session's channels must close,
// within the test's shutdown bound — itself well inside the 10s cmd.WaitDelay
// that makes the forced shutdown terminate at all. The escaped process itself
// is not reaped — that is what escaping means — which is why the fixture kills
// the holder itself.
//
// The sibling TestHermesBackendCancelsBeforeWaitingForLingeringProcess does not
// reach this: its lingering process closes the inherited descriptors first, so
// the readers get their EOF.
func TestHermesBackendReportsTurnWhenEscapedDescendantHoldsPipes(t *testing.T) {
	// The turn completes normally; only the shutdown has to cope with the
	// holder.
	session, messagesClosed := startHermesWithEscapedHolder(t, `
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"ses_escaped_holder"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      %s
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;`)

	result := awaitHermesResult(t, session)
	if result.Status != "completed" {
		t.Fatalf("status: got %q (error %q), want %q", result.Status, result.Error, "completed")
	}
	awaitHermesMessagesClosed(t, messagesClosed)
}

// TestHermesBackendClosesSessionWhenEarlyFailureLeavesEscapedHolder covers the
// other side of that shutdown: a handshake failure returns from the lifecycle
// goroutine before the drain runs at all, so the deferred cleanup is the only
// thing that closes the pipes and joins the readers.
//
// This is a contract test, not a regression: it passes on the unfixed backend
// too, because Messages gets closed either way. What it pins is that the early
// path still reaches a terminal Result and closes the session within the same
// bound. The join it sits next to guards something a test cannot observe
// reliably — a reader still live when the goroutine closes msgCh panics in
// trySend — so the assertion here is the closure, not the join.
func TestHermesBackendClosesSessionWhenEarlyFailureLeavesEscapedHolder(t *testing.T) {
	session, messagesClosed := startHermesWithEscapedHolder(t, `
    *'"method":"initialize"'*)
      %s
      printf '{"jsonrpc":"2.0","id":%%s,"error":{"code":-32603,"message":"initialize refused"}}\n' "$id"
      exit 0
      ;;`)

	result := awaitHermesResult(t, session)
	if result.Status != "failed" {
		t.Fatalf("status: got %q (error %q), want %q", result.Status, result.Error, "failed")
	}
	awaitHermesMessagesClosed(t, messagesClosed)
}

// startHermesWithEscapedHolder runs the hermes backend against a fake agent
// built from cases — the bodies of a `case "$line" in` over incoming JSON-RPC,
// with one %s where the holder is started. It returns the session and a channel
// that closes when Messages does, having already confirmed the holder really
// left the agent's process group.
func startHermesWithEscapedHolder(t *testing.T, cases string) (*Session, <-chan struct{}) {
	t.Helper()

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	dir := t.TempDir()
	statePath := filepath.Join(dir, "holder-state")
	fakePath := filepath.Join(dir, "hermes")
	// The holder is started without redirections on purpose: inheriting fd 1
	// and 2 is what keeps the daemon's pipes open after their writer is gone.
	//
	// The agent waits for it to record its identity — which it does once it has
	// left the process group — before answering at all. Without that wait the
	// daemon can kill the group first, as it does the moment a handshake fails,
	// and the holder dies as an ordinary member before it ever escapes. The wait
	// is bounded by hermesEscapedHolderReady, the same budget the test gives it,
	// so a broken holder fails the test rather than hanging it.
	spawnHolder := fmt.Sprintf(`%s=%q %q -test.run='^TestHermesEscapedPipeHolderHelper$' &
      waited=0
      while [ ! -f %[2]q ] && [ $waited -lt %[4]d ]; do sleep 0.05; waited=$((waited+1)); done`,
		hermesEscapedHolderEnv, statePath, testBinary, hermesEscapedHolderReady/(50*time.Millisecond))
	script := fmt.Sprintf(`#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in`+cases+`
  esac
done
`, spawnHolder)
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("hermes", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 25 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	messagesClosed := make(chan struct{})
	go func() {
		defer close(messagesClosed)
		for range session.Messages {
		}
	}()

	// Read the holder before waiting on anything it can outlast, so a failing
	// run kills it on the way out instead of leaving it to time out on its own.
	pid, pgid := readEscapedHolderIdentity(t, statePath)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	// A session leader's group id is its own pid. Anything else means the
	// holder stayed in the group cmd.Cancel kills, and the test would pass
	// without ever exercising the shutdown path it is here for.
	if pid != pgid {
		t.Fatalf("holder pid %d is in group %d; it never left the agent's process group", pid, pgid)
	}
	return session, messagesClosed
}

// awaitHermesResult returns the session's terminal Result, failing if the
// shutdown waited on the escaped holder instead of bounding itself.
func awaitHermesResult(t *testing.T, session *Session) Result {
	t.Helper()
	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		return result
	case <-time.After(hermesEscapedHolderBound):
		t.Fatalf("no result within %s: the escaped pipe holder wedged the shutdown", hermesEscapedHolderBound)
		return Result{}
	}
}

// awaitHermesMessagesClosed fails if the lifecycle goroutine never closed
// Messages. The Result alone does not prove it: the readers are joined after
// the Result is sent, and Messages closes only once they are.
func awaitHermesMessagesClosed(t *testing.T, messagesClosed <-chan struct{}) {
	t.Helper()
	select {
	case <-messagesClosed:
	case <-time.After(hermesEscapedHolderBound):
		t.Fatalf("Messages still open %s after the result; the readers were never joined", hermesEscapedHolderBound)
	}
}

// readEscapedHolderIdentity returns the holder's pid and process group, waiting
// hermesEscapedHolderReady for it to record them — the same budget the fake
// agent allows, since this wait starts as soon as Execute returns, before the
// handshake the agent is holding up. Missing at the end of it means the holder
// never started or setsid(2) failed, so it never held the pipes.
func readEscapedHolderIdentity(t *testing.T, path string) (pid, pgid int) {
	t.Helper()
	deadline := time.Now().Add(hermesEscapedHolderReady)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			if _, scanErr := fmt.Sscan(strings.TrimSpace(string(raw)), &pid, &pgid); scanErr != nil {
				t.Fatalf("holder state %q: %v", raw, scanErr)
			}
			return pid, pgid
		}
		if time.Now().After(deadline) {
			t.Fatalf("holder never recorded its identity at %s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
