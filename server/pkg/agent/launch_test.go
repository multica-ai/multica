package agent

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// prefixIndex reports where the first token of want starts inside argv, or -1.
func prefixIndex(argv, want []string) int {
	for i := 0; i+len(want) <= len(argv); i++ {
		if strings.Join(argv[i:i+len(want)], "\x00") == strings.Join(want, "\x00") {
			return i
		}
	}
	return -1
}

// TestLaunchPrefixPrecedesProtocolFlags is GH #7046 in one assertion. `ccms
// start q36` only reaches the real Claude binary after its `start` subcommand
// is selected, so a `-p` that arrives first is parsed by the wrapper, which
// rejects it outright ("unknown option '-p'"). The subcommand tokens have to
// come first.
func TestLaunchPrefixPrecedesProtocolFlags(t *testing.T) {
	t.Parallel()

	cfg := Config{ExecutablePath: "ccms", LaunchPrefix: []string{"start", "q36"}, Logger: slog.Default()}
	argv := cfg.commandAt("ccms").Argv(buildClaudeArgs(ExecOptions{}, slog.Default())...)

	subcommand := prefixIndex(argv, []string{"start", "q36"})
	if subcommand != 0 {
		t.Fatalf("fixed_args must be the argv prefix, found at %d: %v", subcommand, argv)
	}
	protocol := prefixIndex(argv, []string{"-p"})
	if protocol < 0 {
		t.Fatalf("protocol flags missing from argv: %v", argv)
	}
	if protocol < subcommand+2 {
		t.Fatalf("-p at %d must come after the `start q36` prefix: %v", protocol, argv)
	}
}

// TestLaunchPrefixPlacesFlagStyleWrappersFirst pins where a flag-style prefix
// lands. It asserts the argv Multica builds, not that any particular third-party
// parser accepts the new position — most treat the two orders as equivalent,
// but a CLI that separates global from subcommand flags may not, which is why
// the docs call the move out instead of promising it is invisible.
func TestLaunchPrefixPlacesFlagStyleWrappersFirst(t *testing.T) {
	t.Parallel()

	cfg := Config{ExecutablePath: "agent", LaunchPrefix: []string{"--model", "composer-2.5"}, Logger: slog.Default()}
	argv := cfg.commandAt("agent").Argv(buildClaudeArgs(ExecOptions{}, slog.Default())...)

	if idx := prefixIndex(argv, []string{"--model", "composer-2.5"}); idx != 0 {
		t.Fatalf("flag-style fixed_args must still lead the argv, found at %d: %v", idx, argv)
	}
}

// TestLaunchPrefixLosesToExplicitAgentModel pins the precedence flip this
// change makes deliberately. Before GH #7046 the prefix sat last and won every
// last-wins parse, so a runtime pinned to `--model composer-2.5` silently
// overrode the model the member picked in the UI. Prefix-first inverts that:
// the more specific per-agent choice is the later token and wins.
func TestLaunchPrefixLosesToExplicitAgentModel(t *testing.T) {
	t.Parallel()

	cfg := Config{ExecutablePath: "agent", LaunchPrefix: []string{"--model", "composer-2.5"}, Logger: slog.Default()}
	argv := cfg.commandAt("agent").Argv(buildClaudeArgs(ExecOptions{Model: "claude-opus-4-7"}, slog.Default())...)

	runtimeModel := prefixIndex(argv, []string{"--model", "composer-2.5"})
	agentModel := prefixIndex(argv, []string{"--model", "claude-opus-4-7"})
	if runtimeModel < 0 || agentModel < 0 {
		t.Fatalf("expected both --model occurrences: %v", argv)
	}
	if agentModel <= runtimeModel {
		t.Fatalf("the agent's explicit model must be the later --model (runtime at %d, agent at %d): %v",
			runtimeModel, agentModel, argv)
	}
}

