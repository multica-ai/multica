package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Runtime-ownership lifecycle regression tests (GH #8280 review follow-up).
//
// The first revision fixed who may serve a runtime. These tests cover the
// lifecycle around that: a standby daemon starting against a fully sibling-owned
// workspace, a partial standby retrying after the sibling dies, the claims a
// daemon must release when it stops serving a runtime, the three outcomes of the
// mixed-version peer probe, and the runtime-gone re-registration that must not
// recover a surviving runtime's tasks.
//
// Every test models two daemon PROCESSES as two Daemon values sharing one scope
// lock directory and one fake server (see runtime_owner_test.go), never as two
// goroutines on one Daemon.

// shareRuntimeOwnership gives the batch fixture's daemon runtime-owner state on a
// shared scope lock directory, which is what makes a second Daemon built on that
// same directory contend with it.
func (fx *batchFixture) shareRuntimeOwnership(lockDir string) {
	fx.daemon.scopeLocks = execenv.NewScopeLocks(lockDir)
	fx.daemon.runtimeOwners = newRuntimeOwners()
}

// isolatedProfileHome keeps the same-backend peer inventory - which is derived
// from the profile directories under HOME - empty, so a test that is not about
// peers never sees the developer machine's real profiles.
func isolatedProfileHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// newOwnedSyncDaemon builds the second daemon process: freshDaemon's full set of
// maps (the sync path touches many of them) plus runtime-owner state on the
// shared lock directory.
func newOwnedSyncDaemon(t *testing.T, lockDir, serverURL string) *Daemon {
	t.Helper()
	d := freshDaemon(serverURL)
	d.cfg = Config{
		ServerBaseURL:  serverURL,
		WorkspacesRoot: t.TempDir(),
		Agents:         map[string]AgentEntry{"codex": {Path: "/fake/codex"}},
	}
	d.scopeLocks = execenv.NewScopeLocks(lockDir)
	d.runtimeOwners = newRuntimeOwners()
	d.profileLaunchSpecs = make(map[string]profileLaunchSpec)
	return d
}

func containsString(got []string, want string) bool {
	for _, v := range got {
		if v == want {
			return true
		}
	}
	return false
}

// waitUntil polls condition until it holds or the deadline passes. It is used
// only to observe a flag the goroutine under test sets before it blocks, never
// to sample state that is still moving.
func waitUntil(deadline time.Duration, condition func() bool) bool {
	expires := time.Now().Add(deadline)
	for time.Now().Before(expires) {
		if condition() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return condition()
}

func countString(got []string, want string) int {
	n := 0
	for _, v := range got {
		if v == want {
			n++
		}
	}
	return n
}

func trackedWorkspaceCount(d *Daemon) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.workspaces)
}

