package daemon

// CEREBRO-PATCH(daemon-prompt-test): cerebro modification of upstream file

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

// TestBuildQuickCreatePromptRules locks in the rules that govern how the
// quick-create agent is allowed to translate raw user input into the issue
// description body. Each substring corresponds to a concrete failure mode
// observed in production output:
//   - meta-instructions ("create an issue", "cc @X") leaking into the body
//   - the Context section being misused as an apology log when no external
//     references were actually fetched
//   - hard-line rules being silently dropped on prompt rewrites
func TestBuildQuickCreatePromptRules(t *testing.T) {
	out := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})

	mustContain := []string{
		// high-fidelity invariant
		"Faithfully restate what the user wants",
		"Preserve specific names, identifiers, file paths",
		// strip non-spec material: verbal routing wrappers + conversational fillers
		"verbal routing wrappers about creating the issue",
		"pure conversational fillers",
		// cc routing must survive: mention link stays in description so the
		// auto-subscribe path fires (multica issue create has no --subscriber flag)
		"CC exception",
		"auto-subscribes members",
		// context section is conditional and must not be an apology log
		"include ONLY when the input cited external resources",
		"never use it as an apology log",
		// hard rules
		"never invent requirements",
		"never reduce multi-sentence input",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt output missing required rule: %q", s)
		}
	}
}

// TestBuildQuickCreatePromptProjectPinning verifies that when the user
// pins a project in the quick-create modal, the prompt instructs the agent
// to pass `--project <uuid>` exactly. Without this, the agent would re-read
// the workspace default and silently drop the user's selection — the same
// "I have to retype 'in project X' every time" failure mode the modal
// addition was meant to fix.
func TestBuildQuickCreatePromptProjectPinning(t *testing.T) {
	const projectID = "11111111-2222-3333-4444-555555555555"
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		ProjectID:         projectID,
		ProjectTitle:      "Web App",
	})
	mustContain := []string{
		"--project \"" + projectID + "\"",
		"Web App",
		"modal selection is authoritative",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with project missing %q\n--- output ---\n%s", s, out)
		}
	}

	// Without a project, the prompt must keep the legacy "omit" instruction
	// so the agent doesn't accidentally start passing --project on plain
	// quick-create runs.
	plain := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	if !strings.Contains(plain, "**project**: omit") {
		t.Errorf("buildQuickCreatePrompt without project must keep the omit instruction, got:\n%s", plain)
	}
	if strings.Contains(plain, "--project") {
		t.Errorf("buildQuickCreatePrompt without project must NOT mention --project, got:\n%s", plain)
	}
}
