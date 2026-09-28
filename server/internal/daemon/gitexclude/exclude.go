// Package gitexclude hides agent runtime files from Git inside one checkout.
//
// A linked worktree's private git dir has an info/ directory, but Git does not
// read it: info/exclude is resolved from the common dir (gitrepository-layout,
// "this directory is ignored if $GIT_COMMON_DIR is set"). Writing the common
// file hides those paths in every worktree of the repository, including a
// sibling task or the user's other checkout. Enabling extensions.worktreeConfig
// on a bare cache is not a substitute: the cache's core.bare=true then applies
// to every linked worktree that has no config.worktree of its own, and Git
// refuses to treat them as work trees.
//
// The include below matches one git dir exactly. A trailing slash would expand
// to /** and also match nested worktree git dirs. core.excludesFile replaces
// the user's global excludes file, so that file is copied ahead of the agent
// patterns.
package gitexclude

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	excludesFileName = "multica-excludes"
	snippetFileName  = "multica-excludes.config"
	managedMarker    = "# multica-agent-excludes:begin"
)

// Install makes patterns effective for worktreePath only. Later calls add
// patterns; they do not drop ones installed earlier on the same checkout.
func Install(ctx context.Context, worktreePath string, patterns []string) error {
	patterns, err := normalizePatterns(patterns)
	if err != nil {
		return err
	}
	if len(patterns) == 0 {
		return nil
	}

	gitDir, err := absoluteGitDir(ctx, worktreePath)
	if err != nil {
		return err
	}
	infoDir := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("create git info dir: %w", err)
	}
	excludesPath := filepath.Join(infoDir, excludesFileName)
	snippetPath := filepath.Join(infoDir, snippetFileName)

	baseline, err := baselineExcludes(ctx, worktreePath, excludesPath)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(excludesPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read worktree excludes: %w", err)
	}
	content := composeExcludes(baseline, mergePatterns(string(existing), patterns))
	if err := os.WriteFile(excludesPath, content, 0o644); err != nil {
		return fmt.Errorf("write worktree excludes: %w", err)
	}
	if err := os.Remove(snippetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace excludes config: %w", err)
	}
	if _, err := gitOutput(ctx, "config", "--file", snippetPath, "core.excludesFile", excludesPath); err != nil {
		return fmt.Errorf("write excludes config: %w", err)
	}
	// Key form is includeIf.gitdir:<exact git dir>.path — no slash before
	// ".path". Git stores that as [includeIf "gitdir:<exact git dir>"].
	// Skip the write when it is already correct: that command locks the
	// common config, and a reused checkout must not take the lock.
	key := "includeIf.gitdir:" + gitDir + ".path"
	if current, err := gitOutput(ctx, "-C", worktreePath, "config", "--get", key); err != nil || current != snippetPath {
		if _, err := gitOutput(ctx, "-C", worktreePath, "config", key, snippetPath); err != nil {
			return fmt.Errorf("set worktree excludes include: %w", err)
		}
	}

	probe := patterns[len(patterns)-1]
	out, err := gitCombined(ctx, "-C", worktreePath, "check-ignore", "--no-index", "-v", "--", probe)
	if err != nil {
		return fmt.Errorf("git did not apply agent excludes to %s: %s: %w", probe, strings.TrimSpace(out), err)
	}
	if !bytes.Contains([]byte(out), []byte(excludesPath)) {
		return fmt.Errorf("git ignored %s from an unexpected source: %s", probe, strings.TrimSpace(out))
	}
	return nil
}

func normalizePatterns(patterns []string) ([]string, error) {
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimRight(pattern, "\r")
		if pattern == "" {
			continue
		}
		if strings.ContainsAny(pattern, "\r\n") {
			return nil, fmt.Errorf("exclude pattern %q contains a newline", pattern)
		}
		out = append(out, pattern)
	}
	return out, nil
}

