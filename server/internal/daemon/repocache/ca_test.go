package repocache

import (
	"context"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newHTTPSGitServer serves a one-commit repository over HTTPS with a
// self-signed certificate (httptest's) through git http-backend, and returns
// the clone URL and the server's certificate as PEM.
func newHTTPSGitServer(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("private CA bundles are not applied on Windows")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	execPath, err := exec.Command(gitPath, "--exec-path").Output()
	if err != nil {
		t.Skip("git --exec-path failed")
	}
	backend := filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend not available")
	}
	if len(readSystemCABundle()) == 0 {
		t.Skip("no system CA bundle")
	}

	root := t.TempDir()
	source := createTestRepo(t)
	served := filepath.Join(root, "app.git")
	if out, err := exec.Command(gitPath, "clone", "--bare", source, served).CombinedOutput(); err != nil {
		t.Fatalf("prepare served repo: %s: %v", out, err)
	}
	// http-backend refuses anonymous pushes unless receive-pack is enabled.
	if out, err := exec.Command(gitPath, "-C", served, "config", "http.receivepack", "true").CombinedOutput(); err != nil {
		t.Fatalf("enable receive-pack: %s: %v", out, err)
	}

	srv := httptest.NewTLSServer(&cgi.Handler{
		Path: backend,
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	})
	t.Cleanup(srv.Close)
	t.Setenv("NO_PROXY", "*")
	t.Setenv("no_proxy", "*")

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	return srv.URL + "/app.git", string(caPEM)
}

// agentGit runs git the way an agent does: without gitEnv, so only the
// repository's own config can supply the CA bundle.
func agentGit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestExtraCACertsLetGitReachPrivateCAHost(t *testing.T) {
	repoURL, caPEM := newHTTPSGitServer(t)
	t.Cleanup(func() { activeCABundle.Store(nil) })

	cache := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	repos := []RepoInfo{{URL: repoURL}}

	err := cache.Sync("ws-1", repos)
	if err == nil {
		t.Fatal("sync without the CA succeeded; the test server is unexpectedly trusted")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "certificate") {
		t.Fatalf("sync without the CA: want a certificate error, got %v", err)
	}

	if err := cache.SetExtraCACerts(caPEM); err != nil {
		t.Fatalf("SetExtraCACerts: %v", err)
	}
	if err := cache.Sync("ws-1", repos); err != nil {
		t.Fatalf("sync with the CA: %v", err)
	}
	barePath := cache.Lookup("ws-1", repoURL)
	if barePath == "" {
		t.Fatal("repo not cached after sync")
	}

	bundle, err := os.ReadFile(cache.caBundlePath())
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if !strings.HasPrefix(string(bundle), string(readSystemCABundle())) || !strings.HasSuffix(string(bundle), caPEM) {
		t.Fatal("bundle must be the system bundle followed by the server's CA")
	}

	for _, isolated := range []bool{false, true} {
		workDir := t.TempDir()
		result, err := cache.CreateWorktree(WorktreeParams{
			WorkspaceID:         "ws-1",
			RepoURL:             repoURL,
			WorkDir:             workDir,
			AgentName:           "agent",
			TaskID:              "task-ca",
			IsolatedGitMetadata: isolated,
		})
		if err != nil {
			t.Fatalf("CreateWorktree(isolated=%v): %v", isolated, err)
		}
		if out, err := agentGit(t, result.Path, "fetch", "origin"); err != nil {
			t.Fatalf("agent fetch (isolated=%v): %s: %v", isolated, out, err)
		}
		if out, err := agentGit(t, result.Path, "push", "origin", "HEAD:refs/heads/agent-push-"+result.BranchName); err != nil {
			t.Fatalf("agent push (isolated=%v): %s: %v", isolated, out, err)
		}
	}

	if err := cache.SetExtraCACerts(""); err != nil {
		t.Fatalf("clear CA: %v", err)
	}
	if _, err := os.Stat(cache.caBundlePath()); !os.IsNotExist(err) {
		t.Fatalf("bundle still present after clearing: %v", err)
	}
	cache.configureRepoCABundleLogged(context.Background(), barePath)
	if out, _ := agentGit(t, barePath, "config", "--local", "--get", caBundleConfigKey); strings.TrimSpace(out) != "" {
		t.Fatalf("%s still set after clearing: %q", caBundleConfigKey, out)
	}
}

func TestConfigureRepoCABundleKeepsManualValue(t *testing.T) {
	t.Cleanup(func() { activeCABundle.Store(nil) })
	repo := createTestRepo(t)
	cache := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	if out, err := agentGit(t, repo, "config", "--local", caBundleConfigKey, "/etc/custom/ca.pem"); err != nil {
		t.Fatalf("set manual value: %s: %v", out, err)
	}
	activeCABundle.Store(nil)
	if err := cache.configureRepoCABundleContext(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if out, _ := agentGit(t, repo, "config", "--local", "--get", caBundleConfigKey); strings.TrimSpace(out) != "/etc/custom/ca.pem" {
		t.Fatalf("manual %s was changed: %q", caBundleConfigKey, out)
	}
}

func TestSetExtraCACertsRejectsInvalidPEM(t *testing.T) {
	t.Cleanup(func() { activeCABundle.Store(nil) })
	cache := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := cache.SetExtraCACerts("not a certificate"); err == nil {
		t.Fatal("expected an error for input without a certificate")
	}
	if currentCABundle() != "" {
		t.Fatal("invalid input must not activate a bundle")
	}
}

func TestGitEnvAddsCABundleAfterExistingConfig(t *testing.T) {
	t.Cleanup(func() { activeCABundle.Store(nil) })
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "http.https://example.com/.sslCAInfo")
	t.Setenv("GIT_CONFIG_VALUE_0", "/etc/example.pem")
	path := "/tmp/bundle.pem"
	activeCABundle.Store(&path)

	env := envMap(gitEnv())
	want := map[string]string{
		"GIT_CONFIG_COUNT":   "3",
		"GIT_CONFIG_KEY_0":   "http.https://example.com/.sslCAInfo",
		"GIT_CONFIG_KEY_1":   "safe.directory",
		"GIT_CONFIG_KEY_2":   caBundleConfigKey,
		"GIT_CONFIG_VALUE_2": path,
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
}

// envMap resolves duplicate keys the way os/exec does: the last one wins.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}
