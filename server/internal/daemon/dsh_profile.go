package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/redact"
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
// forwards to, newest first. Only directories that exist are returned:
// prepending a miss to PATH buys nothing and makes the environment harder to
// read in a failure report.
//
// DSH Desktop keeps its private pnpm shim under runtime-commands in its own
// per-user data directory, on both platforms — the daemon inherits no path to
// it, and without this the install fails with "pnpm not found on PATH" on
// exactly the machines the automatic install is for.
//
// The layout is not the same on both, which is why this shares the enumerator
// with CLI-shim discovery rather than composing a path: macOS 2.0.5 keeps a
// flat runtime-commands/bin, while a Windows install keeps
// runtime-commands/generations/<id>/bin. Both shapes, and both levels, are
// searched, so neither is a version this daemon silently stops supporting.
func dshPluginPathDirs() []string {
	// An explicit override is the whole answer: an operator who pinned a
	// directory that does not exist should get "pnpm not found", not a silent
	// fallback to a bundled one they were trying to bypass.
	if override := strings.TrimSpace(os.Getenv(dshPluginPathEnv)); override != "" {
		if isExistingDir(override) {
			return []string{override}
		}
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	appData := dshDesktopAppDataDir(runtime.GOOS, os.Getenv, home)
	if appData == "" {
		return nil
	}
	root := filepath.Join(appData, "runtime-commands")
	return append(dshDesktopBinDirs(filepath.Join(root, "generations")), dshDesktopBinDirs(root)...)
}

// dshPluginEnv copies the daemon's environment with dshPluginPathDirs prepended
// to PATH, so `dsh plugin` resolves the pnpm DSH ships when the operator has no
// pnpm of their own.
//
// The PATH entry is matched case-insensitively, which is not cosmetic on
// Windows: the variable is conventionally spelled `Path`, so an exact `PATH=`
// match misses it, and the appended entry then wins under Go's
// case-insensitive de-duplication of the child environment — leaving the
// package manager with only the plugin directory and none of the system path.
// The original spelling is preserved rather than normalized so the merge cannot
// itself introduce a second entry.
func dshPluginEnv() []string {
	return mergeDshPluginPath(os.Environ(), dshPluginPathDirs())
}

// mergeDshPluginPath prepends dirs to whichever PATH-like entry env already has,
// preserving that entry's own spelling, or appends one when there is none.
//
// Split out from the environment read so the merge is testable with the exact
// shape Windows hands the daemon: matching is case-insensitive because the
// variable is conventionally spelled `Path` there, and an exact `PATH=` match
// misses it — after which the appended entry wins under Go's case-insensitive
// de-duplication of the child environment, leaving the package manager with only
// the plugin directory and none of the system path.
func mergeDshPluginPath(env, dirs []string) []string {
	if len(dirs) == 0 {
		return env
	}
	prefix := strings.Join(dirs, string(os.PathListSeparator))
	for i, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 || !strings.EqualFold(kv[:eq], "PATH") {
			continue
		}
		env[i] = kv[:eq+1] + prefix + string(os.PathListSeparator) + kv[eq+1:]
		return env
	}
	return append(env, "PATH="+prefix)
}

// dshProvisionOutputBytes bounds the package-manager output kept in a log line.
// pnpm prints a tree plus a summary and the useful part is the tail, so the tail
// is what survives.
const dshProvisionOutputBytes = 2048

// dshProvisionURLCredentials matches a URL carrying credentials in its
// userinfo. The scheme and host survive — which registry refused the request is
// what an operator reads, and is not the secret — and userinfo requires BOTH a
// colon and an @ before the next path separator, so "https://host:8443/p" and
// "ssh://git@host/p" are left alone.
//
// Local to provisioning rather than added to pkg/redact's shared pattern list.
// redact.Text runs over every agent's task messages, comments and error text, so
// a rule added there changes output for every provider; the case that needs it
// here is one package manager's error text, and widening a shared filter is its
// own change with its own blast radius.
var dshProvisionURLCredentials = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.\-]*)://[^/?#\s:@]+:[^/?#\s@]+@`)

