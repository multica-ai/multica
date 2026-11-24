//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)

// daemonSysProcAttr returns the attributes used when spawning the background
// daemon. The withBreakaway argument exists only to share a signature with
// the Windows version (where it controls CREATE_BREAKAWAY_FROM_JOB); on
// Unix Setsid alone is sufficient to detach the child from its parent's
// session and process group.
func daemonSysProcAttr(_ bool) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// isAccessDeniedSpawnErr is always false on Unix. The Windows version
// looks for ERROR_ACCESS_DENIED to detect "parent Job Object disallowed
// breakaway" and trigger the breakaway-disabled retry; that retry is a
// no-op on Unix.
func isAccessDeniedSpawnErr(_ error) bool { return false }

func notifyShutdownContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
}

// repointStdioToErrLog is a no-op on Unix. The rotating daemon.log writer
// renames the active file on rotation, and on POSIX an open file descriptor —
// including one an older self-update launcher inherited onto this process's
// stdout/stderr — never blocks a rename or unlink, so the rotator works without
// re-pointing our standard handles. Only Windows needs to release the inherited
// handle (see the windows build for the rationale).
func repointStdioToErrLog(_ string) {}

func tailLogFile(logPath string, lines int, follow bool) error {
	args := []string{"-n", strconv.Itoa(lines)}
	if follow {
		// -F (follow by name), not -f (follow by descriptor): after the rotator
		// renames daemon.log out from under us and opens a fresh one, -F reopens
		// the new file by path, whereas -f would stay glued to the rotated-away
		// inode and silently stop showing new output. Both GNU and BSD/macOS
		// tail support -F.
		args = append(args, "-F")
	}
	args = append(args, logPath)

	tail := exec.Command("tail", args...)
	tail.Stdout = os.Stdout
	tail.Stderr = os.Stderr
	return tail.Run()
}

// processOwnedByOtherUser reports whether pid is a live process that belongs
// to a different OS user. Every user's default profile hashes to the same
// health port, so on a shared machine another user's daemon can answer ours;
// its process owner, not the profile name, tells them apart.
func processOwnedByOtherUser(pid int) bool {
	if pid <= 0 {
		return false
	}
	info, err := os.Stat("/proc/" + strconv.Itoa(pid))
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) != os.Getuid()
}