// An all-standby workspace must remain active for takeover retries.
//
// The new-workspace path of syncWorkspacesFromAPI folds errAllRuntimesPeerOwned
// into "the callback succeeded", which left resp == nil for the steps after it
// (resp.Repos, resp.Runtimes). The ordinary "second daemon starts while the first
// serves every runtime" scenario therefore panicked instead of standing by.
func TestRuntimeOwnership_AllPeerOwnedWorkspaceStartsWithoutPanic(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "all-peer-owned")
	fx.shareRuntimeOwnership(lockDir)
	standby := fx.daemon
	standby.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})

	// Daemon A is already serving the workspace's only runtime.
	owner := newOwnedSyncDaemon(t, lockDir, fx.server.URL)
	if err := owner.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("owner sync: %v", err)
	}
	owned := owner.ownedRuntimeIDs()
	if len(owned) != 1 {
		t.Fatalf("owner runtimes = %v, want the workspace's only runtime", owned)
	}
	recoveriesBefore := len(fx.recoveredIDs())

	// Daemon B starts against the same machine, backend and workspace. Every
	// candidate is owned by A: this is the standby path.
	if err := standby.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("standby sync: %v", err)
	}

	if got := standby.allRuntimeIDs(); len(got) != 0 {
		t.Errorf("standby daemon registered %v, want nothing", got)
	}
	if standby.ownsTarget(runtimeOwnerTarget("ws-1", "codex", "")) {
		t.Error("standby daemon took ownership of a runtime a sibling is serving")
	}
	if got := trackedWorkspaceCount(standby); got != 0 {
		t.Errorf("standby daemon started tracking %d workspace(s), want 0", got)
	}
	// The standby is recorded, which is what keeps the workspace reconciled for
	// the day the sibling disappears.
	if !standby.workspaceHasStandbyRuntime("ws-1") {
		t.Error("the sibling-owned runtime was not recorded as standby")
	}

	// A is untouched: still the owner, still online, and B never recovered its
	// orphans (which would hard-fail A's running tasks).
	if got := owner.ownedRuntimeIDs(); len(got) != 1 || got[0] != owned[0] {
		t.Errorf("owner runtimes after the standby start = %v, want %v", got, owned)
	}
	if !fx.runtimeOnline(owned[0]) {
		t.Error("the owner's runtime was taken offline by the standby daemon")
	}
	if got := fx.recoveredIDs()[recoveriesBefore:]; len(got) != 0 {
		t.Errorf("standby daemon recovered orphans on a sibling runtime: %v", got)
	}
	if got := fx.deregisteredIDs(); len(got) != 0 {
		t.Errorf("standby daemon deregistered %v, want nothing", got)
	}
}

// A daemon serving one runtime must retry its sibling-owned candidates.
//
// A workspace whose built-in runtime this process owns while a sibling owns a
// custom profile used to be a dead end: with a runtime still tracked,
// workspaceNeedsRuntimeRecovery was false, the profile signature had not changed,
// and the standby entry had no reader - so when the sibling died, nothing ever
// retried the now-free runtime.
func TestRuntimeOwnership_PartialStandbyTakesOverAfterPeerCrash(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "partial-standby")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}

	// The sibling process already owns the custom profile's runtime.
	customTarget := runtimeOwnerTarget("ws-1", "codex", "prof-1")
	peer := newOwnedSyncDaemon(t, lockDir, fx.server.URL)
	if outcome, err := peer.acquireRuntimeOwnership(customTarget); err != nil || outcome != runtimeOwnedByThisProcess {
		t.Fatalf("sibling claim on the custom runtime: outcome=%v err=%v", outcome, err)
	}

	// This process takes the built-in and records the custom one as standby.
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	codexTarget := runtimeOwnerTarget("ws-1", "codex", "")
	if !d.ownsTarget(codexTarget) {
		t.Fatal("precondition: the built-in runtime was not taken")
	}
	if !d.workspaceHasStandbyRuntime("ws-1") {
		t.Fatal("the sibling-owned runtime was not recorded as standby, so nothing would retry it")
	}
	builtinRuntime := d.allRuntimeIDs()
	if len(builtinRuntime) != 1 {
		t.Fatalf("tracked runtimes = %v, want only the built-in", builtinRuntime)
	}

	// A reconcile tick while the sibling is alive changes nothing.
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("reconcile while standing by: %v", err)
	}
	if !d.workspaceHasStandbyRuntime("ws-1") {
		t.Error("standby entry was dropped while the sibling still owns the runtime")
	}

	// The sibling crashes: its claim dies with the process.
	peer.releaseAllRuntimeOwnership()

	// One normal reconcile tick, with no profile signature change.
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("reconcile after the sibling crashed: %v", err)
	}

	// The freed runtime is taken over even though the workspace never reached
	// zero runtimes...
	if !d.ownsTarget(customTarget) {
		t.Fatal("the freed sibling runtime was not taken over")
	}
	if d.workspaceHasStandbyRuntime("ws-1") {
		t.Error("the acquired runtime is still recorded as standby")
	}
	if got := d.allRuntimeIDs(); len(got) != 2 {
		t.Errorf("tracked runtimes = %v, want the built-in plus the custom profile's", got)
	}
	if got := fx.runtimeIDFor("ws-1", "prof-1"); got == "" {
		t.Error("the custom runtime was never registered server-side")
	}

	// ...while the runtime this process already owned stays owned and tracked:
	// no deregistration, no re-registration churn, nothing recovered away.
	if !d.ownsTarget(codexTarget) {
		t.Fatal("the built-in runtime's claim was released by the takeover")
	}
	if !containsString(d.allRuntimeIDs(), builtinRuntime[0]) {
		t.Error("the built-in runtime stopped being tracked")
	}
	if got := fx.deregisteredIDs(); len(got) != 0 {
		t.Errorf("takeover deregistered %v, want nothing", got)
	}
}

