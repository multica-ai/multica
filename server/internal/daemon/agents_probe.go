package daemon

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// shellResolveTTL bounds how long one login-shell PATH resolution is reused
// across probeAgentCLIs calls.
//
// This is deliberately much longer than agentDiscoveryInterval so the frequent
// discovery round stays a pure exec.LookPath sweep: resolveAgentsViaLoginShell
// forks the user's login shell and runs their rc files, and there is almost
// always at least one uninstalled provider to miss LookPath on, so a short TTL
// would turn discovery into a shell fork every few minutes for the life of the
// daemon.
//
// The practical effect: a CLI on the daemon's own PATH is discovered within
// agentDiscoveryInterval, while one reachable only through the login shell
// (nvm/fnm shims, a ~/.local/bin that only ~/.zshrc adds) takes up to this long
// — still without a restart, which is the part that was previously impossible.
var shellResolveTTL = 30 * time.Minute

var (
	shellResolveMu    sync.Mutex
	shellResolveCache map[string]string
	shellResolveKey   string
	shellResolvedAt   time.Time
)

// shellResolveEnvKey fingerprints the environment that determines what a login
// shell resolves. A change to any of these invalidates the cache immediately,
// independent of the TTL — the cached answer was for a different environment.
func shellResolveEnvKey() string {
	return strings.Join([]string{
		os.Getenv("PATH"),
		os.Getenv("SHELL"),
		os.Getenv("HOME"),
	}, "\x00")
}

// cachedShellResolvedAgents resolves every standard agent command name through
// the user's login shell, reusing the previous result for shellResolveTTL as
// long as the resolution-relevant environment is unchanged.
//
// resolveAgentsViaLoginShell forks the user's login shell, which runs their rc
// files, so this must stay a cache and not a per-probe call: probeAgentCLIs now
// runs periodically on a live daemon, and there is almost always at least one
// uninstalled provider to miss LookPath on.
func cachedShellResolvedAgents() map[string]string {
	shellResolveMu.Lock()
	defer shellResolveMu.Unlock()
	key := shellResolveEnvKey()
	if shellResolveCache != nil && shellResolveKey == key && time.Since(shellResolvedAt) < shellResolveTTL {
		return shellResolveCache
	}
	resolved := resolveAgentsViaLoginShell(defaultAgentCommandNames)
	if resolved == nil {
		// Distinguish "resolved nothing" from "never resolved" so a failing
		// shell doesn't get re-forked on every probe inside the TTL window.
		resolved = map[string]string{}
	}
	shellResolveCache = resolved
	shellResolveKey = key
	shellResolvedAt = time.Now()
	return shellResolveCache
}