// TestFilterLaunchPrefixKeepsSubcommandsDropsProtocolFlags covers the
// blocklist split: positional tokens are the command's identity and pass
// through, flag tokens compete with the daemon's own protocol arguments and do
// not. A blocked flag takes its value token with it.
func TestFilterLaunchPrefixKeepsSubcommandsDropsProtocolFlags(t *testing.T) {
	t.Parallel()

	got := filterLaunchPrefix(
		[]string{"start", "q36", "-p", "--output-format", "text", "--model", "composer-2.5"},
		"claude", slog.Default())
	want := []string{"start", "q36", "--model", "composer-2.5"}

	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("filterLaunchPrefix = %v, want %v", got, want)
	}
}

// TestNewFiltersLaunchPrefixOnce proves the filter runs at the single point
// that knows the protocol family, so no backend has to remember to call it.
func TestNewFiltersLaunchPrefixOnce(t *testing.T) {
	t.Parallel()

	backend, err := New("claude", Config{
		ExecutablePath: "ccms",
		LaunchPrefix:   []string{"start", "q36", "--output-format", "text"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("New(claude): %v", err)
	}
	got := backend.(*claudeBackend).cfg.LaunchPrefix
	want := []string{"start", "q36"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("New did not filter the launch prefix: got %v, want %v", got, want)
	}
}

// TestLaunchPrefixReachesACPFamilies is the regression guard for the half of
// the bug the report did not name: fixed_args used to ride on ExtraArgs, which
// the ACP backends never read, so on those families it was silently dropped
// rather than merely misordered. The prefix must land ahead of the hardcoded
// `acp` subcommand.
func TestLaunchPrefixReachesACPFamilies(t *testing.T) {
	t.Parallel()

	for _, family := range []string{"kimi", "hermes", "kiro", "reasonix", "qwenpaw", "dim", "zeroclaw"} {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			cfg := Config{LaunchPrefix: []string{"start", "q36"}, Logger: slog.Default()}
			argv := cfg.commandAt("wrapper").Argv("acp")
			if idx := prefixIndex(argv, []string{"start", "q36", "acp"}); idx != 0 {
				t.Fatalf("%s: prefix must precede the acp subcommand, got %v", family, argv)
			}
		})
	}
}

// TestDiscoveryCacheKeySeparatesLaunchPrefixes: one binary behind two
// different prefixes is two different catalogs.
func TestDiscoveryCacheKeySeparatesLaunchPrefixes(t *testing.T) {
	t.Parallel()

	q36 := discoveryCacheKey("claude", Command{Path: "/usr/local/bin/ccms", Prefix: []string{"start", "q36"}})
	opus := discoveryCacheKey("claude", Command{Path: "/usr/local/bin/ccms", Prefix: []string{"start", "opus"}})
	bare := discoveryCacheKey("claude", Command{Path: "/usr/local/bin/ccms"})

	if q36 == opus {
		t.Fatalf("two prefixes on one binary share a cache key: %q", q36)
	}
	if q36 == bare || opus == bare {
		t.Fatalf("a prefixed command must not share the bare binary's key: %q / %q / %q", q36, opus, bare)
	}
}

// TestCommandArgvNeverAliasesItsInputs: Argv results get appended to by
// callers, so a shared backing array would let one invocation's arguments
// leak into the next.
func TestCommandArgvNeverAliasesItsInputs(t *testing.T) {
	t.Parallel()

	prefix := []string{"start", "q36"}
	cmd := Command{Path: "ccms", Prefix: prefix}

	first := cmd.Argv("-p")
	first = append(first, "mutated")
	second := cmd.Argv("--version")

	if strings.Join(cmd.Prefix, "\x00") != "start\x00q36" {
		t.Fatalf("Argv mutated the command's prefix: %v", cmd.Prefix)
	}
	if prefixIndex(second, []string{"mutated"}) >= 0 {
		t.Fatalf("a later Argv saw an earlier call's append: %v", second)
	}
}

