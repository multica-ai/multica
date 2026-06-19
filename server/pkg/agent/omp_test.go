package agent

import (
	"log/slog"
	"strings"
	"testing"
)

func TestBuildOmpArgsBasicFlags(t *testing.T) {
	args := buildOmpArgs("hello world", "", ExecOptions{
		Model:        "anthropic/claude-sonnet-4-20250514",
		SystemPrompt: "be helpful",
	}, slog.Default())

	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-p",
		"--mode json",
		"--auto-approve",
		"--provider anthropic",
		"--model claude-sonnet-4-20250514",
		"--append-system-prompt",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in args, got: %v", want, args)
		}
	}

	// Prompt must be the last positional argument.
	if args[len(args)-1] != "hello world" {
		t.Errorf("prompt should be last arg, got %q", args[len(args)-1])
	}
}

func TestBuildOmpArgsResume(t *testing.T) {
	args := buildOmpArgs("continue", "abc123", ExecOptions{}, slog.Default())

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--resume abc123") {
		t.Errorf("expected --resume abc123 in args, got: %v", args)
	}
}

func TestBuildOmpArgsNoResume(t *testing.T) {
	args := buildOmpArgs("fresh start", "", ExecOptions{}, slog.Default())

	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--resume") {
		t.Errorf("--resume should not appear for fresh sessions, got: %v", args)
	}
}

func TestBuildOmpArgsNoToolRestriction(t *testing.T) {
	args := buildOmpArgs("test", "", ExecOptions{}, slog.Default())
	for i, arg := range args {
		if arg == "--tools" {
			t.Errorf("buildOmpArgs emits --tools %q; should not restrict tool registry", args[i+1])
		}
	}
}

func TestBuildOmpArgsCustomArgsAppended(t *testing.T) {
	args := buildOmpArgs("prompt", "", ExecOptions{
		CustomArgs: []string{"--no-lsp"},
	}, slog.Default())

	found := false
	for _, arg := range args {
		if arg == "--no-lsp" {
			found = true
		}
	}
	if !found {
		t.Errorf("custom --no-lsp should pass through via custom_args, got: %v", args)
	}
}

func TestBuildOmpArgsBlockedArgsFiltered(t *testing.T) {
	args := buildOmpArgs("p", "", ExecOptions{
		CustomArgs: []string{"-p", "--mode", "text"},
	}, slog.Default())

	pCount := 0
	modeCount := 0
	for _, arg := range args {
		if arg == "-p" {
			pCount++
		}
		if arg == "--mode" {
			modeCount++
		}
		// The value "text" from custom_args must be filtered.
		if arg == "text" {
			t.Errorf("\"text\" from custom_args should be filtered, got: %v", args)
		}
	}
	if pCount != 1 {
		t.Errorf("expected exactly one -p (the hardcoded one), got %d in: %v", pCount, args)
	}
	if modeCount != 1 {
		t.Errorf("expected exactly one --mode (the hardcoded one), got %d in: %v", modeCount, args)
	}
}

func TestParseOmpModelsProviderSection(t *testing.T) {
	output := "Provider models\n" +
		"provider  model         context  max-out  thinking  images\n" +
		"anthropic claude-sonnet  200000   32000   low,high  yes\n" +
		"openai    gpt-5.2        128000   16000   off,low   yes\n"

	models := parseOmpModels(output)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %v", len(models), models)
	}
	if models[0].ID != "anthropic/claude-sonnet" {
		t.Errorf("expected anthropic/claude-sonnet, got %s", models[0].ID)
	}
	if models[0].Provider != "anthropic" {
		t.Errorf("expected provider=anthropic, got %s", models[0].Provider)
	}
	if models[1].ID != "openai/gpt-5.2" {
		t.Errorf("expected openai/gpt-5.2, got %s", models[1].ID)
	}
}

func TestParseOmpModelsCanonicalSection(t *testing.T) {
	output := "Canonical models\n" +
		"canonical     selected               variants  context  max-out\n" +
		"claude-sonnet anthropic/claude-sonnet  2      200000   32000\n"

	models := parseOmpModels(output)
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %v", len(models), models)
	}
	if models[0].ID != "anthropic/claude-sonnet" {
		t.Errorf("expected anthropic/claude-sonnet, got %s", models[0].ID)
	}
}

func TestParseOmpModelsDeduplication(t *testing.T) {
	output := "Canonical models\n" +
		"canonical     selected               variants  context  max-out\n" +
		"claude-sonnet anthropic/claude-sonnet  2      200000   32000\n" +
		"\n" +
		"Provider models\n" +
		"provider  model         context  max-out  thinking  images\n" +
		"anthropic claude-sonnet  200000   32000   low,high  yes\n"

	models := parseOmpModels(output)
	if len(models) != 1 {
		t.Fatalf("expected 1 deduplicated model, got %d: %v", len(models), models)
	}
}

func TestParseOmpModelsEmpty(t *testing.T) {
	models := parseOmpModels("")
	if len(models) != 0 {
		t.Errorf("expected 0 models for empty input, got %d", len(models))
	}
}

func TestParseOmpModelsNoModelsMessage(t *testing.T) {
	models := parseOmpModels("No models available. Set API keys in environment variables.")
	if len(models) != 0 {
		t.Errorf("expected 0 models for no-models message, got %d", len(models))
	}
}