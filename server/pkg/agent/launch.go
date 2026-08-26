package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const redactedAgentCommandArg = "<redacted>"

const maxLoggedAgentCommandFlagLen = 64

// agentCommandLogArgs describes the adapter-assembled argv handed to the
// launch boundary. trustedPositionals contains indexes into invocationArgs for
// literal subcommands owned by the adapter; every other positional remains
// redacted. Keeping index-and-literal assertions instead of a global string
// allowlist makes trust follow the argument's source, so an identical token
// supplied through custom_args is still treated as sensitive.
type agentCommandLogArgs struct {
	invocationArgs     []string
	trustedPositionals []trustedAgentCommandPositional
}

type trustedAgentCommandPositional struct {
	index int
	value string
}

func trustAgentCommandPositional(index int, value string) trustedAgentCommandPositional {
	return trustedAgentCommandPositional{index: index, value: value}
}

func newAgentCommandLogArgs(invocationArgs []string, trustedPositionals ...trustedAgentCommandPositional) agentCommandLogArgs {
	return agentCommandLogArgs{
		invocationArgs:     invocationArgs,
		trustedPositionals: append([]trustedAgentCommandPositional(nil), trustedPositionals...),
	}
}

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
	return newRuntimeCmd(exec.CommandContext(ctx, c.Path, c.Argv(args...)...))
}

// newRuntimeCmd applies the process-lifecycle defaults every runtime process in
// this package gets. It runs at construction because both of them have to be in
// place before the process exists.
//
// Both used to be opt-in, and opt-in is why GH #7522 happened. Of the 27 places
// this package starts a process, 8 asked for a process group; the rest left
// their CLI in the daemon's group, where a group-wide signal cannot reach it.
// os/exec's default Cancel is the same leak by another route: it kills the
// leader alone, so a cancelled task's tool subprocesses — MCP servers, shells,
// whatever the agent spawned — survive it. On Linux that was #5918. On Windows,
// where the leader is often a cmd.exe shim and the real CLI is already a
// grandchild, a cancelled agent kept working for 40 minutes.
//
// A backend that wants a graceful shutdown instead of an immediate kill assigns
// its own cmd.Cancel after construction and wins; claude, dsh and deveco do,
// because they drive SIGTERM → grace → SIGKILL themselves.
func newRuntimeCmd(cmd *exec.Cmd) *exec.Cmd {
	configureProcessGroup(cmd)
	cmd.Cancel = func() error {
		signalProcessGroup(cmd, syscall.SIGKILL)
		return nil
	}
	return cmd
}

// runOwned is Run() over an owned process tree: start, wait, drop ownership.
//
// os/exec's Run/Output/CombinedOutput call Start themselves, so a probe written
// with them never reaches startOwnedProcessTree and owns nothing on Windows —
// where the direct child of a `--version` probe is typically the shim, not the
// CLI. That is the same escape GH #7522 was reported for, in a path nobody was
// looking at: detectCLIVersion's own comment already describes a broken CLI
// leaving grandchildren behind. These three helpers are how a synchronous probe
// gets the same ownership a task launch has.
func runOwned(cmd *exec.Cmd, logger *slog.Logger) error {
	// Without a bound this waits for its own cleanup and never returns. A
	// descendant that inherited the output pipes holds them open after the
	// leader exits; cmd.Wait blocks until the copy goroutines see EOF; and the
	// thing that would close those pipes is the release below, which runs
	// after Wait. WaitDelay is defined for exactly this case — "a child
	// process that exits but leaves its I/O pipes unclosed" — and cancellation
	// is no help, because a probe like checkOpenclawVersion runs on the
	// caller's context before any task timeout exists.
	//
	// A caller that set its own bound keeps it; detectCLIVersion has had one
	// since MUL-3812 for this exact shape.
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = probeWaitDelay
	}
	if err := startOwnedProcessTree(cmd, logger); err != nil {
		return err
	}
	err := cmd.Wait()
	// The probe is over the moment its leader is: nothing it spawned should
	// outlive the answer. Signalling before the release covers Unix, where
	// releasing a process group is a no-op; on Windows closing the Job Object
	// would take the tree down on its own.
	signalProcessGroup(cmd, syscall.SIGKILL)
	releaseProcessGroup(cmd)
	return err
}

// probeWaitDelay bounds how long a finished probe waits on output pipes its
// descendants left open. It matches the bound detectCLIVersion already sets by
// hand. The timer only starts once the child has exited or the context is
// done, so a healthy probe never pays it.
const probeWaitDelay = 2 * time.Second

// outputOwned is cmd.Output() over an owned process tree. It matches the
// stdlib's contract — stdout returned, a failed run's stderr attached to the
// *exec.ExitError — except that the stderr sample is the last
// probeStderrSampleBytes rather than the stdlib's head-and-tail, which is where
// a CLI's actual failure line ends up.
func outputOwned(cmd *exec.Cmd, logger *slog.Logger) ([]byte, error) {
	if cmd.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// A caller that set its own Stderr wants it; only fill in the sample when
	// nothing else is watching, exactly as Output() does.
	var stderr *tailBuffer
	if cmd.Stderr == nil {
		stderr = &tailBuffer{max: probeStderrSampleBytes}
		cmd.Stderr = stderr
	}

	err := runOwned(cmd, logger)
	var exitErr *exec.ExitError
	if stderr != nil && errors.As(err, &exitErr) {
		exitErr.Stderr = stderr.Bytes()
	}
	return stdout.Bytes(), err
}

