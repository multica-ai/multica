package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// workspaceCoAuthoredByEnabled gates the prepare-commit-msg hook installed in
// agent worktrees. The trailer is opt-in: the hook is installed only when the
// workspace explicitly sets `co_authored_by_enabled`=true, so absent, empty, or
// malformed settings all resolve to off. RFC MUL-2414's `github_enabled` master
// switch still forces the hook off when explicitly false, even if
// `co_authored_by_enabled` is true.
func TestWorkspaceCoAuthoredByEnabled(t *testing.T) {
	cases := []struct {
		name     string
		register bool
		settings string
		want     bool
	}{
		{"unknown workspace defaults off", false, "", false},
		{"registered workspace, nil settings defaults off", true, "", false},
		{"empty object defaults off", true, "{}", false},
		{"co_authored_by absent defaults off", true, `{"github_enabled":true}`, false},
		{"co_authored_by true", true, `{"co_authored_by_enabled":true}`, true},
		{"co_authored_by false", true, `{"co_authored_by_enabled":false}`, false},
		{
			"master off forces hook off even when co_authored_by true",
			true,
			`{"github_enabled":false,"co_authored_by_enabled":true}`,
			false,
		},
		{
			"master on lets co_authored_by decide",
			true,
			`{"github_enabled":true,"co_authored_by_enabled":false}`,
			false,
		},
		{"malformed settings defaults off", true, `not json`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Daemon{workspaces: make(map[string]*workspaceState)}
			if tc.register {
				var raw json.RawMessage
				if tc.settings != "" {
					raw = json.RawMessage(tc.settings)
				}
				d.workspaces["ws"] = newWorkspaceState("ws", nil, "", nil, raw)
			}
			if got := d.workspaceCoAuthoredByEnabled("ws"); got != tc.want {
				t.Fatalf("workspaceCoAuthoredByEnabled(%q) = %v, want %v",
					tc.settings, got, tc.want)
			}
		})
	}
}

// fetchCoAuthoredByEnabled is the controller-mode gate: stateless worker pods
// have no synced workspaceState, so the setting is read live at checkout time
// instead of from the daemon's settings map. The trailer is opt-in — it must
// honor an explicit co_authored_by_enabled=false (the live bug it fixes:
// controller mode used to hardcode the hook on), default to off for absent
// settings, respect the github_enabled master switch, and resolve to off when
// the live fetch fails so an unreachable server can never re-enable attribution.
func TestFetchCoAuthoredByEnabled(t *testing.T) {
	const workspaceID = "ws-1"

	cases := []struct {
		name     string
		settings string // body returned by the repos endpoint; "" → omit Settings
		fail     bool   // serve 500 to simulate a fetch error
		want     bool
	}{
		{"explicit false disables hook", `{"co_authored_by_enabled":false}`, false, false},
		{"explicit true enables hook", `{"co_authored_by_enabled":true}`, false, true},
		{"absent settings default off", "", false, false},
		{"empty object defaults off", "{}", false, false},
		{"master switch off forces hook off", `{"github_enabled":false,"co_authored_by_enabled":true}`, false, false},
		{"fetch error defaults off", "", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/daemon/workspaces/"+workspaceID+"/repos" {
					http.NotFound(w, r)
					return
				}
				if tc.fail {
					http.Error(w, "boom", http.StatusInternalServerError)
					return
				}
				var raw json.RawMessage
				if tc.settings != "" {
					raw = json.RawMessage(tc.settings)
				}
				json.NewEncoder(w).Encode(WorkspaceReposResponse{
					WorkspaceID:  workspaceID,
					Repos:        []RepoData{},
					ReposVersion: "v1",
					Settings:     raw,
				})
			}))
			t.Cleanup(srv.Close)

			d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
			if got := d.fetchCoAuthoredByEnabled(context.Background(), workspaceID); got != tc.want {
				t.Fatalf("fetchCoAuthoredByEnabled(%q) = %v, want %v", tc.settings, got, tc.want)
			}
		})
	}
}

// syncWorkspacesFromAPI must refresh the cached workspace settings on already-
// tracked workspaces so that toggling `co_authored_by_enabled` (or the
// `github_enabled` master switch) in the web UI takes effect on the next gated
// operation without a daemon restart. Reviewed in PR #2847 by Emacs — the
// original cut only wrote settings during registration, so a running daemon
// would keep installing the Co-authored-by hook on the next `repo checkout`
// even after the workspace flipped the switch off.
func TestSyncWorkspacesRefreshesSettingsOnExistingWorkspace(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"

	var settingsPayload atomic.Value
	settingsPayload.Store(json.RawMessage(`{"github_enabled":true,"co_authored_by_enabled":true}`))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces":
			json.NewEncoder(w).Encode([]WorkspaceInfo{{ID: workspaceID, Name: "ws"}})
		case "/api/daemon/workspaces/" + workspaceID + "/repos":
			raw, _ := settingsPayload.Load().(json.RawMessage)
			json.NewEncoder(w).Encode(WorkspaceReposResponse{
				WorkspaceID:  workspaceID,
				Repos:        []RepoData{},
				ReposVersion: "v1",
				Settings:     raw,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:       NewClient(srv.URL),
		logger:       slog.Default(),
		workspaces:   make(map[string]*workspaceState),
		runtimeIndex: make(map[string]Runtime),
		runtimeSet:   newRuntimeSetWatcher(),
	}
	// Pretend the workspace was already registered with co-author ON. A live
	// runtime ID keeps workspaceNeedsRuntimeRecovery from short-circuiting the
	// sync into a re-register.
	d.workspaces[workspaceID] = newWorkspaceState(
		workspaceID,
		[]string{"rt-1"},
		"v1",
		nil,
		json.RawMessage(`{"github_enabled":true,"co_authored_by_enabled":true}`),
	)

	if !d.workspaceCoAuthoredByEnabled(workspaceID) {
		t.Fatalf("precondition: expected co-author hook to start enabled")
	}

	// The user opens Settings → GitHub and turns the master switch off.
	settingsPayload.Store(json.RawMessage(`{"github_enabled":false,"co_authored_by_enabled":true}`))

	if err := d.syncWorkspacesFromAPI(context.Background()); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if d.workspaceCoAuthoredByEnabled(workspaceID) {
		t.Fatalf("expected co-author hook disabled after toggle; daemon is still using stale cached settings")
	}

	// Flipping the master switch back on must take effect the next sync too —
	// the refresh path is not one-way.
	settingsPayload.Store(json.RawMessage(`{"github_enabled":true,"co_authored_by_enabled":true}`))
	if err := d.syncWorkspacesFromAPI(context.Background()); err != nil {
		t.Fatalf("syncWorkspacesFromAPI (re-enable): %v", err)
	}
	if !d.workspaceCoAuthoredByEnabled(workspaceID) {
		t.Fatalf("expected co-author hook re-enabled after toggling back on")
	}
}
