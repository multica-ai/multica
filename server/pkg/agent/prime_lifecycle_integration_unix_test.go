//go:build agentintegration && unix

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// primeRunningAgentPids returns the pids of every live `prime-agent` process
// visible to this user. It matches on the exact process NAME (`pgrep -x`),
// which is what prime-agent sets via process.title, and deliberately not on
// the full command line (`pgrep -f`): the latter also matches any harness,
// shell or CI step whose own argv merely mentions "prime-agent", which made an
// earlier revision of this test skip itself against its own invocation.
// pgrep exits 1 when nothing matched, which is not an error here.
func primeRunningAgentPids(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("pgrep", "-x", "prime-agent").Output()
	if err != nil {
		// pgrep exits 1 when nothing matched. Any other failure (pgrep absent)
		// is reported so the caller can skip rather than assume "none".
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil
		}
		t.Skipf("cannot enumerate prime-agent processes with pgrep (%v); skipping a test whose process assertions depend on it", err)
	}
	var pids []string
	for _, f := range strings.Fields(string(out)) {
		pids = append(pids, f)
	}
	return pids
}

// primeDaemonLogSizes snapshots the current size of every prime-agent daemon
// supervisor log. Prime writes one log per daemon socket
// (getDaemonLogPath(socketPath) -> <agentDir>/logs/<socket-basename>.<hash>.log)
// and appends forever, so a test can only attribute lines to itself by reading
// the delta past these offsets.
func primeDaemonLogSizes(t *testing.T, agentDir string) map[string]int64 {
	t.Helper()
	sizes := map[string]int64{}
	matches, err := filepath.Glob(filepath.Join(agentDir, "logs", "daemon.sock.*.log"))
	if err != nil {
		return sizes
	}
	for _, path := range matches {
		if info, statErr := os.Stat(path); statErr == nil {
			sizes[path] = info.Size()
		}
	}
	return sizes
}

// primeDaemonLogDelta reads only the bytes appended to each supervisor log
// since the baseline snapshot, including logs that did not exist then (a fresh
// daemon socket). Historical lines from earlier runs are never returned.
func primeDaemonLogDelta(t *testing.T, agentDir string, baseline map[string]int64) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(agentDir, "logs", "daemon.sock.*.log"))
	if err != nil {
		return ""
	}
	sort.Strings(matches)
	var b strings.Builder
	for _, path := range matches {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		from := baseline[path]
		if from > int64(len(raw)) {
			from = 0 // rotated/truncated since the baseline
		}
		b.WriteString(string(raw[from:]))
	}
	return b.String()
}

// primeRegisteredHeartbeatJob scans prime-agent's per-session artifact
// directories for a scheduled-jobs.json holding an rlm_heartbeat job that
// belongs to THIS test, identified by uniqueMarker (a path segment unique to
// the test's temp dir). Returns the matching job's JSON and the file it came
// from. Prime writes this file when the heartbeat is actually registered
// (cron-jobs.ts's AgentCronJobStore.forSessionArtifacts +
// SESSION_SCHEDULED_JOBS_FILENAME), so finding it is positive, non-racy proof
// of registration — unlike waiting for the heartbeat to fire, which
// shouldDeferHeartbeatCronJob suppresses while the turn is busy.
func primeRegisteredHeartbeatJob(agentDir, uniqueMarker string) (job string, path string, scanned []string) {
	matches, err := filepath.Glob(filepath.Join(agentDir, "session-artifacts", "*", "scheduled-jobs.json"))
	if err != nil {
		return "", "", nil
	}
	for _, file := range matches {
		scanned = append(scanned, file)
		raw, readErr := os.ReadFile(file)
		if readErr != nil {
			continue
		}
		var parsed struct {
			Jobs []map[string]any `json:"jobs"`
		}
		if json.Unmarshal(raw, &parsed) != nil {
			continue
		}
		for _, j := range parsed.Jobs {
			if source, _ := j["source"].(string); source != "rlm_heartbeat" {
				continue
			}
			prompt, _ := j["prompt"].(string)
			cwd, _ := j["cwd"].(string)
			if !strings.Contains(prompt, uniqueMarker) && !strings.Contains(cwd, uniqueMarker) {
				continue
			}
			encoded, _ := json.Marshal(j)
			return string(encoded), file, scanned
		}
	}
	return "", "", scanned
}

// primeShutdownWorkerID extracts the session-worker id the supervisor reported
// receiving a shutdown command for, from a supervisor log delta. Prime logs
// worker lines as "Session worker <id> stderr: <message>".
func primeShutdownWorkerID(delta string) string {
	const prefix = "Session worker "
	const marker = " stderr: shutdown command received over socket"
	for _, line := range strings.Split(delta, "\n") {
		i := strings.Index(line, prefix)
		if i < 0 {
			continue
		}
		j := strings.Index(line, marker)
		if j < 0 || j <= i+len(prefix) {
			continue
		}
		return line[i+len(prefix) : j]
	}
	return ""
}

