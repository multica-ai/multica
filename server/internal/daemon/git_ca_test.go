package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

type caRecordingCache struct {
	mu    sync.Mutex
	certs []string
	err   error
}

func (c *caRecordingCache) Lookup(string, string) string                 { return "" }
func (c *caRecordingCache) BarePath(string, string) string               { return "" }
func (c *caRecordingCache) Sync(string, []repocache.RepoInfo) error      { return nil }
func (c *caRecordingCache) WithRepoLock(_ string, fn func() error) error { return fn() }
func (c *caRecordingCache) CreateWorktree(repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	return nil, nil
}

func (c *caRecordingCache) SetExtraCACerts(pemCerts string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.certs = append(c.certs, pemCerts)
	return c.err
}

func TestRefreshWorkspaceReposAppliesServerCACerts(t *testing.T) {
	t.Parallel()

	const pemCerts = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WorkspaceReposResponse{WorkspaceID: "ws-1", ReposVersion: "v1", CACerts: pemCerts})
	}))
	t.Cleanup(srv.Close)

	cache := &caRecordingCache{}
	d := &Daemon{
		client:     NewClient(srv.URL),
		repoCache:  cache,
		logger:     slog.Default(),
		workspaces: map[string]*workspaceState{"ws-1": newWorkspaceState("ws-1", nil, "", nil, nil)},
	}
	if _, err := d.refreshWorkspaceRepos(context.Background(), "ws-1"); err != nil {
		t.Fatal(err)
	}
	if len(cache.certs) != 1 || cache.certs[0] != pemCerts {
		t.Fatalf("SetExtraCACerts calls = %q, want the server's PEM once", cache.certs)
	}
}

func TestApplyGitCACertsLogsPersistentFailureOnce(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	cache := &caRecordingCache{err: errors.New("not applied on Windows")}
	d := &Daemon{repoCache: cache, logger: slog.New(slog.NewTextHandler(&logs, nil))}

	d.applyGitCACerts("pem")
	d.applyGitCACerts("pem")
	if n := strings.Count(logs.String(), "git CA:"); n != 1 {
		t.Fatalf("logged %d warnings, want 1:\n%s", n, logs.String())
	}

	cache.err = nil
	d.applyGitCACerts("pem")
	cache.err = errors.New("not applied on Windows")
	d.applyGitCACerts("pem")
	if n := strings.Count(logs.String(), "git CA:"); n != 2 {
		t.Fatalf("logged %d warnings after recovery and new failure, want 2", n)
	}
}
