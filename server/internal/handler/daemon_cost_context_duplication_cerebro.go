package handler

// CEREBRO-PATCH(daemon-context-duplication): FIR-2765 measure duplicate content
// across composed meta-skill sections at task claim.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	costSavingContextDuplication = "context_duplication"
	costMetricTokens             = "tokens"
)

// applyContextDuplicationSaving scores the composed runtime brief for duplicate
// sentences across sections when the saving is shadow or on.
func (h *Handler) applyContextDuplicationSaving(
	ctx context.Context,
	resp *AgentTaskResponse,
	issue db.Issue,
	taskID pgtype.UUID,
) {
	if h.CerebroQueries == nil || !issue.WorkspaceID.Valid || !taskID.Valid {
		return
	}
	mode := h.contextDuplicationMode(ctx, issue.WorkspaceID)
	if mode != costSavingModeShadow && mode != costSavingModeOn {
		return
	}

	envCtx := execenv.TaskContextForEnv{
		IssueID:              uuidToString(issue.ID),
		AgentID:              resp.AgentID,
		AgentName:            taskAgentName(resp.Agent),
		AgentInstructions:    taskAgentInstructions(resp.Agent),
		WorkspaceContext:     resp.WorkspaceContext,
		IssueSnapshotInlined: resp.IssueSnapshot != "",
		BundleContextHint:    resp.BundleContextHint,
	}
	if len(resp.Repos) > 0 {
		for _, r := range resp.Repos {
			envCtx.Repos = append(envCtx.Repos, execenv.RepoContextForEnv{
				URL:         r.URL,
				Description: r.Description,
			})
		}
	}

	// CEREBRO-PATCH(context-duplication-full-prompt): include repo prompt files in duplication totals.
	repoURL := ""
	if len(resp.Repos) > 0 {
		repoURL = strings.TrimSpace(resp.Repos[0].URL)
	}
	repoSnap := execenv.LoadRepoPromptSnapshot(ctx, repoURL, strings.TrimSpace(resp.WorkDir))
	inspection := execenv.InspectMetaSkill("claude", envCtx, repoSnap)
	dup := inspection.Duplication

	detail, _ := json.Marshal(map[string]any{
		"composed_tokens":      inspection.ComposedTokens,
		"duplicate_pct":        dup.DuplicatePct,
		"top_overlap_sections": dup.TopOverlapSections,
	})

	if err := h.CerebroQueries.RecordCerebroCostOptimizationMeasurement(ctx, cerebrodb.RecordCerebroCostOptimizationMeasurementParams{
		WorkspaceID:     issue.WorkspaceID,
		TaskID:          taskID,
		SavingKey:       costSavingContextDuplication,
		Mode:            mode,
		Applied:         mode == costSavingModeOn,
		HeldOut:         false,
		Metric:          costMetricTokens,
		BaselineValue:   dup.TotalTokens,
		EffectiveValue:  dup.TotalTokens - dup.DuplicateTokens,
		SavedCents:      0,
		ActualCostCents: 0,
		DetailJson:      detail,
	}); err != nil {
		slog.Warn("context_duplication cost-saving: record measurement failed", "error", err)
	}
}

func (h *Handler) contextDuplicationMode(ctx context.Context, workspaceID pgtype.UUID) string {
	rows, err := h.CerebroQueries.ListCerebroCostOptimization(ctx, workspaceID)
	if err != nil {
		slog.Warn("context_duplication cost-saving: list modes failed", "error", err)
		return ""
	}
	for _, row := range rows {
		if row.SavingKey == costSavingContextDuplication {
			return row.Mode
		}
	}
	return ""
}

func taskAgentName(a *TaskAgentData) string {
	if a == nil {
		return ""
	}
	return a.Name
}

func taskAgentInstructions(a *TaskAgentData) string {
	if a == nil {
		return ""
	}
	return a.Instructions
}
