package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// FIR-3212 slice 2. A hand-maintained matrix rots the moment someone adds a
// backend, and a rotten matrix is worse than none: it answers confidently and
// wrongly. These tests are what make the table trustworthy.
//
// Three guards, each closing a different hole:
//  1. drift — the matrix covers exactly the backends New() registers, derived by
//     parsing the switch rather than by copying it.
//  2. self-consistency — the matrix agrees with cerebro_prompt_mode.go.
//  3. contract — the flags we emit exist in the CLI installed on this host.
//
// Only (3) needs a binary; it skips when the CLI is absent, so CI stays green.

// registeredProviders extracts the case values of the switch in New() straight
// from agent.go's AST.
//
// Parsing the source rather than hardcoding a list is the entire point: a
// hardcoded list is another copy that drifts, and the copy would drift in the
// same direction as the matrix, so the test would keep passing while both were
// wrong. The AST cannot disagree with the compiler.
func registeredProviders(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "agent.go", nil, 0)
	if err != nil {
		t.Fatalf("parse agent.go: %v", err)
	}

	// Provider constants are referenced by identifier in the switch (e.g.
	// firtalGatewayProvider), so resolve them to their string values.
	constValues := map[string]string{
		"firtalGatewayProvider": firtalGatewayProvider,
		"openaiEUProvider":      openaiEUProvider,
		"firtalLocalProvider":   firtalLocalProvider,
	}

	var providers []string
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "New" {
			return true
		}
		ast.Inspect(fn, func(inner ast.Node) bool {
			clause, ok := inner.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				switch v := expr.(type) {
				case *ast.BasicLit:
					if v.Kind == token.STRING {
						if s, err := strconv.Unquote(v.Value); err == nil {
							providers = append(providers, s)
						}
					}
				case *ast.Ident:
					if s, ok := constValues[v.Name]; ok {
						providers = append(providers, s)
					} else {
						t.Fatalf("New() has a case on unknown identifier %q; "+
							"add it to constValues in this test", v.Name)
					}
				}
			}
			return true
		})
		return false
	})

	if len(providers) == 0 {
		t.Fatal("parsed no provider cases from New(); the switch shape changed and this test is now blind")
	}
	sort.Strings(providers)
	return providers
}

// The matrix must describe every backend that can actually run, and no backend
// that cannot. A provider missing here answers ok=false and every caller falls
// back to "assume supported" — which is how a deny-policy silently stops
// applying to a brand-new runtime.
func TestExecOptionsMatrixCoversEveryRegisteredBackend(t *testing.T) {
	registered := registeredProviders(t)
	for _, provider := range registered {
		if _, ok := ExecOptionsSupportFor(provider); !ok {
			t.Errorf("New() registers backend %q but the ExecOptions matrix has no entry; "+
				"add one to execOptionsSupport in cerebro_exec_options_matrix.go", provider)
		}
	}

	inRegistry := map[string]bool{}
	for _, p := range registered {
		inRegistry[p] = true
	}
	for _, provider := range ExecOptionsMatrixProviders() {
		if !inRegistry[provider] {
			t.Errorf("ExecOptions matrix describes %q, which New() does not register; "+
				"the backend was removed and the matrix now lies about it", provider)
		}
	}

	// Sanity-check the parse itself: 15 backends at the time of writing. A bare
	// count assertion would be brittle, but zero-or-tiny means the AST walk
	// silently stopped finding cases.
	if len(registered) < 10 {
		t.Fatalf("parsed only %d providers from New(); expected the full backend set", len(registered))
	}
}

// Every provider IsSupportedType accepts must be in the matrix. This is the
// same guard from the other direction, and it uses the real constructor rather
// than the AST, so a divergence between the two would surface here.
func TestExecOptionsMatrixAgreesWithIsSupportedType(t *testing.T) {
	for _, provider := range ExecOptionsMatrixProviders() {
		if !IsSupportedType(provider) {
			t.Errorf("matrix describes %q but IsSupportedType(%q) is false", provider, provider)
		}
	}
}

// The matrix and the slice-1 prompt table are two descriptions of one fact.
// They are allowed to be shaped differently; they are not allowed to disagree.
func TestExecOptionsMatrixAgreesWithSystemPromptSupport(t *testing.T) {
	for _, provider := range ExecOptionsMatrixProviders() {
		support, ok := SystemPromptSupportFor(provider)
		if !ok {
			t.Errorf("provider %q is in the ExecOptions matrix but has no system-prompt entry", provider)
			continue
		}
		handling, _ := ExecOptionsHandling(provider, FieldSystemPrompt)

		// A provider with delivery modes must be recorded as honouring the
		// field; one with no modes must not be.
		acceptsPrompt := len(support.Modes) > 0
		if acceptsPrompt && handling != HandlingHonoured {
			t.Errorf("provider %q advertises system-prompt modes %v but the matrix records %q",
				provider, support.Modes, handling)
		}
		if !acceptsPrompt && handling == HandlingHonoured {
			t.Errorf("provider %q advertises no system-prompt modes but the matrix claims it is honoured", provider)
		}
	}
}

