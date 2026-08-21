package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// stubAgentProbe replaces CLI discovery for the duration of a test. The returned
// setter swaps in the next probe result, simulating the user installing or
// uninstalling a CLI while the daemon runs.
//
// Goroutine-safe: agentDiscoveryLoop calls the probe from its own goroutine
// while the test body swaps the result.
func stubAgentProbe(t *testing.T, initial map[string]AgentEntry) func(map[string]AgentEntry) {
	t.Helper()
	orig := probeAgentCLIs
	t.Cleanup(func() { probeAgentCLIs = orig })
	var (
		mu      sync.Mutex
		current = initial
	)
	probeAgentCLIs = func() map[string]AgentEntry {
		mu.Lock()
		defer mu.Unlock()
		// Copy so the caller can iterate without holding the lock.
		out := make(map[string]AgentEntry, len(current))
		for name, entry := range current {
			out[name] = entry
		}
		return out
	}
	return func(next map[string]AgentEntry) {
		mu.Lock()
		defer mu.Unlock()
		current = next
	}
}

// registeredProviders returns the built-in providers the daemon currently holds
// a runtime for in a workspace, as the daemon itself sees them.
func registeredProviders(t *testing.T, d *Daemon, workspaceID string) []string {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		return nil
	}
	var out []string
	for _, id := range ws.runtimeIDs {
		rt, found := d.runtimeIndex[id]
		if !found || rt.ProfileID != "" {
			continue
		}
		out = append(out, rt.Provider)
	}
	sort.Strings(out)
	return out
}

// TestDiscovery_RegistersCLIInstalledAfterStartup is the MUL-5439 regression
// (GH #6077): the availability set used to be built once in LoadConfig, so a CLI
// installed while the daemon was running never registered — and on Desktop,
// quitting the app does not restart the daemon, so the user had no way to
// recover short of an explicit daemon restart.
func TestDiscovery_RegistersCLIInstalledAfterStartup(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})

	setProbe := stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}
	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("initial providers = %v, want [codex]", got)
	}

	// The user now installs Antigravity.
	setProbe(map[string]AgentEntry{
		"codex":       {Path: "/fake/codex"},
		"antigravity": {Path: "/fake/agy"},
	})

	gained := d.refreshAgentAvailability()
	if len(gained) != 1 || gained[0] != "antigravity" {
		t.Fatalf("gained = %v, want [antigravity]", gained)
	}
	d.convergeRuntimeRegistrations(context.Background())

	got := registeredProviders(t, d, "ws-1")
	if len(got) != 2 || got[0] != "antigravity" || got[1] != "codex" {
		t.Errorf("providers after converge = %v, want [antigravity codex]", got)
	}
	if missing := d.providersMissingRuntimes(); missing != nil {
		t.Errorf("still missing %v after a successful converge", missing)
	}
}

// TestDiscovery_RetriesAfterVersionProbeFailure is the review's P1: publishing a
// provider into the availability set must not be mistaken for "handled". A
// version probe that fails the first time (timeout, CLI busy) has to be retried
// on a later round, or the user is back to restarting the daemon.
func TestDiscovery_RetriesAfterVersionProbeFailure(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	setProbe := stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	// agy is installed, but every `agy --version` in this round fails.
	fx.setProbeErr(func(path string, _ int) error {
		if path == "/fake/agy" {
			return errors.New("boom")
		}
		return nil
	})
	setProbe(map[string]AgentEntry{
		"codex":       {Path: "/fake/codex"},
		"antigravity": {Path: "/fake/agy"},
	})

	d.refreshAgentAvailability()
	d.convergeRuntimeRegistrations(context.Background())
	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 {
		t.Fatalf("providers = %v, want only codex while the version probe fails", got)
	}
	missing := d.providersMissingRuntimes()
	if len(missing) != 1 || missing[0] != "antigravity" {
		t.Fatalf("missing = %v, want [antigravity] so a later round retries", missing)
	}
	// The failure is visible, not just retried.
	if _, ok := d.skippedAgentsSnapshot()["antigravity"]; !ok {
		t.Error("antigravity missing from skipped_agents while its version probe fails")
	}

	// The CLI starts responding. No new discovery happens — the retry must be
	// driven by the still-missing runtime, not by a fresh "gained" event.
	fx.setProbeErr(nil)
	if gained := d.refreshAgentAvailability(); gained != nil {
		t.Fatalf("gained = %v, want none (antigravity was already discovered)", gained)
	}
	d.convergeRuntimeRegistrations(context.Background())

	got := registeredProviders(t, d, "ws-1")
	if len(got) != 2 || got[0] != "antigravity" {
		t.Errorf("providers after recovery = %v, want antigravity registered", got)
	}
	if missing := d.providersMissingRuntimes(); missing != nil {
		t.Errorf("still missing %v after recovery", missing)
	}
}

