package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
)

// Command is the identity of a runtime CLI: the executable Multica spawns plus
// the argv prefix that belongs to the command itself rather than to any single
// invocation.
//
// A custom runtime profile (MUL-3284) is configured as `command_name` plus
// `fixed_args`, and for a wrapper like `ccms start q36` those two fixed_args
// tokens are part of *what the program is*, not options passed to it. The
// wrapper only reaches the real Claude binary after its `start` subcommand has
// been selected, so `-p` and the rest of the stream-json protocol flags are
// meaningless — and rejected outright by a Commander-style parser — until the
// prefix has been consumed (GH #7046).
//
// So the prefix goes directly after the executable and before everything a
// backend builds:
//
//	<Path> <Prefix...> <protocol args...> <ExtraArgs...> <CustomArgs...>
//
// Subcommand-style prefixes work only in this position. Flag-style prefixes
// generally parse the same either way — `agent --model composer-2.5 -p …` and
// `agent -p … --model composer-2.5` are equivalent to most parsers — so
// prefix-first is the broader of the two orders, not a universally safe one: a
// CLI that separates global flags from subcommand flags can still care where a
// flag lands. That residual risk is why the position change is called out in
// the runtime docs rather than treated as invisible.
//
// The zero Command is a bare executable with no prefix, which is what every
// built-in runtime uses.
type Command struct {
	// Path is the executable to spawn — a PATH lookup name or an absolute path.
	Path string
	// Prefix is the argv prefix that belongs to the command, already filtered
	// by FilterLaunchPrefix. Never mutate a Command's Prefix in place; Argv
	// copies it precisely so a caller cannot alias it.
	Prefix []string
	// logger reports prefix/argument conflicts at the moment a process is
	// built. Optional: a zero Command logs nothing.
	logger *slog.Logger
}

// NewCommand builds a Command from a resolved executable path and a launch
// prefix. Callers inside this package normally go through Config.commandAt
// instead, which carries the prefix the daemon configured for the runtime.
//
// The prefix should already have been through FilterLaunchPrefix; pass a raw
// fixed_args list only where no protocol channel exists to protect, such as a
// one-shot `--version` probe.
func NewCommand(path string, prefix []string) Command {
	return Command{Path: path, Prefix: append([]string(nil), prefix...)}
}

// Argv returns the full argument vector for one invocation: the command's own
// prefix followed by args. The result never aliases either input, so callers
// may append to it freely.
func (c Command) Argv(args ...string) []string {
	argv := make([]string, 0, len(c.Prefix)+len(args))
	argv = append(argv, c.Prefix...)
	argv = append(argv, args...)
	return argv
}

// exec builds the *exec.Cmd that runs this command with args.
//
// This is the single place in the package where a runtime CLI process is
// constructed. Everything the daemon launches or probes — task execution,
// `--version` detection, model discovery — routes through here or through
// execVia, so a new backend cannot forget the launch prefix and silently
// reintroduce GH #7046. TestOnlyLaunchGoSpawnsRuntimeProcesses enforces it.
func (c Command) exec(ctx context.Context, args ...string) *exec.Cmd {
	warnLaunchPrefixOverlap(c.Prefix, args, c.logger)
	return exec.CommandContext(ctx, c.Path, c.Argv(args...)...)
}

// invocationChooser is the shape of the per-tool platform launch rewrites
// (chooseCursorInvocation, chooseCopilotInvocation, choosePiInvocation). On
// Windows they replace argv[0] with a PowerShell host or a bundled native
// binary and wrap the CLI's own argv; on other platforms they pass through.
type invocationChooser func(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string)

// execVia builds the *exec.Cmd for a runtime CLI whose launch goes through a
// platform invocation chooser.
//
// The prefix is applied to the CLI's own argv *before* the chooser runs, never
// to the final argv[0]: on Windows the chooser may prepend its own tokens
// (`-NoProfile -ExecutionPolicy Bypass -File cursor-agent.ps1`) that must stay
// in front. `ccms start q36` has to land after the PowerShell preamble, on the
// wrapped CLI, not ahead of PowerShell's own flags.
func (c Command) execVia(ctx context.Context, choose invocationChooser, lookedUp string, args []string, logger *slog.Logger) (*exec.Cmd, string, []string) {
	warnLaunchPrefixOverlap(c.Prefix, args, logger)
	argv0, cmdArgs := choose(c.Path, lookedUp, c.Argv(args...), logger)
	return exec.CommandContext(ctx, argv0, cmdArgs...), argv0, cmdArgs
}

