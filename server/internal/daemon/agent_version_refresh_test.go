package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// newVersionRefreshFixture brings up a daemon with one registered codex runtime
// in two workspaces, which is the steady state refreshAgentVersions is for:
// nothing is missing, so the converge path never runs and the version the
// daemon (and the server) knows about is frozen at registration.
func newVersionRefreshFixture(t *testing.T) *batchFixture {
	t.Helper()
	return newVersionRefreshFixtureWith(t, nil)
}

// newVersionRefreshFixtureWith is newVersionRefreshFixture with a hook to
// configure the fixture BEFORE the first registration, for the settings that
// only mean anything if they were in place when the rows were created.
func newVersionRefreshFixtureWith(t *testing.T, configure func(fx *batchFixture)) *batchFixture {
	t.Helper()
	fx := newBatchFixture(t)
	if configure != nil {
		configure(fx)
	}
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

// TestRefreshAgentVersions_UpdatesTheVersionTasksActuallyRead is the end-to-end
// half of the healed-pair fix. The unit test covers refreshHealedVersion; this
// one proves the refresh path reaches it, which is the part that was missing.
//
// For a provider that has been self-healed — the version-manager population most
// likely to upgrade in place again — resolveAgentEntry returns healed.version,
// not the shared cache. Refreshing only the cache told the server a new version
// while every task kept launching under the old one.
func TestRefreshAgentVersions_UpdatesTheVersionTasksActuallyRead(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon

	// A real on-disk binary, since resolveAgentEntry gates the healed path on it
	// actually being present.
	healedPath := filepath.Join(t.TempDir(), "codex")
	writeExecStub(t, healedPath)
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: healedPath, Command: "codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: healedPath, Command: "codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	// Pretend a self-heal previously adopted this binary at 9.9.9. (freshDaemon
	// leaves resolvedPaths nil, like a daemon that has never healed anything.)
	d.resolvedPathsMu.Lock()
	d.resolvedPaths = map[string]healedAgent{"codex": {path: healedPath, version: "9.9.9"}}
	d.resolvedPathsMu.Unlock()

	entry := d.cfg.Agents["codex"]
	if _, version := d.resolveAgentEntry(context.Background(), "codex", entry); version != "9.9.9" {
		t.Fatalf("baseline resolveAgentEntry version = %q, want 9.9.9", version)
	}

	// codex is upgraded in place at the same path.
	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())

	if _, version := d.resolveAgentEntry(context.Background(), "codex", entry); version != "10.0.0" {
		t.Errorf("resolveAgentEntry version = %q, want 10.0.0 — this is the value runTask keys the Codex "+
			"sandbox policy off, so tasks would still run under the old version's rules", version)
	}
	if got := fx.registeredVersionFor("ws-1", "codex"); got != "10.0.0" {
		t.Errorf("ws-1 registered codex version = %q, want 10.0.0", got)
	}
}

// TestRefreshAgentVersions_ReachesWorkspacesAnotherPathDidNotRegister covers the
// hole in treating the version cache as a record of what the server knows.
//
// Every path that registers probes through setAgentVersion — the converge round,
// a new workspace's sync, a runtime_gone re-register, a self-heal at task launch
// — and each of them tells only the workspaces it happened to be working on, or
// none at all. Whichever writer lands first leaves the cache matching what is on
// disk, so a before/after diff of the cache sees nothing to do, and every
// workspace that writer skipped keeps the old version until the daemon restarts.
//
// The population most exposed is the one this feature exists for: a version
// manager upgrading in place deletes the pinned path, so the next task launch
// self-heals and advances the cache before any refresh round runs.
func TestRefreshAgentVersions_ReachesWorkspacesAnotherPathDidNotRegister(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	// codex is upgraded, and something other than the refresh round probes it
	// first. detectBuiltinRuntimes is exactly what those paths call.
	fx.setProbeVersion("10.0.0")
	if _, belowMin, _ := d.detectBuiltinRuntimes(context.Background()); len(belowMin) != 0 {
		t.Fatalf("below-minimum = %v, want none", belowMin)
	}
	if got := d.agentVersion("codex"); got != "10.0.0" {
		t.Fatalf("cached codex version = %q, want the other path to have advanced it to 10.0.0", got)
	}

	d.refreshAgentVersions(context.Background())

	for _, workspaceID := range []string{"ws-1", "ws-2"} {
		if got := fx.registeredVersionFor(workspaceID, "codex"); got != "10.0.0" {
			t.Errorf("%s registered codex version = %q, want 10.0.0 — the cache moving first must not "+
				"swallow the change for the workspaces nothing re-registered", workspaceID, got)
		}
	}

	// And it settles rather than re-registering every round from here on.
	calls := fx.registerCallCount()
	d.refreshAgentVersions(context.Background())
	if got := fx.registerCallCount() - calls; got != 0 {
		t.Errorf("made %d Register calls after settling, want 0", got)
	}
}

// TestRefreshAgentVersions_BlankToRealVersionReRegisters covers a provider that
// registered while its version probe was failing: the server shows a blank
// version for it, and nothing else will ever fix that.
//
// Converge only looks at providers MISSING a runtime and this one has one, so it
// never revisits it. And the ordering inside a refresh round hides it from the
// refresh too — detectBuiltinRuntimes calls setAgentVersion with the real
// version before the comparison runs, so by the next round the cache already
// matches and there is no change to see. Blank -> real has exactly one chance to
// be reported.
func TestRefreshAgentVersions_BlankToRealVersionReRegisters(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})

	// Register while the CLI reports no version at all.
	fx.setProbeVersion("")
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}
	if got := fx.registeredVersionFor("ws-1", "codex"); got != "" {
		t.Fatalf("registered codex version = %q, want blank for this setup", got)
	}

	// The probe recovers.
	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-1", "codex"); got != "10.0.0" {
		t.Errorf("registered codex version = %q, want 10.0.0 — a blank version would stay on the server forever", got)
	}

	// And it settles: no further rounds re-register.
	calls := fx.registerCallCount()
	d.refreshAgentVersions(context.Background())
	if got := fx.registerCallCount() - calls; got != 0 {
		t.Errorf("made %d Register calls after settling, want 0", got)
	}
}

