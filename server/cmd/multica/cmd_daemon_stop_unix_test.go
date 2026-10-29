//go:build !windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDaemonStopWithTaskProcess(t *testing.T) {
	switch os.Getenv("MULTICA_TEST_STOP_PROCESS") {
	case "task":
		fmt.Println("ready")
		time.Sleep(20 * time.Second)
		return
	case "daemon":
		lock, acquired, err := tryLockDaemonProfile(os.Getenv("MULTICA_TEST_STOP_LOCK"))
		if err != nil || !acquired {
			t.Fatalf("acquire profile lock: %v", err)
		}
		defer lock.Close()
		task := exec.Command(os.Args[0], "-test.run=^TestDaemonStopWithTaskProcess$")
		task.Env = append(os.Environ(), "MULTICA_TEST_STOP_PROCESS=task")
		task.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		stdout, err := task.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := task.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = task.Process.Kill(); _ = task.Wait() }()
		if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
			t.Fatalf("task readiness: %q, %v", line, err)
		}
		fmt.Println(task.Process.Pid)
		time.Sleep(20 * time.Second)
		return
	}
	t.Skip("subprocess helper")
}

func TestDaemonStopForceDoesNotReportOrphanedTaskAsStopped(t *testing.T) {
	clearDaemonTaskEnv(t)
	const profile = "stop-orphan-test"
	mkProfiles(t, profile)
	daemon := exec.Command(os.Args[0], "-test.run=^TestDaemonStopWithTaskProcess$")
	daemon.Env = append(os.Environ(), "MULTICA_TEST_STOP_PROCESS=daemon",
		"MULTICA_TEST_STOP_LOCK="+filepath.Join(daemonDirForProfile(profile), "daemon.lock"))
	stdout, err := daemon.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Process.Kill(); _ = daemon.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	taskPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || taskPID <= 1 {
		t.Fatalf("task PID: %q, %v", line, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-taskPID, syscall.SIGKILL) })
	health := map[string]any{"status": "running", "profile": profile, "pid": float64(daemon.Process.Pid)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shutdown" {
			http.Error(w, "shutdown unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(health)
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	err = stopDaemonAndWait(profile, port, health, 0, time.Second)
	if err == nil || !strings.Contains(err.Error(), "task cleanup could not be confirmed") {
		t.Fatalf("forced termination must report uncertain task cleanup: %v", err)
	}
	if err := syscall.Kill(taskPID, 0); err != nil {
		t.Fatalf("expected independent task to survive daemon termination: %v", err)
	}
	if stopped, err := daemonStopComplete(profile); err != nil || !stopped {
		t.Fatalf("daemon lock should be released: stopped=%v, err=%v", stopped, err)
	}
}
