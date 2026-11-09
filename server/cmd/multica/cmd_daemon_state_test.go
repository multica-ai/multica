package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/spf13/cobra"
)

func TestBackendLegacyMigrationPreservesRuntimeAndSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	root := filepath.Join(t.TempDir(), "legacy-work")
	if err := os.MkdirAll(filepath.Join(root, "workspace", "issue"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := daemon.Config{Profile: "legacy", ServerBaseURL: "https://example.test", WorkspacesRoot: root, DaemonID: "original-runtime", LegacyDaemonIDs: []string{"hostname"}}
	dir, _ := backendServiceDir(cfg.ServerBaseURL)
	if err := bindBackendAccount(dir, &cfg, "a"); err == nil || !strings.Contains(err.Error(), "migrate-state") {
		t.Fatalf("silently replaced legacy state: %v", err)
	}
	if err := migrateLegacyExecutionState(dir, cfg, "a"); err != nil {
		t.Fatal(err)
	}
	next := daemon.Config{Profile: "desktop-example", ServerBaseURL: cfg.ServerBaseURL, WorkspacesRoot: t.TempDir(), DaemonID: "machine"}
	if err := bindBackendAccount(dir, &next, "a"); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := util.ResolveSymlinksBestEffort(root)
	if err != nil {
		t.Fatal(err)
	}
	if next.ExecutionStateProfile() != "legacy" || next.WorkspacesRoot != canonicalRoot || next.DaemonID != "original-runtime" || len(next.LegacyDaemonIDs) != 1 {
		t.Fatalf("migration lost runtime/workdir/session namespace: %+v", next)
	}
	if err := migrateLegacyExecutionState(dir, cfg, "b"); err == nil {
		t.Fatal("another account adopted the migrated namespace")
	}
	other := daemon.Config{Profile: "legacy", ServerBaseURL: cfg.ServerBaseURL, WorkspacesRoot: root, DaemonID: "machine"}
	if err := bindBackendAccount(dir, &other, "b"); err != nil {
		t.Fatal(err)
	}
	if other.WorkspacesRoot == root || other.ExecutionStateProfile() == "legacy" || other.DaemonID == cfg.DaemonID {
		t.Fatal("new account reused migrated state")
	}
}

func TestBackendMigrationCommandRequiresStoppedService(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"id": "a"})
	}))
	defer server.Close()
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{Token: "test-token", ServerURL: server.URL}, ""); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspaces-root", t.TempDir(), "")
	dir, _ := backendServiceDir(server.URL)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	lock, acquired, err := tryLockDaemonProfile(filepath.Join(dir, "service.lock"))
	if err != nil || !acquired {
		t.Fatal(err)
	}
	if err := runDaemonMigrateState(cmd, nil); err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatalf("migrated running service: %v", err)
	}
	lock.Close()
	if err := runDaemonMigrateState(cmd, nil); err != nil {
		t.Fatal(err)
	}
	state, err := readAccountState(accountStatePath(dir, "a"))
	if err != nil || state.StateProfile != "" || state.DaemonID == "" {
		t.Fatalf("default profile migration failed: %+v %v", state, err)
	}
}

