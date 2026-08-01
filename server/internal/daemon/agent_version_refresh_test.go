package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	if _, belowMin := d.detectBuiltinRuntimes(context.Background()); len(belowMin) != 0 {
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
			return errors.New("version 0.0.1 is below minimum required 1.0.0")
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
		d.demoteBelowMinimumRuntimes(context.Background(), map[string]string{"codex": "0.0.1"})
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

// TestRefreshAgentVersions_RetriesAfterRegisterFailure closes the hole the
// version cache creates: setAgentVersion has already moved on by the time the
// register call fails, so the next round sees no change. Without a pending
// record a transient 5xx would leave the server displaying a version the daemon
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
	if got := d.pendingAgentVersionsSnapshot(); got["codex"] != "10.0.0" {
		t.Fatalf("pending = %v, want codex 10.0.0 held for retry or the version change is lost", got)
	}

	fx.failRegisterFor("ws-2", false)
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-2", "codex"); got != "10.0.0" {
		t.Errorf("ws-2 registered codex version = %q after the retry, want 10.0.0", got)
	}
	if got := d.pendingAgentVersionsSnapshot(); len(got) != 0 {
		t.Errorf("pending = %v, want empty once every workspace re-registered", got)
	}
}

// TestRefreshAgentVersions_PendingSurvivesAnIncompletePayload is why the pending
// record is per-provider rather than one flag.
//
// codex's new version fails to register, then codex's probe fails on the next
// round while claude's succeeds. That round registers a payload with no codex in
// it at all. A shared flag would be cleared by that success — and because
// codex's cache already holds the new version, no later round would report it as
// a change, so the server would keep showing the old version indefinitely.
func TestRefreshAgentVersions_PendingSurvivesAnIncompletePayload(t *testing.T) {
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

	// Round 1: both upgrade, the register fails.
	fx.failRegister(true)
	fx.setProbeVersion("10.0.0")
	d.refreshAgentVersions(context.Background())
	if got := d.pendingAgentVersionsSnapshot(); got["codex"] != "10.0.0" {
		t.Fatalf("pending after failed register = %v, want codex 10.0.0", got)
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
	if got := d.pendingAgentVersionsSnapshot(); got["codex"] != "10.0.0" {
		t.Fatalf("pending = %v, want codex still held: that payload never mentioned it", got)
	}

	// Round 3: codex's probe recovers. Its cache already reads 10.0.0, so
	// nothing here is a "change" — the pending record is the only reason the
	// server ever learns about it.
	fx.setProbeErr(nil)
	d.refreshAgentVersions(context.Background())

	if got := fx.registeredVersionFor("ws-1", "codex"); got != "10.0.0" {
		t.Errorf("codex registered version = %q, want 10.0.0 once its probe recovered", got)
	}
	if got := d.pendingAgentVersionsSnapshot(); len(got) != 0 {
		t.Errorf("pending = %v, want empty", got)
	}
}

// TestRefreshAgentVersions_UnreachableProviderDoesNotStormTheServer guards the
// regression the pending map invites: an entry can outlive its provider.
//
// refreshAgentAvailability is deliberately one-directional, so an uninstalled
// CLI is never dropped from the availability set — it just keeps failing its
// probe and never reappears in the payload, leaving its pending entry
// unconfirmable forever. Retrying on "anything pending" would turn that into one
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

	// The round that can still confirm claude registers once.
	d.refreshAgentVersions(context.Background())
	settled := fx.registerCallCount()
	if got := d.pendingAgentVersionsSnapshot(); got["codex"] != "10.0.0" {
		t.Fatalf("pending = %v, want codex still held", got)
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
