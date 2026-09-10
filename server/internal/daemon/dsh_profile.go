package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The DSH backend drives `dsh --profile multica --stdio`, and that profile is
// what supplies the protocol: DSH ships no machine-drivable mode of its own.
// `--profile headless` answers one task and exits, and ACP — the one protocol
// DSH does implement — is itself a profile rather than a built-in. Every other
// built-in drives a protocol its CLI already has (`claude -p --output-format
// stream-json`, `codex app-server`, `kimi acp`), which is why dsh is the only
// provider whose integration depends on the user installing something into the
// agent's own home first.
//
// These knobs let the daemon close that gap instead of only reporting it.
const (
	// dshProfileBundleEnv names the DSH-side bundle to install into the
	// `multica` profile when a dsh CLI resolves without one. It takes a
	// comma-separated list of candidates tried in order, each of which may be
	// anything `dsh plugin --profile multica add` accepts: an npm spec, a
	// directory, or a packed tarball.
	//
	// Unset is the default and means "do not touch the user's DSH
	// installation". Installing a bundle into another product's home is a
	// side effect the operator has to ask for — and there is nothing to point
	// it at by default either, because Multica's own DSH bridge is not
	// published to npm yet (multica#6936). Setting this is what turns the
	// reported drop into a self-healing one.
	dshProfileBundleEnv = "MULTICA_DSH_PROFILE_BUNDLE"

	// dshPluginPathEnv overrides where `dsh plugin` finds pnpm. It exists
	// because DSH Desktop ships pnpm inside its own runtime-commands
	// directory and injects that directory into the PATH of the processes it
	// spawns itself. A daemon started by Multica's desktop app, by launchd or
	// from a terminal is not one of them, so the install fails with "pnpm not
	// found on PATH" on exactly the machines this feature is for.
	dshPluginPathEnv = "MULTICA_DSH_PLUGIN_PATH"

	// dshMulticaProfileName is the profile the backend launches. Kept beside
	// the install command so the two cannot drift.
	dshMulticaProfileName = "multica"
)

// dshProvisionTimeout bounds the whole install. It is a package manager
// talking to a registry, so it is generous next to the CLI probes.
const dshProvisionTimeout = 3 * time.Minute

// dshMulticaProfilePresent reports whether the `multica` profile is installed.
//
// A stat, deliberately. This is the cheap change detector the discovery loop
// runs on every tick, and both things that change the answer — `dsh plugin
// --profile multica add` and a user removing the directory — create or remove
// exactly this file. DSH decides a profile exists the same way: dsh-app-boot's
// loadProfile falls back to a built-in template, or fails, when the manifest is
// missing.
//
// The authoritative question — "does --probe actually succeed?" — costs booting
// a whole DSH process, so it stays in the probe round this signal forces.
func dshMulticaProfilePresent() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	// DSH_HOME is DSH's own override, and the profile store lives under it
	// either way. Same resolution local_skills.go uses to find ~/.dsh/skills.
	dshHome := strings.TrimSpace(os.Getenv("DSH_HOME"))
	if dshHome == "" {
		dshHome = filepath.Join(home, ".dsh")
	}
	_, err = os.Stat(filepath.Join(dshHome, "profiles", dshMulticaProfileName, "package.json"))
	return err == nil
}

// dshProfileBundleSpecs returns the configured install candidates, in order,
// or nil when the operator has not opted in.
func dshProfileBundleSpecs() []string {
	raw := strings.TrimSpace(os.Getenv(dshProfileBundleEnv))
	if raw == "" {
		return nil
	}
	var specs []string
	for _, spec := range strings.Split(raw, ",") {
		if spec = strings.TrimSpace(spec); spec != "" {
			specs = append(specs, spec)
		}
	}
	return specs
}