func TestBackendDiagnosticsFollowSharedServiceAndOfflineBinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	root := filepath.Join(t.TempDir(), "work", ".multica-accounts", "a")
	if err := os.MkdirAll(filepath.Join(root, "workspace", "issue"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", "issue", "result"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	var record backendServiceRecord
	var running atomic.Bool
	running.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me" {
			switch r.Header.Get("Authorization") {
			case "Bearer mul_a":
				json.NewEncoder(w).Encode(map[string]string{"id": "a"})
			case "Bearer mul_b":
				json.NewEncoder(w).Encode(map[string]string{"id": "b"})
			default:
				w.WriteHeader(http.StatusUnauthorized)
			}
			return
		}
		if !running.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "running", "pid": record.PID, "profile": record.Profile, "owner_id": record.OwnerID, "daemon_id": record.DaemonID, "server_url": record.ServerURL})
	}))
	defer server.Close()
	_, portString, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	record = backendServiceRecord{ServerURL: server.URL, Profile: "desktop-example", PID: 123, Port: port, OwnerID: "a", DaemonID: "machine", WorkspacesRoot: root}
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: server.URL, Token: "mul_a"}, ""); err != nil {
		t.Fatal(err)
	}
	dir, _ := backendServiceDir(server.URL)
	if err := writeServiceJSON(filepath.Join(dir, "active.json"), record); err != nil {
		t.Fatal(err)
	}
	seedDaemonLog(t, daemonLogPathForProfile(record.Profile))
	cmd, errOut := daemonLogsCmdFor(t, "", 20, false)
	tail := stubTailLog(t, errOut)
	if err := runDaemonLogs(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if tail.path != daemonLogPathForProfile(record.Profile) {
		t.Fatalf("read launcher log instead of service log: %s", tail.path)
	}
	for _, tc := range []struct {
		token string
		want  string
	}{
		{"mul_b", "belongs to another account"},
		{"mul_expired", "Could not verify the current account"},
	} {
		if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: server.URL, Token: tc.token}, ""); err != nil {
			t.Fatal(err)
		}
		tail.called = false
		errOut.Reset()
		if err := runDaemonLogs(cmd, nil); err == nil || !strings.Contains(cli.FormatError(err, false), tc.want) {
			t.Fatalf("expected %q, got %v", tc.want, err)
		}
		if tail.called || errOut.Len() != 0 {
			t.Fatal("rejected caller reached the log tail or received the log path")
		}
	}
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: server.URL, Token: "mul_a"}, ""); err != nil {
		t.Fatal(err)
	}

	got, err := resolveDiskUsageRoot(&cobra.Command{}, false, "", "")
	if err != nil || got != root {
		t.Fatalf("wrong live root: %q %v", got, err)
	}
	if got, _ := resolveDiskUsageRoot(&cobra.Command{}, false, "", "/explicit"); got != "/explicit" {
		t.Fatal("lost explicit root override")
	}
	if err := writeServiceJSON(accountStatePath(dir, "a"), backendAccountState{OwnerID: "a", StateProfile: "execution-a", WorkspacesRoot: root, DaemonID: "machine"}); err != nil {
		t.Fatal(err)
	}
	running.Store(false)
	got, err = resolveDiskUsageRoot(&cobra.Command{}, false, "", "")
	if err != nil || got != root {
		t.Fatalf("wrong authenticated root with daemon stopped: %q %v", got, err)
	}
	roots, err := enumerateDiskUsageRoots()
	if err != nil {
		t.Fatal(err)
	}
	report, err := daemon.ScanDiskUsageRoots(roots, nil)
	if err != nil || report.TotalTaskCount != 1 || report.TotalSizeBytes != 4 {
		t.Fatalf("missed account tree: %+v %v", report, err)
	}
	server.Close()
	if got, err := resolveDiskUsageRoot(&cobra.Command{}, false, "", ""); err == nil || got != "" || !strings.Contains(err.Error(), "offline inspection") {
		t.Fatalf("offline diagnostics guessed account: %q %v", got, err)
	} else if message := cli.FormatError(err, false); !strings.Contains(message, "--workspaces-root") || cli.ExitCodeFor(err) != cli.ExitNetwork {
		t.Fatalf("lost offline guidance or network exit code: %s (exit %d)", message, cli.ExitCodeFor(err))
	}
	if got, err := resolveDiskUsageRoot(&cobra.Command{}, false, "", root); err != nil || got != root {
		t.Fatalf("explicit offline root unavailable: %q %v", got, err)
	}

}

func TestBackendRestartForegroundUsesCallingProcess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(map[string]string{"id": "a"}) }))
	defer server.Close()
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{Token: "test-token", ServerURL: server.URL}, ""); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().Bool("foreground", true, "")
	old := runTransitionForeground
	t.Cleanup(func() { runTransitionForeground = old })
	called := false
	runTransitionForeground = func(cmd *cobra.Command) error {
		called = true
		if owner, _ := cmd.Context().Value(daemonTransitionOwnerKey{}).(string); owner != "a" {
			t.Fatal("lost authenticated transition")
		}
		return nil
	}
	if err := runBackendTransitionLocked(cmd, false); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("foreground restart spawned a background replacement")
	}
}

func TestBackendMigrationRejectsAliasedAccountRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	dir, err := backendServiceDir("https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeServiceJSON(accountStatePath(dir, "a"), backendAccountState{OwnerID: "a", StateProfile: "profile-a", WorkspacesRoot: root, DaemonID: "daemon-a"}); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{alias, filepath.Join(alias, "missing-child")} {
		cfg := daemon.Config{Profile: "profile-b", ServerBaseURL: "https://example.test", WorkspacesRoot: candidate, DaemonID: "daemon-b"}
		if err := migrateLegacyExecutionState(dir, cfg, "b"); err == nil || !strings.Contains(err.Error(), "already bound") {
			t.Fatalf("aliased account root accepted: %s (%v)", candidate, err)
		}
	}
	if _, err := os.Stat(accountStatePath(dir, "b")); !os.IsNotExist(err) {
		t.Fatalf("failed migration created binding: %v", err)
	}
	// An unrelated tree must remain available for explicit adoption.
	cfg := daemon.Config{Profile: "profile-c", ServerBaseURL: "https://example.test", WorkspacesRoot: t.TempDir(), DaemonID: "daemon-c"}
	if err := migrateLegacyExecutionState(dir, cfg, "c"); err != nil {
		t.Fatalf("independent state rejected: %v", err)
	}
}

func TestBackendForegroundRejectsAccountChangedAfterConfirmation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Chdir(t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := "a"
		if r.Header.Get("Authorization") == "Bearer mul_b" {
			owner = "b"
		}
		json.NewEncoder(w).Encode(map[string]string{"id": owner})
	}))
	defer server.Close()
	config := cli.CLIConfig{Token: "mul_a", ServerURL: server.URL}
	if err := cli.SaveCLIConfigForProfile(config, ""); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().Bool("foreground", true, "")
	original := runTransitionForeground
	t.Cleanup(func() { runTransitionForeground = original })
	reachedStartup := false
	runTransitionForeground = func(cmd *cobra.Command) error {
		reachedStartup = true
		config.Token = "mul_b"
		if err := cli.SaveCLIConfigForProfile(config, ""); err != nil {
			t.Fatal(err)
		}
		cfg := daemon.Config{ServerBaseURL: server.URL, WorkspacesRoot: t.TempDir(), DaemonID: "machine"}
		lock, _, err := prepareBackendService(&cfg, expectedDaemonOwner(cmd))
		if lock != nil {
			lock.Close()
		}
		return err
	}
	operation, err := acquireBackendOperation(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer operation.Close()
	if err := runBackendTransitionLocked(cmd, false); err == nil || !strings.Contains(err.Error(), "confirmed account") {
		t.Fatalf("foreground started with unconfirmed account: %v", err)
	}
	if !reachedStartup {
		t.Fatal("did not reach foreground startup")
	}
	dir, _ := backendServiceDir(server.URL)
	if _, err := os.Stat(accountStatePath(dir, "b")); !os.IsNotExist(err) {
		t.Fatalf("wrong account was bound: %v", err)
	}
	lock, acquired, err := tryLockDaemonProfile(filepath.Join(dir, "service.lock"))
	if lock != nil {
		lock.Close()
	}
	if err != nil || !acquired {
		t.Fatal("failed startup leaked service lock")
	}
}

