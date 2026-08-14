package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// batchFixture wires a Daemon against a fake server that serves a configurable
// workspace list, per-workspace runtime profiles, and a register endpoint that
// records the runtime payload each workspace was registered with. It also
// counts `<cli> --version` probes per executable path so a test can assert the
// daemon probed the machine's built-in CLIs once per sync instead of once per
// workspace (MUL-5225).
type batchFixture struct {
	daemon *Daemon
	server *httptest.Server

	mu sync.Mutex
	// workspaces is the workspace list the fake server returns.
	workspaces []WorkspaceInfo
	// profiles maps a workspace ID to the custom runtime profiles the server
	// reports for it.
	profiles map[string][]RuntimeProfile
	// registered records, in call order, the workspace ID and the "type" of
	// every runtime in that Register call's payload.
	registered []registeredCall
	// probes counts detectAgentVersion calls per executable path.
	probes map[string]int
	// probeVersion is what the version probe stub reports; defaults to "9.9.9".
	// Changing it mid-test simulates an in-place agent CLI upgrade.
	probeVersion string
	// probeErr, when set, is consulted by the version probe stub before it
	// succeeds. It receives the executable path and the 1-based attempt count
	// for that path, and returns a non-nil error to fail that attempt.
	probeErr func(path string, attempt int) error
	// registerFail, when non-nil, makes /api/daemon/register return 500. A
	// non-empty string key scopes the failure to that workspace ID; the empty
	// string key fails every workspace. Used to exercise the discovery retry
	// paths (MUL-5439).
	registerFail map[string]bool
	// profilesFail makes the runtime-profiles route return 500, reproducing the
	// best-effort profile fetch failing while a discovery-driven registration
	// is in flight.
	profilesFail bool
	// deregistered accumulates every runtime ID sent to /api/daemon/deregister,
	// so a test can assert the server was actually told to stop routing work.
	deregistered []string
	// offlineReasons is the server's view of WHY each row is offline. The
	// register upsert overwrites it (see the register handler), which is what
	// makes "the reason survived a late healthy register" testable.
	offlineReasons map[string]RuntimeOfflineReason
	// registerDelay, when non-zero, makes the register handler sleep before
	// recording the call, widening the window in which two unserialized
	// register calls for the same workspace would overlap.
	registerDelay time.Duration
	// registerGate, when set, is invoked by the register handler (off fx.mu)
	// with the workspace ID before the response is built. A test blocks in it to
	// hold one register in flight while it drives another state change, which is
	// how the "response predates a verdict" interleaves are made deterministic
	// instead of timing-dependent.
	registerGate func(workspaceID string)
	// registerInFlight / registerMaxInFlight track how many register handlers
	// run at once, so a test can assert same-workspace serialization.
	registerInFlight    int
	registerMaxInFlight int
	// deregisterGate, when set, is invoked by the deregister handler (off fx.mu)
	// with the runtime IDs before they are applied. A test blocks in it to hold
	// one cleanup in flight while it drives a recovery, which is how the
	// "an older cleanup outlives a newer recovery" interleave is made
	// deterministic instead of timing-dependent.
	deregisterGate func(runtimeIDs []string)
	// stableRuntimeIDs makes the register handler return ONE id per
	// (workspace, runtime) rather than a fresh one per call, mirroring the
	// server's UpsertAgentRuntime: re-registering a provider returns the row
	// that already exists. A cleanup can only undo a recovery when the two name
	// the same row, so that interleave is invisible without this.
	stableRuntimeIDs bool
	runtimeIDs       map[string]string // "<workspace>|<profile_id or type>" -> runtime ID
	// online is the server's view of each runtime row: register puts it online,
	// deregister takes it offline. This is what a test asserts on when the
	// question is "which side won", rather than "was a call made".
	online map[string]bool
}

// enableStableRuntimeIDs must be called before the first registration.
func (fx *batchFixture) enableStableRuntimeIDs() {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.stableRuntimeIDs = true
}

// setDeregisterGate installs a hook the deregister handler calls before the
// rows are taken offline.
func (fx *batchFixture) setDeregisterGate(fn func(runtimeIDs []string)) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.deregisterGate = fn
}

// runtimeIDFor returns the server-side runtime ID for a workspace's provider
// (or profile), which only exists under enableStableRuntimeIDs.
func (fx *batchFixture) runtimeIDFor(workspaceID, key string) string {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.runtimeIDs[workspaceID+"|"+key]
}

