package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestBindDaemonHealthPortFallsBackWithoutReleasingListener(t *testing.T) {
	preferred := healthPortForProfile("desktop-api.multica.ai")
	var attempts []string
	ln, port, err := bindDaemonHealthPort(preferred, func(network, addr string) (net.Listener, error) {
		attempts = append(attempts, addr)
		if addr == fmt.Sprintf("127.0.0.1:%d", preferred) {
			return nil, syscall.EACCES
		}
		return net.Listen(network, addr)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if len(attempts) != 2 || attempts[1] != "127.0.0.1:0" || port == preferred || port != ln.Addr().(*net.TCPAddr).Port {
		t.Fatalf("attempts=%v port=%d listener=%v", attempts, port, ln.Addr())
	}
	if second, err := net.Listen("tcp", ln.Addr().String()); err == nil {
		second.Close()
		t.Fatal("fallback listener was released before the caller could use it")
	}
}

func TestResolveHealthPortValidatesRecordAndFallsBack(t *testing.T) {
	mkProfiles(t, "desktop-api.multica.ai")
	profile := "desktop-api.multica.ai"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"running","profile":"desktop-api.multica.ai","pid":12345}`)
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	if port == healthPortForProfile(profile) {
		t.Skip("test server selected the legacy profile port")
	}
	if err := publishDaemonPort(profile, daemonPortRecord{Port: port, PID: 12345}); err != nil {
		t.Fatal(err)
	}
	if got := resolveHealthPort(profile); got != port {
		t.Fatalf("resolved port=%d, want live record %d", got, port)
	}
	if err := publishDaemonPort(profile, daemonPortRecord{Port: port, PID: 999}); err != nil {
		t.Fatal(err)
	}
	if got := resolveHealthPort(profile); got != healthPortForProfile(profile) {
		t.Fatalf("resolved stale PID port=%d, want legacy %d", got, healthPortForProfile(profile))
	}
	removeDaemonPortIfOwned(profile, 12345)
	if _, err := os.Stat(daemonPortPathForProfile(profile)); err != nil {
		t.Fatalf("different PID removed the record: %v", err)
	}
	removeDaemonPortIfOwned(profile, 999)
	if _, err := os.Stat(daemonPortPathForProfile(profile)); !os.IsNotExist(err) {
		t.Fatalf("owner cleanup left record: %v", err)
	}
}

func TestResolveHealthPortRejectsAnotherProfile(t *testing.T) {
	mkProfiles(t, "desktop-api.multica.ai")
	profile := "desktop-api.multica.ai"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"running","profile":"other","pid":12345}`)
	}))
	defer server.Close()
	if err := publishDaemonPort(profile, daemonPortRecord{
		Port: server.Listener.Addr().(*net.TCPAddr).Port,
		PID:  12345,
	}); err != nil {
		t.Fatal(err)
	}
	if got := resolveHealthPort(profile); got != healthPortForProfile(profile) {
		t.Fatalf("resolved foreign daemon port=%d, want legacy %d", got, healthPortForProfile(profile))
	}
}

