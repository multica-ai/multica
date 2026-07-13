package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// TestProfileSetSignature_StableUnderReorder ensures the digest only
// captures the *content* of the profile set, not the order the server
// happened to return profiles in. Without this property duplicate change
// notifications could re-register whenever the server shuffled rows (which
// the API contract does not forbid).
func TestProfileSetSignature_StableUnderReorder(t *testing.T) {
	a := []RuntimeProfile{
		{ID: "p-a", ProtocolFamily: "codex", CommandName: "a", Enabled: true},
		{ID: "p-b", ProtocolFamily: "claude", CommandName: "b", Enabled: true},
	}
	b := []RuntimeProfile{
		{ID: "p-b", ProtocolFamily: "claude", CommandName: "b", Enabled: true},
		{ID: "p-a", ProtocolFamily: "codex", CommandName: "a", Enabled: true},
	}
	if profileSetSignature(a) != profileSetSignature(b) {
		t.Errorf("digest must be order-independent")
	}
}

// TestProfileSetSignature_DetectsRegistrationAffectingChanges asserts the
// digest covers exactly the fields the daemon sends in a Register call.
// Coverage gaps here would mean a real server-side change goes undetected
// and the user has to restart the daemon — the bug MUL-3332 is about.
func TestProfileSetSignature_DetectsRegistrationAffectingChanges(t *testing.T) {
	base := []RuntimeProfile{{
		ID:             "p1",
		ProtocolFamily: "codex",
		CommandName:    "company-codex",
		FixedArgs:      []string{"--foo"},
		Visibility:     "workspace",
		Enabled:        true,
	}}
	baseSig := profileSetSignature(base)

	// Empty list must hash differently from a one-profile list.
	if profileSetSignature(nil) == baseSig {
		t.Errorf("empty list must hash differently from a populated list")
	}

	cases := []struct {
		name   string
		mutate func([]RuntimeProfile) []RuntimeProfile
	}{
		{"add new profile", func(in []RuntimeProfile) []RuntimeProfile {
			return append(in, RuntimeProfile{ID: "p2", ProtocolFamily: "claude", CommandName: "c", Enabled: true})
		}},
		{"flip enabled", func(in []RuntimeProfile) []RuntimeProfile {
			out := append([]RuntimeProfile(nil), in...)
			out[0].Enabled = !out[0].Enabled
			return out
		}},
		{"change command_name", func(in []RuntimeProfile) []RuntimeProfile {
			out := append([]RuntimeProfile(nil), in...)
			out[0].CommandName = "different-bin"
			return out
		}},
		{"change protocol_family", func(in []RuntimeProfile) []RuntimeProfile {
			out := append([]RuntimeProfile(nil), in...)
			out[0].ProtocolFamily = "claude"
			return out
		}},
		{"change fixed_args", func(in []RuntimeProfile) []RuntimeProfile {
			out := append([]RuntimeProfile(nil), in...)
			out[0].FixedArgs = []string{"--foo", "--bar"}
			return out
		}},
		{"change visibility", func(in []RuntimeProfile) []RuntimeProfile {
			out := append([]RuntimeProfile(nil), in...)
			out[0].Visibility = "private"
			return out
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := profileSetSignature(tc.mutate(base))
			if got == baseSig {
				t.Errorf("digest must change when %s; baseSig=%s mutatedSig=%s",
					tc.name, baseSig, got)
			}
		})
	}
}

// driftFixture wires a Daemon against a fake server whose runtime-profiles
// response can be swapped at runtime. It also tracks how many times the
// server saw a /api/daemon/register, /api/daemon/runtimes/:id/recover-orphans,
// and /api/daemon/deregister call so tests can assert exactly which side
// effects the drift refresh triggered (or didn't).
type driftFixture struct {
	daemon              *Daemon
	server              *httptest.Server
	registerCalls       atomic.Int32
	recoverOrphansCalls []string // runtime IDs the server received recover-orphans for, in order
	recoverOrphansMu    sync.Mutex
	deregisterCalls     [][]string // each entry is one Deregister call's runtime_ids payload, in order
	deregisterMu        sync.Mutex
	currentProfiles     []RuntimeProfile
}