// runtimeOfflineReason returns the server's stored cause for a runtime row, or
// false when the row carries none — which is what "offline for an unknown
// reason" looks like to every admission path.
func (fx *batchFixture) runtimeOfflineReason(id string) (RuntimeOfflineReason, bool) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	reason, ok := fx.offlineReasons[id]
	return reason, ok
}

// runtimeOnline reports the server's current status for a runtime row.
func (fx *batchFixture) runtimeOnline(id string) bool {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.online[id]
}

// failRegister toggles register failure for every workspace.
func (fx *batchFixture) failRegister(fail bool) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	if fx.registerFail == nil {
		fx.registerFail = make(map[string]bool)
	}
	fx.registerFail[""] = fail
}

// failRegisterFor toggles register failure for one workspace.
func (fx *batchFixture) failRegisterFor(workspaceID string, fail bool) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	if fx.registerFail == nil {
		fx.registerFail = make(map[string]bool)
	}
	fx.registerFail[workspaceID] = fail
}

func (fx *batchFixture) registerShouldFail(workspaceID string) bool {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.registerFail[""] || fx.registerFail[workspaceID]
}

// failProfiles toggles failure of the custom runtime profiles fetch.
func (fx *batchFixture) failProfiles(fail bool) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.profilesFail = fail
}

func (fx *batchFixture) profilesShouldFail() bool {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.profilesFail
}

type registeredCall struct {
	workspaceID string
	types       []string
	// versions maps runtime type -> the version that call reported, so a test
	// can assert the server was told about an in-place CLI upgrade.
	versions map[string]string
	// names maps runtime type -> the display name that call reported, so a
	// test can assert the name a user will see for a runtime identity.
	names map[string]string
}

func (fx *batchFixture) setWorkspaces(ws ...WorkspaceInfo) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.workspaces = ws
}

func (fx *batchFixture) probeCount(path string) int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.probes[path]
}

// setProbeVersion changes what `<cli> --version` reports from now on.
func (fx *batchFixture) setProbeVersion(version string) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.probeVersion = version
}

// registeredVersionFor returns the version the LAST Register call for a
// workspace reported for a provider.
func (fx *batchFixture) registeredVersionFor(workspaceID, provider string) string {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	var version string
	for _, call := range fx.registered {
		if call.workspaceID == workspaceID {
			version = call.versions[provider]
		}
	}
	return version
}

func (fx *batchFixture) setProbeErr(fn func(path string, attempt int) error) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.probeErr = fn
}

// registeredNameFor returns the display name the last Register call for a
// workspace reported for a runtime type.
func (fx *batchFixture) registeredNameFor(workspaceID, runtimeType string) string {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	var name string
	for _, call := range fx.registered {
		if call.workspaceID == workspaceID {
			name = call.names[runtimeType]
		}
	}
	return name
}

// registrationFor returns the runtime types registered for a workspace, and
// how many Register calls that workspace received.
func (fx *batchFixture) registrationFor(workspaceID string) ([]string, int) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	var types []string
	var calls int
	for _, call := range fx.registered {
		if call.workspaceID == workspaceID {
			types = call.types
			calls++
		}
	}
	return types, calls
}

// deregisteredCount returns how many runtime IDs were deregistered server-side.
func (fx *batchFixture) deregisteredCount() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return len(fx.deregistered)
}

// deregisteredIDs copies the runtime IDs deregistered server-side, in order.
func (fx *batchFixture) deregisteredIDs() []string {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return append([]string(nil), fx.deregistered...)
}

// setRegisterDelay makes every register call linger, so a test can detect two
// of them overlapping when they should be serialized.
// setRegisterGate installs a hook the register handler calls before responding.
func (fx *batchFixture) setRegisterGate(fn func(workspaceID string)) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.registerGate = fn
}

func (fx *batchFixture) setRegisterDelay(d time.Duration) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.registerDelay = d
}

// maxRegisterInFlight reports the peak number of concurrently running
// register handlers.
func (fx *batchFixture) maxRegisterInFlight() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.registerMaxInFlight
}

func (fx *batchFixture) registerCallCount() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return len(fx.registered)
}