// primeHeartbeatTimestamps returns the non-empty lines currently in the
// heartbeat log, i.e. one entry per heartbeat turn that actually executed.
func primeHeartbeatTimestamps(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(raw), "\n") {
		if trimmed := strings.TrimSpace(l); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// TestPrimeRealACPHeartbeatDoesNotRunAfterCancel is the integration-level
// counterpart to the unit tests in prime_cancel_unix_test.go, and the only one
// that exercises Prime's DETACHED daemon worker — the process the session
// really runs in, which lives outside the process group Multica signals and so
// cannot be reached by SIGTERM/SIGKILL at all. The only supported way to stop
// it is Prime's own ACP shutdown hook, which the cancellation path reaches by
// letting the stdin EOF land before signalling:
//
//	closeStdin -> handle.closed -> connection.dispose() ->
//	complete_owned_session -> supervisor stopWorker -> cronScheduler.stop()
//
// The test registers a real 10s rlm_heartbeat, proves it was registered from
// Prime's own on-disk artifact, cancels the run through primeBackend.Execute's
// ordinary context cancellation, and then observes.
//
// SCOPE OF THE EVIDENCE — read before strengthening any claim here. This
// provides an integration-level observation that no heartbeat execution was
// observed after cancellation, given that the heartbeat had been positively
// registered beforehand. It is COMPLEMENTARY evidence: the test does not
// independently prove the absence of all detached background work, and it must
// not be described as proving that. Two specific limits:
//
//   - The post-cancellation half is a NEGATIVE assertion. There is currently no
//     positive control in this harness that forces a heartbeat to run after
//     cancellation, because producing that outcome requires losing the
//     dispose-versus-signal race, which cannot be arranged without artificially
//     reproducing the race. So an empty hb.log is consistent with the fix
//     working AND with the heartbeat never having been eligible to fire.
//   - shouldDeferHeartbeatCronJob (cron-jobs.ts) defers a heartbeat while the
//     session has work in flight. The prompt therefore keeps the turn busy only
//     briefly, so that a worker which SURVIVED cancellation would go idle well
//     inside the observation window and its scheduler would have had a real
//     opportunity to fire. That is what keeps the negative assertion meaningful
//     rather than vacuous; it is not a guarantee.
//
// The registration half, by contrast, is deterministic and attributable: it
// matches an rlm_heartbeat job carrying this test's unique temp-dir marker.
func TestPrimeRealACPHeartbeatDoesNotRunAfterCancel(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.LookPath("prime-agent")
	if err != nil {
		t.Skip("prime-agent not on PATH; skipping real-binary smoke test")
	}
	agentDir, err := primeAgentDirFor(os.Environ(), "")
	if err != nil || agentDir == "" {
		t.Skipf("cannot resolve prime-agent's agent dir: %v", err)
	}

	// Baseline guard: this test attributes supervisor-log lines and live
	// processes to itself, which is only sound when nothing else is running
	// prime-agent on this machine.
	// A concurrent Prime session would append its own worker lines to the same
	// supervisor log, and nothing in those lines identifies which Multica run
	// they belong to, so the worker-id correlation below would stop being
	// attributable. Note that Prime's supervisor is long-lived and survives a
	// run by design, so a previous invocation of this test leaves one behind:
	// stop stray prime-agent daemons (or wait out their idle eviction) before
	// re-running.
	if pids := primeRunningAgentPids(t); len(pids) > 0 {
		t.Skipf("prime-agent already running (pids %s); skipping so the supervisor-log evidence stays attributable to this test — stop stray prime-agent daemons and re-run", strings.Join(pids, ","))
	}
	logBaseline := primeDaemonLogSizes(t, agentDir)

	cwd := t.TempDir()
	hbLog := filepath.Join(cwd, "hb.log")
	// The temp dir's parent carries the test name plus a random suffix, so it
	// is unique to this run and survives the /private symlink prefix macOS
	// applies to the cwd Prime records.
	uniqueMarker := filepath.Base(filepath.Dir(cwd))

	backend, err := New("prime", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Step 2 keeps the turn busy only ~25s: long enough that cancellation lands
	// mid-turn, short enough that a surviving worker would be idle — and its
	// heartbeat therefore eligible — well inside the observation window below.
	prompt := fmt.Sprintf(
		"Use the ipython tool. Do exactly these two steps, then stop.\n"+
			"Step 1: await rlm_heartbeat.create('Run exactly this bash command and nothing else: date -u +%%FT%%TZ >> %s', interval='10s', label='hb')\n"+
			"Step 2: import time; time.sleep(25)\n", hbLog)

	session, err := backend.Execute(ctx, prompt, ExecOptions{Cwd: cwd, Timeout: 4 * time.Minute})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	// Phase B — registration must be proven from Prime's own artifact before
	// anything is cancelled. Never wait for the heartbeat to FIRE here.
	var job, jobFile string
	var scanned []string
	registrationDeadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(registrationDeadline) {
		if job, jobFile, scanned = primeRegisteredHeartbeatJob(agentDir, uniqueMarker); job != "" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if job == "" {
		cancel()
		<-session.Result
		t.Fatalf("no rlm_heartbeat job carrying this test's marker %q was ever registered.\nsearched: %s\nscanned files: %v",
			uniqueMarker, filepath.Join(agentDir, "session-artifacts", "*", "scheduled-jobs.json"), scanned)
	}
	t.Logf("EVIDENCE A — heartbeat registered in %s:\n%s", jobFile, job)

	beforeCancel := primeHeartbeatTimestamps(hbLog)
	t.Logf("EVIDENCE A — heartbeat executions before cancellation: %d %v", len(beforeCancel), beforeCancel)

	// Phase D — cancel through Execute's ordinary path. No knob is touched, so
	// this exercises the default graceful window and escalation.
	cancelledAt := time.Now()
	cancel()
	var status string
	select {
	case res := <-session.Result:
		status = res.Status
	case <-time.After(90 * time.Second):
		t.Fatal("Execute did not return after cancellation")
	}
	if status != "aborted" && status != "failed" {
		t.Fatalf("EVIDENCE B — unexpected cancellation status %q, want aborted or failed", status)
	}
	t.Logf("EVIDENCE B — Execute returned status=%q %s after cancel", status, time.Since(cancelledAt).Round(time.Millisecond))

	// Phase E — the supervisor must show that stopWorker actually ran, which is
	// what proves connection.dispose() -> complete_owned_session was reached.
	const shutdownMarker = "shutdown command received over socket"
	var delta string
	shutdownDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(shutdownDeadline) {
		delta = primeDaemonLogDelta(t, agentDir, logBaseline)
		if strings.Contains(delta, shutdownMarker) {
			break
		}
		time.Sleep(time.Second)
	}
	if !strings.Contains(delta, shutdownMarker) {
		t.Fatalf("EVIDENCE C — supervisor never logged %q after cancellation, so Prime's owned-session teardown was not observed.\nsupervisor log delta since test start:\n%s", shutdownMarker, delta)
	}
	t.Logf("EVIDENCE C — supervisor log delta shows owned-session teardown:\n%s", strings.TrimSpace(delta))

	// Phase F — the SESSION WORKER, which is the process the turn actually runs
	// in, must have exited. Prime reports that itself: the worker whose id was
	// told to shut down above must also log its own exit. This is asserted on
	// the worker id rather than on the process table because the supervisor is
	// a deliberately long-lived, shared daemon (idle-evicted after
	// DEFAULT_IDLE_EVICTION_MINUTES) that lives in its own process group and is
	// neither owned by nor reachable from a single Multica run — a surviving
	// supervisor is Prime's normal architecture, not a leak, and failing on it
	// would assert the wrong property.
	workerID := primeShutdownWorkerID(delta)
	if workerID == "" {
		t.Fatalf("EVIDENCE D — could not identify the session worker that was shut down; supervisor log delta:\n%s", delta)
	}
	workerExited := fmt.Sprintf("Session worker %s stderr: shutting down", workerID)
	exitDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(exitDeadline) {
		delta = primeDaemonLogDelta(t, agentDir, logBaseline)
		if strings.Contains(delta, workerExited) {
			break
		}
		time.Sleep(time.Second)
	}
	if !strings.Contains(delta, workerExited) {
		t.Fatalf("EVIDENCE D — session worker %s was told to shut down but never reported exiting; supervisor log delta:\n%s", workerID, delta)
	}
	t.Logf("EVIDENCE D — session worker %s reported its own exit", workerID)
	// Diagnostic only, never an assertion: records which prime-agent processes
	// outlived the run so a real leak stays visible in the log even though the
	// supervisor is expected among them.
	t.Logf("prime-agent processes still alive (supervisor expected): %v", primeRunningAgentPids(t))

	// Phase G — observational window. 60s past cancellation covers the ~25s
	// busy step plus several 10s heartbeat intervals.
	observeUntil := cancelledAt.Add(60 * time.Second)
	if d := time.Until(observeUntil); d > 0 {
		t.Logf("observing hb.log for %s past cancellation", d.Round(time.Second))
		time.Sleep(d)
	}
	afterCancel := primeHeartbeatTimestamps(hbLog)
	if len(afterCancel) > len(beforeCancel) {
		t.Fatalf("EVIDENCE E — heartbeat executed %d time(s) AFTER cancellation (before=%d after=%d): %v",
			len(afterCancel)-len(beforeCancel), len(beforeCancel), len(afterCancel), afterCancel[len(beforeCancel):])
	}
	t.Logf("EVIDENCE E — no heartbeat execution observed after cancellation (before=%d after=%d)", len(beforeCancel), len(afterCancel))
}
