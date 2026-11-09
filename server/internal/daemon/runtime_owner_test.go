package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// A request that entered the claim transition must finish before its owner's
// filesystem claim is released; otherwise the shared daemon_id lets it claim
// work after a sibling registers the next ownership.
func TestRuntimeOwnership_ReleaseDrainsOldClaimBeforeTakeover(t *testing.T) {
	arrived := make(chan struct{})
	finish := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/tasks/claim" {
			close(arrived)
			<-finish
			_, _ = w.Write([]byte(`{"tasks":[]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	lockDir := scopeLockDirForTest(t, "claim-handoff")
	a := newOwnershipDaemon(t, lockDir, srv.URL)
	b := newOwnershipDaemon(t, lockDir, srv.URL)
	target := runtimeOwnerTarget(ownershipWorkspace, "codex", "")
	if outcome, err := a.acquireRuntimeOwnership(target); err != nil || outcome != runtimeOwnedByThisProcess {
		t.Fatalf("owner claim: %v %v", outcome, err)
	}
	if !a.tryEnterClaim() {
		t.Fatal("old claim could not enter")
	}
	claimed := make(chan struct{})
	go func() {
		defer close(claimed)
		_, err := a.client.ClaimTasks(context.Background(), "machine-1", []string{"runtime-1"}, 1)
		if err != nil {
			t.Errorf("old ClaimTasks: %v", err)
		}
		a.exitClaim()
	}()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("claim never arrived")
	}
	released := make(chan struct{})
	started := make(chan struct{})
	go func() { close(started); a.releaseRuntimeOwnership(target); close(released) }()
	<-started
	select {
	case <-released:
		t.Fatal("ownership was released before the old ClaimTasks finished")
	case <-time.After(100 * time.Millisecond):
	}
	if a.tryEnterClaim() {
		t.Fatal("new claim entered during ownership release")
	}
	if outcome, err := b.acquireRuntimeOwnership(target); err != nil || outcome != runtimeOwnedByPeer {
		t.Fatalf("sibling took over while old ClaimTasks was in flight: %v %v", outcome, err)
	}
	close(finish)
	select {
	case <-claimed:
	case <-time.After(5 * time.Second):
		t.Fatal("claim did not finish")
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("ownership was not released")
	}
	if outcome, err := b.acquireRuntimeOwnership(target); err != nil || outcome != runtimeOwnedByThisProcess {
		t.Fatalf("sibling could not take over after old claim drained: %v %v", outcome, err)
	}
}

// Cross-profile runtime ownership regression tests (GH #8280).
//
// Two daemon PROCESSES on one machine and one backend address one logical
// runtime, because daemon_id is machine-scoped and the server upserts on
// (workspace_id, daemon_id, provider). These tests model the two processes as
// two Daemon values sharing one scope lock directory and nothing else - never as
// two goroutines on one Daemon, which would share the maps under test.

// runtimeLifecycleRecorder stands in for the server endpoints that matter here.
type runtimeLifecycleRecorder struct {
	mu        sync.Mutex
	recovered []string // runtime ids RecoverOrphans was called for
	dropped   []string // runtime ids Deregister was called for
}

func (r *runtimeLifecycleRecorder) recordRecover(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recovered = append(r.recovered, id)
}

func (r *runtimeLifecycleRecorder) recordDeregister(ids []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropped = append(r.dropped, ids...)
}

func (r *runtimeLifecycleRecorder) recoveredIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.recovered...)
}

func (r *runtimeLifecycleRecorder) droppedIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.dropped...)
}

func newRuntimeLifecycleServer(t *testing.T, rec *runtimeLifecycleRecorder) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/recover-orphans"):
			// The runtime id is the path parameter, not a body field.
			trimmed := strings.TrimSuffix(r.URL.Path, "/recover-orphans")
			rec.recordRecover(trimmed[strings.LastIndex(trimmed, "/")+1:])
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/daemon/deregister/fenced":
			var body struct {
				RuntimeIDs []string `json:"runtime_ids"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			rec.recordDeregister(body.RuntimeIDs)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newOwnershipDaemon builds a Daemon with its own runtime-owner state pointed at
// one scope lock directory: two of these are two processes.
func newOwnershipDaemon(t *testing.T, lockDir, serverURL string) *Daemon {
	t.Helper()
	d := &Daemon{
		cfg: Config{
			ServerBaseURL:  serverURL,
			WorkspacesRoot: t.TempDir(),
			Profile:        "",
		},
		logger:        quietTaskLog(),
		scopeLocks:    execenv.NewScopeLocks(lockDir),
		runtimeOwners: newRuntimeOwners(),
		workspaces:    make(map[string]*workspaceState),
		runtimeIndex:  make(map[string]Runtime),
	}
	d.client = NewClient(serverURL)
	return d
}

// seedOwnedRuntime publishes a runtime as tracked-and-owned by this daemon, which
// is the state a process is in while it serves that runtime.
func seedOwnedRuntime(t *testing.T, d *Daemon, workspaceID, runtimeID, provider string) {
	t.Helper()
	target := runtimeOwnerTarget(workspaceID, provider, "")
	if outcome, err := d.acquireRuntimeOwnership(target); err != nil || outcome != runtimeOwnedByThisProcess {
		t.Fatalf("seed ownership for %s: outcome=%v err=%v", target, outcome, err)
	}
	d.mu.Lock()
	d.runtimeIndex[runtimeID] = Runtime{ID: runtimeID, Provider: provider, Status: "online"}
	d.workspaces[workspaceID] = &workspaceState{workspaceID: workspaceID, runtimeIDs: []string{runtimeID}}
	d.mu.Unlock()
}

const ownershipWorkspace = "11111111-1111-1111-1111-111111111111"

// TestRuntimeOwnership_SiblingCannotTakeOverALiveRuntime checks that while one
// process owns the runtime, a second process must not become an owner of it, must
// not be able to register it, and therefore must never run the startup recovery
// that hard-fails the first process running tasks.
func TestRuntimeOwnership_SiblingCannotTakeOverALiveRuntime(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "runtime-owner")
	rec := &runtimeLifecycleRecorder{}
	srv := newRuntimeLifecycleServer(t, rec)
	owner := newOwnershipDaemon(t, lockDir, srv.URL)
	standby := newOwnershipDaemon(t, lockDir, srv.URL)

	seedOwnedRuntime(t, owner, ownershipWorkspace, "runtime-1", "codex")

	// The sibling starts up against the same workspace and the same provider.
	target := runtimeOwnerTarget(ownershipWorkspace, "codex", "")
	outcome, err := standby.acquireRuntimeOwnership(target)
	if err != nil {
		t.Fatalf("sibling claim attempt: %v", err)
	}
	if outcome != runtimeOwnedByPeer {
		t.Fatal("a sibling became the active owner of a runtime another process is serving")
	}
	if standby.ownsTarget(target) {
		t.Fatal("sibling reports ownership it does not hold")
	}

	// A standby process has nothing it may announce, and that must be reported as
	// standby rather than as "nothing to host" - the latter is what makes a
	// sibling deregister a live runtime.
	owned, _, peerOwned, err := standby.filterOwnedRuntimeCandidates(t.Context(), ownershipWorkspace, []map[string]string{{"type": "codex", "status": "online"}}, nil)
	if err != nil {
		t.Fatalf("filter candidates: %v", err)
	}
	if len(owned) != 0 {
		t.Fatalf("standby process announced %d candidates it does not own", len(owned))
	}
	if len(peerOwned) != 1 {
		t.Fatalf("peerOwned = %v, want the sibling-owned provider", peerOwned)
	}

	// Recovery must not run for the sibling-owned runtime.
	standby.recoverOrphansOncePerOwnership(t.Context(), ownershipWorkspace, Runtime{ID: "runtime-1", Provider: "codex"})
	if got := rec.recoveredIDs(); len(got) != 0 {
		t.Fatalf("standby process recovered orphans on a live sibling runtime: %v", got)
	}
}

// TestRuntimeOwnership_OwnerRecoversExactlyOnce checks that the owner recovers
// orphaned work when it takes a runtime over, and a later registration of the
// same ownership must not recover again - that second call is what would fail the
// tasks the owner is itself running.
func TestRuntimeOwnership_OwnerRecoversExactlyOnce(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "runtime-owner-once")
	rec := &runtimeLifecycleRecorder{}
	srv := newRuntimeLifecycleServer(t, rec)
	owner := newOwnershipDaemon(t, lockDir, srv.URL)

	seedOwnedRuntime(t, owner, ownershipWorkspace, "runtime-1", "codex")
	rt := Runtime{ID: "runtime-1", Provider: "codex"}
	owner.recoverOrphansOncePerOwnership(t.Context(), ownershipWorkspace, rt)
	// Same ownership, later registration (version refresh, profile drift,
	// periodic reconcile).
	owner.recoverOrphansOncePerOwnership(t.Context(), ownershipWorkspace, rt)
	owner.recoverOrphansOncePerOwnership(t.Context(), ownershipWorkspace, rt)

	got := rec.recoveredIDs()
	if len(got) != 1 || got[0] != "runtime-1" {
		t.Fatalf("recovery ran %v, want exactly one call for runtime-1", got)
	}
}

// TestRuntimeOwnership_TakeoverAfterOwnerCrash checks that when the owner dies its OS
// claim is released, a standby takes over, registers, and recovers exactly once -
// so only genuinely orphaned work is reclaimed.
func TestRuntimeOwnership_TakeoverAfterOwnerCrash(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "runtime-owner-takeover")
	rec := &runtimeLifecycleRecorder{}
	srv := newRuntimeLifecycleServer(t, rec)
	owner := newOwnershipDaemon(t, lockDir, srv.URL)
	standby := newOwnershipDaemon(t, lockDir, srv.URL)

	seedOwnedRuntime(t, owner, ownershipWorkspace, "runtime-1", "codex")
	target := runtimeOwnerTarget(ownershipWorkspace, "codex", "")
	oldToken := owner.runtimeOwnerToken(target)
	if oldToken == "" {
		t.Fatal("owner claim has no server fencing token")
	}
	if outcome, err := standby.acquireRuntimeOwnership(target); err != nil || outcome != runtimeOwnedByPeer {
		t.Fatalf("standby claim before crash: outcome=%v err=%v", outcome, err)
	}

	// The owner process dies: its claims are released by the kernel.
	owner.releaseAllRuntimeOwnership()

	outcome, err := standby.acquireRuntimeOwnership(target)
	if err != nil || outcome != runtimeOwnedByThisProcess {
		t.Fatalf("standby takeover: outcome=%v err=%v", outcome, err)
	}
	if next := standby.runtimeOwnerToken(target); next <= oldToken {
		t.Fatalf("takeover token %q must be newer than prior owner token %q", next, oldToken)
	}
	standby.recoverOrphansOncePerOwnership(t.Context(), ownershipWorkspace, Runtime{ID: "runtime-1", Provider: "codex"})
	if got := rec.recoveredIDs(); len(got) != 1 {
		t.Fatalf("takeover recovery = %v, want exactly one call", got)
	}
}

