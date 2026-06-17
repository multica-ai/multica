package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// TestProfileSetSignature_StableUnderReorder ensures the digest only
// captures the *content* of the profile set, not the order the server
// happened to return profiles in. Without this property the
// workspaceSyncLoop would re-register on every tick whenever the server
// shuffled rows (which the API contract does not forbid).
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
// server saw a /api/daemon/register call so tests can assert that drift
// detection actually triggered (or didn't).
type driftFixture struct {
	daemon          *Daemon
	server          *httptest.Server
	registerCalls   atomic.Int32
	currentProfiles []RuntimeProfile
}

// setProfiles swaps the profile set returned by the fake server. The
// next GetRuntimeProfiles call observes the new value. Tests in this file
// drive the fixture from a single goroutine, so the field is unguarded;
// add a mutex if a future test publishes profile updates concurrently with
// daemon background work.
func (fx *driftFixture) setProfiles(profiles []RuntimeProfile) {
	fx.currentProfiles = profiles
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
	d.profileCommandPaths = make(map[string]string)
	fx.daemon = d
	fx.server = srv
	return fx
}

// TestRefreshWorkspaceRuntimeProfiles_NoDrift_DoesNotReregister verifies the
// hot-path: when the server's profile set has not changed since the daemon
// last registered the workspace, the sync tick must NOT fire a re-register.
// Without this guarantee every quiet sync tick would cost an extra Register
// HTTP call per workspace.
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
// already-tracked workspace becomes a registered runtime within one
// workspaceSyncLoop tick — no daemon restart required.
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
}

// TestRefreshWorkspaceRuntimeProfiles_FetchErrorIsBestEffort verifies that
// a network blip or older server (404) does NOT clear the cached signature
// or trigger a spurious re-register. Without this, a transient 5xx during
// the workspace sync loop would loop the daemon into re-registering forever.
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
	d.profileCommandPaths = make(map[string]string)
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
