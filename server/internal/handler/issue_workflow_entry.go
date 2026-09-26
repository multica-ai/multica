package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Project workflows on the issue write paths (MUL-7420).
//
// Two rules apply to an issue whose project uses a workflow:
//   - its status must be one the workflow lists; a move into such a project
//     keeps an equivalent status instead of failing;
//   - entering a step with a handler, through an ordinary status change of an
//     issue that stays in its project, assigns the issue to that handler and
//     starts its run with the step's instructions. A write that sets the
//     assignee itself keeps its explicit choice.
//
// Handoffs reuse assignment wholesale: the resolved handler goes through the
// same permission gate as a hand-picked assignee, and the run starts through
// the same WillEnqueueRun predicate, with the step's brief as its handoff note.

// workflowHandoff is a handoff an issue write triggers.
type workflowHandoff struct {
	def     issueworkflow.Definition
	step    issueworkflow.Step
	project db.Project
}

// applyIssueWorkflow enforces prev's (possibly new) project workflow on one
// prospective update and may rewrite params: the status of an issue moving
// into the project, and the assignee when the write enters a handoff step.
// run is the calling agent's run on this issue (nil for members): it may only
// move the issue from the step it works on. It returns the handoff that write
// triggers, or nil.
func (h *Handler) applyIssueWorkflow(ctx context.Context, prev db.Issue, params *db.UpdateIssueParams, statusExplicit, assigneeTouched bool, run *db.AgentTaskQueue) (*workflowHandoff, error) {
	def, project, err := issueworkflow.ForProject(ctx, h.Queries, prev.WorkspaceID, params.ProjectID)
	if err != nil || def == nil {
		return nil, err
	}
	projectMoving := params.ProjectID != prev.ProjectID
	if projectMoving && !statusExplicit && !def.Has(prev.Status) {
		target := issueworkflow.MoveTarget(*def, prev.Status, func(key string) string {
			return issuestatus.Category(ctx, h.Queries, prev.WorkspaceID, key)
		})
		params.Status = pgtype.Text{String: target, Valid: true}
	}
	if params.Status.Valid {
		if err := issueworkflow.CheckStatus(def, params.Status.String); err != nil {
			return nil, err
		}
	}
	// Someone else moved the issue while the run worked: the run's decision was
	// made for a step the issue has left, and applying it would undo that move.
	if run != nil && run.WorkflowStep.Valid && run.WorkflowStep.String != prev.Status &&
		statusExplicit && params.Status.Valid && params.Status.String != prev.Status {
		return nil, h.stepMovedError(ctx, prev, run.WorkflowStep.String)
	}
	// A project move is administrative, and re-selecting the current status
	// is a no-op: neither hands the issue off.
	if projectMoving || !statusExplicit || assigneeTouched || prev.TriageState.Valid ||
		!params.Status.Valid || params.Status.String == prev.Status {
		return nil, nil
	}
	step, ok := def.Step(params.Status.String)
	if !ok || !step.HandsOff() {
		return nil, nil
	}
	assigneeType, assigneeID, ok := issueworkflow.ResolveHandler(step, project, prev.CreatorType, prev.CreatorID)
	if !ok {
		return nil, nil
	}
	params.AssigneeType = pgtype.Text{String: assigneeType, Valid: true}
	params.AssigneeID = assigneeID
	return &workflowHandoff{def: *def, step: step, project: project}, nil
}

// workflowHandoffPayload describes a handoff on the issue:updated event, so
// the activity log can record the status change and the handoff as one
// timeline entry.
func workflowHandoffPayload(issue db.Issue, hand *workflowHandoff, note string, runStarted bool) map[string]string {
	out := map[string]string{
		"workflow": hand.def.Name,
		"to_type":  issue.AssigneeType.String,
		"to_id":    uuidToString(issue.AssigneeID),
		"run":      strconv.FormatBool(runStarted),
	}
	if runStarted && note != "" {
		out["note"] = note
	}
	return out
}

// writeIssueWorkflowError answers a workflow rejection. It reports false when
// err is not one, so the caller can fall through to its generic 500.
func writeIssueWorkflowError(w http.ResponseWriter, err error) bool {
	var notIn *issueworkflow.NotInWorkflowError
	if errors.As(err, &notIn) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":            notIn.Error(),
			"code":             "status_not_in_workflow",
			"workflow_name":    notIn.WorkflowName,
			"allowed_statuses": notIn.Allowed,
		})
		return true
	}
	var moved *issueworkflow.StepMovedError
	if errors.As(err, &moved) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":          moved.Error(),
			"code":           "workflow_step_moved",
			"step":           moved.Step,
			"current_status": moved.Current,
		})
		return true
	}
	return false
}