// TestDiscovery_RetriesAfterRegisterRequestFailure covers the other half of the
// same P1: the register HTTP call failing must also leave the provider pending.
func TestDiscovery_RetriesAfterRegisterRequestFailure(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	setProbe := stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	fx.failRegister(true)
	setProbe(map[string]AgentEntry{
		"codex":       {Path: "/fake/codex"},
		"antigravity": {Path: "/fake/agy"},
	})
	d.refreshAgentAvailability()
	d.convergeRuntimeRegistrations(context.Background())

	missing := d.providersMissingRuntimes()
	if len(missing) != 1 || missing[0] != "antigravity" {
		t.Fatalf("missing = %v, want [antigravity] after a failed register", missing)
	}
	// The failed register must not have corrupted what was already there.
	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("providers = %v, want codex preserved through the failure", got)
	}

	fx.failRegister(false)
	d.convergeRuntimeRegistrations(context.Background())
	if got := registeredProviders(t, d, "ws-1"); len(got) != 2 || got[0] != "antigravity" {
		t.Errorf("providers after retry = %v, want antigravity registered", got)
	}
}

// TestDiscovery_RetriesOnlyTheWorkspacesThatStillNeedIt covers a partial failure
// across workspaces: the workspace that registered must not be re-registered,
// and the one that failed must be.
func TestDiscovery_RetriesOnlyTheWorkspacesThatStillNeedIt(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)
	setProbe := stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	fx.failRegisterFor("ws-2", true)
	setProbe(map[string]AgentEntry{
		"codex":       {Path: "/fake/codex"},
		"antigravity": {Path: "/fake/agy"},
	})
	d.refreshAgentAvailability()
	d.convergeRuntimeRegistrations(context.Background())

	if got := registeredProviders(t, d, "ws-1"); len(got) != 2 {
		t.Fatalf("ws-1 providers = %v, want both registered", got)
	}
	if got := registeredProviders(t, d, "ws-2"); len(got) != 1 {
		t.Fatalf("ws-2 providers = %v, want only codex after its register failed", got)
	}

	fx.failRegisterFor("ws-2", false)
	before := fx.registerCallCount()
	d.convergeRuntimeRegistrations(context.Background())
	if got := registeredProviders(t, d, "ws-2"); len(got) != 2 {
		t.Errorf("ws-2 providers after retry = %v, want both registered", got)
	}
	if got := fx.registerCallCount() - before; got != 1 {
		t.Errorf("retry made %d register calls, want 1 (only the workspace that needed it)", got)
	}
}

