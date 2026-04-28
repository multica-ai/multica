package daemon

import (
	"strings"
	"testing"
)

func TestBuildPromptResumedSessionIncludesRehydrationHint(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	prompt := BuildPrompt(Task{
		IssueID:        issueID,
		PriorSessionID: "sess-123",
	})

	if !strings.Contains(prompt, "resumed from a previous turn") {
		t.Errorf("prompt missing re-hydration hint\n---\n%s", prompt)
	}
	if !strings.Contains(prompt, "REFERENCE.md") {
		t.Errorf("prompt missing REFERENCE.md reference\n---\n%s", prompt)
	}
}

func TestBuildPromptFreshSessionOmitsRehydrationHint(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	prompt := BuildPrompt(Task{
		IssueID: issueID,
	})

	if strings.Contains(prompt, "resumed from a previous turn") {
		t.Errorf("fresh session prompt should not contain re-hydration hint\n---\n%s", prompt)
	}
}

func TestBuildPromptChatResumedSessionIncludesHint(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		ChatSessionID:  "chat-1",
		ChatMessage:    "hello",
		PriorSessionID: "sess-123",
	})

	if !strings.Contains(prompt, "resumed from a previous turn") {
		t.Errorf("chat prompt missing re-hydration hint\n---\n%s", prompt)
	}
}

func TestBuildPromptAutopilotResumedSessionIncludesHint(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		AutopilotRunID:          "run-1",
		AutopilotDescription:    "check deps",
		PriorSessionID:          "sess-123",
	})

	if !strings.Contains(prompt, "resumed from a previous turn") {
		t.Errorf("autopilot prompt missing re-hydration hint\n---\n%s", prompt)
	}
}
