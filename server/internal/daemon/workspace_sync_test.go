package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func testDaemonLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func createTestGitRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test User",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test User",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s: %v", args, out, err)
		}
	}

	run("init", dir)
	run("-C", dir, "commit", "--allow-empty", "-m", "initial commit")

	return dir
}

func TestSyncWorkspaceReposRefreshesRepoCache(t *testing.T) {
	sourceRepo := createTestGitRepo(t)

	var (
		mu        sync.RWMutex
		repos     []RepoData
		requested []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		requested = append(requested, r.URL.Path)
		if got, want := r.URL.Path, "/api/workspaces/ws-123"; got != want {
			t.Errorf("unexpected path: got %s want %s", got, want)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		mu.RLock()
		currentRepos := append([]RepoData(nil), repos...)
		mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(WorkspaceDetails{
			ID:    "ws-123",
			Name:  "Test Workspace",
			Repos: currentRepos,
		}); err != nil {
			t.Errorf("encode workspace response: %v", err)
		}
	}))
	defer srv.Close()

	d := New(Config{
		ServerBaseURL:     srv.URL,
		WorkspacesRoot:    t.TempDir(),
		Agents:            map[string]AgentEntry{},
		Profile:           "",
		DaemonID:          "daemon-test",
		RuntimeName:       "Test Runtime",
		DeviceName:        "Test Device",
		CLIVersion:        "test",
		AgentTimeout:      DefaultAgentTimeout,
		PollInterval:      DefaultPollInterval,
		HeartbeatInterval: DefaultHeartbeatInterval,
	}, testDaemonLogger())

	ctx := context.Background()

	// First sync: the workspace has no repos yet, so cache lookup should stay empty.
	d.syncWorkspaceRepos(ctx, []string{"ws-123"})
	if got := d.repoCache.Lookup("ws-123", sourceRepo); got != "" {
		t.Fatalf("expected empty cache before repos are added, got %q", got)
	}

	// Simulate the workspace being updated later with a repo addition.
	mu.Lock()
	repos = []RepoData{{URL: sourceRepo, Description: "source"}}
	mu.Unlock()

	d.syncWorkspaceRepos(ctx, []string{"ws-123"})

	cached := d.repoCache.Lookup("ws-123", sourceRepo)
	if cached == "" {
		t.Fatalf("expected repo cache to be populated after sync")
	}
	if _, err := os.Stat(filepath.Join(cached, "HEAD")); err != nil {
		t.Fatalf("cached repo missing HEAD file at %s: %v", cached, err)
	}

	if got := len(requested); got != 2 {
		t.Fatalf("expected 2 workspace fetches, got %d", got)
	}
}

func TestGetWorkspaceReturnsRepos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/workspaces/ws-abc" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkspaceDetails{
			ID:   "ws-abc",
			Name: "Workspace ABC",
			Repos: []RepoData{
				{URL: "https://github.com/example/repo", Description: "demo"},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ws, err := c.GetWorkspace(context.Background(), "ws-abc")
	if err != nil {
		t.Fatalf("GetWorkspace failed: %v", err)
	}
	if ws == nil {
		t.Fatal("expected workspace details")
	}
	if got, want := ws.ID, "ws-abc"; got != want {
		t.Fatalf("unexpected workspace id: got %s want %s", got, want)
	}
	if got := len(ws.Repos); got != 1 {
		t.Fatalf("expected 1 repo, got %d", got)
	}
	if got, want := ws.Repos[0].URL, "https://github.com/example/repo"; got != want {
		t.Fatalf("unexpected repo url: got %s want %s", got, want)
	}
}
