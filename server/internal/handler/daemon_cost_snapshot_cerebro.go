package handler

// CEREBRO-PATCH(daemon-snapshot-saving): server-side implementation of the
// "snapshot_prompt" cost saving (FIR-2384). When a workspace turns the saving
// on, the daemon claim handler inlines the issue + its recent thread into the
// agent start prompt so the agent skips the per-run `multica issue get` +
// `multica issue comment list` round-trip. Both the behaviour AND its
// measurement live here, server-side at claim time — nothing in the daemon
// runtime has to change, and no daemon→server reporting channel is needed.
// CEREBRO-PATCH(cost-savings-context-tokens): FIR-2572 — measure snapshot_prompt in context_tokens.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

// Cost-saving keys + modes mirror packages/cerebro-cost-optimization/registry.ts
// (the UI source of truth) and the runtime constants in
// server/internal/cerebro/runtime/cost_savings_measure.go. "off" is the absence
// of an override row, so it never appears as a value here.
const (
	costSavingSnapshotKey   = "snapshot_prompt"
	costSavingModeShadow    = "shadow"
	costSavingModeOn        = "on"
	// snapshotInlinedReads is how many platform reads the snapshot replaces per
	// run: the `multica issue get` + `multica issue comment list` the agent
	// would otherwise run at the start of every comment-/assignment-triggered
	// task. It is the measured baseline for this saving.
	snapshotInlinedReads = 2

	// snapshotThreadCommentLimit caps how many of the most recent comments the
	// snapshot inlines. Issue p99 is ~30 comments; bounding it keeps the start
	// prompt from ballooning on long-running threads.
	snapshotThreadCommentLimit = 30
)

// applySnapshotSaving wires the snapshot_prompt cost saving into a daemon task
// claim. Best-effort: any failure (no cerebro queries, DB error) leaves the
// claim untouched, so a measurement bug can never block real work. On "on" it
// renders the snapshot into resp.IssueSnapshot AND records the applied saving;
// on "shadow" it records the would-save without changing the prompt; on
// "off"/absent it does nothing.
func (h *Handler) applySnapshotSaving(ctx context.Context, resp *AgentTaskResponse, issue db.Issue, triggerCommentID, taskID pgtype.UUID) {
	if h.CerebroQueries == nil || !issue.WorkspaceID.Valid || !taskID.Valid {
		return
	}
	mode := h.snapshotSavingMode(ctx, issue.WorkspaceID)
	if mode != costSavingModeOn && mode != costSavingModeShadow {
		return
	}

	var snapshotChars int64
	if mode == costSavingModeOn {
		comments, err := h.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Limit:       2000, // defensive cap; the renderer keeps only the most recent slice
		})
		if err != nil {
			// Could not load the thread — do NOT claim a saving we didn't apply.
			slog.Warn("snapshot cost-saving: list comments failed", "error", err)
			return
		}
		resp.IssueSnapshot = renderIssueSnapshot(issue, comments, triggerCommentID, resp.AgentID)
		snapshotChars = int64(len(resp.IssueSnapshot))
	} else {
		// Shadow: estimate the context volume we would have inlined.
		snapshotChars = estimateSnapshotContextChars(issue, triggerCommentID)
	}

	if err := h.CerebroQueries.RecordCerebroCostOptimizationMeasurement(ctx, snapshotMeasurementParams(issue.WorkspaceID, taskID, mode, snapshotChars)); err != nil {
		slog.Warn("snapshot cost-saving: record measurement failed", "error", err)
	}
}

// snapshotSavingMode returns the workspace's mode for the snapshot_prompt saving
// ("on"/"shadow"), or "" when off/absent or on lookup error.
func (h *Handler) snapshotSavingMode(ctx context.Context, workspaceID pgtype.UUID) string {
	rows, err := h.CerebroQueries.ListCerebroCostOptimization(ctx, workspaceID)
	if err != nil {
		slog.Warn("snapshot cost-saving: list modes failed", "error", err)
		return ""
	}
	for _, row := range rows {
		if row.SavingKey == costSavingSnapshotKey {
			return row.Mode
		}
	}
	return ""
}

