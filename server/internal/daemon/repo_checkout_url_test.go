package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

func TestRepoCheckoutEquivalentURLsUseRegisteredRepo(t *testing.T) {
	const scpURL = "git@gitlab.example.com:group/project.git"
	const httpURL = "http://gitlab.example.com/group/project.git"
	const httpsURL = "https://gitlab.example.com/group/project.git"
	const sshPortURL = "ssh://git@gitlab.example.com:22/group/project.git"
	const httpsPortURL = "https://gitlab.example.com:443/group/project.git"
	const wsID = "ws-checkout"

	// Keep the reported URLs in the request and cache, but direct Git to a
	// local fixture so these tests require neither network nor credentials.
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	source := createDaemonTestRepo(t)
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("-C", source, "branch", "-M", "main")
	git("-C", source, "checkout", "-b", "release/v2")
	git("-C", source, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "release-only commit")
	releaseSHA := git("-C", source, "rev-parse", "HEAD")
	git("-C", source, "checkout", "main")
	mainSHA := git("-C", source, "rev-parse", "HEAD")
	for _, raw := range []string{scpURL, httpURL, httpsURL, sshPortURL, httpsPortURL} {
		git("config", "--file", config, "--add", "url."+source+".insteadOf", raw)
	}

	for _, tc := range []struct {
		name, registered, requested, projectURL, explicitRef string
		cold, isolated                                       bool
	}{
		{name: "reported HTTP input", registered: scpURL, requested: httpURL},
		{name: "HTTPS input", registered: scpURL, requested: httpsURL, cold: true},
		{name: "project ref through HTTP alias", registered: scpURL, requested: httpURL, projectURL: scpURL},
		{name: "explicit ref overrides project", registered: scpURL, requested: httpURL, projectURL: scpURL, explicitRef: "main"},
		{name: "registered default port with cold cache", registered: sshPortURL, requested: httpsURL, cold: true},
		{name: "requested default port with cold cache", registered: scpURL, requested: httpsPortURL, cold: true},
		{name: "registered HTTPS port with warm cache", registered: httpsPortURL, requested: scpURL},
		{name: "workspace and project use different forms", registered: httpsURL, requested: httpsURL, projectURL: scpURL},
		{name: "isolated checkout retains registered origin", registered: scpURL, requested: httpURL, projectURL: scpURL, isolated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := repocache.New(t.TempDir(), slog.Default())
			workDir := t.TempDir()
			d := newRepoCheckoutTestDaemon(t, wsID, tc.registered, workDir, cache)
			t.Cleanup(d.waitBackgroundSyncs)
			if !tc.cold {
				if err := cache.Sync(wsID, []repocache.RepoInfo{{URL: tc.registered}}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.projectURL != "" {
				d.registerTaskRepos(wsID, "task-1", []RepoData{{URL: tc.projectURL, Ref: "release/v2"}})
				d.waitBackgroundSyncs()
			}
			mode := ""
			if tc.isolated {
				mode = repoCheckoutModeIsolated
			}
			body, err := json.Marshal(repoCheckoutRequest{
				URL: tc.requested, WorkspaceID: wsID, WorkDir: workDir,
				TaskID: "task-1", Ref: tc.explicitRef, CheckoutMode: mode,
			})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			d.repoCheckoutHandler().ServeHTTP(rec, authorizedRepoCheckoutRequest(strings.NewReader(string(body))))
			if rec.Code != http.StatusOK {
				t.Fatalf("checkout HTTP %d: %s", rec.Code, rec.Body.String())
			}
			var result repocache.WorktreeResult
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			wantSHA := mainSHA
			if tc.projectURL != "" && tc.explicitRef == "" {
				wantSHA = releaseSHA
			}
			if got := git("-C", result.Path, "rev-parse", "HEAD"); got != wantSHA {
				t.Errorf("checkout HEAD = %s, want %s", got, wantSHA)
			}
			if got := git("-C", result.Path, "config", "--get", "remote.origin.url"); got != tc.registered {
				t.Errorf("checkout origin = %q, want registered URL %q", got, tc.registered)
			}
			if !d.repoBarePathIsLive(cache.BarePath(wsID, tc.registered)) {
				t.Error("registered cache must remain live for GC")
			}
		})
	}
}
