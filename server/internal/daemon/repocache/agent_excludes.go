package repocache

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// agentGitExcludePatterns are runtime files that an agent may create inside a
// repository checkout. Git's per-worktree info/exclude is not read for linked
// worktrees, while the common info/exclude would hide files in sibling tasks.
var agentGitExcludePatterns = []string{
	".agent_context",
	"CLAUDE.md",
	"AGENTS.md",
	".claude",
	".opencode",
	".codeartsdoer",
	".deveco",
	"CODEBUDDY.md",
	".codebuddy",
	".pi",
	".omp",
}

// ConfigureAgentGitExcludes gives the agent and its Git subprocesses a
// task-scoped global excludes file. A checkout may happen after the agent has
// started, so the Git setting must be present in the agent environment before
// its first command, rather than attached to an already-created worktree.
func ConfigureAgentGitExcludes(taskTempDir string, agentEnv map[string]string) (retErr error) {
	if agentEnv == nil {
		return errors.New("agent environment is required")
	}
	if !filepath.IsAbs(taskTempDir) {
		return errors.New("task temp directory must be absolute")
	}
	globalExcludes, err := activeGlobalExcludes(agentEnv)
	if err != nil {
		return fmt.Errorf("resolve existing Git excludes: %w", err)
	}

	f, err := os.CreateTemp(taskTempDir, "git-agent-excludes-*")
	if err != nil {
		return fmt.Errorf("create task Git excludes: %w", err)
	}
	previousParameters, hadParameters := agentEnv["GIT_CONFIG_PARAMETERS"]
	defer func() {
		if retErr != nil {
			_ = os.Remove(f.Name())
			if hadParameters {
				agentEnv["GIT_CONFIG_PARAMETERS"] = previousParameters
			} else {
				delete(agentEnv, "GIT_CONFIG_PARAMETERS")
			}
		}
	}()
	var content strings.Builder
	content.WriteString(globalExcludes)
	if content.Len() > 0 && !strings.HasSuffix(content.String(), "\n") {
		content.WriteByte('\n')
	}
	for _, pattern := range agentGitExcludePatterns {
		content.WriteString("/")
		content.WriteString(pattern)
		content.WriteByte('\n')
	}
	if _, err = f.WriteString(content.String()); err != nil {
		_ = f.Close()
		return fmt.Errorf("write task Git excludes: %w", err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close task Git excludes: %w", err)
	}

	parameters := agentEnv["GIT_CONFIG_PARAMETERS"]
	if parameters == "" {
		parameters = os.Getenv("GIT_CONFIG_PARAMETERS")
	}
	if parameters != "" {
		parameters += " "
	}
	parameters += quoteGitConfigParameter("core.excludesFile=" + f.Name())
	agentEnv["GIT_CONFIG_PARAMETERS"] = parameters
	verify := exec.Command("git", "config", "--path", "--get", "core.excludesFile")
	verify.Dir = filepath.VolumeName(os.TempDir()) + string(os.PathSeparator)
	verify.Env = append(os.Environ(), envEntries(agentEnv)...)
	resolved, verifyErr := verify.Output()
	if verifyErr != nil {
		return fmt.Errorf("verify task Git excludes: %w", verifyErr)
	}
	if strings.TrimSpace(string(resolved)) != f.Name() {
		return errors.New("task Git excludes were overridden by another Git setting")
	}
	return nil
}

func activeGlobalExcludes(agentEnv map[string]string) (string, error) {
	cmd := exec.Command("git", "config", "--path", "--get", "core.excludesFile")
	// Git must not pick up the local config of whichever repository happened to
	// contain the daemon's current directory or task scratch directory.
	cmd.Dir = filepath.VolumeName(os.TempDir()) + string(os.PathSeparator)
	cmd.Env = append(os.Environ(), envEntries(agentEnv)...)
	output, err := cmd.Output()
	var excludePath string
	if err == nil {
		excludePath = strings.TrimSpace(string(output))
	} else {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return "", fmt.Errorf("read Git excludes configuration: %w", err)
		}
		xdg := agentEnv["XDG_CONFIG_HOME"]
		if xdg == "" {
			xdg = os.Getenv("XDG_CONFIG_HOME")
		}
		if xdg == "" {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return "", fmt.Errorf("resolve Git config home: %w", homeErr)
			}
			xdg = filepath.Join(home, ".config")
		}
		excludePath = filepath.Join(xdg, "git", "ignore")
	}
	if excludePath == "" {
		return "", nil
	}
	if !filepath.IsAbs(excludePath) {
		return "", fmt.Errorf("relative core.excludesFile %q cannot be preserved across checkouts", excludePath)
	}
	contents, err := os.ReadFile(excludePath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read existing Git excludes: %w", err)
	}
	return string(contents), nil
}

func quoteGitConfigParameter(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func envEntries(values map[string]string) []string {
	entries := make([]string, 0, len(values))
	for key, value := range values {
		entries = append(entries, key+"="+value)
	}
	return entries
}