// snapshotMeasurementParams builds the measurement row for one claim. Baseline
// is the estimated context tokens the snapshot inlines (chars/4); effective is 0.
func snapshotMeasurementParams(workspaceID, taskID pgtype.UUID, mode string, contextChars int64) cerebrodb.RecordCerebroCostOptimizationMeasurementParams {
	savedTokens := contextChars / 4
	if savedTokens <= 0 {
		savedTokens = int64(snapshotInlinedReads) * 8000
	}
	return cerebrodb.RecordCerebroCostOptimizationMeasurementParams{
		WorkspaceID:     workspaceID,
		TaskID:          taskID,
		SavingKey:       costSavingSnapshotKey,
		Mode:            mode,
		Applied:         mode == costSavingModeOn,
		HeldOut:         false,
		Metric:          "context_tokens",
		BaselineValue:   savedTokens,
		EffectiveValue:  0,
		SavedCents:      0,
		ActualCostCents: 0,
	}
}

// estimateSnapshotContextChars approximates the snapshot block size in shadow
// mode when we do not render it into the prompt.
func estimateSnapshotContextChars(issue db.Issue, triggerCommentID pgtype.UUID) int64 {
	var n int64
	n += int64(len(strings.TrimSpace(issue.Title)))
	if issue.Description.Valid {
		n += int64(len(strings.TrimSpace(issue.Description.String)))
	}
	// Conservative per-read allowance when we have not loaded the full thread.
	n += int64(snapshotInlinedReads) * 4000
	if triggerCommentID.Valid {
		n += 2000
	}
	return n
}

// renderIssueSnapshot formats the issue core + its recent thread into the block
// inlined at the top of the start prompt. The trigger comment is excluded (it is
// already inlined separately as the [NEW COMMENT] block) and non-comment/system
// rows are skipped. Only the most recent snapshotThreadCommentLimit comments are
// kept so the prompt stays bounded.
func renderIssueSnapshot(issue db.Issue, comments []db.Comment, triggerCommentID pgtype.UUID, selfAgentID string) string {
	var b strings.Builder
	b.WriteString("## Your issue (already fetched — do not re-fetch)\n\n")
	fmt.Fprintf(&b, "Issue #%d: %s\n", issue.Number, strings.TrimSpace(issue.Title))
	fmt.Fprintf(&b, "Status: %s · Priority: %s\n", issue.Status, issue.Priority)
	if desc := strings.TrimSpace(issue.Description.String); desc != "" {
		b.WriteString("\n")
		b.WriteString(desc)
		b.WriteString("\n")
	}

	type threadLine struct{ who, content string }
	lines := make([]threadLine, 0, len(comments))
	for _, c := range comments {
		if triggerCommentID.Valid && c.ID == triggerCommentID {
			continue
		}
		if c.Type != "comment" { // skip system rows (status changes, etc.)
			continue
		}
		content := strings.TrimSpace(c.Content)
		if content == "" {
			continue
		}
		who := "A user"
		switch c.AuthorType {
		case "agent":
			who = "An agent"
			if selfAgentID != "" && c.AuthorID.Valid && uuidToString(c.AuthorID) == selfAgentID {
				who = "You (earlier)"
			}
		case "system":
			who = "System"
		}
		lines = append(lines, threadLine{who: who, content: content})
	}
	if len(lines) > snapshotThreadCommentLimit {
		lines = lines[len(lines)-snapshotThreadCommentLimit:]
	}
	if len(lines) > 0 {
		b.WriteString("\n## Recent thread (oldest first; most recent last)\n\n")
		for _, l := range lines {
			fmt.Fprintf(&b, "%s:\n%s\n\n", l.who, l.content)
		}
	}
	return strings.TrimSpace(b.String())
}
