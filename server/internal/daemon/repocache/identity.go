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

const identityConfigInclude = "# Multica identity defaults; explicit worktree settings below take precedence.\n[include]\n\tpath = multica-identity.config\n"

// isolateWorktreeIdentityContext masks identity inherited from the shared cache
// without rewriting it: other, possibly running, worktrees must not change.
// The user's system/global config (including conditional includes evaluated in
// this checkout) supplies the defaults. Explicit worktree config and Git's
// command/environment overrides retain their normal precedence. AgentName is
// deliberately not an author identity, and the co-author hook is independent.
//
// Defaults live in an include before the user's worktree settings, so a later
// checkout can refresh them without overwriting intentional task-local values.
// Missing identities are masked with empty values: fail at commit time rather
// than silently attribute work to the shared cache's previous author.
func isolateWorktreeIdentityContext(ctx context.Context, barePath, checkoutPath string) error {
	if !isGitWorktree(checkoutPath) {
		// Isolated clones already have private config and do not copy the
		// cache's identity. Preserve their intentional repository identity.
		return nil
	}
	out, err := runGitOutputContext(ctx, "-C", checkoutPath, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve linked worktree common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(checkoutPath, commonDir)
	}
	if !sameResolvedPath(commonDir, barePath) {
		return fmt.Errorf("linked worktree common dir %s does not match cache %s", commonDir, barePath)
	}
	if err := enableWorktreeConfigContext(ctx, barePath); err != nil {
		return err
	}
	out, err = runGitOutputContext(ctx, "-C", checkoutPath, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return fmt.Errorf("resolve worktree config directory: %w", err)
	}
	gitDir := strings.TrimSpace(string(out))
	out, err = runGitOutputContext(ctx, "-C", checkoutPath, "config", "--null", "--show-scope", "--includes", "--get-regexp", `^(user|author|committer)\.(name|email)$`)
	var exitErr *exec.ExitError
	if err != nil && !(errors.As(err, &exitErr) && exitErr.ExitCode() == 1) {
		return fmt.Errorf("read user Git identity: %w", err)
	}
	values := make(map[string]string)
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] != "global" && fields[i] != "system" {
			continue
		}
		key, value, _ := strings.Cut(fields[i+1], "\n")
		values[key] = value
	}
	var config strings.Builder
	for _, section := range []string{"user", "author", "committer"} {
		fmt.Fprintf(&config, "[%s]\n", section)
		for _, field := range []string{"name", "email"} {
			// Empty author/committer fields fall back to user.* in Git.
			fmt.Fprintf(&config, "\t%s = %s\n", field, quoteGitConfig(values[section+"."+field]))
		}
	}
	if err := editGitConfigFile(filepath.Join(gitDir, "multica-identity.config"), func(_ []byte, _ string) ([]byte, error) {
		return []byte(config.String()), nil
	}); err != nil {
		return err
	}
	return editGitConfigFile(filepath.Join(gitDir, "config.worktree"), func(contents []byte, _ string) ([]byte, error) {
		if strings.HasPrefix(string(contents), identityConfigInclude) {
			return contents, nil
		}
		return append([]byte(identityConfigInclude), contents...), nil
	})
}

func quoteGitConfig(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\t", "\\t", "\b", "\\b")
	return "\"" + replacer.Replace(value) + "\""
}

// Git requires core.bare/core.worktree to move to the main worktree's config
// when enabling worktreeConfig. Publish the extension and removal together so
// existing linked worktrees never see the bare cache's core.bare=true applied
// to themselves. No shared identity keys are removed or rewritten.
func enableWorktreeConfigContext(ctx context.Context, barePath string) error {
	configPath := filepath.Join(barePath, "config")
	// Once migrated, identity setup only needs private worktree files. Do not
	// compete with agent-side git config writers for the common config lock.
	if enabled, err := worktreeConfigEnabledContext(ctx, configPath); err != nil || enabled {
		return err
	}
	return editGitConfigFile(configPath, func(contents []byte, lockPath string) ([]byte, error) {
		// Another writer may have enabled it before we acquired the lock.
		if enabled, err := worktreeConfigEnabledContext(ctx, lockPath); err != nil || enabled {
			return contents, err
		}
		for _, key := range []string{"core.bare", "core.worktree"} {
			out, err := runGitOutputContext(ctx, "config", "--file", lockPath, "--get", key)
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
				continue
			}
			if err != nil {
				return nil, err
			}
			value := strings.TrimSuffix(string(out), "\n")
			if err := runGitContext(ctx, "config", "--file", filepath.Join(barePath, "config.worktree"), key, value); err != nil {
				return nil, err
			}
			if err := runGitContext(ctx, "config", "--file", lockPath, "--unset-all", key); err != nil {
				return nil, err
			}
		}
		if err := runGitContext(ctx, "config", "--file", lockPath, "extensions.worktreeConfig", "true"); err != nil {
			return nil, err
		}
		return os.ReadFile(lockPath)
	})
}

func worktreeConfigEnabledContext(ctx context.Context, path string) (bool, error) {
	out, err := runGitOutputContext(ctx, "config", "--file", path, "--bool", "--get", "extensions.worktreeConfig")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return strings.TrimSpace(string(out)) == "true", err
}

// Follow Git's lockfile protocol and atomically publish a complete config.
// This also respects concurrent user `git config` writes outside our repo lock.
func editGitConfigFile(path string, edit func([]byte, string) ([]byte, error)) error {
	return editGitConfigFileWithRename(path, edit, os.Rename)
}

// The rename argument lets tests place another writer exactly at lock handoff.
func editGitConfigFileWithRename(path string, edit func([]byte, string) ([]byte, error), rename func(string, string) error) error {
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("lock Git config: %w", err)
	}
	owned := true
	defer func() {
		if owned {
			_ = os.Remove(lockPath)
		}
	}()
	contents, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		f.Close()
		return err
	}
	if info, err := os.Stat(path); err == nil {
		if err := f.Chmod(info.Mode().Perm()); err != nil {
			f.Close()
			return err
		}
	}
	_, writeErr := f.Write(contents)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	updated, err := edit(contents, lockPath)
	if err != nil {
		return err
	}
	if string(contents) == string(updated) {
		return nil
	}
	if err := os.WriteFile(lockPath, updated, 0o600); err != nil {
		return err
	}
	if err := rename(lockPath, path); err != nil {
		return err
	}
	// Rename consumes our lock. Its old path may already belong to another
	// writer, so deferred cleanup must only unlink on rollback, never publish.
	owned = false
	return nil
}
