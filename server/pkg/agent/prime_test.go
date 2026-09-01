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

// TestPrimeNewDispatchesToPiBackend asserts that ResolveBackend("prime")
// dispatches to the pi backend via the descriptor registry — the core
// contract that prime is a runtime identity on the pi protocol (hard fork of
// pi), not a separate protocol family. IsSupportedType("prime") is false (it
// is not a protocol family), but New resolves it through BuiltinRuntimes to
// the pi backend with the correct executable, label, and session-mode
// overrides.
func TestPrimeNewDispatchesToPiBackend(t *testing.T) {
	if IsSupportedType("prime") {
		t.Error("prime must not be in SupportedTypes (it is a runtime identity, not a protocol family)")
	}
	b, err := ResolveBackend("prime", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("ResolveBackend(prime): %v", err)
	}
	pb, ok := b.(*piBackend)
	if !ok {
		t.Fatalf("ResolveBackend(prime) returned %T, want *piBackend", b)
	}
	if pb.defaultExecutable != "prime-agent" {
		t.Errorf("defaultExecutable = %q, want %q", pb.defaultExecutable, "prime-agent")
	}
	if pb.providerLabel != "prime" {
		t.Errorf("providerLabel = %q, want %q", pb.providerLabel, "prime")
	}
	if pb.sessionMode != piSessionModeDir {
		t.Errorf("sessionMode = %v, want piSessionModeDir (prime owns its session dir)", pb.sessionMode)
	}
}

// TestPrimeOmitsSessionAndUsesSessionDir verifies that the prime arg builder
// emits --session-dir and --resume instead of --session. prime removed
// --session (v0.8.0) — this is the compatibility contract.
func TestPrimeArgsUseSessionDirAndResume(t *testing.T) {
	args := buildPrimeArgs("/tmp/prime-sessions", ExecOptions{ResumeSessionID: "01a05c2d-3221-71ab-99b8-1a2f6d968114"}, slog.Default())
	joined := strings.Join(args, " ")
	for _, want := range []string{"-p", "--mode json", "--session-dir /tmp/prime-sessions", "--resume 01a05c2d-3221-71ab-99b8-1a2f6d968114"} {
		if !strings.Contains(joined, want) {
			t.Errorf("buildPrimeArgs missing %q; args: %v", want, args)
		}
	}
	if strings.Contains(joined, "--session ") {
		t.Errorf("buildPrimeArgs must not emit --session (prime rejects it); args: %v", args)
	}
}

// TestPrimeArgsNoResumeOnFreshSession verifies a fresh prime session (empty
// ResumeSessionID) omits --resume altogether.
func TestPrimeArgsNoResumeOnFreshSession(t *testing.T) {
	args := buildPrimeArgs("/tmp/prime-sessions", ExecOptions{}, slog.Default())
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--session-dir /tmp/prime-sessions") {
		t.Fatalf("expected --session-dir in args: %v", args)
	}
	if strings.Contains(joined, "--resume") {
		t.Errorf("fresh prime session must not emit --resume; args: %v", args)
	}
}

// TestPrimeArgsKeepsModelThinkingCustomArgs verifies buildPrimeArgs forwards
// the same model/thinking/custom-arg options buildPiArgs does.
func TestPrimeArgsKeepsModelThinkingCustomArgs(t *testing.T) {
	args := buildPrimeArgs("/tmp/prime-sessions", ExecOptions{
		Model:         "omlx6/Qwen3.6-35B-A3B-oQ4-fp16-mtp",
		ThinkingLevel: "low",
		CustomArgs:    []string{"--offline"},
	}, slog.Default())
	joined := strings.Join(args, " ")
	for _, want := range []string{"--model omlx6/Qwen3.6-35B-A3B-oQ4-fp16-mtp", "--thinking low", "--offline"} {
		if !strings.Contains(joined, want) {
			t.Errorf("buildPrimeArgs missing %q; args: %v", want, args)
		}
	}
}

