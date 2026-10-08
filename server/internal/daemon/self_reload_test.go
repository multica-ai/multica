package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

// newSelfReloadTestDaemon returns a Daemon wired for trySelfReload: a stubbed
// self-executable path so restartTargetBinary resolves without touching the
// real process, plus a sentinel cancelFunc the test asserts on to detect that
// triggerRestart fired.
func newSelfReloadTestDaemon(t *testing.T, runningVersion string) (*Daemon, *atomic.Int32) {
	t.Helper()

	origResolve := resolveSelfExecutable
	origBrew := isBrewInstall
	t.Cleanup(func() {
		resolveSelfExecutable = origResolve
		isBrewInstall = origBrew
	})
	binary := filepath.Join(t.TempDir(), "multica")
	resolveSelfExecutable = func() (string, error) { return binary, nil }
	isBrewInstall = func() bool { return false }

	var restartCalls atomic.Int32
	d := &Daemon{
		cfg:        Config{CLIVersion: runningVersion, AutoReloadEnabled: true},
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		cancelFunc: func() { restartCalls.Add(1) },
	}
	return d, &restartCalls
}

// stubSelfVersion makes the `multica --version` probe report version (or fail).
func stubSelfVersion(t *testing.T, version string, err error) *atomic.Int32 {
	t.Helper()
	orig := detectSelfVersion
	t.Cleanup(func() { detectSelfVersion = orig })
	var calls atomic.Int32
	detectSelfVersion = func(context.Context, string) (string, error) {
		calls.Add(1)
		return version, err
	}
	return &calls
}

// TestTrySelfReload_RestartsWhenOnDiskVersionDiffers is the core of MUL-3269:
// an in-place upgrade leaves the executable path valid, so nothing in the
// existing self-heal notices it and the daemon serves the version it booted
// with indefinitely. Comparing the compile-time version against the binary on
// disk is what closes that.
func TestTrySelfReload_RestartsWhenOnDiskVersionDiffers(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
	stubSelfVersion(t, "0.3.8", nil)

	d.trySelfReload(context.Background())

	if restartCalls.Load() != 1 {
		t.Fatalf("triggerRestart fired %d times, want 1", restartCalls.Load())
	}
	if d.RestartBinary() == "" {
		t.Fatal("restart binary not recorded; the parent has nothing to re-exec")
	}
	if !claimsPaused(t, d) {
		t.Fatal("pauseClaims must stay held across the restart kick, like the auto-update path")
	}
}

// TestTrySelfReload_RestartsOnDowngrade covers a gap tryAutoUpdate structurally
// cannot: isNewerVersion says no, so the GitHub poller never fires, but the
// operator did deliberately install an older build and the daemon must follow it.
func TestTrySelfReload_RestartsOnDowngrade(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.8")
	stubSelfVersion(t, "0.3.7", nil)

	d.trySelfReload(context.Background())

	if restartCalls.Load() != 1 {
		t.Fatalf("triggerRestart fired %d times, want 1 (a downgrade is still a version change)", restartCalls.Load())
	}
}

// TestTrySelfReload_ProbeFailureIsNotAVersionChange is the review's third
// blocker. The binary is most likely to be unreadable during the few hundred
// milliseconds of the very upgrade this check exists to detect (vanished path,
// ETXTBSY), and "couldn't read the version" must never restart the daemon into
// a version nobody confirmed.
func TestTrySelfReload_ProbeFailureIsNotAVersionChange(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
	stubSelfVersion(t, "", errors.New("fork/exec: text file busy"))

	d.trySelfReload(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired on a failed probe")
	}
	if claimsPaused(t, d) {
		t.Fatal("a failed probe must not pause claiming")
	}
	if got := d.reloadPending(); got != "" {
		t.Fatalf("reload pending reason = %q, want empty after a failed probe", got)
	}
}