// combinedOutputOwned is cmd.CombinedOutput() over an owned process tree.
// Stdout and Stderr are the same writer value, which is what makes os/exec give
// them one pipe and therefore one interleaving, as the stdlib does.
func combinedOutputOwned(cmd *exec.Cmd, logger *slog.Logger) ([]byte, error) {
	if cmd.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	if cmd.Stderr != nil {
		return nil, errors.New("exec: Stderr already set")
	}
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	err := runOwned(cmd, logger)
	return combined.Bytes(), err
}

// probeStderrSampleBytes bounds the stderr kept for a failed probe's error.
// os/exec bounds the same sample at 32 KiB; a CLI stuck in a log loop should
// not be able to grow the daemon's heap through a `--version` call.
const probeStderrSampleBytes = 32 << 10

// tailBuffer keeps the last max bytes written to it and discards the rest. It
// always reports a full write, so a child is never blocked or shortened by the
// bound.
type tailBuffer struct {
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) Bytes() []byte { return t.buf }

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
	return newRuntimeCmd(exec.CommandContext(ctx, argv0, cmdArgs...)), argv0, cmdArgs
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

// logAgentCommand is the only boundary allowed to record runtime process
// arguments. It works from the final exec.Cmd so launch prefixes and
// platform-specific rewrites are represented, but never records argument
// values: flag names and adapter-owned literal subcommands remain useful for
// diagnostics, inline values are removed, and every other positional/value
// token is replaced with a marker.
func (c Config) logAgentCommand(cmd *exec.Cmd, source agentCommandLogArgs) {
	c.logAgentCommandFields(cmd, source, 0, false)
}

// logAgentCommandWithPrompt adds only a typed prompt byte count to the safe
// command log. Keeping it separate from logAgentCommand avoids an open-ended
// optional field channel that could accidentally carry prompt content.
func (c Config) logAgentCommandWithPrompt(cmd *exec.Cmd, source agentCommandLogArgs, promptBytes int) {
	c.logAgentCommandFields(cmd, source, promptBytes, true)
}

func (c Config) logAgentCommandFields(cmd *exec.Cmd, source agentCommandLogArgs, promptBytes int, includePromptBytes bool) {
	if cmd == nil {
		return
	}
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}

	args := []string{}
	if len(cmd.Args) > 1 {
		args = cmd.Args[1:]
	}
	trustedPositionals := c.trustedAgentCommandPositionals(args, source)
	fields := []any{
		"provider", c.provider,
		"exec", cmd.Path,
		"args", redactAgentCommandArgs(args, trustedPositionals),
		"arg_count", len(args),
	}
	if includePromptBytes {
		fields = append(fields, "prompt_bytes", promptBytes)
	}
	logger.Info("agent command", fields...)
}

// trustedAgentCommandPositionals maps source indexes onto the final exec.Cmd
// argv. Platform launch rewrites may prepend a PowerShell host and wrapper
// arguments, so the original launch prefix plus adapter argv must match the
// final suffix before any positional is trusted. A mismatch fails closed.
func (c Config) trustedAgentCommandPositionals(finalArgs []string, source agentCommandLogArgs) map[int]struct{} {
	originalLen := len(c.LaunchPrefix) + len(source.invocationArgs)
	start := len(finalArgs) - originalLen
	if start < 0 {
		return nil
	}
	for i, arg := range c.LaunchPrefix {
		if finalArgs[start+i] != arg {
			return nil
		}
	}
	invocationStart := start + len(c.LaunchPrefix)
	for i, arg := range source.invocationArgs {
		if finalArgs[invocationStart+i] != arg {
			return nil
		}
	}

	trusted := make(map[int]struct{}, len(source.trustedPositionals))
	for _, positional := range source.trustedPositionals {
		if positional.index < 0 || positional.index >= len(source.invocationArgs) ||
			source.invocationArgs[positional.index] != positional.value {
			continue
		}
		trusted[invocationStart+positional.index] = struct{}{}
	}
	return trusted
}

// redactAgentCommandArgs preserves only syntactically plausible flag names and
// source-proven adapter subcommands while removing every value. Single-dash
// flags must be one ASCII letter; this keeps a token such as
// "-sTk9xQZ-secretvalue" from being mistaken for a flag. Long flags are
// length-bounded and lose inline values. All other tokens become <redacted>.
func redactAgentCommandArgs(args []string, trustedPositionals map[int]struct{}) []string {
	redacted := make([]string, len(args))
	for i, arg := range args {
		if _, ok := trustedPositionals[i]; ok {
			redacted[i] = arg
			continue
		}
		if flag, ok := safeAgentCommandFlagName(arg); ok {
			redacted[i] = flag
			continue
		}
		redacted[i] = redactedAgentCommandArg
	}
	return redacted
}

func safeAgentCommandFlagName(arg string) (string, bool) {
	flag := arg
	if equals := strings.IndexByte(flag, '='); equals > 0 {
		flag = flag[:equals]
	}
	if len(flag) == 2 && flag[0] == '-' && isASCIIAlpha(flag[1]) {
		return flag, true
	}
	if len(flag) < 3 || len(flag) > maxLoggedAgentCommandFlagLen || !strings.HasPrefix(flag, "--") || !isASCIIAlpha(flag[2]) {
		return "", false
	}
	for i := 3; i < len(flag); i++ {
		ch := flag[i]
		if !isASCIIAlpha(ch) && (ch < '0' || ch > '9') && ch != '.' && ch != '_' && ch != '-' {
			return "", false
		}
	}
	return flag, true
}

func isASCIIAlpha(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
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
	"dim":         dimBlockedArgs,
	"zeroclaw":    zeroclawBlockedArgs,
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