// TestDiscovery_KeepsCustomRuntimeWhenProfileFetchFails is the review's second
// P1. The discovery path registers built-ins only, so its response never
// mentions custom profile runtimes. Applying that as authoritative would evict
// them from the local index and stop their heartbeats — the opposite of
// "existing runtimes are unaffected". The profile fetch failing is the sharpest
// version of the same hazard.
func TestDiscovery_KeepsCustomRuntimeWhenProfileFetchFails(t *testing.T) {
	fx := newBatchFixture(t)
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	setProbe := stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	customID := ""
	d.mu.Lock()
	sigBefore := d.workspaces["ws-1"].profileSetSig
	for _, id := range d.workspaces["ws-1"].runtimeIDs {
		if d.runtimeIndex[id].ProfileID == "prof-1" {
			customID = id
		}
	}
	d.mu.Unlock()
	if customID == "" {
		t.Fatal("custom profile runtime was never registered; fixture setup is wrong")
	}

	// The profile fetch now fails, and a newly installed CLI triggers a
	// discovery-driven registration.
	fx.failProfiles(true)
	setProbe(map[string]AgentEntry{
		"codex":       {Path: "/fake/codex"},
		"antigravity": {Path: "/fake/agy"},
	})
	d.refreshAgentAvailability()
	d.convergeRuntimeRegistrations(context.Background())

	d.mu.Lock()
	_, stillIndexed := d.runtimeIndex[customID]
	var stillWatched bool
	for _, id := range d.workspaces["ws-1"].runtimeIDs {
		if id == customID {
			stillWatched = true
		}
	}
	sigAfter := d.workspaces["ws-1"].profileSetSig
	d.mu.Unlock()

	if !stillIndexed {
		t.Error("custom profile runtime was evicted from runtimeIndex by a builtins-only response")
	}
	if !stillWatched {
		t.Error("custom profile runtime was dropped from the workspace's runtime set (heartbeats would stop)")
	}
	if sigAfter != sigBefore {
		t.Errorf("profileSetSig changed %q -> %q; discovery must not touch it", sigBefore, sigAfter)
	}
	if got := registeredProviders(t, d, "ws-1"); len(got) != 2 || got[0] != "antigravity" {
		t.Errorf("providers = %v, want the new built-in registered alongside codex", got)
	}
}

// TestDiscovery_DoesNotMaskAProfileDisabledConcurrently is the second review
// round's P1. Discovery used to register through the profile-appending path and
// cache the resulting signature. If a user disabled a custom profile at the same
// moment a new CLI was discovered, the additive merge correctly kept the old
// runtime ID but recorded the post-disable signature — so
// refreshWorkspaceRuntimeProfiles saw "no drift", returned early, and the
// disabled runtime stayed tracked and heartbeating forever.
func TestDiscovery_DoesNotMaskAProfileDisabledConcurrently(t *testing.T) {
	fx := newBatchFixture(t)
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	setProbe := stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	customID := ""
	d.mu.Lock()
	for _, id := range d.workspaces["ws-1"].runtimeIDs {
		if d.runtimeIndex[id].ProfileID == "prof-1" {
			customID = id
		}
	}
	d.mu.Unlock()
	if customID == "" {
		t.Fatal("custom profile runtime was never registered; fixture setup is wrong")
	}

	// The user disables the profile at the same time as installing a new CLI.
	fx.profiles["ws-1"] = nil
	setProbe(map[string]AgentEntry{
		"codex":       {Path: "/fake/codex"},
		"antigravity": {Path: "/fake/agy"},
	})
	d.refreshAgentAvailability()
	d.convergeRuntimeRegistrations(context.Background())

	// Discovery must not have decided the profile set is converged.
	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	d.mu.Lock()
	_, stillIndexed := d.runtimeIndex[customID]
	var stillWatched bool
	for _, id := range d.workspaces["ws-1"].runtimeIDs {
		if id == customID {
			stillWatched = true
		}
	}
	d.mu.Unlock()

	if stillWatched || stillIndexed {
		t.Errorf("disabled custom runtime %s remained tracked (indexed=%v watched=%v); discovery masked the drift",
			customID, stillIndexed, stillWatched)
	}
	if got := registeredProviders(t, d, "ws-1"); len(got) == 0 {
		t.Error("built-in runtimes were lost while converging the disabled profile")
	}
}

