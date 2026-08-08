package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestOmpNewDispatchesToPiBackend asserts that New("omp") dispatches to the
// pi backend via the descriptor registry — the core contract that omp is a
// runtime identity on the pi protocol, not a separate protocol family.
// IsSupportedType("omp") is false (it's not a protocol family), but New
// resolves it through BuiltinRuntimes to the pi backend with the correct
// executable and label overrides.
func TestOmpNewDispatchesToPiBackend(t *testing.T) {
	if IsSupportedType("omp") {
		t.Errorf("omp must not be in SupportedTypes (it is a runtime identity, not a protocol family)")
	}
	b, err := New("omp", Config{Logger: slog.Default()})
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

// TestOmpExecuteDefaultsToOmpBinary verifies that New("omp", Config{}) with
// an empty ExecutablePath resolves to the "omp" binary name (not "pi") when
// the daemon hasn't pinned a path. This is the contract the daemon relies on
// when it constructs a backend from a probe result that carries no path.
func TestOmpExecuteDefaultsToOmpBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Create a fake "omp" binary on PATH that asserts it was invoked as omp.
	fakeDir := t.TempDir()
	fakePath := filepath.Join(fakeDir, "omp")
	script := "#!/bin/sh\n" +
		"cat > /dev/null\n" +
		// Emit a valid event stream so Execute returns "completed".
		"printf '%s\\n' '{\"type\":\"agent_start\"}'\n" +
		"printf '%s\\n' '{\"type\":\"turn_end\",\"message\":{\"role\":\"assistant\",\"model\":\"test\",\"usage\":{\"input\":1,\"output\":1}}}'\n" +
		"exit 0\n"
	writeTestExecutable(t, fakePath, []byte(script))

	// Place the fake omp binary on PATH so exec.LookPath finds it.
	t.Setenv("PATH", fakeDir)

	backend, err := New("omp", Config{Logger: slog.Default()})
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

	backend, err := New("omp", Config{ExecutablePath: "/does/not/need/to/exist", Logger: slog.Default()})
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

	backend, err := New("omp", Config{ExecutablePath: "/nonexistent/omp-binary", Logger: slog.Default()})
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

	backend, err := New("omp", Config{ExecutablePath: fakePath, Logger: slog.Default()})
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
		// Verify usage was captured from the omp event stream.
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
// `id`, `selector` (provider/id), and `name` fields.
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
	if models[0].ID != "claude-sonnet-5" || models[0].Provider != "anthropic" || models[0].Label != "Claude Sonnet 5" {
		t.Errorf("models[0] = %+v", models[0])
	}
	if models[1].ID != "gpt-5" || models[1].Provider != "openai" || models[1].Label != "GPT-5" {
		t.Errorf("models[1] = %+v", models[1])
	}
	// Bare model id with empty provider.
	if models[2].ID != "local-model" || models[2].Provider != "" || models[2].Label != "Local Model" {
		t.Errorf("models[2] = %+v", models[2])
	}
}

// TestParseOmpModelsEmptyCatalog verifies an empty JSON array degrades
// gracefully to an empty model list (not an error).
func TestParseOmpModelsEmptyCatalog(t *testing.T) {
	models, err := parseOmpModels([]byte("[]"))
	if err != nil {
		t.Fatalf("parseOmpModels([]): %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models, got %d", len(models))
	}
}

// TestParseOmpModelsInvalidJSON verifies a non-JSON output (e.g. usage text
// from a flag mismatch) degrades to an empty list, not a panic.
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

// TestOmpAndPiCanCoexist verifies that both "pi" and "omp" can be constructed
// and used side by side — the core contract the issue (#3989) asks for.
func TestOmpAndPiCanCoexist(t *testing.T) {
	t.Parallel()

	piBe, err := New("pi", Config{ExecutablePath: "/fake/pi", Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(pi): %v", err)
	}
	ompBe, err := New("omp", Config{ExecutablePath: "/fake/omp", Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(omp): %v", err)
	}
	if _, ok := piBe.(*piBackend); !ok {
		t.Fatalf("pi backend is %T, want *piBackend", piBe)
	}
	if _, ok := ompBe.(*piBackend); !ok {
		t.Fatalf("omp backend is %T, want *piBackend", ompBe)
	}
	// The pi backend's defaultExecutable is empty (defaults to "pi"),
	// the omp backend's is "omp".
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

// TestOmpModelsJSONShape verifies that parseOmpModels correctly parses the
// real omp models --json output shape: {"models":[{...}]} with provider/id
// as separate fields.
func TestOmpModelsJSONShape(t *testing.T) {
	raw := `{"models":[{"provider":"zai","id":"glm-4.6","selector":"zai/glm-4.6","name":"GLM 4.6","contextWindow":128000,"maxTokens":4096},{"provider":"xai","id":"grok-4-fast","selector":"xai/grok-4-fast","name":"Grok 4 Fast"}]}`
	models, err := parseOmpModels([]byte(raw))
	if err != nil {
		t.Fatalf("parseOmpModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "glm-4.6" || models[0].Provider != "zai" || models[0].Label != "GLM 4.6" {
		t.Errorf("models[0] = %+v", models[0])
	}
	if models[1].ID != "grok-4-fast" || models[1].Provider != "xai" || models[1].Label != "Grok 4 Fast" {
		t.Errorf("models[1] = %+v", models[1])
	}
}
