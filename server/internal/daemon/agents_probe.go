package daemon

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

// cachedShellResolvedExecutables adds daemon-specific version-manager
// resolution to the login shell's invocation paths. Keeping this outside the
// shell cache preserves its cheap path-only contract while still pairing a
// mise target with the environment selected from the trusted root.
func cachedShellResolvedExecutables() map[string]executableResolution {
	paths := cachedShellResolvedAgents()
	resolved := make(map[string]executableResolution, len(paths))
	for name, path := range paths {
		entry, _, err := resolveMiseDiscoveredExecutable(path, name)
		if err != nil {
			continue
		}
		resolved[name] = entry
	}
	return resolved
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
	// A probe checks many provider names. Resolve the login-shell snapshot's
	// richer launch contracts at most once for this round: the path snapshot is
	// cached, but mise path/environment resolution intentionally is not, and
	// repeating it once per missing provider would multiply its two-second
	// deadline across the whole provider list.
	var (
		shellResolvedOnce sync.Once
		shellResolved     map[string]executableResolution
	)
	getShellResolved := func() map[string]executableResolution {
		shellResolvedOnce.Do(func() {
			shellResolved = cachedShellResolvedExecutables()
		})
		return shellResolved
	}
	probe := func(envVar, defaultCmd, modelEnv string) (AgentEntry, bool) {
		cmd := envOrDefault(envVar, defaultCmd)
		if resolved, err := resolveAgentExecutable(cmd); err == nil {
			return AgentEntry{
				Path:    resolved.Path,
				Command: cmd,
				Model:   strings.TrimSpace(os.Getenv(modelEnv)),
				MiseEnv: resolved.Env,
			}, true
		}
		// The shell fallback only rescues bare command names. An operator
		// who pinned MULTICA_*_PATH to an absolute or relative path that
		// doesn't exist should hard-miss, not silently get a different
		// binary.
		if strings.ContainsAny(cmd, "/\\") {
			return AgentEntry{}, false
		}
		if resolved, ok := getShellResolved()[cmd]; ok {
			return AgentEntry{
				Path:    resolved.Path,
				Command: cmd,
				Model:   strings.TrimSpace(os.Getenv(modelEnv)),
				MiseEnv: resolved.Env,
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
	// DSH is registered only when its Multica runtime profile is installed.
	// A bare dsh binary is not enough: without the bundle it has no --stdio
	// protocol and every task would fail after being advertised as healthy.
	if e, ok := probe("MULTICA_DSH_PATH", "dsh", "MULTICA_DSH_MODEL"); ok && probeDshMulticaProfile(e.Path, e.MiseEnv) {
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

func probeDshMulticaProfile(executablePath string, runtimeEnv map[string]string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executablePath, "--profile", "multica", "--probe")
	if len(runtimeEnv) > 0 {
		cmd.Env = append([]string(nil), os.Environ()...)
		for key, value := range runtimeEnv {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.WaitDelay = time.Second
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		var frame struct {
			Version         int    `json:"v"`
			Type            string `json:"type"`
			Runtime         string `json:"runtime"`
			ProtocolVersion int    `json:"protocol_version"`
		}
		if json.Unmarshal([]byte(line), &frame) == nil && frame.Version == 1 && frame.Type == "probe" && frame.Runtime == "dsh" && frame.ProtocolVersion == 1 {
			return true
		}
	}
	return false
}
