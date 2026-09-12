package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const claudePluginInventoryMaxBytes = 4 << 20

// ClaudeConfigDirectory resolves settings in the same child environment used
// by the native query and execution, including per-agent environment overrides.
func ClaudeConfigDirectory(cfg Config, cwd string) (string, error) {
	values := make(map[string]string)
	for _, entry := range buildEnv(cfg.Env) {
		key, value, _ := strings.Cut(entry, "=")
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		values[key] = value
	}
	dir := values["CLAUDE_CONFIG_DIR"]
	if dir == "" {
		home := values["HOME"]
		if runtime.GOOS == "windows" {
			home = values["USERPROFILE"]
		}
		if home == "" {
			return "", fmt.Errorf("cannot resolve Claude configuration directory without a runtime home")
		}
		dir = filepath.Join(home, ".claude")
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	return filepath.Abs(dir)
}

// ClaudePluginState is the native CLI's installed-plugin inventory. In
// particular, Enabled reflects project/local settings, not just user settings.
// An absent install is not proof that a stale enabledPlugins entry cannot load;
// callers must check orphaned configuration before treating absence as removal.
type ClaudePluginState struct {
	ID          string
	Scope       string
	Enabled     bool
	InstallPath string
	Errors      []string
}

// QueryClaudePlugins asks the same runtime command, in the same cwd and
// environment as task execution, to resolve plugin enablement. It does not
// start a conversation, execute plugin hooks, or make a model request.
func QueryClaudePlugins(ctx context.Context, cfg Config, opts ExecOptions) ([]ClaudePluginState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateClaudePluginPolicyArgs(cfg.LaunchPrefix, opts.ExtraArgs, opts.CustomArgs); err != nil {
		return nil, err
	}
	if cfg.ExecutablePath == "" {
		return nil, fmt.Errorf("Claude plugin inventory requires the resolved runtime executable")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	cfg.LaunchPrefix = filterLaunchPrefix(cfg.LaunchPrefix, "claude", cfg.Logger)
	args := []string{}
	if opts.ClaudeSettingsPath != "" {
		args = append(args, "--settings", opts.ClaudeSettingsPath)
	}
	args = append(args, "plugin", "list", "--json")
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := cfg.commandAt(cfg.ExecutablePath).exec(probeCtx, args...)
	cmd.Dir = opts.Cwd
	cmd.Env = buildEnv(cfg.Env)
	stdout := &claudePluginInventoryBuffer{cancel: cancel}
	stderr := &tailBuffer{max: probeStderrSampleBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := runOwned(cmd, cfg.Logger)
	if stdout.overflow {
		return nil, fmt.Errorf("Claude plugin inventory exceeds 4 MiB")
	}
	if probeCtx.Err() != nil {
		return nil, fmt.Errorf("query Claude plugin inventory: %w", probeCtx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("query Claude plugin inventory: %w: %s", err, strings.TrimSpace(string(stderr.Bytes())))
	}
	return parseClaudePluginInventory(stdout.Bytes())
}

func parseClaudePluginInventory(raw []byte) ([]ClaudePluginState, error) {
	var entries []struct {
		ID          string   `json:"id"`
		Scope       string   `json:"scope"`
		Enabled     *bool    `json:"enabled"`
		InstallPath string   `json:"installPath"`
		Errors      []string `json:"errors"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode Claude plugin inventory: %w", err)
	}
	if entries == nil {
		return nil, fmt.Errorf("Claude plugin inventory must be an array")
	}
	plugins := make([]ClaudePluginState, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" || entry.Enabled == nil {
			return nil, fmt.Errorf("Claude plugin inventory entry is missing its identity or enablement")
		}
		plugins = append(plugins, ClaudePluginState{
			ID: entry.ID, Scope: entry.Scope, Enabled: *entry.Enabled,
			InstallPath: entry.InstallPath, Errors: entry.Errors,
		})
	}
	return plugins, nil
}

type claudePluginInventoryBuffer struct {
	bytes.Buffer
	cancel   context.CancelFunc
	overflow bool
}

func (b *claudePluginInventoryBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := claudePluginInventoryMaxBytes - b.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.overflow = true
		b.cancel()
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
