package execenv

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
	// SavedTree includes the already-saved leftovers, but excludes ignored
	// dependencies. IndexTree separately guards work staged after delivery.
	SavedTree string `json:"saved_tree"`
	IndexTree string `json:"index_tree"`
	CommonDir string `json:"common_dir"`
	GitDir    string `json:"git_dir"`
}

// Release unregisters a delivered checkout under the same repository lock as
// preparation and finalization. The caller must exclude new environment starts.
func (w *FinalizedLocalWorktree) Release(logger *slog.Logger) error {
	unlock, err := lockGitRoot(w.GitRoot, logger)
	if err != nil {
		return err
	}
	defer unlock()
	ownerCommon, ownerErr := retainedCommonDir(w.GitRoot)
	if ownerErr != nil || w.CommonDir == "" || ownerCommon != w.CommonDir {
		return fmt.Errorf("retained worktree owner changed since delivery; preserving %s", w.Path)
	}
	if _, err := os.Lstat(w.Path); err == nil {
		root, rootErr := runGitTrimmed(w.Path, "rev-parse", "--show-toplevel")
		common, commonErr := retainedCommonDir(w.Path)
		gitDir, gitDirErr := runGitTrimmed(w.Path, "rev-parse", "--absolute-git-dir")
		branch, branchErr := runGitTrimmed(w.Path, "symbolic-ref", "--short", "HEAD")
		tip, tipErr := runGitTrimmed(w.Path, "rev-parse", "--verify", "HEAD")
		if rootErr != nil || commonErr != nil || gitDirErr != nil || branchErr != nil || tipErr != nil || filepath.Clean(root) != filepath.Clean(w.Path) || common != ownerCommon || common != w.CommonDir || filepath.Clean(filepath.FromSlash(gitDir)) != w.GitDir || branch != w.Branch || tip != w.Tip {
			return fmt.Errorf("retained worktree changed since delivery; preserving %s", w.Path)
		}
		// The admin record must still point back to this exact linked checkout.
		registered, err := os.ReadFile(filepath.Join(gitDir, "gitdir"))
		if err != nil || filepath.Clean(strings.TrimSpace(string(registered))) != filepath.Join(w.Path, ".git") {
			return fmt.Errorf("retained worktree registration changed; preserving %s", w.Path)
		}
		tree, treeErr := stagedWorktreeTree(w.Path)
		index, indexErr := runGitTrimmed(w.Path, "write-tree")
		if treeErr != nil || indexErr != nil || w.SavedTree == "" || w.IndexTree == "" || tree != w.SavedTree || index != w.IndexTree {
			return fmt.Errorf("retained worktree has unsaved changes since delivery; preserving %s", w.Path)
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

// retainedCommonDir deliberately bypasses the path cache: a retained checkout
// can be replaced between unsuccessful cleanup attempts.
func retainedCommonDir(path string) (string, error) {
	dir, err := runGitTrimmed(path, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("resolve current common git dir for %q: %w", path, err)
	}
	if dir == "" {
		return "", fmt.Errorf("resolve current common git dir for %q: empty result", path)
	}
	dir = filepath.FromSlash(dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(path, dir)
	}
	return filepath.Clean(dir), nil
}

func (w *LocalWorktree) retainedForEnvironment(tip, leftover string) (*FinalizedLocalWorktree, error) {
	saved := tip
	if leftover != "" {
		saved = leftover
	}
	tree, err := runGitTrimmed(w.Path, "rev-parse", "--verify", saved+"^{tree}")
	if err != nil {
		return nil, err
	}
	index, err := runGitTrimmed(w.Path, "write-tree")
	if err != nil {
		return nil, err
	}
	common, err := retainedCommonDir(w.GitRoot)
	if err != nil {
		return nil, err
	}
	dir, err := runGitTrimmed(w.Path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	return &FinalizedLocalWorktree{GitRoot: w.GitRoot, Path: w.Path, Branch: w.Branch, Tip: tip, SavedTree: tree, IndexTree: index, CommonDir: common, GitDir: filepath.Clean(dir)}, nil
}