// TestRefreshAgentVersions_DemotesRuntimeDowngradedBelowMinimum is the downgrade
// case nothing reacted to.
//
// probeBuiltinRuntime drops a below-minimum provider from the payload, so there
// is no version left to compare; the runtime row survives because the built-in
// merge is additive; and the provider is absent from providersMissingRuntimes
// precisely because it still holds that runtime. Every path looks idle while the
// daemon keeps routing tasks to a binary it has already verified is too old.
func TestRefreshAgentVersions_DemotesRuntimeDowngradedBelowMinimum(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("providers before downgrade = %v, want [codex]", got)
	}

	// The CLI is downgraded in place to a version below the minimum.
	origCheck := checkAgentMinVersion
	t.Cleanup(func() { checkAgentMinVersion = origCheck })
	checkAgentMinVersion = func(_, version string) error {
		if version == "0.0.1" {
			return &agent.BelowMinimumError{AgentType: "codex", Detected: version, Minimum: "1.0.0"}
		}
		return nil
	}
	fx.setProbeVersion("0.0.1")

	d.refreshAgentVersions(context.Background())

	if got := registeredProviders(t, d, "ws-1"); len(got) != 0 {
		t.Errorf("providers after downgrade = %v, want none — a binary confirmed below the minimum "+
			"must stop accepting work", got)
	}
	if got := fx.deregisteredCount(); got == 0 {
		t.Error("runtimes were dropped locally but the server was never told, so it would keep routing tasks")
	}
	// Recovery needs no new machinery: with no runtime it is missing again, so
	// the converge path owns bringing it back after an upgrade.
	if missing := d.providersMissingRuntimes(); len(missing) != 1 || missing[0] != "codex" {
		t.Errorf("providersMissingRuntimes = %v, want [codex] so converge can restore it after an upgrade", missing)
	}
}

// TestRefreshAgentVersions_DemotesWhenTheDowngradeMovedThePath is the same
// downgrade through the shape a version manager actually produces: the pinned
// path is deleted rather than overwritten, so the daemon reaches the too-old
// binary through the self-heal instead of a direct probe.
//
// The heal refuses to adopt it, which is right — it must never be launched — but
// the refusal is also the verdict. Swallowed, the probe falls back to the
// vanished path and can only report "version detection failed", which by design
// leaves the runtime alone; the daemon then keeps claiming tasks for a CLI that
// fails at launch, every time.
func TestRefreshAgentVersions_DemotesWhenTheDowngradeMovedThePath(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon

	pinned := filepath.Join(t.TempDir(), "codex")
	writeExecStub(t, pinned)
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: pinned, Command: "codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: pinned, Command: "codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}
	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("providers before the downgrade = %v, want [codex]", got)
	}

	// The downgrade: the pinned path is gone, and the command now resolves on
	// PATH to an older install that the gate rejects.
	missing, _ := vanishedPinnedPath(t)
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: missing, Command: "codex"}}
	d.agentsAvailable.Store(&map[string]AgentEntry{"codex": {Path: missing, Command: "codex"}})
	// A probe of the deleted path has to fail the way it does in production, or
	// the test quietly exercises the direct below-minimum branch instead of the
	// self-heal one and passes with the verdict still swallowed.
	fx.setProbeErr(func(path string, _ int) error {
		if !agentExecutablePresent(path) {
			return errors.New("fork/exec: no such file or directory")
		}
		return nil
	})
	origCheck := checkAgentMinVersion
	t.Cleanup(func() { checkAgentMinVersion = origCheck })
	checkAgentMinVersion = func(_, version string) error {
		if version == "0.0.1" {
			return &agent.BelowMinimumError{AgentType: "codex", Detected: version, Minimum: "1.0.0"}
		}
		return nil
	}
	fx.setProbeVersion("0.0.1")

	d.refreshAgentVersions(context.Background())

	if got := registeredProviders(t, d, "ws-1"); len(got) != 0 {
		t.Errorf("providers after the downgrade = %v, want none — the daemon has verified this binary "+
			"cannot run and must stop accepting work for it", got)
	}
	if got := fx.deregisteredCount(); got == 0 {
		t.Error("runtime was dropped locally but the server was never told, so it would keep routing tasks")
	}
}

// TestRefreshAgentVersions_KeepsRuntimeWhenTheProbeMerelyFailed is the other half
// of that rule. An unreadable version is transient — the CLI was busy or
// mid-upgrade — and tearing down a working runtime over one is the mistake
// refreshAgentAvailability's one-directional rule exists to avoid.
func TestRefreshAgentVersions_KeepsRuntimeWhenTheProbeMerelyFailed(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon
	fx.setProbeErr(func(string, int) error { return errors.New("fork/exec: text file busy") })

	d.refreshAgentVersions(context.Background())

	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "codex" {
		t.Errorf("providers after a failed probe = %v, want [codex] kept", got)
	}
	if got := fx.deregisteredCount(); got != 0 {
		t.Errorf("deregistered %d runtimes over a probe failure, want 0", got)
	}
}

// TestDemoteBelowMinimumRuntimes_DoesNotRaceTheHealthHandler pins why the filter
// allocates instead of reusing the slice.
//
// healthHandler copies each workspace's runtimeIDs header under d.mu and
// serializes it after releasing the lock, so filtering in place writes into a
// slice another goroutine is reading — a genuine data race even when the value
// written is identical. removeStaleRuntime already follows the same rule.
//
// The assertion is the race detector; the demotion assertion below only proves
// the loop did the work it was supposed to be racing on.
func TestDemoteBelowMinimumRuntimes_DoesNotRaceTheHealthHandler(t *testing.T) {
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

	handler := d.healthHandler(time.Now())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
		}
	}()
	for i := 0; i < 100; i++ {
		d.demoteUnusableRuntimes(context.Background(), map[string]runtimeVerdict{"codex": {reason: "below minimum supported version: 0.0.1"}})
	}
	<-done

	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "claude" {
		t.Errorf("providers after demotion = %v, want [claude]", got)
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
	// The half that actually mattered and was missed the first time: the probe
	// runs setAgentVersion BEFORE refreshAgentVersions gets to inspect anything,
	// so protecting only the payload leaves the daemon's own cache wiped — and
	// that cache, not the payload, is what version-keyed policy reads.
	if got := d.agentVersion("codex"); got != "9.9.9" {
		t.Errorf("cached codex version = %q, want the previous 9.9.9 — a blank probe must not wipe the cache "+
			"every version-keyed policy reads", got)
	}
}