// TestDiscovery_BelowMinVersionRecoversAfterUpgrade covers the review's ask that
// a version-gated provider be re-checked rather than written off, so an in-place
// CLI upgrade brings it online without a restart.
func TestDiscovery_BelowMinVersionRecoversAfterUpgrade(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	setProbe := stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	tooOld := true
	origCheck := checkAgentMinVersion
	t.Cleanup(func() { checkAgentMinVersion = origCheck })
	checkAgentMinVersion = func(provider, _ string) error {
		if provider == "antigravity" && tooOld {
			return &agent.BelowMinimumError{AgentType: provider, Detected: "1.0.0", Minimum: "1.1.0"}
		}
		return nil
	}

	setProbe(map[string]AgentEntry{
		"codex":       {Path: "/fake/codex"},
		"antigravity": {Path: "/fake/agy"},
	})
	d.refreshAgentAvailability()
	d.convergeRuntimeRegistrations(context.Background())

	if got := registeredProviders(t, d, "ws-1"); len(got) != 1 {
		t.Fatalf("providers = %v, want antigravity rejected while below minimum", got)
	}
	reason, ok := d.skippedAgentsSnapshot()["antigravity"]
	if !ok || reason == "" {
		t.Error("below-minimum antigravity should be reported in skipped_agents with a reason")
	}

	// User upgrades the CLI in place.
	tooOld = false
	d.convergeRuntimeRegistrations(context.Background())
	if got := registeredProviders(t, d, "ws-1"); len(got) != 2 || got[0] != "antigravity" {
		t.Errorf("providers after upgrade = %v, want antigravity registered", got)
	}
	if got := d.skippedAgentsSnapshot(); len(got) != 0 {
		t.Errorf("skipped agents = %v after recovery, want empty", got)
	}
}

// TestDiscovery_NoopWhenNothingNew keeps the loop cheap in the steady state: an
// unchanged, fully registered set must not re-register or re-probe.
func TestDiscovery_NoopWhenNothingNew(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}
	registersBefore := fx.registerCallCount()
	probesBefore := fx.probeCount("/fake/codex")

	if gained := d.refreshAgentAvailability(); gained != nil {
		t.Fatalf("gained = %v, want none", gained)
	}
	if missing := d.providersMissingRuntimes(); missing != nil {
		t.Fatalf("missing = %v, want none in the steady state", missing)
	}
	if got := fx.registerCallCount(); got != registersBefore {
		t.Errorf("register calls went %d -> %d, want no change", registersBefore, got)
	}
	if got := fx.probeCount("/fake/codex"); got != probesBefore {
		t.Errorf("version probes went %d -> %d; the steady state must not re-probe", probesBefore, got)
	}
}

// TestDiscovery_KeepsProviderThatStoppedResolving pins the one-directional
// contract. A provider vanishing from a probe is usually an environment
// difference or a version manager mid-upgrade, not an uninstall, so dropping it
// would tear down a runtime that may be executing a task.
func TestDiscovery_KeepsProviderThatStoppedResolving(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{
		"codex":  {Path: "/fake/codex"},
		"claude": {Path: "/fake/claude"},
	}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	setProbe := stubAgentProbe(t, map[string]AgentEntry{
		"codex":  {Path: "/fake/codex"},
		"claude": {Path: "/fake/claude"},
	})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}
	before := fx.registerCallCount()

	setProbe(map[string]AgentEntry{"codex": {Path: "/fake/codex"}})

	if gained := d.refreshAgentAvailability(); gained != nil {
		t.Fatalf("gained = %v, want none", gained)
	}
	if _, ok := d.agents()["claude"]; !ok {
		t.Error("claude was dropped from the availability set; refresh must be additive only")
	}
	if missing := d.providersMissingRuntimes(); missing != nil {
		t.Errorf("missing = %v, want none — a lost provider must not trigger work", missing)
	}
	if after := fx.registerCallCount(); after != before {
		t.Errorf("register calls went %d -> %d, want no change on a lost provider", before, after)
	}
}