// TestTrySelfReload_BlankVersionIsNotAVersionChange covers a probe that exits 0
// without telling us anything, and the mirror case of a running version we don't
// know: restarting on a blank running version would produce a successor that
// reads blank too, i.e. a reload every tick forever.
func TestTrySelfReload_BlankVersionIsNotAVersionChange(t *testing.T) {
	cases := []struct{ name, running, onDisk string }{
		{"blank on disk", "0.3.7", ""},
		{"blank running", "", "0.3.8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, restartCalls := newSelfReloadTestDaemon(t, tc.running)
			stubSelfVersion(t, tc.onDisk, nil)

			d.trySelfReload(context.Background())

			if restartCalls.Load() != 0 {
				t.Fatal("triggerRestart fired on a version it could not compare")
			}
			if got := d.reloadPending(); got != "" {
				t.Fatalf("reload pending reason = %q, want empty", got)
			}
		})
	}
}

// TestTrySelfReload_NoopWhenVersionMatches also covers the operator putting the
// old binary back before the daemon went idle: the deferred-restart note must
// clear rather than linger on /health forever.
func TestTrySelfReload_NoopWhenVersionMatches(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
	stubSelfVersion(t, "0.3.7", nil)
	d.setReloadPending("stale note from an earlier round")

	d.trySelfReload(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired even though the on-disk version matches")
	}
	if got := d.reloadPending(); got != "" {
		t.Fatalf("reload pending reason = %q, want it cleared once the change went away", got)
	}
}

// TestTrySelfReload_DefersWhileBusy pins the "a running task is never
// interrupted by an upgrade" acceptance criterion, for both shapes of busy.
//
// Deferring is the whole mechanism — there is deliberately no pending-restart
// state machine waiting on a drain signal, because that is what parked the
// daemon in the first draft of this feature: the drain hook was anchored to
// exitClaim, which the batch poller (MUL-4257) now calls synchronously right
// after dispatch while activeTasks is still non-zero. Retrying from scratch on
// the next tick cannot get stuck that way.
func TestTrySelfReload_DefersWhileBusy(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*Daemon)
	}{
		{"task running", func(d *Daemon) { d.activeTasks.Store(1) }},
		{"claim in flight", func(d *Daemon) { d.claimMu.Lock(); d.claimsInFlight = 1; d.claimMu.Unlock() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
			probes := stubSelfVersion(t, "0.3.8", nil)
			tc.setup(d)

			d.trySelfReload(context.Background())

			if restartCalls.Load() != 0 {
				t.Fatalf("triggerRestart fired while the daemon was busy")
			}
			if claimsPaused(t, d) {
				t.Fatal("a deferred reload must leave claiming enabled, not park the poller")
			}
			if got := d.reloadPending(); got == "" {
				t.Fatal("a deferred reload must be visible on /health so the stale version is explainable")
			}

			// The next tick re-probes from scratch and restarts once idle —
			// no baseline to go stale, no signal to wait for.
			d.activeTasks.Store(0)
			d.claimMu.Lock()
			d.claimsInFlight = 0
			d.claimMu.Unlock()
			d.trySelfReload(context.Background())

			if restartCalls.Load() != 1 {
				t.Fatalf("triggerRestart fired %d times after the daemon went idle, want 1", restartCalls.Load())
			}
			if probes.Load() != 2 {
				t.Fatalf("probed %d times, want 2 (each tick re-probes)", probes.Load())
			}
		})
	}
}

// TestTrySelfReload_SkipsWhenAnUpdateIsInFlight keeps this check off the
// server-triggered handleUpdate's toes: that path runs on its own goroutine and
// ends in triggerRestart too.
func TestTrySelfReload_SkipsWhenAnUpdateIsInFlight(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
	probes := stubSelfVersion(t, "0.3.8", nil)
	d.updating.Store(true)

	d.trySelfReload(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatal("triggerRestart fired while another update was in progress")
	}
	if probes.Load() != 0 {
		t.Fatal("skipping must happen before the probe, not after paying for it")
	}
}

// TestTrySelfReload_SkipsWhenRestartAlreadyScheduled stops a second tick from
// re-entering during the shutdown window after triggerRestart cancelled the
// root context.
func TestTrySelfReload_SkipsWhenRestartAlreadyScheduled(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
	stubSelfVersion(t, "0.3.8", nil)

	d.trySelfReload(context.Background())
	d.trySelfReload(context.Background())

	if restartCalls.Load() != 1 {
		t.Fatalf("cancelFunc fired %d times, want 1", restartCalls.Load())
	}
}