// TestRefreshAgentVersions_KeepsRuntimeWhenTheVersionGoesBlank runs a blank
// probe through the REAL minimum-version gate, where codex has a floor.
// CheckMinVersion fails on a blank for two very different reasons — the version
// parsed and is genuinely too old, or it could not be parsed at all — and only
// the first is a verdict. Classifying the parse failure as below-minimum would
// demote a healthy, online runtime because one probe printed nothing.
func TestRefreshAgentVersions_KeepsRuntimeWhenTheVersionGoesBlank(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon
	stubProbeRetry(t, time.Millisecond, time.Second)
	origCheck := checkAgentMinVersion
	t.Cleanup(func() { checkAgentMinVersion = origCheck })
	checkAgentMinVersion = agent.CheckMinVersion

	fx.setProbeVersion("")
	d.refreshAgentVersions(context.Background())

	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "codex" {
		t.Errorf("providers after a blank probe = %v, want [codex] — an unreadable version is transient "+
			"and must never tear down a working runtime", got)
	}
	if got := fx.deregisteredCount(); got != 0 {
		t.Errorf("deregistered %d runtimes off a blank version, want 0", got)
	}
	if got := d.agentVersion("codex"); got != "9.9.9" {
		t.Errorf("cached codex version = %q, want the previous 9.9.9 preserved", got)
	}
}

// TestRefreshAgentVersions_BlankProbeDoesNotWipeAnUnflooredProvidersVersion is
// the other direction of the same gap. A provider with no MinVersions floor
// passes the gate with a blank version, and the payload is rebuilt from probe
// results — so a NEIGHBOUR's legitimate upgrade re-sends the whole built-in
// set and would carry the blank to the server, wiping a version it already
// knows while the daemon's own cache (protected by setAgentVersion) still
// holds it. The payload must reuse the last known version instead.
func TestRefreshAgentVersions_BlankProbeDoesNotWipeAnUnflooredProvidersVersion(t *testing.T) {
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

	// claude's `--version` starts exiting 0 while printing nothing; the fixture
	// gate has no floor, so the blank sails through to the payload builder.
	origDetect := detectAgentVersion
	t.Cleanup(func() { detectAgentVersion = origDetect })
	detectAgentVersion = func(ctx context.Context, runtimeCmd agent.Command) (string, error) {
		path := runtimeCmd.Path
		if strings.Contains(path, "claude") {
			return "", nil
		}
		return origDetect(ctx, runtimeCmd)
	}

	// codex upgrades, which re-registers the whole built-in set — claude rides
	// along in the same payload.
	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-1", "codex"); got != "10.0.0" {
		t.Fatalf("codex registered version = %q, want 10.0.0", got)
	}
	if got := fx.registeredVersionFor("ws-1", "claude"); got != "9.9.9" {
		t.Errorf("claude registered version = %q, want 9.9.9 — one provider's upgrade must not carry "+
			"its neighbour's blank to the server", got)
	}
	if got := d.agentVersion("claude"); got != "9.9.9" {
		t.Errorf("cached claude version = %q, want 9.9.9", got)
	}
}

// TestReregisterAfterRuntimeGone_KeepsRuntimeWhoseProbeFailed covers the
// authoritative half of the unavailable/below-minimum split. The runtime_gone
// recovery re-registers the whole workspace and applies the response as the
// complete runtime set — but a provider dropped from the payload because its
// probe FAILED is absent by accident, not by decision. Treating the response
// as authoritative for it deletes a healthy runtime over one transient probe
// failure (mid-upgrade, ETXTBSY) — the exact case the verdict split promises
// to leave alone. Only a below-minimum verdict may take runtimes down.
func TestReregisterAfterRuntimeGone_KeepsRuntimeWhoseProbeFailed(t *testing.T) {
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
		t.Fatalf("providers before recovery = %v, want [claude codex]", got)
	}

	// codex is mid-upgrade: its --version fails while the recovery runs.
	stubProbeRetry(t, time.Millisecond, time.Second)
	fx.setProbeErr(func(path string, _ int) error {
		if strings.Contains(path, "codex") {
			return errors.New("fork/exec: text file busy")
		}
		return nil
	})

	if err := d.reregisterWorkspaceAfterRuntimeGone(context.Background(), "ws-1"); err != nil {
		t.Fatalf("reregisterWorkspaceAfterRuntimeGone: %v", err)
	}

	if got := registeredProviders(t, d, "ws-1"); len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Errorf("providers after recovery = %v, want [claude codex] — a transient probe failure must not "+
			"evict a healthy runtime from an authoritative re-register", got)
	}
}

// TestProfileDriftRefresh_KeepsRuntimeWhoseProbeFailed is the same contract on
// the drift path, which is sharper-edged: it Deregisters whatever the
// authoritative apply drops, so without the preserve the transient failure
// would not just untrack the healthy runtime locally but proactively take it
// offline server-side, failing its in-flight tasks via the offline sweeper.
func TestProfileDriftRefresh_KeepsRuntimeWhoseProbeFailed(t *testing.T) {
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
	var codexID string
	d.mu.Lock()
	for _, id := range d.workspaces["ws-1"].runtimeIDs {
		if rt := d.runtimeIndex[id]; rt.Provider == "codex" {
			codexID = id
		}
	}
	d.mu.Unlock()
	if codexID == "" {
		t.Fatal("codex runtime not registered in setup")
	}

	// The drift: a profile appears server-side. Its command_name is blank, so
	// it changes the profile signature without entering the payload — the
	// register carries built-ins only.
	fx.mu.Lock()
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Broken",
		ProtocolFamily: "codex", Visibility: "workspace", Enabled: true,
	}}
	fx.mu.Unlock()
	stubProbeRetry(t, time.Millisecond, time.Second)
	fx.setProbeErr(func(path string, _ int) error {
		if strings.Contains(path, "codex") {
			return errors.New("fork/exec: text file busy")
		}
		return nil
	})

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	if got := registeredProviders(t, d, "ws-1"); len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Errorf("providers after drift refresh = %v, want [claude codex]", got)
	}
	for _, id := range fx.deregisteredIDs() {
		if id == codexID {
			t.Errorf("codex runtime %s was Deregistered — the drift path took a healthy runtime offline "+
				"over a failed probe", id)
		}
	}
}

// TestProfileDriftRefresh_DoesNotConvergeToZeroOnFailedProbes guards the
// sharpest corner of the same gap: when EVERY builtin probe fails in the same
// round, the register payload is empty and the drift path used to read that as
// "this daemon hosts nothing here" — deregistering every healthy runtime the
// workspace has. An empty payload only proves convergence-to-zero when every
// probe actually concluded.
func TestProfileDriftRefresh_DoesNotConvergeToZeroOnFailedProbes(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	fx.mu.Lock()
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Broken",
		ProtocolFamily: "codex", Visibility: "workspace", Enabled: true,
	}}
	fx.mu.Unlock()
	stubProbeRetry(t, time.Millisecond, time.Second)
	fx.setProbeErr(func(string, int) error { return errors.New("fork/exec: text file busy") })

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "codex" {
		t.Errorf("providers after drift refresh = %v, want [codex] — an all-probes-failed round must not "+
			"converge the workspace to zero", got)
	}
	if got := fx.deregisteredCount(); got != 0 {
		t.Errorf("deregistered %d runtimes off failed probes, want 0", got)
	}
}