// setProfiles swaps the profile set returned by the fake server. The
// next GetRuntimeProfiles call observes the new value. Tests in this file
// drive the fixture from a single goroutine, so the field is unguarded;
// add a mutex if a future test publishes profile updates concurrently with
// daemon background work.
func (fx *driftFixture) setProfiles(profiles []RuntimeProfile) {
	fx.currentProfiles = profiles
}

// recordedRecoverOrphans returns a copy of the runtime IDs the fake server
// received /recover-orphans calls for since the fixture was created.
func (fx *driftFixture) recordedRecoverOrphans() []string {
	fx.recoverOrphansMu.Lock()
	defer fx.recoverOrphansMu.Unlock()
	out := make([]string, len(fx.recoverOrphansCalls))
	copy(out, fx.recoverOrphansCalls)
	return out
}

// recordedDeregisters returns a copy of every Deregister call's runtime_ids
// payload, in the order the fake server received them.
func (fx *driftFixture) recordedDeregisters() [][]string {
	fx.deregisterMu.Lock()
	defer fx.deregisterMu.Unlock()
	out := make([][]string, len(fx.deregisterCalls))
	for i, ids := range fx.deregisterCalls {
		cp := make([]string, len(ids))
		copy(cp, ids)
		out[i] = cp
	}
	return out
}