// probeAgentCLIs discovers which built-in agent CLIs are installed on this
// machine and returns one AgentEntry per provider that resolved.
//
// This is pure discovery: no version detection and no minimum-version gate
// (detectBuiltinRuntimes owns those, per registration round). The result is
// therefore the machine's *availability* set, which is exactly what
// /health.agents reports and what `multica daemon probe-runtimes` prints.
//
// It is called once from LoadConfig at startup and again from the periodic
// workspace sync (refreshAgentAvailability), so a CLI the user installs while
// the daemon is already running gets picked up without a restart (MUL-5439).
// Everything it reads is process-external (PATH, MULTICA_*_PATH, MULTICA_*_MODEL),
// so re-running it is the only way to observe such an install.
//
// A var so tests can stub discovery without installing real CLIs.
var probeAgentCLIs = func() map[string]AgentEntry {
	// Probe available agent CLIs. exec.LookPath is the primary path, but on
	// macOS/Linux a GUI-launched daemon (Electron, Launchpad) does not
	// inherit the user's interactive shell PATH — fnm/nvm/volta multishells,
	// the Anthropic native installer prefix, and per-user npm prefixes all
	// live in dirs that only get added to PATH by ~/.zshrc or ~/.bashrc.
	// shellResolvedAgents asks the user's login shell, lazily on first miss,
	// to resolve every standard agent name to its canonical absolute path,
	// so we can find binaries the bare daemon process can't see. See
	// resolveAgentsViaLoginShell for the details and constraints.
	//
	// Laziness matters: the happy path (every agent on the daemon's PATH or
	// pinned to an explicit MULTICA_*_PATH) must not pay the cost of
	// spawning the user's login shell — that touches their rc files and
	// adds startup latency that scales with whatever they put in there. We
	// only fork a shell when a bare command name actually missed LookPath.
	//
	// The resolution is cached process-wide with a TTL (not per call) because
	// this function now also runs periodically on a live daemon: a per-call
	// sync.Once would fork a login shell on every discovery round, since there
	// is almost always at least one uninstalled provider to miss on. The TTL
	// still lets a CLI installed into a login-shell-only PATH dir (nvm, fnm,
	// ~/.local/bin via ~/.zshrc) be discovered without a restart (MUL-5439).
	getShellResolved := cachedShellResolvedAgents
	probe := func(envVar, defaultCmd, modelEnv string) (AgentEntry, bool) {
		cmd := envOrDefault(envVar, defaultCmd)
		if path, err := resolveAgentExecutablePath(cmd); err == nil {
			return AgentEntry{
				Path:    path,
				Command: cmd,
				Model:   strings.TrimSpace(os.Getenv(modelEnv)),
			}, true
		}
		// The shell fallback only rescues bare command names. An operator
		// who pinned MULTICA_*_PATH to an absolute or relative path that
		// doesn't exist should hard-miss, not silently get a different
		// binary.
		if strings.ContainsAny(cmd, "/\\") {
			return AgentEntry{}, false
		}
		if path, ok := getShellResolved()[cmd]; ok {
			return AgentEntry{
				Path:    path,
				Command: cmd,
				Model:   strings.TrimSpace(os.Getenv(modelEnv)),
			}, true
		}
		if defaultCmd == "codex" && cmd == defaultCmd {
			// Codex Desktop bundles its CLI inside the macOS app instead of
			// installing it onto PATH.
			for _, p := range codexDesktopAppBundlePaths() {
				if _, err := os.Stat(p); err == nil {
					return AgentEntry{
						Path:    p,
						Command: cmd,
						Model:   strings.TrimSpace(os.Getenv(modelEnv)),
					}, true
				}
			}
		}
		if defaultCmd == "dsh" && cmd == defaultCmd {
			// DeepSeek Harness Desktop bundles its CLI inside the macOS app
			// instead of installing `dsh` onto PATH, and the login-shell
			// fallback above cannot rescue it: no rc file knows that path.
			// The candidate is a Node script rather than a native binary, so
			// it only counts while it is executable — an entry discovered
			// without the executable bit would be advertised as a healthy
			// runtime and then fail on every spawn.
			//
			// The Multica runtime profile gate in the caller still applies:
			// a bundled CLI without the `multica` profile is not a runtime.
			for _, p := range dshDesktopAppBundlePaths() {
				if executableCandidate(p) {
					return AgentEntry{
						Path:    p,
						Command: cmd,
						Model:   strings.TrimSpace(os.Getenv(modelEnv)),
					}, true
				}
			}
		}
		return AgentEntry{}, false
	}

	agents := map[string]AgentEntry{}
	if e, ok := probe("MULTICA_CLAUDE_PATH", "claude", "MULTICA_CLAUDE_MODEL"); ok {
		agents["claude"] = e
	}
	if e, ok := probe("MULTICA_CODEX_PATH", "codex", "MULTICA_CODEX_MODEL"); ok {
		agents["codex"] = e
	}
	if e, ok := probe("MULTICA_OPENCODE_PATH", "opencode", "MULTICA_OPENCODE_MODEL"); ok {
		agents["opencode"] = e
	}
	if e, ok := probe("MULTICA_CODEARTS_PATH", "codearts", "MULTICA_CODEARTS_MODEL"); ok {
		agents["codearts"] = e
	} else if strings.TrimSpace(os.Getenv("MULTICA_CODEARTS_PATH")) == "" {
		// The native CodeArts installer may update PATH only for future
		// terminals. A GUI-launched daemon can still discover its stable
		// user-level launcher. An explicit but invalid override remains a hard
		// miss and never falls back here.
		home, err := os.UserHomeDir()
		if err == nil {
			for _, name := range []string{"codearts.cmd", "codearts"} {
				candidate := filepath.Join(home, ".codeartsdoer", "installers", name)
				path, resolveErr := resolveAgentExecutablePath(candidate)
				if resolveErr != nil {
					continue
				}
				agents["codearts"] = AgentEntry{
					Path:    path,
					Command: "codearts",
					Model:   strings.TrimSpace(os.Getenv("MULTICA_CODEARTS_MODEL")),
				}
				break
			}
		}
	}
	if e, ok := probe("MULTICA_DEVECO_PATH", "deveco", "MULTICA_DEVECO_MODEL"); ok {
		agents["deveco"] = e
	}
	if e, ok := probe("MULTICA_OPENCLAW_PATH", "openclaw", "MULTICA_OPENCLAW_MODEL"); ok {
		agents["openclaw"] = e
	}
	if e, ok := probe("MULTICA_HERMES_PATH", "hermes", "MULTICA_HERMES_MODEL"); ok {
		agents["hermes"] = e
	}
	if e, ok := probe("MULTICA_PI_PATH", "pi", "MULTICA_PI_MODEL"); ok {
		agents["pi"] = e
	}
	// Built-in runtime identities (e.g. omp) are derived from the descriptor
	// registry in server/pkg/agent/builtin_runtimes.go. Each one probes a
	// separate CLI independently so a host with both pi and omp installed gets
	// two runtimes. The env prefix and default command come from the
	// descriptor, so adding a new fork is a descriptor entry, not a probe edit.
	for _, desc := range agent.BuiltinRuntimes {
		pathEnv := desc.EnvPrefix + "_PATH"
		modelEnv := desc.EnvPrefix + "_MODEL"
		if e, ok := probe(pathEnv, desc.DefaultCommand, modelEnv); ok {
			agents[desc.ID] = e
		}
	}
	if e, ok := probe("MULTICA_CURSOR_PATH", "cursor-agent", "MULTICA_CURSOR_MODEL"); ok {
		agents["cursor"] = e
	}
	if e, ok := probe("MULTICA_COPILOT_PATH", "copilot", "MULTICA_COPILOT_MODEL"); ok {
		agents["copilot"] = e
	}
	if e, ok := probe("MULTICA_KIMI_PATH", "kimi", "MULTICA_KIMI_MODEL"); ok {
		agents["kimi"] = e
	}
	if e, ok := probe("MULTICA_REASONIX_PATH", "reasonix", "MULTICA_REASONIX_MODEL"); ok {
		agents["reasonix"] = e
	}
	// DSH resolves here like any other CLI. Whether it is *usable* is decided
	// one layer up: the Multica runtime profile is what gives it the --stdio
	// protocol, so a bare `dsh` prints a version and still cannot run a task.
	// That check lives in probeBuiltinRuntime, where a failure produces a
	// verdict the user can actually see — /health reports it as a skipped
	// agent carrying the repair command, and the daemon logs it. Gating here
	// instead made the drop invisible: the provider vanished from the
	// availability set with nothing anywhere saying why.
	if e, ok := probe("MULTICA_DSH_PATH", "dsh", "MULTICA_DSH_MODEL"); ok {
		agents["dsh"] = e
	}
	if e, ok := probe("MULTICA_KIRO_PATH", "kiro-cli", "MULTICA_KIRO_MODEL"); ok {
		agents["kiro"] = e
	}
	if e, ok := probe("MULTICA_CODEBUDDY_PATH", "codebuddy", "MULTICA_CODEBUDDY_MODEL"); ok {
		agents["codebuddy"] = e
	}
	// agy 1.0.6 added a `--model` flag (MUL-3125), so Antigravity now takes a
	// model env like every other backend. MULTICA_ANTIGRAVITY_MODEL seeds the
	// daemon-wide default; its value is the exact `agy models` display string
	// (e.g. "Claude Opus 4.6 (Thinking)"), not a provider/model slug.
	if e, ok := probe("MULTICA_ANTIGRAVITY_PATH", "agy", "MULTICA_ANTIGRAVITY_MODEL"); ok {
		agents["antigravity"] = e
	}
	// Qoder CLI ships as the `qodercli` binary (Qoder Desktop does not put it
	// on PATH; users install it separately, often via an npm global prefix).
	// It must go through probe() like every other provider so the login-shell
	// fallback applies: a GUI/Launchpad-started daemon does not inherit the
	// interactive shell PATH, and without the fallback a perfectly good
	// qodercli install stayed invisible across restarts (MUL-5524).
	if e, ok := probe("MULTICA_QODER_PATH", "qodercli", "MULTICA_QODER_MODEL"); ok {
		agents["qoder"] = e
	}
	// Qoder CN CLI exposes the same ACP transport as Qoder CLI under a
	// separate `qoderclicn` binary and account/config root. Register it as an
	// independent provider so hosts with either or both editions get the
	// matching runtime without a custom profile.
	if e, ok := probe("MULTICA_QODERCLICN_PATH", "qoderclicn", "MULTICA_QODERCLICN_MODEL"); ok {
		agents["qoderclicn"] = e
	}
	// ByteDance official TRAE CLI (the `traecli` binary from https://docs.trae.cn/cli),
	// driven over ACP via `traecli acp serve --yolo`. MULTICA_TRAECLI_MODEL seeds
	// the daemon-wide default model (a model id from the user's logged-in traecli
	// catalog).
	if e, ok := probe("MULTICA_TRAECLI_PATH", "traecli", "MULTICA_TRAECLI_MODEL"); ok {
		agents["traecli"] = e
	}
	// xAI Grok Build CLI (`grok`), driven over ACP via
	// `grok agent --always-approve stdio`. MULTICA_GROK_MODEL seeds the
	// daemon-wide default (e.g. grok-4.5).
	if e, ok := probe("MULTICA_GROK_PATH", "grok", "MULTICA_GROK_MODEL"); ok {
		agents["grok"] = e
	}
	// Qwen Code (`qwen`) runs headlessly with -p and stream-json. Its native
	// QWEN.md and .qwen/skills task context is prepared by execenv.
	if e, ok := probe("MULTICA_QWEN_PATH", "qwen", "MULTICA_QWEN_MODEL"); ok {
		agents["qwen"] = e
	}
	// QwenPaw (`qwenpaw`) is the QwenPaw CLI agent, driven over ACP via
	// `qwenpaw acp`. It takes no model env var: the backend never calls
	// session/set_model (it would rewrite QwenPaw's shared agent config), so
	// ExecOptions.Model is ignored — see ModelSelectionSupported. Reading one
	// here would only advertise a knob that silently does nothing.
	if e, ok := probe("MULTICA_QWENPAW_PATH", "qwenpaw", ""); ok {
		agents["qwenpaw"] = e
	}
	// Dim (`dim`) is the DimCode CLI agent, driven over ACP via `dim acp`.
	// MULTICA_DIM_MODEL seeds the daemon-wide default (a model id from the
	// user's logged-in dim catalog).
	if e, ok := probe("MULTICA_DIM_PATH", "dim", "MULTICA_DIM_MODEL"); ok {
		agents["dim"] = e
	}
	// MiniMax Code (`mcode`) exposes an ACP v1 server through `mcode acp`.
	// Model selection is owned by the MCode runtime, so there is no model env.
	if e, ok := probe("MULTICA_MCODE_PATH", "mcode", ""); ok {
		agents["mcode"] = e
	}
	// ZeroClaw (`zeroclaw`) is a Rust-based generic agent CLI, driven over
	// ACP via `zeroclaw acp`. It takes no model env var: its ACP server has no
	// `session/set_model` and no handler reads a model param, so the model
	// comes from ZeroClaw's own agent profile and ExecOptions.Model can never
	// be applied — see ModelSelectionSupported. Reading one here would only
	// advertise a knob that silently does nothing.
	if e, ok := probe("MULTICA_ZEROCLAW_PATH", "zeroclaw", ""); ok {
		agents["zeroclaw"] = e
	}
	return agents
}