func TestRedactAgentCommandArgsPreservesOnlySafeFlagNames(t *testing.T) {
	t.Parallel()

	overlongFlag := "--" + strings.Repeat("a", maxLoggedAgentCommandFlagLen)
	args := []string{
		"--api-key", "api-key-secret",
		"--dash-prefixed-secret", "-sTk9xQZ-secretvalue",
		"--token=token-secret",
		"--header", "Authorization: Bearer header-secret",
		"-c", `model_providers.example.api_key="config-secret"`,
		"--future-secret", "future-value-secret",
		"prompt-secret",
		"--verbose",
		"-not-a-short-flag",
		overlongFlag,
	}
	want := []string{
		"--api-key", redactedAgentCommandArg,
		"--dash-prefixed-secret", redactedAgentCommandArg,
		"--token",
		"--header", redactedAgentCommandArg,
		"-c", redactedAgentCommandArg,
		"--future-secret", redactedAgentCommandArg,
		redactedAgentCommandArg,
		"--verbose",
		redactedAgentCommandArg,
		redactedAgentCommandArg,
	}

	got := redactAgentCommandArgs(args, nil)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("redactAgentCommandArgs = %v, want %v", got, want)
	}
}

func TestTrustedAgentCommandPositionalsFollowSourceIndexes(t *testing.T) {
	t.Parallel()

	invocationArgs := []string{"acp", "--api-key", "-sTk9xQZ-secretvalue", "acp"}
	finalArgs := []string{
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "wrapper.ps1",
		"start", "q36",
		"acp", "--api-key", "-sTk9xQZ-secretvalue", "acp",
	}
	cfg := Config{LaunchPrefix: []string{"start", "q36"}}
	trusted := cfg.trustedAgentCommandPositionals(finalArgs,
		newAgentCommandLogArgs(invocationArgs, trustAgentCommandPositional(0, "acp")))

	got := redactAgentCommandArgs(finalArgs, trusted)
	want := []string{
		redactedAgentCommandArg, redactedAgentCommandArg, redactedAgentCommandArg, redactedAgentCommandArg, redactedAgentCommandArg,
		redactedAgentCommandArg, redactedAgentCommandArg,
		"acp", "--api-key", redactedAgentCommandArg, redactedAgentCommandArg,
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("source-aware redaction = %v, want %v", got, want)
	}
}

func TestLogAgentCommandRedactsTextAndJSON(t *testing.T) {
	t.Parallel()

	args := []string{
		"--api-key", "api-key-secret",
		"--dash-prefixed-secret", "-sTk9xQZ-secretvalue",
		"--token=token-secret",
		"--header", "Authorization: Bearer header-secret",
		"-c", `model_providers.example.api_key="config-secret"`,
		"--future-secret", "future-value-secret",
		"prompt-secret",
	}
	secrets := []string{
		"api-key-secret",
		"-sTk9xQZ-secretvalue",
		"token-secret",
		"Authorization: Bearer header-secret",
		"config-secret",
		"future-value-secret",
		"prompt-secret",
	}

	for _, tc := range []struct {
		name    string
		handler func(*bytes.Buffer) slog.Handler
	}{
		{"text", func(buf *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buf, nil) }},
		{"json", func(buf *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(buf, nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cfg := Config{Logger: slog.New(tc.handler(&buf)), provider: "codex"}
			cmd := &exec.Cmd{Path: "/opt/multica/bin/codex", Args: append([]string{"codex"}, args...)}
			cfg.logAgentCommandWithPrompt(cmd, newAgentCommandLogArgs(args), 123)

			output := buf.String()
			for _, secret := range secrets {
				if strings.Contains(output, secret) {
					t.Errorf("%s log exposed %q: %s", tc.name, secret, output)
				}
			}
			for _, diagnostic := range []string{
				"agent command", "provider", "codex", "/opt/multica/bin/codex",
				"--api-key", "--token", "--header", "-c", "--future-secret",
				redactedAgentCommandArg, "arg_count", "prompt_bytes",
			} {
				if !strings.Contains(output, diagnostic) {
					t.Errorf("%s log omitted diagnostic %q: %s", tc.name, diagnostic, output)
				}
			}
		})
	}
}

