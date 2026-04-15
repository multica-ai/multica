package daemon

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNormalizeServerBaseURL(t *testing.T) {
	t.Parallel()

	got, err := NormalizeServerBaseURL("ws://localhost:8080/ws")
	if err != nil {
		t.Fatalf("NormalizeServerBaseURL returned error: %v", err)
	}
	if got != "http://localhost:8080" {
		t.Fatalf("expected http://localhost:8080, got %s", got)
	}
}

func TestBuildPromptContainsIssueID(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	prompt := BuildPrompt(Task{
		IssueID: issueID,
		Agent: &AgentData{
			Name: "Local Codex",
			Skills: []SkillData{
				{Name: "Concise", Content: "Be concise."},
			},
		},
	})

	// Prompt should contain the issue ID and CLI hint.
	for _, want := range []string{
		issueID,
		"multica issue get",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}

	// Skills should NOT be inlined in the prompt (they're in runtime config).
	for _, absent := range []string{"## Agent Skills", "Be concise."} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q (skills are in runtime config)", absent)
		}
	}
}

func TestBuildPromptNoIssueDetails(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID: "test-id",
		Agent:   &AgentData{Name: "Test"},
	})

	// Prompt should not contain issue title/description (agent fetches via CLI).
	for _, absent := range []string{"**Issue:**", "**Summary:**"} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q — agent fetches details via CLI", absent)
		}
	}
}

func TestIsWorkspaceNotFoundError(t *testing.T) {
	t.Parallel()

	err := &requestError{
		Method:     http.MethodPost,
		Path:       "/api/daemon/register",
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"workspace not found"}`,
	}
	if !isWorkspaceNotFoundError(err) {
		t.Fatal("expected workspace not found error to be recognized")
	}

	if isWorkspaceNotFoundError(&requestError{StatusCode: http.StatusInternalServerError, Body: `{"error":"workspace not found"}`}) {
		t.Fatal("did not expect 500 to be treated as workspace not found")
	}
}

func TestEmptyOutputErrorPreservesSpecificAgentError(t *testing.T) {
	t.Parallel()

	err := emptyOutputError("codex", "codex upstream connection failed: OpenAI responses websocket returned 500 Internal Server Error")
	if got := err.Error(); !strings.Contains(got, "responses websocket returned 500") {
		t.Fatalf("expected specific error to be preserved, got %q", got)
	}
}

func TestEmptyOutputErrorFallsBackToGenericMessage(t *testing.T) {
	t.Parallel()

	err := emptyOutputError("codex", "   ")
	if got := err.Error(); got != "codex returned empty output" {
		t.Fatalf("got %q, want generic empty-output message", got)
	}
}

func TestIsCodexTransientUpstreamError(t *testing.T) {
	t.Parallel()

	err := emptyOutputError("codex", "codex upstream connection failed: OpenAI responses websocket returned 500 Internal Server Error")
	if !isCodexTransientUpstreamError(err) {
		t.Fatal("expected transient upstream error to be recognized")
	}
}

func TestShouldRetryCodexTask(t *testing.T) {
	t.Parallel()

	err := emptyOutputError("codex", "codex upstream connection failed: OpenAI responses websocket returned 500 Internal Server Error")
	if !shouldRetryCodexTask("codex", err, 1, 3) {
		t.Fatal("expected first codex attempt to be retryable")
	}
	if !shouldRetryCodexTask("codex", err, 2, 3) {
		t.Fatal("expected second codex attempt to be retryable")
	}
	if shouldRetryCodexTask("codex", err, 3, 3) {
		t.Fatal("did not expect retry once max attempts are exhausted")
	}
	if shouldRetryCodexTask("claude", err, 1, 3) {
		t.Fatal("did not expect non-codex provider to retry on codex-specific upstream error")
	}
}

func TestCodexRetryDefaults(t *testing.T) {
	t.Parallel()

	if DefaultCodexRetryAttempts != 2 {
		t.Fatalf("got retry attempts %d, want 2", DefaultCodexRetryAttempts)
	}
	if DefaultCodexRetryJitter != 1500*time.Millisecond {
		t.Fatalf("got retry jitter %s, want 1.5s", DefaultCodexRetryJitter)
	}
}

func TestCodexRetryDelayWithoutJitter(t *testing.T) {
	t.Parallel()

	base := 5 * time.Second
	if got := codexRetryDelay(base, 0, 1); got != 5*time.Second {
		t.Fatalf("got %s, want 5s", got)
	}
	if got := codexRetryDelay(base, 0, 2); got != 10*time.Second {
		t.Fatalf("got %s, want 10s", got)
	}
	if got := codexRetryDelay(base, 0, 3); got != 20*time.Second {
		t.Fatalf("got %s, want 20s", got)
	}
}

func TestCodexRetryDelayAddsJitter(t *testing.T) {
	t.Parallel()

	original := codexRetryJitterInt63n
	codexRetryJitterInt63n = func(n int64) int64 {
		want := int64(1500*time.Millisecond) + 1
		if n != want {
			t.Fatalf("got jitter range %d, want %d", n, want)
		}
		return int64(400 * time.Millisecond)
	}
	defer func() {
		codexRetryJitterInt63n = original
	}()

	if got := codexRetryDelay(5*time.Second, 1500*time.Millisecond, 2); got != 10*time.Second+400*time.Millisecond {
		t.Fatalf("got %s, want 10.4s", got)
	}
}

func TestCodexRetryDelayClampsJitterToHalfDelay(t *testing.T) {
	t.Parallel()

	original := codexRetryJitterInt63n
	codexRetryJitterInt63n = func(n int64) int64 {
		want := int64(1*time.Second) + 1
		if n != want {
			t.Fatalf("got jitter range %d, want %d", n, want)
		}
		return int64(500 * time.Millisecond)
	}
	defer func() {
		codexRetryJitterInt63n = original
	}()

	if got := codexRetryDelay(2*time.Second, 3*time.Second, 1); got != 2500*time.Millisecond {
		t.Fatalf("got %s, want 2.5s", got)
	}
}