func newDriftFixture(t *testing.T, initial []RuntimeProfile) *driftFixture {
	t.Helper()
	fx := &driftFixture{currentProfiles: initial}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/daemon/register":
			fx.registerCalls.Add(1)
			var body struct {
				Runtimes []map[string]any `json:"runtimes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			var resp RegisterResponse
			for i, rt := range body.Runtimes {
				id := "rt-" + strconv.Itoa(i)
				profileID, _ := rt["profile_id"].(string)
				typ, _ := rt["type"].(string)
				resp.Runtimes = append(resp.Runtimes, Runtime{
					ID: id, Name: "n", Provider: typ, Status: "online", ProfileID: profileID,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, "/api/daemon/runtimes/") && strings.HasSuffix(r.URL.Path, "/recover-orphans"):
			// /api/daemon/runtimes/<id>/recover-orphans
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/daemon/runtimes/"), "/")
			runtimeID := parts[0]
			fx.recoverOrphansMu.Lock()
			fx.recoverOrphansCalls = append(fx.recoverOrphansCalls, runtimeID)
			fx.recoverOrphansMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"orphaned":0,"retried":0}`))
		case r.URL.Path == "/api/daemon/deregister":
			var body struct {
				RuntimeIDs []string `json:"runtime_ids"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			fx.deregisterMu.Lock()
			ids := make([]string, len(body.RuntimeIDs))
			copy(ids, body.RuntimeIDs)
			fx.deregisterCalls = append(fx.deregisterCalls, ids)
			fx.deregisterMu.Unlock()
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/runtime-profiles"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RuntimeProfilesResponse{
				WorkspaceID:     "ws-1",
				RuntimeProfiles: fx.currentProfiles,
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

// TestSyncWorkspacesSkipsRuntimeProfileRefreshOnExistingWorkspace pins the
// on-demand contract: the periodic workspace sync may discover workspaces and
// recover missing runtimes, but it must not poll the runtime-profiles endpoint
// for a healthy workspace. Profile create/update/delete notifications arrive
// through the daemon WebSocket and call refreshWorkspaceRuntimeProfiles
// directly.
func TestSyncWorkspacesSkipsRuntimeProfileRefreshOnExistingWorkspace(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"
	var profileCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]WorkspaceInfo{{ID: workspaceID, Name: "ws"}})
		case "/api/daemon/workspaces/" + workspaceID + "/runtime-profiles":
			profileCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RuntimeProfilesResponse{WorkspaceID: workspaceID})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.workspaces[workspaceID] = newWorkspaceState(workspaceID, []string{"rt-1"}, "", nil, nil)
	d.workspaces[workspaceID].profileSetSig = profileSetSignature(nil)
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1", Provider: "codex"}

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if got := profileCalls.Load(); got != 0 {
		t.Fatalf("workspace sync polled runtime-profiles %d times, want 0", got)
	}
}

// TestSyncWorkspacesRefreshesRuntimeProfilesOnReconcile preserves the
// reliability backstop for a profile mutation that happened while the daemon
// WebSocket was disconnected. The reconnect broadcast must make one on-demand
// request for each tracked workspace; otherwise removing the ticker polling
// would leave the daemon permanently stale until it restarted.
func TestSyncWorkspacesRefreshesRuntimeProfilesOnReconcile(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"
	var profileCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]WorkspaceInfo{{ID: workspaceID, Name: "ws"}})
		case "/api/daemon/workspaces/" + workspaceID + "/runtime-profiles":
			profileCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RuntimeProfilesResponse{WorkspaceID: workspaceID})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	d.workspaces[workspaceID] = newWorkspaceState(workspaceID, []string{"rt-1"}, "", nil, nil)
	d.workspaces[workspaceID].profileSetSig = profileSetSignature(nil)
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1", Provider: "codex"}

	if err := d.syncWorkspacesFromAPI(context.Background(), true); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if got := profileCalls.Load(); got != 1 {
		t.Fatalf("reconcile fetched runtime-profiles %d times, want 1", got)
	}
}

// TestRefreshWorkspaceRuntimeProfiles_NoDrift_DoesNotReregister verifies the
// hot-path: when the server's profile set has not changed since the daemon
// last registered the workspace, a duplicate notification must NOT fire a
// re-register.
func TestRefreshWorkspaceRuntimeProfiles_NoDrift_DoesNotReregister(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	profiles := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	fx := newDriftFixture(t, profiles)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{}

	// Initial register seeds workspaceState (and ws.profileSetSig).
	resp, profileSig, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{resp.Runtimes[0].ID}, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = profileSig
	for _, rt := range resp.Runtimes {
		d.runtimeIndex[rt.ID] = rt
	}
	if fx.registerCalls.Load() != 1 {
		t.Fatalf("setup expected 1 register call, got %d", fx.registerCalls.Load())
	}

	// Server returns the same profile set: refresh must not re-register.
	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}
	if fx.registerCalls.Load() != 1 {
		t.Errorf("no-drift refresh must not re-register; got %d total register calls", fx.registerCalls.Load())
	}
}

// TestRefreshWorkspaceRuntimeProfiles_NewProfileTriggersReregister verifies
// the user-visible fix for MUL-3332: a profile created via the web UI on an
// already-tracked workspace becomes a registered runtime when the daemon
// receives the server's change notification — no daemon restart required.
func TestRefreshWorkspaceRuntimeProfiles_NewProfileTriggersReregister(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{
		"company-codex": "/opt/bin/company-codex",
		"team-claude":   "/opt/bin/team-claude",
	})
	initial := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	fx := newDriftFixture(t, initial)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{}

	// Initial register with one profile.
	resp, profileSig, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	ids := make([]string, 0, len(resp.Runtimes))
	for _, rt := range resp.Runtimes {
		ids = append(ids, rt.ID)
		d.runtimeIndex[rt.ID] = rt
	}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", ids, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = profileSig
	beforeRegisterCalls := fx.registerCalls.Load()

	// User adds a second profile via the web UI: server's response now
	// includes prof-2.
	fx.setProfiles([]RuntimeProfile{
		{ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
			ProtocolFamily: "codex", CommandName: "company-codex",
			Visibility: "workspace", Enabled: true},
		{ID: "prof-2", WorkspaceID: "ws-1", DisplayName: "Team Claude",
			ProtocolFamily: "claude", CommandName: "team-claude",
			Visibility: "workspace", Enabled: true},
	})

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	// Drift must fire a new register call.
	if got := fx.registerCalls.Load(); got != beforeRegisterCalls+1 {
		t.Errorf("new profile must trigger one re-register; before=%d after=%d", beforeRegisterCalls, got)
	}

	// The daemon's runtimeIndex must now hold a runtime for the new profile.
	d.mu.Lock()
	var seenProf2 bool
	for _, rt := range d.runtimeIndex {
		if rt.ProfileID == "prof-2" {
			seenProf2 = true
			break
		}
	}
	d.mu.Unlock()
	if !seenProf2 {
		t.Errorf("expected runtimeIndex to contain a runtime for prof-2 after refresh")
	}

	// And the cached signature must now match the new profile set, so a
	// follow-up refresh with no further changes is a no-op.
	stableCalls := fx.registerCalls.Load()
	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if got := fx.registerCalls.Load(); got != stableCalls {
		t.Errorf("steady-state refresh must not re-register; before=%d after=%d", stableCalls, got)
	}

	// MUL-3332 review (concern 1): the drift path MUST NOT call recover-
	// orphans on any returned runtime. Recover-orphans hard-fails every
	// dispatched/running task on a runtime, so calling it for a built-in
	// runtime that the user had been actively running tasks on would kill
	// real work just because they added an unrelated sibling profile. The
	// runtime_gone recovery path keeps recover-orphans because those rows
	// were truly deleted server-side; drift surfaces existing runtimes.
	if got := fx.recordedRecoverOrphans(); len(got) != 0 {
		t.Errorf("drift path must not trigger recover-orphans for any runtime; got %v", got)
	}
}

// TestRefreshWorkspaceRuntimeProfiles_DriftWithRunningRuntimeSkipsOrphanRecovery
// is the targeted regression for MUL-3332 review concern #1: a daemon that
// is actively executing tasks on its existing runtime (built-in or
// previously-registered profile) must NOT have those tasks killed when
// the user adds a *new* sibling profile. Adding a profile must surface as
// a re-register that includes the new profile while LEAVING the existing
// runtime IDs untouched and recover-orphans untriggered.
func TestRefreshWorkspaceRuntimeProfiles_DriftWithRunningRuntimeSkipsOrphanRecovery(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{
		"company-codex": "/opt/bin/company-codex",
		"team-claude":   "/opt/bin/team-claude",
	})
	// Mixed setup: one built-in runtime (claude) plus one custom profile,
	// the closest analogue of "a daemon that is running real work and the
	// user just added another profile". Profile drift fires next.
	initial := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	fx := newDriftFixture(t, initial)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/usr/bin/true"}}

	resp, profileSig, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	ids := make([]string, 0, len(resp.Runtimes))
	for _, rt := range resp.Runtimes {
		ids = append(ids, rt.ID)
		d.runtimeIndex[rt.ID] = rt
	}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", ids, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = profileSig
	if len(ids) < 2 {
		t.Fatalf("setup expected at least 2 runtimes (built-in + custom); got %d", len(ids))
	}

	// User adds a second profile.
	fx.setProfiles([]RuntimeProfile{
		{ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
			ProtocolFamily: "codex", CommandName: "company-codex",
			Visibility: "workspace", Enabled: true},
		{ID: "prof-2", WorkspaceID: "ws-1", DisplayName: "Team Claude",
			ProtocolFamily: "claude", CommandName: "team-claude",
			Visibility: "workspace", Enabled: true},
	})

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	// Hard regression: zero recover-orphans calls for ANY runtime ID.
	// The whole point of MUL-3332 review concern #1 is that adding a
	// profile must not nuke in-flight work on existing runtimes.
	if got := fx.recordedRecoverOrphans(); len(got) != 0 {
		t.Errorf("drift refresh leaked recover-orphans calls (would fail running tasks on existing runtimes): %v", got)
	}
}

// TestRefreshWorkspaceRuntimeProfiles_DisableConvergesCustomOnlyDaemon is the
// targeted regression for MUL-3332 review concern #2: when a custom-only
// daemon (no built-in agents) has its only enabled profile disabled, the
// daemon must converge to a zero-runtime state — clear local tracking AND
// tell the server to mark the orphaned runtime row offline immediately,
// rather than leaving the daemon polling/heartbeating a runtime the user
// can no longer use.
func TestRefreshWorkspaceRuntimeProfiles_DisableConvergesCustomOnlyDaemon(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	initial := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	fx := newDriftFixture(t, initial)
	d := fx.daemon
	// Custom-only daemon: no built-in agents at all.
	d.cfg.Agents = map[string]AgentEntry{}

	// Initial register: one custom runtime.
	resp, profileSig, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	if len(resp.Runtimes) != 1 {
		t.Fatalf("setup expected exactly one runtime; got %d", len(resp.Runtimes))
	}
	initialRuntimeID := resp.Runtimes[0].ID
	d.runtimeIndex[initialRuntimeID] = resp.Runtimes[0]
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{initialRuntimeID}, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = profileSig

	// User disables the only profile: server's daemon-facing list now
	// returns zero enabled profiles.
	fx.setProfiles(nil)

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	// Local tracking must be empty: the daemon stops polling/heartbeating
	// runtimes that no longer represent anything the user can run.
	d.mu.Lock()
	gotRuntimeIDs := append([]string(nil), d.workspaces["ws-1"].runtimeIDs...)
	_, stillIndexed := d.runtimeIndex[initialRuntimeID]
	gotSig := d.workspaces["ws-1"].profileSetSig
	d.mu.Unlock()
	if len(gotRuntimeIDs) != 0 {
		t.Errorf("workspaceState.runtimeIDs must be empty after convergence-to-zero; got %v", gotRuntimeIDs)
	}
	if stillIndexed {
		t.Errorf("runtimeIndex must drop the previously-tracked runtime %q after convergence-to-zero", initialRuntimeID)
	}
	if gotSig != profileSetSignature(nil) {
		t.Errorf("converged signature must match the empty-profile-list digest so duplicate notifications are no-ops; got %q want %q", gotSig, profileSetSignature(nil))
	}

	// Server-side cleanup: the daemon must Deregister the orphaned runtime
	// ID so the runtime row goes offline immediately instead of waiting
	// 150 s for the stale-heartbeat sweep.
	deregs := fx.recordedDeregisters()
	if len(deregs) == 0 {
		t.Fatalf("expected one Deregister call for the orphaned runtime ID; got none")
	}
	var sawInitial bool
	for _, ids := range deregs {
		for _, id := range ids {
			if id == initialRuntimeID {
				sawInitial = true
			}
		}
	}
	if !sawInitial {
		t.Errorf("Deregister payload must include the initial runtime ID %q; got %v", initialRuntimeID, deregs)
	}

	// Convergence-to-zero must NOT call recover-orphans either: the daemon
	// has nothing actively running for the user once they disabled the
	// profile, but if it had, killing those tasks for a profile DISABLE
	// (vs delete) would still be wrong — the user might re-enable.
	if got := fx.recordedRecoverOrphans(); len(got) != 0 {
		t.Errorf("convergence-to-zero must not trigger recover-orphans; got %v", got)
	}

	// Steady state: a follow-up refresh with the same (empty) profile set
	// is a no-op. No new register / deregister / recover-orphans calls.
	stableRegisters := fx.registerCalls.Load()
	stableDeregs := len(fx.recordedDeregisters())
	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("second refresh after convergence: %v", err)
	}
	if got := fx.registerCalls.Load(); got != stableRegisters {
		t.Errorf("converged steady state must not re-register; before=%d after=%d", stableRegisters, got)
	}
	if got := len(fx.recordedDeregisters()); got != stableDeregs {
		t.Errorf("converged steady state must not deregister again; before=%d after=%d", stableDeregs, got)
	}
}

// TestRefreshWorkspaceRuntimeProfiles_DisableOneOfManyDeregistersDroppedID
// covers the partial-drift case: a daemon hosting both a built-in runtime
// and a custom profile has its custom profile disabled. The built-in
// survives (Register call carries it forward), the daemon's runtime set
// converges to just the built-in, and the dropped custom runtime ID is
// Deregistered so the server marks it offline immediately.
func TestRefreshWorkspaceRuntimeProfiles_DisableOneOfManyDeregistersDroppedID(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	initial := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	fx := newDriftFixture(t, initial)
	d := fx.daemon
	// Mixed: one built-in + one custom.
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/usr/bin/true"}}

	resp, profileSig, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	if len(resp.Runtimes) != 2 {
		t.Fatalf("setup expected 2 runtimes; got %d (%+v)", len(resp.Runtimes), resp.Runtimes)
	}
	var customID string
	for _, rt := range resp.Runtimes {
		d.runtimeIndex[rt.ID] = rt
		if rt.ProfileID == "prof-1" {
			customID = rt.ID
		}
	}
	if customID == "" {
		t.Fatalf("setup expected a runtime for prof-1; got %+v", resp.Runtimes)
	}
	initialIDs := []string{resp.Runtimes[0].ID, resp.Runtimes[1].ID}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", initialIDs, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = profileSig

	// User disables the custom profile.
	fx.setProfiles(nil)

	if err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRuntimeProfiles: %v", err)
	}

	// The custom runtime ID must be Deregistered.
	deregs := fx.recordedDeregisters()
	var sawCustom bool
	for _, ids := range deregs {
		for _, id := range ids {
			if id == customID {
				sawCustom = true
			}
		}
	}
	if !sawCustom {
		t.Errorf("Deregister payload must include the dropped custom runtime ID %q; got %v", customID, deregs)
	}

	// Surviving built-in must NOT be in any deregister payload.
	for _, ids := range deregs {
		for _, id := range ids {
			if id != customID {
				t.Errorf("Deregister leaked surviving runtime ID %q (only %q should be deregistered)", id, customID)
			}
		}
	}

	// And the still-surviving built-in must NOT have recover-orphans called
	// against it (concern #1 again, partial-drift flavour).
	if got := fx.recordedRecoverOrphans(); len(got) != 0 {
		t.Errorf("partial-drift refresh leaked recover-orphans calls: %v", got)
	}
}

// TestRefreshWorkspaceRuntimeProfiles_FetchErrorIsBestEffort verifies that
// a network blip or older server (404) does NOT clear the cached signature
// or trigger a spurious re-register.
func TestRefreshWorkspaceRuntimeProfiles_FetchErrorIsBestEffort(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})
	profiles := []RuntimeProfile{{
		ID: "prof-1", WorkspaceID: "ws-1", DisplayName: "Company Codex",
		ProtocolFamily: "codex", CommandName: "company-codex",
		Visibility: "workspace", Enabled: true,
	}}
	// Server that returns 404 for the profiles route.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/runtime-profiles") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	d := freshDaemon(srv.URL)
	d.profileLaunchSpecs = make(map[string]profileLaunchSpec)
	knownSig := profileSetSignature(profiles)
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-1"}, "", nil, nil)
	d.workspaces["ws-1"].profileSetSig = knownSig

	err := d.refreshWorkspaceRuntimeProfiles(context.Background(), "ws-1")
	if err == nil {
		t.Fatalf("404 must surface as an error so the caller can log it at debug")
	}

	// Cached signature must be preserved (no overwrite on transient failures).
	d.mu.Lock()
	gotSig := d.workspaces["ws-1"].profileSetSig
	d.mu.Unlock()
	if gotSig != knownSig {
		t.Errorf("transient fetch error must not clobber cached sig; want %q got %q", knownSig, gotSig)
	}
}
