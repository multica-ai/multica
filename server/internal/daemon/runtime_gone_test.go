package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// freshDaemon builds a Daemon with every map field the production New() seeds
// so callers can exercise handleRuntimeGone without going through Run.
func freshDaemon(serverURL string) *Daemon {
	return &Daemon{
		client:                    NewClient(serverURL),
		logger:                    slog.New(slog.NewTextHandler(testNopWriter{}, &slog.HandlerOptions{Level: slog.LevelWarn})),
		workspaces:                make(map[string]*workspaceState),
		runtimeIndex:              make(map[string]Runtime),
		runtimeSet:                newRuntimeSetWatcher(),
		agentVersions:             make(map[string]string),
		wsHBLastAck:               make(map[string]time.Time),
		activeEnvRoots:            make(map[string]int),
		runtimeGoneInflight:       make(map[string]struct{}),
		reregisterNextAttempt:     make(map[string]time.Time),
		reregisterLastCompletedAt: make(map[string]time.Time),
	}
}

// testNopWriter discards log output so tests don't spam stderr.
type testNopWriter struct{}

func (testNopWriter) Write(p []byte) (int, error) { return len(p), nil }

// stubAgentVersion swaps out the agent version probes that registerRuntimesForWorkspace
// would normally shell out for, and restores the production hooks on cleanup.
// Returns a no-op cleanup so callers can use t.Cleanup directly.
func stubAgentVersion(t *testing.T) func() {
	t.Helper()
	origDetect := detectAgentVersion
	origCheck := checkAgentMinVersion
	detectAgentVersion = func(_ context.Context, _ string) (string, error) {
		return "9.9.9", nil
	}
	checkAgentMinVersion = func(_, _ string) error { return nil }
	return func() {
		detectAgentVersion = origDetect
		checkAgentMinVersion = origCheck
	}
}

func TestRemoveStaleRuntime_PrunesAllLocalState(t *testing.T) {
	t.Parallel()

	d := freshDaemon("")
	ws := &workspaceState{
		workspaceID: "ws-1",
		runtimeIDs:  []string{"rt-1", "rt-2", "rt-3"},
	}
	d.workspaces["ws-1"] = ws
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1"}
	d.runtimeIndex["rt-2"] = Runtime{ID: "rt-2"}
	d.runtimeIndex["rt-3"] = Runtime{ID: "rt-3"}
	d.wsHBLastAck["rt-2"] = time.Now()

	workspaceID, removed := d.removeStaleRuntime("rt-2")
	if !removed {
		t.Fatalf("removeStaleRuntime: removed=false, want true")
	}
	if workspaceID != "ws-1" {
		t.Fatalf("workspaceID = %q, want ws-1", workspaceID)
	}
	if got := ws.runtimeIDs; len(got) != 2 || got[0] != "rt-1" || got[1] != "rt-3" {
		t.Fatalf("runtimeIDs = %v, want [rt-1 rt-3]", got)
	}
	if _, ok := d.runtimeIndex["rt-2"]; ok {
		t.Fatalf("runtimeIndex still contains rt-2")
	}
	if _, ok := d.wsHBLastAck["rt-2"]; ok {
		t.Fatalf("wsHBLastAck still contains rt-2")
	}
}

func TestRemoveStaleRuntime_UnknownRuntimeIsNoop(t *testing.T) {
	t.Parallel()

	d := freshDaemon("")
	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-1"}}
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1"}

	workspaceID, removed := d.removeStaleRuntime("rt-unknown")
	if removed {
		t.Fatalf("removeStaleRuntime: removed=true for unknown id, want false")
	}
	if workspaceID != "" {
		t.Fatalf("workspaceID = %q for unknown id, want empty", workspaceID)
	}
	if got := d.workspaces["ws-1"].runtimeIDs; len(got) != 1 {
		t.Fatalf("unrelated workspace runtimeIDs mutated: %v", got)
	}
}

func TestRemoveStaleRuntime_PreservesWorkspaceStatePointer(t *testing.T) {
	t.Parallel()

	// The Daemon contract is that workspaceState pointers must NEVER be
	// replaced — only fields mutated — because ensureRepoReady holds a long
	// repoRefreshMu through repo syncs. Regressing this turns concurrent
	// repo refreshes into a deadlock against the wrong mutex copy. Guard it
	// here so the invariant is observable in tests.
	d := freshDaemon("")
	original := &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-1"}}
	d.workspaces["ws-1"] = original
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1"}

	d.removeStaleRuntime("rt-1")

	if d.workspaces["ws-1"] != original {
		t.Fatalf("workspaceState pointer was replaced; ensureRepoReady's mutex assumption broken")
	}
}

