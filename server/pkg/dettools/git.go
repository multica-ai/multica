package dettools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// gitAvailable reports whether a git binary is on PATH.
func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// gitOutputRaw runs `git <args...>` in workDir and returns stdout verbatim.
// Used where leading whitespace is significant (e.g. porcelain status columns).
func gitOutputRaw(ctx context.Context, workDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workDir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

// gitOutput runs `git <args...>` in workDir and returns trimmed stdout.
func gitOutput(ctx context.Context, workDir string, args ...string) (string, error) {
	out, err := gitOutputRaw(ctx, workDir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// isGitRepo reports whether workDir is inside a git work tree.
func isGitRepo(ctx context.Context, workDir string) bool {
	out, err := gitOutput(ctx, workDir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// currentBranch returns the abbreviated current branch name (or "HEAD" when
// detached).
func currentBranch(ctx context.Context, workDir string) (string, error) {
	return gitOutput(ctx, workDir, "rev-parse", "--abbrev-ref", "HEAD")
}

// changedFiles returns the set of paths with staged or unstaged changes,
// parsed from `git status --porcelain`. Renames report the destination path.
func changedFiles(ctx context.Context, workDir string) ([]string, error) {
	// Raw output: porcelain v1 lines begin with a two-column status code, so the
	// leading byte may be a space (e.g. " M path"). Trimming the whole output
	// would shift the first line's path by one character.
	out, err := gitOutputRaw(ctx, workDir, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		// Porcelain v1 lines are "XY <path>" or "XY <old> -> <new>".
		path := strings.TrimSpace(line[3:])
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		path = strings.Trim(path, "\"")
		if path != "" {
			files = append(files, path)
		}
	}
	return files, nil
}