// TestConcurrentRegisters_SameWorkspaceSerializedSoRecordMatchesServer pins the
// workspaceRegisterLock contract: the per-workspace version record must follow
// the SERVER's processing order. Two concurrent registers for one workspace
// whose HTTP responses complete in the opposite order from the server's
// processing would otherwise record the newer payload while the server kept
// the older one — a mismatch the refresh round cannot see, precisely because
// the record is what it compares against, so it would persist until the next
// version change or restart.
func TestConcurrentRegisters_SameWorkspaceSerializedSoRecordMatchesServer(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	// Make every register linger, so unserialized sends would overlap.
	fx.setRegisterDelay(30 * time.Millisecond)
	payloads := [][]map[string]string{
		{{"name": "Codex", "type": "codex", "version": "1.0.0", "status": "online"}},
		{{"name": "Codex", "type": "codex", "version": "2.0.0", "status": "online"}},
	}
	var wg sync.WaitGroup
	for _, p := range payloads {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Through the wrapper, because that is the production contract: the
			// Locked entry points assume their caller took the workspace's
			// register lock for the whole send → apply → cleanup sequence.
			_ = d.withWorkspaceRegisterLock("ws-1", func() error {
				if _, err := d.registerBuiltinRuntimesForWorkspaceLocked(context.Background(), "ws-1", p); err != nil {
					t.Errorf("register: %v", err)
				}
				return nil
			})
		}()
	}
	wg.Wait()

	if got := fx.maxRegisterInFlight(); got != 1 {
		t.Errorf("max concurrent Register calls for one workspace = %d, want 1 — unserialized sends let "+
			"the record contradict the server's processing order", got)
	}
	serverHas := fx.registeredVersionFor("ws-1", "codex")
	d.mu.Lock()
	recorded := d.workspaces["ws-1"].builtinVersions["codex"]
	d.mu.Unlock()
	if recorded != serverHas {
		t.Errorf("recorded version %q != server's final version %q — the refresh round would never "+
			"re-register this workspace", recorded, serverHas)
	}
}

// TestRefreshAgentVersions_RevisitsWorkspaceRegisteredByAnOlderConcurrentRound
// pins the interleave that breaks any GLOBAL acknowledgement: registration
// entry points are not serialized, so a register that probed before an upgrade
// can land after a refresh round already brought every workspace it could see
// up to date. A global "done" cleared at that moment would strand the late
// workspace on the old version forever. Scoped per workspace, the late call
// simply records what it sent, the record disagrees with the next probe round,
// and that round revisits exactly the workspace that fell behind.
func TestRefreshAgentVersions_RevisitsWorkspaceRegisteredByAnOlderConcurrentRound(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	// codex upgrades; the refresh round brings both workspaces to 10.0.0.
	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())
	if got := fx.registeredVersionFor("ws-2", "codex"); got != "10.0.0" {
		t.Fatalf("ws-2 registered codex version = %q, want 10.0.0", got)
	}

	// A register that probed BEFORE the upgrade lands now, exactly as the
	// production entry points would send it: the server holds 9.9.9 for ws-2
	// again, and the daemon records what that call carried.
	stale := []map[string]string{{"name": "Codex", "type": "codex", "version": "9.9.9", "status": "online"}}
	resp, err := d.registerBuiltinRuntimesForWorkspaceLocked(context.Background(), "ws-2", stale)
	if err != nil {
		t.Fatalf("late register: %v", err)
	}
	if _, _, ok := d.mergeBuiltinRegisterResponse("ws-2", resp); !ok {
		t.Fatal("merge of the late register response failed")
	}
	if got := fx.registeredVersionFor("ws-2", "codex"); got != "9.9.9" {
		t.Fatalf("ws-2 registered codex version = %q, want the stale 9.9.9 (interleave precondition)", got)
	}

	// The next round must notice ws-2 fell behind and bring it back — without
	// dragging ws-1 through another register to do it.
	_, ws1Calls := fx.registrationFor("ws-1")
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-2", "codex"); got != "10.0.0" {
		t.Errorf("ws-2 registered codex version = %q, want 10.0.0 — a workspace registered by an older "+
			"concurrent round must be revisited, or it is stuck on the old version forever", got)
	}
	if _, calls := fx.registrationFor("ws-1"); calls != ws1Calls {
		t.Errorf("ws-1 received %d extra Register calls, want 0 — only the workspace that fell behind "+
			"needs revisiting", calls-ws1Calls)
	}
}

// TestRefreshAgentVersions_RetriesAfterRegisterFailure closes the hole the
// version cache creates: setAgentVersion has already moved on by the time the
// register call fails, so a cache diff would see no change on the next round.
// The per-workspace record only advances on a register the server accepted, so
// the failed workspace stays behind and is retried — without dragging the
// workspaces that succeeded back through another register.
func TestRefreshAgentVersions_RetriesAfterRegisterFailure(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon
	fx.failRegisterFor("ws-2", true)

	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-2", "codex"); got == "10.0.0" {
		t.Fatal("ws-2 register was supposed to fail")
	}
	if got := fx.registeredVersionFor("ws-1", "codex"); got != "10.0.0" {
		t.Fatalf("ws-1 registered codex version = %q, want 10.0.0 — one workspace failing must not block the others", got)
	}

	fx.failRegisterFor("ws-2", false)
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-2", "codex"); got != "10.0.0" {
		t.Errorf("ws-2 registered codex version = %q after the retry, want 10.0.0", got)
	}
	// And it settles: the retry advanced ws-2's record, so nothing is behind.
	calls := fx.registerCallCount()
	d.refreshAgentVersions(context.Background())
	if got := fx.registerCallCount() - calls; got != 0 {
		t.Errorf("made %d Register calls after settling, want 0", got)
	}
}

