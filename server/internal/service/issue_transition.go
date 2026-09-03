package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuelifecycle"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var (
	// ErrIssueTransitionConflict means a caller attempted to transition from a
	// stale issue revision or lifecycle entry.
	ErrIssueTransitionConflict = errors.New("issue transition conflict")
	// ErrIssueTransitionStatusUnavailable means the target status is absent or
	// archived in the issue's workspace catalog.
	ErrIssueTransitionStatusUnavailable = errors.New("issue transition status unavailable")
	// ErrIssueEntryPolicyExecutorUnavailable leaves the issue at its previous
	// status rather than entering an automated node without the configured run.
	ErrIssueEntryPolicyExecutorUnavailable = errors.New("issue entry policy executor unavailable")
	ErrIssueEntryPolicyAssigneeUnavailable = errors.New("issue entry policy assignee unavailable")
)

type IssueTransitionParams struct {
	IssueID              pgtype.UUID
	WorkspaceID          pgtype.UUID
	Status               string
	Actor                issuelifecycle.TransitionActor
	Cause                string
	ExpectedRevision     pgtype.Int8
	ExpectedTransitionID pgtype.UUID
}

type IssueStatusNodeTransitionParams struct {
	IssueID              pgtype.UUID
	WorkspaceID          pgtype.UUID
	LifecycleStatusID    pgtype.UUID
	Actor                issuelifecycle.TransitionActor
	Cause                string
	ExpectedRevision     pgtype.Int8
	ExpectedTransitionID pgtype.UUID
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

// TransitionIssue is the canonical status-only write boundary. It serializes
// the issue row, applies optimistic preconditions, dual-writes the legacy key
// and lifecycle node, and records the immutable transition in one transaction.
func TransitionIssue(ctx context.Context, q *db.Queries, txStarter TxStarter, p IssueTransitionParams) (IssueTransitionResult, error) {
	if txStarter == nil {
		return IssueTransitionResult{}, errors.New("issue transition requires transaction starter")
	}
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return IssueTransitionResult{}, fmt.Errorf("begin issue transition: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)

	// Match the catalog archive protocol used by HTTP updates: catalog first,
	// then issue row, so archive and transition cannot deadlock.
	if !issuestatus.IsBuiltIn(p.Status) {
		if err := qtx.LockIssueStatusCatalogShared(ctx, p.WorkspaceID); err != nil {
			return IssueTransitionResult{}, fmt.Errorf("lock issue status catalog: %w", err)
		}
		if _, err := issuestatus.Resolve(ctx, qtx, p.WorkspaceID, p.Status); err != nil {
			if errors.Is(err, issuestatus.ErrUnknownStatus) {
				return IssueTransitionResult{}, ErrIssueTransitionStatusUnavailable
			}
			return IssueTransitionResult{}, err
		}
	}

	previous, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID: p.IssueID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if p.ExpectedRevision.Valid && previous.Revision != p.ExpectedRevision.Int64 {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}
	if p.ExpectedTransitionID.Valid && previous.LastTransitionID != p.ExpectedTransitionID {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}

	current, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: p.IssueID, WorkspaceID: p.WorkspaceID, Status: p.Status,
	})
	if err != nil {
		return IssueTransitionResult{}, err
	}
	current, transition, changed, err := issuelifecycle.RecordTransition(
		ctx, qtx, &previous, current, p.Actor, p.Cause,
	)
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IssueTransitionResult{}, fmt.Errorf("commit issue transition: %w", err)
	}
	return IssueTransitionResult{
		Previous: previous, Issue: current, Transition: transition, Changed: changed,
	}, nil
}

// TransitionIssueToStatusNode is the canonical lifecycle-native status write.
// The stable node ID, not the legacy status key, selects the destination. The
// legacy key is updated in the same transaction as a compatibility projection
// for installed clients and rolling rollback.
func TransitionIssueToStatusNode(ctx context.Context, q *db.Queries, txStarter TxStarter, p IssueStatusNodeTransitionParams) (IssueTransitionResult, error) {
	return transitionIssueToStatusNode(ctx, q, txStarter, nil, p)
}