// TestTrySelfReload_ReleasesBarrierWhenHandoffFails is the review's nit about
// the first draft looping: a failed restart must not leave the daemon paused
// forever, and because nothing is remembered across ticks the next round simply
// re-detects and retries.
func TestTrySelfReload_ReleasesBarrierWhenHandoffFails(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
	stubSelfVersion(t, "0.3.8", nil)

	// Resolve succeeds for the probe, then starts failing at the handoff.
	resolveSelfExecutable = func() (string, error) { return "", errors.New("cannot resolve executable") }

	d.trySelfReload(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatal("cancelFunc fired without a restart binary")
	}
	if claimsPaused(t, d) {
		t.Fatal("pauseClaims must be released when the restart handoff fails, or the daemon claims nothing ever again")
	}
}

// TestAutoUpdateLoop_WatchesTheBinaryWhenAutoUpdateIsOff is the review's first
// product decision: the on-disk check must not sit behind
// MULTICA_DAEMON_AUTO_UPDATE. Self-hosted daemons default auto-update off
// (MUL-2381), and "don't pull from GitHub" is not "don't follow the binary I
// replaced myself".
func TestAutoUpdateLoop_WatchesTheBinaryWhenAutoUpdateIsOff(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
	d.cfg.AutoUpdateEnabled = false
	stubSelfVersion(t, "0.3.8", nil)
	withStubRelease(t, &cli.GitHubRelease{TagName: "v9.9.9"}, errors.New("fetchLatestRelease must not be called"))
	d.runUpdateFn = func(string) (string, error) {
		t.Fatal("runUpdateFn called with auto-update disabled")
		return "", nil
	}
	shortenLoopTimers(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		d.autoUpdateLoop(ctx)
		close(done)
	}()

	waitFor(t, func() bool { return restartCalls.Load() == 1 },
		"self-reload never restarted with auto-update disabled")
	cancel()
	<-done
}

// TestAutoUpdateLoop_AlsoWatchesTheBinaryForDevBuilds covers the other half of
// the same gap: isReleaseVersion skips dev builds for the GitHub poller, but a
// developer who rebuilt and replaced the binary wants the daemon to run it.
func TestAutoUpdateLoop_AlsoWatchesTheBinaryForDevBuilds(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "v0.3.7-42-gabcdef0")
	d.cfg.AutoUpdateEnabled = true
	stubSelfVersion(t, "v0.3.7-43-gbcdef01", nil)
	withStubRelease(t, nil, errors.New("fetchLatestRelease must not be called for a dev build"))
	shortenLoopTimers(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		d.autoUpdateLoop(ctx)
		close(done)
	}()

	waitFor(t, func() bool { return restartCalls.Load() == 1 },
		"self-reload never restarted for a dev build")
	cancel()
	<-done
}

// TestAutoUpdateLoop_SkipsBothHalvesForDesktop asserts the Desktop exclusion is
// drawn once, above both checks: the Electron app ships and replaces the CLI
// binary itself, so following it would fight the app's own lifecycle.
func TestAutoUpdateLoop_SkipsBothHalvesForDesktop(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
	d.cfg.AutoUpdateEnabled = true
	d.cfg.LaunchedBy = "desktop"
	probes := stubSelfVersion(t, "0.3.8", nil)
	withStubRelease(t, nil, errors.New("fetchLatestRelease must not be called on Desktop"))
	shortenLoopTimers(t)

	// Returns immediately rather than looping, so no cancel is needed.
	d.autoUpdateLoop(context.Background())

	if probes.Load() != 0 {
		t.Fatalf("probed the binary %d times on a Desktop-managed daemon, want 0", probes.Load())
	}
	if restartCalls.Load() != 0 {
		t.Fatal("Desktop-managed daemon scheduled a restart")
	}
}