func TestBackendDesktopFirstDoesNotBlockLegacyCLIMigration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")
	const serverURL = "https://example.test"
	legacyRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(legacyRoot, "workspace", "issue"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: serverURL, WorkspacesRoot: legacyRoot}, ""); err != nil {
		t.Fatal(err)
	}
	desktop := daemon.Config{Profile: "desktop-example.test", ServerBaseURL: serverURL, WorkspacesRoot: filepath.Join(t.TempDir(), "fresh-desktop"), DaemonID: "machine"}
	dir, _ := backendServiceDir(serverURL)
	if err := bindBackendAccount(dir, &desktop, "a"); err == nil || !strings.Contains(err.Error(), "migrate-state") {
		t.Fatalf("Desktop hid legacy CLI state: %v", err)
	}
	if _, err := os.Stat(accountStatePath(dir, "a")); !os.IsNotExist(err) {
		t.Fatalf("Desktop created binding before migration: %v", err)
	}
	legacy := daemon.Config{ServerBaseURL: serverURL, WorkspacesRoot: legacyRoot, DaemonID: "legacy-runtime"}
	if err := migrateLegacyExecutionState(dir, legacy, "a"); err != nil {
		t.Fatalf("CLI migration blocked: %v", err)
	}
	if err := bindBackendAccount(dir, &desktop, "a"); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, _ := util.ResolveSymlinksBestEffort(legacyRoot)
	if desktop.WorkspacesRoot != canonicalRoot || desktop.DaemonID != "legacy-runtime" || desktop.ExecutionStateProfile() != "" {
		t.Fatal("Desktop did not preserve migrated CLI state")
	}
	// Legacy data from another backend must not block a new deployment.
	otherURL := "https://other.test"
	other := daemon.Config{Profile: "desktop-other.test", ServerBaseURL: otherURL, WorkspacesRoot: filepath.Join(t.TempDir(), "fresh-other"), DaemonID: "machine"}
	otherDir, _ := backendServiceDir(otherURL)
	if err := bindBackendAccount(otherDir, &other, "a"); err != nil {
		t.Fatalf("unrelated backend was blocked: %v", err)
	}
}

func TestBackendDiskUsageHonorsExplicitServerURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("MULTICA_SERVER_URL", "")
	var requestsA, requestsB atomic.Int32
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsA.Add(1)
		json.NewEncoder(w).Encode(map[string]string{"id": "a"})
	}))
	defer serverA.Close()
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsB.Add(1)
		json.NewEncoder(w).Encode(map[string]string{"id": "a"})
	}))
	defer serverB.Close()
	rootA, rootB := t.TempDir(), t.TempDir()
	for serverURL, root := range map[string]string{serverA.URL: rootA, serverB.URL: rootB} {
		dir, _ := backendServiceDir(serverURL)
		if err := writeServiceJSON(accountStatePath(dir, "a"), backendAccountState{OwnerID: "a", StateProfile: "execution-a", WorkspacesRoot: root, DaemonID: "machine"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: serverA.URL, Token: "mul_a"}, ""); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("server-url", "", "")
	if err := cmd.Flags().Set("server-url", serverB.URL); err != nil {
		t.Fatal(err)
	}
	got, err := resolveDiskUsageRoot(cmd, false, "", "")
	if err != nil || got != rootB || requestsA.Load() != 0 || requestsB.Load() != 1 {
		t.Fatalf("wrong backend selection: %q %v, requests A=%d B=%d", got, err, requestsA.Load(), requestsB.Load())
	}
}

func TestBackendDiskUsageDoesNotInferOwnerFromSingleBinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("MULTICA_SERVER_URL", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(map[string]string{"id": "b"}) }))
	defer server.Close()
	dir, _ := backendServiceDir(server.URL)
	rootA, rootB := t.TempDir(), t.TempDir()
	stateA := backendAccountState{OwnerID: "a", StateProfile: "execution-a", WorkspacesRoot: rootA, DaemonID: "daemon-a"}
	if err := writeServiceJSON(accountStatePath(dir, "a"), stateA); err != nil {
		t.Fatal(err)
	}
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: server.URL, Token: "mul_b"}, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveDiskUsageRoot(&cobra.Command{}, false, "", ""); err == nil || got != "" || !strings.Contains(err.Error(), "no saved execution state") {
		t.Fatalf("selected A's only binding for B: %q %v", got, err)
	}
	if err := writeServiceJSON(accountStatePath(dir, "b"), backendAccountState{OwnerID: "b", StateProfile: "execution-b", WorkspacesRoot: rootB, DaemonID: "daemon-b"}); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveDiskUsageRoot(&cobra.Command{}, false, "", ""); err != nil || got != rootB {
		t.Fatalf("did not select verified B: %q %v", got, err)
	}
	// Even a correctly named file must attest the expected owner.
	if err := writeServiceJSON(accountStatePath(dir, "b"), stateA); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveDiskUsageRoot(&cobra.Command{}, false, "", ""); err == nil || got != "" {
		t.Fatalf("accepted wrong owner inside binding: %q %v", got, err)
	}
}
