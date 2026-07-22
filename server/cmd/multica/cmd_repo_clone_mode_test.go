package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// repoRegistryStub serves the two workspace endpoints `repo add` uses and
// records whatever repo list the command sends back.
func repoRegistryStub(t *testing.T, initial []workspaceRepo, patched *[]workspaceRepo) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initial})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			var body struct {
				Repos []workspaceRepo `json:"repos"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			*patched = body.Repos
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: body.Repos})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRunRepoAddSendsCloneMode(t *testing.T) {
	var patched []workspaceRepo
	srv := repoRegistryStub(t, []workspaceRepo{}, &patched)
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := cmd.Flags().Set("clone-mode", "on-demand"); err != nil {
		t.Fatal(err)
	}
	if err := runRepoAdd(cmd, []string{"https://git.example.com/big.git"}); err != nil {
		t.Fatalf("runRepoAdd: %v", err)
	}

	if len(patched) != 1 {
		t.Fatalf("expected 1 repo patched, got %d", len(patched))
	}
	if patched[0].CloneMode != "on-demand" {
		t.Fatalf("clone_mode = %q, want %q", patched[0].CloneMode, "on-demand")
	}
}

// Full is the default, and the wire shape for it must stay exactly what it was
// before the flag existed so an older server keeps accepting it.
func TestRunRepoAddOmitsDefaultCloneMode(t *testing.T) {
	var patched []workspaceRepo
	srv := repoRegistryStub(t, []workspaceRepo{}, &patched)
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := runRepoAdd(cmd, []string{"https://git.example.com/web.git"}); err != nil {
		t.Fatalf("runRepoAdd: %v", err)
	}

	if len(patched) != 1 {
		t.Fatalf("expected 1 repo patched, got %d", len(patched))
	}
	if patched[0].CloneMode != "" {
		t.Fatalf("clone_mode should be omitted by default, got %q", patched[0].CloneMode)
	}
	body, err := json.Marshal(patched[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "clone_mode") {
		t.Fatalf("default repo payload should not carry clone_mode: %s", body)
	}
}

func TestRunRepoAddUpdatesCloneModeOnExistingRepo(t *testing.T) {
	var patched []workspaceRepo
	srv := repoRegistryStub(t,
		[]workspaceRepo{{URL: "https://git.example.com/web.git", Description: "web"}},
		&patched)
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := cmd.Flags().Set("clone-mode", "on-demand"); err != nil {
		t.Fatal(err)
	}
	if err := runRepoAdd(cmd, []string{"https://git.example.com/web.git"}); err != nil {
		t.Fatalf("runRepoAdd: %v", err)
	}

	if len(patched) != 1 {
		t.Fatalf("expected 1 repo patched, got %d", len(patched))
	}
	if patched[0].CloneMode != "on-demand" {
		t.Fatalf("clone_mode = %q, want on-demand", patched[0].CloneMode)
	}
	if patched[0].Description != "web" {
		t.Fatalf("description should be preserved, got %q", patched[0].Description)
	}
}

// Re-stating the mode a repo already has is not a change, so it must not
// trigger a write.
func TestRunRepoAddSkipsPatchWhenCloneModeUnchanged(t *testing.T) {
	patchCount := 0
	initial := []workspaceRepo{{URL: "https://git.example.com/web.git", CloneMode: "on-demand"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initial})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			patchCount++
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initial})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := cmd.Flags().Set("clone-mode", "on-demand"); err != nil {
		t.Fatal(err)
	}
	if err := runRepoAdd(cmd, []string{"https://git.example.com/web.git"}); err != nil {
		t.Fatalf("runRepoAdd: %v", err)
	}
	if patchCount != 0 {
		t.Fatalf("expected no PATCH when nothing changed, got %d", patchCount)
	}
}

// An entry with no stored mode already behaves as a full clone, so explicitly
// asking for full must not rewrite it.
func TestRunRepoAddTreatsMissingCloneModeAsFull(t *testing.T) {
	patchCount := 0
	initial := []workspaceRepo{{URL: "https://git.example.com/web.git"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initial})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			patchCount++
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initial})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := cmd.Flags().Set("clone-mode", "full"); err != nil {
		t.Fatal(err)
	}
	if err := runRepoAdd(cmd, []string{"https://git.example.com/web.git"}); err != nil {
		t.Fatalf("runRepoAdd: %v", err)
	}
	if patchCount != 0 {
		t.Fatalf("expected no PATCH for a no-op clone mode, got %d", patchCount)
	}
}

// `--depth`-style input was deliberately not implemented; it must fail loudly
// at the flag rather than reach the server.
func TestParseCloneModeFlag(t *testing.T) {
	t.Parallel()

	newCmd := func(value string, set bool) *cobra.Command {
		cmd := &cobra.Command{Use: "x"}
		cmd.Flags().String("clone-mode", cloneModeFull, "")
		if set {
			_ = cmd.Flags().Set("clone-mode", value)
		}
		return cmd
	}

	if got, err := parseCloneModeFlag(newCmd("", false)); err != nil || got != "" {
		t.Fatalf("unset flag: got %q, err %v; want empty, nil", got, err)
	}
	if got, err := parseCloneModeFlag(newCmd("full", true)); err != nil || got != cloneModeFull {
		t.Fatalf("full: got %q, err %v", got, err)
	}
	if got, err := parseCloneModeFlag(newCmd("On-Demand", true)); err != nil || got != cloneModeOnDemand {
		t.Fatalf("mixed case: got %q, err %v", got, err)
	}
	for _, bad := range []string{"1", "depth", "shallow", "blob:none"} {
		if _, err := parseCloneModeFlag(newCmd(bad, true)); err == nil {
			t.Fatalf("clone mode %q should be rejected", bad)
		}
	}
}

func TestCloneModeLabel(t *testing.T) {
	t.Parallel()
	if got := cloneModeLabel(""); got != cloneModeFull {
		t.Fatalf("empty clone mode should display as %q, got %q", cloneModeFull, got)
	}
	if got := cloneModeLabel("on-demand"); got != "on-demand" {
		t.Fatalf("got %q", got)
	}
}
