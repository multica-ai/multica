package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// detectGitRepo checks if dir is inside a git repository (regular or bare).
// Returns the git root path and true if found.
func detectGitRepo(dir string) (string, bool) {
	// Try regular repo first.
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")

	if out, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(out)), true
	}

	// Try bare repo: git-dir is "." for bare repos when -C points at the repo.
	cmd = exec.Command("git", "-C", dir, "rev-parse", "--is-bare-repository")

	if out, err := cmd.Output(); err == nil && strings.TrimSpace(string(out)) == "true" {
		return dir, true
	}

	return "", false
}

// fetchOrigin runs `git fetch origin` to ensure the local repo has the latest remote refs.
func fetchOrigin(gitRoot string) error {
	cmd := exec.Command("git", "-C", gitRoot, "fetch", "origin")

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch origin: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// getRemoteDefaultBranch returns "origin/<branch>" for the remote's default branch.
// Falls back to "origin/main", then "HEAD".
func getRemoteDefaultBranch(gitRoot string) string {
	// Try symbolic-ref of origin/HEAD (set by `git clone` or `git remote set-head`).
	cmd := exec.Command("git", "-C", gitRoot, "symbolic-ref", "refs/remotes/origin/HEAD")

	if out, err := cmd.Output(); err == nil {
		ref := strings.TrimSpace(string(out))
		// ref looks like "refs/remotes/origin/main" — return "origin/main".
		if strings.HasPrefix(ref, "refs/remotes/") {
			return strings.TrimPrefix(ref, "refs/remotes/")
		}
		return ref
	}

	// Fallback: check if origin/main exists.
	cmd = exec.Command("git", "-C", gitRoot, "rev-parse", "--verify", "origin/main")

	if err := cmd.Run(); err == nil {
		return "origin/main"
	}

	// Fallback: check if origin/master exists.
	cmd = exec.Command("git", "-C", gitRoot, "rev-parse", "--verify", "origin/master")

	if err := cmd.Run(); err == nil {
		return "origin/master"
	}

	return "HEAD"
}

// setupGitWorktree creates a git worktree at worktreePath with a new branch.
func setupGitWorktree(gitRoot, worktreePath, branchName, baseRef string) error {
	// Remove the workdir created by caller — git worktree add needs to create it.
	if err := os.Remove(worktreePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove placeholder workdir: %w", err)
	}

	err := runGitWorktreeAdd(gitRoot, worktreePath, branchName, baseRef)
	if err != nil && strings.Contains(err.Error(), "already exists") {
		// Branch name collision: append timestamp and retry once.
		branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
		err = runGitWorktreeAdd(gitRoot, worktreePath, branchName, baseRef)
	}
	return err
}

func runGitWorktreeAdd(gitRoot, worktreePath, branchName, baseRef string) error {
	cmd := exec.Command("git", "-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath, baseRef)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// removeGitWorktree removes a worktree and its branch. Best-effort: logs errors.
func removeGitWorktree(gitRoot, worktreePath, branchName string, logger *slog.Logger) {
	// Remove the worktree.
	cmd := exec.Command("git", "-C", gitRoot, "worktree", "remove", "--force", worktreePath)

	if out, err := cmd.CombinedOutput(); err != nil {
		logger.Warn("execenv: git worktree remove failed", "output", strings.TrimSpace(string(out)), "error", err)
	}

	// Delete the branch (best-effort).
	if branchName != "" {
		cmd = exec.Command("git", "-C", gitRoot, "branch", "-D", branchName)

		if out, err := cmd.CombinedOutput(); err != nil {
			logger.Warn("execenv: git branch delete failed", "branch", branchName, "output", strings.TrimSpace(string(out)), "error", err)
		}
	}
}

// excludeFromGit adds a pattern to the worktree's .git/info/exclude file.
func excludeFromGit(worktreePath, pattern string) error {
	// Resolve the actual git dir for this worktree.
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-dir")

	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("resolve git dir: %w", err)
	}

	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}

	excludePath := filepath.Join(gitDir, "info", "exclude")

	// Ensure the info directory exists.
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("create info dir: %w", err)
	}

	// Check if pattern is already present.
	existing, _ := os.ReadFile(excludePath)
	if strings.Contains(string(existing), pattern) {
		return nil
	}

	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open exclude file: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "\n%s\n", pattern); err != nil {
		return fmt.Errorf("write exclude pattern: %w", err)
	}
	return nil
}

// repoNameFromURL extracts a short directory name from a git remote URL.
// e.g. "https://github.com/org/my-repo.git" → "my-repo"
func repoNameFromURL(url string) string {
	// Strip trailing slashes and .git suffix.
	url = strings.TrimRight(url, "/")
	url = strings.TrimSuffix(url, ".git")

	// Take the last path segment.
	if i := strings.LastIndex(url, "/"); i >= 0 {
		url = url[i+1:]
	}
	// Also handle SSH-style "host:org/repo".
	if i := strings.LastIndex(url, ":"); i >= 0 {
		url = url[i+1:]
		if j := strings.LastIndex(url, "/"); j >= 0 {
			url = url[j+1:]
		}
	}

	name := strings.TrimSpace(url)
	if name == "" {
		return "repo"
	}
	return name
}

// taskKeyLen is how many hex chars of the task id identify a task in a path or
// a branch name. Every char here is spent twice — the env root prefixes the
// agent's whole workdir, and the branch name becomes a path under
// .git/refs/heads/ inside that workdir — and Windows still enforces MAX_PATH
// (260). The full 32-char id overflows it on a deep checkout, so the segment
// stays short and buys its uniqueness from entropy instead of length.
const taskKeyLen = 12

// taskKey returns the segment identifying a task in a path or branch name: the
// LAST taskKeyLen hex chars of the id.
//
// Which end matters more than how many chars. Task ids are UUIDv7 — 48 bits of
// millisecond timestamp, then randomness. The leading 8 hex chars are the high
// 32 bits of that timestamp, so they only advance once every 2^16 ms (~65.5s):
// taking them from the front gave every task started inside one such window an
// identical segment, and therefore one shared env root. That is not a rare hash
// collision, it is the common case, and it made Prepare's "remove existing env"
// step delete a concurrently running task's directory (#7326).
//
// The tail is drawn from the id's random field, so 12 chars carry 48 random
// bits. Prepare additionally refuses to delete an env root another task owns,
// so even an improbable clash fails closed instead of destroying work.
//
// Use shortID for logs, never for identity.
func taskKey(uuid string) string {
	s := strings.ReplaceAll(uuid, "-", "")
	if len(s) > taskKeyLen {
		return s[len(s)-taskKeyLen:]
	}
	return s
}

// shortID returns the first 8 characters of a UUID string (dashes stripped).
// Display and logging only — see taskKey for anything that must be unique.
func shortID(uuid string) string {
	s := strings.ReplaceAll(uuid, "-", "")
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// sanitizeName produces a git-branch-safe name from a human-readable string.
func sanitizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 30 {
		s = s[:30]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		s = "agent"
	}
	return s
}