// TestRefreshAgentVersions_ObligationSurvivesAnIncompletePayload is why the
// per-workspace record is merged per provider rather than replaced with each
// payload.
//
// codex's new version fails to register, then codex's probe fails on the next
// round while claude's succeeds. That round registers a payload with no codex in
// it at all. Counting that success as covering codex too would strand it — its
// cache already holds the new version, so no cache diff would ever report a
// change again and the server would keep showing the old version indefinitely.
func TestRefreshAgentVersions_ObligationSurvivesAnIncompletePayload(t *testing.T) {
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

	// Round 1: both upgrade, the register fails, so the server still holds the
	// versions the initial sync registered.
	fx.failRegister(true)
	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())
	if got := fx.registeredVersionFor("ws-1", "codex"); got != "9.9.9" {
		t.Fatalf("registered codex version after failed register = %q, want the original 9.9.9", got)
	}

	// Round 2: register recovers, but codex's probe now fails, so codex is
	// dropped from the payload while claude registers fine.
	fx.failRegister(false)
	fx.setProbeErr(func(path string, _ int) error {
		if strings.Contains(path, "codex") {
			return errors.New("fork/exec: text file busy")
		}
		return nil
	})
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-1", "claude"); got != "10.0.0" {
		t.Errorf("claude registered version = %q, want 10.0.0 — a dropped provider must not block the others", got)
	}

	// Round 3: codex's probe recovers. Its cache already reads 10.0.0, so
	// nothing here is a "change" — the per-workspace record still saying 9.9.9
	// is the only reason the server ever learns about it.
	fx.setProbeErr(nil)
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-1", "codex"); got != "10.0.0" {
		t.Errorf("codex registered version = %q, want 10.0.0 once its probe recovered", got)
	}
	// And it settles.
	calls := fx.registerCallCount()
	d.refreshAgentVersions(context.Background())
	if got := fx.registerCallCount() - calls; got != 0 {
		t.Errorf("made %d Register calls after settling, want 0", got)
	}
}

