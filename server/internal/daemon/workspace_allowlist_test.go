package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// workspaceAllowed / filterAllowedWorkspaces back the --workspace-allowlist
// feature (issue #3223): an operator can scope a machine's daemon to a subset of
// the workspaces its token can reach. An empty allowlist must preserve the
// historical "register in every workspace" behavior, and matching is
// case-insensitive against the workspace slug or ID.
func TestWorkspaceAllowed(t *testing.T) {
	t.Parallel()

	ws := WorkspaceInfo{ID: "11111111-1111-1111-1111-111111111111", Name: "Eskyfun", Slug: "eskyfun"}

	cases := []struct {
		name      string
		allowlist []string
		ws        WorkspaceInfo
		want      bool
	}{
		{"empty allowlist allows all", nil, ws, true},
		{"slug match", []string{"eskyfun"}, ws, true},
		{"slug match is case-insensitive", []string{"EskyFun"}, ws, true},
		{"slug match ignores surrounding whitespace", []string{"  eskyfun "}, ws, true},
		{"id match", []string{"11111111-1111-1111-1111-111111111111"}, ws, true},
		{"id match is case-insensitive", []string{"11111111-1111-1111-1111-111111111111"}, WorkspaceInfo{ID: "ABC-DEF", Slug: "x"}, false},
		{"no match", []string{"funtimes"}, ws, false},
		{"one of several matches", []string{"funtimes", "eskyfun"}, ws, true},
		{"blank entries are ignored", []string{"", "   "}, ws, false},
		{"name is not a match target", []string{"Eskyfun"}, WorkspaceInfo{ID: "id", Name: "Eskyfun", Slug: "slug"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workspaceAllowed(tc.allowlist, tc.ws); got != tc.want {
				t.Fatalf("workspaceAllowed(%v, %+v) = %v, want %v", tc.allowlist, tc.ws, got, tc.want)
			}
		})
	}
}

func TestFilterAllowedWorkspaces(t *testing.T) {
	t.Parallel()

	in := []WorkspaceInfo{
		{ID: "id-a", Slug: "eskyfun"},
		{ID: "id-b", Slug: "funtimes"},
		{ID: "id-c", Slug: "other"},
	}

	kept, excluded := filterAllowedWorkspaces([]string{"eskyfun", "id-c"}, in)
	if len(kept) != 2 || kept[0].Slug != "eskyfun" || kept[1].Slug != "other" {
		t.Fatalf("kept = %+v, want eskyfun + other (input order preserved)", kept)
	}
	if len(excluded) != 1 || excluded[0].Slug != "funtimes" {
		t.Fatalf("excluded = %+v, want funtimes", excluded)
	}

	// Empty allowlist keeps everything and excludes nothing.
	kept, excluded = filterAllowedWorkspaces(nil, in)
	if len(kept) != len(in) || len(excluded) != 0 {
		t.Fatalf("empty allowlist: kept=%d excluded=%d, want kept=%d excluded=0", len(kept), len(excluded), len(in))
	}
}

// syncWorkspacesFromAPI must not register runtimes in workspaces excluded by the
// allowlist, and must prune any excluded workspace that was already tracked (the
// case the issue calls out: deleting a runtime via the API otherwise comes back
// on the next sync). The allowed workspace is left untouched.
func TestSyncWorkspacesFromAPI_FiltersByAllowlist(t *testing.T) {
	t.Parallel()

	const (
		allowID = "ws-allow"
		denyID  = "ws-deny"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces":
			json.NewEncoder(w).Encode([]WorkspaceInfo{
				{ID: allowID, Name: "Eskyfun", Slug: "eskyfun"},
				{ID: denyID, Name: "Funtimes", Slug: "funtimes"},
			})
		case "/api/daemon/workspaces/" + allowID + "/repos":
			json.NewEncoder(w).Encode(WorkspaceReposResponse{
				WorkspaceID:  allowID,
				Repos:        []RepoData{},
				ReposVersion: "v1",
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
		cfg:          Config{WorkspaceAllowlist: []string{"eskyfun"}},
	}
	// Both workspaces start tracked with a live runtime — simulating a daemon
	// that registered everywhere before the allowlist was configured. A live
	// runtime ID keeps workspaceNeedsRuntimeRecovery from forcing a re-register.
	d.workspaces[allowID] = newWorkspaceState(allowID, []string{"rt-allow"}, "v1", nil, nil)
	d.workspaces[denyID] = newWorkspaceState(denyID, []string{"rt-deny"}, "v1", nil, nil)
	d.runtimeIndex["rt-allow"] = Runtime{ID: "rt-allow"}
	d.runtimeIndex["rt-deny"] = Runtime{ID: "rt-deny"}

	if err := d.syncWorkspacesFromAPI(context.Background()); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.workspaces[denyID]; ok {
		t.Fatalf("disallowed workspace %q should have been pruned", denyID)
	}
	if _, ok := d.runtimeIndex["rt-deny"]; ok {
		t.Fatalf("runtime for disallowed workspace should have been removed from the index")
	}
	if _, ok := d.workspaces[allowID]; !ok {
		t.Fatalf("allowed workspace %q should still be tracked", allowID)
	}
	if _, ok := d.runtimeIndex["rt-allow"]; !ok {
		t.Fatalf("runtime for allowed workspace should remain in the index")
	}
}
