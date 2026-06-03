package handler

// CEREBRO-PATCH(daemon-snapshot-saving): server-side implementation of the
// "snapshot_prompt" cost saving (FIR-2384). When a workspace turns the saving
// on, the daemon claim handler inlines the issue + its recent thread into the
// agent start prompt so the agent skips the per-run `multica issue get` +
// `multica issue comment list` round-trip. Both the behaviour AND its
// measurement live here, server-side at claim time — nothing in the daemon
// runtime has to change, and no daemon→server reporting channel is needed.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	// CEREBRO-PATCH(cost-optimization-token-metrics): estimate snapshot_prompt saved tokens.
	"github.com/multica-ai/multica/server/internal/cerebro/contextduplication"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

// Cost-saving keys + modes mirror packages/cerebro-cost-optimization/registry.ts
// (the UI source of truth) and the runtime constants in
// server/internal/cerebro/runtime/cost_savings_measure.go. "off" is the absence
// of an override row, so it never appears as a value here.
const (
	costSavingSnapshotKey = "snapshot_prompt"
	costSavingModeShadow  = "shadow"
	costSavingModeOn      = "on"
	costMetricInputTokens = "input_tokens"

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

	comments, err := h.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Limit:       2000, // defensive cap; the renderer keeps only the most recent slice
	})
	if err != nil {
		slog.Warn("snapshot cost-saving: list comments failed", "error", err)
		return
	}
	snapshotText := renderIssueSnapshot(issue, comments, triggerCommentID, resp.AgentID)
	if mode == costSavingModeOn {
		resp.IssueSnapshot = snapshotText
	}

	if err := h.CerebroQueries.RecordCerebroCostOptimizationMeasurement(ctx, snapshotMeasurementParams(issue.WorkspaceID, taskID, mode, snapshotText)); err != nil {
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
// is the estimated tokens in the inlined snapshot; effective is 0 because the
// agent no longer pays to fetch that context via separate tool calls.
func snapshotMeasurementParams(workspaceID, taskID pgtype.UUID, mode, snapshotMarkdown string) cerebrodb.RecordCerebroCostOptimizationMeasurementParams {
	baseline := contextduplication.EstimateTokens(snapshotMarkdown)
	if baseline <= 0 {
		// Fallback when the thread is empty — still two avoided reads.
		baseline = snapshotInlinedReads * 800
	}
	return cerebrodb.RecordCerebroCostOptimizationMeasurementParams{
		WorkspaceID:     workspaceID,
		TaskID:          taskID,
		SavingKey:       costSavingSnapshotKey,
		Mode:            mode,
		Applied:         mode == costSavingModeOn,
		HeldOut:         false,
		Metric:          costMetricInputTokens,
		BaselineValue:   baseline,
		EffectiveValue:  0,
		SavedCents:      0,
		ActualCostCents: 0,
	}
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
