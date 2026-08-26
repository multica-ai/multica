package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// taskDirCustodyHold reports why a completed task directory must not be
// deleted. Unique commits (present locally, on no remote-tracking ref) and
// uncommitted/untracked files are sole copies; APEX-832 forbids deleting
// those. An empty reason means the tree is regenerable from remotes.
func taskDirCustodyHold(ctx context.Context, taskDir string) string {
	workDir := filepath.Join(taskDir, "workdir")
	info, err := os.Stat(workDir)
	if err != nil || !info.IsDir() {
		return ""
	}
	var hold string
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || hold != "" {
			return nil
		}
		if !d.IsDir() && d.Name() != ".git" {
			return nil
		}
		if d.Name() != ".git" {
			return nil
		}
		repo := filepath.Dir(path)
		if reason := gitRepoCustodyHold(ctx, repo); reason != "" {
			hold = reason
			return filepath.SkipAll
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	return hold
}

func gitRepoCustodyHold(ctx context.Context, repo string) string {
	out, err := runGitGCCommandContext(ctx, repo, "status", "--porcelain")
	if err != nil {
		// A checkout we cannot interrogate might still hold unique work.
		return "git status failed in " + repo
	}
	if strings.TrimSpace(out) != "" {
		return "uncommitted or untracked files in " + repo
	}
	out, err = runGitGCCommandContext(ctx, repo, "rev-list", "--all", "--not", "--remotes", "--max-count=1")
	if err != nil {
		return "git rev-list failed in " + repo
	}
	if strings.TrimSpace(out) != "" {
		return "commits not on any remote-tracking ref in " + repo
	}
	return ""
}
