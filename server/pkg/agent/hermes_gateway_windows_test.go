//go:build windows

package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestHermesGatewayProcessHelper(t *testing.T) {
	mode := os.Getenv("MULTICA_TEST_GATEWAY_HELPER")
	if mode == "" {
		return
	}
	if mode == "child" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if os.Getenv("PYTHONIOENCODING") != "utf-8" || os.Getenv("HERMES_GATEWAY_DETACHED") != "1" {
		t.Fatal("gateway launch environment does not match the Windows contract")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestHermesGatewayProcessHelper$")
	child.Env = append(os.Environ(), "MULTICA_TEST_GATEWAY_HELPER=child")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	home := os.Getenv("HERMES_HOME")
	if err := os.WriteFile(filepath.Join(home, "child.pid"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(home, "gateway.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	position := windows.Overlapped{Offset: 1024 * 1024}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &position); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Minute)
}

func TestHermesGatewayWindowsCleanupOwnsDescendants(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelled), func(t *testing.T) {
			home := t.TempDir()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stop, err := StartHermesTaskGateway(ctx, Config{
				ExecutablePath: exe, LaunchPrefix: []string{"-test.run=^TestHermesGatewayProcessHelper$", "--"},
				CLIVersion: "0.22.6", Env: map[string]string{"HERMES_HOME": home, "MULTICA_TEST_GATEWAY_HELPER": "owner"},
			}, home)
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			pid := waitForDescendantPid(t, filepath.Join(home, "child.pid"))
			if !processStillRunning(pid) {
				t.Fatal("gateway descendant did not start")
			}
			if cancelled {
				cancel()
			}
			stop()
			deadline := time.Now().Add(5 * time.Second)
			for processStillRunning(pid) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if processStillRunning(pid) {
				t.Fatal("gateway descendant survived cleanup")
			}
			if owned, err := hermesGatewayReserved(home); err != nil || owned {
				t.Fatalf("gateway reservation survived cleanup: %v, %v", owned, err)
			}
		})
	}
}

func TestHermesGatewayReservation(t *testing.T) {
	home := t.TempDir()
	check := func(want bool) {
		t.Helper()
		got, err := hermesGatewayReserved(home)
		if err != nil || got != want {
			t.Fatalf("reserved = %v, %v; want %v", got, err, want)
		}
	}
	check(false)
	f, err := os.OpenFile(filepath.Join(home, "gateway.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// A stale PID record is not a live reservation.
	if _, err := f.WriteString(`{"pid":1234}`); err != nil {
		t.Fatal(err)
	}
	check(false)
	position := windows.Overlapped{Offset: 1024 * 1024}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &position); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &position); err != nil {
		t.Fatal(err)
	}
	check(false)
}