// TestPrimeExecuteUsesSessionDir verifies the dir-mode execute path launches
// with --session-dir (not --session) end to end, using a fake prime binary
// that emits the pi JSON event stream. The result carries the session event's
// id as SessionID (the value --resume needs on the next turn).
func TestPrimeExecuteUsesSessionDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	events := []string{
		`{"type":"session","version":3,"id":"01a05c2d-prime-test-abcd","timestamp":"2026-01-01T00:00:00Z","cwd":"/tmp","rlmDepth":0}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello from prime"}}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"prime-test","usage":{"input":10,"output":5}}}`,
	}
	fakePath := filepath.Join(t.TempDir(), "prime-agent")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript(events)))

	backend, err := ResolveBackend("prime", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("ResolveBackend(prime): %v", err)
	}
	pb := backend.(*piBackend)
	if pb.sessionMode != piSessionModeDir {
		t.Fatalf("backend sessionMode = %v, want piSessionModeDir", pb.sessionMode)
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
		if result.Output != "hello from prime" {
			t.Errorf("Output = %q, want %q", result.Output, "hello from prime")
		}
		// In dir mode the SessionID must be the session event's id, not a path.
		if result.SessionID != "01a05c2d-prime-test-abcd" {
			t.Errorf("SessionID = %q, want the session event id %q (prime resumes by id)", result.SessionID, "01a05c2d-prime-test-abcd")
		}
		if len(result.Usage) != 1 {
			t.Fatalf("expected 1 usage entry, got %d", len(result.Usage))
		}
		u, ok := result.Usage["prime-test"]
		if !ok {
			t.Fatalf("expected usage for model %q, got %v", "prime-test", result.Usage)
		}
		if u.InputTokens != 10 || u.OutputTokens != 5 {
			t.Errorf("usage: input=%d output=%d, want input=10 output=5", u.InputTokens, u.OutputTokens)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestPrimeAndPiCanCoexist verifies both "pi" and "prime" backends construct
// side by side, with prime defaulting to dir mode and pi to file mode.
func TestPrimeAndPiCanCoexist(t *testing.T) {
	piBe, err := New("pi", Config{ExecutablePath: "/fake/pi", Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(pi): %v", err)
	}
	primeBe, err := ResolveBackend("prime", Config{ExecutablePath: "/fake/prime-agent", Logger: slog.Default()})
	if err != nil {
		t.Fatalf("ResolveBackend(prime): %v", err)
	}
	pb := piBe.(*piBackend)
	primeb := primeBe.(*piBackend)
	if pb.sessionMode != piSessionModeFile {
		t.Errorf("pi sessionMode = %v, want piSessionModeFile", pb.sessionMode)
	}
	if primeb.sessionMode != piSessionModeDir {
		t.Errorf("prime sessionMode = %v, want piSessionModeDir", primeb.sessionMode)
	}
}

// TestPrimeDescriptorFieldsAreConsumed guards that the prime descriptor's
// fields are all populated (the descriptor-driven probe and UI read them).
func TestPrimeDescriptorFieldsAreConsumed(t *testing.T) {
	desc, ok := BuiltinRuntimeByID("prime")
	if !ok {
		t.Fatal("prime descriptor not found")
	}
	checks := map[string]string{
		"ID":                desc.ID,
		"ProtocolFamily":    desc.ProtocolFamily,
		"DefaultCommand":    desc.DefaultCommand,
		"EnvPrefix":         desc.EnvPrefix,
		"DisplayName":       desc.DisplayName,
		"SkillsDir":         desc.SkillsDir,
		"UserSkillsDir":     desc.UserSkillsDir,
		"LaunchHeader":      desc.LaunchHeader,
		"DefaultExecutable": desc.DefaultExecutable,
		"ProviderLabel":     desc.ProviderLabel,
	}
	for name, val := range checks {
		if val == "" {
			t.Errorf("descriptor field %s is empty", name)
		}
	}
	if desc.ModelDiscovery == nil {
		t.Error("descriptor field ModelDiscovery is nil")
	}
	if desc.SessionMode != piSessionModeDir {
		t.Errorf("descriptor SessionMode = %v, want piSessionModeDir", desc.SessionMode)
	}
}

// TestDiscoverPrimeModelsTable verifies `prime-agent model list` output (the
// same table shape as pi) is parsed through parsePiModels.
func TestDiscoverPrimeModelsTable(t *testing.T) {
	table := "provider  model                       context  max-out  thinking  images\n" +
		"litellm   opencode-jj-deepseek-v4-flash   131.1K   16.4K    no        no\n" +
		"omlx6     Qwen3.6-35B-A3B-oQ4-fp16-mtp    131.1K   16.4K    yes       no\n"
	models, err := discoverPrimeModelsTable(table)
	if err != nil {
		t.Fatalf("discoverPrimeModelsTable: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].ID != "litellm/opencode-jj-deepseek-v4-flash" || models[0].Provider != "litellm" {
		t.Errorf("models[0] = %+v", models[0])
	}
	if models[1].ID != "omlx6/Qwen3.6-35B-A3B-oQ4-fp16-mtp" || models[1].Provider != "omlx6" {
		t.Errorf("models[1] = %+v", models[1])
	}
}

// discoverPrimeModelsTable is a thin wrapper so the table-parsing path is
// testable without spawning a binary. It mirrors discoverPrimeModels' final
// parse step.
func discoverPrimeModelsTable(output string) ([]Model, error) {
	return parsePiModels(output), nil
}