// TestRefreshAgentVersions_UnreachableProviderDoesNotStormTheServer guards the
// regression the per-workspace record invites: an entry can outlive its
// provider.
//
// refreshAgentAvailability is deliberately one-directional, so an uninstalled
// CLI is never dropped from the availability set — it just keeps failing its
// probe and never reappears in the payload, leaving its record permanently
// behind. Retrying on "any record disagrees" rather than "a version this
// round's payload actually carries disagrees" would turn that into one
// register call per workspace every few minutes for the life of the daemon.
func TestRefreshAgentVersions_UnreachableProviderDoesNotStormTheServer(t *testing.T) {
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

	// Both upgrade; the register fails, so both are pending.
	fx.failRegister(true)
	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())

	// codex is then uninstalled — its probe fails from here on, permanently.
	fx.failRegister(false)
	fx.setProbeErr(func(path string, _ int) error {
		if strings.Contains(path, "codex") {
			return errors.New("executable file not found")
		}
		return nil
	})

	// The round that can still advance claude registers once.
	d.refreshAgentVersions(context.Background())
	settled := fx.registerCallCount()
	if got := fx.registeredVersionFor("ws-1", "claude"); got != "10.0.0" {
		t.Fatalf("claude registered version = %q, want 10.0.0 — the reachable provider must still advance", got)
	}

	// Every later round can confirm nothing, so it must issue no requests at all.
	for i := 0; i < 5; i++ {
		d.refreshAgentVersions(context.Background())
	}
	if got := fx.registerCallCount() - settled; got != 0 {
		t.Errorf("made %d Register calls across 5 rounds that could confirm nothing, want 0 — "+
			"a permanently unreachable provider must not become a request storm", got)
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

// stubBelowMinimumAt makes checkAgentMinVersion reject exactly one version, so a
// test can move the probe onto it and produce a CONFIRMED below-minimum verdict.
func stubBelowMinimumAt(t *testing.T, rejected string) {
	t.Helper()
	orig := checkAgentMinVersion
	t.Cleanup(func() { checkAgentMinVersion = orig })
	checkAgentMinVersion = func(_, version string) error {
		if version == rejected {
			return &agent.BelowMinimumError{AgentType: "codex", Detected: version, Minimum: "1.0.0"}
		}
		return nil
	}
}

// TestDemoteBelowMinimumRuntimes_LateRegisterResponseCannotReviveTheProvider
// pins the interleave that deleting the rows does not survive on its own.
//
// Registration entry points are not serialized against the refresh loop, so a
// register whose payload was built while the CLI was still acceptable can still
// be in flight when the downgrade is confirmed and the provider demoted. Its
// response lands afterwards describing the provider as healthy, and every apply
// path treats a response as fresh truth — so the runtime goes back into
// runtimeIndex while the server's upsert puts the row back online. The demotion
// is undone by the answer to a question asked before it, and the too-old CLI
// keeps taking work until the next refresh tick notices.
//
// Both sides now run under d.mu, which gives them a total order: either the
// demotion wins and the late response is rejected, or the apply wins and the
// demotion removes the row it just added. This drives the first ordering.
func TestDemoteBelowMinimumRuntimes_LateRegisterResponseCannotReviveTheProvider(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	// A register sent while codex was still acceptable. Its response is held
	// here and applied after the demotion, which is what "in flight across the
	// verdict" means for a deterministic test.
	inFlight := []map[string]string{{"name": "Codex", "type": "codex", "version": "9.9.9", "status": "online"}}
	staleResp, err := d.registerBuiltinRuntimesForWorkspaceLocked(context.Background(), "ws-1", inFlight)
	if err != nil {
		t.Fatalf("in-flight register: %v", err)
	}

	stubBelowMinimumAt(t, "0.0.1")
	fx.setProbeVersion("0.0.1")
	d.refreshAgentVersions(context.Background())
	if got := registeredProviders(t, d, "ws-1"); len(got) != 0 {
		t.Fatalf("providers after downgrade = %v, want none (demotion precondition)", got)
	}
	deregsBefore := fx.deregisteredCount()

	// The older register's response finally lands.
	newIDs, revived, ok := d.mergeBuiltinRegisterResponse("ws-1", staleResp)
	if !ok {
		t.Fatal("merge of the late register response failed")
	}
	if len(newIDs) != 0 {
		t.Errorf("late response added runtimes %v; a provider confirmed below the minimum must not come "+
			"back through a reply to a register sent before the verdict", newIDs)
	}
	if got := registeredProviders(t, d, "ws-1"); len(got) != 0 {
		t.Errorf("providers after the late response = %v, want none", got)
	}
	// Local rejection is only half the fix: the server upserted that row back to
	// online, so it would keep listing the runtime and routing work to a daemon
	// that no longer tracks it — those tasks sit unclaimed until the stale sweep.
	if len(revived.ids) == 0 {
		t.Fatal("late response was rejected locally but reported no runtime ID to deregister, so the " +
			"server would still believe the runtime is online")
	}
	d.deregisterRevivedRuntimes(context.Background(), "ws-1", revived)
	if got := fx.deregisteredCount(); got <= deregsBefore {
		t.Errorf("deregister calls %d -> %d; the revived server row was never taken offline", deregsBefore, got)
	}

	// The provider must still look missing so converge can restore it once the
	// CLI is upgraded again — the demotion is a hold, not a tombstone.
	if missing := d.providersMissingRuntimes(); len(missing) != 1 || missing[0] != "codex" {
		t.Errorf("providersMissingRuntimes = %v, want [codex]", missing)
	}
}

// TestDemoteBelowMinimumRuntimes_LateAuthoritativeResponseCannotReviveTheProvider
// is the same interleave through the other apply flavor. The drift and
// runtime-gone paths use applyRegisterResponseInPlace, which treats the response
// as authoritative for the whole workspace, so it has to reject a demoted
// provider too — and hand back its ID, because those callers deregister
// droppedIDs.
func TestDemoteBelowMinimumRuntimes_LateAuthoritativeResponseCannotReviveTheProvider(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	inFlight := []map[string]string{{"name": "Codex", "type": "codex", "version": "9.9.9", "status": "online"}}
	staleResp, err := d.registerBuiltinRuntimesForWorkspaceLocked(context.Background(), "ws-1", inFlight)
	if err != nil {
		t.Fatalf("in-flight register: %v", err)
	}

	stubBelowMinimumAt(t, "0.0.1")
	fx.setProbeVersion("0.0.1")
	d.refreshAgentVersions(context.Background())
	if got := registeredProviders(t, d, "ws-1"); len(got) != 0 {
		t.Fatalf("providers after downgrade = %v, want none (demotion precondition)", got)
	}

	newIDs, droppedIDs, ok := d.applyRegisterResponseInPlace("ws-1", staleResp, "", nil)
	if !ok {
		t.Fatal("apply of the late register response failed")
	}
	if len(newIDs) != 0 {
		t.Errorf("late authoritative response added runtimes %v, want none", newIDs)
	}
	if got := registeredProviders(t, d, "ws-1"); len(got) != 0 {
		t.Errorf("providers after the late authoritative response = %v, want none", got)
	}
	if len(droppedIDs) == 0 {
		t.Error("demoted runtime was rejected but not reported in droppedIDs, so the caller would never " +
			"deregister the row the server brought back online")
	}
	// The version record must not claim the server holds a version for a
	// provider whose rows are on their way offline — that is exactly what makes
	// a later refresh round think this workspace is already up to date.
	d.mu.Lock()
	recorded, hasRecord := d.workspaces["ws-1"].builtinVersions["codex"]
	d.mu.Unlock()
	if hasRecord {
		t.Errorf("builtinVersions still records codex=%q after demotion; a later refresh round would "+
			"read that as the workspace being current", recorded)
	}
}

// TestDemoteBelowMinimumRuntimes_UpgradeClearsTheHold proves the demotion is
// released by the probe round that finds the CLI acceptable again. Without that,
// the guard protecting the demotion would reject the very register converge
// makes to bring the provider back, and the runtime would never return.
func TestDemoteBelowMinimumRuntimes_UpgradeClearsTheHold(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	stubBelowMinimumAt(t, "0.0.1")
	fx.setProbeVersion("0.0.1")
	d.refreshAgentVersions(context.Background())
	if got := registeredProviders(t, d, "ws-1"); len(got) != 0 {
		t.Fatalf("providers after downgrade = %v, want none (demotion precondition)", got)
	}

	// The user upgrades again; converge re-probes and registers.
	fx.setProbeVersion("10.0.0")
	d.convergeRuntimeRegistrations(context.Background())

	for _, workspaceID := range []string{"ws-1", "ws-2"} {
		if got := registeredProviders(t, d, workspaceID); len(got) != 1 || got[0] != "codex" {
			t.Errorf("%s providers after upgrade = %v, want [codex] — the demotion hold outlived the "+
				"downgrade and blocked recovery", workspaceID, got)
		}
	}
	if got := fx.registeredVersionFor("ws-1", "codex"); got != "10.0.0" {
		t.Errorf("ws-1 registered codex version = %q, want 10.0.0", got)
	}
}

// TestSyncWorkspacesFromAPI_FirstRegistrationRejectsDemotedProvider covers the
// third path a register response reaches local state through.
//
// A workspace seen for the first time does not go through
// applyRegisterResponseInPlace or mergeBuiltinRegisterResponse — it builds the
// workspaceState directly — so the demotion guard on those two left this one
// open. A workspace joining while a provider is held below-minimum would bring
// that provider back, on the strength of a payload probed before the verdict.
func TestSyncWorkspacesFromAPI_FirstRegistrationRejectsDemotedProvider(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	// Hold the new workspace's register in flight. Its payload was probed while
	// codex was still acceptable; the demotion lands while it waits.
	arrived := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	fx.setRegisterGate(func(workspaceID string) {
		if workspaceID != "ws-3" {
			return
		}
		once.Do(func() { close(arrived) })
		<-release
	})

	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
		WorkspaceInfo{ID: "ws-3", Name: "three"},
	)
	synced := make(chan error, 1)
	go func() { synced <- d.syncWorkspacesFromAPI(context.Background(), false) }()
	<-arrived

	// The downgrade is confirmed while ws-3's register is still in flight.
	stubBelowMinimumAt(t, "0.0.1")
	fx.setProbeVersion("0.0.1")
	d.refreshAgentVersions(context.Background())
	if got := registeredProviders(t, d, "ws-1"); len(got) != 0 {
		t.Fatalf("providers for ws-1 after downgrade = %v, want none (demotion precondition)", got)
	}
	deregsBefore := fx.deregisteredCount()

	close(release)
	if err := <-synced; err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if got := registeredProviders(t, d, "ws-3"); len(got) != 0 {
		t.Errorf("ws-3 providers = %v, want none — a workspace joining while codex is held "+
			"below the minimum must not be how it comes back online", got)
	}
	if got := fx.deregisteredCount(); got <= deregsBefore {
		t.Errorf("deregister calls %d -> %d; the row the server created for ws-3 was left online, so it "+
			"would keep routing work to a runtime the daemon does not track", deregsBefore, got)
	}
	// The version record must not claim ws-3 is current for a provider it
	// refused, or the next refresh round would skip repairing it.
	d.mu.Lock()
	_, recorded := d.workspaces["ws-3"].builtinVersions["codex"]
	d.mu.Unlock()
	if recorded {
		t.Error("ws-3 recorded a codex version for a provider whose rows it rejected")
	}
}

