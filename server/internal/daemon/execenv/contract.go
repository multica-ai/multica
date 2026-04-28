package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PromptMetrics holds token counts and compaction signal for a task's prompt
// stratification.
type PromptMetrics struct {
	SystemPromptTokens int
	ReferenceTokens    int
	InstructionsTokens int
	CompactionDetected bool
}

// estimateTokens returns a rough token count using the standard ~4 chars/token
// heuristic. It is intentionally fast and dependency-free.
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text) / 4
}

// extractIdentityAnchor returns the first paragraph of instructions, truncated
// to a 200-token hard cap. If instructions are empty, it returns a default
// anchor.
func extractIdentityAnchor(instructions string) string {
	if instructions == "" {
		return "You are a coding agent in the Multica platform."
	}
	// Split on first blank line to get the first paragraph.
	para := instructions
	if idx := strings.Index(instructions, "\n\n"); idx >= 0 {
		para = instructions[:idx]
	}
	para = strings.TrimSpace(para)
	if estimateTokens(para) > 200 {
		cutoff := 800 // ~200 tokens
		if len(para) > cutoff {
			para = para[:cutoff] + "…"
		}
	}
	return para
}

// BuildContract constructs the compact, task-type-aware runtime contract that
// inline providers receive via SystemPrompt. It stays under 500 tokens.
func BuildContract(ctx TaskContextForEnv) string {
	var b strings.Builder

	// Identity anchor
	if ctx.AgentName != "" {
		fmt.Fprintf(&b, "You are %s", ctx.AgentName)
		if ctx.AgentID != "" {
			fmt.Fprintf(&b, " (ID: `%s`)", ctx.AgentID)
		}
		b.WriteString(". ")
	}
	b.WriteString(extractIdentityAnchor(ctx.AgentInstructions))
	b.WriteString("\n\n")

	// Task-type-aware body
	switch {
	case ctx.ChatSessionID != "":
		b.WriteString("TASK: Chat session. Respond to the user's message. You have full access to the `multica` CLI. Keep responses concise.\n")
	case ctx.AutopilotRunID != "":
		b.WriteString("TASK: Autopilot run")
		if ctx.AutopilotRunID != "" {
			fmt.Fprintf(&b, " `%s`", ctx.AutopilotRunID)
		}
		b.WriteString(" — run-only mode, no issue.\n")
		if ctx.AutopilotTitle != "" {
			fmt.Fprintf(&b, "Title: %s\n", ctx.AutopilotTitle)
		}
		b.WriteString("Complete the autopilot instructions. Do not run `multica issue get` unless instructed.\n")
	case ctx.TriggerCommentID != "":
		fmt.Fprintf(&b, "TASK: Issue `%s` — reply to a NEW comment (trigger ID: `%s`).\n", ctx.IssueID, ctx.TriggerCommentID)
		b.WriteString("1. Run `multica issue get` and read comments to understand the request.\n")
		b.WriteString("2. Decide if a reply is warranted. If yes, post results as a comment.\n")
		b.WriteString("3. Do NOT change issue status unless explicitly asked.\n")
	default:
		fmt.Fprintf(&b, "TASK: Issue `%s` — new assignment.\n", ctx.IssueID)
		b.WriteString("1. Run `multica issue get` to understand the task.\n")
		b.WriteString("2. Do the work.\n")
		b.WriteString("3. Post final results as a comment.\n")
		b.WriteString("4. Update status to `in_review` when done.\n")
	}

	b.WriteString("\n")

	// Critical output contract (must survive compaction)
	b.WriteString("⚠️ Final results MUST be delivered via `multica issue comment add`. Terminal output is invisible to users.\n")
	b.WriteString("Mention links have side effects: `mention://agent/<id>` enqueues a new run. Don't @mention as a sign-off.\n")
	b.WriteString("Read REFERENCE.md for full commands, mention rules, and workflow details.\n")

	return b.String()
}

// BuildReferenceContent returns the full reference markdown for the agent.
// It reuses the existing meta-skill generator.
func BuildReferenceContent(provider string, ctx TaskContextForEnv) string {
	return buildMetaSkillContent(provider, ctx)
}

// WritePromptStratificationFiles writes CONTRACT.md and REFERENCE.md into the
// workdir. REFERENCE.md contains the full meta skill; CONTRACT.md contains the
// compact contract. A failure to write REFERENCE.md is returned as an error;
// CONTRACT.md failure is non-fatal.
func WritePromptStratificationFiles(workDir string, provider string, ctx TaskContextForEnv) (PromptMetrics, error) {
	pm := PromptMetrics{}

	// Write REFERENCE.md (full reference) — fatal on failure.
	reference := BuildReferenceContent(provider, ctx)
	pm.ReferenceTokens = estimateTokens(reference)
	pm.InstructionsTokens = estimateTokens(ctx.AgentInstructions)

	refPath := filepath.Join(workDir, "REFERENCE.md")
	if err := os.WriteFile(refPath, []byte(reference), 0o644); err != nil {
		return pm, fmt.Errorf("write REFERENCE.md: %w", err)
	}

	// Write CONTRACT.md (compact contract — for debugging/observability).
	contract := BuildContract(ctx)
	pm.SystemPromptTokens = estimateTokens(contract)

	contractPath := filepath.Join(workDir, "CONTRACT.md")
	_ = os.WriteFile(contractPath, []byte(contract), 0o644) // non-fatal

	return pm, nil
}

// IsInlineProvider reports whether the provider receives instructions via
// SystemPrompt rather than a native config file.
func IsInlineProvider(provider string) bool {
	switch strings.ToLower(provider) {
	case "claude", "codex", "hermes", "kimi", "kiro", "openclaw", "pi", "opencode":
		return true
	}
	return false
}
