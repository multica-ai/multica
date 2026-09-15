package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var (
	// ErrIssueTransitionConflict means a caller attempted to transition from a
	// stale issue revision or workflow entry.
	ErrIssueTransitionConflict = errors.New("issue transition conflict")
	// ErrIssueTransitionStatusUnavailable means the target status is absent or
	// archived in the issue's workspace catalog.
	ErrIssueTransitionStatusUnavailable = errors.New("issue transition status unavailable")
	// ErrIssueEntryPolicyExecutorUnavailable leaves the issue at its previous
	// status rather than entering an automated node without the configured run.
	ErrIssueEntryPolicyExecutorUnavailable = errors.New("issue entry policy executor unavailable")
)

type IssueTransitionParams struct {
	IssueID              pgtype.UUID
	WorkspaceID          pgtype.UUID
	Status               string
	Actor                issueworkflow.TransitionActor
	Cause                string
	ExpectedRevision     pgtype.Int8
	ExpectedTransitionID pgtype.UUID
}

type IssueStatusNodeTransitionParams struct {
	ExpectedWorkflowRevision pgtype.Int8
	IssueID                  pgtype.UUID
	WorkspaceID              pgtype.UUID
	WorkflowStatusID         pgtype.UUID
	Actor                    issueworkflow.TransitionActor
	Cause                    string
	ExpectedRevision         pgtype.Int8
	ExpectedTransitionID     pgtype.UUID
}

type IssueTransitionResult struct {
	Previous           db.Issue
	Issue              db.Issue
	PreviousStatusName string
	StatusName         string
	Transition         db.IssueTransition
	Execution          db.AutomationExecution
	Task               db.AgentTaskQueue
	CancelledTasks     []db.AgentTaskQueue
	Changed            bool
}

type IssueAutomationTakeoverParams struct {
	IssueID          pgtype.UUID
	WorkspaceID      pgtype.UUID
	ExecutionID      pgtype.UUID
	MemberID         pgtype.UUID
	ExpectedRevision pgtype.Int8
}

type IssueAutomationTakeoverResult struct {
	Issue          db.Issue
	Execution      db.AutomationExecution
	CancelledTasks []db.AgentTaskQueue
}

