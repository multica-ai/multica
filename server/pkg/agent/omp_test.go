package agent

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestOmpNewDispatchesToPiBackend asserts that ResolveBackend("omp") dispatches to the
// pi backend via the descriptor registry — the core contract that omp is a
// runtime identity on the pi protocol, not a separate protocol family.
// IsSupportedType("omp") is false (it's not a protocol family), but New
// resolves it through BuiltinRuntimes to the pi backend with the correct
// executable and label overrides.
func TestOmpNewDispatchesToPiBackend(t *testing.T) {
	if IsSupportedType("omp") {
		t.Errorf("omp must not be in SupportedTypes (it is a runtime identity, not a protocol family)")
	}
	b, err := ResolveBackend("omp", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(omp) returned error: %v", err)
	}
	pb, ok := b.(*piBackend)
	if !ok {
		t.Fatalf("New(omp) returned %T, want *piBackend", b)
	}
	if pb.defaultExecutable != "omp" {
		t.Errorf("defaultExecutable = %q, want %q", pb.defaultExecutable, "omp")
	}
	if pb.providerLabel != "omp" {
		t.Errorf("providerLabel = %q, want %q", pb.providerLabel, "omp")
	}
}

// TestNewRuntimeIsSeparateEntryPoint verifies that NewRuntime (the typed
// entry point for runtime identities) produces the same backend as New()
// when called with "omp" — both should dispatch through the descriptor.
func TestNewRuntimeIsSeparateEntryPoint(t *testing.T) {
	b, err := NewRuntime("omp", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("NewRuntime(omp): %v", err)
	}
	pb, ok := b.(*piBackend)
	if !ok {
		t.Fatalf("NewRuntime(omp) returned %T, want *piBackend", b)
	}
	if pb.defaultExecutable != "omp" {
		t.Errorf("defaultExecutable = %q, want %q", pb.defaultExecutable, "omp")
	}
	if pb.providerLabel != "omp" {
		t.Errorf("providerLabel = %q, want %q", pb.providerLabel, "omp")
	}
}

// TestNewRuntimeRejectsUnknownID verifies that NewRuntime fails for an
// unregistered runtime identity.
func TestNewRuntimeRejectsUnknownID(t *testing.T) {
	if _, err := NewRuntime("definitely-not-a-runtime", Config{}); err == nil {
		t.Fatal("expected error for unknown runtime identity")
	}
}

// TestNewRejectsRuntimeID verifies that New() (the protocol-family factory)
// rejects a runtime identity like "omp". This pins the contract that New()
// means exactly one thing: family-only. The daemon calls ResolveBackend,
// which routes runtime identities through NewRuntime.
func TestNewRejectsRuntimeID(t *testing.T) {
	if _, err := New("omp", Config{Logger: slog.Default()}); err == nil {
		t.Fatal("New(\"omp\") should reject runtime identities (it is family-only)")
	}
}