func newBatchFixture(t *testing.T) *batchFixture {
	t.Helper()
	fx := &batchFixture{
		profiles:     make(map[string][]RuntimeProfile),
		probes:       make(map[string]int),
		probeVersion: "9.9.9",
		runtimeIDs:   make(map[string]string),
		online:       make(map[string]bool),
	}

	origDetect := detectAgentVersion
	origCheck := checkAgentMinVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetect
		checkAgentMinVersion = origCheck
	})
	detectAgentVersion = func(_ context.Context, path string) (string, error) {
		fx.mu.Lock()
		fx.probes[path]++
		attempt := fx.probes[path]
		probeErr := fx.probeErr
		version := fx.probeVersion
		fx.mu.Unlock()
		if probeErr != nil {
			if err := probeErr(path, attempt); err != nil {
				return "", err
			}
		}
		return version, nil
	}
	checkAgentMinVersion = func(_, _ string) error { return nil }

	var runtimeSeq atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/daemon/workspaces":
			fx.mu.Lock()
			list := append([]WorkspaceInfo(nil), fx.workspaces...)
			fx.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)
		case r.URL.Path == "/api/daemon/register":
			var body struct {
				WorkspaceID string              `json:"workspace_id"`
				Runtimes    []map[string]string `json:"runtimes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			fx.mu.Lock()
			fx.registerInFlight++
			if fx.registerInFlight > fx.registerMaxInFlight {
				fx.registerMaxInFlight = fx.registerInFlight
			}
			delay := fx.registerDelay
			gate := fx.registerGate
			fx.mu.Unlock()
			if gate != nil {
				gate(body.WorkspaceID)
			}
			defer func() {
				fx.mu.Lock()
				fx.registerInFlight--
				fx.mu.Unlock()
			}()
			if delay > 0 {
				time.Sleep(delay)
			}
			if fx.registerShouldFail(body.WorkspaceID) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"injected register failure"}`))
				return
			}
			call := registeredCall{
				workspaceID: body.WorkspaceID,
				versions:    map[string]string{},
				names:       map[string]string{},
			}
			var resp RegisterResponse
			fx.mu.Lock()
			for _, rt := range body.Runtimes {
				call.types = append(call.types, rt["type"])
				call.versions[rt["type"]] = rt["version"]
				call.names[rt["type"]] = rt["name"]
				// Mirror UpsertAgentRuntime: a re-register of a row that already
				// exists returns that row's ID rather than minting a new one.
				id := "rt-" + strconv.Itoa(int(runtimeSeq.Add(1)))
				if fx.stableRuntimeIDs {
					key := body.WorkspaceID + "|" + rt["type"]
					if rt["profile_id"] != "" {
						key = body.WorkspaceID + "|" + rt["profile_id"]
					}
					if existing, ok := fx.runtimeIDs[key]; ok {
						id = existing
					} else {
						fx.runtimeIDs[key] = id
					}
				}
				fx.online[id] = true
				// The real upsert overwrites metadata wholesale, so a register
				// drops any recorded reason. This is the mechanism the revived-row
				// cleanup has to compensate for.
				delete(fx.offlineReasons, id)
				resp.Runtimes = append(resp.Runtimes, Runtime{
					ID:        id,
					Name:      rt["name"],
					Provider:  rt["type"],
					Status:    "online",
					ProfileID: rt["profile_id"],
				})
			}
			fx.registered = append(fx.registered, call)
			fx.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/api/daemon/deregister":
			var body struct {
				RuntimeIDs     []string                        `json:"runtime_ids"`
				OfflineReasons map[string]RuntimeOfflineReason `json:"offline_reasons"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			fx.mu.Lock()
			gate := fx.deregisterGate
			fx.mu.Unlock()
			if gate != nil {
				gate(body.RuntimeIDs)
			}
			fx.mu.Lock()
			fx.deregistered = append(fx.deregistered, body.RuntimeIDs...)
			for _, id := range body.RuntimeIDs {
				fx.online[id] = false
				// Mirror the server: the reason is stored on the runtime row, and
				// a deregister without one leaves whatever was there — which is
				// how the "revived then cleaned up" interleave loses it.
				if reason, ok := body.OfflineReasons[id]; ok {
					if fx.offlineReasons == nil {
						fx.offlineReasons = make(map[string]RuntimeOfflineReason)
					}
					fx.offlineReasons[id] = reason
				}
			}
			fx.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/runtime-profiles"):
			if fx.profilesShouldFail() {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"injected profiles failure"}`))
				return
			}
			workspaceID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/daemon/workspaces/"), "/runtime-profiles")
			fx.mu.Lock()
			profiles := fx.profiles[workspaceID]
			fx.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RuntimeProfilesResponse{
				WorkspaceID:     workspaceID,
				RuntimeProfiles: profiles,
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.profileLaunchSpecs = make(map[string]profileLaunchSpec)
	fx.daemon = d
	fx.server = srv
	return fx
}