// TestRuntimeOwnership_StaleStandbyTargetsAreForgotten is item 2's cleanup
// requirement: a standby target that is no longer a candidate - the profile was
// deleted, the provider was uninstalled, the profile changed provider identity -
// must not keep its workspace in a permanent reconcile loop.
func TestRuntimeOwnership_StaleStandbyTargetsAreForgotten(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "standby-cleanup")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}

	peer := newOwnedSyncDaemon(t, lockDir, fx.server.URL)
	if outcome, err := peer.acquireRuntimeOwnership(runtimeOwnerTarget("ws-1", "codex", "prof-1")); err != nil || outcome != runtimeOwnedByThisProcess {
		t.Fatalf("sibling claim: outcome=%v err=%v", outcome, err)
	}
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !d.workspaceHasStandbyRuntime("ws-1") {
		t.Fatal("precondition: the sibling-owned profile was not recorded as standby")
	}

	// The profile is deleted server-side: the standby target is no longer a
	// candidate and must not outlive it.
	fx.profiles["ws-1"] = nil
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("reconcile after the profile was deleted: %v", err)
	}
	if d.workspaceHasStandbyRuntime("ws-1") {
		t.Fatal("a stale standby target survived: the workspace would be reconciled forever for a runtime nobody hosts")
	}
	if d.workspaceNeedsRuntimeReconcile("ws-1") {
		t.Error("the workspace still reports a runtime to reconcile with no standby target and a live runtime")
	}
}

// TestRuntimeOwnership_DisabledProfileReleasesItsClaim checks that a
// daemon that stops serving a runtime but keeps its ownership claim locks every
// sibling out of that runtime until the process exits.
func TestRuntimeOwnership_DisabledProfileReleasesItsClaim(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "profile-claim-release")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync: %v", err)
	}

	customTarget := runtimeOwnerTarget("ws-1", "codex", "prof-1")
	if !d.ownsTarget(customTarget) {
		t.Fatal("precondition: the profile's runtime was not taken")
	}
	customRuntime := fx.runtimeIDFor("ws-1", "prof-1")
	if customRuntime == "" {
		t.Fatal("precondition: the profile's runtime was not registered")
	}

	// The profile is disabled: the drift refresh drops its runtime.
	fx.profiles["ws-1"] = nil
	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("profile drift refresh: %v", err)
	}

	if d.ownsTarget(customTarget) {
		t.Fatal("the daemon kept an ownership claim for a runtime it stopped serving; no sibling could take it over")
	}
	if fx.runtimeOnline(customRuntime) {
		t.Error("the disabled profile's runtime was left online server-side")
	}
	// The claim is genuinely free: a sibling takes it with this process still
	// running.
	peer := newOwnedSyncDaemon(t, lockDir, fx.server.URL)
	if outcome, err := peer.acquireRuntimeOwnership(customTarget); err != nil || outcome != runtimeOwnedByThisProcess {
		t.Fatalf("sibling takeover after the profile was disabled: outcome=%v err=%v", outcome, err)
	}
	// The runtime this process still serves keeps its claim.
	if !d.ownsTarget(runtimeOwnerTarget("ws-1", "codex", "")) {
		t.Error("the built-in runtime's claim was released along with the profile's")
	}
}

