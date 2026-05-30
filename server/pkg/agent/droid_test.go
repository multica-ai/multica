package agent

import (
	"log/slog"
	"strings"
	"testing"
)

func TestNewReturnsDroidBackend(t *testing.T) {
	t.Parallel()

	b, err := New("droid", Config{ExecutablePath: "/nonexistent/droid"})
	if err != nil {
		t.Fatalf("New(droid) error: %v", err)
	}
	if _, ok := b.(*droidBackend); !ok {
		t.Fatalf("expected *droidBackend, got %T", b)
	}
}

func TestBuildDroidArgsBaseline(t *testing.T) {
	t.Parallel()

	args := buildDroidArgs("fix the bug", ExecOptions{}, slog.Default())
	expected := []string{
		"exec",
		"-o", "stream-json",
		"--auto", "medium",
		"fix the bug",
	}

	if strings.Join(args, "\x00") != strings.Join(expected, "\x00") {
		t.Fatalf("expected %v, got %v", expected, args)
	}
}

func TestBuildDroidArgsWithOptions(t *testing.T) {
	t.Parallel()

	args := buildDroidArgs("continue", ExecOptions{
		Cwd:             "/tmp/work",
		Model:           "gpt-5.5",
		ThinkingLevel:   "high",
		ResumeSessionID: "ses_123",
		SystemPrompt:    "follow Multica instructions",
	}, slog.Default())

	wantPairs := map[string]string{
		"--cwd":                  "/tmp/work",
		"-m":                     "gpt-5.5",
		"-r":                     "high",
		"--session-id":           "ses_123",
		"--append-system-prompt": "follow Multica instructions",
	}
	for flag, want := range wantPairs {
		found := false
		for i, arg := range args {
			if arg == flag {
				if i+1 >= len(args) || args[i+1] != want {
					t.Fatalf("expected %s followed by %q, got %v", flag, want, args)
				}
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %s in args, got %v", flag, args)
		}
	}
	if got := args[len(args)-1]; got != "continue" {
		t.Fatalf("expected prompt to stay last, got %q in %v", got, args)
	}
}

func TestBuildDroidArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildDroidArgs("hi", ExecOptions{
		CustomArgs: []string{"-o", "json", "--enabled-tools", "ApplyPatch"},
	}, slog.Default())

	for i, arg := range args {
		if arg == "-o" && i+1 < len(args) && args[i+1] == "json" {
			t.Fatalf("blocked -o json should have been filtered: %v", args)
		}
	}
	var foundEnabledTools bool
	for i, arg := range args {
		if arg == "--enabled-tools" && i+1 < len(args) && args[i+1] == "ApplyPatch" {
			foundEnabledTools = true
		}
	}
	if !foundEnabledTools {
		t.Fatalf("expected --enabled-tools ApplyPatch to pass through, got %v", args)
	}
	if got := args[len(args)-1]; got != "hi" {
		t.Fatalf("expected prompt to stay last, got %q in %v", got, args)
	}
}

func TestDroidProcessEvents(t *testing.T) {
	t.Parallel()

	backend := &droidBackend{cfg: Config{Logger: slog.Default()}}
	input := strings.NewReader(strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"ses_1"}`,
		`{"type":"message","role":"assistant","text":"Working","session_id":"ses_1"}`,
		`{"type":"completion","finalText":"Done","durationMs":42,"session_id":"ses_1","usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}`,
	}, "\n"))
	ch := make(chan Message, 8)

	result := backend.processEvents(input, ch)
	close(ch)

	if result.status != "completed" {
		t.Fatalf("expected completed status, got %q", result.status)
	}
	if result.output != "Done" {
		t.Fatalf("expected final output Done, got %q", result.output)
	}
	if result.sessionID != "ses_1" {
		t.Fatalf("expected session ID ses_1, got %q", result.sessionID)
	}
	usage := result.usage["droid"]
	if usage.InputTokens != 10 || usage.OutputTokens != 2 || usage.CacheReadTokens != 3 || usage.CacheWriteTokens != 4 {
		t.Fatalf("unexpected usage: %+v", usage)
	}

	var sawText bool
	for msg := range ch {
		if msg.Type == MessageText && msg.Content == "Working" {
			sawText = true
		}
	}
	if !sawText {
		t.Fatalf("expected assistant text message to be emitted")
	}
}
