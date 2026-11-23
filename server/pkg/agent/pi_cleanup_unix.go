//go:build linux || darwin

package agent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

type piProcessIdentity interface {
	signal(syscall.Signal) error
	close()
}

type piTrackedProcess struct {
	info cursorUnixProcessInfo
	id   piProcessIdentity
}

type piUnixProcessTracker struct {
	root                int
	known               map[int]piTrackedProcess
	observedDescendants bool
	toolInFlight        bool
}

func newPiProcessTracker(cmd *exec.Cmd) piProcessTracker {
	return &piUnixProcessTracker{root: cmd.Process.Pid, known: make(map[int]piTrackedProcess)}
}

const piCanObserveRuntimeExit = true

func piRuntimeExited(cmd *exec.Cmd) bool {
	info, err := readCursorUnixProcessInfo(cmd.Process.Pid)
	return piProcessMissing(cmd.Process.Pid, err) || (err == nil && info.zombie)
}

func (p *piUnixProcessTracker) unobservedTools() bool { return !p.observedDescendants }

func (p *piUnixProcessTracker) snapshot(toolInFlight bool) error {
	if toolInFlight != p.toolInFlight {
		p.observedDescendants = false
		p.toolInFlight = toolInFlight
	}
	all, err := listCursorUnixProcessInfos()
	if err != nil {
		return fmt.Errorf("inspect runtime descendants: %w", err)
	}
	current := make(map[int]cursorUnixProcessInfo, len(all))
	owned := make(map[int]bool)
	for _, info := range all {
		if info.zombie {
			continue
		}
		current[info.pid] = info
		if previous, ok := p.known[info.pid]; ok && previous.info.start == info.start {
			owned[info.pid] = true
		}
	}
	// Release handles for exited incarnations during long runs, rather than
	// retaining one descriptor for every command the agent ever executed.
	for pid, previous := range p.known {
		info, exists := current[pid]
		if !exists {
			// Enumeration can omit an inaccessible process. Keep its retained
			// handle unless a direct identity query proves that it exited.
			var readErr error
			info, readErr = readCursorUnixProcessInfo(pid)
			if readErr != nil && !piProcessMissing(pid, readErr) {
				return fmt.Errorf("inspect retained process %d: %w", pid, readErr)
			}
			exists = readErr == nil && !info.zombie
			if exists {
				current[pid] = info
				if info.start == previous.info.start {
					owned[pid] = true
				}
			}
		}
		if !exists || info.start != previous.info.start {
			previous.id.close()
			delete(p.known, pid)
		}
	}
	if len(p.known) == 0 {
		owned[p.root] = true
	}
	// Resolve ancestry while it exists, retaining birth identities across
	// reparenting. A reused PID never becomes an ancestry anchor.
	for changed := true; changed; {
		changed = false
		for _, info := range current {
			if !owned[info.pid] && owned[info.ppid] {
				owned[info.pid] = true
				changed = true
			}
		}
	}
	var captureErr error
	for pid := range owned {
		info, ok := current[pid]
		if !ok {
			continue
		}
		if prior, ok := p.known[pid]; ok {
			if prior.info.start == info.start {
				if toolInFlight && pid != p.root {
					p.observedDescendants = true
				}
				continue
			}
			// Retain the original identity; never adopt the replacement.
			continue
		}
		if !p.belongsToRun(info) {
			continue
		}
		identity, err := capturePiProcessIdentity(info)
		if err != nil {
			if !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrNotExist) {
				captureErr = errors.Join(captureErr, fmt.Errorf("retain process %d: %w", pid, err))
			}
			continue
		}
		p.known[pid] = piTrackedProcess{info: info, id: identity}
		if toolInFlight && pid != p.root {
			p.observedDescendants = true
		}
	}
	return captureErr
}

// Revalidate the live ancestry before retaining a new identity. Enumeration is
// not atomic: a parent PID from that snapshot may already have been reused.
func (p *piUnixProcessTracker) belongsToRun(info cursorUnixProcessInfo) bool {
	seen := make(map[int]bool)
	for info.pid > 1 && !seen[info.pid] {
		if info.pid == p.root {
			return true // The root is our own, still-unreaped exec.Cmd child.
		}
		seen[info.pid] = true
		parent, err := readCursorUnixProcessInfo(info.ppid)
		if err != nil || parent.zombie {
			return false
		}
		child, err := readCursorUnixProcessInfo(info.pid)
		if err != nil || child.start != info.start || child.ppid != parent.pid {
			return false
		}
		if retained, ok := p.known[parent.pid]; ok && retained.info.start == parent.start {
			return true
		}
		info = parent
	}
	return false
}

func (p *piUnixProcessTracker) signal(sig syscall.Signal) error {
	var result error
	for pid, proc := range p.known {
		if err := proc.id.signal(sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			result = errors.Join(result, fmt.Errorf("signal owned process %d: %w", pid, err))
		}
	}
	return result
}

func (p *piUnixProcessTracker) remaining() ([]int, error) {
	var remaining []int
	for pid, proc := range p.known {
		current, err := readCursorUnixProcessInfo(pid)
		if piProcessMissing(pid, err) {
			continue
		}
		if err != nil {
			return remaining, fmt.Errorf("inspect owned process %d: %w", pid, err)
		}
		if current.start == proc.info.start && !current.zombie {
			remaining = append(remaining, pid)
		}
	}
	return remaining, nil
}

func (p *piUnixProcessTracker) close() {
	for _, proc := range p.known {
		proc.id.close()
	}
}