// TestRuntimeOwnership_WorkspaceRemovalReleasesItsClaims checks that a
// workspace the user is no longer a member of drops out of the sync, and the
// claims for the runtimes it was hosting must go with it.
func TestRuntimeOwnership_WorkspaceRemovalReleasesItsClaims(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "workspace-removal")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	leavingTarget := runtimeOwnerTarget("ws-2", "codex", "")
	keptTarget := runtimeOwnerTarget("ws-1", "codex", "")
	if !d.ownsTarget(leavingTarget) || !d.ownsTarget(keptTarget) {
		t.Fatal("precondition: both workspaces' runtimes were not taken")
	}
	leavingRuntime := fx.runtimeIDFor("ws-2", "codex")

	// The user is removed from ws-2: it disappears from API membership.
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync after the workspace disappeared: %v", err)
	}

	if d.ownsTarget(leavingTarget) {
		t.Error("the daemon kept the claim for a runtime of a workspace it stopped watching")
	}
	if !d.ownsTarget(keptTarget) {
		t.Error("an unrelated workspace's claim was released with it")
	}
	if fx.runtimeOnline(leavingRuntime) {
		t.Error("the left workspace's runtime was left online server-side")
	}
	peer := newOwnedSyncDaemon(t, lockDir, fx.server.URL)
	if outcome, err := peer.acquireRuntimeOwnership(leavingTarget); err != nil || outcome != runtimeOwnedByThisProcess {
		t.Fatalf("sibling takeover of the left workspace's runtime: outcome=%v err=%v", outcome, err)
	}
	if outcome, err := peer.acquireRuntimeOwnership(keptTarget); err != nil || outcome != runtimeOwnedByPeer {
		t.Fatalf("the still-tracked workspace's runtime is not protected: outcome=%v err=%v", outcome, err)
	}
}

// Releasing the claim before deregistration would let a sibling bring the
// runtime back online before this process's Deregister landed.
func TestRuntimeOwnership_ClaimIsReleasedOnlyAfterTheDeregistrationAttempt(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "release-ordering")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	customTarget := runtimeOwnerTarget("ws-1", "codex", "prof-1")
	customRuntime := fx.runtimeIDFor("ws-1", "prof-1")
	if customRuntime == "" || !d.ownsTarget(customTarget) {
		t.Fatal("precondition: the profile's runtime was not taken and registered")
	}

	// The deregister handler is where the ordering can be observed
	// deterministically: a sibling tries to take the claim while the server call
	// is in flight.
	peer := newOwnedSyncDaemon(t, lockDir, fx.server.URL)
	var acquiredWhileDeregistering atomic.Bool
	fx.setDeregisterGate(func(runtimeIDs []string) {
		if !containsString(runtimeIDs, customRuntime) {
			return
		}
		if outcome, err := peer.acquireRuntimeOwnership(customTarget); err == nil && outcome == runtimeOwnedByThisProcess {
			acquiredWhileDeregistering.Store(true)
		}
	})

	fx.profiles["ws-1"] = nil
	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("profile drift refresh: %v", err)
	}

	if acquiredWhileDeregistering.Load() {
		t.Fatal("a sibling acquired the runtime before this process had made its deregistration attempt")
	}
	if outcome, err := peer.acquireRuntimeOwnership(customTarget); err != nil || outcome != runtimeOwnedByThisProcess {
		t.Fatalf("the claim was still held after the deregistration attempt: outcome=%v err=%v", outcome, err)
	}
}