func transitionIssueToStatusNode(ctx context.Context, q *db.Queries, txStarter TxStarter, taskService *TaskService, p IssueStatusNodeTransitionParams) (IssueTransitionResult, error) {
	if txStarter == nil {
		return IssueTransitionResult{}, errors.New("issue transition requires transaction starter")
	}
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return IssueTransitionResult{}, fmt.Errorf("begin issue transition: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)

	previous, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID: p.IssueID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if p.ExpectedRevision.Valid && previous.Revision != p.ExpectedRevision.Int64 {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}
	if p.ExpectedTransitionID.Valid && previous.LastTransitionID != p.ExpectedTransitionID {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}
	if !previous.LifecycleID.Valid {
		return IssueTransitionResult{}, ErrIssueTransitionConflict
	}
	target, err := qtx.GetIssueLifecycleStatusByID(ctx, db.GetIssueLifecycleStatusByIDParams{
		WorkspaceID: p.WorkspaceID,
		LifecycleID: previous.LifecycleID,
		ID:          p.LifecycleStatusID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IssueTransitionResult{}, ErrIssueTransitionStatusUnavailable
		}
		return IssueTransitionResult{}, err
	}
	if target.ArchivedAt.Valid || !target.LegacyStatusKey.Valid {
		return IssueTransitionResult{}, ErrIssueTransitionStatusUnavailable
	}
	if previous.LifecycleStatusID == target.ID {
		name := lifecycleStatusSnapshotName(target)
		return IssueTransitionResult{
			Previous: previous, Issue: previous,
			PreviousStatusName: name, StatusName: name,
		}, nil
	}
	var previousStatus db.IssueLifecycleStatus
	if previous.LifecycleStatusID.Valid {
		previousStatus, err = qtx.GetIssueLifecycleStatusByID(ctx, db.GetIssueLifecycleStatusByIDParams{
			WorkspaceID: p.WorkspaceID,
			LifecycleID: previous.LifecycleID,
			ID:          previous.LifecycleStatusID,
		})
		if err != nil {
			return IssueTransitionResult{}, fmt.Errorf("load previous lifecycle status: %w", err)
		}
	}
	policy, err := issuelifecycle.DecodeEntryPolicy(target.EntryPolicy)
	if err != nil {
		return IssueTransitionResult{}, fmt.Errorf("decode lifecycle entry policy: %w", err)
	}
	policySnapshot, policy, err := issuelifecycle.EncodeEntryPolicy(policy)
	if err != nil {
		return IssueTransitionResult{}, fmt.Errorf("normalize lifecycle entry policy: %w", err)
	}
	assigneeType, assigneeID, err := resolveEntryPolicyAssignee(ctx, qtx, previous, policy)
	if err != nil {
		return IssueTransitionResult{}, err
	}
	executor, err := resolveEntryPolicyExecutor(ctx, qtx, p.WorkspaceID, policy)
	if err != nil {
		return IssueTransitionResult{}, err
	}
	lifecycle, err := qtx.GetIssueLifecycleByID(ctx, db.GetIssueLifecycleByIDParams{
		ID: previous.LifecycleID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return IssueTransitionResult{}, fmt.Errorf("load issue lifecycle: %w", err)
	}

	current, err := qtx.UpdateIssueLifecycleStatusAndAssignee(ctx, db.UpdateIssueLifecycleStatusAndAssigneeParams{
		IssueID: p.IssueID, WorkspaceID: p.WorkspaceID, LifecycleStatusID: target.ID,
		AssigneeType: assigneeType, AssigneeID: assigneeID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IssueTransitionResult{}, ErrIssueTransitionStatusUnavailable
		}
		return IssueTransitionResult{}, err
	}
	current, transition, changed, err := issuelifecycle.RecordTransition(
		ctx, qtx, &previous, current, p.Actor, p.Cause,
	)
	if err != nil {
		return IssueTransitionResult{}, err
	}
	if !changed {
		return IssueTransitionResult{
			Previous: previous, Issue: current,
			PreviousStatusName: lifecycleStatusSnapshotName(previousStatus),
			StatusName:         lifecycleStatusSnapshotName(target),
		}, nil
	}

	if _, err := qtx.SupersedeIssueAutomationExecutions(ctx, db.SupersedeIssueAutomationExecutionsParams{
		IssueID: current.ID, WorkspaceID: p.WorkspaceID,
	}); err != nil {
		return IssueTransitionResult{}, fmt.Errorf("supersede previous automation executions: %w", err)
	}
	cancelledTasks, err := qtx.CancelTasksForSupersededAutomationExecutions(ctx, db.CancelTasksForSupersededAutomationExecutionsParams{
		IssueID: current.ID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return IssueTransitionResult{}, fmt.Errorf("cancel superseded automation tasks: %w", err)
	}
	executionStatus := "dormant"
	if executor.agent.ID.Valid {
		executionStatus = "pending"
	}
	execution, err := qtx.CreateAutomationExecution(ctx, db.CreateAutomationExecutionParams{
		ID: dbid.NewV7(), WorkspaceID: p.WorkspaceID, IssueID: current.ID,
		TriggerTransitionID: transition.ID, LifecycleID: lifecycle.ID,
		LifecycleRevision: lifecycle.Revision, StatusID: target.ID,
		PolicyRevision: target.EntryPolicyRevision, PolicySnapshot: policySnapshot,
		ExecutorType: executor.executorType, ExecutorID: executor.executorID,
		Status: executionStatus,
	})
	if err != nil {
		return IssueTransitionResult{}, fmt.Errorf("create automation execution: %w", err)
	}

	var task db.AgentTaskQueue
	if executor.agent.ID.Valid {
		task, err = createLifecycleEntryTask(ctx, qtx, current, transition, execution, executor, policy, p.Actor)
		if err != nil {
			return IssueTransitionResult{}, fmt.Errorf("create lifecycle entry task: %w", err)
		}
		execution, err = qtx.GetAutomationExecution(ctx, db.GetAutomationExecutionParams{
			ID: execution.ID, WorkspaceID: p.WorkspaceID,
		})
		if err != nil {
			return IssueTransitionResult{}, fmt.Errorf("reload automation execution: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return IssueTransitionResult{}, fmt.Errorf("commit issue transition: %w", err)
	}
	if taskService != nil {
		if len(cancelledTasks) > 0 {
			taskService.BroadcastCancelledTasks(ctx, util.UUIDToString(p.WorkspaceID), cancelledTasks)
		}
		if task.ID.Valid {
			taskService.BroadcastTaskQueued(ctx, task)
			taskService.NotifyTaskEnqueued(ctx, task)
		}
	}
	return IssueTransitionResult{
		Previous: previous, Issue: current, Transition: transition, Execution: execution,
		PreviousStatusName: lifecycleStatusSnapshotName(previousStatus),
		StatusName:         lifecycleStatusSnapshotName(target),
		Task:               task, CancelledTasks: cancelledTasks, Changed: changed,
	}, nil
}

// lifecycleStatusSnapshotName keeps user-authored lifecycle names in history
// while leaving untouched built-ins blank so clients can continue localizing
// those canonical labels from their stable legacy keys.
func lifecycleStatusSnapshotName(status db.IssueLifecycleStatus) string {
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

func createLifecycleEntryTask(
	ctx context.Context,
	q *db.Queries,
	issue db.Issue,
	transition db.IssueTransition,
	execution db.AutomationExecution,
	executor resolvedEntryExecutor,
	policy issuelifecycle.EntryPolicy,
	actor issuelifecycle.TransitionActor,
) (db.AgentTaskQueue, error) {
	originatorID := executor.agent.OwnerID
	originatorSource := "owner_fallback"
	if actor.Type == "member" && actor.ID.Valid {
		originatorID = actor.ID
		originatorSource = "direct_human"
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
		TriggerEvidenceKind:  pgtype.Text{String: "issue_transition", Valid: true},
		TriggerEvidenceRefID: transition.ID, AutomationExecutionID: execution.ID,
	})
}

// enterInitialLifecycleStatus applies the first node's policy after
// RecordTransition has pinned a new issue. The caller keeps this inside the
// create transaction, so assignment, execution, and run are atomic.
func enterInitialLifecycleStatus(ctx context.Context, q *db.Queries, issue db.Issue, transition db.IssueTransition, actor issuelifecycle.TransitionActor) (db.Issue, db.AutomationExecution, db.AgentTaskQueue, error) {
	target, err := q.GetIssueLifecycleStatusByID(ctx, db.GetIssueLifecycleStatusByIDParams{
		WorkspaceID: issue.WorkspaceID, LifecycleID: issue.LifecycleID, ID: issue.LifecycleStatusID,
	})
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("load initial lifecycle status: %w", err)
	}
	policy, err := issuelifecycle.DecodeEntryPolicy(target.EntryPolicy)
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("decode initial entry policy: %w", err)
	}
	policySnapshot, policy, err := issuelifecycle.EncodeEntryPolicy(policy)
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, err
	}
	assigneeType, assigneeID, err := resolveEntryPolicyAssignee(ctx, q, issue, policy)
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, err
	}
	executor, err := resolveEntryPolicyExecutor(ctx, q, issue.WorkspaceID, policy)
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, err
	}
	if issue.AssigneeType != assigneeType || issue.AssigneeID != assigneeID {
		issue, err = q.UpdateIssueAssigneeFromEntryPolicy(ctx, db.UpdateIssueAssigneeFromEntryPolicyParams{
			AssigneeType: assigneeType, AssigneeID: assigneeID,
			IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("apply initial entry assignee: %w", err)
		}
	}
	lifecycle, err := q.GetIssueLifecycleByID(ctx, db.GetIssueLifecycleByIDParams{
		ID: issue.LifecycleID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("load initial lifecycle: %w", err)
	}
	executionStatus := "dormant"
	if executor.agent.ID.Valid {
		executionStatus = "pending"
	}
	execution, err := q.CreateAutomationExecution(ctx, db.CreateAutomationExecutionParams{
		ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
		TriggerTransitionID: transition.ID, LifecycleID: lifecycle.ID,
		LifecycleRevision: lifecycle.Revision, StatusID: target.ID,
		PolicyRevision: target.EntryPolicyRevision, PolicySnapshot: policySnapshot,
		ExecutorType: executor.executorType, ExecutorID: executor.executorID, Status: executionStatus,
	})
	if err != nil {
		return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("create initial automation execution: %w", err)
	}
	var task db.AgentTaskQueue
	if executor.agent.ID.Valid {
		task, err = createLifecycleEntryTask(ctx, q, issue, transition, execution, executor, policy, actor)
		if err != nil {
			return db.Issue{}, db.AutomationExecution{}, db.AgentTaskQueue{}, fmt.Errorf("create initial lifecycle task: %w", err)
		}
	}
	return issue, execution, task, nil
}

func resolveEntryPolicyAssignee(ctx context.Context, q *db.Queries, issue db.Issue, policy issuelifecycle.EntryPolicy) (pgtype.Text, pgtype.UUID, error) {
	if policy.Assignee.Type == issuelifecycle.AssigneeKeep {
		return issue.AssigneeType, issue.AssigneeID, nil
	}
	id, err := util.ParseUUID(policy.Assignee.ID)
	if err != nil {
		return pgtype.Text{}, pgtype.UUID{}, fmt.Errorf("invalid entry policy assignee: %w", err)
	}
	typ := policy.Assignee.Type
	if typ == issuelifecycle.AssigneeHuman {
		typ = "member"
	}
	switch typ {
	case "member":
		if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: id, WorkspaceID: issue.WorkspaceID}); err != nil {
			return pgtype.Text{}, pgtype.UUID{}, fmt.Errorf("%w: human is no longer a workspace member", ErrIssueEntryPolicyAssigneeUnavailable)
		}
	case "agent":
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: issue.WorkspaceID})
		if err != nil || agent.ArchivedAt.Valid {
			return pgtype.Text{}, pgtype.UUID{}, fmt.Errorf("%w: agent is missing or archived", ErrIssueEntryPolicyAssigneeUnavailable)
		}
	case "squad":
		squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: id, WorkspaceID: issue.WorkspaceID})
		if err != nil || squad.ArchivedAt.Valid {
			return pgtype.Text{}, pgtype.UUID{}, fmt.Errorf("%w: squad is missing or archived", ErrIssueEntryPolicyAssigneeUnavailable)
		}
	}
	return pgtype.Text{String: typ, Valid: true}, id, nil
}

