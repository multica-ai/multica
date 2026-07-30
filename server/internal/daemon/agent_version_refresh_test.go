package daemon

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// newVersionRefreshFixture brings up a daemon with one registered codex runtime
// in two workspaces, which is the steady state refreshAgentVersions is for:
// nothing is missing, so the converge path never runs and the version the
// daemon (and the server) knows about is frozen at registration.
func newVersionRefreshFixture(t *testing.T) *batchFixture {
	t.Helper()
	fx := newBatchFixture(t)
	fx.daemon.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)
	stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := fx.daemon.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}
	if got := fx.daemon.agentVersion("codex"); got != "9.9.9" {
		t.Fatalf("registered codex version = %q, want 9.9.9", got)
	}
	return fx
}

// TestRefreshAgentVersions_HotRefreshesWithoutRestart is the review's second
// product decision: an agent CLI upgrading is not a reason to bounce the daemon.
// The user needs subsequent tasks to run under the new version — which means the
// cached version (read back through resolveAgentEntry to key policy such as the
// Codex sandbox) and the version the server displays, both refreshed in place.
func TestRefreshAgentVersions_HotRefreshesWithoutRestart(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon
	restarts := trackRestarts(t, d)
	callsBefore := fx.registerCallCount()

	// The user runs `npm i -g @openai/codex`: same path, new binary.
	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())

	if got := d.agentVersion("codex"); got != "10.0.0" {
		t.Errorf("cached codex version = %q, want 10.0.0 — version-keyed policy would still use the old rules", got)
	}
	for _, workspaceID := range []string{"ws-1", "ws-2"} {
		if got := fx.registeredVersionFor(workspaceID, "codex"); got != "10.0.0" {
			t.Errorf("%s registered codex version = %q, want 10.0.0", workspaceID, got)
		}
	}
	if got := fx.registerCallCount() - callsBefore; got != 2 {
		t.Errorf("made %d Register calls, want 2 (one per tracked workspace)", got)
	}
	if restarts.Load() != 0 {
		t.Errorf("scheduled %d restarts; an agent CLI upgrade must refresh, not restart", restarts.Load())
	}
	if got := d.RestartBinary(); got != "" {
		t.Errorf("restart binary = %q, want empty", got)
	}
}

// TestRefreshAgentVersions_DoesNotDisturbTheRuntimeSet pins the "scoped so one
// provider's update doesn't disturb other providers or workspaces" criterion.
//
// Re-registration carries the whole built-in set because that is the shape the
// endpoint upserts, which means every provider's runtime comes back through
// mergeBuiltinRegisterResponse. The invariant the daemon owns is that the
// workspace still holds exactly one runtime per provider afterwards — a
// server-side ID rotation must be swapped in place, not accumulated as a
// duplicate that would double the provider's heartbeats.
func TestRefreshAgentVersions_DoesNotDisturbTheRuntimeSet(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{
		"claude": {Path: "/fake/claude"},
		"codex":  {Path: "/fake/codex"},
	}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	stubAgentProbe(t, map[string]AgentEntry{
		"claude": {Path: "/fake/claude"},
		"codex":  {Path: "/fake/codex"},
	})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}
	if got := registeredProviders(t, d, "ws-1"); len(got) != 2 {
		t.Fatalf("providers before refresh = %v, want [claude codex]", got)
	}

	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())

	got := registeredProviders(t, d, "ws-1")
	if len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Errorf("providers after refresh = %v, want exactly [claude codex] — one runtime per provider, no duplicates", got)
	}
}

// TestRefreshAgentVersions_NoopWhenNothingChanged keeps the steady state quiet:
// a round that finds the same versions must not re-register, or the daemon would
// send one Register call per workspace every few minutes forever.
func TestRefreshAgentVersions_NoopWhenNothingChanged(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	callsBefore := fx.registerCallCount()

	fx.daemon.refreshAgentVersions(context.Background())

	if got := fx.registerCallCount() - callsBefore; got != 0 {
		t.Errorf("made %d Register calls with no version change, want 0", got)
	}
}

// TestRefreshAgentVersions_ProbeFailureIsNotAVersionChange mirrors the self-
// reload rule on the agent side. detectBuiltinRuntimes drops a provider whose
// version it cannot read, so the previous version must survive rather than being
// overwritten with a blank and re-registered as such.
func TestRefreshAgentVersions_ProbeFailureIsNotAVersionChange(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon
	callsBefore := fx.registerCallCount()
	fx.setProbeErr(func(string, int) error { return errors.New("fork/exec: text file busy") })

	d.refreshAgentVersions(context.Background())

	if got := d.agentVersion("codex"); got != "9.9.9" {
		t.Errorf("cached codex version = %q, want the previous 9.9.9 preserved across a failed probe", got)
	}
	if got := fx.registerCallCount() - callsBefore; got != 0 {
		t.Errorf("made %d Register calls off a failed probe, want 0", got)
	}

	// A later round with a working probe still catches the real upgrade.
	fx.setProbeErr(nil)
	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())

	if got := d.agentVersion("codex"); got != "10.0.0" {
		t.Errorf("cached codex version = %q, want 10.0.0 once the probe recovered", got)
	}
}