// An unresponsive peer with a held health port keeps activation blocked.
//
// A same-backend peer profile whose health endpoint does not answer used to be
// read as "no peer there", so a legacy daemon that was merely slow - or whose
// health endpoint was unhealthy - was ignored and this process activated the
// runtime it was still serving. Only a free health port proves the peer is gone.
func TestRuntimeCoordination_UnresponsivePeerBlocksActivation(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "unresponsive-peer")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	d.cfg.ServerBaseURL = fx.server.URL
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	withStagedPeerConfig(t, "desktop-host", fx.server.URL)

	// The peer does not answer.
	stubRuntimeCoordination(t, func(context.Context, string) peerCoordinationStatus {
		return peerCoordinationStatus{}
	}, nil)

	// ...but its health port is still held: the peer is alive, just not
	// answering (or not answering usefully), so activation is refused.
	peerHealthPortOwnedFunc = func(int) bool { return true }
	if decision := d.checkRuntimeCoordinationPeers(context.Background()); !decision.Blocked {
		t.Fatal("an unresponsive peer whose health port is still held did not block activation")
	}
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync while blocked: %v", err)
	}
	if got := fx.registerCallCount(); got != 0 {
		t.Errorf("made %d Register calls while an uncoordinated peer was live", got)
	}
	if d.tryEnterClaim() {
		t.Error("work could still be claimed while both processes are known live")
	}

	// The port is free, so the profile directory is stale and the peer really is
	// gone: activation proceeds.
	peerHealthPortOwnedFunc = func(int) bool { return false }
	if decision := d.checkRuntimeCoordinationPeers(context.Background()); decision.Blocked {
		t.Fatalf("a stale same-backend profile directory blocked activation: %v", decision.Peers)
	}
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync after the peer was gone: %v", err)
	}
	if got := fx.registerCallCount(); got != 1 {
		t.Errorf("Register calls after the peer was gone = %d, want 1", got)
	}
	if !d.tryEnterClaim() {
		t.Error("work could not be claimed after the conflicting peer went away")
	}
}

// A late legacy peer makes an already active daemon yield its runtimes.
//
// The peer check used to return from the sync, which is enough only while this
// process owns nothing. A legacy daemon that starts AFTER this one is already
// active used to leave both processes serving the same logical runtime: this
// process kept claiming, heartbeating and re-registering it.
func TestRuntimeCoordination_LateLegacyPeerYieldsRuntimes(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "late-legacy-peer")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	d.cfg.ServerBaseURL = fx.server.URL
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})

	stubRuntimeCoordination(t, nil, nil)

	// No peer yet: this process becomes the active owner.
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	codexTarget := runtimeOwnerTarget("ws-1", "codex", "")
	runtimeID := fx.runtimeIDFor("ws-1", "codex")
	if runtimeID == "" || !d.ownsTarget(codexTarget) {
		t.Fatal("precondition: the runtime was not taken and registered")
	}

	// A legacy daemon starts on the same backend: it is alive and does not
	// advertise the coordination capability.
	withStagedPeerConfig(t, "legacy-host", fx.server.URL)
	peerProbeFunc = func(context.Context, string) peerCoordinationStatus {
		return peerCoordinationStatus{Alive: true, Backend: fx.server.URL}
	}
	peerHealthPortOwnedFunc = func(int) bool { return true }
	// Subscribed after the registration above, so the only notification this
	// channel can carry is the yield's.
	runtimeSetCh, unsub := d.runtimeSet.Subscribe()
	defer unsub()

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync with a late legacy peer: %v", err)
	}

	if !d.legacyPeerStandby.Load() {
		t.Error("legacy standby was not entered")
	}
	if d.ownsTarget(codexTarget) {
		t.Error("this process kept the runtime claim while an uncoordinated peer serves the machine")
	}
	if got := d.allRuntimeIDs(); len(got) != 0 {
		t.Errorf("still tracking runtimes %v after yielding to the peer", got)
	}
	if d.tryEnterClaim() {
		t.Error("a new task could still be claimed while both processes are known live")
	}
	// The shared server row is NOT taken offline: both processes use the
	// machine-scoped daemon identity, so that row is the one the legacy peer is
	// still serving. Deregistering here would reproduce the sibling-shutdown
	// failure (#8280) through the mixed-version path.
	if got := fx.deregisteredIDs(); len(got) != 0 {
		t.Errorf("the legacy-peer yield deregistered %v; the peer is still serving that shared row", got)
	}
	if !fx.runtimeOnline(runtimeID) {
		t.Error("the shared runtime was taken offline during the handoff")
	}
	// Heartbeating stops through the runtime-set notification: the heartbeat
	// loop cancels the goroutine of every ID that left allRuntimeIDs().
	select {
	case <-runtimeSetCh:
	case <-time.After(2 * time.Second):
		t.Error("the runtime-set watchers were not notified, so the heartbeat goroutine would keep running")
	}

	// The peer is upgraded or stopped: the next tick resumes and this process
	// takes its runtimes back.
	peerProbeFunc = func(context.Context, string) peerCoordinationStatus {
		return peerCoordinationStatus{}
	}
	peerHealthPortOwnedFunc = func(int) bool { return false }
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync after the peer was gone: %v", err)
	}
	if !d.ownsTarget(codexTarget) {
		t.Error("ownership was not retaken after the conflicting peer went away")
	}
	if got := d.allRuntimeIDs(); len(got) != 1 {
		t.Fatalf("tracked runtimes after resuming = %v, want the runtime back", got)
	}
	// No fake offline -> online transition in between: the row never left the
	// online state the legacy peer kept it in.
	if !fx.runtimeOnline(d.allRuntimeIDs()[0]) {
		t.Error("the shared runtime is not online after ownership was retaken")
	}
	if !d.tryEnterClaim() {
		t.Error("claiming was not resumed after the peer went away")
	}
}