// Pins the security finding this slice exists to surface: a deny-policy is
// honoured by exactly claude, cursor and gemini, and silently dropped by the
// other 12 (daemon.go:3954 sends it to all of them).
//
// The assertion is deliberately exact. If a backend gains --disallowedTools
// support this test fails and someone updates the matrix on purpose; if one
// loses it, it fails too. Either way the number stops being folklore.
func TestDisallowedToolsIsHonouredByExactlyThreeBackends(t *testing.T) {
	var honouring []string
	for _, provider := range ExecOptionsMatrixProviders() {
		if h, ok := ExecOptionsHandling(provider, FieldDisallowedTools); ok && h == HandlingHonoured {
			honouring = append(honouring, provider)
		}
	}
	sort.Strings(honouring)

	want := []string{"claude", "cursor", "gemini"}
	if strings.Join(honouring, ",") != strings.Join(want, ",") {
		t.Errorf("tool deny-policy honoured by %v, want %v — if this changed on purpose, "+
			"update the matrix and docs/cerebro-patches.md's fail-closed note", honouring, want)
	}

	// The complement is the actual exposure, and it must not shrink by accident.
	silent := ProvidersSilentlyIgnoring(FieldDisallowedTools)
	if len(silent) != len(ExecOptionsMatrixProviders())-len(want) {
		t.Errorf("expected every non-honouring backend to ignore the deny-policy silently, got %v", silent)
	}
}

// An unknown provider must read as "unknown", never as "supports nothing" —
// the StaticCatalog contract. Getting this backwards would hide a control that
// works, which is the failure mode the whole ok-flag convention prevents.
func TestUnknownProviderIsUnknownNotUnsupported(t *testing.T) {
	const unknown = "does-not-exist"

	if _, ok := ExecOptionsSupportFor(unknown); ok {
		t.Fatal("unknown provider must not report ok=true")
	}
	if _, ok := ExecOptionsHandling(unknown, FieldMaxTurns); ok {
		t.Fatal("ExecOptionsHandling must report ok=false for an unknown provider")
	}
	if !ExecOptionsHonours(unknown, FieldMaxTurns) {
		t.Fatal("an unknown provider must be assumed to honour a field; " +
			"assuming otherwise removes a control that may work")
	}
}

// A known provider with no entry for a field ignores it silently — and says so
// with ok=true, because we do know the answer: nothing happens.
func TestKnownProviderMissingFieldIsSilentIgnore(t *testing.T) {
	// copilot has no MaxTurns branch anywhere in copilot.go.
	handling, ok := ExecOptionsHandling("copilot", FieldMaxTurns)
	if !ok {
		t.Fatal("copilot is a known provider; ok must be true")
	}
	if handling != HandlingIgnoredSilent {
		t.Fatalf("copilot MaxTurns handling = %q, want %q", handling, HandlingIgnoredSilent)
	}
	if ExecOptionsHonours("copilot", FieldMaxTurns) {
		t.Fatal("copilot must not be reported as honouring MaxTurns")
	}
}

// MaxTurns is the trap a grep-derived matrix falls into: opencode.go mentions
// opts.MaxTurns, but only to warn that it is ignored. "The field is referenced"
// and "the field works" are different claims.
func TestMaxTurnsHonouredOnlyByClaude(t *testing.T) {
	for _, provider := range ExecOptionsMatrixProviders() {
		honours := ExecOptionsHonours(provider, FieldMaxTurns)
		if provider == "claude" && !honours {
			t.Error("claude must honour MaxTurns (claude.go:640 --max-turns)")
		}
		if provider != "claude" && honours {
			t.Errorf("provider %q must not be recorded as honouring MaxTurns", provider)
		}
	}

	// opencode is the one backend that tells the operator, and that is the
	// behaviour every other silent-ignore should grow into.
	if h, _ := ExecOptionsHandling("opencode", FieldMaxTurns); h != HandlingIgnoredLogged {
		t.Errorf("opencode MaxTurns handling = %q, want %q (opencode.go:78 warns)", h, HandlingIgnoredLogged)
	}
}