// handleRuntimeGoneFixture wires up a Daemon against a fake server that
// answers register/recover-orphans. registerCount is incremented exactly
// once per /api/daemon/register call so tests can assert on coalescing.
type handleRuntimeGoneFixture struct {
	daemon        *Daemon
	server        *httptest.Server
	registerCount *atomic.Int64
}

func newHandleRuntimeGoneFixture(t *testing.T) *handleRuntimeGoneFixture {
	t.Helper()

	var registerCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/daemon/register":
			registerCount.Add(1)
			// Each register call returns the same fresh runtime ID so
			// downstream assertions can observe it.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RegisterResponse{
				Runtimes: []Runtime{{ID: "rt-new", Name: "Claude", Provider: "claude", Status: "online"}},
				Repos:    []RepoData{},
			})
		case strings.HasSuffix(r.URL.Path, "/recover-orphans"):
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	// Attach a single configured agent so registerRuntimesForWorkspace would
	// produce a non-empty request body. The fake server ignores the body,
	// but the registerRuntimesForWorkspace pre-flight (DetectVersion) would
	// otherwise reject the call.
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/usr/bin/true"}}
	// Replace the agent version probe so the test doesn't shell out.
	t.Cleanup(stubAgentVersion(t))
	return &handleRuntimeGoneFixture{daemon: d, server: srv, registerCount: &registerCount}
}

func TestHandleRuntimeGone_PrunesAndReregisters(t *testing.T) {
	// Not t.Parallel: stubAgentVersion mutates package-level vars used by
	// registerRuntimesForWorkspace. Other Parallel tests in this file that
	// don't exercise registration are still parallel-safe.
	fx := newHandleRuntimeGoneFixture(t)
	d := fx.daemon
	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-old"}}
	d.runtimeIndex["rt-old"] = Runtime{ID: "rt-old"}
	d.wsHBLastAck["rt-old"] = time.Now()

	d.handleRuntimeGone("rt-old")

	if got := d.runtimeIndex["rt-old"]; got.ID != "" {
		t.Fatalf("rt-old still present in runtimeIndex: %+v", got)
	}
	if _, ok := d.runtimeIndex["rt-new"]; !ok {
		t.Fatalf("rt-new not added to runtimeIndex after re-register")
	}
	if got := d.workspaces["ws-1"].runtimeIDs; len(got) != 1 || got[0] != "rt-new" {
		t.Fatalf("workspace runtimeIDs after recovery = %v, want [rt-new]", got)
	}
	if _, ok := d.wsHBLastAck["rt-old"]; ok {
		t.Fatalf("wsHBLastAck not cleared for rt-old")
	}
	if got := fx.registerCount.Load(); got != 1 {
		t.Fatalf("register endpoint called %d times, want 1", got)
	}
}

func TestHandleRuntimeGone_CoalescesConcurrentCallers(t *testing.T) {
	// Not t.Parallel — stubAgentVersion via newHandleRuntimeGoneFixture.
	// Three goroutines (heartbeat, poller, WS) may each detect the same
	// stale runtime within the same beat. Exactly one re-register must
	// reach the server.
	fx := newHandleRuntimeGoneFixture(t)
	d := fx.daemon
	d.workspaces["ws-1"] = &workspaceState{
		workspaceID: "ws-1",
		runtimeIDs:  []string{"rt-a", "rt-b", "rt-c"},
	}
	d.runtimeIndex["rt-a"] = Runtime{ID: "rt-a"}
	d.runtimeIndex["rt-b"] = Runtime{ID: "rt-b"}
	d.runtimeIndex["rt-c"] = Runtime{ID: "rt-c"}

	var wg sync.WaitGroup
	for _, rid := range []string{"rt-a", "rt-b", "rt-c"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			d.handleRuntimeGone(id)
		}(rid)
	}
	wg.Wait()

	if got := fx.registerCount.Load(); got != 1 {
		t.Fatalf("register endpoint called %d times under stampede, want 1", got)
	}
	if got := d.workspaces["ws-1"].runtimeIDs; len(got) != 1 || got[0] != "rt-new" {
		t.Fatalf("workspace runtimeIDs after stampede = %v, want [rt-new]", got)
	}
}