// The local drop has to happen inside the workspace's registration lock, or a
// register response that is already in flight publishes the runtime back into
// the set the yield just emptied - leaving standby set, the runtime tracked
// again, and its heartbeat and claim running.
//
// The register gate holds the response of a reconcile register while the yield
// runs. With the drop inside the lock, the response lands first and the yield
// removes the row afterwards; with the drop before the lock, the response
// republishes the row and it is tracked again at the end.
func TestRuntimeCoordination_YieldIsSerializedAgainstRegistration(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "yield-vs-register")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	d.cfg.ServerBaseURL = fx.server.URL
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})

	stubRuntimeCoordination(t, nil, nil)

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	codexTarget := runtimeOwnerTarget("ws-1", "codex", "")
	runtimeID := fx.runtimeIDFor("ws-1", "codex")
	if runtimeID == "" || !d.ownsTarget(codexTarget) {
		t.Fatal("precondition: the runtime was not taken and registered")
	}

	// Hold one register for ws-1 in flight while the legacy peer appears.
	arrived := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	fx.setRegisterGate(func(workspaceID string) {
		if workspaceID != "ws-1" {
			return
		}
		once.Do(func() { close(arrived) })
		<-release
	})

	registering := make(chan struct{})
	go func() {
		defer close(registering)
		if err := d.reregisterWorkspaceAfterRuntimeGone(context.Background(), "ws-1"); err != nil {
			t.Errorf("registration in flight: %v", err)
		}
	}()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("the register request never reached the server")
	}

	withStagedPeerConfig(t, "legacy-host", fx.server.URL)
	peerProbeFunc = func(context.Context, string) peerCoordinationStatus {
		return peerCoordinationStatus{Alive: true, Backend: fx.server.URL}
	}
	peerHealthPortOwnedFunc = func(int) bool { return true }

	yielding := make(chan struct{})
	go func() {
		defer close(yielding)
		if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
			t.Errorf("sync with a live legacy peer: %v", err)
		}
	}()

	// Wait for the yield to have closed the claim gate. From here the register
	// response is still held, so a drop that is not serialized by the register
	// lock would already be visible as an empty local runtime set.
	if !waitUntil(5*time.Second, func() bool { return d.legacyPeerStandby.Load() }) {
		t.Fatal("legacy standby was never entered")
	}
	if got := d.allRuntimeIDs(); len(got) == 0 {
		t.Error("the yield dropped local tracking while a register response was still in flight")
	}

	close(release)
	<-registering
	select {
	case <-yielding:
	case <-time.After(5 * time.Second):
		t.Fatal("the yield never completed")
	}

	// The register response must not have republished the runtime after the
	// yield finished.
	if got := d.allRuntimeIDs(); len(got) != 0 {
		t.Errorf("a runtime is tracked again after the yield: %v", got)
	}
	if d.ownsTarget(codexTarget) {
		t.Error("this process still owns the runtime's logical target after the yield")
	}
	if !d.legacyPeerStandby.Load() {
		t.Error("legacy standby was cleared")
	}
	if got := fx.deregisteredIDs(); len(got) != 0 {
		t.Errorf("the yield deregistered %v", got)
	}
	if !fx.runtimeOnline(runtimeID) {
		t.Error("the shared runtime was taken offline during the handoff")
	}
}

