//go:build linux || darwin

package agent

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCaptureCursorBackgroundProcessReapsLeaderExitedGroup(t *testing.T) {
	dir := t.TempDir()
	root := exec.Command(os.Args[0])
	root.Env = append(os.Environ(), cursorFakeModeEnv+"=budget", "CURSOR_FAKE_DIR="+dir, "CURSOR_FAKE_DURATION=30s")
	configureProcessGroup(root)
	if err := startOwnedProcessTree(root, slog.Default()); err != nil {
		t.Fatal(err)
	}
	defer func() { signalProcessGroup(root, syscall.SIGKILL); _ = root.Wait() }()
	var pids []int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(dir, "0.json"))
		if json.Unmarshal(data, &pids) == nil && len(pids) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pids) != 2 {
		t.Fatal("fixture did not publish shell and child PIDs")
	}
	owned, err := captureCursorBackgroundProcess(root, pids[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owned.Terminate(); owned.Close() }()
	group := owned.platform.(*cursorUnixBackgroundProcess)
	group.leaderStart++
	if err := owned.Terminate(); err == nil {
		t.Fatal("signalled a process whose recorded identity changed")
	}
	group.leaderStart--
	if err := syscall.Kill(pids[0], syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	assertCursorTestProcessGone(t, pids[0])
	if err := owned.Terminate(); err != nil {
		t.Fatalf("cleanup with exited leader: %v", err)
	}
	assertCursorTestProcessGone(t, pids[1])
	if alive, err := owned.Alive(); alive || err != nil {
		t.Fatalf("Alive=%v err=%v", alive, err)
	}
}

func TestCaptureCursorBackgroundProcessRejectsUnrelatedPID(t *testing.T) {
	spawn := func() *exec.Cmd {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), cursorFakeModeEnv+"=leaf", "CURSOR_FAKE_DURATION=30s")
		cmd.Stdout = io.Discard
		configureProcessGroup(cmd)
		if err := startOwnedProcessTree(cmd, slog.Default()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { signalProcessGroup(cmd, syscall.SIGKILL); _ = cmd.Wait() })
		return cmd
	}
	root, other := spawn(), spawn()
	if _, err := captureCursorBackgroundProcess(root, other.Process.Pid); err == nil {
		t.Fatal("accepted unrelated process")
	}
}

func TestCaptureCursorBackgroundSurvivesRootExitDuringCapture(t *testing.T) {
	dir := t.TempDir()
	root := exec.Command(os.Args[0])
	root.Env = append(os.Environ(), cursorFakeModeEnv+"=budget", "CURSOR_FAKE_DIR="+dir, "CURSOR_FAKE_DURATION=30s")
	configureProcessGroup(root)
	if err := root.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Process.Kill(); _ = root.Wait() })
	var pids []int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(dir, "0.json"))
		if json.Unmarshal(data, &pids) == nil && len(pids) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(pids) != 2 {
		t.Fatal("fixture did not publish shell and child PIDs")
	}
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	rootInfo, err := readCursorUnixProcessInfo(root.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	rootInfo.start++
	if cursorUnixIsDescendant(pids[0], rootInfo) {
		t.Fatal("accepted ancestry through a reused root identity")
	}
	owned, err := captureCursorUnixBackgroundProcess(root, pids[0], func(pgid int) (*cursorUnixGroupHandle, error) {
		group, err := captureCursorUnixGroup(pgid)
		if err != nil {
			return nil, err
		}
		// Ancestry has been proven and the handle retained. Force the root to
		// exit here, before post-capture validation, as normal finalization can.
		_ = root.Process.Kill()
		_ = root.Wait()
		return group, nil
	})
	if err != nil {
		t.Fatalf("lost verified background work when Cursor root exited: %v", err)
	}
	defer owned.Close()
	if err := owned.Terminate(); err != nil {
		t.Fatal(err)
	}
	for _, pid := range pids {
		assertCursorTestProcessGone(t, pid)
	}
}
