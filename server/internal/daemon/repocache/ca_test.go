package repocache

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// isolateGitConfig keeps the developer's own git config out of a test and
// resets the package CA state afterwards. Tests using it cannot run in
// parallel because they set environment variables.
func isolateGitConfig(t *testing.T) string {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("NO_PROXY", "*")
	t.Setenv("no_proxy", "*")
	t.Cleanup(func() { caState.Store(nil) })
	return global
}

// newServerCert returns a self-signed certificate for 127.0.0.1 and its PEM.
func newServerCert(t *testing.T, name string) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// newHTTPSGitServer serves a one-commit repository over HTTPS, with its own
// self-signed certificate, through git http-backend. It returns the clone URL
// and the certificate as PEM.
func newHTTPSGitServer(t *testing.T, name string) (string, string) {
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
	served := filepath.Join(root, "app.git")
	if out, err := exec.Command(gitPath, "clone", "--bare", createTestRepo(t), served).CombinedOutput(); err != nil {
		t.Fatalf("prepare served repo: %s: %v", out, err)
	}
	// http-backend refuses anonymous pushes unless receive-pack is enabled.
	if out, err := exec.Command(gitPath, "-C", served, "config", "http.receivepack", "true").CombinedOutput(); err != nil {
		t.Fatalf("enable receive-pack: %s: %v", out, err)
	}

	cert, certPEM := newServerCert(t, name)
	srv := httptest.NewUnstartedServer(&cgi.Handler{
		Path: backend,
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	})
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.URL + "/app.git", certPEM
}

// agentGit runs git the way an agent does: with the inherited environment and
// no daemon-added options, so only git config can supply the CA bundle.
func agentGit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func newTestCacheQuiet(t *testing.T) *Cache {
	t.Helper()
	return New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func createCheckouts(t *testing.T, cache *Cache, repoURL, taskID string) map[string]string {
	t.Helper()
	paths := map[string]string{}
	for name, isolated := range map[string]bool{"linked": false, "isolated": true} {
		result, err := cache.CreateWorktree(WorktreeParams{
			WorkspaceID:         "ws-1",
			RepoURL:             repoURL,
			WorkDir:             t.TempDir(),
			AgentName:           "agent",
			TaskID:              taskID,
			IsolatedGitMetadata: isolated,
		})
		if err != nil {
			t.Fatalf("CreateWorktree(%s): %v", name, err)
		}
		paths[name] = result.Path
	}
	return paths
}

func TestExtraCACertsLetGitReachPrivateCAHost(t *testing.T) {
	isolateGitConfig(t)
	repoURL, caPEM := newHTTPSGitServer(t, "backend-ca")
	cache := newTestCacheQuiet(t)
	repos := []RepoInfo{{URL: repoURL}}

	err := cache.Sync("ws-1", repos)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "certificate") {
		t.Fatalf("sync without the CA: want a certificate error, got %v", err)
	}

	if err := cache.SetExtraCACerts(caPEM); err != nil {
		t.Fatalf("SetExtraCACerts: %v", err)
	}
	if err := cache.Sync("ws-1", repos); err != nil {
		t.Fatalf("sync with the CA: %v", err)
	}
	bundle, err := os.ReadFile(cache.caBundlePath())
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if string(bundle) != string(readSystemCABundle())+caPEM && string(bundle) != string(readSystemCABundle())+"\n"+caPEM {
		t.Fatal("bundle must be the system bundle followed by the server's CA")
	}

	for name, path := range createCheckouts(t, cache, repoURL, "task-ca") {
		if out, err := agentGit(t, path, "fetch", "origin"); err != nil {
			t.Fatalf("agent fetch (%s): %s: %v", name, out, err)
		}
		if out, err := agentGit(t, path, "push", "origin", "HEAD:refs/heads/push-"+name); err != nil {
			t.Fatalf("agent push (%s): %s: %v", name, out, err)
		}
	}

	if err := cache.SetExtraCACerts(""); err != nil {
		t.Fatalf("clear CA: %v", err)
	}
	if got, err := os.ReadFile(cache.caBundlePath()); err != nil || string(got) != string(readSystemCABundle()) {
		t.Fatalf("after clearing, the bundle must hold the system trust alone (err=%v)", err)
	}
	barePath := cache.Lookup("ws-1", repoURL)
	if err := configureRepoCABundleContext(context.Background(), barePath); err != nil {
		t.Fatal(err)
	}
	if out, _ := agentGit(t, barePath, "config", "--local", "--get", caBundleConfigKey); strings.TrimSpace(out) != "" {
		t.Fatalf("%s still set after clearing: %q", caBundleConfigKey, out)
	}
}