// A claim that
// entered before the standby barrier is allowed to finish its
// ClaimTask -> dispatch step, and only then is ownership dropped. The claim
// accounting (claimMu / claimsInFlight / tryEnterClaim / exitClaim) is the
// transition boundary the daemon already tracks.
func TestRuntimeCoordination_YieldWaitsForClaimsInFlight(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "yield-vs-claim")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	d.cfg.ServerBaseURL = fx.server.URL
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	codexTarget := runtimeOwnerTarget("ws-1", "codex", "")
	runtimeID := fx.runtimeIDFor("ws-1", "codex")
	if runtimeID == "" || !d.ownsTarget(codexTarget) {
		t.Fatal("precondition: the runtime was not taken and registered")
	}

	// A claim enters the transition and its response is still in flight.
	if !d.tryEnterClaim() {
		t.Fatal("precondition: the claim must enter")
	}

	yielded := make(chan struct{})
	go func() {
		defer close(yielded)
		d.yieldRuntimesToLegacyPeer(context.Background(), legacyPeerDecision{
			Blocked: true,
			Peers:   []string{"legacy-host (http://127.0.0.1:19514/health) is alive"},
		})
	}()

	if !waitUntil(5*time.Second, func() bool { return d.legacyPeerStandby.Load() }) {
		t.Fatal("legacy standby was never entered")
	}
	// No new claim may start once the barrier is up.
	if d.tryEnterClaim() {
		t.Error("a new claim entered after the standby barrier")
	}
	// ...and the ownership of the claimed runtime is still intact underneath the
	// in-flight claim.
	select {
	case <-yielded:
		t.Fatal("runtime ownership was yielded while a claim was still in flight")
	case <-time.After(100 * time.Millisecond):
	}
	if got := d.allRuntimeIDs(); len(got) != 1 || got[0] != runtimeID {
		t.Errorf("runtime tracking was dropped under an in-flight claim: %v", got)
	}
	if !d.ownsTarget(codexTarget) {
		t.Error("the ownership claim was released under an in-flight claim")
	}

	// The response arrives and its dispatch transition completes.
	d.exitClaim()
	select {
	case <-yielded:
	case <-time.After(5 * time.Second):
		t.Fatal("the yield did not complete after the claim drained")
	}

	if got := d.allRuntimeIDs(); len(got) != 0 {
		t.Errorf("runtime tracking after the yield = %v, want none", got)
	}
	if d.ownsTarget(codexTarget) {
		t.Error("the ownership claim survived the yield")
	}
	if d.tryEnterClaim() {
		t.Error("a new claim entered while legacy standby is set")
	}
	if got := fx.deregisteredIDs(); len(got) != 0 {
		t.Errorf("the yield deregistered %v", got)
	}
	if !fx.runtimeOnline(runtimeID) {
		t.Error("the shared runtime was taken offline during the handoff")
	}
}