func TestBackendFactoriesSetCommandLogProvider(t *testing.T) {
	t.Parallel()

	claude, err := New("claude", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(claude): %v", err)
	}
	if got := claude.(*claudeBackend).cfg.provider; got != "claude" {
		t.Fatalf("claude log provider = %q, want claude", got)
	}

	omp, err := NewRuntime("omp", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("NewRuntime(omp): %v", err)
	}
	if got := omp.(*piBackend).cfg.provider; got != "omp" {
		t.Fatalf("omp log provider = %q, want omp", got)
	}
}

// TestOnlyLaunchGoSpawnsRuntimeProcesses is the structural half of this fix.
//
// Distributed opt-in is what let ExtraArgs rot: it was honoured by six of
// twenty-one backends, and MULTICA_QWENPAW_ARGS shipped plumbed-but-dropped
// because nothing failed when a backend forgot to read it. Re-establishing the
// same convention for the launch prefix would rot the same way, so the rule is
// mechanical instead: every runtime process in this package is constructed in
// launch.go, which is the one place that applies the prefix.
//
// A new backend that calls os/exec directly fails here rather than silently
// reintroducing GH #7046.
func TestOnlyLaunchGoSpawnsRuntimeProcesses(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var offenders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// launch.go owns the boundary. The platform invocation rewrites resolve
		// a PowerShell host with exec.LookPath but never spawn the runtime.
		if name == "launch.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "exec" {
				return true
			}
			if sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext" {
				return true
			}
			offenders = append(offenders,
				fset.Position(call.Pos()).String()+": exec."+sel.Sel.Name)
			return true
		})
	}

	if len(offenders) > 0 {
		t.Fatalf("runtime processes must be built through Command.exec / Command.execVia in launch.go, "+
			"otherwise a custom runtime's fixed_args are dropped (GH #7046). Offending sites:\n%s",
			strings.Join(offenders, "\n"))
	}
}

func containsRuntimeArgReference(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			if node.Sel.Name == "Args" {
				found = true
				return false
			}
		case *ast.Ident:
			switch strings.ToLower(node.Name) {
			case "args", "cmdargs", "argv":
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

// TestOnlyLaunchGoLogsAgentCommandArgs keeps raw argv out of adapter-local log
// calls. Every runtime process log must flow through Config.logAgentCommand in
// launch.go, where values are redacted consistently for text and JSON handlers.
// The guard checks both common field labels and the expressions themselves, so
// renaming an "args" field to "argv" cannot bypass it while still passing
// cmd.Args, args, cmdArgs, or argv to a logger.
func TestOnlyLaunchGoLogsAgentCommandArgs(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var offenders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "launch.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Debug", "Info", "Warn", "Error", "Log", "LogAttrs":
			default:
				return true
			}
			for _, arg := range call.Args {
				literal, ok := arg.(*ast.BasicLit)
				if ok && literal.Kind == token.STRING &&
					(literal.Value == `"args"` || literal.Value == `"argv"` || literal.Value == `"agent command"`) {
					offenders = append(offenders, fset.Position(call.Pos()).String())
					break
				}
				if containsRuntimeArgReference(arg) {
					offenders = append(offenders, fset.Position(call.Pos()).String())
					break
				}
			}
			return true
		})
	}

	if len(offenders) > 0 {
		t.Fatalf("runtime argv logs must use Config.logAgentCommand in launch.go so values are redacted. Offending sites:\n%s",
			strings.Join(offenders, "\n"))
	}
}