// executableCandidate reports whether a Desktop-app bundle candidate can be
// spawned directly. Bundle candidates reached through probe() never went
// through exec.LookPath, which is what normally enforces this.
//
// "Executable" is not the same fact on both platforms, and the difference is
// not cosmetic. On Unix it is the executable bit — the macOS candidate is a
// Node script that runs through its shebang, and one that lost its bit would be
// advertised as a healthy runtime and then fail on every spawn.
//
// Windows has no such bit: Go synthesizes a regular file's mode from the
// read-only attribute and never sets 0111 on one, so the Unix test would reject
// every candidate and the whole fallback would be dead code there. What decides
// on Windows is the extension, and exec.LookPath is the component that already
// knows the rule (PATHEXT) — the same check agentExecutablePresent makes, so a
// candidate accepted here is one the launcher can actually start.
func executableCandidate(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		_, err := exec.LookPath(path)
		return err == nil
	}
	return info.Mode().Perm()&0o111 != 0
}

// dshProbeFrame is the discovery frame a Multica-capable DSH profile prints for
// `--probe`. Only these fields are read; the bundle owns everything else.
type dshProbeFrame struct {
	Version         int    `json:"v"`
	Type            string `json:"type"`
	Runtime         string `json:"runtime"`
	ProtocolVersion int    `json:"protocol_version"`
}

