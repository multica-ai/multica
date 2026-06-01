package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// fakeReposServer returns a stub Multica server that responds to
// GET /api/daemon/workspaces/{ws}/repos with the given repo URL list.
func fakeReposServer(t *testing.T, urls map[string][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// path is /api/daemon/workspaces/{ws}/repos
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(parts) < 5 {
			http.NotFound(w, r)
			return
		}
		wsID := parts[3]
		repoURLs, ok := urls[wsID]
		if !ok {
			http.NotFound(w, r)
			return
		}
		repos := make([]map[string]string, 0, len(repoURLs))
		for _, u := range repoURLs {
			repos = append(repos, map[string]string{"url": u})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"workspace_id":  wsID,
			"repos":         repos,
			"repos_version": "v1",
		})
	}))
}

func TestSyncOnce_GathersRepoListsPerWorkspace(t *testing.T) {
	// Use example.invalid so any attempted clone fails fast on DNS; that's
	// the desired aggregated error path we're testing.
	srv := fakeReposServer(t, map[string][]string{
		"ws-A": {"https://example.invalid/owner/repo-a.git"},
		"ws-B": {"https://example.invalid/owner/repo-b.git"},
	})
	defer srv.Close()

	cli := daemon.NewClient(srv.URL)
	cli.SetToken("tk")
	dir := t.TempDir()
	cache := repocache.New(filepath.Join(dir, "repos"), testLogger(t))

	cfg := &Config{Workspaces: []WorkspaceConfig{{ID: "ws-A"}, {ID: "ws-B"}}}

	err := SyncOnce(context.Background(), cli, cache, cfg)
	// Per-workspace clone fails (bad host), so we expect a non-nil aggregated
	// error that mentions both workspaces — but no panic.
	if err == nil {
		t.Fatal("expected aggregated error from failed clones, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ws-A") || !strings.Contains(msg, "ws-B") {
		t.Errorf("expected error to mention both workspaces, got: %s", msg)
	}
}

func TestSyncOnce_BumpsSyncErrorMetric(t *testing.T) {
	srv := fakeReposServer(t, map[string][]string{
		"ws-metric-A": {"https://example.invalid/owner/repo.git"},
	})
	defer srv.Close()

	cli := daemon.NewClient(srv.URL)
	cli.SetToken("tk")
	dir := t.TempDir()
	cache := repocache.New(filepath.Join(dir, "repos"), testLogger(t))

	cfg := &Config{Workspaces: []WorkspaceConfig{{ID: "ws-metric-A"}}}
	before := testutil.ToFloat64(syncTotal.WithLabelValues("ws-metric-A", "sync_error"))
	_ = SyncOnce(context.Background(), cli, cache, cfg)
	after := testutil.ToFloat64(syncTotal.WithLabelValues("ws-metric-A", "sync_error"))

	if after-before < 1 {
		t.Errorf("expected sync_error counter to increment by at least 1, before=%f after=%f", before, after)
	}
}

func TestSyncOnce_BumpsReposFetchErrorMetric(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cli := daemon.NewClient(srv.URL)
	cli.SetToken("tk")
	dir := t.TempDir()
	cache := repocache.New(filepath.Join(dir, "repos"), testLogger(t))

	cfg := &Config{Workspaces: []WorkspaceConfig{{ID: "ws-metric-B"}}}
	before := testutil.ToFloat64(syncTotal.WithLabelValues("ws-metric-B", "repos_fetch_error"))
	_ = SyncOnce(context.Background(), cli, cache, cfg)
	after := testutil.ToFloat64(syncTotal.WithLabelValues("ws-metric-B", "repos_fetch_error"))

	if after-before < 1 {
		t.Errorf("expected repos_fetch_error counter to increment by at least 1, before=%f after=%f", before, after)
	}
}

func TestSyncOnce_GetReposFailureAggregated(t *testing.T) {
	// Server returns 500 for everything: GetWorkspaceRepos should fail per ws.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cli := daemon.NewClient(srv.URL)
	cli.SetToken("tk")
	dir := t.TempDir()
	cache := repocache.New(filepath.Join(dir, "repos"), testLogger(t))

	cfg := &Config{Workspaces: []WorkspaceConfig{{ID: "ws-X"}}}
	err := SyncOnce(context.Background(), cli, cache, cfg)
	if err == nil || !strings.Contains(err.Error(), "ws-X") {
		t.Errorf("expected error mentioning ws-X, got: %v", err)
	}
}