// dshProvisionOutput renders captured output for a log line. Registry errors
// routinely echo the request URL back, so URL credentials are scrubbed and the
// shared redactor still runs for the token shapes it knows, before the text is
// bounded.
func dshProvisionOutput(raw []byte) string {
	text := dshProvisionURLCredentials.ReplaceAllString(
		strings.TrimSpace(string(raw)), "$1://[REDACTED URL CREDENTIALS]@")
	text = redact.Text(text)
	if len(text) <= dshProvisionOutputBytes {
		return text
	}
	// Cut on a rune boundary. Package-manager output is full of box drawing and
	// check marks, and non-ASCII paths are ordinary on the machines this runs
	// on, so a byte cut lands mid-rune often enough to matter — and what it
	// produces is an invalid-UTF-8 log line, which downstream JSON encoders
	// rewrite into replacement characters.
	tail := text[len(text)-dshProvisionOutputBytes:]
	for len(tail) > 0 && !utf8.ValidString(tail[:1]) {
		tail = tail[1:]
	}
	return "…" + tail
}

// dshProfileRepair names what is missing, and deliberately carries NO command.
//
// The install is `dsh plugin --profile multica add <bundle>`, and <bundle> is
// the operator's own choice of npm spec, directory or tarball — Multica's own
// bridge is not on a public registry yet (multica#6936). A Repair.Command is
// rendered to the user inside a shell code fence as the thing to run on that
// machine, so shipping the placeholder there hands out a line that fails when
// pasted. Package alone still tells the server which repair to explain; the
// prose that explains it lives in the server's notice, where it can name both
// ways to supply a real bundle.
func dshProfileRepair() agent.ExecFormatRepair {
	return agent.ExecFormatRepair{Package: "DeepSeek Harness runtime profile"}
}

// dshProvisionCommand builds one install invocation. Split out from the run so
// the argv and the environment it executes under are assertable on every
// platform, including the Windows ones where a shell fixture cannot stand in
// for a package manager.
func dshProvisionCommand(dshPath, spec string, runtimeEnv map[string]string) *exec.Cmd {
	cmd := exec.Command(dshPath, "plugin", "--profile", dshMulticaProfileName, "add", spec)
	cmd.Env = dshPluginEnv()
	// A mise target must retain its paired interpreter, even when Desktop
	// supplies a different Node alongside its plugin package manager.
	for key, value := range runtimeEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	return cmd
}