// TestRefreshAgentVersions_BlankVersionKeepsThePreviousOne closes a hole the
// minimum-version gate leaves open: agent.CheckMinVersion returns nil for any
// provider with no MinVersions entry, so a CLI whose `--version` succeeds but
// prints nothing lands in the registration payload with a blank version.
// Treating that as a change would replace a version the server had with nothing.
func TestRefreshAgentVersions_BlankVersionKeepsThePreviousOne(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon
	callsBefore := fx.registerCallCount()

	fx.setProbeVersion("")
	d.refreshAgentVersions(context.Background())

	if got := fx.registerCallCount() - callsBefore; got != 0 {
		t.Errorf("made %d Register calls off a blank version, want 0", got)
	}
	if got := fx.registeredVersionFor("ws-1", "codex"); got != "9.9.9" {
		t.Errorf("ws-1 registered codex version = %q, want the previous 9.9.9 intact", got)
	}
}

// TestRefreshAgentVersions_RetriesAfterRegisterFailure closes the hole the
// version cache creates: setAgentVersion has already moved on by the time the
// register call fails, so the next round sees no change. Without the retry flag
// a transient 5xx would leave the server displaying a version the daemon
// stopped using, until something unrelated re-registered.
func TestRefreshAgentVersions_RetriesAfterRegisterFailure(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon
	fx.failRegisterFor("ws-2", true)

	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-2", "codex"); got == "10.0.0" {
		t.Fatal("ws-2 register was supposed to fail")
	}
	if !d.agentReregisterPending.Load() {
		t.Fatal("a failed re-register must arm the retry, or the version change is lost")
	}

	fx.failRegisterFor("ws-2", false)
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-2", "codex"); got != "10.0.0" {
		t.Errorf("ws-2 registered codex version = %q after the retry, want 10.0.0", got)
	}
	if d.agentReregisterPending.Load() {
		t.Error("retry flag must clear once every workspace re-registered")
	}
}

// TestRefreshAgentVersions_NotStarvedByAStuckProvider guards against an
// optimization that looks free and isn't: skipping the round while the converge
// half has work to do.
//
// A provider permanently below the minimum supported version never leaves
// providersMissingRuntimes — deliberately, so it recovers on its own after an
// upgrade. Yielding to that would let one stuck CLI silently disable version
// refresh for every healthy provider on the machine, forever.
func TestRefreshAgentVersions_NotStarvedByAStuckProvider(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	// A second CLI is installed but permanently rejected by the version gate,
	// so it stays in the missing set no matter how often converge retries.
	setProbe := stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	setProbe(map[string]AgentEntry{
		"codex":  {Path: "/fake/codex"},
		"claude": {Path: "/fake/claude-ancient"},
	})
	d.refreshAgentAvailability()
	origCheck := checkAgentMinVersion
	t.Cleanup(func() { checkAgentMinVersion = origCheck })
	checkAgentMinVersion = func(provider, _ string) error {
		if provider == "claude" {
			return errors.New("version too old")
		}
		return nil
	}
	d.convergeRuntimeRegistrations(context.Background())
	if missing := d.providersMissingRuntimes(); len(missing) == 0 {
		t.Fatal("claude should still be missing a runtime; the starvation setup is wrong")
	}

	// codex nevertheless upgrades, and must still be picked up.
	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())

	if got := d.agentVersion("codex"); got != "10.0.0" {
		t.Errorf("cached codex version = %q, want 10.0.0 — a stuck provider must not starve the healthy ones", got)
	}
	if got := fx.registeredVersionFor("ws-1", "codex"); got != "10.0.0" {
		t.Errorf("ws-1 registered codex version = %q, want 10.0.0", got)
	}
}

// TestAgentDiscoveryLoop_PicksUpAnInPlaceCLIUpgrade is the loop-level wiring:
// the converge half only ever looks at providers *missing* a runtime, so without
// the version ticker an in-place upgrade stays invisible until a daemon restart.
func TestAgentDiscoveryLoop_PicksUpAnInPlaceCLIUpgrade(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	origDiscovery, origVersion := agentDiscoveryInterval, agentVersionRefreshInterval
	t.Cleanup(func() {
		agentDiscoveryInterval = origDiscovery
		agentVersionRefreshInterval = origVersion
	})
	// Discovery stays long so only the version ticker can drive this test.
	agentDiscoveryInterval = time.Hour
	agentVersionRefreshInterval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		d.agentDiscoveryLoop(ctx)
		close(done)
	}()

	fx.setProbeVersion("10.0.0")
	waitFor(t, func() bool { return d.agentVersion("codex") == "10.0.0" },
		"agentDiscoveryLoop never refreshed the agent CLI version")
	waitFor(t, func() bool { return fx.registeredVersionFor("ws-1", "codex") == "10.0.0" },
		"agentDiscoveryLoop never re-registered with the new version")

	cancel()
	<-done
}

// trackRestarts installs a cancelFunc that counts restart kicks, so a test can
// assert that a code path did NOT bounce the daemon.
func trackRestarts(t *testing.T, d *Daemon) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	prev := d.cancelFunc
	t.Cleanup(func() { d.cancelFunc = prev })
	d.cancelFunc = func() { calls.Add(1) }
	return &calls
}
