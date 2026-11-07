//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Model a daemon that releases its profile lock without cleaning up an
// independent run process.
func TestDaemonStopUnconfirmedExitHelper(t *testing.T) {
	if os.Getenv("MULTICA_TEST_UNCONFIRMED_EXIT") != "1" {
		t.Skip("subprocess only")
	}
	lock, acquired, err := tryLockDaemonProfile(os.Getenv("MULTICA_TEST_UNCONFIRMED_LOCK"))
	if err != nil || !acquired {
		t.Fatalf("lock: %v, acquired=%v", err, acquired)
	}
	defer lock.Close()
	task := exec.Command("/bin/sleep", "30")
	task.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := task.Start(); err != nil {
		t.Fatal(err)
	}
	fmt.Println(task.Process.Pid)
	defer task.Process.Release()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(os.Getenv("MULTICA_TEST_UNCONFIRMED_RELEASE")); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = task.Process.Kill()
	t.Fatal("shutdown request never arrived")
}

func TestDaemonStopForceRejectsGracefulExitWithSurvivingRun(t *testing.T) {
	clearDaemonTaskEnv(t)
	const profile = "stop-unconfirmed-cleanup"
	mkProfiles(t, profile)
	release := filepath.Join(t.TempDir(), "release")
	child := exec.Command(os.Args[0], "-test.run=^TestDaemonStopUnconfirmedExitHelper$")
	child.Env = append(os.Environ(),
		"MULTICA_TEST_UNCONFIRMED_EXIT=1",
		"MULTICA_TEST_UNCONFIRMED_LOCK="+filepath.Join(daemonDirForProfile(profile), "daemon.lock"),
		"MULTICA_TEST_UNCONFIRMED_RELEASE="+release)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	taskPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || taskPID <= 1 {
		t.Fatalf("invalid test task PID: %q (%v)", line, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(taskPID, syscall.SIGKILL) })
	health := map[string]any{"status": "running", "profile": profile, "pid": float64(child.Process.Pid)}
	_, port := stopTestServer(t, health, func() { _ = os.WriteFile(release, nil, 0o600) })
	stopErr := stopDaemonAndWait(profile, port, health, time.Second, time.Second)
	if err := child.Wait(); err != nil {
		t.Fatalf("test daemon did not exit normally: %v", err)
	}
	if err := syscall.Kill(taskPID, 0); err != nil {
		t.Fatalf("test run did not survive: %v", err)
	}
	if stopErr == nil {
		t.Fatal("stop returned success after daemon exited normally, but its independent run process is still alive")
	}
	// Cancelling the failure dialog and quitting again must not erase the
	// uncertainty while the same independent run process is still alive.
	if retryErr := stopDaemonAndWait(profile, 0, nil, 0, 0); retryErr == nil {
		t.Fatal("repeated stop returned success without cleanup confirmation while the independent run process is still alive")
	}
}
