package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// stubLookPath swaps the package-level lookPath indirection used by
// registerRuntimesForWorkspace to resolve custom runtime-profile commands,
// so tests don't have to mutate the process PATH. resolved maps a command
// name to the absolute path it should resolve to; an absent name reports
// "not found".
func stubLookPath(t *testing.T, resolved map[string]string) {
	t.Helper()
	orig := lookPath
	lookPath = func(cmd string) (string, error) {
		if p, ok := resolved[cmd]; ok {
			return p, nil
		}
		return "", &osExecNotFound{cmd: cmd}
	}
	t.Cleanup(func() { lookPath = orig })
}

type osExecNotFound struct{ cmd string }

func (e *osExecNotFound) Error() string { return "exec: " + e.cmd + ": not found in $PATH" }

// TestClient_GetRuntimeProfiles_RequestShape asserts the daemon GETs the
// documented path and parses the server's runtime_profiles payload.
func TestClient_GetRuntimeProfiles_RequestShape(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"workspace_id":"ws-1",
			"runtime_profiles":[{
				"id":"prof-1",
				"workspace_id":"ws-1",
				"display_name":"Company Codex",
				"protocol_family":"codex",
				"command_name":"company-codex",
				"description":null,
				"fixed_args":["--foo"],
				"visibility":"workspace",
				"created_by":null,
				"enabled":true,
				"created_at":"2026-01-01T00:00:00Z",
				"updated_at":"2026-01-01T00:00:00Z"
			}]
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("tok")
	resp, err := c.GetRuntimeProfiles(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("GetRuntimeProfiles: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/daemon/workspaces/ws-1/runtime-profiles" {
		t.Errorf("path = %q, want /api/daemon/workspaces/ws-1/runtime-profiles", gotPath)
	}
	if resp.WorkspaceID != "ws-1" || len(resp.RuntimeProfiles) != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	p := resp.RuntimeProfiles[0]
	if p.ID != "prof-1" || p.ProtocolFamily != "codex" || p.CommandName != "company-codex" {
		t.Errorf("profile fields wrong: %+v", p)
	}
	if !p.Enabled {
		t.Errorf("profile should be enabled")
	}
	if len(p.FixedArgs) != 1 || p.FixedArgs[0] != "--foo" {
		t.Errorf("fixed_args = %v, want [--foo]", p.FixedArgs)
	}
}

// profileRegisterFixture wires a Daemon against a fake server that serves a
// configurable set of runtime profiles and captures the runtimes array sent
// to /api/daemon/register.
type profileRegisterFixture struct {
	daemon       *Daemon
	server       *httptest.Server
	sentRuntimes []map[string]any
}

func newProfileRegisterFixture(t *testing.T, profiles []RuntimeProfile, profilesStatus int) *profileRegisterFixture {
	t.Helper()
	fx := &profileRegisterFixture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/daemon/register":
			var body struct {
				Runtimes []map[string]any `json:"runtimes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			fx.sentRuntimes = body.Runtimes
			// Echo back a Runtime row per requested runtime, threading
			// profile_id so the caller can populate runtimeIndex from it.
			var resp RegisterResponse
			for i, rt := range body.Runtimes {
				id := "rt-" + strconv.Itoa(i)
				profileID, _ := rt["profile_id"].(string)
				typ, _ := rt["type"].(string)
				resp.Runtimes = append(resp.Runtimes, Runtime{
					ID:        id,
					Name:      "n",
					Provider:  typ,
					Status:    "online",
					ProfileID: profileID,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case len(r.URL.Path) > len("/runtime-profiles") && strings.HasSuffix(r.URL.Path, "/runtime-profiles"):
			if profilesStatus != 0 && profilesStatus != http.StatusOK {
				w.WriteHeader(profilesStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(RuntimeProfilesResponse{
				WorkspaceID:     "ws-1",
				RuntimeProfiles: profiles,
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

// TestRegisterRuntimes_AppendsProfileRuntime verifies that a custom profile
// whose command resolves on PATH is appended as a runtime entry carrying
// profile_id, and that its resolved command path is recorded for runTask.
// Uses a custom-only host (no built-in agents) to also prove that path still
// registers.
func TestRegisterRuntimes_AppendsProfileRuntime(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{"company-codex": "/opt/bin/company-codex"})

	profiles := []RuntimeProfile{{
		ID:             "prof-1",
		WorkspaceID:    "ws-1",
		DisplayName:    "Company Codex",
		ProtocolFamily: "codex",
		CommandName:    "company-codex",
		Visibility:     "workspace",
		Enabled:        true,
	}}
	fx := newProfileRegisterFixture(t, profiles, http.StatusOK)
	d := fx.daemon
	// Custom-only host: no built-in agents configured.
	d.cfg.Agents = map[string]AgentEntry{}

	resp, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("registerRuntimesForWorkspace: %v", err)
	}

	// The register request must carry exactly one runtime: the profile.
	if len(fx.sentRuntimes) != 1 {
		t.Fatalf("sent runtimes = %d, want 1: %+v", len(fx.sentRuntimes), fx.sentRuntimes)
	}
	sent := fx.sentRuntimes[0]
	if sent["type"] != "codex" {
		t.Errorf("sent type = %v, want codex", sent["type"])
	}
	if sent["profile_id"] != "prof-1" {
		t.Errorf("sent profile_id = %v, want prof-1", sent["profile_id"])
	}
	if sent["status"] != "online" {
		t.Errorf("sent status = %v, want online", sent["status"])
	}

	// The resolved command path must be recorded keyed by profile_id.
	if got := d.profileCommandPaths["prof-1"]; got != "/opt/bin/company-codex" {
		t.Errorf("profileCommandPaths[prof-1] = %q, want /opt/bin/company-codex", got)
	}

	// The response runtime carries the profile_id back.
	if len(resp.Runtimes) != 1 || resp.Runtimes[0].ProfileID != "prof-1" {
		t.Fatalf("response runtimes wrong: %+v", resp.Runtimes)
	}
}

// TestRegisterRuntimes_SkipsProfileNotOnPath verifies a profile whose command
// is missing on this host is skipped, and that a host with no built-in agents
// and no resolvable profiles fails registration (len==0 guard preserved).
func TestRegisterRuntimes_SkipsProfileNotOnPath(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{}) // nothing resolves

	profiles := []RuntimeProfile{{
		ID:             "prof-1",
		WorkspaceID:    "ws-1",
		DisplayName:    "Company Codex",
		ProtocolFamily: "codex",
		CommandName:    "company-codex",
		Enabled:        true,
	}}
	fx := newProfileRegisterFixture(t, profiles, http.StatusOK)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{}

	_, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err == nil {
		t.Fatalf("expected error when no runtimes resolve, got nil")
	}
	if _, ok := d.profileCommandPaths["prof-1"]; ok {
		t.Errorf("profileCommandPaths should not record an unresolved profile")
	}
}

// TestRegisterRuntimes_ProfilesFetchErrorIsBestEffort verifies a 404 from the
// profiles endpoint does not fail registration when a built-in agent exists.
func TestRegisterRuntimes_ProfilesFetchErrorIsBestEffort(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))
	stubLookPath(t, map[string]string{})

	fx := newProfileRegisterFixture(t, nil, http.StatusNotFound)
	d := fx.daemon
	// Built-in agent present so registration has something to register.
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/usr/bin/true"}}

	resp, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("registration should succeed despite profiles 404: %v", err)
	}
	if len(fx.sentRuntimes) != 1 || fx.sentRuntimes[0]["type"] != "claude" {
		t.Fatalf("expected only the built-in claude runtime, got %+v", fx.sentRuntimes)
	}
	if len(resp.Runtimes) != 1 {
		t.Fatalf("response runtimes = %d, want 1", len(resp.Runtimes))
	}
}

// TestCustomCommandPathForRuntime covers the runtimeID -> command-path
// bookkeeping that runTask relies on to override the launch path.
func TestCustomCommandPathForRuntime(t *testing.T) {
	d := freshDaemon("")
	d.profileCommandPaths = map[string]string{"prof-1": "/opt/bin/company-codex"}
	// rt-custom is a custom-profile runtime; rt-builtin is a normal one.
	d.runtimeIndex["rt-custom"] = Runtime{ID: "rt-custom", Provider: "codex", ProfileID: "prof-1"}
	d.runtimeIndex["rt-builtin"] = Runtime{ID: "rt-builtin", Provider: "claude"}

	if path, ok := d.customCommandPathForRuntime("rt-custom"); !ok || path != "/opt/bin/company-codex" {
		t.Errorf("custom runtime: got (%q, %v), want (/opt/bin/company-codex, true)", path, ok)
	}
	if path, ok := d.customCommandPathForRuntime("rt-builtin"); ok || path != "" {
		t.Errorf("built-in runtime: got (%q, %v), want (\"\", false)", path, ok)
	}
	if path, ok := d.customCommandPathForRuntime("rt-unknown"); ok || path != "" {
		t.Errorf("unknown runtime: got (%q, %v), want (\"\", false)", path, ok)
	}
	// A custom runtime whose profile path was never resolved on this host
	// (profile_id not in profileCommandPaths) must report not-custom so
	// runTask falls back to its normal provider lookup rather than launching
	// an empty path.
	d.runtimeIndex["rt-unresolved"] = Runtime{ID: "rt-unresolved", Provider: "codex", ProfileID: "prof-missing"}
	if path, ok := d.customCommandPathForRuntime("rt-unresolved"); ok || path != "" {
		t.Errorf("unresolved profile: got (%q, %v), want (\"\", false)", path, ok)
	}
}