// withFilteredPrefix returns a copy of the command whose prefix has been
// through fn.
//
// Moving the prefix to the front makes it lose an ordinary last-wins flag
// conflict, which is the point. But a few settings are not decided by argv
// order at all: Codex's `-c key=value` beats the daemon-written config.toml
// from any position, so a `-c mcp_servers.…` in fixed_args shadows managed MCP
// whether it sits first or last. Those need the same removal that ExtraArgs
// and CustomArgs already get, and only the backend knows which they are.
func (c Command) withFilteredPrefix(fn func([]string) []string) Command {
	c.Prefix = fn(c.Prefix)
	return c
}

// cacheKey identifies the command for the model-discovery memo. The prefix is
// part of it because two profiles can share one binary and still enumerate
// different catalogs — `ccms start q36` and `ccms start opus` are the same
// executable with different models behind it, and keying on the path alone
// would serve the first one's catalog to the second (see discoveryCacheKey).
func (c Command) cacheKey() string {
	if len(c.Prefix) == 0 {
		return c.Path
	}
	return c.Path + "\x00" + strings.Join(c.Prefix, "\x00")
}

// String renders the command the way a user wrote it in the Runtimes UI, for
// logs and error messages.
func (c Command) String() string {
	if len(c.Prefix) == 0 {
		return c.Path
	}
	return c.Path + " " + strings.Join(c.Prefix, " ")
}

// commandAt pairs a resolved executable path with this runtime's launch
// prefix. Backends call it after their own PATH resolution and default-binary
// fallback, which is why the path is a parameter rather than read from
// Config.ExecutablePath.
func (c Config) commandAt(path string) Command {
	return Command{Path: path, Prefix: c.LaunchPrefix, logger: c.Logger}
}

// launchPrefixBlockedArgs maps a protocol family to the flags a launch prefix
// may not set. It exists so New can filter the prefix once, at the single
// point that knows the family, instead of asking each backend to remember.
//
// Only families that reject flags at all appear here; a family whose backend
// has no blocked-arg policy accepts any prefix.
var launchPrefixBlockedArgs = map[string]map[string]blockedArgMode{
	"antigravity": antigravityBlockedArgs,
	"claude":      claudeBlockedArgs,
	"codebuddy":   codebuddyBlockedArgs,
	"codex":       codexBlockedArgs,
	"copilot":     copilotBlockedArgs,
	"cursor":      cursorBlockedArgs,
	"deveco":      devecoBlockedArgs,
	"grok":        grokBlockedArgs,
	"hermes":      hermesBlockedArgs,
	"kimi":        kimiBlockedArgs,
	"kiro":        kiroBlockedArgs,
	"opencode":    opencodeBlockedArgs,
	"openclaw":    openclawBlockedArgs,
	"pi":          piBlockedArgs,
	"qoder":       qoderBlockedArgs,
	"qoderclicn":  qoderBlockedArgs,
	"qwen":        qwenBlockedArgs,
	"qwenpaw":     qwenpawBlockedArgs,
	"reasonix":    reasonixBlockedArgs,
	"traecli":     traecliBlockedArgs,
}

// FilterLaunchPrefix is the exported form for callers outside this package —
// the daemon, which builds Commands for CLI probes that never reach New.
// Without it a `--version` probe and a task launch would disagree about what
// the runtime's prefix is.
func FilterLaunchPrefix(agentType string, prefix []string, logger *slog.Logger) []string {
	return filterLaunchPrefix(prefix, agentType, logger)
}