// dshProbeVerdict is what `dsh --profile multica --probe` actually said.
//
// The distinction is the point. "The profile is not installed" is a confirmed,
// locally repairable fact that justifies installing it and taking a live
// runtime offline; a timeout or an unparseable answer is evidence about this
// instant and nothing more. Collapsing both into one bool is what let a
// momentary failure during a DSH upgrade demote a working runtime, and — with a
// bundle configured — start overwriting an installation that was already there.
type dshProbeVerdict int

const (
	// dshProbeOK: the profile answered with the protocol version this backend drives.
	dshProbeOK dshProbeVerdict = iota
	// dshProbeMissingProfile: the probe ran and no profile manifest is present,
	// which is the same fact DSH itself checks before refusing to boot. This is
	// the only verdict that may install a bundle or condemn the runtime.
	dshProbeMissingProfile
	// dshProbeIncompatible: something answered, with a protocol this daemon does
	// not drive. The profile is installed, so installing over it would replace a
	// deliberate configuration with one this daemon still cannot drive.
	dshProbeIncompatible
	// dshProbeUnavailable: nothing usable was learned — the probe timed out,
	// could not be executed, or printed something unparseable. Transient.
	dshProbeUnavailable
)

// dshProbeTimeout bounds one `--probe`.
//
// The healthy path boots a whole DSH process — on a Desktop install that means
// starting the app's Electron binary as node — and then prints one line. Five
// seconds was too thin for that on a cold VM, and cutting a slow-but-healthy
// probe short reports it as unusable. This is generous instead, because the
// cost is paid only by a probe that has nothing to say, and it stays well
// inside the daemon's own 45s startup window.
//
// A missing profile does spend the whole budget here: DSH does not refuse the
// probe, it hangs. That is bounded by construction — the state ends the moment
// someone installs the profile — and the verdict below no longer depends on
// telling that hang apart from a busy machine.
//
// A var so tests can shrink it instead of waiting out a real timeout.
var dshProbeTimeout = 15 * time.Second