// TestClearProviderDemotions_KeepsHoldWhenEvidencePredatesTheVerdict pins the
// ordering rule that d.mu alone cannot supply.
//
// Four callers run probe rounds concurrently and version sampling happens off
// the lock, so the round that finishes last is not necessarily the one that
// looked last. An OK sample taken before a downgrade must not release the hold
// that downgrade established — once the hold is gone, a stale register response
// passes every other guard.
func TestClearProviderDemotions_KeepsHoldWhenEvidencePredatesTheVerdict(t *testing.T) {
	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), runtimeIndex: map[string]Runtime{}}

	// A round that started before the verdict.
	sampledAfter := d.demotionSeqSnapshot()

	d.mu.Lock()
	d.markProvidersDemotedLocked(map[string]runtimeVerdict{"codex": {reason: "below minimum supported version: 0.0.1"}})
	d.mu.Unlock()

	d.clearProviderDemotions([]string{"codex"}, sampledAfter)
	d.mu.Lock()
	stillHeld := d.providerDemotedLocked("codex")
	d.mu.Unlock()
	if !stillHeld {
		t.Fatal("an OK sample taken before the downgrade released the hold; a stale register response " +
			"would now walk through every remaining guard")
	}

	// A round that starts after the verdict carries newer evidence and releases.
	d.clearProviderDemotions([]string{"codex"}, d.demotionSeqSnapshot())
	d.mu.Lock()
	stillHeld = d.providerDemotedLocked("codex")
	d.mu.Unlock()
	if stillHeld {
		t.Error("a probe round that started after the verdict must release the hold, or the provider " +
			"could never recover")
	}
}

// TestDetectBuiltinRuntimes_StaleOKRoundDoesNotReleaseANewerHold drives the same
// ordering through the real probe round rather than the counter directly: the
// round samples an acceptable version, blocks, and only finishes after a
// downgrade has been confirmed.
func TestDetectBuiltinRuntimes_StaleOKRoundDoesNotReleaseANewerHold(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	// The stub reads probeVersion BEFORE consulting this hook, so the round has
	// already sampled the acceptable version by the time it blocks here.
	arrived := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	fx.setProbeErr(func(string, int) error {
		once.Do(func() {
			close(arrived)
			<-release
		})
		return nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		d.detectBuiltinRuntimes(context.Background())
	}()
	<-arrived

	// The downgrade is confirmed while that round is still in flight.
	d.mu.Lock()
	d.markProvidersDemotedLocked(map[string]runtimeVerdict{"codex": {reason: "below minimum supported version: 0.0.1"}})
	d.mu.Unlock()

	close(release)
	<-done

	d.mu.Lock()
	stillHeld := d.providerDemotedLocked("codex")
	d.mu.Unlock()
	if !stillHeld {
		t.Error("a probe round that sampled before the downgrade cleared the newer hold — ordering the " +
			"map writes is not enough when the evidence itself is sampled off the lock")
	}
}

// stubBelowMinimumForProvider rejects ONE provider at a given version, leaving
// every other provider acceptable at the same version. Round-4's findings are
// about mixed rounds — one CLI too old while its neighbours are fine — which the
// single-provider stub cannot express.
func stubBelowMinimumForProvider(t *testing.T, provider, rejected string) {
	t.Helper()
	orig := checkAgentMinVersion
	t.Cleanup(func() { checkAgentMinVersion = orig })
	checkAgentMinVersion = func(name, version string) error {
		if name == provider && version == rejected {
			return &agent.BelowMinimumError{AgentType: name, Detected: version, Minimum: "1.0.0"}
		}
		return nil
	}
}

// TestDemoteBelowMinimumRuntimes_CleanupCannotOutliveANewerRecovery closes the
// TOCTOU that a tracking re-check could only narrow.
//
// A deregistration is decided under d.mu and issued after releasing it. The
// re-check asked "does the daemon still track this row?" immediately before the
// request, but the recovery can just as easily complete in the gap BETWEEN the
// check and the request: the register brings the row back — under the same ID,
// because the server upserts — and the older Deregister lands on top of it. The
// daemon then tracks and heartbeats a runtime the server has marked offline,
// which is the direction that strands work silently: the server will not route
// to it, and neither side notices the disagreement.
//
// A point-in-time check cannot fix that; only putting the request inside the
// order can. The workspace's register lock now spans send → apply → cleanup, so
// a recovery for the same workspace cannot start while an older cleanup for it
// is still in flight.
func TestDemoteBelowMinimumRuntimes_CleanupCannotOutliveANewerRecovery(t *testing.T) {
	// Stable IDs from the first registration, because a cleanup can only undo a
	// recovery when the two name the same server-side row.
	fx := newVersionRefreshFixtureWith(t, func(fx *batchFixture) { fx.enableStableRuntimeIDs() })
	d := fx.daemon
	codexID := fx.runtimeIDFor("ws-1", "codex")
	if codexID == "" {
		t.Fatal("no server-side runtime ID recorded for ws-1/codex")
	}
	if !fx.runtimeOnline(codexID) {
		t.Fatalf("runtime %s is not online after registration", codexID)
	}

	// Hold the demotion's Deregister in flight. Only the first call blocks; the
	// sibling workspace's cleanup runs normally once released.
	deregisterStarted := make(chan struct{})
	releaseDeregister := make(chan struct{})
	var gateOnce sync.Once
	fx.setDeregisterGate(func([]string) {
		first := false
		gateOnce.Do(func() { first = true })
		if !first {
			return
		}
		close(deregisterStarted)
		<-releaseDeregister
	})
	// Signals that a register for ws-1 reached the server.
	recovered := make(chan struct{})
	var regOnce sync.Once
	fx.setRegisterGate(func(workspaceID string) {
		if workspaceID != "ws-1" {
			return
		}
		regOnce.Do(func() { close(recovered) })
	})

	// codex downgrades below the minimum; the refresh round demotes it and
	// blocks in the cleanup.
	stubBelowMinimumAt(t, "0.0.1")
	fx.setProbeVersion("0.0.1")
	refreshDone := make(chan struct{})
	go func() {
		defer close(refreshDone)
		d.refreshAgentVersions(context.Background())
	}()
	<-deregisterStarted

	// The user upgrades again while that Deregister is still in flight, and
	// converge tries to bring the provider back.
	fx.setProbeVersion("10.0.0")
	convergeDone := make(chan struct{})
	go func() {
		defer close(convergeDone)
		d.convergeRuntimeRegistrations(context.Background())
	}()

	select {
	case <-recovered:
		t.Error("a recovery register for ws-1 reached the server while an older Deregister for the same " +
			"workspace was still in flight; the cleanup is outside the registration order, so it can land " +
			"after the recovery and knock the restored runtime offline")
	case <-time.After(250 * time.Millisecond):
		// Expected: the recovery is queued behind the cleanup.
	}

	close(releaseDeregister)
	<-refreshDone
	<-convergeDone

	if !fx.runtimeOnline(codexID) {
		t.Errorf("runtime %s is offline server-side after the upgrade recovered it — an older cleanup "+
			"landed on top of a newer registration, and the daemon now heartbeats a row the server will "+
			"not route work to", codexID)
	}
	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "codex" {
		t.Errorf("ws-1 providers after recovery = %v, want [codex]", got)
	}
}

// TestRegisterRuntimesForWorkspace_ReportsBelowMinimumAsNotAuthoritative pins
// the return value round 4 found being discarded.
//
// registerRuntimesForWorkspaceLocked probes, and its round can reach a
// below-minimum verdict — but its callers apply the response as AUTHORITATIVE
// for the whole workspace, so a provider absent from the payload has its rows
// dropped and deregistered. Acting on the verdict there means acting on it
// without the claim barrier (so a running task can have its runtime pulled out
// from under it) and without recording the hold (so the next in-flight response
// revives the provider and undoes it). The verdict therefore has to travel back
// as "do not treat this response as authoritative about this provider".
func TestRegisterRuntimesForWorkspace_ReportsBelowMinimumAsNotAuthoritative(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon

	stubBelowMinimumAt(t, "0.0.1")
	fx.setProbeVersion("0.0.1")

	var preserve map[string]string
	err := d.withWorkspaceRegisterLock("ws-1", func() error {
		_, _, p, err := d.registerRuntimesForWorkspaceLocked(context.Background(), "ws-1")
		preserve = p
		return err
	})
	// codex is the only provider, so its payload is empty and the register
	// short-circuits — the verdict still has to come back.
	if err != nil && !errors.Is(err, ErrNoRuntimesToRegister) {
		t.Fatalf("registerRuntimesForWorkspaceLocked: %v", err)
	}
	if _, ok := preserve["codex"]; !ok {
		t.Errorf("preserve set = %v, want codex — a caller that applies this response as authoritative "+
			"would drop and deregister the provider's runtimes outside the one path allowed to demote", preserve)
	}
}

// TestReregisterAfterRuntimeGone_LeavesBelowMinimumToTheDemotionPath is the
// behaviour that return value buys.
//
// The runtime-gone recovery reaches the same below-minimum verdict
// demoteBelowMinimumRuntimes does, but it has neither of the things that make
// the demotion safe: the claim barrier, so the rows are never pulled out from
// under a running task, and the seq-stamped hold, so a register sent before the
// verdict cannot revive the provider. Dropping the rows here left no record that
// the verdict had been reached, and the next in-flight response undid it.
//
// So it preserves instead. The verdict is not lost — the refresh tick that owns
// the demotion still takes the provider offline, with both guards in place.
func TestReregisterAfterRuntimeGone_LeavesBelowMinimumToTheDemotionPath(t *testing.T) {
	fx := newBatchFixture(t)
	// Stable IDs, so re-registering the surviving provider returns the row it
	// already has — what UpsertAgentRuntime does. Without it every re-register
	// looks like an ID rotation and the deregister count says nothing about the
	// demotion.
	fx.enableStableRuntimeIDs()
	d := fx.daemon
	agents := map[string]AgentEntry{
		"claude": {Path: "/fake/claude"},
		"codex":  {Path: "/fake/codex"},
	}
	d.cfg.Agents = agents
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	stubAgentProbe(t, agents)
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}
	if got := registeredProviders(t, d, "ws-1"); len(got) != 2 {
		t.Fatalf("providers after registration = %v, want claude and codex", got)
	}

	// codex is downgraded; claude is fine at the same version.
	stubBelowMinimumForProvider(t, "codex", "0.0.1")
	fx.setProbeVersion("0.0.1")
	deregsBefore := fx.deregisteredCount()

	if err := d.reregisterWorkspaceAfterRuntimeGone(context.Background(), "ws-1"); err != nil {
		t.Fatalf("reregisterWorkspaceAfterRuntimeGone: %v", err)
	}

	if got := registeredProviders(t, d, "ws-1"); len(got) != 2 {
		t.Errorf("providers after runtime_gone recovery = %v, want claude and codex kept — this path "+
			"reached the below-minimum verdict without the claim barrier or the hold that make acting on "+
			"it safe, so it must leave the demotion to the refresh tick", got)
	}
	if got := fx.deregisteredCount(); got != deregsBefore {
		t.Errorf("deregister calls %d -> %d; the recovery took a runtime offline on a verdict it may not act on",
			deregsBefore, got)
	}
	d.mu.Lock()
	held := d.providerDemotedLocked("codex")
	d.mu.Unlock()
	if held {
		t.Error("the recovery recorded a demotion hold; the hold and the rows must be taken by the same " +
			"barrier-protected path, or recovery ordering has two owners")
	}

	// The verdict is deferred, not dropped: the owner still acts on it.
	d.refreshAgentVersions(context.Background())
	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "claude" {
		t.Errorf("providers after the refresh tick = %v, want [claude] — deferring the demotion must not "+
			"lose it", got)
	}
}