// filterLaunchPrefix drops protocol-critical flags from a launch prefix while
// letting positional tokens through.
//
// The two token kinds answer different questions. A positional token is the
// command's identity — `start`, `q36` — and the daemon has no opinion about
// it. A flag token competes with the protocol arguments the daemon owns, and
// the prefix position would let it win: putting `--output-format text` in
// fixed_args would break the daemon↔CLI stream-json channel exactly like
// putting it in custom_args, which is already refused.
//
// This deliberately does NOT delegate to filterCustomArgs, even though the two
// share blocklists. Those maps also carry positional tokens — Hermes and
// QwenPaw block `acp`, TRAE blocks `acp` and `serve`, Grok blocks `agent`,
// `stdio` and `serve` — because in custom_args a bare `acp` really is someone
// re-issuing the backend's own subcommand. In a launch prefix the same token
// is part of the command's identity: `hermes-wrapper acp tenant` names a
// program, and dropping `acp` from it would silently rewrite the command into
// one the operator never configured. Only a leading `-` makes a token eligible
// for the blocklist here.
//
// A blocked flag still consumes its separate value token, positional or not,
// so `--permission-mode ask` cannot leave a stray `ask` behind to be read as a
// subcommand.
func filterLaunchPrefix(prefix []string, agentType string, logger *slog.Logger) []string {
	if len(prefix) == 0 {
		return nil
	}
	blocked, ok := launchPrefixBlockedArgs[agentType]
	if !ok {
		return append([]string(nil), prefix...)
	}
	if logger == nil {
		logger = slog.Default()
	}
	filtered := make([]string, 0, len(prefix))
	for i := 0; i < len(prefix); i++ {
		arg := unshellQuoteArg(prefix[i])
		flag, isFlag := launchPrefixFlagName(arg)
		if !isFlag {
			// The command's own identity. Never filtered, even when it
			// collides with a backend subcommand name.
			filtered = append(filtered, arg)
			continue
		}
		mode, isBlocked := blocked[flag]
		if !isBlocked {
			filtered = append(filtered, arg)
			continue
		}
		logger.Warn("custom runtime fixed_args: blocked protocol-critical flag, skipping", "flag", flag)
		hasInlineValue := strings.Contains(arg, "=")
		if mode == blockedWithValue && !hasInlineValue {
			i++
		} else if mode == blockedOptionalValue && !hasInlineValue && i+1 < len(prefix) &&
			!strings.HasPrefix(unshellQuoteArg(prefix[i+1]), "-") {
			i++
		}
	}
	return filtered
}

// warnLaunchPrefixOverlap logs the flags a launch prefix shares with the
// arguments the daemon builds for this run.
//
// Overlap is not an error: the prefix comes first, so the daemon's own value
// is the later one and wins under the last-wins parsing every supported CLI
// uses. That is the intended precedence — an explicitly chosen per-agent model
// should beat a runtime-wide default — but before GH #7046 the prefix sat last
// and quietly won instead, so a runtime pinned to `--model composer-2.5` used
// to override the model the member picked in the UI. Anyone who was relying on
// that gets a line in the log naming the flag rather than a silent change.
func warnLaunchPrefixOverlap(prefix, args []string, logger *slog.Logger) {
	if len(prefix) == 0 || len(args) == 0 || logger == nil {
		return
	}
	later := make(map[string]bool, len(args))
	for _, a := range args {
		if flag, ok := launchPrefixFlagName(a); ok {
			later[flag] = true
		}
	}
	for _, p := range prefix {
		flag, ok := launchPrefixFlagName(p)
		if !ok || !later[flag] {
			continue
		}
		logger.Warn("custom runtime fixed_args set a flag the runtime also sets; the runtime's value wins",
			"flag", flag)
	}
}

// launchPrefixFlagName extracts the flag name from an argv token, reporting
// false for positional tokens. `--model=o3` and `--model o3` both yield
// "--model" so the two spellings compare equal.
func launchPrefixFlagName(arg string) (string, bool) {
	arg = unshellQuoteArg(arg)
	if !strings.HasPrefix(arg, "-") || arg == "-" || arg == "--" {
		return "", false
	}
	if idx := strings.Index(arg, "="); idx > 0 {
		return arg[:idx], true
	}
	return arg, true
}