// parseDshProbeFrame returns the first decodable probe frame in the output.
func parseDshProbeFrame(output string) (dshProbeFrame, bool) {
	for _, line := range strings.Split(output, "\n") {
		var frame dshProbeFrame
		if json.Unmarshal([]byte(line), &frame) != nil {
			continue
		}
		if frame.Type == "probe" && frame.Runtime != "" {
			return frame, true
		}
	}
	return dshProbeFrame{}, false
}

// probeDshMulticaProfile classifies one `--probe` attempt. See dshProbeVerdict
// for why this is not a bool.
//
// Scoped to the caller's context as well as its own timeout: `--probe` boots a
// whole DSH process, and a round abandoned by a shutting-down daemon should not
// go on holding one for the rest of the timeout — once per retry, per provider.
func probeDshMulticaProfile(ctx context.Context, executablePath string) dshProbeVerdict {
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, dshProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executablePath, "--profile", dshMulticaProfileName, "--probe")
	// processtree, not cmd.Output: `--probe` boots a whole DSH profile, so the
	// process it starts is a tree. Killing only the direct child leaves
	// grandchildren holding the stdout pipe open — which is both a leaked
	// process and, as this probe's own tests showed, a wait that outlives the
	// timeout it was supposed to be bounded by.
	output, _ := processtree.Output(ctx, cmd, time.Second)

	// The frame decides, not the exit status. A profile that printed a valid
	// answer has answered, whatever the process did on its way out — and the
	// run helper reports a lifecycle error for a tree that outlived its leader,
	// which says nothing about the protocol.
	//
	// agent.DshProtocolVersion, not a local copy: this decides whether the
	// daemon may drive the profile, and the code that drives it is the only
	// honest source for that number.
	if frame, ok := parseDshProbeFrame(string(output)); ok {
		if frame.Version == 1 && frame.Runtime == "dsh" && frame.ProtocolVersion == agent.DshProtocolVersion {
			return dshProbeOK
		}
		return dshProbeIncompatible
	}

	// No usable answer. A cancelled ROUND is the one case that says nothing
	// about the profile: the daemon is shutting down, or this round was
	// abandoned, and the probe never got to finish on its own terms.
	if parent.Err() != nil {
		return dshProbeUnavailable
	}

	// Otherwise the manifest decides, and how the probe failed does not.
	//
	// Timeouts used to be classed transient on the reasoning that a busy
	// machine is not a missing profile. That reasoning does not survive
	// contact with DSH: asked for a profile it does not have, `--probe` does
	// not refuse — it HANGS, printing nothing, until something kills it. So a
	// timeout is the ordinary way a missing profile presents, and treating it
	// as transient made the profile undetectable on exactly the host the
	// install exists for: the daemon reported "the probe returned no usable
	// answer", never started the install, and exited for want of a runtime.
	//
	// The manifest is the same file DSH's own loadProfile consults before it
	// refuses to boot, so a confirmed-absent one is evidence, not a guess —
	// while a manifest that IS present keeps every failure transient, which is
	// what protects a working install from being overwritten during an upgrade.
	if !dshMulticaProfilePresent() {
		return dshProbeMissingProfile
	}
	return dshProbeUnavailable
}