func TestDaemonProfileLockExcludesSecondProcessHandle(t *testing.T) {
	mkProfiles(t, "desktop-api.multica.ai")
	lock, err := acquireDaemonProfileLock("desktop-api.multica.ai", false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	second, acquired, err := tryLockDaemonProfile(lock.Name())
	if err != nil {
		t.Fatal(err)
	}
	if second != nil {
		second.Close()
	}
	if acquired {
		t.Fatal("second handle acquired the active profile lock")
	}
}

func TestWaitForDaemonProfileReleaseAllowsSuccessorAfterLongShutdown(t *testing.T) {
	const profile = "shutdown-lock-test"
	mkProfiles(t, profile)
	lockPath := filepath.Join(daemonDirForProfile(profile), "daemon.lock")
	releasePath := filepath.Join(t.TempDir(), "release")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	old := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDaemonProfileLockHolderProcess$")
	old.Env = append(os.Environ(), "MULTICA_TEST_DAEMON_LOCK_PATH="+lockPath, "MULTICA_TEST_DAEMON_LOCK_RELEASE="+releasePath)
	stdout, err := old.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "locked\n" {
		_ = old.Wait()
		t.Fatalf("lock holder did not start: line=%q error=%v", ready, err)
	}
	releaseErr := make(chan error, 1)
	go func() {
		time.Sleep(6 * time.Second)
		releaseErr <- os.WriteFile(releasePath, nil, 0o600)
	}()

	start := time.Now()
	waitErr := waitForDaemonProfileRelease(profile, 15*time.Second)
	elapsed := time.Since(start)
	if err := <-releaseErr; err != nil {
		t.Fatal(err)
	}
	if err := old.Wait(); err != nil {
		t.Fatalf("lock holder failed: %v", err)
	}
	if waitErr != nil {
		t.Fatal(waitErr)
	}
	if elapsed < 5*time.Second {
		t.Fatalf("lock wait ended after %s while the previous daemon was still exiting", elapsed)
	}
	successor, err := acquireDaemonProfileLock(profile, false)
	if err != nil {
		t.Fatalf("successor could not acquire the profile lock: %v", err)
	}
	successor.Close()
}

// A separate process owns the old daemon's lock after its health listener is
// gone. The parent test releases it only after a longer-than-child-wait delay.
func TestDaemonProfileLockHolderProcess(t *testing.T) {
	path := os.Getenv("MULTICA_TEST_DAEMON_LOCK_PATH")
	if path == "" {
		return
	}
	lock, acquired, err := tryLockDaemonProfile(path)
	if err != nil || !acquired {
		t.Fatalf("acquire holder lock: acquired=%t error=%v", acquired, err)
	}
	defer lock.Close()
	fmt.Fprintln(os.Stdout, "locked")
	releasePath := os.Getenv("MULTICA_TEST_DAEMON_LOCK_RELEASE")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(releasePath); err == nil {
			if path := os.Getenv("MULTICA_TEST_DAEMON_STOP_RESULT"); path != "" {
				outcome, _ := os.ReadFile(releasePath + ".outcome")
				result := daemonShutdownResult{PID: os.Getpid(), CleanupConfirmed: true}
				switch string(outcome) {
				case "missing":
					return
				case "malformed":
					if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
						t.Fatal(err)
					}
					return
				case "unconfirmed":
					result.CleanupConfirmed = false
				case "stale":
					result.PID++
				}
				data, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("lock holder was never released")
}

func TestWaitForDaemonProfileReleaseDetectsConcurrentStart(t *testing.T) {
	const profile = "concurrent-start-test"
	mkProfiles(t, profile)
	lock, err := acquireDaemonProfileLock(profile, false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"starting","profile":"concurrent-start-test","pid":12345}`)
	}))
	defer server.Close()
	if err := publishDaemonPort(profile, daemonPortRecord{Port: server.Listener.Addr().(*net.TCPAddr).Port, PID: 12345}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = waitForDaemonProfileRelease(profile, 3*time.Second)
	if err == nil || !strings.Contains(err.Error(), "already running or starting") {
		t.Fatalf("lock wait = %v, want concurrent daemon error", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("concurrent daemon took %s to detect", elapsed)
	}
}

func TestDaemonBackgroundWaitsForShutdownBeforeSpawning(t *testing.T) {
	const profile = "background-lock-test"
	mkProfiles(t, profile)
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{Token: "mul_fake"}, profile); err != nil {
		t.Fatal(err)
	}
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Skipf("false binary unavailable: %v", err)
	}
	old, err := acquireDaemonProfileLock(profile, false)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	released := make(chan struct{})
	go func() {
		time.Sleep(700 * time.Millisecond)
		old.Close()
		close(released)
	}()

	originalExecutable := daemonExecutable
	daemonExecutable = func() (string, error) { return falseBin, nil }
	t.Cleanup(func() { daemonExecutable = originalExecutable })
	originalSync := syncDaemonAutostart
	syncDaemonAutostart = func(string, bool) {}
	t.Cleanup(func() { syncDaemonAutostart = originalSync })

	cmd := &cobra.Command{Use: "start"}
	cmd.Flags().String("profile", profile, "")
	cmd.Flags().String("server-url", "", "")
	start := time.Now()
	err = runDaemonBackground(cmd)
	elapsed := time.Since(start)
	<-released
	// The fake child exits immediately. Reaching its startup failure proves
	// the launcher waited for the old lock before starting the replacement.
	if err == nil || !strings.Contains(err.Error(), "daemon exited during startup") {
		t.Fatalf("background start = %v, want fake child startup failure after lock release", err)
	}
	if elapsed < 600*time.Millisecond {
		t.Fatalf("background child started after %s, before the previous daemon released its lock", elapsed)
	}
}
