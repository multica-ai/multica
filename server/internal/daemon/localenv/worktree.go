package localenv

import (
	"path/filepath"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// FinalizeWorktree delivers the branch without removing files a declared
// environment still serves. Holding the same lock as Start closes the gap
// between deciding whether to retain and removing the checkout.
func (m *Manager) FinalizeWorktree(w *execenv.LocalWorktree) (execenv.LocalWorktreeOutcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var users []*entry
	path := ""
	if w != nil {
		path = w.Path
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		for _, e := range m.envs {
			if (e.live() || e.restartPins > 0) && within(e.rec.RepoRoot, path) {
				users = append(users, e)
			}
		}
	}
	if len(users) == 0 {
		return w.Finalize(m.logger)
	}
	outcome, err := w.FinalizeForEnvironment(m.logger)
	if err != nil {
		return outcome, err
	}
	outcome.RetainedWorktree.Path = path
	for _, e := range users {
		e.rec.Worktree = outcome.RetainedWorktree
		m.persistLocked(e)
	}
	return outcome, nil
}

// releaseWorktreesLocked also checks ended records: one environment can end
// while a newer one uses its checkout, and the obligation to clean up must
// survive both that handoff and a daemon restart.
func (m *Manager) releaseWorktreesLocked() {
	attempted := make(map[string]bool)
	for _, e := range m.envs {
		w := e.rec.Worktree
		if w == nil || attempted[w.Path] {
			continue
		}
		attempted[w.Path] = true
		inUse := false
		for _, user := range m.envs {
			if !within(user.rec.RepoRoot, w.Path) {
				continue
			}
			if user.live() || user.restartPins > 0 || groupAlive(user.rec.PID) {
				inUse = true
				break
			}
			for _, pid := range user.rec.Adopted {
				if groupAlive(pid) {
					inUse = true
					break
				}
			}
			if inUse {
				break
			}
		}
		if inUse {
			continue
		}
		if err := w.Release(m.logger); err != nil {
			m.logger.Warn("release environment worktree failed", "path", w.Path, "error", err)
			continue
		}
		for _, user := range m.envs {
			if user.rec.Worktree != nil && user.rec.Worktree.Path == w.Path {
				user.rec.Worktree = nil
				m.persistLocked(user)
			}
		}
	}
}
