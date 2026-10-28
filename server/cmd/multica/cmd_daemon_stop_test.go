package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Reuse the existing bounded lock-holder test process; never run an Agent CLI.
func startStopLockHolder(t *testing.T, profile string, daemonName bool) (*exec.Cmd, string) {
	t.Helper()
	executable := os.Args[0]
	if daemonName {
		name := "multica"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		executable = filepath.Join(t.TempDir(), name)
		in, err := os.Open(os.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		out, err := os.OpenFile(executable, os.O_CREATE|os.O_WRONLY, 0o700)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(out, in)
		out.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	release := filepath.Join(t.TempDir(), "release")
	child := exec.Command(executable, "-test.run=^TestDaemonProfileLockHolderProcess$", "daemon", "start", "--foreground", "--profile", profile)
	child.Env = append(os.Environ(),
		"MULTICA_TEST_DAEMON_LOCK_PATH="+filepath.Join(daemonDirForProfile(profile), "daemon.lock"),
		"MULTICA_TEST_DAEMON_LOCK_RELEASE="+release,
		"MULTICA_TEST_DAEMON_STOP_RESULT="+daemonShutdownResultPath(profile))
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("start lock holder: %q, %v", line, err)
	}
	if err := os.WriteFile(daemonPIDPathForProfile(profile), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	return child, release
}

func stopTestServer(t *testing.T, health map[string]any, shutdown func()) (*httptest.Server, int) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shutdown" {
			shutdown()
			fmt.Fprint(w, "{}")
			return
		}
		_ = json.NewEncoder(w).Encode(health)
	}))
	t.Cleanup(server.Close)
	return server, server.Listener.Addr().(*net.TCPAddr).Port
}

func TestDaemonStopForceWaitsForCleanupAfterHealthCloses(t *testing.T) {
	clearDaemonTaskEnv(t)
	const profile = "stop-cleanup-test"
	mkProfiles(t, profile)
	child, release := startStopLockHolder(t, profile, false)
	health := map[string]any{"status": "running", "profile": profile, "pid": float64(child.Process.Pid)}
	var shutdowns atomic.Int32
	_, port := stopTestServer(t, nil, func() {
		shutdowns.Add(1)
		go func() { time.Sleep(200 * time.Millisecond); _ = os.WriteFile(release, nil, 0o600) }()
	})
	start := time.Now()
	if err := stopDaemonAndWait(profile, port, health, time.Second, time.Second); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 150*time.Millisecond || shutdowns.Load() != 1 {
		t.Fatal("stop returned before cleanup released the profile lock")
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("graceful daemon was killed: %v", err)
	}
}

func TestDaemonStopForceWaitsForExistingCleanupWhenShutdownFails(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(fmt.Sprintf("endpoint_closed_%t", closed), func(t *testing.T) {
			clearDaemonTaskEnv(t)
			const profile = "stop-existing-cleanup-test"
			mkProfiles(t, profile)
			child, release := startStopLockHolder(t, profile, true)
			health := map[string]any{"status": "running", "profile": profile, "pid": float64(child.Process.Pid)}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/shutdown" {
					http.Error(w, "already shutting down", http.StatusServiceUnavailable)
					return
				}
				_ = json.NewEncoder(w).Encode(health)
			}))
			t.Cleanup(server.Close)
			port := server.Listener.Addr().(*net.TCPAddr).Port
			if closed {
				server.Close()
				health = nil
			}
			go func() { time.Sleep(200 * time.Millisecond); _ = os.WriteFile(release, nil, 0o600) }()
			if err := stopDaemonAndWait(profile, port, health, time.Second, time.Second); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err != nil {
				t.Fatalf("daemon was killed during its cleanup grace period: %v", err)
			}
		})
	}
}

