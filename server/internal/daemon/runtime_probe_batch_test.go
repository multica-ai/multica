package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
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
		fx.mu.Unlock()
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
