package daemon

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// MulticaCLIPathEnv is an optional launcher hint for the multica binary agents
// should find on PATH. Desktop or another supervisor may set it when the
// daemon's own executable path is not the right path to expose to tasks.
const MulticaCLIPathEnv = "MULTICA_CLI_PATH"

const codexSandboxUsersGroup = "CodexSandboxUsers"

var grantWindowsCodexSandboxUsersRX = grantWindowsCodexSandboxUsersRXImpl
var resolveAgentSelfExecutable = os.Executable

func agentCLIDirForTask(provider, envRoot string, logger *slog.Logger) string {
	return agentCLIDirForTaskForGOOS(provider, envRoot, runtime.GOOS, logger)
}

func agentCLIDirForTaskForGOOS(provider, envRoot, goos string, logger *slog.Logger) string {
	source := resolveAgentCLIPath(logger)
	if source == "" {
		return ""
	}
	if provider == "codex" && goos == "windows" && envRoot != "" {
		staged, err := stageTaskScopedAgentCLI(source, envRoot, goos, logger)
		if err != nil {
			if logger != nil {
				logger.Warn("agent CLI: task-scoped Windows copy failed; falling back to source path", "source", source, "error", err)
			}
			return filepath.Dir(source)
		}
		return filepath.Dir(staged)
	}
	return filepath.Dir(source)
}

func resolveAgentCLIPath(logger *slog.Logger) string {
	if hint := strings.TrimSpace(os.Getenv(MulticaCLIPathEnv)); hint != "" {
		if info, err := os.Stat(hint); err == nil && !info.IsDir() {
			return hint
		} else if logger != nil {
			logger.Warn("agent CLI: ignoring invalid MULTICA_CLI_PATH", "path", hint, "error", err)
		}
	}
	selfBin, err := resolveAgentSelfExecutable()
	if err != nil {
		if logger != nil {
			logger.Warn("agent CLI: could not resolve daemon executable", "error", err)
		}
		return ""
	}
	return selfBin
}

func stageTaskScopedAgentCLI(source, envRoot, goos string, logger *slog.Logger) (string, error) {
	if goos != "windows" {
		return source, nil
	}
	destDir := filepath.Join(envRoot, "bin")
	dest := filepath.Join(destDir, "multica.exe")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir task cli dir: %w", err)
	}
	tmp := filepath.Join(destDir, fmt.Sprintf(".multica.exe.tmp-%d", os.Getpid()))
	if err := copyFile(source, tmp); err != nil {
		return "", fmt.Errorf("copy task cli: %w", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("chmod task cli: %w", err)
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("replace task cli: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("install task cli: %w", err)
	}
	// Codex's elevated Windows sandbox runs commands as users in this local
	// group. Grant only Read & Execute to the task-local CLI directory and file;
	// failures are non-fatal so ordinary Windows Codex runs without that group
	// remain compatible.
	if err := grantWindowsCodexSandboxUsersRX(destDir, true); err != nil && logger != nil {
		logger.Warn("agent CLI: could not grant Codex sandbox group access to task cli dir", "path", destDir, "error", err)
	}
	if err := grantWindowsCodexSandboxUsersRX(dest, false); err != nil && logger != nil {
		logger.Warn("agent CLI: could not grant Codex sandbox group access to task cli", "path", dest, "error", err)
	}
	return dest, nil
}

func copyFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func grantWindowsCodexSandboxUsersRXImpl(path string, inheritable bool) error {
	grant := codexSandboxUsersGroup + ":(RX)"
	if inheritable {
		grant = codexSandboxUsersGroup + ":(OI)(CI)(RX)"
	}
	return exec.Command("icacls", path, "/grant", grant).Run()
}
