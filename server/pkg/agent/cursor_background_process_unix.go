//go:build linux || darwin

package agent

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

type cursorUnixProcessInfo struct {
	pid    int
	ppid   int
	pgid   int
	start  uint64
	zombie bool
}

type cursorUnixBackgroundProcess struct {
	pgid        int
	leaderStart uint64
	group       *cursorUnixGroupHandle
}

func captureCursorBackgroundProcess(cmd *exec.Cmd, pid int) (*cursorBackgroundProcess, error) {
	return captureCursorUnixBackgroundProcess(cmd, pid, captureCursorUnixGroup)
}

func captureCursorUnixBackgroundProcess(cmd *exec.Cmd, pid int, retainGroup func(int) (*cursorUnixGroupHandle, error)) (*cursorBackgroundProcess, error) {
	if cmd == nil || cmd.Process == nil || pid <= 0 || pid == cmd.Process.Pid {
		return nil, errCursorBackgroundProcessInvalid
	}
	root, err := readCursorUnixProcessInfo(cmd.Process.Pid)
	if err != nil {
		return nil, fmt.Errorf("read Cursor process: %w", err)
	}
	target, err := readCursorUnixProcessInfo(pid)
	if err != nil || target.zombie || target.pid != pid {
		return nil, errCursorBackgroundProcessInvalid
	}
	if target.pgid != target.pid || target.pgid == root.pgid {
		return nil, errCursorBackgroundProcessInvalid
	}
	if !cursorUnixIsDescendant(pid, root) {
		return nil, errCursorBackgroundProcessInvalid
	}
	leader, err := readCursorUnixProcessInfo(target.pgid)
	if err != nil || leader.pid != target.pgid || leader.pgid != target.pgid || leader.start != target.start {
		return nil, errCursorBackgroundProcessInvalid
	}
	group, err := retainGroup(target.pgid)
	if err != nil {
		return nil, fmt.Errorf("retain Cursor shell process group: %w", err)
	}
	// The ancestry above proves where this process came from. The Cursor root
	// may now exit and reparent it; retain the claim while the target identity
	// is unchanged, without requiring its original parent to remain alive.
	leader, err = readCursorUnixProcessInfo(pid)
	if err != nil || leader.pid != target.pid || leader.start != target.start || leader.zombie || leader.pgid != target.pgid {
		group.close()
		return nil, errCursorBackgroundProcessInvalid
	}
	return &cursorBackgroundProcess{platform: &cursorUnixBackgroundProcess{
		pgid:        target.pgid,
		leaderStart: target.start,
		group:       group,
	}}, nil
}

func (p *cursorUnixBackgroundProcess) alive() (bool, error) {
	owned, err := p.groupOwned()
	if err != nil || !owned {
		return false, err
	}
	return true, nil
}

func (p *cursorUnixBackgroundProcess) terminate() error {
	deadline := time.Now().Add(time.Second)
	for {
		owned, err := p.groupOwned()
		if err != nil {
			return err
		}
		if !owned {
			return nil
		}
		// A shell can fork during cleanup. Revalidate and stop newly proven
		// members within this existing cleanup bound, without a liveness timer.
		if err := p.group.signal(syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Cursor background process group %d still active after %s", p.pgid, time.Second)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *cursorUnixBackgroundProcess) close() { p.group.close() }

func (p *cursorUnixBackgroundProcess) groupOwned() (bool, error) {
	leader, leaderErr := readCursorUnixProcessInfo(p.pgid)
	if leaderErr == nil && !leader.zombie && leader.pid == p.pgid && leader.pgid == p.pgid {
		if leader.start != p.leaderStart {
			return false, errCursorBackgroundProcessIdentity
		}
	}
	// Linux retains a kernel group reference; Darwin verifies individual birth
	// identities and lineage. Neither signals an unverified numeric PGID.
	if err := p.group.signal(0); err != nil {
		if err == syscall.ESRCH {
			return false, nil
		}
		return false, err
	}
	members, err := listCursorUnixProcessGroup(p.pgid)
	if err != nil {
		return false, err
	}
	return len(members) > 0, nil
}

func listCursorUnixProcessGroup(pgid int) (map[int]uint64, error) {
	all, err := listCursorUnixProcessInfos()
	if err != nil {
		return nil, err
	}
	members := make(map[int]uint64)
	for _, info := range all {
		if info.pgid == pgid && !info.zombie {
			members[info.pid] = info.start
		}
	}
	return members, nil
}
