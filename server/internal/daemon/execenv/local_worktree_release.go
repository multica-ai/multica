package execenv

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// FinalizedLocalWorktree records only the cleanup remaining after a successful
// delivery. It is safe to persist alongside the environments using the checkout;
// release must not re-finalize, auto-commit, or update the conversation record.
type FinalizedLocalWorktree struct {
	GitRoot     string `json:"git_root"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Tip         string `json:"tip"`
	EmptyBranch string `json:"empty_branch,omitempty"`
	EmptyTip    string `json:"empty_tip,omitempty"`
}

// Release unregisters a delivered checkout under the same repository lock as
// preparation and finalization. The caller must exclude new environment starts.
func (w *FinalizedLocalWorktree) Release(logger *slog.Logger) error {
	unlock, err := lockGitRoot(w.GitRoot, logger)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Lstat(w.Path); err == nil {
		root, rootErr := runGitTrimmed(w.Path, "rev-parse", "--show-toplevel")
		common, commonErr := gitCommonDirFor(w.Path)
		ownerCommon, ownerErr := gitCommonDirFor(w.GitRoot)
		branch, branchErr := runGitTrimmed(w.Path, "symbolic-ref", "--short", "HEAD")
		tip, tipErr := runGitTrimmed(w.Path, "rev-parse", "--verify", "HEAD")
		if rootErr != nil || commonErr != nil || ownerErr != nil || branchErr != nil || tipErr != nil || filepath.Clean(root) != filepath.Clean(w.Path) || common != ownerCommon || branch != w.Branch || tip != w.Tip {
			return fmt.Errorf("retained worktree changed since delivery; preserving %s", w.Path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := removeLocalWorktreeDir(w.GitRoot, w.Path, logger); err != nil {
		return err
	}
	if w.EmptyBranch != "" {
		// A branch changed since delivery is no longer an empty task artifact.
		if tip, err := runGitTrimmed(w.GitRoot, "rev-parse", "--verify", "refs/heads/"+w.EmptyBranch); err == nil && tip == w.EmptyTip {
			dropBranch(w.GitRoot, w.EmptyBranch, logger)
		}
	}
	return nil
}