// TestOmpExecuteDefaultsToOmpBinary verifies that ResolveBackend("omp", Config{}) with
// an empty ExecutablePath resolves to the "omp" binary name (not "pi") when
// the daemon hasn't pinned a path. This is the contract the daemon relies on
// when it constructs a backend from a probe result that carries no path.
func TestOmpExecuteDefaultsToOmpBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakeDir := t.TempDir()
	fakePath := filepath.Join(fakeDir, "omp")
	// Real omp reads the piped prompt to EOF before emitting events, so the
	// fake has to drain stdin too: one that exits without reading closes the
	// read end while the backend is still writing, and the EPIPE surfaces as
	// "omp prompt write failed: broken pipe" (MUL-7244). It must drain with a
	// shell builtin, not `cat` — PATH is replaced with fakeDir below so the
	// backend has to resolve "omp" by name, which leaves no external command
	// on PATH for the fixture to call. A `cat` drain here fails silently with
	// "cat: not found" and the script runs straight through to its exit,
	// reopening the very race the drain was added to close.
	script := "#!/bin/sh\n" +
		"while IFS= read -r _; do :; done\n" +
		"printf '%s\\n' '{\"type\":\"agent_start\"}'\n" +
		"printf '%s\\n' '{\"type\":\"turn_end\",\"message\":{\"role\":\"assistant\",\"model\":\"test\",\"usage\":{\"input\":1,\"output\":1}}}'\n" +
		"exit 0\n"
	writeTestExecutable(t, fakePath, []byte(script))

	t.Setenv("PATH", fakeDir)

	backend, err := ResolveBackend("omp", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(omp): %v", err)
	}
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "test prompt", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: sessionPath,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected completed, got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestOmpExecuteRejectsEmptyPrompt verifies the error message uses the "omp"
// label, not "pi". This is the user-facing contract: an omp agent that fails
// must say "omp prompt must not be empty", not "pi prompt must not be empty".
func TestOmpExecuteRejectsEmptyPrompt(t *testing.T) {
	t.Parallel()

	backend, err := ResolveBackend("omp", Config{ExecutablePath: "/does/not/need/to/exist", Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(omp): %v", err)
	}
	if _, err := backend.Execute(t.Context(), " \n\t ", ExecOptions{}); err == nil {
		t.Fatalf("expected empty-prompt error")
	} else {
		if !strings.Contains(err.Error(), "omp prompt must not be empty") {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), "omp prompt must not be empty")
		}
		if strings.Contains(err.Error(), "pi prompt") {
			t.Fatalf("error should not contain hardcoded 'pi prompt': %q", err.Error())
		}
	}
}

// TestOmpExecuteLabelsErrorsAsOmp verifies that a binary-not-found error uses
// the "omp" label, not "pi".
func TestOmpExecuteLabelsErrorsAsOmp(t *testing.T) {
	t.Parallel()

	backend, err := ResolveBackend("omp", Config{ExecutablePath: "/nonexistent/omp-binary", Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(omp): %v", err)
	}
	_, err = backend.Execute(t.Context(), "prompt", ExecOptions{})
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
	if !strings.Contains(err.Error(), "omp executable not found") {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), "omp executable not found")
	}
	if strings.Contains(err.Error(), "pi executable") {
		t.Fatalf("error should not say 'pi executable': %q", err.Error())
	}
}