// TestAutoUpdateLoop_ExitsWhenBothHalvesDisabled keeps the goroutine from
// spinning a ticker nobody reads.
func TestAutoUpdateLoop_ExitsWhenBothHalvesDisabled(t *testing.T) {
	d, _ := newSelfReloadTestDaemon(t, "0.3.7")
	d.cfg.AutoUpdateEnabled = false
	d.cfg.AutoReloadEnabled = false
	probes := stubSelfVersion(t, "0.3.8", nil)
	shortenLoopTimers(t)

	d.autoUpdateLoop(context.Background())

	if probes.Load() != 0 {
		t.Fatalf("probed %d times with both halves disabled, want 0", probes.Load())
	}
}

func TestParseSelfVersion(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "release build template",
			raw:  "multica 0.3.7 (commit: abc1234, built: 2026-07-29T10:00:00Z)\ngo: go1.26.1, os/arch: darwin/arm64\n",
			want: "0.3.7",
		},
		{
			name: "dev build from git describe",
			raw:  "multica v0.3.7-42-gabcdef0-dirty (commit: abcdef0, built: unknown)\n",
			want: "v0.3.7-42-gabcdef0-dirty",
		},
		{
			// Unrecognized shape is reported as-is rather than swallowed: it
			// compares unequal, which surfaces as a reload instead of silence.
			name: "unexpected shape falls back to the first line",
			raw:  "0.3.7\n",
			want: "0.3.7",
		},
		{
			name: "empty output",
			raw:  "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseSelfVersion(tc.raw); got != tc.want {
				t.Fatalf("ParseSelfVersion(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// shortenLoopTimers collapses the loop's startup delay and check interval so a
// loop-level test finishes in milliseconds.
func shortenLoopTimers(t *testing.T) {
	t.Helper()
	origDelay, origInterval := autoUpdateInitialDelay, selfReloadCheckInterval
	t.Cleanup(func() {
		autoUpdateInitialDelay = origDelay
		selfReloadCheckInterval = origInterval
	})
	autoUpdateInitialDelay = time.Millisecond
	selfReloadCheckInterval = 5 * time.Millisecond
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(msg)
}

// claimsPaused reads pauseClaims under claimMu, honoring the field's own
// concurrency contract rather than relying on the test being single-goroutine.
func claimsPaused(t *testing.T, d *Daemon) bool {
	t.Helper()
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	return d.pauseClaims
}

// TestTryAutoUpdate_ResumesClaimingWhenRestartCannotBeScheduled is the
// permanent-stall case. tryAutoUpdate marks both cleanup switches as released
// just before the handoff, on the assumption that the process is about to exit.
// If triggerRestart cannot resolve a target, no exit is coming — and holding the
// updating flag and the claim barrier for a restart that never happens leaves
// the daemon alive but claiming nothing, forever. Every poller's tryEnterClaim
// returns false and nothing ever clears it.
//
// Note this outlived the void-returning triggerRestart it came from: the failure
// was unobservable before. trySelfReload handles the same failure, so the two
// halves of one loop must not disagree about it.
func TestTryAutoUpdate_ResumesClaimingWhenRestartCannotBeScheduled(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)
	d.runUpdateFn = func(string) (string, error) { return "upgraded", nil }

	origResolve := resolveSelfExecutable
	t.Cleanup(func() { resolveSelfExecutable = origResolve })
	resolveSelfExecutable = func() (string, error) { return "", errors.New("cannot resolve executable") }

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatal("cancelFunc fired without a restart binary")
	}
	if claimsPaused(t, d) {
		t.Error("pauseClaims held for a restart that will never happen — the daemon would claim nothing forever")
	}
	if d.updating.Load() {
		t.Error("updating flag held for a restart that will never happen — no later update or reload could run")
	}
}