// TestClaudeBackendLaunchesWrapperSubcommandFirst is the end-to-end proof:
// a real subprocess, spawned through the real backend, records the argv it was
// handed. The fake wrapper stands in for `ccms` and never touches a
// user-installed CLI.
func TestClaudeBackendLaunchesWrapperSubcommandFirst(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	argsFile := filepath.Join(dir, "argv")
	wrapper := filepath.Join(dir, "ccms")
	writeTestExecutable(t, wrapper, []byte("#!/bin/sh\n"+
		"for arg in \"$@\"; do printf '%s\\n' \"$arg\" >> \""+argsFile+"\"; done\n"+
		"exit 0\n"))

	backend, err := New("claude", Config{
		ExecutablePath: wrapper,
		LaunchPrefix:   []string{"start", "q36"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("New(claude): %v", err)
	}
	session, err := backend.Execute(context.Background(), "hello", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("wrapper recorded no argv: %v", err)
	}
	argv := strings.Fields(string(bytes.TrimSpace(raw)))
	if idx := prefixIndex(argv, []string{"start", "q36"}); idx != 0 {
		t.Fatalf("wrapper was launched as %v; `start q36` must lead", argv)
	}
	if idx := prefixIndex(argv, []string{"-p"}); idx < 2 {
		t.Fatalf("-p reached the wrapper before its subcommand: %v", argv)
	}
}

// TestLaunchPrefixOutranksExtraArgs locks the full argv contract that replaced
// "fixed_args are prepended to ExtraArgs" (#4408). The old arrangement put the
// prefix inside ExtraArgs, which lands *after* the protocol flags — and which
// fifteen of the twenty-one backends never read at all. The prefix now has its
// own slot ahead of everything.
func TestLaunchPrefixOutranksExtraArgs(t *testing.T) {
	t.Parallel()

	cfg := Config{LaunchPrefix: []string{"start", "q36"}, Logger: slog.Default()}
	argv := cfg.commandAt("ccms").Argv(buildClaudeArgs(ExecOptions{
		ExtraArgs:  []string{"--extra-arg"},
		CustomArgs: []string{"--custom-arg"},
	}, slog.Default())...)

	prefix := prefixIndex(argv, []string{"start", "q36"})
	protocol := prefixIndex(argv, []string{"-p"})
	extra := prefixIndex(argv, []string{"--extra-arg"})
	custom := prefixIndex(argv, []string{"--custom-arg"})

	if prefix != 0 {
		t.Fatalf("launch prefix must lead: %v", argv)
	}
	if !(prefix < protocol && protocol < extra && extra < custom) {
		t.Fatalf("want prefix < protocol < extra < custom, got %d/%d/%d/%d: %v",
			prefix, protocol, extra, custom, argv)
	}
}

// TestBuiltinRuntimeIdentitiesFilterLaunchPrefix: NewRuntime composes New, so
// a runtime identity (omp) inherits the same prefix policy as its family.
func TestBuiltinRuntimeIdentitiesFilterLaunchPrefix(t *testing.T) {
	t.Parallel()

	backend, err := ResolveBackend("omp", Config{
		ExecutablePath: "wrapper",
		LaunchPrefix:   []string{"start", "q36", "-p"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("ResolveBackend(omp): %v", err)
	}
	got := backend.(*piBackend).cfg.LaunchPrefix
	if strings.Join(got, "\x00") != "start\x00q36" {
		t.Fatalf("runtime identity did not inherit prefix filtering: %v", got)
	}
}

// TestCodexLaunchPrefixCannotShadowManagedConfig: prefix-first wins ordinary
// flag conflicts on purpose, but Codex `-c key=value` beats the daemon-written
// config.toml from any argv position. A fixed_args entry aimed at a managed
// namespace has to be removed, not merely outranked.
func TestCodexLaunchPrefixCannotShadowManagedConfig(t *testing.T) {
	t.Parallel()

	cmd := Command{Prefix: []string{
		"proxy", "run",
		"-c", "mcp_servers.evil.command=/bin/sh",
		"-c", "shell_environment_policy.inherit=all",
		"--model", "gpt-5",
	}}

	filtered := cmd.
		withFilteredPrefix(func(p []string) []string { return filterCodexShellEnvConfigOverrides(p, slog.Default()) }).
		withFilteredPrefix(func(p []string) []string { return filterCodexCustomConfigOverrides(p, slog.Default()) })

	want := []string{"proxy", "run", "--model", "gpt-5"}
	if strings.Join(filtered.Prefix, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("prefix = %v, want the managed-namespace overrides dropped and %v kept",
			filtered.Prefix, want)
	}
}

// TestFilterLaunchPrefixKeepsPositionalCollidingWithSubcommand is Elon's
// must-fix 2. The blocklists are shared with custom_args, and several carry
// positional tokens — hermes/qwenpaw block `acp`, traecli blocks `acp` and
// `serve`, grok blocks `agent`, `stdio` and `serve`. Those entries exist to
// stop someone re-issuing the backend's own subcommand through custom_args.
// A launch prefix is the opposite case: the token is the command's identity,
// and dropping it rewrites the operator's command into a different one.
func TestFilterLaunchPrefixKeepsPositionalCollidingWithSubcommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		family string
		prefix []string
		want   []string
	}{
		{"hermes", []string{"acp", "tenant"}, []string{"acp", "tenant"}},
		{"qwenpaw", []string{"acp", "tenant"}, []string{"acp", "tenant"}},
		{"traecli", []string{"serve", "acp"}, []string{"serve", "acp"}},
		{"grok", []string{"agent", "stdio"}, []string{"agent", "stdio"}},
		{"grok", []string{"leader", "headless"}, []string{"leader", "headless"}},
		// Flags in the same prefix are still filtered; grok blocks -p.
		{"grok", []string{"agent", "-p", "stdio"}, []string{"agent", "stdio"}},
	}
	for _, tc := range cases {
		t.Run(tc.family+"/"+strings.Join(tc.prefix, "_"), func(t *testing.T) {
			t.Parallel()
			got := filterLaunchPrefix(tc.prefix, tc.family, slog.Default())
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Fatalf("filterLaunchPrefix(%v, %s) = %v, want %v", tc.prefix, tc.family, got, tc.want)
			}
		})
	}
}

// TestFilterLaunchPrefixConsumesBlockedFlagValue: dropping a blocked flag must
// take its value with it, or the leftover value token would be read as a
// subcommand — precisely the kind of token this filter otherwise preserves.
func TestFilterLaunchPrefixConsumesBlockedFlagValue(t *testing.T) {
	t.Parallel()

	got := filterLaunchPrefix(
		[]string{"start", "--permission-mode", "ask", "q36", "--output-format=text"},
		"claude", slog.Default())
	want := []string{"start", "q36"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("filterLaunchPrefix = %v, want %v", got, want)
	}
}

// TestDimLaunchPrefixFiltersBlockedFlags proves the Dim launch-prefix safety
// policy: allowed positional tokens reach the command ahead of the hardcoded
// `acp` subcommand, while protocol-breaking flags (--help, --auth-setup,
// --remote, -h, acp) are stripped. Without this the fixed_args regression
// this round fixed could return silently.
func TestDimLaunchPrefixFiltersBlockedFlags(t *testing.T) {
	t.Parallel()

	// Allowed positional prefix tokens survive and precede `acp`.
	cfg := Config{LaunchPrefix: []string{"start", "q36"}, Logger: slog.Default()}
	argv := cfg.commandAt("wrapper").Argv("acp")
	if idx := prefixIndex(argv, []string{"start", "q36", "acp"}); idx != 0 {
		t.Fatalf("dim: allowed prefix must precede the acp subcommand, got %v", argv)
	}

	// Protocol-breaking flags are removed from the prefix.
	got := filterLaunchPrefix(
		[]string{"start", "--help", "--auth-setup", "--remote", "-h", "q36"},
		"dim", slog.Default())
	want := []string{"start", "q36"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("dim: blocked flags must be stripped, got %v, want %v", got, want)
	}
}

// TestZeroclawLaunchPrefixFiltersBlockedFlags proves the ZeroClaw
// launch-prefix safety policy: allowed positional tokens reach the command
// ahead of the hardcoded `acp` subcommand, while protocol-breaking flags
// (--help, -h, login, --login, --auth, acp) are stripped.
func TestZeroclawLaunchPrefixFiltersBlockedFlags(t *testing.T) {
	t.Parallel()

	// Allowed positional prefix tokens survive and precede `acp`.
	cfg := Config{LaunchPrefix: []string{"start", "q36"}, Logger: slog.Default()}
	argv := cfg.commandAt("wrapper").Argv("acp")
	if idx := prefixIndex(argv, []string{"start", "q36", "acp"}); idx != 0 {
		t.Fatalf("zeroclaw: allowed prefix must precede the acp subcommand, got %v", argv)
	}

	// Protocol-breaking flags are removed from the prefix.
	got := filterLaunchPrefix(
		[]string{"start", "--help", "--login", "--auth", "-h", "q36"},
		"zeroclaw", slog.Default())
	want := []string{"start", "q36"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("zeroclaw: blocked flags must be stripped, got %v, want %v", got, want)
	}
}

// TestHermesLaunchArgvMatchesBackendAssembly is the root of the second-round
// Hermes finding: the daemon must resolve the profile from the argv the backend
// actually builds, not from a concatenation that leaves out `acp`.
//
// With fixed_args `--model` and custom_args `-p research`, the two disagree.
// `--model` is a value-taking flag, so the approximation `--model -p research`
// consumes `-p` as its value and finds no selection at all, while the real
// `--model acp -p research` skips `--model acp` and selects `research`. Seeding
// the overlay from the first answer while the process runs the second is a
// silent config mismatch.
func TestHermesLaunchArgvMatchesBackendAssembly(t *testing.T) {
	t.Parallel()

	prefix := []string{"--model"}
	custom := []string{"-p", "research"}

	argv := HermesLaunchArgv(prefix, custom, slog.Default())
	if want := []string{"--model", "acp", "-p", "research"}; strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("HermesLaunchArgv = %v, want %v", argv, want)
	}
	// It must equal what the backend hands the launch boundary.
	backend := Command{Prefix: prefix}.Argv(hermesCLIArgs(custom, slog.Default())...)
	if strings.Join(argv, "\x00") != strings.Join(backend, "\x00") {
		t.Fatalf("resolver argv %v diverges from backend argv %v", argv, backend)
	}
	sel := ParseHermesProfileArgs(argv)
	if !sel.Found || sel.Name != "research" {
		t.Fatalf("selection = %+v, want research — the profile the process really reads", sel)
	}
	// The approximation this replaced is what got it wrong.
	if naive := ParseHermesProfileArgs(append(append([]string{}, prefix...), custom...)); naive.Found {
		t.Fatalf("expected the prefix++custom approximation to disagree, got %+v", naive)
	}
}

