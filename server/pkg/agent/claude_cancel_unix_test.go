//go:build unix

package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// claudeCancelFakeScript returns a POSIX-sh script that impersonates a
// long-running `claude` in stream-json mode: it spawns a background grandchild
// (standing in for an MCP server or tool subprocess), records both its own
// (process-group-leader) pid and the grandchild pid, emits a stream-json system
// init line, then streams stdout in a tight loop forever. This is the shape
// that orphaned a descendant for 64+ minutes in #5918 when the daemon only
// killed the leader. When ignoreTerm is true the whole group ignores SIGTERM,
// forcing the SIGKILL escalation path.
func claudeCancelFakeScript(ignoreTerm bool) string {
	trap := "trap 'exit 0' TERM\n"
	if ignoreTerm {
		trap = "trap '' TERM\n"
	}
	return "#!/bin/sh\n" + trap +
		`# Background grandchild so the test can assert the *whole* group is
# terminated on cancellation, not just the direct child.
( sleep 300 ) &
child=$!
if [ -n "$CLAUDE_PID_FILE" ]; then
  printf '%s %s\n' "$$" "$child" > "$CLAUDE_PID_FILE"
fi
printf '{"type":"system","session_id":"ses_fake"}\n'
while true; do
  printf '{"type":"system","session_id":"ses_fake"}\n'
  sleep 0.1
done
`
}

// claudeMixedSignalFakeScript returns a fake `claude` whose leader RESPECTS
// SIGTERM (so it exits and cmd.Wait() returns the instant the group is
// signalled) while a background grandchild IGNORES SIGTERM and detaches its
// stdio, so it holds neither the leader alive nor claude's stdout pipe. This is
// the mixed case that leaks when escalation keys off the leader's exit
// (procDone) instead of the whole process group: the leader is reaped, procDone
// closes, the SIGKILL is skipped, and the SIGTERM-resistant descendant survives
// — the exact #5918 orphan.
func claudeMixedSignalFakeScript() string {
	return "#!/bin/sh\n" + "trap 'exit 0' TERM\n" +
		`# Grandchild ignores TERM and redirects its stdio away from the pipe so
# it does not keep claude's stdout open after the leader exits.
( trap '' TERM; sleep 300 ) </dev/null >/dev/null 2>&1 &
child=$!
if [ -n "$CLAUDE_PID_FILE" ]; then
  printf '%s %s\n' "$$" "$child" > "$CLAUDE_PID_FILE"
fi
printf '{"type":"system","session_id":"ses_fake"}\n'
while true; do
  printf '{"type":"system","session_id":"ses_fake"}\n'
  sleep 0.1
done
`
}

// TestClaudeCancellationTerminatesProcessGroupGraceful verifies that cancelling
// a run terminates a SIGTERM-respecting claude and its whole process group,
// returns an "aborted" result without hanging, and leaves no orphaned
// descendant.
func TestClaudeCancellationTerminatesProcessGroupGraceful(t *testing.T) {
	runClaudeCancellationTest(t, claudeCancelFakeScript(false))
}

// TestClaudeCancellationEscalatesToSIGKILL verifies the worst case from #5918:
// claude (and the descendants it spawned) ignore SIGTERM and keep running.
// Cancellation must escalate to a group SIGKILL, still return promptly, and
// still reap the whole group — without deadlocking on the stdout scanner or
// closing the pipe under a live writer.
func TestClaudeCancellationEscalatesToSIGKILL(t *testing.T) {
	claudeTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() { claudeTerminateGraceNanos.Store(0) })
	runClaudeCancellationTest(t, claudeCancelFakeScript(true))
}

// TestClaudeCancellationEscalatesWhenDescendantIgnoresTERM is the mixed-signal
// regression for #5918: a SIGTERM-respecting leader plus a SIGTERM-ignoring,
// stdio-detached descendant. Cancellation must still reap the descendant, which
// only holds when the SIGKILL escalation is gated on the whole process group
// (not the leader's exit). This fails against the leader-keyed escalation.
func TestClaudeCancellationEscalatesWhenDescendantIgnoresTERM(t *testing.T) {
	claudeTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() { claudeTerminateGraceNanos.Store(0) })
	runClaudeCancellationTest(t, claudeMixedSignalFakeScript())
}

func runClaudeCancellationTest(t *testing.T, script string) {
	t.Helper()

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "claude")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"CLAUDE_PID_FILE": pidFile},
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Cwd: tempDir})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Drain streamed messages so the reader never blocks on a full channel.
	go func() {
		for range session.Messages {
		}
	}()

	pids := waitForPids(t, pidFile)

	cancel() // user cancels the task

	select {
	case res := <-session.Result:
		if res.Status != "aborted" {
			t.Errorf("status = %q, want aborted", res.Status)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after cancellation (possible scanner deadlock or unkilled process)")
	}

	// The leader and the grandchild must both be gone — cancellation reaped the
	// whole group, leaving no orphan spinning.
	for _, pid := range pids {
		waitProcessGone(t, pid)
	}
}

// claudeErrorResultFakeScript impersonates claude after a spent session limit
// while a background task is still running: it reports an error result, then
// stays alive (as the CLI does until its background tasks end) with a
// grandchild standing in for the never-ending background command.
func claudeErrorResultFakeScript() string {
	return "#!/bin/sh\n" + "trap 'exit 0' TERM\n" +
		`( sleep 300 ) &
child=$!
if [ -n "$CLAUDE_PID_FILE" ]; then
  printf '%s %s\n' "$$" "$child" > "$CLAUDE_PID_FILE"
fi
printf '{"type":"system","session_id":"ses_fake"}\n'
printf '{"type":"result","subtype":"success","is_error":true,"result":"You'"'"'ve hit your session limit · resets 2pm (UTC)","session_id":"ses_fake"}\n'
while true; do
  sleep 0.1
done
`
}

// TestClaudeErrorResultTerminatesLingeringProcess verifies that a claude which
// keeps running after an error result (it waits for a background task) is
// stopped after the grace period: the run fails with the result's error
// instead of hanging until the background task ends.
func TestClaudeErrorResultTerminatesLingeringProcess(t *testing.T) {
	claudeErrorResultGraceNanos.Store(int64(200 * time.Millisecond))
	t.Cleanup(func() { claudeErrorResultGraceNanos.Store(0) })

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "claude")
	writeTestExecutable(t, fakePath, []byte(claudeErrorResultFakeScript()))

	backend, err := New("claude", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"CLAUDE_PID_FILE": pidFile},
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{Cwd: tempDir})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	pids := waitForPids(t, pidFile)

	select {
	case res := <-session.Result:
		if res.Status != "failed" {
			t.Errorf("status = %q, want failed", res.Status)
		}
		if !strings.Contains(res.Error, "hit your session limit") {
			t.Errorf("error = %q, want the result's session limit text", res.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after an error result while claude kept running")
	}

	for _, pid := range pids {
		waitProcessGone(t, pid)
	}
}
