package gitops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// PublishOptions configures how a workspace repository is published.
type PublishOptions struct {
	Remote        string
	CommitMessage string
}

// PublishResult describes the outcome of publishing a single repository.
type PublishResult struct {
	Root          string `json:"root"`
	Branch        string `json:"branch"`
	Remote        string `json:"remote"`
	CommitHash    string `json:"commit_hash,omitempty"`
	Committed     bool   `json:"committed"`
	Pushed        bool   `json:"pushed"`
	StagedChanges bool   `json:"staged_changes"`
}

// PublishWorkspace discovers git repositories under root and publishes each
// one. If root itself is a git repository, it is published directly; otherwise
// direct child directories are scanned for git repos.
func PublishWorkspace(root string, opts PublishOptions) ([]PublishResult, error) {
	roots, err := discoverPublishRoots(root)
	if err != nil {
		return nil, err
	}

	results := make([]PublishResult, 0, len(roots))
	var errs []error
	for _, repoRoot := range roots {
		result, err := PublishRepo(repoRoot, opts)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", repoRoot, err))
			continue
		}
		results = append(results, result)
	}

	if len(errs) > 0 {
		return results, joinErrors(errs...)
	}
	return results, nil
}

// PublishRepo stages local changes, commits them if needed, and pushes the
// current branch to the selected remote.
func PublishRepo(root string, opts PublishOptions) (PublishResult, error) {
	root = filepath.Clean(root)
	if !isGitRepo(root) {
		return PublishResult{}, fmt.Errorf("not a git repository: %s", root)
	}

	branch, err := gitOutput(root, "branch", "--show-current")
	if err != nil {
		return PublishResult{}, fmt.Errorf("detect branch: %w", err)
	}
	if branch == "" {
		return PublishResult{}, fmt.Errorf("detached HEAD not supported for publish: %s", root)
	}

	remote := strings.TrimSpace(opts.Remote)
	if remote == "" {
		remote = "origin"
	}

	statusOut, err := gitOutput(root, "status", "--porcelain")
	if err != nil {
		return PublishResult{}, fmt.Errorf("read git status: %w", err)
	}
	hasChanges := strings.TrimSpace(statusOut) != ""

	var committed bool
	if hasChanges {
		if err := gitRun(root, "add", "-A"); err != nil {
			return PublishResult{}, fmt.Errorf("git add: %w", err)
		}
		staged, err := hasStagedChanges(root)
		if err != nil {
			return PublishResult{}, fmt.Errorf("detect staged changes: %w", err)
		}
		if staged {
			message := strings.TrimSpace(opts.CommitMessage)
			if message == "" {
				message = "multica auto publish"
			}
			if err := gitRun(root, "commit", "-m", message); err != nil {
				return PublishResult{}, fmt.Errorf("git commit: %w", err)
			}
			committed = true
		}
	}

	if err := gitRun(root, "push", "-u", remote, branch); err != nil {
		return PublishResult{}, fmt.Errorf("git push: %w", err)
	}

	commitHash, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return PublishResult{}, fmt.Errorf("resolve commit hash: %w", err)
	}

	return PublishResult{
		Root:          root,
		Branch:        branch,
		Remote:        remote,
		CommitHash:    strings.TrimSpace(commitHash),
		Committed:     committed,
		Pushed:        true,
		StagedChanges: hasChanges,
	}, nil
}

func discoverPublishRoots(root string) ([]string, error) {
	if isGitRepo(root) {
		return []string{root}, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read workspace root: %w", err)
	}

	var roots []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child := filepath.Join(root, entry.Name())
		if isGitRepo(child) {
			roots = append(roots, child)
		}
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		return nil, fmt.Errorf("no git repositories found under %s", root)
	}
	return roots, nil
}

func isGitRepo(path string) bool {
	if path == "" {
		return false
	}
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s: %w", strings.Join(append([]string{"git"}, args...), " "), strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitRun(root string, args ...string) error {
	_, err := gitOutput(root, args...)
	return err
}

func hasStagedChanges(root string) (bool, error) {
	cmd := exec.Command("git", "-C", root, "diff", "--cached", "--quiet")
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil
		}
		return false, fmt.Errorf("git diff --cached --quiet: %w", err)
	}
	return false, nil
}

func joinErrors(errs ...error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return errors.Join(errs...)
}