// agentRunOnIssue returns the calling agent's run when it works on this issue,
// nil for members and for runs on other issues. The task id is the
// server-trusted X-Task-ID resolveActor vouched for.
func (h *Handler) agentRunOnIssue(r *http.Request, actorType string, issue db.Issue) *db.AgentTaskQueue {
	if actorType != "agent" {
		return nil
	}
	taskID, err := util.ParseUUID(r.Header.Get("X-Task-ID"))
	if err != nil {
		return nil
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil || task.IssueID != issue.ID {
		return nil
	}
	return &task
}

// advanceRunStep moves a run's workflow step along with a status change the
// run made itself, so its next change is judged from where it put the issue.
func (h *Handler) advanceRunStep(ctx context.Context, run *db.AgentTaskQueue, prev, issue db.Issue) {
	if run == nil || !run.WorkflowStep.Valid || prev.Status == issue.Status {
		return
	}
	if err := h.Queries.SetTaskWorkflowStep(ctx, db.SetTaskWorkflowStepParams{
		ID:           run.ID,
		WorkflowStep: pgtype.Text{String: issue.Status, Valid: true},
	}); err != nil {
		slog.Warn("workflow: advance run step failed", "task_id", uuidToString(run.ID), "error", err)
	}
}

// stepMovedError explains a refused status change: where the run's step was,
// where the issue is now, and who moved it.
func (h *Handler) stepMovedError(ctx context.Context, issue db.Issue, step string) error {
	resolver := issuestatus.NewResolver(issue.WorkspaceID)
	out := &issueworkflow.StepMovedError{
		IssueIdentifier: h.getIssuePrefix(ctx, issue.WorkspaceID) + "-" + strconv.Itoa(int(issue.Number)),
		Step:            step,
		StepName:        resolver.Name(ctx, h.Queries, step),
		Current:         issue.Status,
		CurrentName:     resolver.Name(ctx, h.Queries, issue.Status),
	}
	if last, err := h.Queries.GetLastIssueStatusChange(ctx, issue.ID); err == nil && last.ActorID.Valid {
		out.MovedBy = h.actorDisplayName(ctx, issue.WorkspaceID, last.ActorType.String, last.ActorID)
	}
	return out
}

// workflowHandoffNote renders the brief the handler's run receives. Only an
// agent or squad handler runs, so members never need one.
func (h *Handler) workflowHandoffNote(ctx context.Context, issue db.Issue, hand *workflowHandoff) string {
	if hand == nil || !issue.AssigneeType.Valid {
		return ""
	}
	if t := issue.AssigneeType.String; t != "agent" && t != "squad" {
		return ""
	}
	resolver := issuestatus.NewResolver(issue.WorkspaceID)
	identifier := h.getIssuePrefix(ctx, issue.WorkspaceID) + "-" + strconv.Itoa(int(issue.Number))
	return issueworkflow.Brief(issueworkflow.BriefInput{
		Workflow:        hand.def,
		StatusKey:       hand.step.StatusKey,
		IssueIdentifier: identifier,
		StatusName: func(key string) string {
			return resolver.Name(ctx, h.Queries, key)
		},
		HandlerName: func(step issueworkflow.Step) string {
			if !step.HandsOff() {
				return ""
			}
			return h.stepHandlerName(ctx, issue, hand.project, step)
		},
	})
}

// actorDisplayName names an agent, squad or member for a brief. It falls back
// to the actor type so a lookup failure never blocks a handoff.
func (h *Handler) actorDisplayName(ctx context.Context, wsUUID pgtype.UUID, actorType string, id pgtype.UUID) string {
	switch actorType {
	case "agent":
		if agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: wsUUID}); err == nil {
			return agent.Name
		}
	case "squad":
		if squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: id, WorkspaceID: wsUUID}); err == nil {
			return squad.Name
		}
	case "member":
		if user, err := h.Queries.GetUser(ctx, id); err == nil {
			return user.Name
		}
	}
	return actorType
}