// TestSyncWorkspaces_ProbesBuiltinCLIsOncePerBatch is the MUL-5225 regression:
// registering N workspaces must execute each built-in agent CLI's `--version`
// once for the machine, not once per workspace, while still sending one
// Register call per workspace with the full built-in payload.
func TestSyncWorkspaces_ProbesBuiltinCLIsOncePerBatch(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{
		"claude": {Path: "/fake/claude"},
		"codex":  {Path: "/fake/codex"},
	}
	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	for _, path := range []string{"/fake/claude", "/fake/codex"} {
		if got := fx.probeCount(path); got != 1 {
			t.Errorf("probed %s %d times, want 1 (built-ins are machine-level, not per-workspace)", path, got)
		}
	}

	// Sharing the probe must not collapse the registrations themselves: both
	// workspaces still register, each with the same built-in runtimes.
	if got := fx.registerCallCount(); got != 2 {
		t.Fatalf("got %d Register calls, want 2 (one per workspace)", got)
	}
	for _, workspaceID := range []string{"ws-1", "ws-2"} {
		types, calls := fx.registrationFor(workspaceID)
		if calls != 1 {
			t.Errorf("%s registered %d times, want 1", workspaceID, calls)
		}
		sort.Strings(types)
		if len(types) != 2 || types[0] != "claude" || types[1] != "codex" {
			t.Errorf("%s registered runtimes %v, want [claude codex]", workspaceID, types)
		}
	}
}

// TestSyncWorkspaces_CustomProfilesDoNotLeakAcrossWorkspaces guards the sharing
// mechanism: the batch built-in payload is reused by reference-free copy, so a
// workspace-scoped custom runtime profile must land only in its own
// registration.
func TestSyncWorkspaces_CustomProfilesDoNotLeakAcrossWorkspaces(t *testing.T) {
	fx := newBatchFixture(t)
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/fake/claude"}}
	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	withProfile, _ := fx.registrationFor("ws-1")
	sort.Strings(withProfile)
	if len(withProfile) != 2 || withProfile[0] != "claude" || withProfile[1] != "codex" {
		t.Errorf("ws-1 registered %v, want its built-in plus its custom profile [claude codex]", withProfile)
	}

	withoutProfile, _ := fx.registrationFor("ws-2")
	if len(withoutProfile) != 1 || withoutProfile[0] != "claude" {
		t.Errorf("ws-2 registered %v, want only the built-in [claude]; ws-1's profile leaked", withoutProfile)
	}
}

// TestSyncWorkspaces_ReprobesOnNextSync pins the refresh semantics the sharing
// must not break: the payload is scoped to one sync, so a workspace that shows
// up later re-detects versions and picks up an in-place CLI upgrade.
func TestSyncWorkspaces_ReprobesOnNextSync(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/fake/claude"}}

	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if got := fx.probeCount("/fake/claude"); got != 1 {
		t.Fatalf("first sync probed %d times, want 1", got)
	}

	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if got := fx.probeCount("/fake/claude"); got != 2 {
		t.Fatalf("second sync probed %d times total, want 2 (one fresh probe for the new workspace)", got)
	}
}

// TestSyncWorkspaces_SkipsProbeWhenNothingToRegister keeps the periodic sync
// free of side effects. Every workspace is already tracked and healthy, so the
// lazy probe must never fire — this is what stops a 30-minute sync from
// re-executing agent CLI wrappers on a steady-state daemon.
func TestSyncWorkspaces_SkipsProbeWhenNothingToRegister(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/fake/claude"}}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-1"}, "", nil, nil)
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1", Provider: "claude"}

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if got := fx.probeCount("/fake/claude"); got != 0 {
		t.Fatalf("steady-state sync probed %d times, want 0", got)
	}
}

// TestRegisterRuntimesForWorkspace_ProbesOnStandaloneCall pins the other half of
// the contract: a standalone registration (runtime_gone re-register, profile
// drift refresh, recovery retry) still runs its own probe round, so it reports
// the CLI version live at that moment rather than one cached from startup.
func TestRegisterRuntimesForWorkspace_ProbesOnStandaloneCall(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/fake/claude"}}

	for i := 1; i <= 2; i++ {
		if _, _, _, err := d.registerRuntimesForWorkspaceLocked(context.Background(), "ws-1"); err != nil {
			t.Fatalf("register #%d: %v", i, err)
		}
		if got := fx.probeCount("/fake/claude"); got != i {
			t.Fatalf("after %d standalone registrations, probed %d times; want %d", i, got, i)
		}
	}
}