// TestProfileDriftRefresh_LeavesBelowMinimumToTheDemotionPath is the same
// deferral on the second authoritative caller.
//
// The drift path is the sharper-edged of the two: it Deregisters whatever the
// authoritative apply drops, so acting on a below-minimum verdict here does not
// just untrack the runtime locally, it takes the row offline server-side —
// outside the claim barrier, so a task executing on that runtime has it pulled
// away mid-flight, and with no hold recorded, so the next in-flight response
// puts it back anyway.
func TestProfileDriftRefresh_LeavesBelowMinimumToTheDemotionPath(t *testing.T) {
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	d := fx.daemon
	agents := map[string]AgentEntry{
		"claude": {Path: "/fake/claude"},
		"codex":  {Path: "/fake/codex"},
	}
	d.cfg.Agents = agents
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	stubAgentProbe(t, agents)
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	// The drift: a profile appears server-side. Its command_name is blank, so it
	// changes the profile signature without entering the payload — the register
	// carries built-ins only, and its response is applied as authoritative.
	fx.mu.Lock()
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Broken",
		ProtocolFamily: "codex", Visibility: "workspace", Enabled: true,
	}}
	fx.mu.Unlock()
	stubBelowMinimumForProvider(t, "codex", "0.0.1")
	fx.setProbeVersion("0.0.1")
	deregsBefore := fx.deregisteredCount()

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	if got := registeredProviders(t, d, "ws-1"); len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Errorf("providers after drift refresh = %v, want [claude codex] — the drift path may not act on "+
			"a below-minimum verdict it cannot record or barrier-protect", got)
	}
	if got := fx.deregisteredCount(); got != deregsBefore {
		t.Errorf("deregister calls %d -> %d; the drift refresh took a runtime offline on a verdict it may "+
			"not act on", deregsBefore, got)
	}

	// Deferred, not dropped.
	d.refreshAgentVersions(context.Background())
	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "claude" {
		t.Errorf("providers after the refresh tick = %v, want [claude]", got)
	}
}