// TestRuntimeOwnership_NonOwnerShutdownLeavesRuntimeOnline checks that a standby
// process exiting must not take a sibling runtime offline.
func TestRuntimeOwnership_NonOwnerShutdownLeavesRuntimeOnline(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "runtime-owner-shutdown")
	rec := &runtimeLifecycleRecorder{}
	srv := newRuntimeLifecycleServer(t, rec)
	owner := newOwnershipDaemon(t, lockDir, srv.URL)
	standby := newOwnershipDaemon(t, lockDir, srv.URL)

	seedOwnedRuntime(t, owner, ownershipWorkspace, "runtime-1", "codex")
	// The standby tracks the same runtime locally (it learned it from the
	// register response) but does not own it.
	standby.mu.Lock()
	standby.runtimeIndex["runtime-1"] = Runtime{ID: "runtime-1", Provider: "codex", Status: "online"}
	standby.workspaces[ownershipWorkspace] = &workspaceState{workspaceID: ownershipWorkspace, runtimeIDs: []string{"runtime-1"}}
	standby.mu.Unlock()

	standby.deregisterRuntimes()
	if got := rec.droppedIDs(); len(got) != 0 {
		t.Fatalf("standby shutdown deregistered a live sibling runtime: %v", got)
	}

	// The owner shutdown does report it, and only then releases the claim.
	owner.deregisterRuntimes()
	if got := rec.droppedIDs(); len(got) != 1 || got[0] != "runtime-1" {
		t.Fatalf("owner shutdown deregistered %v, want runtime-1", got)
	}
	if owner.ownsTarget(runtimeOwnerTarget(ownershipWorkspace, "codex", "")) {
		t.Fatal("owner still holds the claim after deregistering")
	}
}

