package daemon

import (
	"strings"
	"testing"
)

// TestBuildChatPrompt_SingleMessage covers the common case: one user message
// in a chat session, framed as "User message:" so the agent gets the same
// shape it had before mid-stream coalescing was introduced.
func TestBuildChatPrompt_SingleMessage(t *testing.T) {
	t.Parallel()

	got := buildChatPrompt(Task{
		ChatSessionID: "abc",
		ChatMessages:  []string{"hello world"},
	})

	if !strings.Contains(got, "User message:\nhello world") {
		t.Fatalf("expected single-message framing, got:\n%s", got)
	}
	if strings.Contains(got, "multiple messages") {
		t.Fatalf("expected no multi-message preamble, got:\n%s", got)
	}
}

// TestBuildChatPrompt_MultipleMessages covers the mid-stream coalesce case:
// the user fired a follow-up while the agent was still responding. The prompt
// must include every queued user message (oldest first) so the agent can
// address all of them — not just the latest. Regression guard for the bug
// Sara caught in the original "coalesce-only" plan, where the daemon only
// loaded the most recent user message and silently dropped the rest.
func TestBuildChatPrompt_MultipleMessages(t *testing.T) {
	t.Parallel()

	got := buildChatPrompt(Task{
		ChatSessionID: "abc",
		ChatMessages:  []string{"first", "second", "third"},
	})

	if !strings.Contains(got, "multiple messages") {
		t.Fatalf("expected multi-message preamble, got:\n%s", got)
	}
	for i, msg := range []string{"first", "second", "third"} {
		if !strings.Contains(got, msg) {
			t.Fatalf("expected message %d (%q) in prompt, got:\n%s", i+1, msg, got)
		}
	}
	// Order matters — oldest first so the agent reads them chronologically.
	if strings.Index(got, "first") > strings.Index(got, "second") ||
		strings.Index(got, "second") > strings.Index(got, "third") {
		t.Fatalf("expected chronological order in prompt, got:\n%s", got)
	}
}

// TestBuildChatPrompt_EmptyMessages defends against a daemon claim that
// somehow returns zero user messages (race where the message row is gone).
// The prompt should still render cleanly rather than panic on a nil index.
func TestBuildChatPrompt_EmptyMessages(t *testing.T) {
	t.Parallel()

	got := buildChatPrompt(Task{ChatSessionID: "abc"})
	if !strings.Contains(got, "User message:") {
		t.Fatalf("expected fallback to single-message framing for empty input, got:\n%s", got)
	}
}