// TestRuntimeCoordination_YieldDoesNotWaitForRunningTasks is item 4: the
// transition drains the claim step only. A task that is already executing keeps
// running and is not waited for - the acceptance requirement is "no new work
// starts", not "drain in flight".
//
// The invariant under test is LOCAL: the yield neither waits for nor cancels
// active tasks. It is NOT "active tasks survive a late legacy daemon" - this
// test does not start a real legacy process, and it cannot prove anything about
// what one would do. A legacy binary does not participate in the ownership
// protocol and may run its own startup recovery against the shared runtime row,
// which is outside this daemon's control (see yieldRuntimesToLegacyPeer).
func TestRuntimeCoordination_YieldDoesNotWaitForRunningTasks(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "yield-vs-running-task")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	runtimeID := fx.runtimeIDFor("ws-1", "codex")

	// A task is executing locally while the peer conflict appears.
	d.activeTasks.Add(1)
	defer d.activeTasks.Add(-1)

	yielded := make(chan struct{})
	go func() {
		defer close(yielded)
		d.yieldRuntimesToLegacyPeer(context.Background(), legacyPeerDecision{Blocked: true, Peers: []string{"legacy-host"}})
	}()
	select {
	case <-yielded:
	case <-time.After(5 * time.Second):
		t.Fatal("the yield waited for the running task instead of the claim transition")
	}

	if got := d.activeTasks.Load(); got != 1 {
		t.Errorf("active tasks = %d after the yield, want the running task untouched", got)
	}
	if got := d.allRuntimeIDs(); len(got) != 0 {
		t.Errorf("runtime tracking after the yield = %v, want none", got)
	}
	if !fx.runtimeOnline(runtimeID) {
		t.Error("the shared runtime was taken offline during the handoff")
	}
}

// Re-registering one deleted runtime must not recover surviving runtimes.
//
// reregisterWorkspaceAfterRuntimeGone used to call RecoverOrphans for every
// runtime the register response returned. That response carries the workspace's
// SURVIVING runtimes too, and recovery hard-fails every running task on the
// runtime it targets - so one deleted runtime's recovery failed the work this
// process was still executing on its sibling.
func TestRuntimeOwnership_RuntimeGoneRecoversOnlyTheDeletedRuntime(t *testing.T) {
	isolatedProfileHome(t)
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	lockDir := scopeLockDirForTest(t, "runtime-gone-recovery")
	fx.shareRuntimeOwnership(lockDir)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{
		"claude": {Path: "/fake/claude"},
		"codex":  {Path: "/fake/codex"},
	}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("sync: %v", err)
	}

	codexRuntime := fx.runtimeIDFor("ws-1", "codex")
	claudeRuntime := fx.runtimeIDFor("ws-1", "claude")
	if codexRuntime == "" || claudeRuntime == "" {
		t.Fatalf("precondition: both runtimes registered (%q, %q)", codexRuntime, claudeRuntime)
	}
	// Taking a runtime over recovers its orphans exactly once.
	if got := fx.recoveredIDs(); countString(got, codexRuntime) != 1 || countString(got, claudeRuntime) != 1 {
		t.Fatalf("startup recoveries = %v, want exactly one per runtime", got)
	}

	// The server deletes the codex runtime row; the daemon notices and
	// re-registers the workspace, which returns codex AND the surviving claude.
	d.handleRuntimeGone(codexRuntime)

	if got := fx.recoveredIDs(); countString(got, codexRuntime) != 2 {
		t.Errorf("recoveries = %v, want a second one for the deleted runtime's new row", got)
	}
	if got := fx.recoveredIDs(); countString(got, claudeRuntime) != 1 {
		t.Errorf("recoveries = %v: the surviving runtime was recovered again, which fails the task it is running", got)
	}
	if !d.ownsTarget(runtimeOwnerTarget("ws-1", "claude", "")) {
		t.Error("the surviving runtime's ownership claim was dropped by the runtime-gone recovery")
	}
	if got := d.allRuntimeIDs(); len(got) != 2 {
		t.Errorf("tracked runtimes = %v, want both after the re-register", got)
	}
	if !fx.runtimeOnline(codexRuntime) {
		t.Error("the re-registered runtime was not brought back online")
	}
}