// TestStripHermesProfileSelectorsSpansRegionBoundary: a launch prefix ending in
// a bare `-p` captures the backend's own `acp` token as its profile value.
// Neither region holds a complete selection, so stripping them separately
// leaves the selector live and the task walks out of the overlay.
func TestStripHermesProfileSelectorsSpansRegionBoundary(t *testing.T) {
	t.Parallel()

	prefix, custom := StripHermesProfileSelectors([]string{"-p"}, []string{"research", "--yolo"}, slog.Default())

	if sel := ParseHermesProfileArgs(Command{Prefix: prefix}.Argv(hermesCLIArgsFrom(custom)...)); sel.Found {
		t.Fatalf("launched argv still selects %q: prefix=%v custom=%v", sel.Name, prefix, custom)
	}
	if len(prefix) != 0 {
		t.Fatalf("the straddling `-p` must be removed from the prefix, got %v", prefix)
	}
	if strings.Join(custom, "\x00") != strings.Join([]string{"research", "--yolo"}, "\x00") {
		t.Fatalf("custom args must survive intact, got %v", custom)
	}
}

// TestStripHermesProfileSelectorsRemovesEveryOccurrence: hermes honours the
// first selection and ignores the rest, so removing one promotes the next.
// With prefix and custom args configured separately, several selections is
// ordinary configuration rather than a user mistake.
func TestStripHermesProfileSelectorsRemovesEveryOccurrence(t *testing.T) {
	t.Parallel()

	prefix, custom := StripHermesProfileSelectors(
		[]string{"wrapper-sub", "-p", "research"},
		[]string{"--profile=ops", "--model", "x", "-p", "third"},
		slog.Default())

	argv := Command{Prefix: prefix}.Argv(hermesCLIArgsFrom(custom)...)
	if sel := ParseHermesProfileArgs(argv); sel.Found {
		t.Fatalf("a selection survived: %+v in %v", sel, argv)
	}
	if strings.Join(prefix, "\x00") != "wrapper-sub" {
		t.Fatalf("prefix = %v, want the command identity kept and the selector gone", prefix)
	}
	if strings.Join(custom, "\x00") != strings.Join([]string{"--model", "x"}, "\x00") {
		t.Fatalf("custom = %v, want unrelated args kept", custom)
	}
}

