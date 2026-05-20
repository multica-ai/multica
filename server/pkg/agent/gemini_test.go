package agent

import (
	"log/slog"
	"strings"
	"testing"
)

func TestBuildGeminiArgsBaseline(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("write a haiku", ExecOptions{}, slog.Default())
	expected := []string{
		"-p", "write a haiku",
		"--dangerously-skip-permissions",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Fatalf("expected args[%d] = %q, got %q", i, want, args[i])
		}
	}
}

func TestBuildGeminiArgsIgnoresModel(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{Model: "gemini-3.5-flash-high"}, slog.Default())

	for _, a := range args {
		if a == "-m" || a == "gemini-3.5-flash-high" {
			t.Fatalf("AGY does not expose a model flag; expected model to be omitted, got args=%v", args)
		}
	}
}

func TestBuildGeminiArgsWithResume(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{ResumeSessionID: "3"}, slog.Default())

	var foundResume bool
	for i, a := range args {
		if a == "--conversation" {
			if i+1 >= len(args) || args[i+1] != "3" {
				t.Fatalf("expected --conversation followed by session id, got %v", args)
			}
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Fatalf("expected --conversation flag when ResumeSessionID is set, got args=%v", args)
	}
}

func TestBuildGeminiArgsOmitsModelWhenEmpty(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{}, slog.Default())
	for _, a := range args {
		if a == "-m" {
			t.Fatalf("expected no -m flag when Model is empty, got args=%v", args)
		}
		if a == "--conversation" {
			t.Fatalf("expected no --conversation flag when ResumeSessionID is empty, got args=%v", args)
		}
	}
}

func TestBuildGeminiArgsPassesThroughCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{
		CustomArgs: []string{"--sandbox"},
	}, slog.Default())

	if args[len(args)-1] != "--sandbox" {
		t.Fatalf("expected --sandbox at end of args, got %v", args)
	}
}

// envLookup returns the value of key in an env slice, or ("", false) if absent.
// When the key appears multiple times the last occurrence wins, mirroring how
// libc's getenv resolves duplicates on the daemon's supported platforms — the
// caller-supplied override therefore takes precedence over our default.
func envLookup(env []string, key string) (string, bool) {
	prefix := key + "="
	var value string
	var found bool
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			value = strings.TrimPrefix(entry, prefix)
			found = true
		}
	}
	return value, found
}

func TestBuildGeminiEnvSetsTrustWorkspaceDefault(t *testing.T) {
	t.Parallel()

	env := buildGeminiEnv(nil)
	got, ok := envLookup(env, "GEMINI_CLI_TRUST_WORKSPACE")
	if !ok {
		t.Fatalf("expected GEMINI_CLI_TRUST_WORKSPACE to be set, got env=%v", env)
	}
	if got != "true" {
		t.Fatalf("expected GEMINI_CLI_TRUST_WORKSPACE=true, got %q", got)
	}
}

func TestBuildGeminiEnvRespectsExplicitOverride(t *testing.T) {
	t.Parallel()

	// Users who deliberately set the value (e.g. to "false" to opt back into
	// the legacy Gemini folder-trust gate, or to a future-proofed value) must win over
	// our daemon default.
	env := buildGeminiEnv(map[string]string{"GEMINI_CLI_TRUST_WORKSPACE": "false"})
	got, ok := envLookup(env, "GEMINI_CLI_TRUST_WORKSPACE")
	if !ok {
		t.Fatalf("expected GEMINI_CLI_TRUST_WORKSPACE to be set, got env=%v", env)
	}
	if got != "false" {
		t.Fatalf("expected caller's GEMINI_CLI_TRUST_WORKSPACE=false to win, got %q", got)
	}
}

func TestBuildGeminiEnvPreservesOtherExtras(t *testing.T) {
	t.Parallel()

	env := buildGeminiEnv(map[string]string{"GEMINI_API_KEY": "secret"})
	if got, ok := envLookup(env, "GEMINI_API_KEY"); !ok || got != "secret" {
		t.Fatalf("expected GEMINI_API_KEY=secret to pass through, got %q (ok=%v)", got, ok)
	}
	if got, ok := envLookup(env, "GEMINI_CLI_TRUST_WORKSPACE"); !ok || got != "true" {
		t.Fatalf("expected default GEMINI_CLI_TRUST_WORKSPACE=true, got %q (ok=%v)", got, ok)
	}
}

func TestBuildGeminiArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildGeminiArgs("hi", ExecOptions{
		CustomArgs: []string{"-o", "text", "--sandbox"},
	}, slog.Default())

	// -o text should be filtered, --sandbox should pass through
	for i, a := range args {
		if a == "-o" && i+1 < len(args) && args[i+1] == "text" {
			t.Fatalf("blocked -o text should have been filtered: %v", args)
		}
	}
	if args[len(args)-1] != "--sandbox" {
		t.Fatalf("expected --sandbox to pass through, got %v", args)
	}
}

func TestAgyAuthErrorDetectsAuthPrompt(t *testing.T) {
	t.Parallel()

	output := "Authentication required. Please visit the URL to log in:\nError: authentication timed out."
	if got := agyAuthError(output); got != "agy authentication required" {
		t.Fatalf("agyAuthError() = %q, want agy authentication required", got)
	}
}

func TestAgyAuthErrorIgnoresNormalOutput(t *testing.T) {
	t.Parallel()

	if got := agyAuthError("completed review"); got != "" {
		t.Fatalf("agyAuthError() = %q, want empty", got)
	}
}
