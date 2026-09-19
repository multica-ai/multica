//go:build !windows

package agent

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// pidTags remembers, for each *exec.Cmd we started, the /proc/<pid>/stat
// "starttime" field observed the instant Start() returned. That pair (pid +
// starttime) is what Linux itself uses to disambiguate a process from
// anything that later reuses the same pid number — see stillOurProcess.
//
// Keyed by the *exec.Cmd pointer (not the pid) because the pid is exactly
// the identifier that can go stale; the Cmd pointer cannot be confused with
// a later, unrelated process the same way a bare integer can.
var pidTags sync.Map // map[*exec.Cmd]uint64

// recordStartTime captures cmd's pid's current /proc/<pid>/stat starttime.
// Call this immediately after a successful Start(), before anything else
// runs — any delay widens the exact window this exists to close. Failure to
// read /proc (non-Linux, restricted /proc, race too tight to matter) leaves
// no tag, which stillOurProcess treats as "assume yes" so behavior degrades
// to the pre-patch one rather than a false no.
func recordStartTime(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if st, err := readProcStartTime(cmd.Process.Pid); err == nil {
		pidTags.Store(cmd, st)
	}
}

// forgetStartTime drops the tag once a command's lifecycle is fully done, so
// a long-lived daemon does not accumulate one entry per short-lived probe
// forever.
func forgetStartTime(cmd *exec.Cmd) {
	pidTags.Delete(cmd)
}

// stillOurProcess reports whether it is safe to signal cmd.Process.Pid,
// using the tag recordStartTime laid down right after Start(). Four
// outcomes:
//
//   - No tag was ever recorded (recordStartTime never ran, or /proc could not
//     be read that once): behave exactly as before this patch — assume yes,
//     since that is what every caller did previously.
//   - pid does not currently exist at all: nothing this package started is
//     there to hit, so signaling it is a harmless no-op (ESRCH) exactly as
//     before this patch — answer yes. This is also the common, wanted case:
//     the direct child has already exited but may have left a grandchild
//     forked into the same process group, still holding a pipe or a
//     repository lock, that this signal is the only way left to reach.
//     Refusing here would silently stop reaping that grandchild.
//   - pid exists but its starttime no longer matches the tag: this is a
//     *different* process that came to hold the same number after the one
//     this package started already exited. Answer no — signaling now would
//     hit that unrelated process (or, worst case, this daemon's own process
//     group) instead of anything this package owns.
//   - pid exists and its starttime still matches: this is still the same
//     process this package started. Answer yes.
func stillOurProcess(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	tagged, ok := pidTags.Load(cmd)
	if !ok {
		return true
	}
	current, err := readProcStartTime(cmd.Process.Pid)
	if err != nil {
		// pid no longer exists — signaling it is a harmless no-op, and
		// required to reach any grandchild still alive in its group.
		return true
	}
	return current == tagged.(uint64)
}

// readProcStartTime parses field 22 (starttime, in clock ticks since boot)
// from /proc/<pid>/stat. The process name in field 2 is parenthesized and
// may itself contain spaces or parentheses, so field boundaries are found
// from the *last* ')' rather than by a fixed-width split.
func readProcStartTime(pid int) (uint64, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	idx := bytes.LastIndexByte(data, ')')
	if idx < 0 || idx+2 > len(data) {
		return 0, os.ErrInvalid
	}
	// Fields from here on: state(3) ppid(4) pgrp(5) session(6) tty_nr(7)
	// tpgid(8) flags(9) minflt(10) cminflt(11) majflt(12) cmajflt(13)
	// utime(14) stime(15) cutime(16) cstime(17) priority(18) nice(19)
	// num_threads(20) itrealvalue(21) starttime(22) ...
	// so starttime is at 0-based index 22-3 = 19 in this slice.
	const starttimeIndex = 19
	fields := strings.Fields(string(data[idx+1:]))
	if starttimeIndex >= len(fields) {
		return 0, os.ErrInvalid
	}
	return strconv.ParseUint(fields[starttimeIndex], 10, 64)
}