func absoluteGitDir(ctx context.Context, worktreePath string) (string, error) {
	out, err := gitOutput(ctx, "-C", worktreePath, "rev-parse", "--absolute-git-dir")
	if err == nil && out != "" {
		return out, nil
	}
	raw, fallbackErr := gitOutput(ctx, "-C", worktreePath, "rev-parse", "--git-dir")
	if fallbackErr != nil {
		if err != nil {
			return "", fmt.Errorf("resolve git dir: %w", err)
		}
		return "", fmt.Errorf("resolve git dir: %w", fallbackErr)
	}
	if filepath.IsAbs(raw) {
		return raw, nil
	}
	return filepath.Join(worktreePath, raw), nil
}

// baselineExcludes is the excludes file Git would use before this worktree's
// includeIf. core.excludesFile is a single path, not a list, so replacing it
// without this copy drops the user's global ignore rules inside the checkout.
func baselineExcludes(ctx context.Context, worktreePath, excludesPath string) (string, error) {
	common, err := gitOutput(ctx, "-C", worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("resolve git common dir: %w", err)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(worktreePath, common)
	}
	// --file does not follow includeIf, so a previous install does not show
	// up here and get copied back into itself.
	if local, ok, err := gitConfigFile(ctx, filepath.Join(common, "config"), "core.excludesFile"); err != nil {
		return "", err
	} else if ok && !samePath(local, excludesPath) {
		return readExcludes(local)
	}
	if global, ok, err := gitConfigArgs(ctx, []string{"--global"}, "core.excludesFile"); err != nil {
		return "", err
	} else if ok && !samePath(global, excludesPath) {
		return readExcludes(global)
	}
	if system, ok, err := gitConfigArgs(ctx, []string{"--system"}, "core.excludesFile"); err != nil {
		return "", err
	} else if ok && !samePath(system, excludesPath) {
		return readExcludes(system)
	}
	return readExcludes(defaultGitExcludesPath())
}

func defaultGitExcludesPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "git", "ignore")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "git", "ignore")
}

func readExcludes(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read existing excludes %s: %w", path, err)
	}
	return stripManaged(string(data)), nil
}

func stripManaged(content string) string {
	if i := strings.Index(content, managedMarker); i >= 0 {
		content = content[:i]
	}
	return strings.TrimRight(content, "\n")
}

func mergePatterns(existing string, patterns []string) []string {
	seen := make(map[string]struct{})
	var merged []string
	add := func(pattern string) {
		if _, ok := seen[pattern]; ok {
			return
		}
		seen[pattern] = struct{}{}
		merged = append(merged, pattern)
	}
	for _, pattern := range readManaged(existing) {
		add(pattern)
	}
	for _, pattern := range patterns {
		add(pattern)
	}
	return merged
}

func readManaged(content string) []string {
	i := strings.Index(content, managedMarker)
	if i < 0 {
		return nil
	}
	var patterns []string
	for _, line := range strings.Split(content[i+len(managedMarker):], "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

func composeExcludes(baseline string, patterns []string) []byte {
	var b strings.Builder
	if baseline != "" {
		b.WriteString(baseline)
		b.WriteByte('\n')
	}
	b.WriteString(managedMarker)
	b.WriteByte('\n')
	for _, pattern := range patterns {
		b.WriteString(pattern)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, aerr := filepath.Abs(a)
	bb, berr := filepath.Abs(b)
	if aerr != nil || berr != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func gitConfigFile(ctx context.Context, file, key string) (string, bool, error) {
	return gitConfigArgs(ctx, []string{"--file", file}, key)
}

func gitConfigArgs(ctx context.Context, prefix []string, key string) (string, bool, error) {
	args := append(append([]string{}, prefix...), "--path", "--get", key)
	out, err := gitCombined(ctx, append([]string{"config"}, args...)...)
	if err == nil {
		return strings.TrimSpace(out), true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("read git %s: %s: %w", key, strings.TrimSpace(out), err)
}

func gitOutput(ctx context.Context, args ...string) (string, error) {
	out, err := gitCombined(ctx, args...)
	if err != nil {
		trimmed := strings.TrimSpace(out)
		if trimmed == "" {
			return "", err
		}
		return "", fmt.Errorf("%w (%s)", err, trimmed)
	}
	return strings.TrimSpace(out), nil
}

func gitCombined(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
