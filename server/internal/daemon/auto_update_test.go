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
	if !d.tryBeginUpdate(false) {
		t.Fatal("failed to acquire update owner for test setup")
	}
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
	if d.isUpdating() {
		t.Fatalf("update owner should not have been acquired while tasks were running")
	}
}

// TestTryAutoUpdate_DefersWhenClaimInFlightAtBarrier covers the race the
// review flagged: cheap pre-fetch idle check passes (activeTasks == 0), then
// during the release fetch a poller decides to claim and bumps
// claimsInFlight. tryBeginUpdate must observe that and defer rather than
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
	if d.isUpdating() {
		t.Fatalf("update owner must be released after a deferred upgrade so the next tick can retry")
	}
	if !d.tryEnterClaim() {
		t.Fatal("claims must remain enabled after a deferred upgrade")
	}
	d.exitClaim()
}

// TestTryAutoUpdate_HoldsBarrierAcrossRestart asserts the success path leaves
// update ownership held: process exit is imminent and clearing the barrier would
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
	if !d.isUpdating() {
		t.Fatalf("update ownership must remain held across the restart kick; got cleared")
	}
}

// TestTryAutoUpdate_ReleasesBarrierOnUpgradeFailure asserts the failure path
// clears update ownership so the daemon can keep claiming tasks normally and
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
	if d.isUpdating() {
		t.Fatalf("update ownership must be cleared after a failed upgrade so pollers resume claiming")
	}
}

// TestTryEnterClaim_RespectsBarrier asserts the poller-side helper returns
// false while a lifecycle owner is held and that pairs of enter/exit balance the
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

	if !d.tryBeginUpdate(true) {
		t.Fatal("tryBeginUpdate should acquire the barrier when idle")
	}
	if d.tryEnterClaim() {
		t.Fatal("tryEnterClaim must refuse while barrier is held")
	}
	d.releaseUpdate()
	if !d.tryBeginDrain() {
		t.Fatal("tryBeginDrain should acquire an idle claim barrier")
	}
	if d.tryBeginUpdate(true) {
		t.Fatal("auto-update must not steal a manual drain barrier")
	}
	d.releaseUpdate()
	if d.tryEnterClaim() {
		d.exitClaim()
		t.Fatal("a mismatched update release must not clear the drain barrier")
	}
	d.releaseDrain()
	if !d.tryEnterClaim() {
		t.Fatal("tryEnterClaim should succeed after barriers are released")
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
	if !d.isUpdating() {
		t.Fatalf("update owner should remain held across the restart kick; got cleared")
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
	if d.isUpdating() {
		t.Fatalf("update owner must be released after a failed upgrade so the next tick can retry")
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
