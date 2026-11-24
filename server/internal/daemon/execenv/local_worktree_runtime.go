package execenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProtectRuntimeConfig keeps an injected brief out of commits made during the
// run. A linked worktree has its own index, so skip-worktree does not affect
// the user's checkout or require shared git configuration changes.
func (w *LocalWorktree) ProtectRuntimeConfig(provider string) error {
	if w == nil || w.RuntimeConfigSkipWorktree != "" {
		return nil
	}
	path := runtimeConfigPath(w.WorkDir, provider)
	if path == "" {
		return nil
	}
	// Injection follows symlinks, so protect the file that received the brief.
	path, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("execenv: resolve runtime config: %w", err)
	}
	root, err := filepath.EvalSymlinks(w.Path)
	if err != nil {
		return fmt.Errorf("execenv: resolve worktree %q: %w", w.Path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil // External targets do not belong to this worktree's index.
	}
	rel = filepath.ToSlash(rel)
	tracked, err := runGit(w.Path, "ls-files", "-z", "--", ":(literal)"+rel)
	if err != nil {
		return fmt.Errorf("execenv: inspect tracked runtime config %q: %w", rel, err)
	}
	if tracked == "" {
		return nil
	}
	if _, err := runGit(w.Path, "update-index", "--skip-worktree", "--", rel); err != nil {
		return fmt.Errorf("execenv: protect runtime config %q from agent commits: %w", rel, err)
	}
	w.RuntimeConfigSkipWorktree = rel
	return nil
}

// RestoreRuntimeConfigTracking must run before brief cleanup and finalization
// so genuine instruction-file edits remain visible to the delivery policy.
func (w *LocalWorktree) RestoreRuntimeConfigTracking() error {
	if w == nil || w.RuntimeConfigSkipWorktree == "" {
		return nil
	}
	if _, err := runGit(w.Path, "update-index", "--no-skip-worktree", "--", w.RuntimeConfigSkipWorktree); err != nil {
		return fmt.Errorf("execenv: restore runtime config tracking for %q: %w", w.RuntimeConfigSkipWorktree, err)
	}
	w.RuntimeConfigSkipWorktree = ""
	return nil
}