// An operator's general http.sslCAInfo must keep working while backend CAs
// are added, and checkouts created meanwhile must keep working after the
// backend CAs are removed.
func TestExtraCACertsKeepOperatorTrust(t *testing.T) {
	global := isolateGitConfig(t)
	operatorURL, operatorPEM := newHTTPSGitServer(t, "operator-ca")
	backendURL, backendPEM := newHTTPSGitServer(t, "backend-ca")

	machinePEM := filepath.Join(t.TempDir(), "machine.pem")
	machineTrust := string(readSystemCABundle()) + "\n" + operatorPEM
	if err := os.WriteFile(machinePEM, []byte(machineTrust), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "config", "--file", global, caBundleConfigKey, machinePEM).CombinedOutput(); err != nil {
		t.Fatalf("set operator %s: %s: %v", caBundleConfigKey, out, err)
	}

	cache := newTestCacheQuiet(t)
	if err := cache.Sync("ws-1", []RepoInfo{{URL: operatorURL}}); err != nil {
		t.Fatalf("operator repo before backend CAs: %v", err)
	}

	if err := cache.SetExtraCACerts(backendPEM); err != nil {
		t.Fatalf("SetExtraCACerts: %v", err)
	}
	if bundle, _ := os.ReadFile(cache.caBundlePath()); !strings.HasPrefix(string(bundle), machineTrust) {
		t.Fatal("bundle must start with the operator's http.sslCAInfo, not the system bundle")
	}
	if err := cache.Sync("ws-1", []RepoInfo{{URL: operatorURL}, {URL: backendURL}}); err != nil {
		t.Fatalf("sync with backend CAs: %v", err)
	}
	operatorCheckouts := createCheckouts(t, cache, operatorURL, "task-op")
	for name, path := range operatorCheckouts {
		if out, err := agentGit(t, path, "fetch", "origin"); err != nil {
			t.Fatalf("agent fetch from operator repo (%s): %s: %v", name, out, err)
		}
	}
	for name, path := range createCheckouts(t, cache, backendURL, "task-be") {
		if out, err := agentGit(t, path, "fetch", "origin"); err != nil {
			t.Fatalf("agent fetch from backend-CA repo (%s): %s: %v", name, out, err)
		}
	}

	if err := cache.SetExtraCACerts(""); err != nil {
		t.Fatalf("clear CA: %v", err)
	}
	// Existing checkouts still point at the bundle and are not revisited.
	for name, path := range operatorCheckouts {
		if out, err := agentGit(t, path, "fetch", "origin"); err != nil {
			t.Fatalf("agent fetch from operator repo after clearing (%s): %s: %v", name, out, err)
		}
	}
	if got, err := os.ReadFile(cache.caBundlePath()); err != nil || string(got) != machineTrust {
		t.Fatalf("after clearing, the bundle must hold the operator's trust alone (err=%v)", err)
	}
	if err := cache.Sync("ws-1", []RepoInfo{{URL: operatorURL}}); err != nil {
		t.Fatalf("operator repo after clearing: %v", err)
	}
}

func TestConfigureRepoCABundleKeepsManualValue(t *testing.T) {
	isolateGitConfig(t)
	repo := createTestRepo(t)
	const manual = "/etc/custom/ca.pem"
	if out, err := agentGit(t, repo, "config", "--local", caBundleConfigKey, manual); err != nil {
		t.Fatalf("set manual value: %s: %v", out, err)
	}

	for _, active := range []bool{true, false} {
		caState.Store(&caBundleState{path: "/tmp/managed/bundle.pem", active: active})
		if err := configureRepoCABundleContext(context.Background(), repo); err != nil {
			t.Fatal(err)
		}
		if out, _ := agentGit(t, repo, "config", "--local", "--get", caBundleConfigKey); strings.TrimSpace(out) != manual {
			t.Fatalf("active=%v: manual %s was changed to %q", active, caBundleConfigKey, out)
		}
	}
}

func TestSetExtraCACertsRejectsInvalidPEM(t *testing.T) {
	isolateGitConfig(t)
	cache := newTestCacheQuiet(t)
	if err := cache.SetExtraCACerts("not a certificate"); err == nil {
		t.Fatal("expected an error for input without a certificate")
	}
	if activeCABundle() != "" {
		t.Fatal("invalid input must not activate a bundle")
	}
}

func TestGitEnvDoesNotOverrideRepoConfig(t *testing.T) {
	isolateGitConfig(t)
	caState.Store(&caBundleState{path: "/tmp/managed/bundle.pem", active: true})
	for _, kv := range gitEnv() {
		if strings.Contains(kv, caBundleConfigKey) {
			t.Fatalf("gitEnv must not set %s, which would override operator config: %s", caBundleConfigKey, kv)
		}
	}
}
