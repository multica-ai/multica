package main

import (
	"encoding/json"
	"errors"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"
)

func TestBackendLifetimeLockAcrossProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "service.lock")
	a, ok, err := tryLockDaemonProfile(path)
	if err != nil || !ok {
		t.Fatal(err)
	}
	b, ok, err := tryLockDaemonProfile(path)
	if b != nil {
		b.Close()
	}
	if err != nil || ok {
		t.Fatalf("second launcher admitted: %v", err)
	}
	a.Close()
	b, ok, err = tryLockDaemonProfile(path)
	if err != nil || !ok {
		t.Fatal(err)
	}
	b.Close()
}

func TestBackendAccountBindingSurvivesLauncherAndAccountSwitch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")
	dir := t.TempDir()
	initial := daemon.Config{Profile: "", ServerBaseURL: "https://example.test", DaemonID: "machine", WorkspacesRoot: filepath.Join(t.TempDir(), "old-work")}
	if err := bindBackendAccount(dir, &initial, "account-a"); err != nil {
		t.Fatal(err)
	}
	if initial.ExecutionStateProfile() == "" || initial.DaemonID == "machine" || len(initial.LegacyDaemonIDs) > 0 {
		t.Fatal("first authenticated account adopted unproven legacy state")
	}
	next := daemon.Config{Profile: "desktop-example", ServerBaseURL: initial.ServerBaseURL, DaemonID: "machine", WorkspacesRoot: "fresh-desktop"}
	if err := bindBackendAccount(dir, &next, "account-a"); err != nil {
		t.Fatal(err)
	}
	if next.ExecutionStateProfile() != initial.ExecutionStateProfile() || next.WorkspacesRoot != initial.WorkspacesRoot || next.DaemonID != initial.DaemonID {
		t.Fatalf("lost old state: %+v", next)
	}
	other := daemon.Config{Profile: "desktop-example", ServerBaseURL: initial.ServerBaseURL, DaemonID: "machine", WorkspacesRoot: initial.WorkspacesRoot, LegacyDaemonIDs: []string{"legacy"}}
	if err := bindBackendAccount(dir, &other, "account-b"); err != nil {
		t.Fatal(err)
	}
	if other.ExecutionStateProfile() == initial.ExecutionStateProfile() || other.WorkspacesRoot == initial.WorkspacesRoot || other.DaemonID == initial.DaemonID || len(other.LegacyDaemonIDs) > 0 {
		t.Fatalf("cross-account state reuse: %+v", other)
	}
	again := daemon.Config{Profile: "desktop-example", DaemonID: "machine"}
	if err := bindBackendAccount(dir, &again, "account-a"); err != nil {
		t.Fatal(err)
	}
	if again.WorkspacesRoot != initial.WorkspacesRoot {
		t.Fatal("switching back lost account state")
	}
}

func TestBackendDiscoveryRejectsChangedIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	owner := "account-a"
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "running", "pid": 123, "profile": "cli", "owner_id": owner, "daemon_id": "machine", "server_url": "https://example.test"})
	}))
	defer service.Close()
	_, portStr, _ := net.SplitHostPort(service.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	dir, err := backendServiceDir("https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	record := backendServiceRecord{ServerURL: "https://example.test", OwnerID: "account-a", Profile: "cli", DaemonID: "machine", Port: port, PID: 123}
	if err := writeServiceJSON(filepath.Join(dir, "active.json"), record); err != nil {
		t.Fatal(err)
	}
	if got, _, err := liveBackendService(record.ServerURL); err != nil || got == nil {
		t.Fatalf("cannot discover service: %v", err)
	}
	owner = "account-b"
	if _, _, err := liveBackendService(record.ServerURL); err == nil {
		t.Fatal("adopted another account's process")
	}
	info, err := os.Stat(filepath.Join(dir, "active.json"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatal("record permissions")
	}
}

func TestBackendRestartCannotSwitchAccount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Chdir(t.TempDir())
	var stopped atomic.Bool
	var serverURL string
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			json.NewEncoder(w).Encode(map[string]string{"id": "account-b"})
		case "/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "running", "pid": 123, "profile": "old-profile", "owner_id": "account-a", "daemon_id": "machine", "server_url": serverURL})
		default:
			stopped.Store(true)
			w.WriteHeader(500)
		}
	}))
	defer fixture.Close()
	serverURL = fixture.URL
	dir, err := backendServiceDir(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	port := fixture.Listener.Addr().(*net.TCPAddr).Port
	if err := writeServiceJSON(filepath.Join(dir, "active.json"), backendServiceRecord{ServerURL: serverURL, OwnerID: "account-a", Profile: "old-profile", DaemonID: "machine", PID: 123, Port: port}); err != nil {
		t.Fatal(err)
	}
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{Token: "mul_fake"}, "desktop-test"); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("profile", "desktop-test", "")
	cmd.Flags().String("server-url", serverURL, "")
	_ = cmd.Flags().Set("server-url", serverURL)
	if err := runDaemonRestart(cmd, nil); err == nil || !strings.Contains(err.Error(), "another account owns") {
		t.Fatalf("restart authorized account switch: %v", err)
	}
	if stopped.Load() {
		t.Fatal("restart contacted stop/drain before rejecting owner")
	}
	cmd.Flags().Bool("desktop-credentials-stdin", true, "")
	cmd.SetIn(strings.NewReader(`{"token":"mul_new","owner_id":"account-c"}`))
	if err := runBackendTakeover(cmd, nil); err == nil || !strings.Contains(err.Error(), "could not verify") {
		t.Fatalf("unverified account reached handoff: %v", err)
	}
	config, _ := cli.LoadCLIConfigForProfile("desktop-test")
	if stopped.Load() || config.Token != "mul_fake" {
		t.Fatal("failed verification mutated the old service or credentials")
	}
}

func TestBackendCredentialCommitAndStartupHoldTransitionLock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Chdir(t.TempDir())
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"id": "account-b"})
	}))
	defer fixture.Close()
	cmd := &cobra.Command{}
	cmd.Flags().String("profile", "desktop-test", "")
	cmd.Flags().String("server-url", fixture.URL, "")
	_ = cmd.Flags().Set("server-url", fixture.URL)
	cmd.Flags().Bool("desktop-credentials-stdin", true, "")
	cmd.SetIn(strings.NewReader(`{"token":"mul_new","owner_id":"account-b"}`))
	originalExecutable, originalSync := daemonExecutable, syncDaemonAutostart
	defer func() { daemonExecutable = originalExecutable; syncDaemonAutostart = originalSync }()
	syncDaemonAutostart = func(string, bool) {}
	reachedStartup := false
	daemonExecutable = func() (string, error) {
		reachedStartup = true
		dir, _ := backendServiceDir(fixture.URL)
		lock, acquired, err := tryLockDaemonProfile(filepath.Join(dir, "operation.lock"))
		if lock != nil {
			lock.Close()
		}
		if err != nil || acquired {
			t.Fatal("handoff released transition lock before startup")
		}
		config, err := cli.LoadCLIConfigForProfile("desktop-test")
		profileDir, _ := cli.ProfileDir("desktop-test")
		owner, ownerErr := os.ReadFile(filepath.Join(profileDir, ".desktop-user-id"))
		if err != nil || ownerErr != nil || config.Token != "mul_new" || string(owner) != "account-b" {
			t.Fatal("replacement startup did not see committed credentials")
		}
		if cmd.Context().Value(daemonTransitionOwnerKey{}) != "account-b" {
			t.Fatal("replacement account was not pinned")
		}
		return "", errors.New("fixture startup failure")
	}
	if err := runBackendTakeover(cmd, nil); err == nil || !strings.Contains(err.Error(), "fixture startup failure") {
		t.Fatalf("unexpected result: %v", err)
	}
	if !reachedStartup {
		t.Fatal("did not reach replacement startup")
	}
	operation, err := acquireBackendOperation(cmd)
	if err != nil {
		t.Fatal("failed startup did not release transition lock:", err)
	}
	operation.Close()
}