// TestTrySelfReload_YieldsToAServerTriggeredUpdate closes the ownership race,
// interleaved the way it actually happens rather than pre-set.
//
// The bug isn't "updating was already true" — a plain Load() catches that. It is
// the window *after* the look: handleUpdate wins the CAS while this path is
// still probing, starts replacing the binary, and this path carries on to
// triggerRestart anyway — re-execing a half-written file and cutting the
// update's status reporting off partway. The claim barrier arbitrates nothing
// here, because handleUpdate reaches it through tryBeginServerUpdate and the two
// never inspect each other's state. Acquiring ownership up front is what makes
// the window unreachable.
func TestTrySelfReload_YieldsToAServerTriggeredUpdate(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")

	// The server-triggered path tries to take ownership mid-probe, exactly as
	// handleUpdate would via tryBeginServerUpdate's CAS.
	var serverUpdateWon atomic.Bool
	orig := detectSelfVersion
	t.Cleanup(func() { detectSelfVersion = orig })
	detectSelfVersion = func(context.Context, string) (string, error) {
		if d.updating.CompareAndSwap(false, true) {
			serverUpdateWon.Store(true)
		}
		return "0.3.8", nil
	}

	d.trySelfReload(context.Background())

	if serverUpdateWon.Load() {
		t.Fatalf("a server-triggered update acquired ownership mid-reload (restarts fired: %d) — "+
			"this path only sampled the flag instead of taking it, so both could act on the binary at once",
			restartCalls.Load())
	}
}

// TestTrySelfReload_SkipsWhenOwnershipIsAlreadyHeld is the simpler half: an
// update already in flight when the tick fires.
func TestTrySelfReload_SkipsWhenOwnershipIsAlreadyHeld(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
	probes := stubSelfVersion(t, "0.3.8", nil)
	if !d.updating.CompareAndSwap(false, true) {
		t.Fatal("precondition: updating should have been free")
	}

	d.trySelfReload(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatal("restarted while a server-triggered update was replacing the binary")
	}
	if claimsPaused(t, d) {
		t.Error("took the claim barrier out from under the in-flight update")
	}
	if probes.Load() != 0 {
		t.Error("ownership must be settled before paying for a probe")
	}
	if !d.updating.Load() {
		t.Error("released the other path's update ownership")
	}
}

// TestTrySelfReload_ReleasesUpdateOwnershipOnEveryNonRestartPath: holding the
// flag after deciding not to restart would block every later update — the
// server-triggered one included — with nothing to clear it.
func TestTrySelfReload_ReleasesUpdateOwnershipOnEveryNonRestartPath(t *testing.T) {
	cases := []struct {
		name    string
		onDisk  string
		probeer error
		busy    bool
	}{
		{name: "probe failed", probeer: errors.New("text file busy")},
		{name: "no change", onDisk: "0.3.7"},
		{name: "blank on disk", onDisk: ""},
		{name: "deferred while busy", onDisk: "0.3.8", busy: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := newSelfReloadTestDaemon(t, "0.3.7")
			stubSelfVersion(t, tc.onDisk, tc.probeer)
			if tc.busy {
				d.activeTasks.Store(1)
			}

			d.trySelfReload(context.Background())

			if d.updating.Load() {
				t.Fatal("update ownership held after declining to restart; nothing would ever clear it")
			}
		})
	}
}

// TestTrySelfReload_HoldsUpdateOwnershipAcrossRestart is the one path that must
// keep it: the process is going down, and clearing it would let a second
// attempt start mid-shutdown.
func TestTrySelfReload_HoldsUpdateOwnershipAcrossRestart(t *testing.T) {
	d, restartCalls := newSelfReloadTestDaemon(t, "0.3.7")
	stubSelfVersion(t, "0.3.8", nil)

	d.trySelfReload(context.Background())

	if restartCalls.Load() != 1 {
		t.Fatalf("triggerRestart fired %d times, want 1", restartCalls.Load())
	}
	if !d.updating.Load() {
		t.Error("update ownership must survive the restart kick")
	}
}

// TestTrySetClaimBarrier_RefusesWhenAlreadyHeld: without this the function
// silently double-acquires, and whichever holder finishes first releases the
// barrier out from under the other.
func TestTrySetClaimBarrier_RefusesWhenAlreadyHeld(t *testing.T) {
	d := &Daemon{}

	if !d.trySetClaimBarrier() {
		t.Fatal("first acquisition should succeed on an idle daemon")
	}
	if d.trySetClaimBarrier() {
		t.Fatal("second acquisition succeeded while the barrier was already held")
	}
	d.releaseClaimBarrier()
	if !d.trySetClaimBarrier() {
		t.Fatal("acquisition should succeed again after release")
	}
}