func TestHandleRuntimeGone_BackoffOnFailure(t *testing.T) {
	// Not t.Parallel — stubAgentVersion.
	// Failure path: the register endpoint returns 500 — exactly one attempt
	// should make the round trip; subsequent immediate calls must be
	// short-circuited by the failure backoff. This is the "don't replace
	// log spam with register spam" guarantee.
	var registerCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/register" {
			registerCount.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/usr/bin/true"}}
	t.Cleanup(stubAgentVersion(t))

	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-1", "rt-2"}}
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1"}
	d.runtimeIndex["rt-2"] = Runtime{ID: "rt-2"}

	d.handleRuntimeGone("rt-1")
	d.handleRuntimeGone("rt-2")

	if got := registerCount.Load(); got != 1 {
		t.Fatalf("register endpoint called %d times on failure path, want 1 (second call should be coalesced)", got)
	}
	// Local state pruning still happened for both, even though re-register
	// failed: the workspace is now empty, which workspaceSyncLoop will
	// retry on the next tick.
	if got := d.workspaces["ws-1"].runtimeIDs; len(got) != 0 {
		t.Fatalf("workspace runtimeIDs after failed recovery = %v, want []", got)
	}
}

func TestHandleWSHeartbeatAck_RuntimeGoneTriggersRecovery(t *testing.T) {
	// The WS path's twin of an HTTP 404 "runtime not found". When the server
	// flags a runtime as gone, the daemon must NOT record a freshness mark
	// — doing so would tell the HTTP heartbeat to skip its tick and let the
	// daemon keep believing the runtime is alive.
	fx := newHandleRuntimeGoneFixture(t)
	d := fx.daemon
	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-old"}}
	d.runtimeIndex["rt-old"] = Runtime{ID: "rt-old"}
	d.wsHBLastAck["rt-old"] = time.Now()

	d.handleWSHeartbeatAck(context.Background(), &HeartbeatResponse{
		RuntimeID:   "rt-old",
		Status:      "runtime_gone",
		RuntimeGone: true,
	})

	// handleRuntimeGone is fired asynchronously via `go`; spin briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		_, stillOld := d.runtimeIndex["rt-old"]
		_, gotNew := d.runtimeIndex["rt-new"]
		d.mu.Unlock()
		if !stillOld && gotNew {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, stillOld := d.runtimeIndex["rt-old"]; stillOld {
		t.Fatalf("rt-old not pruned after RuntimeGone ack")
	}
	if _, ok := d.wsHBLastAck["rt-old"]; ok {
		t.Fatalf("WS freshness mark not cleared for gone runtime — HTTP heartbeat would skip its tick")
	}
}

func TestHandleWSHeartbeatAck_NormalAckRecordsFreshness(t *testing.T) {
	t.Parallel()

	d := freshDaemon("")
	d.handleWSHeartbeatAck(context.Background(), &HeartbeatResponse{
		RuntimeID: "rt-1",
		Status:    "ok",
	})
	if !d.wsHeartbeatRecentlyAcked("rt-1") {
		t.Fatalf("normal ack should record WS freshness for rt-1")
	}
}

func TestHandleWSHeartbeatAck_EmptyAckIgnored(t *testing.T) {
	t.Parallel()

	d := freshDaemon("")
	d.handleWSHeartbeatAck(context.Background(), nil)
	d.handleWSHeartbeatAck(context.Background(), &HeartbeatResponse{RuntimeID: ""})
	// Should not panic, should not record any state.
	if len(d.wsHBLastAck) != 0 {
		t.Fatalf("empty ack recorded state: %v", d.wsHBLastAck)
	}
}

func TestWorkspaceNeedsRuntimeRecovery(t *testing.T) {
	t.Parallel()

	d := freshDaemon("")
	d.workspaces["ws-empty"] = &workspaceState{workspaceID: "ws-empty"}
	d.workspaces["ws-full"] = &workspaceState{workspaceID: "ws-full", runtimeIDs: []string{"rt-1"}}

	if !d.workspaceNeedsRuntimeRecovery("ws-empty") {
		t.Fatalf("ws-empty should need recovery")
	}
	if d.workspaceNeedsRuntimeRecovery("ws-full") {
		t.Fatalf("ws-full should NOT need recovery")
	}
	if d.workspaceNeedsRuntimeRecovery("ws-unknown") {
		t.Fatalf("untracked workspace should NOT need recovery")
	}
}

// multiProviderRegisterFixture mirrors handleRuntimeGoneFixture but speaks the
// upsert semantics of UpsertAgentRuntime: surviving providers keep their
// runtime IDs across re-registers, deleted ones get a fresh ID. The fake
// server is the source of truth and rewrites its own knowledge of which
// providers are alive each time a runtime is deleted.
//
// markDeleted(rid) emulates a UI Delete by removing the row server-side and
// returning a brand-new ID for that provider on the next register call.
type multiProviderRegisterFixture struct {
	daemon        *Daemon
	server        *httptest.Server
	registerCount *atomic.Int64
	mu            sync.Mutex
	// providerToID maps provider -> current server-side runtime ID. The fake
	// register handler reads/mutates this so the test reflects realistic
	// upsert behavior.
	providerToID map[string]string
	idCounter    int
}

func newMultiProviderRegisterFixture(t *testing.T, providers map[string]string) *multiProviderRegisterFixture {
	t.Helper()

	fx := &multiProviderRegisterFixture{
		providerToID: make(map[string]string, len(providers)),
	}
	for p, id := range providers {
		fx.providerToID[p] = id
	}

	var registerCount atomic.Int64
	fx.registerCount = &registerCount

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/daemon/register":
			registerCount.Add(1)
			fx.mu.Lock()
			runtimes := make([]Runtime, 0, len(fx.providerToID))
			for provider, id := range fx.providerToID {
				if id == "" {
					// Provider was marked deleted; mint a fresh ID
					// (the UpsertAgentRuntime INSERT branch).
					fx.idCounter++
					id = fmt.Sprintf("%s-new-%d", provider, fx.idCounter)
					fx.providerToID[provider] = id
				}
				runtimes = append(runtimes, Runtime{
					ID: id, Name: provider, Provider: provider, Status: "online",
				})
			}
			fx.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RegisterResponse{
				Runtimes: runtimes,
				Repos:    []RepoData{},
			})
		case strings.HasSuffix(r.URL.Path, "/recover-orphans"):
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.cfg.Agents = make(map[string]AgentEntry, len(providers))
	for p := range providers {
		d.cfg.Agents[p] = AgentEntry{Path: "/usr/bin/true"}
	}
	t.Cleanup(stubAgentVersion(t))
	fx.daemon = d
	fx.server = srv
	return fx
}