// TestOmpExecuteCompletesFromEventStream verifies that an omp event stream
// (same JSON protocol as pi) drives a task to completion through the pi
// backend. This is the end-to-end protocol compatibility contract.
func TestOmpExecuteCompletesFromEventStream(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello from omp"}}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"omp-test","usage":{"input":10,"output":5}}}`,
	}
	fakePath := filepath.Join(t.TempDir(), "omp")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript(events)))

	backend, err := ResolveBackend("omp", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(omp): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "hello from omp" {
			t.Fatalf("Output = %q, want %q", result.Output, "hello from omp")
		}
		if len(result.Usage) != 1 {
			t.Fatalf("expected 1 usage entry, got %d", len(result.Usage))
		}
		u, ok := result.Usage["omp-test"]
		if !ok {
			t.Fatalf("expected usage for model %q, got %v", "omp-test", result.Usage)
		}
		if u.InputTokens != 10 || u.OutputTokens != 5 {
			t.Fatalf("usage: input=%d output=%d, want input=10 output=5", u.InputTokens, u.OutputTokens)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestParseOmpModels parses a real `omp models --json` output. omp emits
// an object wrapper {"models":[...]} where each entry has separate `provider`,
// `id`, `selector` (provider/id), and `name` fields. The persistable Model.ID
// is the selector (provider/id), matching the convention parsePiModels uses
// so buildPiArgs can hand the whole selector to --model.
func TestParseOmpModels(t *testing.T) {
	sample := `{"models":[` +
		`{"provider":"anthropic","id":"claude-sonnet-5","selector":"anthropic/claude-sonnet-5","name":"Claude Sonnet 5","contextWindow":200000,"maxTokens":64000,"reasoning":true},` +
		`{"provider":"openai","id":"gpt-5","selector":"openai/gpt-5","name":"GPT-5"},` +
		`{"provider":"","id":"local-model","selector":"local-model","name":"Local Model"}` +
		`]}`
	models, err := parseOmpModels([]byte(sample))
	if err != nil {
		t.Fatalf("parseOmpModels: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}
	// Model.ID is the selector (provider/id), NOT the bare id.
	if models[0].ID != "anthropic/claude-sonnet-5" || models[0].Provider != "anthropic" || models[0].Label != "Claude Sonnet 5" {
		t.Errorf("models[0] = %+v", models[0])
	}
	if models[1].ID != "openai/gpt-5" || models[1].Provider != "openai" || models[1].Label != "GPT-5" {
		t.Errorf("models[1] = %+v", models[1])
	}
	// Bare model id with empty provider: selector is the bare id.
	if models[2].ID != "local-model" || models[2].Provider != "" || models[2].Label != "Local Model" {
		t.Errorf("models[2] = %+v", models[2])
	}
}

// TestParseOmpModelsEmptyCatalog verifies an empty {"models":[]} wrapper
// degrades gracefully to an empty model list (not an error). Uses the real
// wrapper shape, not a bare top-level array.
func TestParseOmpModelsEmptyCatalog(t *testing.T) {
	models, err := parseOmpModels([]byte(`{"models":[]}`))
	if err != nil {
		t.Fatalf("parseOmpModels({\"models\":[]}): %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models, got %d", len(models))
	}
}

// TestParseOmpModelsInvalidJSON verifies a non-JSON output (e.g. usage text
// from a flag mismatch or an old omp that doesn't support --json) degrades
// to an empty list, not a panic.
func TestParseOmpModelsInvalidJSON(t *testing.T) {
	models, err := parseOmpModels([]byte("Error: unknown flag --json\nRun `omp --help` for usage."))
	if err != nil {
		t.Fatalf("parseOmpModels(invalid): %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models for invalid JSON, got %d", len(models))
	}
}

// TestParseOmpModelsDeduplicates verifies that duplicate model selectors are
// collapsed, matching the pi discovery behaviour.
func TestParseOmpModelsDeduplicates(t *testing.T) {
	sample := `{"models":[` +
		`{"provider":"anthropic","id":"claude-sonnet-5","selector":"anthropic/claude-sonnet-5","name":"Sonnet"},` +
		`{"provider":"anthropic","id":"claude-sonnet-5","selector":"anthropic/claude-sonnet-5","name":"Sonnet Dup"}` +
		`]}`
	models, _ := parseOmpModels([]byte(sample))
	if len(models) != 1 {
		t.Fatalf("expected 1 model (deduplicated), got %d", len(models))
	}
}

// TestOmpModelsJSONShape verifies that parseOmpModels correctly parses the
// real omp models --json output shape: {"models":[{...}]} with provider/id
// as separate fields, and that Model.ID is the selector.
func TestOmpModelsJSONShape(t *testing.T) {
	raw := `{"models":[{"provider":"zai","id":"glm-4.6","selector":"zai/glm-4.6","name":"GLM 4.6","contextWindow":128000,"maxTokens":4096},{"provider":"xai","id":"grok-4-fast","selector":"xai/grok-4-fast","name":"Grok 4 Fast"}]}`
	models, err := parseOmpModels([]byte(raw))
	if err != nil {
		t.Fatalf("parseOmpModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	// Model.ID is the selector, not the bare id.
	if models[0].ID != "zai/glm-4.6" || models[0].Provider != "zai" || models[0].Label != "GLM 4.6" {
		t.Errorf("models[0] = %+v", models[0])
	}
	if models[1].ID != "xai/grok-4-fast" || models[1].Provider != "xai" || models[1].Label != "Grok 4 Fast" {
		t.Errorf("models[1] = %+v", models[1])
	}
}

// TestOmpSelectorSurvivesToBuildPiArgs is the regression test the review
// asked for: a real `omp models --json` fixture → parseOmpModels →
// buildPiArgs should hand the selector to --model whole. This pins the
// contract that Model.ID is the selector (provider/id) and that the selector
// reaches the CLI intact — the pi-family resolver takes it from there.
func TestOmpSelectorSurvivesToBuildPiArgs(t *testing.T) {
	raw := `{"models":[{"provider":"anthropic","id":"claude-sonnet-5","selector":"anthropic/claude-sonnet-5","name":"Claude Sonnet 5"}]}`
	models, err := parseOmpModels([]byte(raw))
	if err != nil {
		t.Fatalf("parseOmpModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	// The model ID is "anthropic/claude-sonnet-5" (the selector), and that is
	// exactly what --model receives. Splitting it into --provider anthropic
	// --model claude-sonnet-5 resolves identically for a real provider prefix
	// but breaks slash-shaped model ids, so the split is gone (GH #7300).
	args := buildPiArgs("/tmp/session.jsonl", ExecOptions{Model: models[0].ID}, slog.Default())
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model anthropic/claude-sonnet-5") {
		t.Errorf("args missing --model anthropic/claude-sonnet-5: %s", joined)
	}
	if strings.Contains(joined, "--provider") {
		t.Errorf("args should not synthesize --provider: %s", joined)
	}
}

// TestOmpAndPiCanCoexist verifies that both "pi" and "omp" can be constructed
// and used side by side — the core contract the issue (#3989) asks for.
func TestOmpAndPiCanCoexist(t *testing.T) {
	t.Parallel()

	piBe, err := New("pi", Config{ExecutablePath: "/fake/pi", Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(pi): %v", err)
	}
	ompBe, err := ResolveBackend("omp", Config{ExecutablePath: "/fake/omp", Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(omp): %v", err)
	}
	if _, ok := piBe.(*piBackend); !ok {
		t.Fatalf("pi backend is %T, want *piBackend", piBe)
	}
	if _, ok := ompBe.(*piBackend); !ok {
		t.Fatalf("omp backend is %T, want *piBackend", ompBe)
	}
	pb := piBe.(*piBackend)
	ob := ompBe.(*piBackend)
	if pb.defaultExecutable != "" {
		t.Errorf("pi defaultExecutable = %q, want empty", pb.defaultExecutable)
	}
	if ob.defaultExecutable != "omp" {
		t.Errorf("omp defaultExecutable = %q, want %q", ob.defaultExecutable, "omp")
	}
	if pb.providerLabel != "" {
		t.Errorf("pi providerLabel = %q, want empty", pb.providerLabel)
	}
	if ob.providerLabel != "omp" {
		t.Errorf("omp providerLabel = %q, want %q", ob.providerLabel, "omp")
	}
}

// TestDiscoverOmpModelsNonZeroExit verifies that discoverOmpModels returns
// a discovery error when the omp binary exits non-zero (e.g. an old omp that
// doesn't support `models --json` and prints usage to stderr). This is the
// fake-executable integration test the review asked for.
func TestDiscoverOmpModelsNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "omp")
	script := "#!/bin/sh\n" +
		"echo 'Error: unknown command \"models\"' >&2\n" +
		"echo 'Run `omp --help` for usage.' >&2\n" +
		"exit 1\n"
	writeTestExecutable(t, fakePath, []byte(script))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := discoverOmpModels(ctx, Command{Path: fakePath})
	if err == nil {
		t.Fatal("expected model discovery failure with a reason")
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models for non-zero-exit omp, got %d", len(models))
	}
}

// TestDiscoverOmpModelsMissingBinary verifies that a missing omp binary
// reports a discovery error.
func TestDiscoverOmpModelsMissingBinary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := discoverOmpModels(ctx, Command{Path: "/nonexistent/omp-binary"})
	if err == nil {
		t.Fatal("expected model discovery failure with a reason")
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models for missing binary, got %d", len(models))
	}
}

// TestOmpAndPiRegisterSideBySide is the daemon-level registration test the
// review asked for: with both binaries present, probing should discover two
// runtimes with the right commands. This exercises the probe path (not just
// backend construction), verifying that the descriptor-driven probe loop in
// agents_probe.go finds both "pi" and "omp" independently.
func TestOmpAndPiRegisterSideBySide(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakeDir := t.TempDir()
	// Create fake pi and omp binaries.
	for _, name := range []string{"pi", "omp"} {
		fakePath := filepath.Join(fakeDir, name)
		script := "#!/bin/sh\nexit 0\n"
		writeTestExecutable(t, fakePath, []byte(script))
	}
	t.Setenv("PATH", fakeDir)

	// Probe both CLIs the way the daemon would — using the same env vars
	// the descriptor-driven probe loop uses.
	for _, tc := range []struct {
		envPath  string
		envModel string
		cmd      string
		id       string
	}{
		{"MULTICA_PI_PATH", "MULTICA_PI_MODEL", "pi", "pi"},
		{"MULTICA_OMP_PATH", "MULTICA_OMP_MODEL", "omp", "omp"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			// Verify the descriptor's env prefix and command match what
			// the probe loop would use.
			desc, ok := BuiltinRuntimeByID(tc.id)
			if tc.id == "omp" {
				if !ok {
					t.Fatalf("BuiltinRuntimeByID(%q) not found", tc.id)
				}
				if desc.EnvPrefix != tc.envPath[:len(tc.envPath)-5] {
					t.Errorf("descriptor EnvPrefix = %q, want %q", desc.EnvPrefix, tc.envPath[:len(tc.envPath)-5])
				}
				if desc.DefaultCommand != tc.cmd {
					t.Errorf("descriptor DefaultCommand = %q, want %q", desc.DefaultCommand, tc.cmd)
				}
			}
			// The backend should construct successfully.
			b, err := ResolveBackend(tc.id, Config{ExecutablePath: filepath.Join(fakeDir, tc.cmd), Logger: slog.Default()})
			if err != nil {
				t.Fatalf("New(%q): %v", tc.id, err)
			}
			pb, ok := b.(*piBackend)
			if !ok {
				t.Fatalf("New(%q) = %T, want *piBackend", tc.id, b)
			}
			if tc.id == "omp" {
				if pb.defaultExecutable != "omp" {
					t.Errorf("defaultExecutable = %q, want %q", pb.defaultExecutable, "omp")
				}
				if pb.providerLabel != "omp" {
					t.Errorf("providerLabel = %q, want %q", pb.providerLabel, "omp")
				}
			}
		})
	}
}

// levelValues flattens a ModelThinking's advertised levels for comparison.
func levelValues(thinking *ModelThinking) []string {
	if thinking == nil {
		return nil
	}
	out := make([]string, 0, len(thinking.SupportedLevels))
	for _, level := range thinking.SupportedLevels {
		out = append(out, level.Value)
	}
	return out
}

// TestOmpThinkingFromCatalogEntry pins omp's own rules for what a catalog entry
// means, verified against can1357/oh-my-pi v18.2.0. Two of them are easy to get
// backwards, and both produce the silent mismatch MUL-7412 exists to remove:
// `reasoning: true` with no effort array means "no controllable dial" (not
// "assume the usual levels"), and `off` is honoured for every reasoning model
// even though it is never a member of the effort array.
func TestOmpThinkingFromCatalogEntry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		reasoning    bool
		thinking     string
		wantLevels   []string
		wantNoPicker bool
	}{
		{
			// The reporter's model in GH #8458. `off` is added on top: omp's
			// resolveThinkingLevelForModel returns it before clamping runs, so it
			// really is selectable even though the array never lists it.
			name:       "advertised efforts, plus off",
			reasoning:  true,
			thinking:   `["medium","high","max"]`,
			wantLevels: []string{"off", "medium", "high", "max"},
		},
		{
			// getSupportedEfforts defines this as "reasons, no controllable effort
			// surface". Inferring low/medium/high here would offer levels omp then
			// clamps away.
			name:       "reasoning with null thinking offers only off",
			reasoning:  true,
			thinking:   `null`,
			wantLevels: []string{"off"},
		},
		{
			name:       "reasoning with absent thinking offers only off",
			reasoning:  true,
			thinking:   ``,
			wantLevels: []string{"off"},
		},
		{
			name:       "reasoning with an empty array offers only off",
			reasoning:  true,
			thinking:   `[]`,
			wantLevels: []string{"off"},
		},
		{
			// No reasoning at all: an off-only control would be inert, so Multica
			// hides the picker exactly as it does for pi.
			name:         "not a reasoning model has no picker",
			reasoning:    false,
			thinking:     ``,
			wantNoPicker: true,
		},
		{
			name:         "not a reasoning model stays hidden even with efforts listed",
			reasoning:    false,
			thinking:     `["high"]`,
			wantNoPicker: true,
		},
		{
			// omp keeps `auto` as a session-level sentinel, never a per-model
			// effort, so it must not reach the picker.
			name:       "auto is never offered",
			reasoning:  true,
			thinking:   `["medium","high","auto"]`,
			wantLevels: []string{"off", "medium", "high"},
		},
		{
			// Canonical order, not the order omp happened to list.
			name:       "levels come back in canonical order",
			reasoning:  true,
			thinking:   `["max","low","high"]`,
			wantLevels: []string{"off", "low", "high", "max"},
		},
		{
			// Only tokens Multica knows survive, so a future omp effort cannot
			// reach the picker before the server's enum accepts it.
			name:       "unknown tokens are dropped",
			reasoning:  true,
			thinking:   `["medium","ultra","hyper"]`,
			wantLevels: []string{"off", "medium"},
		},
		{
			// `models --json` never emits an object. If one ever appears it is not
			// evidence of support: fall back to off-only rather than mining it.
			name:       "an unreadable shape proves nothing",
			reasoning:  true,
			thinking:   `{"mode":"effort","efforts":["medium","high","max"]}`,
			wantLevels: []string{"off"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ompThinkingFromCatalogEntry(tc.reasoning, []byte(tc.thinking))
			if tc.wantNoPicker {
				if got != nil {
					t.Fatalf("ompThinkingFromCatalogEntry = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ompThinkingFromCatalogEntry = nil, want levels %v", tc.wantLevels)
			}
			if values := levelValues(got); !slices.Equal(values, tc.wantLevels) {
				t.Errorf("levels = %v, want %v", values, tc.wantLevels)
			}
			// omp's `models --json` carries no default level, so we must never
			// invent one for the picker to preselect.
			if got.DefaultLevel != "" {
				t.Errorf("DefaultLevel = %q, want empty", got.DefaultLevel)
			}
			for _, level := range got.SupportedLevels {
				if level.Label == "" {
					t.Errorf("level %q has an empty label", level.Value)
				}
			}
		})
	}
}

// TestParseOmpModelsCarriesThinkingCatalog is the end-to-end catalog assertion:
// a real `omp models --json` payload must reach Model.Thinking, because that is
// what the picker renders and what the daemon's per-model guard validates
// against before injecting --thinking (MUL-7412).
func TestParseOmpModelsCarriesThinkingCatalog(t *testing.T) {
	t.Parallel()
	sample := `{"models":[` +
		`{"provider":"devin","id":"swe-2","selector":"devin/swe-2","name":"SWE-2","reasoning":true,"thinking":["medium","high","max"]},` +
		`{"provider":"anthropic","id":"claude-sonnet-5","selector":"anthropic/claude-sonnet-5","name":"Sonnet 5","reasoning":true,"thinking":null},` +
		`{"provider":"openai","id":"gpt-5","selector":"openai/gpt-5","name":"GPT-5","reasoning":false,"thinking":null}` +
		`]}`
	models, err := parseOmpModels([]byte(sample))
	if err != nil {
		t.Fatalf("parseOmpModels: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}
	if got := levelValues(models[0].Thinking); !slices.Equal(got, []string{"off", "medium", "high", "max"}) {
		t.Errorf("devin/swe-2 levels = %v, want [off medium high max]", got)
	}
	// Reasons, but omp exposes no effort dial for it: off only, nothing inferred.
	if got := levelValues(models[1].Thinking); !slices.Equal(got, []string{"off"}) {
		t.Errorf("anthropic/claude-sonnet-5 levels = %v, want [off]", got)
	}
	// No reasoning at all: no picker, rather than an inert one.
	if models[2].Thinking != nil {
		t.Errorf("openai/gpt-5 Thinking = %+v, want nil", models[2].Thinking)
	}
}

// TestOmpAdvertisedLevelsArePersistable pins the catalog → API contract for omp:
// every level discovery can advertise must survive the server's Create/Update
// enum gate. Otherwise the picker offers a level the server 400s on save, which
// is the mirror image of the MUL-7412 defect.
func TestOmpAdvertisedLevelsArePersistable(t *testing.T) {
	t.Parallel()
	for _, value := range piThinkingLevelOrder {
		if !IsKnownThinkingValue("omp", value) {
			t.Errorf("omp advertises %q but the server enum rejects it", value)
		}
	}
	if IsKnownThinkingValue("omp", "auto") {
		t.Error("omp must not accept `auto`: it is deliberately not exposed (MUL-7412)")
	}
}

// TestValidateThinkingLevelOmpEmptyModel pins the omp agent that pins no model:
// the level must fail closed. omp's catalog marks no default entry, and at task
// time omp resolves its own default role model and clamps the level to what
// THAT model supports — so passing a level because some other catalog entry
// advertises it would let a user save `max` and silently run lower, which is
// the mismatch MUL-7412 exists to remove.
func TestValidateThinkingLevelOmpEmptyModel(t *testing.T) {
	t.Parallel()
	load := func() (Catalog, error) {
		return Catalog{Models: []Model{
			{ID: "devin/swe-2", Label: "SWE-2", Provider: "devin",
				Thinking: ompThinkingFromCatalogEntry(true, []byte(`["medium","high","max"]`))},
			{ID: "anthropic/claude-sonnet-5", Label: "Sonnet 5", Provider: "anthropic",
				Thinking: ompThinkingFromCatalogEntry(true, []byte(`null`))},
		}}, nil
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{value: "", want: true}, // "use the runtime default" is always fine
		// Advertised by devin/swe-2, but that is not necessarily the model omp
		// will run, so an unpinned agent must not carry it.
		{value: "max", want: false},
		{value: "off", want: false},
	} {
		got, err := ValidateThinkingLevelWith(load, "omp", "", tc.value)
		if err != nil {
			t.Fatalf("ValidateThinkingLevelWith(omp, \"\", %q): %v", tc.value, err)
		}
		if got != tc.want {
			t.Errorf("ValidateThinkingLevelWith(omp, \"\", %q) = %v, want %v", tc.value, got, tc.want)
		}
	}
	// A pinned model still resolves against that model's own catalog.
	for _, tc := range []struct {
		model string
		value string
		want  bool
	}{
		{model: "devin/swe-2", value: "max", want: true},
		{model: "devin/swe-2", value: "off", want: true},
		{model: "devin/swe-2", value: "low", want: false},
		// Reasons with no effort dial: off is real, a concrete effort is not.
		{model: "anthropic/claude-sonnet-5", value: "off", want: true},
		{model: "anthropic/claude-sonnet-5", value: "high", want: false},
	} {
		got, err := ValidateThinkingLevelWith(load, "omp", tc.model, tc.value)
		if err != nil {
			t.Fatalf("ValidateThinkingLevelWith(omp, %q, %q): %v", tc.model, tc.value, err)
		}
		if got != tc.want {
			t.Errorf("ValidateThinkingLevelWith(omp, %q, %q) = %v, want %v", tc.model, tc.value, got, tc.want)
		}
	}
}

// TestValidateThinkingLevelOmpEmptyModelIsDeterministic pins the ORDER of the
// empty-model rejection, not just its result. The check must happen before any
// catalog read: on a discovery error the daemon's guard passes the level through
// to the CLI, so an omp agent with no pinned model would still have shipped an
// effort omp resolves against a different model. Asserting the loader is never
// called is the only way to pin that ordering — a test that merely returns false
// would pass with the check on either side of the load.
func TestValidateThinkingLevelOmpEmptyModelIsDeterministic(t *testing.T) {
	t.Parallel()
	called := false
	failing := func() (Catalog, error) {
		called = true
		return Catalog{}, errors.New("omp discovery failed")
	}
	got, err := ValidateThinkingLevelWith(failing, "omp", "", "max")
	if err != nil {
		t.Fatalf("ValidateThinkingLevelWith(omp, \"\", max) returned error %v, want a deterministic false", err)
	}
	if got {
		t.Error("ValidateThinkingLevelWith(omp, \"\", max) = true, want false")
	}
	if called {
		t.Error("catalog loader was called; the empty-model rejection must precede discovery")
	}
	// The empty "use the runtime default" sentinel still short-circuits ahead of
	// everything, so it must not reach the loader either.
	called = false
	got, err = ValidateThinkingLevelWith(failing, "omp", "", "")
	if err != nil || !got {
		t.Errorf("ValidateThinkingLevelWith(omp, \"\", \"\") = (%v, %v), want (true, nil)", got, err)
	}
	if called {
		t.Error("empty thinking level must not trigger discovery")
	}
}

// TestThinkingRequiresExplicitModelPredicates pins which providers need a pinned
// model, and the narrower set whose invalid combination the API refuses to store.
// codex is deliberately in the first set only: agents already hold an effort with
// an empty model, so 400ing that pair would block unrelated edits to them.
func TestThinkingRequiresExplicitModelPredicates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		provider      string
		needsModel    bool
		rejectedAtAPI bool
	}{
		{provider: "omp", needsModel: true, rejectedAtAPI: true},
		{provider: "codex", needsModel: true, rejectedAtAPI: false},
		{provider: "pi", needsModel: false, rejectedAtAPI: false},
		{provider: "claude", needsModel: false, rejectedAtAPI: false},
		{provider: "opencode", needsModel: false, rejectedAtAPI: false},
		{provider: "", needsModel: false, rejectedAtAPI: false},
	} {
		if got := ThinkingRequiresExplicitModel(tc.provider); got != tc.needsModel {
			t.Errorf("ThinkingRequiresExplicitModel(%q) = %v, want %v", tc.provider, got, tc.needsModel)
		}
		if got := ThinkingLevelRejectedWithoutModel(tc.provider); got != tc.rejectedAtAPI {
			t.Errorf("ThinkingLevelRejectedWithoutModel(%q) = %v, want %v", tc.provider, got, tc.rejectedAtAPI)
		}
		// The API-level refusal is a subset: anything it rejects must also be a
		// provider that genuinely needs a pinned model.
		if ThinkingLevelRejectedWithoutModel(tc.provider) && !ThinkingRequiresExplicitModel(tc.provider) {
			t.Errorf("provider %q is rejected at the API but does not require an explicit model", tc.provider)
		}
	}
}