// stopPreviousAssigneeRuns cancels the active runs of whoever a handoff took
// the issue from — an agent's runs, or every run made on a squad's behalf —
// when the caller asked for it. A handoff back to the same handler keeps its
// run, and the agent making the handoff keeps its own: that run is the one
// making this write.
func (h *Handler) stopPreviousAssigneeRuns(ctx context.Context, prev, issue db.Issue, actorType, actorID string) {
	if issue.AssigneeType == prev.AssigneeType && issue.AssigneeID == prev.AssigneeID {
		return
	}
	tasks := h.assigneeActiveRuns(ctx, prev)
	if len(tasks) == 0 {
		return
	}
	actor := service.TaskCancellationActor{Type: actorType}
	if id, err := parseUUIDString(actorID); err == nil {
		actor.ID = id
		actor.Name = h.actorDisplayName(ctx, issue.WorkspaceID, actorType, id)
	}
	for _, task := range tasks {
		if actorType == "agent" && uuidToString(task.AgentID) == actorID {
			continue
		}
		if _, err := h.TaskService.CancelTaskByUser(ctx, task.ID, actor); err != nil {
			slog.Warn("workflow: cancel previous run failed",
				"issue_id", uuidToString(issue.ID), "task_id", uuidToString(task.ID), "error", err)
		}
	}
}

// assigneeActiveRuns lists the active runs of an issue's assignee on it: the
// agent's own runs, or every run made on a squad's behalf. Members run
// nothing.
func (h *Handler) assigneeActiveRuns(ctx context.Context, issue db.Issue) []db.AgentTaskQueue {
	if !issue.AssigneeID.Valid {
		return nil
	}
	assigneeType := issue.AssigneeType.String
	if assigneeType != "agent" && assigneeType != "squad" {
		return nil
	}
	tasks, err := h.Queries.ListActiveTasksByIssue(ctx, issue.ID)
	if err != nil {
		slog.Warn("workflow: list active tasks failed", "issue_id", uuidToString(issue.ID), "error", err)
		return nil
	}
	var out []db.AgentTaskQueue
	for _, task := range tasks {
		if (assigneeType == "agent" && task.AgentID == issue.AssigneeID) ||
			(assigneeType == "squad" && task.SquadID == issue.AssigneeID) {
			out = append(out, task)
		}
	}
	return out
}

// claimProjectWorkflow describes the issue's project workflow for a task
// claim, or nil when the project uses the Default workflow.
func (h *Handler) claimProjectWorkflow(ctx context.Context, issue db.Issue) *TaskProjectWorkflowData {
	def, project, err := issueworkflow.ForProject(ctx, h.Queries, issue.WorkspaceID, issue.ProjectID)
	if err != nil {
		slog.Warn("task claim: failed to load project workflow for brief injection",
			"issue_id", uuidToString(issue.ID), "error", err)
		return nil
	}
	if def == nil {
		return nil
	}
	resolver := issuestatus.NewResolver(issue.WorkspaceID)
	out := &TaskProjectWorkflowData{
		Name:             def.Name,
		CurrentStatusKey: issue.Status,
		Steps:            make([]TaskProjectWorkflowStep, 0, len(def.Steps)),
	}
	for _, step := range def.Steps {
		entry := TaskProjectWorkflowStep{
			Key:           step.StatusKey,
			Name:          resolver.Name(ctx, h.Queries, step.StatusKey),
			NextStatusKey: step.NextStatusKey,
			BackStatusKey: step.BackStatusKey,
		}
		if step.HandsOff() {
			entry.Handler = h.stepHandlerName(ctx, issue, project, step)
		}
		if step.StatusKey == issue.Status {
			entry.Instructions = step.Instructions
		}
		out.Steps = append(out.Steps, entry)
	}
	return out
}

// stepHandlerName describes who a step hands the issue to, for briefs.
func (h *Handler) stepHandlerName(ctx context.Context, issue db.Issue, project db.Project, step issueworkflow.Step) string {
	t, id, ok := issueworkflow.ResolveHandler(step, project, issue.CreatorType, issue.CreatorID)
	if !ok {
		switch step.Handler.Type {
		case issueworkflow.HandlerProjectLead:
			return "the project lead (none set)"
		case issueworkflow.HandlerCreator:
			return "the issue creator"
		}
		return ""
	}
	name := h.actorDisplayName(ctx, issue.WorkspaceID, t, id)
	switch step.Handler.Type {
	case issueworkflow.HandlerProjectLead:
		return name + " (project lead)"
	case issueworkflow.HandlerCreator:
		return name + " (issue creator)"
	}
	return name
}