// markDeleted simulates server-side runtime deletion: the next register call
// will mint a new ID for this provider, matching the UI Delete + re-register
// path's UpsertAgentRuntime INSERT branch.
func (fx *multiProviderRegisterFixture) markDeleted(provider string) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.providerToID[provider] = ""
}

func TestHandleRuntimeGone_PartialWorkspaceRecoveryKeepsSibling(t *testing.T) {
	// Workspace has two providers, only one runtime is deleted. The siblings
	// must NOT end up duplicated in workspaceState.runtimeIDs — that would
	// leak through allRuntimeIDs(), deregister(), and re-recovery state.
	// This is the regression test for Finding #3 (register response is
	// authoritative for the workspace's runtime set, not an append).
	fx := newMultiProviderRegisterFixture(t, map[string]string{
		"claude": "rt-claude-1",
		"codex":  "rt-codex-1",
	})
	d := fx.daemon
	d.workspaces["ws-1"] = &workspaceState{
		workspaceID: "ws-1",
		runtimeIDs:  []string{"rt-claude-1", "rt-codex-1"},
	}
	d.runtimeIndex["rt-claude-1"] = Runtime{ID: "rt-claude-1", Provider: "claude"}
	d.runtimeIndex["rt-codex-1"] = Runtime{ID: "rt-codex-1", Provider: "codex"}

	// Only the claude runtime gets deleted server-side.
	fx.markDeleted("claude")
	d.handleRuntimeGone("rt-claude-1")

	got := append([]string(nil), d.workspaces["ws-1"].runtimeIDs...)
	if len(got) != 2 {
		t.Fatalf("workspace runtimeIDs has %d entries after partial recovery; want 2; got %v", len(got), got)
	}
	// Set comparison: must contain rt-codex-1 (surviving) and a freshly
	// minted claude id, with NO duplicates.
	seen := make(map[string]int, len(got))
	for _, id := range got {
		seen[id]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("duplicate runtime id %q (count=%d) after partial recovery: %v", id, count, got)
		}
	}
	if _, ok := seen["rt-codex-1"]; !ok {
		t.Fatalf("surviving codex runtime missing from workspace state after recovery: %v", got)
	}
	if _, ok := seen["rt-claude-1"]; ok {
		t.Fatalf("deleted claude runtime should not be in workspace state: %v", got)
	}
	// And the runtimeIndex must reflect the same: codex kept, claude-1 dropped.
	if _, ok := d.runtimeIndex["rt-claude-1"]; ok {
		t.Fatalf("rt-claude-1 still in runtimeIndex after deletion")
	}
	if _, ok := d.runtimeIndex["rt-codex-1"]; !ok {
		t.Fatalf("rt-codex-1 dropped from runtimeIndex during partial recovery")
	}
}

