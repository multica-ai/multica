//go:build linux

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Linux 6.9's UAPI flag is not yet exported by x/sys/unix. It targets the
// pidfd's retained struct pid as a group, including children after leader exit.
const cursorPidfdSignalProcessGroup = 1 << 2

type cursorUnixGroupHandle struct{ fd int }

func captureCursorUnixGroup(pgid int) (*cursorUnixGroupHandle, error) {
	fd, err := unix.PidfdOpen(pgid, 0)
	if err != nil {
		return nil, err
	}
	g := &cursorUnixGroupHandle{fd: fd}
	// Probe support without sending a signal. Older kernels fail closed;
	// silently falling back to kill(-pgid) would lose durable ownership.
	if err := g.signal(0); err != nil {
		g.close()
		return nil, err
	}
	return g, nil
}

func (g *cursorUnixGroupHandle) signal(sig syscall.Signal) error {
	return unix.PidfdSendSignal(g.fd, unix.Signal(sig), nil, cursorPidfdSignalProcessGroup)
}
func (g *cursorUnixGroupHandle) close() { _ = unix.Close(g.fd) }

func readCursorUnixProcessInfo(pid int) (cursorUnixProcessInfo, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return cursorUnixProcessInfo{}, err
	}
	raw := string(data)
	openParen := strings.IndexByte(raw, '(')
	closeParen := strings.LastIndex(raw, ") ")
	if openParen <= 0 || closeParen < openParen {
		return cursorUnixProcessInfo{}, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	parsedPID, err := strconv.Atoi(strings.TrimSpace(raw[:openParen]))
	if err != nil || parsedPID != pid {
		return cursorUnixProcessInfo{}, fmt.Errorf("unexpected pid in /proc/%d/stat", pid)
	}
	fields := strings.Fields(raw[closeParen+2:])
	if len(fields) <= 19 {
		return cursorUnixProcessInfo{}, fmt.Errorf("short /proc/%d/stat", pid)
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return cursorUnixProcessInfo{}, err
	}
	pgid, err := strconv.Atoi(fields[2])
	if err != nil {
		return cursorUnixProcessInfo{}, err
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return cursorUnixProcessInfo{}, err
	}
	return cursorUnixProcessInfo{pid: pid, ppid: ppid, pgid: pgid, start: start, zombie: fields[0] == "Z"}, nil
}

func listCursorUnixProcessInfos() ([]cursorUnixProcessInfo, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	infos := make([]cursorUnixProcessInfo, 0, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		info, err := readCursorUnixProcessInfo(pid)
		if err == nil && !info.zombie {
			infos = append(infos, info)
		}
	}
	return infos, nil
}

func cursorUnixIsDescendant(pid int, root cursorUnixProcessInfo) bool {
	seen := make(map[int]struct{}, 8)
	for pid > 1 {
		if pid == root.pid {
			info, err := readCursorUnixProcessInfo(pid)
			return err == nil && info.pid == root.pid && info.start == root.start
		}
		if _, ok := seen[pid]; ok {
			return false
		}
		seen[pid] = struct{}{}
		info, err := readCursorUnixProcessInfo(pid)
		if err != nil {
			return false
		}
		pid = info.ppid
	}
	return false
}
