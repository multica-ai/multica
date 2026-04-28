package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestContractSizeUnder500Tokens verifies that the compact contract stays under
// 500 tokens for all task types.
func TestContractSizeUnder500Tokens(t *testing.T) {
	t.Parallel()

	baseCtx := TaskContextForEnv{
		IssueID:           "issue-1",
		AgentID:           "agent-1",
		AgentName:         "Test Agent",
		AgentInstructions: "You are a test agent. Be thorough and complete all work before reporting done.",
	}

	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{"assignment", baseCtx},
		{"comment-triggered", func() TaskContextForEnv {
			c := baseCtx
			c.TriggerCommentID = "comment-1"
			return c
		}()},
		{"autopilot", func() TaskContextForEnv {
			c := baseCtx
			c.IssueID = ""
			c.AutopilotRunID = "run-1"
			c.AutopilotTitle = "Daily check"
			c.AutopilotDescription = "Check dependencies."
			return c
		}()},
		{"chat", func() TaskContextForEnv {
			c := baseCtx
			c.IssueID = ""
			c.ChatSessionID = "chat-1"
			return c
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			contract := BuildContract(tc.ctx)
			tokens := estimateTokens(contract)
			if tokens > 500 {
				t.Errorf("contract size = %d tokens, want <= 500\n---\n%s", tokens, contract)
			}
		})
	}
}

// TestContractContainsIdentityAnchor verifies that the agent's first-paragraph
// identity is present in the contract.
func TestContractContainsIdentityAnchor(t *testing.T) {
	t.Parallel()

	ctx := TaskContextForEnv{
		AgentName:         "Atlas",
		AgentInstructions: "You are a senior backend engineer.\n\nYou specialize in Go and distributed systems.",
	}

	contract := BuildContract(ctx)
	if !strings.Contains(contract, "You are a senior backend engineer.") {
		t.Errorf("contract missing identity anchor\n---\n%s", contract)
	}
	if strings.Contains(contract, "You specialize in Go and distributed systems.") {
		t.Error("contract should not contain second paragraph of instructions")
	}
}

// TestIdentityAnchor200TokenCap verifies that instructions longer than 200
// tokens are truncated.
func TestIdentityAnchor200TokenCap(t *testing.T) {
	t.Parallel()

	longInstructions := strings.Repeat("You are a very detailed agent with many instructions. ", 30)
	anchor := extractIdentityAnchor(longInstructions)
	if estimateTokens(anchor) > 210 {
		t.Errorf("identity anchor = %d tokens, want <= ~200", estimateTokens(anchor))
	}
	if !strings.HasSuffix(anchor, "…") {
		t.Error("expected identity anchor to be truncated with ellipsis")
	}
}

// TestReferenceContainsFullMetaSkill verifies that REFERENCE.md contains the
// full meta skill content including agent instructions and commands.
func TestReferenceContainsFullMetaSkill(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID:           "issue-1",
		AgentID:           "agent-1",
		AgentName:         "Test",
		AgentInstructions: "You are a test agent.",
		AgentSkills:       []SkillContextForEnv{{Name: "Coding", Content: "Write good code."}},
	}

	pm, err := WritePromptStratificationFiles(dir, "claude", ctx)
	if err != nil {
		t.Fatalf("WritePromptStratificationFiles failed: %v", err)
	}
	if pm.ReferenceTokens == 0 {
		t.Error("expected non-zero reference_tokens")
	}

	data, err := os.ReadFile(filepath.Join(dir, "REFERENCE.md"))
	if err != nil {
		t.Fatalf("failed to read REFERENCE.md: %v", err)
	}
	s := string(data)

	for _, want := range []string{
		"Multica Agent Runtime",
		"You are: Test",
		"You are a test agent.",
		"multica issue get",
		"Coding",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("REFERENCE.md missing %q", want)
		}
	}
}

// TestReferenceWriteFailureIsFatal verifies that a failure to write
// REFERENCE.md returns an error.
func TestReferenceWriteFailureIsFatal(t *testing.T) {
	t.Parallel()

	// Use a non-existent nested path to force a write error.
	badDir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(badDir, 0o555); err != nil {
		t.Fatal(err)
	}

	ctx := TaskContextForEnv{IssueID: "issue-1"}
	_, err := WritePromptStratificationFiles(badDir, "claude", ctx)
	if err == nil {
		t.Fatal("expected error when REFERENCE.md write fails")
	}
}

// TestContractWrittenForObservability verifies that CONTRACT.md is written
// even though write failure is non-fatal.
func TestContractWrittenForObservability(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{IssueID: "issue-1", AgentName: "Test"}
	_, err := WritePromptStratificationFiles(dir, "claude", ctx)
	if err != nil {
		t.Fatalf("WritePromptStratificationFiles failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "CONTRACT.md"))
	if err != nil {
		t.Fatalf("failed to read CONTRACT.md: %v", err)
	}

	s := string(data)
	if !strings.Contains(s, "issue-1") {
		t.Errorf("CONTRACT.md missing issue ID")
	}
	if !strings.Contains(s, "Test") {
		t.Errorf("CONTRACT.md missing agent name")
	}
}

// TestIsInlineProvider covers the inline provider classification.
func TestIsInlineProvider(t *testing.T) {
	t.Parallel()

	inline := []string{"claude", "codex", "hermes", "kimi", "kiro", "openclaw", "pi", "opencode"}
	fileOnly := []string{"cursor", "copilot", "gemini"}

	for _, p := range inline {
		if !IsInlineProvider(p) {
			t.Errorf("expected %q to be inline", p)
		}
	}
	for _, p := range fileOnly {
		if IsInlineProvider(p) {
			t.Errorf("expected %q to be file-only", p)
		}
	}
}
