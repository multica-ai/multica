package execenv

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Directories to symlink from the shared ~/.codex/ into the per-task CODEX_HOME.
// The shared directory is created if it doesn't exist, ensuring Codex session
// logs are always written to the global home where users can find them.
var codexSymlinkedDirs = []string{
	"sessions",
}

// Files to symlink from the shared ~/.codex/ into the per-task CODEX_HOME.
// Symlinks share state (e.g. auth tokens) so changes propagate automatically.
var codexSymlinkedFiles = []string{
	"auth.json",
}

// Files to copy from the shared ~/.codex/ into the per-task CODEX_HOME.
// Copies are isolated — changes don't affect the shared home.
var codexCopiedFiles = []string{
	"config.json",
	"config.toml",
	"instructions.md",
}

// CodexHomeOptions carries optional inputs for prepareCodexHomeWithOpts that
// affect the generated per-task config.toml.
type CodexHomeOptions struct {
	// CodexVersion is the detected Codex CLI version (e.g. "0.121.0"). Empty
	// means unknown; on macOS, unknown is treated as "probably broken" so the
	// daemon falls back to danger-full-access for network access. See
	// codex_sandbox.go for details.
	CodexVersion string
	// GOOS overrides the target platform when deciding the sandbox policy.
	// Empty means use runtime.GOOS. Primarily exists so tests can exercise
	// both macOS and Linux paths deterministically.
	GOOS string
	// McpConfig is the agent's MCP server config JSON. When non-empty,
	// [mcp_servers.*] sections are written into config.toml.
	McpConfig json.RawMessage
}

// prepareCodexHome is a thin wrapper around prepareCodexHomeWithOpts kept for
// tests that don't care about platform-aware sandbox configuration. It
// assumes a Linux-like environment where workspace-write + network_access
// works correctly.
func prepareCodexHome(codexHome string, logger *slog.Logger) error {
	return prepareCodexHomeWithOpts(codexHome, CodexHomeOptions{GOOS: "linux"}, logger)
}

// prepareCodexHomeWithOpts creates a per-task CODEX_HOME directory and seeds
// it with config from the shared ~/.codex/ home. Auth is symlinked (shared),
// config files are copied (isolated). The per-task config.toml gets a
// daemon-managed sandbox block picked by codexSandboxPolicyFor.
func prepareCodexHomeWithOpts(codexHome string, opts CodexHomeOptions, logger *slog.Logger) error {
	sharedHome := resolveSharedCodexHome()

	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		return fmt.Errorf("create codex-home dir: %w", err)
	}

	// Symlink shared directories (sessions) so logs stay in the global home.
	for _, name := range codexSymlinkedDirs {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := ensureDirSymlink(src, dst); err != nil {
			logger.Warn("execenv: codex-home dir symlink failed", "dir", name, "error", err)
		}
	}

	// Symlink shared files (auth).
	for _, name := range codexSymlinkedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := ensureSymlink(src, dst); err != nil {
			logger.Warn("execenv: codex-home symlink failed", "file", name, "error", err)
		}
	}

	// Copy config files (isolated per task).
	for _, name := range codexCopiedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := copyFileIfExists(src, dst); err != nil {
			logger.Warn("execenv: codex-home copy failed", "file", name, "error", err)
		}
	}

	// Write a daemon-managed sandbox block into config.toml. On macOS we may
	// need to fall back to danger-full-access because of openai/codex#10390;
	// see codex_sandbox.go for the full rationale.
	policy := codexSandboxPolicyFor(opts.GOOS, opts.CodexVersion)
	if err := ensureCodexSandboxConfig(filepath.Join(codexHome, "config.toml"), policy, opts.CodexVersion, logger); err != nil {
		logger.Warn("execenv: codex-home ensure sandbox config failed", "error", err)
	}

	// Write agent MCP servers into config.toml so Codex discovers them.
	if len(opts.McpConfig) > 0 {
		if err := writeMcpServersToConfig(filepath.Join(codexHome, "config.toml"), opts.McpConfig); err != nil {
			logger.Warn("execenv: codex-home write mcp servers failed", "error", err)
		}
	}

	return nil
}

