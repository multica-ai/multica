package repocache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const worktreeExcludesInclude = "# Multica runtime file exclusions.\n[include]\n\tpath = multica-excludes.config\n"

// Linked worktrees share info/exclude. Configure only the managed worktree,
// after identity setup has migrated core.bare/core.worktree if necessary.
// Neither the task process environment nor the pool's shared ignores change.
func configureWorktreeExcludesContext(ctx context.Context, checkoutPath string) error {
	if !isGitWorktree(checkoutPath) {
		return nil
	}
	out, err := runGitOutputContext(ctx, "-C", checkoutPath, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return fmt.Errorf("resolve worktree Git directory: %w", err)
	}
	gitDir := strings.TrimSuffix(string(out), "\n")
	managed := filepath.Join(gitDir, "multica-runtime.ignore")
	source, err := inheritedExcludesPath(ctx, checkoutPath, managed)
	if err != nil {
		return err
	}

	var content strings.Builder
	if source != "" {
		data, err := os.ReadFile(source)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read inherited Git excludes: %w", err)
		}
		content.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			content.WriteByte('\n')
		}
	}
	for _, pattern := range agentGitExcludePatterns {
		fmt.Fprintf(&content, "/%s\n", pattern)
	}
	if err := editGitConfigFile(managed, func(_ []byte, _ string) ([]byte, error) {
		return []byte(content.String()), nil
	}); err != nil {
		return err
	}
	if err := editGitConfigFile(filepath.Join(gitDir, "multica-excludes.config"), func(_ []byte, _ string) ([]byte, error) {
		config := "[core]\n\texcludesFile = " + quoteGitConfig(filepath.ToSlash(managed)) + "\n"
		return []byte(config), nil
	}); err != nil {
		return err
	}
	return editGitConfigFile(filepath.Join(gitDir, "config.worktree"), func(contents []byte, _ string) ([]byte, error) {
		// Refresh on reuse without accumulating includes or ignoring intentional
		// settings elsewhere. The built-in file should be the last file setting.
		config := strings.ReplaceAll(string(contents), worktreeExcludesInclude, "")
		if config != "" && !strings.HasSuffix(config, "\n") {
			config += "\n"
		}
		return []byte(config + worktreeExcludesInclude), nil
	})
}

// core.excludesFile is single-valued. Find the setting that would have won
// without our generated include, rather than recursively copying last run's
// generated file. Git itself expands tilde and Git's own path prefixes.
func inheritedExcludesPath(ctx context.Context, checkoutPath, managed string) (string, error) {
	out, err := runGitOutputContext(ctx, "-C", checkoutPath, "config", "--null", "--includes", "--path", "--get-all", "core.excludesFile")
	var exitErr *exec.ExitError
	if err != nil && !(errors.As(err, &exitErr) && exitErr.ExitCode() == 1) {
		return "", fmt.Errorf("read Git excludes configuration: %w", err)
	}
	configured := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	for i := len(configured) - 1; err == nil && i >= 0; i-- {
		path := configured[i]
		if path == "" {
			return "", nil
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(checkoutPath, path)
		}
		if filepath.Clean(path) != managed {
			return path, nil
		}
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "git", "ignore"), nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return "", nil
	}
	return filepath.Join(home, ".config", "git", "ignore"), nil
}