type resolvedEntryExecutor struct {
	executorType pgtype.Text
	executorID   pgtype.UUID
	agent        db.Agent
	squadID      pgtype.UUID
}

func resolveEntryPolicyExecutor(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, policy issuelifecycle.EntryPolicy) (resolvedEntryExecutor, error) {
	if policy.Executor.Type == issuelifecycle.ExecutorNone {
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
	issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: p.IssueID, WorkspaceID: p.WorkspaceID})
	if err == nil && issue.LifecycleID.Valid {
		target, targetErr := s.Queries.GetIssueLifecycleStatusByLegacyKey(ctx, db.GetIssueLifecycleStatusByLegacyKeyParams{
			WorkspaceID: p.WorkspaceID, LifecycleID: issue.LifecycleID,
			LegacyStatusKey: pgtype.Text{String: p.Status, Valid: true},
		})
		if targetErr == nil {
			return transitionIssueToStatusNode(ctx, s.Queries, s.TxStarter, s.TaskService, IssueStatusNodeTransitionParams{
				IssueID: p.IssueID, WorkspaceID: p.WorkspaceID, LifecycleStatusID: target.ID,
				Actor: p.Actor, Cause: p.Cause, ExpectedRevision: p.ExpectedRevision,
				ExpectedTransitionID: p.ExpectedTransitionID,
			})
		}
		if !errors.Is(targetErr, pgx.ErrNoRows) {
			return IssueTransitionResult{}, targetErr
		}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return IssueTransitionResult{}, err
	}
	return TransitionIssue(ctx, s.Queries, s.TxStarter, p)
}

func (s *IssueService) TransitionStatusNode(ctx context.Context, p IssueStatusNodeTransitionParams) (IssueTransitionResult, error) {
	return transitionIssueToStatusNode(ctx, s.Queries, s.TxStarter, s.TaskService, p)
}

// TakeOverAutomationExecution atomically stops the active lifecycle run and
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
	if err != nil || execution.IssueID != issue.ID || execution.StatusID != issue.LifecycleStatusID {
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
	issue, err = qtx.UpdateIssueAssigneeFromEntryPolicy(ctx, db.UpdateIssueAssigneeFromEntryPolicyParams{
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
	return TransitionIssue(ctx, s.Queries, s.TxStarter, p)
}