// stubProbeRetry shrinks the version-probe retry knobs so a test can exercise
// the retry path without paying the production delay, and can classify a probe
// as "slow" (not worth retrying) without sleeping for a real second.
func stubProbeRetry(t *testing.T, delay, window time.Duration) {
	t.Helper()
	origDelay, origWindow := runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow
	t.Cleanup(func() {
		runtimeVersionProbeRetryDelay = origDelay
		runtimeVersionProbeRetryWindow = origWindow
	})
	runtimeVersionProbeRetryDelay = delay
	runtimeVersionProbeRetryWindow = window
}

// TestSyncWorkspaces_RetriesFailedProbeForWholeBatch is the fail-once
// regression for the batch reuse. One probe round now serves every workspace
// the sync registers, so a single transient `--version` failure would otherwise
// drop that provider from ALL of them at once — and nothing would restore it,
// because workspaceNeedsRuntimeRecovery only retries a workspace that lost
// every runtime. A fast failure gets one more attempt before the provider is
// dropped.
func TestSyncWorkspaces_RetriesFailedProbeForWholeBatch(t *testing.T) {
	fx := newBatchFixture(t)
	stubProbeRetry(t, time.Millisecond, time.Second)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{
		"claude": {Path: "/fake/claude"},
		"codex":  {Path: "/fake/codex"},
	}
	// codex fails its first attempt only — the transient shape of an in-place
	// CLI upgrade briefly removing the binary.
	fx.setProbeErr(func(path string, attempt int) error {
		if path == "/fake/codex" && attempt == 1 {
			return errors.New("fork/exec: resource temporarily unavailable")
		}
		return nil
	})
	fx.setWorkspaces(
		WorkspaceInfo{ID: "ws-1", Name: "one"},
		WorkspaceInfo{ID: "ws-2", Name: "two"},
	)

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if got := fx.probeCount("/fake/codex"); got != 2 {
		t.Errorf("probed /fake/codex %d times, want 2 (one failure + one retry)", got)
	}
	// The retry is scoped to the provider that failed: a healthy CLI is not
	// re-probed just because a sibling stumbled.
	if got := fx.probeCount("/fake/claude"); got != 1 {
		t.Errorf("probed /fake/claude %d times, want 1 (only the failed provider retries)", got)
	}
	for _, workspaceID := range []string{"ws-1", "ws-2"} {
		types, _ := fx.registrationFor(workspaceID)
		sort.Strings(types)
		if len(types) != 2 || types[0] != "claude" || types[1] != "codex" {
			t.Errorf("%s registered %v, want [claude codex]; one transient probe failure cost the whole batch a runtime", workspaceID, types)
		}
	}
}

// TestDetectBuiltinRuntimes_DropsProviderAfterRetriesExhausted keeps the retry
// bounded: a provider that keeps failing is still dropped from the payload
// after runtimeVersionProbeAttempts, and the healthy providers still register.
func TestDetectBuiltinRuntimes_DropsProviderAfterRetriesExhausted(t *testing.T) {
	fx := newBatchFixture(t)
	stubProbeRetry(t, time.Millisecond, time.Second)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{
		"claude": {Path: "/fake/claude"},
		"codex":  {Path: "/fake/codex"},
	}
	fx.setProbeErr(func(path string, _ int) error {
		if path == "/fake/codex" {
			return errors.New("no such file or directory")
		}
		return nil
	})

	runtimes, _, _ := d.detectBuiltinRuntimes(context.Background())

	if got := fx.probeCount("/fake/codex"); got != runtimeVersionProbeAttempts {
		t.Errorf("probed /fake/codex %d times, want %d (retry must stay bounded)", got, runtimeVersionProbeAttempts)
	}
	if len(runtimes) != 1 || runtimes[0]["type"] != "claude" {
		t.Errorf("detected %v, want only claude", runtimes)
	}
}

