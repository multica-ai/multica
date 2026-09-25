//go:build darwin

package agent

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

type cursorUnixGroupHandle struct {
	pgid  int
	known map[uint64]struct{}
}

func captureCursorUnixGroup(pgid int) (*cursorUnixGroupHandle, error) {
	// Probe with a positive PID beyond XNU's PID_MAX (99999). PID zero is
	// rejected as EINVAL on some kernels, indistinguishable from a missing API.
	// Unsupported kernels fail closed; numeric kill is not a safe fallback.
	if err := signalCursorDarwinIdentity(1<<31-1, 0, syscall.SIGKILL); err != syscall.ESRCH {
		return nil, errors.Join(errors.New("Cursor background identity signalling is unavailable on this macOS kernel"), err)
	}
	identity, err := readCursorDarwinIdentity(pgid)
	if err != nil {
		return nil, err
	}
	g := &cursorUnixGroupHandle{pgid: pgid, known: map[uint64]struct{}{identity.unique: {}}}
	if err := g.signal(0); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *cursorUnixGroupHandle) signal(sig syscall.Signal) error {
	all, err := listCursorUnixProcessInfos()
	if err != nil {
		return err
	}
	candidates := make(map[int]cursorDarwinIdentity)
	for _, info := range all {
		if info.pgid != g.pgid || info.zombie {
			continue
		}
		identity, err := readCursorDarwinIdentity(info.pid)
		if err != nil {
			if err == syscall.ESRCH {
				continue
			}
			return err
		}
		current, err := readCursorUnixProcessInfo(info.pid)
		if err != nil || current.zombie || current.start != info.start || current.pgid != g.pgid {
			continue
		}
		candidates[info.pid] = identity
	}
	if len(candidates) == 0 {
		remaining, err := listCursorUnixProcessGroup(g.pgid)
		if err != nil {
			return err
		}
		if len(remaining) != 0 {
			return errCursorBackgroundProcessIdentity
		}
		return syscall.ESRCH
	}
	// Retain observed birth identities after exit. Parent unique IDs survive
	// reparenting, allowing a late direct child to prove its origin even after
	// its leader exits. An entirely unseen intermediate generation is a limit:
	// never infer ownership just because an unknown process has this PGID.
	for changed := true; changed; {
		changed = false
		for _, identity := range candidates {
			if _, known := g.known[identity.unique]; known {
				continue
			}
			if _, parentKnown := g.known[identity.parentUnique]; parentKnown {
				g.known[identity.unique] = struct{}{}
				changed = true
			}
		}
	}
	owned := 0
	var signalErr error
	for pid, identity := range candidates {
		if _, known := g.known[identity.unique]; !known {
			continue
		}
		owned++
		if sig != 0 {
			// The kernel checks pidversion and holds the exact process reference
			// while signalling. PID reuse or exec after this snapshot is rejected.
			err := signalCursorDarwinIdentity(pid, identity.version, sig)
			if err != nil && err != syscall.ESRCH {
				signalErr = errors.Join(signalErr, err)
			}
		}
	}
	if owned == 0 {
		return errCursorBackgroundProcessIdentity
	}
	return signalErr
}
func (g *cursorUnixGroupHandle) close() { g.known = nil }

func cursorUnixIsDescendant(pid int, root cursorUnixProcessInfo) bool {
	rootIdentity, err := readCursorDarwinIdentity(root.pid)
	if err != nil {
		return false
	}
	currentRoot, err := readCursorUnixProcessInfo(root.pid)
	if err != nil || currentRoot.start != root.start {
		return false
	}
	// Cursor may already be an unreaped zombie while we drain its last output.
	// Original parent identities still prove ancestry after reparenting to init.
	var expected uint64
	seen := make(map[uint64]bool)
	for pid > 1 {
		identity, err := readCursorDarwinIdentity(pid)
		if err != nil || (expected != 0 && identity.unique != expected) || seen[identity.unique] {
			return false
		}
		if identity.unique == rootIdentity.unique || identity.parentUnique == rootIdentity.unique {
			return true
		}
		seen[identity.unique] = true
		info, err := readCursorUnixProcessInfo(pid)
		if err != nil {
			return false
		}
		expected, pid = identity.parentUnique, info.ppid
	}
	return false
}

// XNU bsd/sys/proc_info_private.h defines this 56-byte ABI. Only the kernel's
// unique birth ID and PID version are used; no Mach port or cgo is required.
type cursorDarwinIdentity struct {
	uuid                   [16]byte
	unique, parentUnique   uint64
	version, parentVersion uint32
	reserved               [2]uint64
}

func readCursorDarwinIdentity(pid int) (cursorDarwinIdentity, error) {
	var identity cursorDarwinIdentity
	// arg=1 includes unreaped zombies, needed to identify a root that exited
	// while its final stream events were buffered. Executable-work scans filter them.
	n, _, errno := syscall.Syscall6(unix.SYS_PROC_INFO, 2, uintptr(pid), 17, 1, uintptr(unsafe.Pointer(&identity)), unsafe.Sizeof(identity))
	if errno != 0 {
		return identity, errno
	}
	if n != 56 || identity.unique == 0 {
		return identity, errCursorBackgroundProcessIdentity
	}
	return identity, nil
}

func signalCursorDarwinIdentity(pid int, version uint32, sig syscall.Signal) error {
	// proc_signal_with_audittoken consumes PID and pidversion from the token;
	// permissions still come from the caller's real credentials.
	var token [8]uint32
	token[5], token[7] = uint32(pid), version
	_, _, errno := syscall.Syscall6(unix.SYS_PROC_INFO, 0x11, 0, uintptr(sig), 0, uintptr(unsafe.Pointer(&token)), unsafe.Sizeof(token))
	if errno != 0 {
		return errno
	}
	return nil
}

// Darwin's proc state constants are defined in <sys/proc.h>; x/sys/unix does
// not export SZOMB on all supported Darwin architectures.
const cursorDarwinZombieState = 5

func readCursorUnixProcessInfo(pid int) (cursorUnixProcessInfo, error) {
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return cursorUnixProcessInfo{}, err
	}
	return cursorUnixProcessInfo{
		pid:    int(proc.Proc.P_pid),
		ppid:   int(proc.Eproc.Ppid),
		pgid:   int(proc.Eproc.Pgid),
		start:  uint64(proc.Proc.P_starttime.Sec)*1_000_000 + uint64(proc.Proc.P_starttime.Usec),
		zombie: proc.Proc.P_stat == cursorDarwinZombieState,
	}, nil
}

func listCursorUnixProcessInfos() ([]cursorUnixProcessInfo, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	infos := make([]cursorUnixProcessInfo, 0, len(procs))
	for _, proc := range procs {
		infos = append(infos, cursorUnixProcessInfo{
			pid:    int(proc.Proc.P_pid),
			ppid:   int(proc.Eproc.Ppid),
			pgid:   int(proc.Eproc.Pgid),
			start:  uint64(proc.Proc.P_starttime.Sec)*1_000_000 + uint64(proc.Proc.P_starttime.Usec),
			zombie: proc.Proc.P_stat == cursorDarwinZombieState,
		})
	}
	return infos, nil
}
