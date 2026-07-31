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

func (fx *batchFixture) setProbeErr(fn func(path string, attempt int) error) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.probeErr = fn
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

func (fx *batchFixture) registerCallCount() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return len(fx.registered)
}

func newBatchFixture(t *testing.T) *batchFixture {
	t.Helper()
	fx := &batchFixture{
		profiles: make(map[string][]RuntimeProfile),
		probes:   make(map[string]int),
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
		fx.mu.Unlock()
		if probeErr != nil {
			if err := probeErr(path, attempt); err != nil {
				return "", err
			}
		}
		return "9.9.9", nil
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
			if fx.registerShouldFail(body.WorkspaceID) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"injected register failure"}`))
				return
			}
			call := registeredCall{workspaceID: body.WorkspaceID}
			var resp RegisterResponse
			for _, rt := range body.Runtimes {
				call.types = append(call.types, rt["type"])
				resp.Runtimes = append(resp.Runtimes, Runtime{
					ID:        "rt-" + strconv.Itoa(int(runtimeSeq.Add(1))),
					Name:      rt["name"],
					Provider:  rt["type"],
					Status:    "online",
					ProfileID: rt["profile_id"],
				})
			}
			fx.mu.Lock()
			fx.registered = append(fx.registered, call)
			fx.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
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
		if _, _, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1"); err != nil {
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

	runtimes := d.detectBuiltinRuntimes(context.Background())

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

	if runtimes := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
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

	if runtimes := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
		t.Fatalf("detected %v, want none", runtimes)
	}
	if got := probes.Load(); got != 2 {
		t.Errorf("ran %d version probes, want 2 (one self-heal + one outer probe); the retry window must cover the whole attempt, not just the outer probe", got)
	}
}

// TestDetectBuiltinRuntimes_BoundsRetryWhenSelfHealRejectsVersion documents the
// cost of the case the retry cannot tell apart: a self-heal candidate rejected
// by the min-version gate leaves the stale path in place, and the outer probe
// then fails fast — indistinguishable from a transient failure, so the attempt
// is retried. The verdict is deterministic, so that retry is wasted work; what
// matters is that it stays bounded and cheap (every probe in it fails fast).
func TestDetectBuiltinRuntimes_BoundsRetryWhenSelfHealRejectsVersion(t *testing.T) {
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
			return errors.New("version too old")
		}
		return nil
	}

	d := freshDaemon("")
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: missing, Command: "codex"}}

	if runtimes := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
		t.Fatalf("detected %v (healed path %q), want none: a below-minimum candidate must not be adopted", runtimes, healed)
	}
	// Two attempts, each paying one self-heal probe plus one outer probe.
	if got := probes.Load(); got != int32(2*runtimeVersionProbeAttempts) {
		t.Errorf("ran %d version probes, want %d (%d bounded attempts)", got, 2*runtimeVersionProbeAttempts, runtimeVersionProbeAttempts)
	}
}

// TestDetectBuiltinRuntimes_DoesNotRetryMinVersionRejection pins the other
// no-retry case: the minimum-version verdict is a pure function of the detected
// version, so a second probe would reach the same conclusion.
func TestDetectBuiltinRuntimes_DoesNotRetryMinVersionRejection(t *testing.T) {
	fx := newBatchFixture(t)
	stubProbeRetry(t, time.Millisecond, time.Second)
	checkAgentMinVersion = func(provider, _ string) error {
		if provider == "codex" {
			return errors.New("version too old")
		}
		return nil
	}
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "/fake/codex"}}

	if runtimes := d.detectBuiltinRuntimes(context.Background()); len(runtimes) != 0 {
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
	if _, _, err := d.registerRuntimesForWorkspaceBatch(context.Background(), "ws-1", builtins); err != nil {
		t.Fatalf("batch register: %v", err)
	}

	if len(builtins) != 1 || builtins[0]["type"] != "claude" {
		t.Fatalf("shared built-in payload was mutated: %v", builtins)
	}
}