// TestDetectBuiltinRuntimes_DoesNotRetrySlowProbe protects registration
// latency. A probe that burned its whole timeout is a hung CLI, not a hiccup;
// retrying it would double the round's worst case, which is the latency that
// used to strand the desktop runtime step in its empty state (MUL-5119).
func TestDetectBuiltinRuntimes_DoesNotRetrySlowProbe(t *testing.T) {
	fx := newBatchFixture(t)
	const probeWindow = 20 * time.Millisecond
	stubProbeRetry(t, time.Millisecond, probeWindow)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}
	fx.setProbeErr(func(path string, _ int) error {
		time.Sleep(2 * probeWindow)
		return errors.New("signal: killed")
	})

	if runtimes, _, _ := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
		t.Fatalf("detected %v, want none", runtimes)
	}
	if got := fx.probeCount("/fake/codex"); got != 1 {
		t.Errorf("probed /fake/codex %d times, want 1 (a probe that ran to its timeout is not retried)", got)
	}
}

// vanishedPinnedPath lays out the MUL-4486 shape a probe retry has to reckon
// with: a stable command name that resolves on PATH to a runnable stub, plus
// the pinned absolute path an in-place upgrade already deleted. It returns the
// missing pinned path and the path the self-heal re-resolves to.
func vanishedPinnedPath(t *testing.T) (missing, healed string) {
	t.Helper()
	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	writeExecStub(t, filepath.Join(stableBin, "codex"))
	// Prepend rather than replace: exec.LookPath takes the first match, so the
	// stub still wins deterministically over any codex installed on the host,
	// while goroutines left running by earlier tests can still resolve git/sh.
	t.Setenv("PATH", stableBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// An unsupported shell disables the login-shell fallback, which would only
	// run if the lookup above missed.
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	return filepath.Join(root, "gone", "codex"), canonicalExecutablePath(filepath.Join(stableBin, "codex"))
}

// countingVersionProbe swaps detectAgentVersion for a stub that counts calls
// and answers per path, so a test can assert how many `--version` executions a
// probe round actually cost.
func countingVersionProbe(t *testing.T, answer func(path string) (string, error)) *atomic.Int32 {
	t.Helper()
	origDetect := detectAgentVersion
	origCheck := checkAgentMinVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetect
		checkAgentMinVersion = origCheck
	})
	var probes atomic.Int32
	detectAgentVersion = func(_ context.Context, path string) (string, error) {
		probes.Add(1)
		return answer(path)
	}
	checkAgentMinVersion = func(_, _ string) error { return nil }
	return &probes
}

// TestDetectBuiltinRuntimes_DoesNotRetryWhenSelfHealBurnsTheWindow keeps the
// slow-probe guard honest about where an attempt's time actually goes. When the
// pinned path has vanished, resolveAgentEntry runs its own version probe on the
// re-resolved candidate (MUL-4486) — which can burn the full 10s timeout on its
// own. Timing only the outer probe would read the attempt as a fast failure
// (the stale path fails instantly), retry, and pay the slow self-heal a second
// time — exactly the doubled worst case the guard exists to prevent.
func TestDetectBuiltinRuntimes_DoesNotRetryWhenSelfHealBurnsTheWindow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH/exec-bit stub layout is POSIX-specific")
	}
	const probeWindow = 20 * time.Millisecond
	stubProbeRetry(t, time.Millisecond, probeWindow)
	missing, _ := vanishedPinnedPath(t)
	probes := countingVersionProbe(t, func(path string) (string, error) {
		if path == missing {
			// The stale pinned path is gone: this fails immediately.
			return "", errors.New("no such file or directory")
		}
		// The re-resolved candidate hangs and dies on its own timeout.
		time.Sleep(2 * probeWindow)
		return "", errors.New("signal: killed")
	})

	d := freshDaemon("")
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: missing, Command: "codex"}}

	if runtimes, _, _ := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
		t.Fatalf("detected %v, want none", runtimes)
	}
	if got := probes.Load(); got != 2 {
		t.Errorf("ran %d version probes, want 2 (one self-heal + one outer probe); the retry window must cover the whole attempt, not just the outer probe", got)
	}
}