// TestDiscovery_GainWithNoWorkspacesStillPublishes covers a bootstrap daemon:
// the provider must join the availability set (so /health and the first
// registration see it) even when there is no workspace to register against.
func TestDiscovery_GainWithNoWorkspacesStillPublishes(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{}
	stubAgentProbe(t, map[string]AgentEntry{"antigravity": {Path: "/fake/agy"}})

	gained := d.refreshAgentAvailability()
	if len(gained) != 1 || gained[0] != "antigravity" {
		t.Fatalf("gained = %v, want [antigravity]", gained)
	}
	if _, ok := d.agents()["antigravity"]; !ok {
		t.Error("antigravity missing from the availability set")
	}
	if missing := d.providersMissingRuntimes(); missing != nil {
		t.Errorf("missing = %v, want none with no tracked workspaces", missing)
	}
	if got := fx.registerCallCount(); got != 0 {
		t.Errorf("made %d register calls with no tracked workspaces, want 0", got)
	}
}

// TestHealth_ReportsSkippedAgents covers the diagnostic half of MUL-5439: a CLI
// that IS installed but gets dropped at registration used to be
// indistinguishable from a CLI that is not installed — both produced no runtime.
func TestHealth_ReportsSkippedAgents(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{
		"codex":       {Path: "/fake/codex"},
		"antigravity": {Path: "/fake/agy"},
	}
	fx.setProbeErr(func(path string, _ int) error {
		if path == "/fake/agy" {
			return context.DeadlineExceeded
		}
		return nil
	})

	d.detectBuiltinRuntimes(context.Background())

	rec := httptest.NewRecorder()
	d.healthHandler(time.Now())(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	reason, ok := resp.SkippedAgents["antigravity"]
	if !ok {
		t.Fatalf("skipped_agents = %v, want an antigravity entry", resp.SkippedAgents)
	}
	if reason == "" {
		t.Error("antigravity skip reason is empty; the UI needs something to show")
	}
	if _, ok := resp.SkippedAgents["codex"]; ok {
		t.Error("codex registered successfully but is listed as skipped")
	}

	// A later round where the CLI works must clear the entry, not accumulate.
	fx.setProbeErr(nil)
	d.detectBuiltinRuntimes(context.Background())
	if got := d.skippedAgentsSnapshot(); len(got) != 0 {
		t.Errorf("skipped agents = %v after a clean round, want empty", got)
	}
}

// TestMergeBuiltinRegisterResponse_ReplacesRotatedRuntimeID guards the one case
// the additive merge still has to handle destructively: the server returning a
// new ID for a runtime the workspace already had must swap, not duplicate, or
// the daemon would run two heartbeat goroutines for one runtime.
func TestMergeBuiltinRegisterResponse_ReplacesRotatedRuntimeID(t *testing.T) {
	d := freshDaemon("http://localhost:0")
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-old"}, "", nil, nil)
	d.runtimeIndex["rt-old"] = Runtime{ID: "rt-old", Provider: "codex"}

	newIDs, _, ok := d.mergeBuiltinRegisterResponse("ws-1", &RegisterResponse{
		Runtimes: []Runtime{{ID: "rt-new", Provider: "codex"}},
	})
	if !ok {
		t.Fatal("merge reported the workspace as untracked")
	}
	if len(newIDs) != 1 || newIDs[0] != "rt-new" {
		t.Fatalf("newIDs = %v, want [rt-new]", newIDs)
	}
	if got := d.workspaces["ws-1"].runtimeIDs; len(got) != 1 || got[0] != "rt-new" {
		t.Errorf("runtimeIDs = %v, want [rt-new] (rotated, not duplicated)", got)
	}
	if _, stale := d.runtimeIndex["rt-old"]; stale {
		t.Error("rt-old left in runtimeIndex; its heartbeat goroutine would leak")
	}
}

// TestMergeBuiltinRegisterResponse_IgnoresCustomProfileRuntimes pins the
// invariant guard: only the drift path may introduce a custom profile runtime, so
// a profile-bearing entry arriving here must be ignored rather than adopted.
func TestMergeBuiltinRegisterResponse_IgnoresCustomProfileRuntimes(t *testing.T) {
	d := freshDaemon("http://localhost:0")
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

	newIDs, _, ok := d.mergeBuiltinRegisterResponse("ws-1", &RegisterResponse{
		Runtimes: []Runtime{
			{ID: "rt-builtin", Provider: "codex"},
			{ID: "rt-custom", Provider: "codex", ProfileID: "prof-1"},
		},
	})
	if !ok {
		t.Fatal("merge reported the workspace as untracked")
	}
	if len(newIDs) != 1 || newIDs[0] != "rt-builtin" {
		t.Errorf("newIDs = %v, want only the built-in runtime", newIDs)
	}
	if _, adopted := d.runtimeIndex["rt-custom"]; adopted {
		t.Error("custom profile runtime was adopted by the discovery merge")
	}
}

// TestMergeBuiltinRegisterResponse_NeverTouchesProfileSignature is the direct
// unit-level pin for the second review round's P1.
func TestMergeBuiltinRegisterResponse_NeverTouchesProfileSignature(t *testing.T) {
	d := freshDaemon("http://localhost:0")
	ws := newWorkspaceState("ws-1", nil, "", nil, nil)
	ws.profileSetSig = "sig-before"
	d.workspaces["ws-1"] = ws

	if _, _, ok := d.mergeBuiltinRegisterResponse("ws-1", &RegisterResponse{
		Runtimes: []Runtime{{ID: "rt-1", Provider: "codex"}},
	}); !ok {
		t.Fatal("merge reported the workspace as untracked")
	}
	if got := d.workspaces["ws-1"].profileSetSig; got != "sig-before" {
		t.Errorf("profileSetSig = %q, want it untouched at sig-before", got)
	}
}

// TestAgentDiscoveryLoop_PicksUpInstallWithoutRestart covers the loop wiring:
// the fix is only useful if something calls the convergence on a schedule.
func TestAgentDiscoveryLoop_PicksUpInstallWithoutRestart(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	setProbe := stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	origInterval := agentDiscoveryInterval
	agentDiscoveryInterval = 5 * time.Millisecond
	t.Cleanup(func() { agentDiscoveryInterval = origInterval })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	// Stop the loop and WAIT for it before the test returns: the fixture's
	// t.Cleanup restores the global version-probe stub, which would otherwise
	// race a probe still in flight inside the loop goroutine.
	defer func() {
		cancel()
		<-done
	}()
	go func() {
		defer close(done)
		d.agentDiscoveryLoop(ctx)
	}()

	setProbe(map[string]AgentEntry{
		"codex":       {Path: "/fake/codex"},
		"antigravity": {Path: "/fake/agy"},
	})

	deadline := time.Now().Add(3 * time.Second)
	for {
		got := registeredProviders(t, d, "ws-1")
		if len(got) == 2 && got[0] == "antigravity" && got[1] == "codex" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("discovery loop never registered the newly installed CLI; providers = %v", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestAgentDiscoveryLoop_BacksOffStuckProvider keeps a permanently unregisterable
// provider (below minimum version forever, --version always broken) from turning
// the loop into a version-probe and register-call busy loop.
func TestAgentDiscoveryLoop_BacksOffStuckProvider(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	setProbe := stubAgentProbe(t, map[string]AgentEntry{"codex": {Path: "/fake/codex"}})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}
	fx.setProbeErr(func(path string, _ int) error {
		if path == "/fake/agy" {
			return errors.New("permanently broken")
		}
		return nil
	})

	origInterval, origMax := agentDiscoveryInterval, agentConvergeMaxBackoff
	origRetryDelay := runtimeVersionProbeRetryDelay
	agentDiscoveryInterval = 2 * time.Millisecond
	agentConvergeMaxBackoff = 50 * time.Millisecond
	// Each convergence version-probes the stuck provider up to
	// runtimeVersionProbeAttempts times; keep that budget short so the
	// observation window below measures backoff rather than probe latency.
	runtimeVersionProbeRetryDelay = time.Millisecond
	t.Cleanup(func() {
		agentDiscoveryInterval = origInterval
		agentConvergeMaxBackoff = origMax
		runtimeVersionProbeRetryDelay = origRetryDelay
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	defer func() {
		cancel()
		<-done
	}()
	go func() {
		defer close(done)
		d.agentDiscoveryLoop(ctx)
	}()

	setProbe(map[string]AgentEntry{
		"codex":       {Path: "/fake/codex"},
		"antigravity": {Path: "/fake/agy"},
	})

	// Wait for the first failing convergence, then measure how many further
	// attempts happen over a window many ticks long. With backoff, attempts
	// must be far fewer than ticks.
	deadline := time.Now().Add(2 * time.Second)
	for fx.probeCount("/fake/agy") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("loop never attempted the stuck provider")
		}
		time.Sleep(2 * time.Millisecond)
	}
	start := fx.probeCount("/fake/agy")
	time.Sleep(400 * time.Millisecond) // ~200 ticks at a 2ms interval
	attempts := fx.probeCount("/fake/agy") - start
	// Ticks in the window: ~200. Convergences allowed by a 50ms cap: ~8, each
	// costing runtimeVersionProbeAttempts probes. The point is the order of
	// magnitude: retries must track the backoff, not the tick rate.
	if attempts > 40 {
		t.Errorf("stuck provider was probed %d times in ~200 ticks; backoff is not limiting retries", attempts)
	}
	if attempts == 0 {
		t.Error("stuck provider was never retried; backoff must not give up entirely")
	}
}

// TestCachedShellResolvedAgents_ReusesResultWithinTTL guards the cost side of
// running discovery periodically: resolveAgentsViaLoginShell forks the user's
// login shell and runs their rc files, so repeated probes must not repeat it.
func TestCachedShellResolvedAgents_ReusesResultWithinTTL(t *testing.T) {
	var calls int
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string {
		calls++
		return map[string]string{"agy": "/fake/agy"}
	}
	resetShellResolveCacheForTest(t)

	for i := 0; i < 5; i++ {
		if got := cachedShellResolvedAgents()["agy"]; got != "/fake/agy" {
			t.Fatalf("resolution %d = %q, want /fake/agy", i, got)
		}
	}
	if calls != 1 {
		t.Errorf("forked the login shell %d times, want 1 within the TTL", calls)
	}

	// An expired TTL must re-resolve, otherwise a CLI installed into a
	// login-shell-only PATH dir would never be discovered.
	shellResolveMu.Lock()
	shellResolvedAt = time.Now().Add(-2 * shellResolveTTL)
	shellResolveMu.Unlock()
	cachedShellResolvedAgents()
	if calls != 2 {
		t.Errorf("forked the login shell %d times, want 2 after the TTL expired", calls)
	}
}

// TestCachedShellResolvedAgents_InvalidatesOnEnvChange keeps the cache honest
// when the resolution-relevant environment changes.
func TestCachedShellResolvedAgents_InvalidatesOnEnvChange(t *testing.T) {
	var calls int
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string {
		calls++
		return map[string]string{}
	}
	resetShellResolveCacheForTest(t)

	t.Setenv("PATH", "/one")
	cachedShellResolvedAgents()
	t.Setenv("PATH", "/two")
	cachedShellResolvedAgents()
	if calls != 2 {
		t.Errorf("resolved %d times across a PATH change, want 2", calls)
	}
}

func resetShellResolveCacheForTest(t *testing.T) {
	t.Helper()
	reset := func() {
		shellResolveMu.Lock()
		shellResolveCache = nil
		shellResolveKey = ""
		shellResolvedAt = time.Time{}
		shellResolveMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}
