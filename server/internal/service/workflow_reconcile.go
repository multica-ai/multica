package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const WorkflowReconcileEvidenceKind = "workflow_reconcile"

const workflowReconcileHandoff = "The previous run ended while this issue remained in_progress and no follow-up work was planned. Reconcile the workflow now: inspect the issue, its latest comments, child work, and the previous run outcome. Continue any work that can proceed without human input. Before finishing, leave the issue in exactly one truthful state: in_progress only when durable downstream work has been queued, in_review when the requested deliverable is ready, or blocked after posting one concrete decision question with context and a recommendation. Do not leave the issue in_progress with no planned work."

type WorkflowReconcileOutcome string

const (
	WorkflowReconcileSkipped   WorkflowReconcileOutcome = "skipped"
	WorkflowReconcileEnqueued  WorkflowReconcileOutcome = "enqueued"
	WorkflowReconcileAttention WorkflowReconcileOutcome = "attention"
)

type WorkflowReconcileResult struct {
	Outcome       WorkflowReconcileOutcome
	Issue         db.Issue
	SourceTask    db.AgentTaskQueue
	Task          db.AgentTaskQueue
	AttentionNote string
}

// ReconcileStalledWorkflow converges one issue after a task reaches a terminal
// state. A normal run gets one automatic continuation when the issue is still
// in_progress with no planned work. A continuation that reaches the same state
// asks for human attention instead of recursively scheduling itself.
func (s *TaskService) ReconcileStalledWorkflow(ctx context.Context, sourceTaskID pgtype.UUID) (WorkflowReconcileResult, error) {
	result := WorkflowReconcileResult{Outcome: WorkflowReconcileSkipped}
	if s == nil || s.Queries == nil || !sourceTaskID.Valid {
		return result, nil
	}

	source, err := s.Queries.GetAgentTask(ctx, sourceTaskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, nil
		}
		return result, fmt.Errorf("load workflow reconciliation source task: %w", err)
	}
	result.SourceTask = source
	if !source.IssueID.Valid || !isWorkflowReconcileTrigger(source) {
		return result, nil
	}

	baseIssue, err := s.Queries.GetIssue(ctx, source.IssueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, nil
		}
		return result, fmt.Errorf("load workflow reconciliation issue: %w", err)
	}
	result.Issue = baseIssue
	if issuestatus.Effective(ctx, s.Queries, baseIssue.WorkspaceID, baseIssue.Status) != issuestatus.InProgress ||
		!workflowHasAgentOwner(baseIssue) {
		return result, nil
	}

	agent, squadID, isLeader, resolveErr := resolveWorkflowReconcileAgent(ctx, s.Queries, baseIssue)
	if resolveErr != nil && !errors.Is(resolveErr, pgx.ErrNoRows) {
		return result, fmt.Errorf("resolve workflow reconciliation agent: %w", resolveErr)
	}
	agentUnavailable := errors.Is(resolveErr, pgx.ErrNoRows) || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid
	// Overlay construction can call an external provider. Keep it outside the
	// issue row lock; the transaction below verifies the assignee did not change
	// before using this snapshot.
	var overlay runtimeMCPOverlayData
	if !agentUnavailable {
		overlay = s.buildRuntimeMCPOverlay(ctx, source.OriginatorUserID, agent)
	}

	var enqueued bool
	forceFreshSession := workflowReconcileNeedsFreshSession(source)
	err = s.runInTx(ctx, func(qtx *db.Queries) error {
		issue, lockErr := qtx.LockIssueForWorkflowReconcile(ctx, source.IssueID)
		if lockErr != nil {
			return lockErr
		}
		result.Issue = issue
		if issuestatus.Effective(ctx, qtx, issue.WorkspaceID, issue.Status) != issuestatus.InProgress {
			return nil
		}
		if !workflowHasAgentOwner(issue) || issue.AssigneeType != baseIssue.AssigneeType || issue.AssigneeID != baseIssue.AssigneeID {
			return nil
		}

		hasPlanned, activeErr := qtx.HasActiveTaskForIssue(ctx, issue.ID)
		if activeErr != nil {
			return fmt.Errorf("check planned workflow work: %w", activeErr)
		}
		if hasPlanned {
			return nil
		}
		latest, latestErr := qtx.GetLatestTerminalTaskForIssue(ctx, issue.ID)
		if latestErr != nil {
			return fmt.Errorf("load latest terminal workflow task: %w", latestErr)
		}
		if latest.ID != source.ID {
			return nil
		}

		if source.TriggerEvidenceKind.Valid && source.TriggerEvidenceKind.String == WorkflowReconcileEvidenceKind {
			result.Outcome = WorkflowReconcileAttention
			result.AttentionNote = "自动续跑后仍没有后续工作，请决定下一步。"
			return nil
		}
		if agentUnavailable {
			result.Outcome = WorkflowReconcileAttention
			if issue.AssigneeType.String == "squad" {
				result.AttentionNote = "当前负责团队或团队负责人不可用，请重新指定负责人。"
			} else {
				result.AttentionNote = "当前负责智能体不可用，请重新指定负责人或恢复其运行时。"
			}
			return nil
		}

		currentAgent, agentErr := qtx.GetAgent(ctx, agent.ID)
		if agentErr != nil && !errors.Is(agentErr, pgx.ErrNoRows) {
			return fmt.Errorf("reload workflow reconciliation agent: %w", agentErr)
		}
		if errors.Is(agentErr, pgx.ErrNoRows) || currentAgent.ArchivedAt.Valid || !currentAgent.RuntimeID.Valid {
			result.Outcome = WorkflowReconcileAttention
			result.AttentionNote = "当前负责智能体不可用，请重新指定负责人或恢复其运行时。"
			return nil
		}

		task, createErr := qtx.CreateAgentTask(ctx, db.CreateAgentTaskParams{
			ID:                   dbid.NewV7(),
			AgentID:              currentAgent.ID,
			RuntimeID:            currentAgent.RuntimeID,
			IssueID:              issue.ID,
			Priority:             priorityToInt(issue.Priority),
			TriggerSummary:       pgtype.Text{String: "Automatic workflow reconciliation after an unplanned stop", Valid: true},
			ForceFreshSession:    pgtype.Bool{Bool: forceFreshSession, Valid: forceFreshSession},
			IsLeaderTask:         pgtype.Bool{Bool: isLeader, Valid: isLeader},
			HandoffNote:          pgtype.Text{String: workflowReconcileHandoff, Valid: true},
			SquadID:              squadID,
			OriginatorUserID:     source.OriginatorUserID,
			AccountableUserID:    source.AccountableUserID,
			RuntimeMcpOverlay:    overlay.Overlay,
			RuntimeConnectedApps: overlay.ConnectedApps,
			OriginatorSource:     source.OriginatorSource,
			DelegatedFromTaskID:  source.DelegatedFromTaskID,
			RuleVersionID:        source.RuleVersionID,
			TriggerEvidenceKind:  pgtype.Text{String: WorkflowReconcileEvidenceKind, Valid: true},
			TriggerEvidenceRefID: source.ID,
			HeadSha:              headShaText(s.ResolveIssueReviewSHA(ctx, issue.ID)),
		})
		if createErr != nil {
			if isDuplicatePendingTaskErr(createErr) || isDuplicateWorkflowReconcileErr(createErr) {
				return nil
			}
			if errors.Is(createErr, pgx.ErrNoRows) {
				result.Outcome = WorkflowReconcileAttention
				result.AttentionNote = "自动续跑无法创建任务，请检查负责人和运行时配置。"
				return nil
			}
			return fmt.Errorf("create workflow reconciliation task: %w", createErr)
		}
		result.Task = task
		result.Outcome = WorkflowReconcileEnqueued
		enqueued = true
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowReconcileResult{Outcome: WorkflowReconcileSkipped, SourceTask: source, Issue: baseIssue}, nil
	}
	if err != nil {
		return result, err
	}
	if enqueued {
		slog.Info("workflow reconciliation task enqueued",
			"source_task_id", util.UUIDToString(source.ID),
			"task_id", util.UUIDToString(result.Task.ID),
			"issue_id", util.UUIDToString(result.Issue.ID),
		)
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, result.Task)
		s.NotifyTaskEnqueued(ctx, result.Task)
	}
	return result, nil
}