func TestHandleRuntimeGone_DistinctDeletionsWithinCoalesceWindowBothRecover(t *testing.T) {
	// Two sequential, distinct runtime deletions in the same workspace fired
	// within the 30s coalesce window. Each deletion must trigger its own
	// re-register: success on call #1 must NOT suppress call #2. Regression
	// for Finding #2 (success-case clear of reregisterNextAttempt).
	fx := newMultiProviderRegisterFixture(t, map[string]string{
		"claude": "rt-claude-1",
		"codex":  "rt-codex-1",
	})
	d := fx.daemon
	d.workspaces["ws-1"] = &workspaceState{
		workspaceID: "ws-1",
		runtimeIDs:  []string{"rt-claude-1", "rt-codex-1"},
	}
	d.runtimeIndex["rt-claude-1"] = Runtime{ID: "rt-claude-1", Provider: "claude"}
	d.runtimeIndex["rt-codex-1"] = Runtime{ID: "rt-codex-1", Provider: "codex"}

	// Sequential, NOT concurrent: the first call fully completes before the
	// second starts, so the in-flight set never collides.
	fx.markDeleted("claude")
	d.handleRuntimeGone("rt-claude-1")

	if got := fx.registerCount.Load(); got != 1 {
		t.Fatalf("after first deletion: register called %d times, want 1", got)
	}
	// Inspect the new claude id the fake assigned, so we can detect that
	// the second recovery actually ran register again.
	fx.mu.Lock()
	claudeIDAfterFirst := fx.providerToID["claude"]
	fx.mu.Unlock()

	// Now delete codex within the coalesce window (effectively t<1s after
	// the first recovery), simulating a user deleting a second runtime
	// shortly after the first.
	fx.markDeleted("codex")
	d.handleRuntimeGone("rt-codex-1")

	if got := fx.registerCount.Load(); got != 2 {
		t.Fatalf("after second distinct deletion: register called %d times, want 2 (coalesce window must clear on success)", got)
	}
	got := append([]string(nil), d.workspaces["ws-1"].runtimeIDs...)
	if len(got) != 2 {
		t.Fatalf("workspace runtimeIDs after both recoveries = %v, want 2 entries", got)
	}
	seen := make(map[string]int, len(got))
	for _, id := range got {
		seen[id]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("duplicate runtime id %q after sequential recoveries: %v", id, got)
		}
	}
	if _, ok := seen[claudeIDAfterFirst]; !ok {
		t.Fatalf("claude id from first recovery missing after second deletion of codex: have %v, expected to keep %q", got, claudeIDAfterFirst)
	}
}

func TestTryClaimRegisterSlot_FirstCallerClaims(t *testing.T) {
	t.Parallel()

	d := freshDaemon("")
	t0 := time.Now()
	if !d.tryClaimRegisterSlot("ws-1", t0, t0) {
		t.Fatalf("first caller should claim the slot, got false")
	}
}