// TestDetectBuiltinRuntimes_SelfHealRejectionIsABelowMinimumVerdict covers the
// downgrade shape a version manager produces: the pinned path is deleted and the
// command now resolves to a binary below the minimum supported version.
//
// The self-heal refuses to adopt it — correctly, it must never be launched — but
// that refusal IS the below-minimum verdict, and swallowing it made the outer
// probe fall back to the vanished path, fail, and report "version detection
// failed" instead. That reads as transient by design, which leaves the runtime
// online and claiming tasks for a CLI that cannot start.
func TestDetectBuiltinRuntimes_SelfHealRejectionIsABelowMinimumVerdict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH/exec-bit stub layout is POSIX-specific")
	}
	stubProbeRetry(t, time.Millisecond, time.Second)
	missing, healed := vanishedPinnedPath(t)
	probes := countingVersionProbe(t, func(path string) (string, error) {
		if path == missing {
			return "", errors.New("no such file or directory")
		}
		return "0.0.1", nil
	})
	checkAgentMinVersion = func(_, version string) error {
		if version == "0.0.1" {
			return &agent.BelowMinimumError{AgentType: "codex", Detected: version, Minimum: "1.0.0"}
		}
		return nil
	}

	d := freshDaemon("")
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: missing, Command: "codex"}}

	runtimes, belowMin, _ := d.detectBuiltinRuntimes(context.Background())
	if len(runtimes) != 0 {
		t.Fatalf("detected %v (healed path %q), want none: a below-minimum candidate must not be adopted", runtimes, healed)
	}
	// The refusal is the verdict, not a probe failure. Without carrying it out of
	// the heal, the loop goes on to probe the vanished pinned path, can only
	// report "version detection failed", and the runtime stays online serving a
	// CLI that cannot launch.
	if !strings.Contains(belowMin["codex"].reason, "0.0.1") || !strings.Contains(belowMin["codex"].reason, "below minimum") {
		t.Errorf("demotable verdict = %v, want codex condemned for being below minimum at 0.0.1", belowMin)
	}
	// Below-minimum keeps today's behaviour on the trigger paths: it takes the
	// runtime offline but does not carry the "a human must repair this" code
	// that refuses new work (MUL-6164 scoped that to the unusable verdict).
	if belowMin["codex"].offline != nil {
		t.Errorf("below-minimum verdict must not carry an offline reason: %+v", belowMin["codex"].offline)
	}
	// Unlike the direct below-minimum case, the heal verdict gets the bounded
	// fast-failure retry before it is returned: the heal re-resolves PATH per
	// attempt, and mid-upgrade a stale sibling install can shadow the
	// not-yet-published new binary — so the demotable verdict is only returned
	// once the retry reached the same conclusion. One probe per attempt, both
	// on the healed candidate (the outer probe of the vanished path never runs).
	if got := probes.Load(); got != 2 {
		t.Errorf("ran %d version probes, want 2 (one per bounded attempt)", got)
	}
}

// TestDetectBuiltinRuntimes_DoesNotRetryMinVersionRejection pins the other
// no-retry case: the minimum-version verdict is a pure function of the detected
// version, so a second probe would reach the same conclusion.
func TestDetectBuiltinRuntimes_DoesNotRetryMinVersionRejection(t *testing.T) {
	fx := newBatchFixture(t)
	stubProbeRetry(t, time.Millisecond, time.Second)
	checkAgentMinVersion = func(provider, version string) error {
		if provider == "codex" {
			return &agent.BelowMinimumError{AgentType: provider, Detected: version, Minimum: "1.0.0"}
		}
		return nil
	}
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}

	if runtimes, _, _ := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
		t.Fatalf("detected %v, want none", runtimes)
	}
	if got := fx.probeCount("/fake/codex"); got != 1 {
		t.Errorf("probed /fake/codex %d times, want 1 (a below-minimum version is not retried)", got)
	}
}

// TestRegisterRuntimesForWorkspaceBatch_DoesNotMutateSharedPayload asserts the
// callee treats the shared built-in payload as read-only. Without the copy, the
// first workspace's custom profile would be appended into the slice the batch
// hands to every later workspace.
func TestRegisterRuntimesForWorkspaceBatch_DoesNotMutateSharedPayload(t *testing.T) {
	fx := newBatchFixture(t)
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{}
	fx.profiles["ws-1"] = []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}

	builtins := []map[string]string{
		{"name": "Claude Code", "type": "claude", "version": "9.9.9", "status": "online"},
	}
	if _, _, err := d.registerRuntimesForWorkspaceBatchLocked(context.Background(), "ws-1", builtins); err != nil {
		t.Fatalf("batch register: %v", err)
	}

	if len(builtins) != 1 || builtins[0]["type"] != "claude" {
		t.Fatalf("shared built-in payload was mutated: %v", builtins)
	}
}