// ResolveIssueWorkflowStatus resolves legacy input within the issue's pinned
// workflow. Call inside the write transaction; the active target is locked.
func ResolveIssueWorkflowStatus(ctx context.Context, q *db.Queries, issue db.Issue, key string) (db.IssueWorkflowStatus, error) {
	workflowID := issue.WorkflowID
	if !workflowID.Valid {
		if err := q.SeedIssueStatusEntries(ctx, issue.WorkspaceID); err != nil {
			return db.IssueWorkflowStatus{}, err
		}
		if _, err := issueworkflow.EnsureDefault(ctx, q, issue.WorkspaceID); err != nil {
			return db.IssueWorkflowStatus{}, err
		}
		workflow, err := issueworkflow.Effective(ctx, q, issue.WorkspaceID, issue.ProjectID)
		if err != nil {
			return db.IssueWorkflowStatus{}, err
		}
		workflowID = workflow.ID
	}
	statusID := issue.WorkflowStatusID
	if !statusID.Valid || issue.Status != key {
		status, err := q.GetIssueWorkflowStatusByLegacyKey(ctx, db.GetIssueWorkflowStatusByLegacyKeyParams{
			WorkspaceID: issue.WorkspaceID, WorkflowID: workflowID, LegacyStatusKey: pgtype.Text{String: key, Valid: true},
		})
		if errors.Is(err, pgx.ErrNoRows) || err == nil && status.ArchivedAt.Valid {
			workflow, workflowErr := q.GetIssueWorkflowByID(ctx, db.GetIssueWorkflowByIDParams{ID: workflowID, WorkspaceID: issue.WorkspaceID})
			if workflowErr != nil {
				return db.IssueWorkflowStatus{}, workflowErr
			}
			// Use the same catalog projection repair as issue creation. Project
			// workflows never import a missing node from the workspace catalog.
			if workflow.ScopeType == "workspace" {
				if syncErr := issueworkflow.SyncDefault(ctx, q, issue.WorkspaceID); syncErr != nil {
					return db.IssueWorkflowStatus{}, syncErr
				}
				status, err = q.GetIssueWorkflowStatusByLegacyKey(ctx, db.GetIssueWorkflowStatusByLegacyKeyParams{
					WorkspaceID: issue.WorkspaceID, WorkflowID: workflowID, LegacyStatusKey: pgtype.Text{String: key, Valid: true},
				})
			}
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return db.IssueWorkflowStatus{}, ErrIssueTransitionStatusUnavailable
		}
		if err != nil {
			return db.IssueWorkflowStatus{}, err
		}
		statusID = status.ID
	}
	target, err := q.LockActiveIssueWorkflowStatus(ctx, db.LockActiveIssueWorkflowStatusParams{
		WorkspaceID: issue.WorkspaceID, WorkflowID: workflowID, ID: statusID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.IssueWorkflowStatus{}, ErrIssueTransitionStatusUnavailable
	}
	return target, err
}

// TransitionIssue accepts installed clients' legacy keys through the same
// transaction and entry policy as native status-node transitions.
func TransitionIssue(ctx context.Context, q *db.Queries, txStarter TxStarter, p IssueTransitionParams) (IssueTransitionResult, error) {
	return transitionIssueToStatusNode(ctx, q, txStarter, nil, IssueStatusNodeTransitionParams{
		IssueID: p.IssueID, WorkspaceID: p.WorkspaceID, Actor: p.Actor, Cause: p.Cause,
		ExpectedRevision: p.ExpectedRevision, ExpectedTransitionID: p.ExpectedTransitionID,
	}, p.Status)
}

func TransitionIssueToStatusNode(ctx context.Context, q *db.Queries, txStarter TxStarter, p IssueStatusNodeTransitionParams) (IssueTransitionResult, error) {
	return transitionIssueToStatusNode(ctx, q, txStarter, nil, p, "")
}

func transitionIssueToStatusNode(ctx context.Context, q *db.Queries, txStarter TxStarter, taskService *TaskService, p IssueStatusNodeTransitionParams, legacyStatus string) (IssueTransitionResult, error) {
	if txStarter == nil {
		return IssueTransitionResult{}, errors.New("issue transition requires transaction starter")
	}
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return IssueTransitionResult{}, fmt.Errorf("begin issue transition: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)
	if legacyStatus != "" && !issuestatus.IsBuiltIn(legacyStatus) {
		if err := qtx.LockIssueStatusCatalogShared(ctx, p.WorkspaceID); err != nil {
			return IssueTransitionResult{}, err
		}
	}
	previous, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{ID: p.IssueID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if p.ExpectedRevision.Valid && previous.Revision != p.ExpectedRevision.Int64 ||
		p.ExpectedTransitionID.Valid && previous.LastTransitionID != p.ExpectedTransitionID {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}
	var target db.IssueWorkflowStatus
	if legacyStatus != "" {
		target, err = ResolveIssueWorkflowStatus(ctx, qtx, previous, legacyStatus)
	} else {
		target, err = qtx.LockActiveIssueWorkflowStatus(ctx, db.LockActiveIssueWorkflowStatusParams{
			WorkspaceID: p.WorkspaceID, WorkflowID: previous.WorkflowID, ID: p.WorkflowStatusID,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueTransitionResult{}, ErrIssueTransitionStatusUnavailable
	}
	if err != nil {
		return IssueTransitionResult{}, err
	}
	workflow, err := qtx.GetIssueWorkflowByID(ctx, db.GetIssueWorkflowByIDParams{ID: target.WorkflowID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if p.ExpectedWorkflowRevision.Valid && workflow.Revision != p.ExpectedWorkflowRevision.Int64 {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}
	if previous.WorkflowStatusID == target.ID {
		name := workflowStatusSnapshotName(target)
		return IssueTransitionResult{Previous: previous, Issue: previous, PreviousStatusName: name, StatusName: name}, nil
	}
	if !previous.WorkflowID.Valid {
		if _, err := qtx.BindIssueToWorkflowStatus(ctx, db.BindIssueToWorkflowStatusParams{
			IssueID: previous.ID, WorkspaceID: previous.WorkspaceID, WorkflowID: target.WorkflowID, WorkflowStatusID: target.ID,
		}); err != nil {
			return IssueTransitionResult{}, err
		}
	}
	var current db.Issue
	if legacyStatus != "" {
		// Preserve legacy column-position semantics for system and installed
		// client writes while sharing all status-entry effects below.
		current, err = qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID: p.IssueID, WorkspaceID: p.WorkspaceID, Status: legacyStatus,
		})
	} else {
		current, err = qtx.UpdateIssueWorkflowStatus(ctx, db.UpdateIssueWorkflowStatusParams{
			IssueID: p.IssueID, WorkspaceID: p.WorkspaceID, WorkflowStatusID: target.ID,
		})
	}
	if err != nil {
		return IssueTransitionResult{}, err
	}
	result, err := EnterIssueWorkflowStatus(ctx, qtx, &previous, current, p.Actor, p.Cause)
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IssueTransitionResult{}, fmt.Errorf("commit issue transition: %w", err)
	}
	if taskService != nil {
		taskService.NotifyWorkflowEntry(ctx, result)
	}
	return result, nil
}

// EnterIssueWorkflowStatus records a real status entry and its effects together.
// q must use the caller's transaction, with the issue locked (or just inserted).
// Creation has no previous issue. Project moves count as a fresh entry even if
// the destination shares the same status. Call NotifyWorkflowEntry after commit.
func EnterIssueWorkflowStatus(ctx context.Context, q *db.Queries, previous *db.Issue, current db.Issue, actor issueworkflow.TransitionActor, cause string) (IssueTransitionResult, error) {
	result := IssueTransitionResult{Issue: current}
	if previous != nil {
		result.Previous = *previous
		if previous.Status == current.Status && previous.ProjectID == current.ProjectID &&
			previous.WorkflowID == current.WorkflowID && previous.WorkflowStatusID == current.WorkflowStatusID {
			return result, nil
		}
		if err := AssertIssueWorkflowWriteAllowed(ctx, q, *previous, actor); err != nil {
			return result, err
		}
	}
	if !current.WorkflowID.Valid || !current.WorkflowStatusID.Valid {
		target, err := ResolveIssueWorkflowStatus(ctx, q, current, current.Status)
		if err != nil {
			return result, err
		}
		current, err = q.BindIssueToWorkflowStatus(ctx, db.BindIssueToWorkflowStatusParams{
			IssueID: current.ID, WorkspaceID: current.WorkspaceID, WorkflowID: target.WorkflowID, WorkflowStatusID: target.ID,
		})
		if err != nil {
			return result, err
		}
	}
	target, err := q.LockActiveIssueWorkflowStatus(ctx, db.LockActiveIssueWorkflowStatusParams{
		WorkspaceID: current.WorkspaceID, WorkflowID: current.WorkflowID, ID: current.WorkflowStatusID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrIssueTransitionStatusUnavailable
	}
	if err != nil {
		return result, err
	}
	result.StatusName = workflowStatusSnapshotName(target)
	if previous != nil && previous.WorkflowStatusID.Valid {
		from, err := q.GetIssueWorkflowStatusByID(ctx, db.GetIssueWorkflowStatusByIDParams{
			WorkspaceID: previous.WorkspaceID, WorkflowID: previous.WorkflowID, ID: previous.WorkflowStatusID,
		})
		if err != nil {
			return result, err
		}
		result.PreviousStatusName = workflowStatusSnapshotName(from)
	}
	current, transition, changed, err := issueworkflow.RecordTransition(ctx, q, previous, current, actor, cause)
	if err != nil {
		return result, err
	}
	result.Issue, result.Transition, result.Changed = current, transition, changed
	if !changed {
		return result, nil
	}
	if previous != nil {
		result.CancelledTasks, err = supersedePreviousWorkflowEntry(ctx, q, *previous, current, actor)
		if err != nil {
			return result, err
		}
	}
	// A Triage status is a proposal, not accepted work. Keep its transition
	// history without starting the proposed node's executor.
	if current.TriageState.Valid {
		return result, nil
	}
	result.Issue, result.Execution, result.Task, err = applyWorkflowEntryPolicy(ctx, q, current, transition, actor)
	return result, err
}

// NotifyWorkflowEntry publishes only committed execution changes.
func (s *TaskService) NotifyWorkflowEntry(ctx context.Context, entry IssueTransitionResult) {
	if len(entry.CancelledTasks) > 0 {
		s.BroadcastCancelledTasks(ctx, util.UUIDToString(entry.Issue.WorkspaceID), entry.CancelledTasks)
	}
	if entry.Task.ID.Valid {
		s.BroadcastTaskQueued(ctx, entry.Task)
		s.NotifyTaskEnqueued(ctx, entry.Task)
	}
}

func supersedePreviousWorkflowEntry(ctx context.Context, q *db.Queries, previous, current db.Issue, actor issueworkflow.TransitionActor) ([]db.AgentTaskQueue, error) {
	if _, err := q.SupersedeIssueAutomationExecutions(ctx, db.SupersedeIssueAutomationExecutionsParams{
		IssueID: current.ID, WorkspaceID: current.WorkspaceID,
		TriggerTransitionID: previous.LastTransitionID,
		ActorTaskID:         actor.TaskID, ActorAgentID: actor.ID,
	}); err != nil {
		return nil, fmt.Errorf("supersede previous automation executions: %w", err)
	}
	tasks, err := q.CancelTasksForSupersededAutomationExecutions(ctx, db.CancelTasksForSupersededAutomationExecutionsParams{
		IssueID: current.ID, WorkspaceID: current.WorkspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("cancel superseded automation tasks: %w", err)
	}
	return tasks, nil
}

// workflowStatusSnapshotName keeps user-authored workflow names in history
// while leaving untouched built-ins blank so clients can continue localizing
// those canonical labels from their stable legacy keys.
func workflowStatusSnapshotName(status db.IssueWorkflowStatus) string {
	if !status.LegacyStatusKey.Valid {
		return status.Name
	}
	canonicalNames := map[string]string{
		"backlog": "Backlog", "todo": "Todo", "in_progress": "In Progress",
		"in_review": "In Review", "done": "Done", "blocked": "Blocked", "cancelled": "Cancelled",
	}
	if status.Name == canonicalNames[status.LegacyStatusKey.String] {
		return ""
	}
	return status.Name
}

func createWorkflowEntryTask(
	ctx context.Context,
	q *db.Queries,
	issue db.Issue,
	transition db.IssueTransition,
	execution db.AutomationExecution,
	executor resolvedEntryExecutor,
	policy issueworkflow.EntryPolicy,
	actor issueworkflow.TransitionActor,
) (db.AgentTaskQueue, error) {
	originatorID := executor.agent.OwnerID
	originatorSource := "owner_fallback"
	var delegatedFromTaskID pgtype.UUID
	if actor.Type == "member" && actor.ID.Valid {
		originatorID = actor.ID
		originatorSource = "direct_human"
	} else if actor.Type == "agent" && actor.TaskID.Valid {
		// A status handoff continues the current run's human authorization.
		// Manual autopilot runs have no trigger whose owner can be resolved.
		parent, err := q.GetAgentTask(ctx, actor.TaskID)
		if err != nil {
			return db.AgentTaskQueue{}, err
		}
		if parent.AgentID != actor.ID || parent.IssueID != issue.ID || !parent.OriginatorUserID.Valid {
			return db.AgentTaskQueue{}, ErrAttributionFailClosed
		}
		originatorID = parent.OriginatorUserID
		originatorSource = "delegation"
		delegatedFromTaskID = parent.ID
	} else if issue.OriginType.String == "autopilot" && issue.OriginID.Valid {
		// Autopilot entry must carry the same principal as ordinary autopilot
		// dispatch. Its run is linked inside the creation transaction first.
		run, err := q.GetAutopilotRunByIssue(ctx, issue.ID)
		if err != nil {
			return db.AgentTaskQueue{}, err
		}
		originatorID = ResolveAutopilotTriggerPrincipal(ctx, q, run.TriggerID, issue.OriginID, issue.WorkspaceID)
		if !originatorID.Valid {
			return db.AgentTaskQueue{}, errors.New("workflow entry has no autopilot trigger principal")
		}
		originatorSource = "trigger_owner"
	}
	return q.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		ID: dbid.NewV7(), AgentID: executor.agent.ID, RuntimeID: executor.agent.RuntimeID,
		IssueID: issue.ID, Priority: priorityToInt(issue.Priority),
		IsLeaderTask: pgtype.Bool{Bool: executor.squadID.Valid, Valid: executor.squadID.Valid},
		HandoffNote: pgtype.Text{
			String: strings.TrimSpace(policy.Instructions), Valid: strings.TrimSpace(policy.Instructions) != "",
		},
		SquadID: executor.squadID, OriginatorUserID: originatorID, AccountableUserID: originatorID,
		OriginatorSource:     pgtype.Text{String: originatorSource, Valid: true},
		DelegatedFromTaskID:  delegatedFromTaskID,
		TriggerEvidenceKind:  pgtype.Text{String: "issue_transition", Valid: true},
		TriggerEvidenceRefID: transition.ID, AutomationExecutionID: execution.ID,
	})
}

// applyWorkflowEntryPolicy snapshots the action and queues its task in the entry transaction.
func applyWorkflowEntryPolicy(ctx context.Context, q *db.Queries, issue db.Issue, transition db.IssueTransition, actor issueworkflow.TransitionActor) (db.Issue, db.AutomationExecution, db.AgentTaskQueue, error) {
	target, err := q.GetIssueWorkflowStatusByID(ctx, db.GetIssueWorkflowStatusByIDParams{
		WorkspaceID: issue.WorkspaceID, WorkflowID: issue.WorkflowID, ID: issue.WorkflowStatusID,
	})
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("load workflow status: %w", err)
	}
	policy, err := issueworkflow.DecodeEntryPolicy(target.EntryPolicy)
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("decode entry policy: %w", err)
	}
	policySnapshot, policy, err := issueworkflow.EncodeEntryPolicy(policy)
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, err
	}
	executor, err := resolveEntryPolicyExecutor(ctx, q, issue.WorkspaceID, policy)
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, err
	}
	workflow, err := q.GetIssueWorkflowByID(ctx, db.GetIssueWorkflowByIDParams{
		ID: issue.WorkflowID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("load workflow: %w", err)
	}
	executionStatus := "dormant"
	if executor.agent.ID.Valid {
		executionStatus = "pending"
	}
	execution, err := q.CreateAutomationExecution(ctx, db.CreateAutomationExecutionParams{
		ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
		TriggerTransitionID: transition.ID, WorkflowID: workflow.ID,
		WorkflowRevision: workflow.Revision, StatusID: target.ID,
		PolicyRevision: target.EntryPolicyRevision, PolicySnapshot: policySnapshot,
		ExecutorType: executor.executorType, ExecutorID: executor.executorID, Status: executionStatus,
	})
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("create automation execution: %w", err)
	}
	var task db.AgentTaskQueue
	if executor.agent.ID.Valid {
		task, err = createWorkflowEntryTask(ctx, q, issue, transition, execution, executor, policy, actor)
		if err != nil {
			return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("create workflow task: %w", err)
		}
		execution, err = q.GetAutomationExecution(ctx, db.GetAutomationExecutionParams{ID: execution.ID, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, err
		}
	}
	return issue, execution, task, nil
}

type resolvedEntryExecutor struct {
	executorType pgtype.Text
	executorID   pgtype.UUID
	agent        db.Agent
	squadID      pgtype.UUID
}

func resolveEntryPolicyExecutor(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, policy issueworkflow.EntryPolicy) (resolvedEntryExecutor, error) {
	if policy.Executor.Type == issueworkflow.ExecutorNone {
		return resolvedEntryExecutor{}, nil
	}
	executorID, err := util.ParseUUID(policy.Executor.ID)
	if err != nil {
		return resolvedEntryExecutor{}, fmt.Errorf("%w: invalid executor id", ErrIssueEntryPolicyExecutorUnavailable)
	}
	resolved := resolvedEntryExecutor{
		executorType: pgtype.Text{String: policy.Executor.Type, Valid: true}, executorID: executorID,
	}
	agentID := executorID
	if policy.Executor.Type == "squad" {
		squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: executorID, WorkspaceID: workspaceID})
		if err != nil || squad.ArchivedAt.Valid {
			return resolvedEntryExecutor{}, fmt.Errorf("%w: squad is missing or archived", ErrIssueEntryPolicyExecutorUnavailable)
		}
		resolved.squadID = squad.ID
		agentID = squad.LeaderID
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceID})
	if err != nil || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		return resolvedEntryExecutor{}, fmt.Errorf("%w: agent is missing, archived, or has no runtime", ErrIssueEntryPolicyExecutorUnavailable)
	}
	resolved.agent = agent
	return resolved, nil
}