func TestDaemonStopForceKillsOnlyAfterGraceExpires(t *testing.T) {
	clearDaemonTaskEnv(t)
	const profile = "stop-force-test"
	mkProfiles(t, profile)
	child, _ := startStopLockHolder(t, profile, false)
	health := map[string]any{"status": "running", "profile": profile, "pid": float64(child.Process.Pid)}
	_, port := stopTestServer(t, health, func() {})
	start := time.Now()
	if err := stopDaemonAndWait(profile, port, health, 50*time.Millisecond, time.Second); err == nil || !strings.Contains(err.Error(), "task cleanup could not be confirmed") {
		t.Fatalf("forced termination must report uncertain task cleanup: %v", err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("daemon was killed before the grace period expired")
	}
	if err := child.Wait(); err == nil {
		t.Fatal("unresponsive daemon was not force stopped")
	}
}

func TestDaemonStopForceHandlesUnresponsiveHealthWithVerifiedProfilePID(t *testing.T) {
	for _, portRecord := range []bool{false, true} {
		t.Run(fmt.Sprintf("port_record_%t", portRecord), func(t *testing.T) {
			clearDaemonTaskEnv(t)
			const profile = "stop-unresponsive-test"
			mkProfiles(t, profile)
			child, _ := startStopLockHolder(t, profile, true)
			_, port := stopTestServer(t, nil, func() {})
			if portRecord {
				if err := publishDaemonPort(profile, daemonPortRecord{Port: port, PID: child.Process.Pid}); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(daemonPIDPathForProfile(profile)); err != nil {
					t.Fatal(err)
				}
			}
			if err := stopDaemonAndWait(profile, port, nil, 0, time.Second); err == nil || !strings.Contains(err.Error(), "task cleanup could not be confirmed") {
				t.Fatalf("forced termination must report uncertain task cleanup: %v", err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("unresponsive verified daemon was not force stopped")
			}
		})
	}
}

func TestDaemonStopForceFlagUsesTheCLIStopPath(t *testing.T) {
	clearDaemonTaskEnv(t)
	const profile = "stop-command-test"
	mkProfiles(t, profile)
	child, release := startStopLockHolder(t, profile, false)
	health := map[string]any{"status": "running", "profile": profile, "pid": float64(child.Process.Pid)}
	_, port := stopTestServer(t, health, func() { _ = os.WriteFile(release, nil, 0o600) })
	if err := publishDaemonPort(profile, daemonPortRecord{Port: port, PID: child.Process.Pid}); err != nil {
		t.Fatal(err)
	}
	cmd := daemonStatusCmdFor(t, profile, "")
	cmd.Flags().Bool("force", true, "")
	if err := runDaemonStop(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonStopForceRequiresMatchingCleanupResult(t *testing.T) {
	for _, outcome := range []string{"confirmed", "unconfirmed", "missing", "malformed", "stale"} {
		t.Run(outcome, func(t *testing.T) {
			clearDaemonTaskEnv(t)
			const profile = "stop-result-test"
			mkProfiles(t, profile)
			child, release := startStopLockHolder(t, profile, false)
			health := map[string]any{"status": "running", "profile": profile, "pid": float64(child.Process.Pid)}
			_, port := stopTestServer(t, health, func() { _ = os.WriteFile(release, nil, 0o600) })
			// The child writes these variants before releasing its lock.
			if err := os.WriteFile(release+".outcome", []byte(outcome), 0o600); err != nil {
				t.Fatal(err)
			}
			err := stopDaemonAndWait(profile, port, health, time.Second, time.Second)
			if (err == nil) != (outcome == "confirmed") {
				t.Fatalf("cleanup outcome %s: %v", outcome, err)
			}
			if err := child.Wait(); err != nil {
				t.Fatalf("graceful daemon was killed: %v", err)
			}
			// Cancelling the failure dialog and quitting again must keep the
			// failure, even when the daemon left no usable result behind.
			if err := stopDaemonAndWait(profile, 0, nil, 0, 0); (err == nil) != (outcome == "confirmed") {
				t.Fatalf("repeated stop after cleanup outcome %s: %v", outcome, err)
			}
		})
	}
}

func TestDaemonStopForceKeepsFailedCleanupOnRepeatedStop(t *testing.T) {
	clearDaemonTaskEnv(t)
	const profile = "stop-repeated-test"
	mkProfiles(t, profile)
	if err := stopDaemonAndWait(profile, 0, nil, 0, 0); err != nil {
		t.Fatalf("idle stop: %v", err)
	}
	for _, confirmed := range []bool{false, true} {
		if err := writeDaemonShutdownResult(profile, daemonShutdownResult{PID: 12345, CleanupConfirmed: confirmed}); err != nil {
			t.Fatal(err)
		}
		if err := stopDaemonAndWait(profile, 0, nil, 0, 0); (err == nil) != confirmed {
			t.Fatalf("previous cleanup confirmed=%t: %v", confirmed, err)
		}
	}
}

func TestDaemonStopForceRefusesUnverifiedPIDAndForeignProfile(t *testing.T) {
	clearDaemonTaskEnv(t)
	const profile = "stop-identity-test"
	mkProfiles(t, profile)
	child, _ := startStopLockHolder(t, profile, false)
	var shutdowns atomic.Int32
	_, port := stopTestServer(t, nil, func() { shutdowns.Add(1) })
	foreign := map[string]any{"status": "running", "profile": "other", "pid": float64(child.Process.Pid)}
	if err := stopDaemonAndWait(profile, port, foreign, 0, time.Second); err == nil || shutdowns.Load() != 0 {
		t.Fatalf("foreign profile was acted on: %v", err)
	}
	foreignOS := "windows"
	if runtime.GOOS == "windows" {
		foreignOS = "linux"
	}
	if err := stopDaemonAndWait(profile, port, map[string]any{"status": "running", "profile": profile, "os": foreignOS}, 0, time.Second); err == nil || shutdowns.Load() != 0 {
		t.Fatalf("foreign OS was acted on: %v", err)
	}
	if err := stopDaemonAndWait(profile, port, nil, 0, time.Second); err == nil || !strings.Contains(err.Error(), "does not identify") {
		t.Fatalf("unrelated test process was accepted as a daemon: %v", err)
	}
	if stopped, _ := daemonStopComplete(profile); stopped {
		t.Fatal("unverified process was terminated")
	}
}

func TestDaemonStopForceReportsFailureIfProfileRemainsLocked(t *testing.T) {
	clearDaemonTaskEnv(t)
	const profile = "stop-still-locked-test"
	mkProfiles(t, profile)
	lock, err := acquireDaemonProfileLock(profile, false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	pid := startBlockingChild(t)
	health := map[string]any{"status": "running", "profile": profile, "pid": float64(pid)}
	_, port := stopTestServer(t, health, func() {})
	if err := stopDaemonAndWait(profile, port, health, 0, 50*time.Millisecond); err == nil || !strings.Contains(err.Error(), "still locked") {
		t.Fatalf("incomplete stop was reported as success: %v", err)
	}
}
