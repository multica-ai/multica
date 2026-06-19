package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// piInstallTimeout bounds the optional adapter-install command.
const piInstallTimeout = 60 * time.Second

// preparePiToolPlane wires the deterministic tool plane for Pi, which has no
// native MCP and reaches the plane through pi-mcp-adapter. The adapter
// auto-discovers a project-local `.pi/mcp.json` relative to the agent's cwd
// (the task work dir), so the daemon simply writes the merged MCP config there —
// per-task by construction, with no shared global file to race. Validated
// against github.com/nicobailon/pi-mcp-adapter (config schema {settings,
// mcpServers}; project-local `.pi/mcp.json` is the highest-precedence override).
//
// The whole path is opt-in (PiAdapterEnabled) and fail-open: any problem logs
// and leaves Pi running without the tool plane. It returns a cleanup func the
// caller must defer — a no-op except when the work dir is the user's own
// repository (local_directory), where it restores the original `.pi/mcp.json`
// so the daemon never leaves state in a user-owned checkout.
func (d *Daemon) preparePiToolPlane(provider, workDir string, localDirectory bool, agentMcpConfig, runtimeConfig json.RawMessage, steps []DeterministicToolData, agentEnv map[string]string, logger *slog.Logger) func() {
	noop := func() {}
	if !d.cfg.DetTools.Enabled || !d.cfg.DetTools.PiAdapterEnabled || provider != "pi" {
		return noop
	}
	if workDir == "" {
		logger.Warn("dettools(pi): no work dir; skipping adapter setup")
		return noop
	}

	effective := computeEffectiveAllowed(d.cfg.DetTools, runtimeConfig)
	profile := parseAgentDetToolsProfile(runtimeConfig)
	allowedSteps := filterStepsByProfile(steps, profile, d.cfg.DetTools.DeniedTools)
	if len(effective) == 0 && len(allowedSteps) == 0 {
		logger.Info("dettools(pi): no tools enabled for this agent after policy; skipping adapter setup")
		return noop
	}

	selfBin, err := os.Executable()
	if err != nil {
		logger.Warn("dettools(pi): cannot resolve daemon binary; skipping", "error", err)
		return noop
	}
	if resolved, rerr := filepath.EvalSymlinks(selfBin); rerr == nil {
		selfBin = resolved
	}

	relPath := d.cfg.DetTools.PiConfigRelPath
	if relPath == "" {
		relPath = DefaultPiConfigRelPath
	}
	cfgPath := filepath.Join(workDir, relPath)

	// Merge into any existing project config so a user's own servers and the
	// adapter's top-level "settings" survive. Absent file → start from the
	// agent's mcp_config (usually empty).
	original, hadOriginal := readFileMaybe(cfgPath)
	base := agentMcpConfig
	if hadOriginal {
		base = original
	}
	stepsFile := ""
	if len(allowedSteps) > 0 {
		if f, werr := writeStepsFile(workDir, allowedSteps); werr != nil {
			logger.Warn("dettools(pi): failed to write authored steps; serving built-ins only", "error", werr)
		} else {
			stepsFile = f
		}
	}

	merged, err := buildEffectiveMcpConfig(base, selfBin, workDir, stepsFile, d.cfg.DetTools, effective)
	if err != nil {
		logger.Warn("dettools(pi): build adapter config failed; launching without tool plane", "error", err)
		return noop
	}

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		logger.Warn("dettools(pi): create config dir failed; launching without tool plane", "error", err)
		return noop
	}
	if err := os.WriteFile(cfgPath, merged, 0o600); err != nil {
		logger.Warn("dettools(pi): write config failed; launching without tool plane", "error", err)
		return noop
	}

	// Optional: ensure the adapter is installed. Default is no-op — installing
	// requires a Pi restart to take effect, so it can't help the current run;
	// operators install pi-mcp-adapter once and set MULTICA_DETTOOLS_PI_INSTALL_CMD
	// only if they want the daemon to attempt it.
	if cmd := strings.TrimSpace(d.cfg.DetTools.PiInstallCmd); cmd != "" {
		runPiInstall(cmd, agentEnv, logger)
	}

	logger.Info("dettools(pi): wrote project-local adapter config",
		"path", cfgPath,
		"tools", effective,
		"merged_into_existing", hadOriginal,
	)

	if !localDirectory {
		// Ephemeral work dir — GC'd after the task, nothing to restore.
		return noop
	}
	return func() {
		// Never leave the authored-steps file in a user-owned checkout.
		if stepsFile != "" {
			_ = os.Remove(stepsFile)
			_ = os.Remove(filepath.Dir(stepsFile)) // .multica/dettools, if now empty
		}
		if hadOriginal {
			if err := os.WriteFile(cfgPath, original, 0o600); err != nil {
				logger.Warn("dettools(pi): restore original config failed", "path", cfgPath, "error", err)
			}
			return
		}
		_ = os.Remove(cfgPath)
		// Remove the .pi dir only if we created it and it's now empty (Remove
		// fails on a non-empty dir, so a user's existing .pi/ is left alone).
		_ = os.Remove(filepath.Dir(cfgPath))
	}
}

// readFileMaybe returns the file contents and whether it existed. Read errors
// other than not-exist are treated as "no original" (the file is overwritten).
func readFileMaybe(path string) ([]byte, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// runPiInstall runs the configured adapter-install command best-effort with the
// agent's environment, bounded by piInstallTimeout. Failure is logged, not
// fatal — the tool plane simply stays unavailable for Pi (fail-open).
func runPiInstall(command string, agentEnv map[string]string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), piInstallTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = mergedEnv(agentEnv)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warn("dettools(pi): adapter install command failed; continuing fail-open",
			"error", err, "output", strings.TrimSpace(string(out)))
		return
	}
	logger.Info("dettools(pi): adapter install command ran", "output", strings.TrimSpace(string(out)))
}

// mergedEnv returns os.Environ overlaid with extra (extra wins on conflict).
func mergedEnv(extra map[string]string) []string {
	skip := make(map[string]bool, len(extra))
	for k := range extra {
		skip[k] = true
	}
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 && skip[kv[:i]] {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}