func (s *IssueService) TransitionStatus(ctx context.Context, p IssueTransitionParams) (IssueTransitionResult, error) {
	return transitionIssueToStatusNode(ctx, s.Queries, s.TxStarter, s.TaskService, IssueStatusNodeTransitionParams{
		IssueID: p.IssueID, WorkspaceID: p.WorkspaceID, Actor: p.Actor, Cause: p.Cause,
		ExpectedRevision: p.ExpectedRevision, ExpectedTransitionID: p.ExpectedTransitionID,
	}, p.Status)
}

func (s *IssueService) TransitionStatusNode(ctx context.Context, p IssueStatusNodeTransitionParams) (IssueTransitionResult, error) {
	return transitionIssueToStatusNode(ctx, s.Queries, s.TxStarter, s.TaskService, p, "")
}

// TakeOverAutomationExecution atomically stops the active workflow run and
// assigns the issue to the requesting human without guessing a next status.
func (s *IssueService) TakeOverAutomationExecution(ctx context.Context, p IssueAutomationTakeoverParams) (IssueAutomationTakeoverResult, error) {
	if s.TxStarter == nil {
		return IssueAutomationTakeoverResult{}, errors.New("automation takeover requires transaction starter")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return IssueAutomationTakeoverResult{}, err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	issue, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID: p.IssueID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return IssueAutomationTakeoverResult{}, err
	}
	if p.ExpectedRevision.Valid && issue.Revision != p.ExpectedRevision.Int64 {
		return IssueAutomationTakeoverResult{}, ErrIssueTransitionConflict
	}
	execution, err := qtx.GetAutomationExecution(ctx, db.GetAutomationExecutionParams{
		ID: p.ExecutionID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil || execution.IssueID != issue.ID || execution.StatusID != issue.WorkflowStatusID {
		return IssueAutomationTakeoverResult{}, ErrIssueTransitionConflict
	}
	execution, err = qtx.SupersedeAutomationExecution(ctx, db.SupersedeAutomationExecutionParams{
		ID: execution.ID, IssueID: issue.ID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IssueAutomationTakeoverResult{}, ErrIssueTransitionConflict
		}
		return IssueAutomationTakeoverResult{}, err
	}
	issue, err = qtx.UpdateIssueAssigneeForTakeover(ctx, db.UpdateIssueAssigneeForTakeoverParams{
		AssigneeType: pgtype.Text{String: "member", Valid: true}, AssigneeID: p.MemberID,
		IssueID: issue.ID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return IssueAutomationTakeoverResult{}, err
	}
	cancelledTasks, err := qtx.CancelTasksForSupersededAutomationExecutions(ctx, db.CancelTasksForSupersededAutomationExecutionsParams{
		IssueID: issue.ID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return IssueAutomationTakeoverResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IssueAutomationTakeoverResult{}, err
	}
	if s.TaskService != nil && len(cancelledTasks) > 0 {
		s.TaskService.BroadcastCancelledTasks(ctx, util.UUIDToString(p.WorkspaceID), cancelledTasks)
	}
	return IssueAutomationTakeoverResult{Issue: issue, Execution: execution, CancelledTasks: cancelledTasks}, nil
}

func (s *TaskService) transitionIssueStatus(ctx context.Context, p IssueTransitionParams) (IssueTransitionResult, error) {
	return transitionIssueToStatusNode(ctx, s.Queries, s.TxStarter, s, IssueStatusNodeTransitionParams{
		IssueID: p.IssueID, WorkspaceID: p.WorkspaceID, Actor: p.Actor, Cause: p.Cause,
		ExpectedRevision: p.ExpectedRevision, ExpectedTransitionID: p.ExpectedTransitionID,
	}, p.Status)
}