// workflowReconcileNeedsFreshSession prevents a continuation from resuming the
// exact provider session that the daemon already had to kill for inactivity.
// The workflow reconciler itself supplies the single bounded continuation;
// if that continuation also stalls, its workflow_reconcile evidence produces
// human attention instead of another automatic run.
func workflowReconcileNeedsFreshSession(source db.AgentTaskQueue) bool {
	return source.Status == "failed" &&
		source.FailureReason.Valid &&
		source.FailureReason.String == "idle_watchdog"
}

func workflowHasAgentOwner(issue db.Issue) bool {
	return issue.AssigneeType.Valid && issue.AssigneeID.Valid &&
		(issue.AssigneeType.String == "agent" || issue.AssigneeType.String == "squad")
}

func resolveWorkflowReconcileAgent(ctx context.Context, queries *db.Queries, issue db.Issue) (db.Agent, pgtype.UUID, bool, error) {
	if issue.AssigneeType.String == "agent" {
		agent, err := queries.GetAgent(ctx, issue.AssigneeID)
		return agent, pgtype.UUID{}, false, err
	}
	squad, err := queries.GetSquad(ctx, issue.AssigneeID)
	if err != nil {
		return db.Agent{}, pgtype.UUID{}, false, err
	}
	agent, err := queries.GetAgent(ctx, squad.LeaderID)
	return agent, issue.AssigneeID, true, err
}

func isWorkflowReconcileTrigger(task db.AgentTaskQueue) bool {
	switch task.Status {
	case "completed", "failed":
		return true
	case "cancelled":
		// A cancellation without a server failure reason is deliberate: the user
		// stopped the run, edited/deleted its trigger, replaced a pending plan, or
		// performed lifecycle cleanup. Restarting it would undo that decision.
		// Durable server-side refusals use CancelTaskWithReason and may reconcile.
		return task.FailureReason.Valid
	default:
		return false
	}
}

func isDuplicateWorkflowReconcileErr(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		pgErr.ConstraintName == "idx_agent_task_workflow_reconcile_source_uidx"
}