// provisionDshMulticaProfile installs the configured runtime bundle into the
// `multica` profile, so a later discovery round finds a usable dsh. Candidates
// are tried in order and the first success wins.
//
// Runs the package manager through processtree: `dsh plugin` forwards to pnpm,
// which spawns node, which spawns whatever the bundle's install script needs.
// Killing only the direct child would leave that tree writing into the operator's
// DSH home after the daemon has stopped, and a daemon rollback cannot undo a
// write that already landed.
//
// Neither the spec nor the raw output is logged: a spec can be an npm URL with
// credentials in its userinfo or a path carrying a username, so the log names
// the candidate by position instead, and the output is redacted and bounded.
//
// A nil return means the profile is on disk, not merely that the package
// manager exited 0 — see the loop for why those are not the same thing.
//
// Returns nil when nothing is configured — the caller treats provisioning as
// best-effort either way, and the missing-profile verdict stands until a probe
// succeeds.
func provisionDshMulticaProfile(ctx context.Context, dshPath string, runtimeEnv map[string]string, logger *slog.Logger) error {
	specs := dshProfileBundleSpecs()
	if len(specs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, dshProvisionTimeout)
	defer cancel()

	var lastErr error
	for i, spec := range specs {
		output, err := processtree.CombinedOutput(ctx, dshProvisionCommand(dshPath, spec, runtimeEnv), 5*time.Second)
		switch {
		case err == nil && dshMulticaProfilePresent():
			logger.Info("installed the DSH runtime profile",
				"bundle_candidate", i+1, "candidates", len(specs),
				"path", dshPath, "profile", dshMulticaProfileName)
			return nil
		case err == nil:
			// Exit 0 with no manifest behind it. The exit status is the package
			// manager's opinion about its own run; dshMulticaProfilePresent is
			// the fact the REST of the daemon judges by — the probe
			// classification and the discovery loop's mismatch check both read
			// it — so an install allowed to report success while disagreeing
			// with it would log "installed the DSH runtime profile" and leave
			// every later round reporting that same profile as missing.
			//
			// Treating it as a failed candidate rather than as done is what
			// gives the remaining candidates their turn: a spec that resolves
			// and installs dependencies without producing a profile has not
			// done the job, and stopping on it would hide a later spec that
			// would have.
			lastErr = fmt.Errorf("dsh plugin --profile %s add (candidate %d of %d): reported success without creating the profile: %s",
				dshMulticaProfileName, i+1, len(specs), dshProvisionOutput(output))
		default:
			lastErr = fmt.Errorf("dsh plugin --profile %s add (candidate %d of %d): %w: %s",
				dshMulticaProfileName, i+1, len(specs), err, dshProvisionOutput(output))
		}
		logger.Warn("DSH runtime profile install failed",
			"bundle_candidate", i+1, "candidates", len(specs), "error", lastErr)
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
func (d *Daemon) startDshProfileProvision(dshPath string, runtimeEnv map[string]string) bool {
	if len(dshProfileBundleSpecs()) == 0 {
		return false
	}
	// The daemon's own context, not the caller's: a probe round is scoped to one
	// round, and an install killed with it would leave the operator with a
	// half-written profile and no explanation.
	ctx := d.daemonLifecycleCtx()
	started := false
	d.dshProvisionOnce.Do(func() {
		started = true
		runtimeEnv = maps.Clone(runtimeEnv)
		// Set before the goroutine rather than inside it: the verdict that
		// starts the install is built in the same round, and an offline reason
		// that says "wait" has to be true from the first deregistration.
		d.dshInstallInFlight.Store(true)
		go func() {
			defer d.dshInstallInFlight.Store(false)
			// provisionDshMulticaProfile returns nil only once the profile is
			// actually on disk, so nil here means there is something new to
			// find. The exit status alone would not have meant that.
			err := provisionDshMulticaProfile(ctx, dshPath, runtimeEnv, d.logger)
			if err == nil {
				d.logger.Info("DSH runtime profile installed; re-probing to bring dsh online")
				// Re-probe now rather than at the next scheduled round. The
				// discovery loop backs off while a provider cannot register, and
				// dsh counts as "missing a runtime" for as long as the profile is
				// absent — so the attempt this replaces can be
				// agentConvergeMaxBackoff away, which is indistinguishable from
				// "it does not work" to anyone watching the runtime list. On a
				// daemon that started with nothing registered this also has to
				// pick the workspace up first; see registerAfterDshProfileInstall.
				d.registerAfterDshProfileInstall(ctx)
				return
			}
			d.logger.Warn("automatic DSH runtime profile install did not produce a profile; dsh stays unregistered until one exists",
				"path", dshPath, "error", err)
			// Withdraw the claim before returning. Any runtime taken offline
			// while this install was running says "installing" on its row, and
			// the server reads that as a wait worth queueing behind. Nothing
			// else will ever rewrite it: the demotion already removed the
			// runtime from the index, so no later round has a runtime to
			// condemn and no later deregistration is sent. Left alone, that row
			// queues work forever behind an install that has already given up.
			d.withdrawDshInstallWait(ctx)
		}()
	})
	return started
}