func TestTryClaimRegisterSlot_StragglerBailsAfterSiblingSuccess(t *testing.T) {
	// Deterministic regression for the race the flaky CoalescesConcurrentCallers
	// test was catching: goroutine B enters at T0, goroutine A enters slightly
	// later, A claims the slot, runs register to success, clears the slot. B's
	// removeStaleRuntime then catches up and B reaches the coalesce gate. Without
	// the lastCompletedAt gate, B would see an empty slot and double-register.
	t.Parallel()

	d := freshDaemon("")
	t0 := time.Now()

	// A: enters at T0+1ms, claims slot, register completes at T0+50ms.
	aEntry := t0.Add(1 * time.Millisecond)
	if !d.tryClaimRegisterSlot("ws-1", aEntry, aEntry) {
		t.Fatalf("A: expected to claim, got bail")
	}
	d.recordRegisterCompletion("ws-1", t0.Add(50*time.Millisecond), nil)

	// B: entered at T0 (BEFORE A completed and cleared the slot) but only
	// arrives at the gate after A is done. Must bail on the lastCompletedAt
	// check.
	bEntry := t0
	bArrive := t0.Add(60 * time.Millisecond)
	if d.tryClaimRegisterSlot("ws-1", bEntry, bArrive) {
		t.Fatalf("B: same-wave straggler must NOT re-claim the slot after A's success")
	}
}

func TestTryClaimRegisterSlot_DistinctLaterEventClaims(t *testing.T) {
	// Companion to StragglerBailsAfterSiblingSuccess: a deletion event that
	// happens AFTER a prior register completed must still trigger its own
	// register. This is the property the on-success delete preserves and that
	// TestHandleRuntimeGone_DistinctDeletionsWithinCoalesceWindowBothRecover
	// validates at the integration layer.
	t.Parallel()

	d := freshDaemon("")
	t0 := time.Now()

	if !d.tryClaimRegisterSlot("ws-1", t0, t0) {
		t.Fatalf("first caller should claim")
	}
	d.recordRegisterCompletion("ws-1", t0.Add(50*time.Millisecond), nil)

	// A genuinely later event: entered AFTER the prior register completed.
	laterEntry := t0.Add(100 * time.Millisecond)
	if !d.tryClaimRegisterSlot("ws-1", laterEntry, laterEntry) {
		t.Fatalf("genuinely later event must be allowed to claim, got bail")
	}
}

func TestTryClaimRegisterSlot_FailureBackoffSuppressesRetries(t *testing.T) {
	// After a failed register, callers within the failure backoff window must
	// bail even if their entry time predates the failure (the daemon must not
	// replace a server-side log flood with a register flood). Callers past the
	// window are allowed to proceed.
	t.Parallel()

	d := freshDaemon("")
	t0 := time.Now()

	if !d.tryClaimRegisterSlot("ws-1", t0, t0) {
		t.Fatalf("first caller should claim")
	}
	failAt := t0.Add(50 * time.Millisecond)
	d.recordRegisterCompletion("ws-1", failAt, errors.New("boom"))

	within := failAt.Add(reregisterFailureBackoff / 2)
	if d.tryClaimRegisterSlot("ws-1", within, within) {
		t.Fatalf("call within failure backoff must be coalesced")
	}

	past := failAt.Add(reregisterFailureBackoff + time.Second)
	if !d.tryClaimRegisterSlot("ws-1", past, past) {
		t.Fatalf("call past failure backoff must be allowed to claim")
	}
}

func TestTryClaimRegisterSlot_StragglerAfterFailedSiblingRetriesPastBackoff(t *testing.T) {
	// Regression for the failure-path semantics gap the second review caught:
	// recordRegisterCompletion must NOT stamp lastCompletedAt on failure,
	// because a failed register has not covered any workspace state. A
	// same-wave straggler whose entryAt predates the failure but who only
	// reaches the gate after the failure backoff has expired must be allowed
	// to claim — otherwise the workspace stays unregistered until
	// workspaceSyncLoop notices, and that loop only fires when the workspace's
	// runtimeIDs fully drain (partial deletions wouldn't trigger it).
	t.Parallel()

	d := freshDaemon("")
	t0 := time.Now()

	// A: enters at T0+1ms, claims slot, register FAILS at T0+50ms. The
	// failure stamps reregisterNextAttempt = failAt + failureBackoff and
	// (per the fix) does NOT stamp lastCompletedAt.
	aEntry := t0.Add(1 * time.Millisecond)
	if !d.tryClaimRegisterSlot("ws-1", aEntry, aEntry) {
		t.Fatalf("A: expected to claim, got bail")
	}
	failAt := t0.Add(50 * time.Millisecond)
	d.recordRegisterCompletion("ws-1", failAt, errors.New("boom"))

	// B: entered at T0 (BEFORE A's failure) but was stuck on removeStaleRuntime
	// mutex contention; arrives at the gate at failAt + failureBackoff + 1s.
	// nextAttempt has expired; lastCompletedAt is unset; B must claim.
	bEntry := t0
	bArrive := failAt.Add(reregisterFailureBackoff + time.Second)
	if !d.tryClaimRegisterSlot("ws-1", bEntry, bArrive) {
		t.Fatalf("B: straggler whose entryAt predates a failed sibling must reclaim once failure backoff expires")
	}
}