// resolveSharedCodexHome returns the path to the user's shared Codex home.
// Checks $CODEX_HOME first, falls back to ~/.codex.
func resolveSharedCodexHome() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		abs, err := filepath.Abs(v)
		if err == nil {
			return abs
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".codex") // last resort fallback
	}
	return filepath.Join(home, ".codex")
}

// ensureDirSymlink creates a symlink dst → src for a directory.
// Unlike ensureSymlink, it creates the source directory if it doesn't exist,
// so Codex can write to it immediately.
func ensureDirSymlink(src, dst string) error {
	if err := os.MkdirAll(src, 0o755); err != nil {
		return fmt.Errorf("create shared dir %s: %w", src, err)
	}

	// Check if dst already exists.
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(dst)
			if err == nil && target == src {
				return nil // already correct
			}
			os.Remove(dst)
		} else {
			// Regular file/dir exists — don't overwrite.
			return nil
		}
	}

	return createDirLink(src, dst)
}

// ensureSymlink creates a symlink dst → src. If src doesn't exist, it's a no-op.
// If dst already exists as a correct symlink, it's a no-op. If dst is a broken
// symlink, it's replaced.
func ensureSymlink(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // source doesn't exist — skip
	}

	// Check if dst already exists.
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			// It's a symlink — check if it points to the right place.
			target, err := os.Readlink(dst)
			if err == nil && target == src {
				return nil // already correct
			}
			// Wrong target — remove and recreate.
			os.Remove(dst)
		} else {
			// Regular file exists — don't overwrite.
			return nil
		}
	}

	return createFileLink(src, dst)
}

// (The daemon used to write a minimal inline config here; the authoritative
// sandbox/network directives now live in a managed block rendered by
// codex_sandbox.go's ensureCodexSandboxConfig so they can be updated
// idempotently without touching user-managed keys.)


// copyFileIfExists copies src to dst. If src doesn't exist, it's a no-op.
// If dst already exists, it's not overwritten.
func copyFileIfExists(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}

	// Don't overwrite existing file.
	if _, err := os.Stat(dst); err == nil {
		return nil
	}

	return copyFile(src, dst)
}

// copyFile copies src to dst unconditionally.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}
	return nil
}

// writeMcpServersToConfig writes [mcp_servers.*] TOML sections to config.toml,
// replacing any existing mcp_servers entries so updates from the platform take effect.
func writeMcpServersToConfig(configPath string, mcpConfig json.RawMessage) error {
	var parsed struct {
		McpServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpConfig, &parsed); err != nil || len(parsed.McpServers) == 0 {
		return nil
	}

	// Strip any existing [mcp_servers.*] blocks from the file.
	existing, _ := os.ReadFile(configPath)
	base := stripMcpServersFromToml(string(existing))

	// Build new mcp_servers sections.
	var sb strings.Builder
	sb.WriteString(strings.TrimRight(base, "\n"))
	for name, srv := range parsed.McpServers {
		sb.WriteString("\n\n[mcp_servers." + fmt.Sprintf("%q", name) + "]\n")
		sb.WriteString("command = " + fmt.Sprintf("%q", srv.Command) + "\n")
		if len(srv.Args) > 0 {
			sb.WriteString("args = [")
			for i, a := range srv.Args {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%q", a))
			}
			sb.WriteString("]\n")
		}
		if len(srv.Env) > 0 {
			sb.WriteString("env = { ")
			first := true
			for k, v := range srv.Env {
				if !first {
					sb.WriteString(", ")
				}
				sb.WriteString(k + " = " + fmt.Sprintf("%q", v))
				first = false
			}
			sb.WriteString(" }\n")
		}
	}

	return os.WriteFile(configPath, []byte(sb.String()), 0o644)
}

// stripMcpServersFromToml removes all [mcp_servers.*] table sections from a TOML string.
func stripMcpServersFromToml(content string) string {
	var out []string
	inMcp := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "[mcp_servers.") {
			inMcp = true
			continue
		}
		if inMcp && strings.HasPrefix(line, "[") {
			inMcp = false
		}
		if !inMcp {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}