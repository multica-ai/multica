//go:build agentintegration && unix

package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestPrimeRealACPLeavesNoOrphanedProcessTree pins the claim primeBackend.Execute
// makes in its own comments: that the ACP path "never spawns Prime's machine-wide
// daemon supervisor".
//
// It does not. `shouldUseDaemonClient` (main.ts) is
//
//	appMode !== "daemon" && !startupBenchmark && !help && listModels === undefined
//
// which is true for `--mode acp`, so the ACP path takes the daemon-client branch
// and connects to (starting if absent) a supervisor spawned with `detached: true`.
// That supervisor reparents to init, its session workers are ITS children, and
// each worker owns an IPython kernel. None of them are in the process group
// startOwnedProcessTree created, so releaseProcessGroup cannot reach them: they
// survive a completed run indefinitely, still holding the environment Multica
// handed them.
//
// The existing fakes cannot catch this — a shell script does not double-fork —
// and TestPrimeRealACPHeartbeatDoesNotRunAfterCancel skips precisely when
// prime-agent is already running, which is the state a leak produces. Hence a
// dedicated test on the success path, where nothing is supposed to survive.
//
// Observed against prime-agent 0.8.1 AND 0.7.3 on darwin: from zero live
// prime-agent processes, one successful Execute leaves a supervisor (ppid 1),
// two workers and an ipykernel_launcher behind. This is not a 0.8.x regression;
// it predates the version this PR was validated against.
func TestPrimeRealACPLeavesNoOrphanedProcessTree(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.LookPath("prime-agent")
	if err != nil {
		t.Skip("prime-agent not on PATH; skipping real-binary smoke test")
	}

	// Only a clean baseline makes the post-run set attributable to this run.
	if pids := primeRunningAgentPids(t); len(pids) > 0 {
		t.Skipf("prime-agent already running (pids %s); stop stray prime-agent processes and re-run", strings.Join(pids, ","))
	}

	backend, err := New("prime", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session, err := backend.Execute(ctx, "Reply with exactly: pong", ExecOptions{Cwd: t.TempDir(), Timeout: 3 * time.Minute})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("expected a completed turn, got status=%q error=%q", res.Status, res.Error)
	}

	// Execute has returned its Result, so the whole teardown Multica controls --
	// stdin close, cmd.Wait, pipe drain, releaseProcessGroup -- has already run.
	// Give a cooperative child a grace period before calling it a leak.
	deadline := time.Now().Add(10 * time.Second)
	var leaked []string
	for time.Now().Before(deadline) {
		leaked = primeRunningAgentPids(t)
		if len(leaked) == 0 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Report the leaked pids and anything parented to them. Matching the ps
	// output on the string "prime-agent" would also pick up this test's own
	// shell, whose argv mentions the binary path.
	leakedSet := make(map[string]bool, len(leaked))
	for _, pid := range leaked {
		leakedSet[pid] = true
	}
	tree, _ := exec.Command("ps", "-eo", "pid,ppid,command").Output()
	var detail []string
	for _, line := range strings.Split(string(tree), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if leakedSet[fields[0]] || leakedSet[fields[1]] {
			detail = append(detail, strings.TrimSpace(line))
		}
	}
	t.Fatalf("a completed prime-agent run left %d process(es) alive after teardown (pids %s); Multica's process group does not own Prime's detached supervisor, its workers or their IPython kernels:\n  %s",
		len(leaked), strings.Join(leaked, ","), strings.Join(detail, "\n  "))
}
