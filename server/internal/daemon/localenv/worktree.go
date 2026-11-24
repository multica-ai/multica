package localenv

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// A reservation excludes starts and other cleanup on the same checkout while
// slow Git and filesystem work runs without the Manager mutex.
type worktreeOperation struct {
	done       chan struct{}
	finalizing bool
}

const worktreeRetryDelay = time.Minute

func checkoutPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func pathsOverlap(a, b string) bool { return within(a, b) || within(b, a) }

func (m *Manager) worktreeBusyLocked(path string) bool {
	for reserved := range m.worktrees {
		if pathsOverlap(path, reserved) {
			return true
		}
	}
	return false
}

// RetainsWorktree is the local GC view of an operation that need not have an
// environment record (in particular, finalization without a declared server).
// No paths from this view are sent to the server.
func (m *Manager) RetainsWorktree(dir string) bool {
	dir = checkoutPath(dir)
	m.mu.Lock()
	defer m.mu.Unlock()
	for path := range m.worktrees {
		if pathsOverlap(path, dir) {
			return true
		}
	}
	return false
}

// FinalizeWorktree reserves the checkout before deciding whether to retain it.
// Entries pinned here survive Stop and Tick until the cleanup token is durable.
func (m *Manager) FinalizeWorktree(w *execenv.LocalWorktree) (execenv.LocalWorktreeOutcome, error) {
	if w == nil {
		return w.Finalize(m.logger)
	}
	path := checkoutPath(w.Path)
	m.mu.Lock()
	if m.worktreeBusyLocked(path) {
		m.mu.Unlock()
		return execenv.LocalWorktreeOutcome{PreservedPath: w.Path}, fmt.Errorf("checkout finalization or release already in progress")
	}
	op := &worktreeOperation{done: make(chan struct{}), finalizing: true}
	m.worktrees[path] = op
	var users []*entry
	for _, e := range m.envs {
		if (e.live() || e.restartPins > 0) && within(e.rec.RepoRoot, path) {
			e.finalizePins++
			users = append(users, e)
		}
	}
	m.mu.Unlock()

	var outcome execenv.LocalWorktreeOutcome
	var err error
	if len(users) == 0 {
		outcome, err = w.Finalize(m.logger)
	} else {
		outcome, err = w.FinalizeForEnvironment(m.logger)
	}

	m.mu.Lock()
	if m.worktrees[path] == op {
		if err == nil && outcome.RetainedWorktree != nil {
			outcome.RetainedWorktree.Path = path
			for _, e := range users {
				e.rec.Worktree = outcome.RetainedWorktree
				e.rec.WorktreeRetryAt = time.Time{}
				m.persistLocked(e)
			}
		}
		for _, e := range users {
			e.finalizePins--
		}
		delete(m.worktrees, path)
		close(op.done)
	}
	m.releaseWorktreesLocked()
	pending := m.cleanupDoneLocked(path)
	m.mu.Unlock()
	waitWorktrees(pending)
	return outcome, err
}

func (m *Manager) worktreeInUseLocked(path string) bool {
	for _, user := range m.envs {
		if !within(user.rec.RepoRoot, path) {
			continue
		}
		if user.live() || user.restartPins > 0 || user.finalizePins > 0 || groupAlive(user.rec.PID) {
			return true
		}
		for _, pid := range user.rec.Adopted {
			if groupAlive(pid) {
				return true
			}
		}
	}
	return false
}

// releaseWorktreesLocked merges every lifecycle request at the path gate.
// Workers are reserved under mu, but slow Release and its warnings run outside
// it. Tick and waiters never wait for Git; synchronous callers collect only
// their checkout's work after unlocking.
func (m *Manager) releaseWorktreesLocked() {
	now := m.now()
	for _, e := range m.envs {
		w := e.rec.Worktree
		if w == nil {
			continue
		}
		path := checkoutPath(w.Path)
		if m.worktreeBusyLocked(path) || m.worktreeInUseLocked(path) {
			continue
		}
		due := e.rec.WorktreeRetryAt
		for _, other := range m.envs {
			if other.rec.Worktree != nil && checkoutPath(other.rec.Worktree.Path) == path && other.rec.WorktreeRetryAt.After(due) {
				due = other.rec.WorktreeRetryAt
			}
		}
		if now.Before(due) {
			continue
		}
		op := &worktreeOperation{done: make(chan struct{})}
		m.worktrees[path] = op
		go func() {
			err := w.Release(m.logger)
			m.mu.Lock()
			if m.worktrees[path] == op {
				retry := time.Time{}
				if err != nil {
					retry = m.now().Add(worktreeRetryDelay)
				}
				for _, user := range m.envs {
					if user.rec.Worktree != nil && checkoutPath(user.rec.Worktree.Path) == path {
						if err == nil {
							user.rec.Worktree = nil
						}
						user.rec.WorktreeRetryAt = retry
						m.persistLocked(user)
					}
				}
				// Publish the diagnostic before completion, so a collected attempt also
				// includes its warning. The logger cannot hold up unrelated Manager work.
				m.mu.Unlock()
				if err != nil {
					m.logger.Warn("release environment worktree failed", "path", path, "error", err)
				}
				m.mu.Lock()
				delete(m.worktrees, path)
				close(op.done)
			}
			m.mu.Unlock()
		}()
	}
}

func (m *Manager) cleanupDoneLocked(root string) []<-chan struct{} {
	var pending []<-chan struct{}
	for path, op := range m.worktrees {
		if !op.finalizing && pathsOverlap(path, root) {
			pending = append(pending, op.done)
		}
	}
	return pending
}

func waitWorktrees(pending []<-chan struct{}) {
	for _, done := range pending {
		<-done
	}
}