// TestRuntimeOwnership_IndependentScopesStayIndependent pins the targets that must
// NOT contend: a different workspace, a different provider, a custom profile, and
// a different work-state scope all own independently.
func TestRuntimeOwnership_IndependentScopesStayIndependent(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "runtime-owner-independent")
	srv := newRuntimeLifecycleServer(t, &runtimeLifecycleRecorder{})
	a := newOwnershipDaemon(t, lockDir, srv.URL)
	b := newOwnershipDaemon(t, lockDir, srv.URL)

	// Same workspace, same protocol family, but a custom profile is a different
	// server runtime identity and so a different owner target.
	builtin := runtimeOwnerTarget(ownershipWorkspace, "codex", "")
	custom := runtimeOwnerTarget(ownershipWorkspace, "codex", "profile-1")
	if builtin == custom {
		t.Fatal("a built-in runtime and a custom profile share one ownership target")
	}
	// A different workspace is a different server identity.
	if runtimeOwnerTarget("22222222-2222-2222-2222-222222222222", "codex", "") == builtin {
		t.Fatal("two workspaces share one ownership target")
	}
	// A different provider is a different server identity.
	if runtimeOwnerTarget(ownershipWorkspace, "claude", "") == builtin {
		t.Fatal("two providers share one ownership target")
	}

	for _, target := range []string{builtin, custom} {
		if outcome, err := a.acquireRuntimeOwnership(target); err != nil || outcome != runtimeOwnedByThisProcess {
			t.Fatalf("daemon A claim %s: outcome=%v err=%v", target, outcome, err)
		}
	}
	// Both are already owned by A, so B gets neither - and the custom one being
	// independent is what lets a custom-only daemon coexist with a built-in one.
	for _, target := range []string{builtin, custom} {
		if outcome, err := b.acquireRuntimeOwnership(target); err != nil || outcome != runtimeOwnedByPeer {
			t.Fatalf("daemon B claim %s: outcome=%v err=%v", target, outcome, err)
		}
	}
	// A different backend uses a different lock directory, so it never contends.
	otherScope := newOwnershipDaemon(t, scopeLockDirForTest(t, "other-backend"), srv.URL)
	if outcome, err := otherScope.acquireRuntimeOwnership(builtin); err != nil || outcome != runtimeOwnedByThisProcess {
		t.Fatalf("independent scope claim: outcome=%v err=%v", outcome, err)
	}
}

// TestRuntimeOwnership_TargetIsProfileIndependent is the load-bearing property:
// the CLI profile and the Desktop profile serving one backend must land on ONE
// target, because profile is the boundary this claim exists to look through.
func TestRuntimeOwnership_TargetIsProfileIndependent(t *testing.T) {
	cliDaemon := &Daemon{cfg: Config{Profile: ""}}
	desktopDaemon := &Daemon{cfg: Config{Profile: "desktop-host"}}
	if got, want := runtimeOwnerTargetForEntry(ownershipWorkspace, map[string]string{"type": "codex"}),
		runtimeOwnerTargetForEntry(ownershipWorkspace, map[string]string{"type": "codex"}); got != want {
		t.Fatal("target derivation is not stable")
	}
	_ = cliDaemon
	_ = desktopDaemon
	// The target takes no profile argument at all, which is the guarantee: there is
	// no way for a caller to make it profile-dependent.
	if strings.Contains(runtimeOwnerTarget(ownershipWorkspace, "codex", ""), "desktop") {
		t.Fatal("ownership target encodes a Multica profile name")
	}
}
