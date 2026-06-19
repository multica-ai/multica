package dettools

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// runShell runs a command line through the system shell in workDir and returns
// the combined stdout+stderr and the process exit code. A spawn failure (or
// missing shell) returns exit code -1 alongside the error.
func runShell(ctx context.Context, workDir, commandline string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", commandline)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	return string(out), exitCode(err), err
}

// runCommand runs command with args directly (no shell) in workDir and returns
// the combined stdout+stderr and process exit code.
func runCommand(ctx context.Context, workDir, command string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	return string(out), exitCode(err), err
}

// exitCode extracts a process exit code from an exec error: 0 on success, the
// real code on a non-zero exit, and -1 when the process could not run at all.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// commandVersion reports whether command is on PATH and, if so, its first line
// of `command <args...>` output (typically the version banner). present=false
// means the binary was not found; an empty version with present=true means the
// binary exists but the probe produced no parseable output.
func commandVersion(ctx context.Context, command string, args ...string) (version string, present bool) {
	if _, err := exec.LookPath(command); err != nil {
		return "", false
	}
	out, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
	if err != nil {
		return "", true
	}
	return firstLine(string(out)), true
}

// firstLine returns the first non-empty trimmed line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// tail returns the last n bytes of s, prefixed with an ellipsis marker when
// truncated, so large command output stays bounded in tool results.
func tail(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if len(s) <= n {
		return s
	}
	return "...(truncated)...\n" + s[len(s)-n:]
}