// TestStripHermesProfileSelectorsLeavesInvalidSelectionAlone: a value hermes
// itself discards redirects nothing, so removing it would only mangle the
// user's argv.
func TestStripHermesProfileSelectorsLeavesInvalidSelectionAlone(t *testing.T) {
	t.Parallel()

	prefix, custom := StripHermesProfileSelectors(nil, []string{"-p", "NOT_A_PROFILE"}, slog.Default())
	if len(prefix) != 0 {
		t.Fatalf("prefix = %v, want empty", prefix)
	}
	if strings.Join(custom, "\x00") != strings.Join([]string{"-p", "NOT_A_PROFILE"}, "\x00") {
		t.Fatalf("custom = %v, want the discarded selection left untouched", custom)
	}
}

// TestCodexLaunchPrefixCannotOverrideExplicitFastMode is Elon's must-fix 3.
// `--disable fast_mode` beats `--enable fast_mode` regardless of argv order and
// `-c features.fast_mode=false` beats config.toml from any position, so being
// first does not make the prefix lose. Both spellings have to be removed.
func TestCodexLaunchPrefixCannotOverrideExplicitFastMode(t *testing.T) {
	t.Parallel()

	for _, prefix := range [][]string{
		{"proxy", "run", "--disable", "fast_mode"},
		{"proxy", "run", "--disable=fast_mode"},
		{"proxy", "run", "-c", "features.fast_mode=false"},
	} {
		t.Run(strings.Join(prefix, "_"), func(t *testing.T) {
			t.Parallel()
			got := stripCodexFastModeConflicts(prefix, slog.Default())
			want := []string{"proxy", "run"}
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("stripCodexFastModeConflicts(%v) = %v, want %v", prefix, got, want)
			}
			// The managed enable is appended once, by buildCodexArgs, not here.
			if prefixIndex(got, []string{"--enable"}) >= 0 {
				t.Fatalf("the strip helper must not append the managed enable: %v", got)
			}
		})
	}
}

// TestEnforceCodexFastModeStillAppendsEnableOnce guards the split: pulling the
// removal out of enforceCodexFastMode must not change what it produces.
func TestEnforceCodexFastModeStillAppendsEnableOnce(t *testing.T) {
	t.Parallel()

	got := enforceCodexFastMode([]string{"--disable", "fast_mode", "--model", "gpt-5"}, slog.Default())
	want := []string{"--model", "gpt-5", "--enable", "fast_mode"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("enforceCodexFastMode = %v, want %v", got, want)
	}
}
