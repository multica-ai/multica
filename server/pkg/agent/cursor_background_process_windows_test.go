//go:build windows

package agent

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// spawnCursorTestTree starts the fake cursor-agent in its default mode, which
// launches one background shell that launches one leaf and publishes
// [shell, leaf] to <dir>/0.json. All three live in the launch's Job.
func spawnCursorTestTree(t *testing.T) (root *exec.Cmd, shell, leaf int) {
	t.Helper()
	dir := t.TempDir()
	root = exec.Command(os.Args[0])
	root.Env = append(os.Environ(), cursorFakeModeEnv+"=natural", "CURSOR_FAKE_DIR="+dir, "CURSOR_FAKE_DURATION=30s")
	hideAgentWindow(root)
	if err := startOwnedProcessTree(root, slog.Default()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { signalProcessGroup(root, syscall.SIGKILL); _ = root.Wait(); releaseProcessGroup(root) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var pids []int
		data, _ := os.ReadFile(filepath.Join(dir, "0.json"))
		if json.Unmarshal(data, &pids) == nil && len(pids) == 2 {
			return root, pids[0], pids[1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("fixture did not publish shell and leaf PIDs")
	return nil, 0, 0
}

func assertCursorTestProcessAlive(t *testing.T, pid int) {
	t.Helper()
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatalf("process %d is gone: %v", pid, err)
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil || status != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("process %d exited: status=%d err=%v", pid, status, err)
	}
}

// A short PID list is not the membership. With the first buffer sized below
// the tree, the query must grow until the two counts agree and return every
// member, whether the kernel signals the shortfall with ERROR_MORE_DATA or
// only through the counts.
func TestCaptureCursorBackgroundJobListGrowsPastInitialCapacity(t *testing.T) {
	root, shell, leaf := spawnCursorTestTree(t)
	tree, ok := lookupProcessTree(root)
	if !ok {
		t.Fatal("launch is not owned")
	}
	saved := cursorJobProcessIDListCapacity
	t.Cleanup(func() { cursorJobProcessIDListCapacity = saved })
	for _, capacity := range []int{1, 2, 3} {
		cursorJobProcessIDListCapacity = capacity
		members, err := cursorJobProcessIDs(tree.job)
		if err != nil {
			t.Fatalf("capacity %d: %v", capacity, err)
		}
		for _, pid := range []int{root.Process.Pid, shell, leaf} {
			if _, ok := members[uint32(pid)]; !ok {
				t.Fatalf("capacity %d: member %d missing from %v", capacity, pid, members)
			}
		}
	}
}

// A launch member that is alive but cannot be opened must fail the capture,
// not be skipped: skipped, it would sit outside the tool Job and outlive the
// tool's completion. The failed capture must also leave the healthy shell and
// its child running, exactly as before the attempt.
func TestCaptureCursorBackgroundProcessFailsWhenLiveMemberCannotBeOpened(t *testing.T) {
	root, shell, leaf := spawnCursorTestTree(t)
	saved := cursorOpenProcess
	t.Cleanup(func() { cursorOpenProcess = saved })
	cursorOpenProcess = func(access uint32, inherit bool, pid uint32) (windows.Handle, error) {
		if pid == uint32(leaf) {
			return 0, windows.ERROR_ACCESS_DENIED
		}
		return saved(access, inherit, pid)
	}
	if p, err := captureCursorBackgroundProcess(root, shell); err == nil {
		p.Close()
		t.Fatal("capture succeeded while a live launch member could not be opened")
	}
	assertCursorTestProcessAlive(t, shell)
	assertCursorTestProcessAlive(t, leaf)
}

// Sanity for the same tree with nothing in the way: the shell and its
// pre-existing child are both claimed and both die with the tool Job.
func TestCaptureCursorBackgroundProcessClaimsExistingChild(t *testing.T) {
	root, shell, leaf := spawnCursorTestTree(t)
	owned, err := captureCursorBackgroundProcess(root, shell)
	if err != nil {
		t.Fatal(err)
	}
	if err := owned.Terminate(); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	assertCursorTestProcessGone(t, shell)
	assertCursorTestProcessGone(t, leaf)
	if alive, err := owned.Alive(); alive || err != nil {
		t.Fatalf("tool job still alive: alive=%v err=%v", alive, err)
	}
	owned.Close()
}
