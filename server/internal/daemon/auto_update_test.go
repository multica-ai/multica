package daemon

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

// newAutoUpdateTestDaemon returns a Daemon stripped to just the pieces
// tryAutoUpdate touches, plus a sentinel cancelFunc the test can assert on to
// detect that triggerRestart fired. The caller is expected to install its own
// runUpdateFn before calling tryAutoUpdate when it wants to exercise the
// upgrade-success path.
func newAutoUpdateTestDaemon(t *testing.T, currentVersion string) (*Daemon, *atomic.Int32) {
	t.Helper()
	var restartCalls atomic.Int32
	d := &Daemon{
		cfg:    Config{CLIVersion: currentVersion, AutoUpdateEnabled: true},
		logger: slog.Default(),
		cancelFunc: func() {
			restartCalls.Add(1)
		},
	}
	d.runUpdateFn = func(string) (string, error) {
		t.Fatalf("runUpdateFn called unexpectedly")
		return "", nil
	}
	return d, &restartCalls
}

func withStubRelease(t *testing.T, release *cli.GitHubRelease, err error) {
	t.Helper()
	prev := fetchLatestRelease
	fetchLatestRelease = func() (*cli.GitHubRelease, error) { return release, err }
	t.Cleanup(func() { fetchLatestRelease = prev })
}

func TestTryAutoUpdate_SkipsWhenUpdating(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	d.updating.Store(true)
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart called while another update was in progress")
	}
}

func TestTryAutoUpdate_SkipsWhenTasksRunning(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	d.activeTasks.Store(1)
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired with active tasks; auto-update must defer")
	}
	if d.updating.Load() {
		t.Fatalf("updating flag should not have been claimed while tasks were running")
	}
}

// TestTryAutoUpdate_DefersWhenClaimInFlightAtBarrier covers the race the
// review flagged: cheap pre-fetch idle check passes (activeTasks == 0), then
// during the release fetch a poller decides to claim and bumps
// claimsInFlight. trySetClaimBarrier must observe that and defer rather than
// proceed into runUpdate (which would lead to a triggerRestart cancelling
// the just-claimed task mid-run).
func TestTryAutoUpdate_DefersWhenClaimInFlightAtBarrier(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

	d.claimsInFlight = 1 // poller is mid-ClaimTask while activeTasks is still 0

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired despite a claim being in flight at the barrier")
	}
	if d.updating.Load() {
		t.Fatalf("updating flag must be released after a deferred upgrade so the next tick can retry")
	}
	if d.pauseClaims {
		t.Fatalf("pauseClaims must be cleared after a deferred upgrade")
	}
}

// TestTryAutoUpdate_HoldsBarrierAcrossRestart asserts the success path leaves
// pauseClaims set: process exit is imminent and clearing the barrier would
// open a window for a poller to claim a task that the imminent restart is
// about to cancel.
func TestTryAutoUpdate_HoldsBarrierAcrossRestart(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)
	d.runUpdateFn = func(string) (string, error) { return "upgraded", nil }

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 1 {
		t.Fatalf("triggerRestart fired %d times, want 1", restartCalls.Load())
	}
	if !d.pauseClaims {
		t.Fatalf("pauseClaims must remain set across the restart kick; got cleared")
	}
}

// TestTryAutoUpdate_ReleasesBarrierOnUpgradeFailure asserts the failure path
// clears pauseClaims so the daemon can keep claiming tasks normally and
// retry the upgrade on the next tick.
func TestTryAutoUpdate_ReleasesBarrierOnUpgradeFailure(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)
	d.runUpdateFn = func(string) (string, error) {
		return "brew network error", errors.New("brew upgrade failed")
	}

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired despite upgrade failure")
	}
	if d.pauseClaims {
		t.Fatalf("pauseClaims must be cleared after a failed upgrade so pollers resume claiming")
	}
}

// TestTryEnterClaim_RespectsBarrier asserts the poller-side helper returns
// false while pauseClaims is held and that pairs of enter/exit balance the
// counter so a later barrier set sees idle.
func TestTryEnterClaim_RespectsBarrier(t *testing.T) {
	d := &Daemon{}

	if !d.tryEnterClaim() {
		t.Fatal("tryEnterClaim should succeed when barrier is unset")
	}
	d.exitClaim()
	if d.claimsInFlight != 0 {
		t.Fatalf("claimsInFlight not balanced: %d", d.claimsInFlight)
	}

	if !d.trySetClaimBarrier() {
		t.Fatal("trySetClaimBarrier should succeed when idle")
	}
	if d.tryEnterClaim() {
		t.Fatal("tryEnterClaim must refuse while barrier is held")
	}
	d.releaseClaimBarrier()
	if !d.tryEnterClaim() {
		t.Fatal("tryEnterClaim should succeed after barrier release")
	}
	d.exitClaim()
}

func TestTryAutoUpdate_SkipsWhenFetchFails(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, nil, errors.New("network down"))

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired despite fetch failure")
	}
}

func TestTryAutoUpdate_SkipsWhenNotNewer(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.13"}, nil)

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired even though latest == current")
	}
}

func TestTryAutoUpdate_RunsUpgradeAndRestartsOnNewer(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

	var upgradedTo string
	d.runUpdateFn = func(target string) (string, error) {
		upgradedTo = target
		return "upgraded", nil
	}

	d.tryAutoUpdate(context.Background())

	if upgradedTo != "v0.1.14" {
		t.Fatalf("runUpdateFn called with %q, want v0.1.14", upgradedTo)
	}
	if restartCalls.Load() != 1 {
		t.Fatalf("triggerRestart fired %d times, want 1", restartCalls.Load())
	}
	if !d.updating.Load() {
		t.Fatalf("updating flag should remain set across the restart kick; got cleared")
	}
}

func TestTryAutoUpdate_DoesNotRestartOnUpgradeFailure(t *testing.T) {
	d, restartCalls := newAutoUpdateTestDaemon(t, "v0.1.13")
	withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

	d.runUpdateFn = func(string) (string, error) {
		return "brew: network error", errors.New("brew upgrade failed")
	}

	d.tryAutoUpdate(context.Background())

	if restartCalls.Load() != 0 {
		t.Fatalf("triggerRestart fired despite upgrade failure")
	}
	if d.updating.Load() {
		t.Fatalf("updating flag must be released after a failed upgrade so the next tick can retry")
	}
}

func TestAutoUpdateLoop_EarlyExits(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "disabled by config",
			cfg:  Config{AutoUpdateEnabled: false, CLIVersion: "v0.1.13"},
		},
		{
			name: "managed by desktop",
			cfg:  Config{AutoUpdateEnabled: true, CLIVersion: "v0.1.13", LaunchedBy: "desktop"},
		},
		{
			name: "dev build",
			cfg:  Config{AutoUpdateEnabled: true, CLIVersion: "v0.1.13-235-gabcdef0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Daemon{cfg: tt.cfg, logger: slog.Default()}
			d.runUpdateFn = func(string) (string, error) {
				t.Fatalf("runUpdateFn called from an early-exit code path")
				return "", nil
			}
			withStubRelease(t, &cli.GitHubRelease{TagName: "v0.1.14"}, nil)

			done := make(chan struct{})
			go func() {
				d.autoUpdateLoop(context.Background())
				close(done)
			}()
			<-done
		})
	}
}