func TestTryClaimRegisterSlot_PeerHoldingSlotForcesCoalesce(t *testing.T) {
	// While a peer holds an unfinished slot, callers must bail regardless of
	// the lastCompletedAt state.
	t.Parallel()

	d := freshDaemon("")
	t0 := time.Now()
	if !d.tryClaimRegisterSlot("ws-1", t0, t0) {
		t.Fatalf("first caller should claim")
	}
	// Peer is still running register; lastCompletedAt is not yet set, but
	// reregisterNextAttempt is t0 + coalesceWindow.
	if d.tryClaimRegisterSlot("ws-1", t0.Add(time.Millisecond), t0.Add(time.Millisecond)) {
		t.Fatalf("caller arriving while peer holds the slot must coalesce")
	}
}

func TestHandleRuntimeGone_RecoveryContextSurvivesCallerCancellation(t *testing.T) {
	// Regression for Finding #1: handleRuntimeGone must not use the per-
	// runtime heartbeat ctx for the register HTTP call. notifyRuntimeSetChanged
	// tears that ctx down as soon as we prune the dead runtime, so forwarding
	// it would self-cancel the in-flight register.
	//
	// We assert by inspecting the register handler's request context: it
	// must not be Done when the daemon's rootCtx is alive, regardless of what
	// upstream contexts (heartbeat, poller, WS) are doing.
	var observedCancelled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/register" {
			// Inspect the inbound request ctx. If handleRuntimeGone had
			// forwarded a cancelled caller ctx, this would be Done.
			select {
			case <-r.Context().Done():
				observedCancelled.Store(true)
			default:
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RegisterResponse{
				Runtimes: []Runtime{{ID: "rt-new", Name: "claude", Provider: "claude", Status: "online"}},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/usr/bin/true"}}
	t.Cleanup(stubAgentVersion(t))

	// rootCtx is what handleRuntimeGone uses for recovery. We keep it alive.
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()
	d.rootCtx = rootCtx

	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-old"}}
	d.runtimeIndex["rt-old"] = Runtime{ID: "rt-old"}

	d.handleRuntimeGone("rt-old")

	if observedCancelled.Load() {
		t.Fatalf("register HTTP call ran with a cancelled context — recovery would self-cancel under runtime-set churn")
	}
	if got := d.workspaces["ws-1"].runtimeIDs; len(got) != 1 || got[0] != "rt-new" {
		t.Fatalf("workspace runtimeIDs after recovery = %v, want [rt-new]", got)
	}
}

func TestHandleRuntimeGone_RecoveryContextStopsOnDaemonShutdown(t *testing.T) {
	// Companion to RecoveryContextSurvivesCallerCancellation: when the daemon
	// IS shutting down, recovery must abort promptly instead of holding the
	// HTTP call open until its 30s client timeout. We bound the server
	// handler with a short safety timeout so test cleanup never hangs on a
	// stuck connection — the assertion is on the daemon-side return time,
	// not on server-side context propagation.
	registerEntered := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/register" {
			select {
			case registerEntered <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/usr/bin/true"}}
	t.Cleanup(stubAgentVersion(t))

	rootCtx, rootCancel := context.WithCancel(context.Background())
	t.Cleanup(rootCancel)
	d.rootCtx = rootCtx

	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-old"}}
	d.runtimeIndex["rt-old"] = Runtime{ID: "rt-old"}

	done := make(chan struct{})
	go func() {
		d.handleRuntimeGone("rt-old")
		close(done)
	}()

	select {
	case <-registerEntered:
	case <-time.After(2 * time.Second):
		t.Fatalf("register endpoint was never reached")
	}

	rootCancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("handleRuntimeGone did not abort after daemon root context cancellation")
	}
}