// --- Contract tests against the CLI actually installed on this host ---------
//
// The matrix claims a flag reaches a CLI. Only the CLI can confirm it. These
// spawn the real binary and skip when it is absent, following the convention
// cerebro_opencode_flag_contract_test.go already set in this package.
//
// Two things worth knowing before reading a failure here:
//
//   - CI never runs these. .github/workflows/ci.yml's backend job is a bare
//     ubuntu-latest with Go, Postgres and Redis and installs no agent CLI, so
//     they skip there. Their value is local, on a machine that has the runtimes.
//   - The codex first-turn tests in this package fail intermittently under
//     machine load (they budget 5s of wall clock inside t.Parallel()). That is
//     pre-existing and NOT caused by the subprocesses spawned here — verified by
//     running the package suite on a clean tree, which flaked at the same rate.
//     Do not "fix" it by deleting these tests.
//
// On this host claude, codex, opencode, gemini and hermes are installed; kiro,
// cursor, pi, kimi, copilot, openclaw and antigravity are not, so their rows
// stay unverified-by-binary — which the matrix states rather than papers over.
func helpOutput(t *testing.T, bin string, args ...string) string {
	t.Helper()
	path, err := exec.LookPath(bin)
	if err != nil {
		t.Skipf("%s not installed on this host; skipping real-binary contract test", bin)
	}
	// --help exits non-zero on some CLIs while still printing the flag table,
	// so the output is what matters, not the exit code.
	out, _ := exec.Command(path, args...).CombinedOutput()
	if len(out) == 0 {
		t.Fatalf("%s %v produced no output", bin, args)
	}
	return string(out)
}

// claudeRejectsFlag reports whether the installed claude rejects flag as unknown.
//
// Scraping --help is not enough, and finding that out cost a false alarm: claude
// 2.1.209 accepts --max-turns but does not list it in --help. Absence from the
// help text is therefore not evidence of absence — the same asymmetry the matrix
// itself encodes, now applied to our own measurement.
//
// So we ask the parser instead of the docs. A deliberately invalid --model makes
// the CLI exit locally, but only AFTER option parsing, so an unknown flag still
// reports "unknown option" while a valid one falls through to the model error.
// No API call, no network, no token spend.
func claudeRejectsFlag(t *testing.T, flagAndValue ...string) bool {
	t.Helper()
	path, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude not installed on this host; skipping real-binary flag contract test")
	}

	args := append([]string{"-p", "--model", "cerebro-invalid-model-probe"}, flagAndValue...)
	args = append(args, "probe")
	out, _ := exec.Command(path, args...).CombinedOutput()

	return strings.Contains(string(out), "unknown option")
}

// The matrix claims claude honours MaxTurns, the deny-policy and Model. Each
// claim is flag-shaped, so the installed binary can settle it. This is the guard
// that would have caught OpenCode's --prompt on day one.
func TestInstalledClaudeAcceptsFlagsTheMatrixClaims(t *testing.T) {
	// Prove the probe can actually detect a rejection. Without this, a probe
	// that silently stopped working would pass every assertion below and the
	// test would be decorative.
	if !claudeRejectsFlag(t, "--cerebro-definitely-not-a-flag") {
		t.Fatal("probe is blind: claude did not reject a bogus flag, so the assertions below prove nothing")
	}

	for _, tc := range []struct {
		field ExecOptionField
		args  []string
	}{
		{FieldMaxTurns, []string{"--max-turns", "3"}},
		{FieldDisallowedTools, []string{"--disallowedTools", "mcp__x__y"}},
	} {
		if !ExecOptionsHonours("claude", tc.field) {
			t.Fatalf("test out of sync with matrix: claude should honour %s", tc.field)
		}
		if claudeRejectsFlag(t, tc.args...) {
			t.Errorf("matrix claims claude honours %s (%v) but the installed CLI rejects the flag; "+
				"every run setting it dies with a usage dump", tc.field, tc.args)
		}
	}
}

// The deny-policy is the security-relevant row: if a CLI we record as silently
// ignoring it actually grew the flag, our matrix is under-reporting a
// capability and an operator is being told a policy does not apply when it now
// could. gemini and cursor are already recorded as honouring it via managed
// config, so opencode is the meaningful probe among the installed CLIs.
func TestInstalledOpencodeStillHasNoDenyToolsFlag(t *testing.T) {
	help := helpOutput(t, "opencode", "run", "--help")

	for _, flag := range []string{"--disallowedTools", "--disallowed-tools"} {
		if strings.Contains(help, flag) {
			t.Errorf("installed opencode now advertises %s; the matrix records the deny-policy "+
				"as silently ignored and is now wrong — wire it up in opencode.go", flag)
		}
	}
	// The flags the matrix does claim must still be there.
	for _, flag := range []string{"--model", "--variant", "--session"} {
		if !strings.Contains(help, flag) {
			t.Errorf("matrix claims opencode honours %s but the installed CLI does not advertise it", flag)
		}
	}
}

// gemini --allowed-tools is deprecated in favour of a Policy Engine (0.44.1).
// The matrix must never grow an allowlist row for gemini on the strength of a
// flag name alone; this test is the tripwire for that, and the reason the
// matrix is derived from the binary rather than from memory.
func TestInstalledGeminiAllowedToolsIsStillDeprecated(t *testing.T) {
	help := helpOutput(t, "gemini", "--help")

	if !strings.Contains(help, "--allowed-tools") {
		t.Skip("installed gemini no longer advertises --allowed-tools at all; nothing to warn about")
	}
	if !strings.Contains(strings.ToLower(help), "deprecated") {
		t.Error("installed gemini no longer marks --allowed-tools deprecated; " +
			"re-read the Policy Engine docs before adding an allowlist dimension for gemini")
	}
}