// dshPluginPathDirs returns the directories that may hold the pnpm `dsh plugin`
// forwards to, most specific first. Only directories that exist are returned:
// prepending a miss to PATH buys nothing and makes the environment harder to
// read in a failure report.
func dshPluginPathDirs() []string {
	var candidates []string
	if override := strings.TrimSpace(os.Getenv(dshPluginPathEnv)); override != "" {
		candidates = append(candidates, override)
	} else if home, err := os.UserHomeDir(); err == nil {
		// DSH Desktop keeps its private pnpm shim here. macOS-only because the
		// desktop app ships only for macOS today; other platforms (and
		// non-standard installs) are what dshPluginPathEnv overrides.
		candidates = append(candidates, filepath.Join(home,
			"Library", "Application Support", "DSH Desktop", "runtime-commands", "bin"))
	}
	var dirs []string
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// dshPluginEnv copies the daemon's environment with dshPluginPathDirs prepended
// to PATH, so `dsh plugin` resolves the pnpm DSH ships when the operator has no
// pnpm of their own.
func dshPluginEnv() []string {
	env := os.Environ()
	dirs := dshPluginPathDirs()
	if len(dirs) == 0 {
		return env
	}
	prefix := strings.Join(dirs, string(os.PathListSeparator))
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + prefix + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
			return env
		}
	}
	return append(env, "PATH="+prefix)
}

// provisionDshMulticaProfile installs the configured runtime bundle into the
// `multica` profile, so a later discovery round finds a usable dsh. Candidates
// are tried in order and the first success wins; every failure is reported with
// the CLI's own output, which is where "pnpm not found on PATH" and registry
// errors actually surface.
//
// Returns nil when nothing is configured — the caller treats provisioning as
// best-effort either way, and the missing-profile verdict stands until a probe
// succeeds.
func provisionDshMulticaProfile(ctx context.Context, dshPath string, logger *slog.Logger) error {
	specs := dshProfileBundleSpecs()
	if len(specs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, dshProvisionTimeout)
	defer cancel()

	var lastErr error
	for _, spec := range specs {
		cmd := exec.CommandContext(ctx, dshPath, "plugin", "--profile", dshMulticaProfileName, "add", spec)
		cmd.Env = dshPluginEnv()
		cmd.WaitDelay = 5 * time.Second
		output, err := cmd.CombinedOutput()
		if err == nil {
			logger.Info("installed the DSH runtime profile",
				"bundle", spec, "path", dshPath, "profile", dshMulticaProfileName)
			return nil
		}
		lastErr = fmt.Errorf("dsh plugin --profile %s add %s: %w: %s",
			dshMulticaProfileName, spec, err, strings.TrimSpace(string(output)))
		logger.Warn("DSH runtime profile install failed", "bundle", spec, "error", lastErr)
	}
	return lastErr
}

// startDshProfileProvision kicks off the one-time automatic install of the DSH
// runtime profile. It reports whether this call was the one that started it, so
// the caller can say so in the drop reason.
//
// Once per daemon lifetime, not once per round: the work is a network install
// that mutates the user's DSH home, and a registry or network failure would
// otherwise be retried every discovery interval forever. A restart is the
// retry.
func (d *Daemon) startDshProfileProvision(dshPath string) bool {
	if len(dshProfileBundleSpecs()) == 0 {
		return false
	}
	started := false
	d.dshProvisionOnce.Do(func() {
		started = true
		go func() {
			if err := provisionDshMulticaProfile(context.Background(), dshPath, d.logger); err != nil {
				d.logger.Warn("automatic DSH runtime profile install failed; dsh stays unregistered until the profile exists",
					"path", dshPath, "error", err)
				return
			}
			d.logger.Info("DSH runtime profile installed; re-probing to bring dsh online")
			// Re-probe now rather than at the next scheduled round. The
			// discovery loop backs off while a provider cannot register, and
			// dsh counts as "missing a runtime" for as long as the profile is
			// absent — so the attempt this replaces can be
			// agentConvergeMaxBackoff away, which is indistinguishable from
			// "it does not work" to anyone watching the runtime list.
			d.kickAgentDiscovery()
		}()
	})
	return started
}
