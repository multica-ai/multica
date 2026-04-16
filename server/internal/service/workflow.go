package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type txStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// WorkflowStep represents a single step in a workflow definition.
type WorkflowStep struct {
	Type          string   `json:"type"`            // "agent", "approval", or "review"
	AgentID       string   `json:"agent_id"`        // for agent steps
	Prompt        string   `json:"prompt"`          // for agent steps (user-facing field)
	Instructions  string   `json:"instructions"`    // for agent steps (legacy alias)
	ReviewerIDs   []string `json:"reviewer_ids"`    // for approval steps
	ReviewAgentID string   `json:"review_agent_id"` // for review steps (agent reviewer)
	ReviewerType  string   `json:"reviewer_type"`   // for review steps: "agent" or "member"
	ReviewPrompt  string   `json:"review_prompt"`   // for review steps: instructions for the reviewer
}

// GetPrompt returns the step prompt, falling back to instructions for backward compat.
func (s WorkflowStep) GetPrompt() string {
	if s.Prompt != "" {
		return s.Prompt
	}
	return s.Instructions
}

type WorkflowService struct {
	Queries     *db.Queries
	Hub         *realtime.Hub
	Bus         *events.Bus
	TaskService *TaskService
	TxStarter   txStarter
}

func NewWorkflowService(q *db.Queries, hub *realtime.Hub, bus *events.Bus, ts *TaskService, txs txStarter) *WorkflowService {
	return &WorkflowService{Queries: q, Hub: hub, Bus: bus, TaskService: ts, TxStarter: txs}
}

// startWorkflowConfig holds optional parameters for StartWorkflow.
type startWorkflowConfig struct {
	skipReview bool
}

// StartWorkflowOption configures StartWorkflow behavior.
type StartWorkflowOption func(*startWorkflowConfig)

// WithSkipReview makes the run auto-approve all review/approval steps.
func WithSkipReview(skip bool) StartWorkflowOption {
	return func(c *startWorkflowConfig) { c.skipReview = skip }
}

// StartWorkflow creates a new workflow run and begins executing the first step.
func (s *WorkflowService) StartWorkflow(ctx context.Context, workflowID pgtype.UUID, triggerType string, triggerID pgtype.UUID, opts ...StartWorkflowOption) (*db.WorkflowRun, error) {
	cfg := &startWorkflowConfig{}
	for _, o := range opts {
		o(cfg)
	}
	workflow, err := s.Queries.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}

	// Orchestrated mode: delegate planning to the planner agent
	if workflow.Mode == "orchestrated" {
		return s.startOrchestratedWorkflow(ctx, workflow, triggerType, triggerID, cfg.skipReview)
	}

	var steps []WorkflowStep
	if err := json.Unmarshal(workflow.Steps, &steps); err != nil {
		return nil, fmt.Errorf("parse workflow steps: %w", err)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("workflow has no steps")
	}

	// Create the workflow run
	run, err := s.Queries.CreateWorkflowRun(ctx, db.CreateWorkflowRunParams{
		WorkflowID:       workflowID,
		WorkspaceID:      workflow.WorkspaceID,
		TriggerType:      triggerType,
		TriggerID:        triggerID,
		Status:           "running",
		CurrentStepIndex: 0,
		Context:          []byte("{}"),
		SkipReview:       cfg.skipReview,
	})
	if err != nil {
		return nil, fmt.Errorf("create workflow run: %w", err)
	}

	slog.Info("workflow run started",
		"run_id", util.UUIDToString(run.ID),
		"workflow_id", util.UUIDToString(workflowID),
		"trigger_type", triggerType,
	)

	s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunCreated, run)

	// Execute the first step
	if err := s.executeStep(ctx, &run, steps, 0); err != nil {
		slog.Error("failed to execute first step", "run_id", util.UUIDToString(run.ID), "error", err)
		s.failRun(ctx, run.ID)
		return &run, fmt.Errorf("execute first step: %w", err)
	}

	return &run, nil
}

// AdvanceWorkflow advances to the next step after the current one completes.
func (s *WorkflowService) AdvanceWorkflow(ctx context.Context, runID pgtype.UUID) error {
	run, err := s.Queries.GetWorkflowRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get workflow run: %w", err)
	}

	if run.Status != "running" && run.Status != "paused" {
		slog.Debug("advance workflow skipped: run not active", "run_id", util.UUIDToString(runID), "status", run.Status)
		return nil
	}

	workflow, err := s.Queries.GetWorkflow(ctx, run.WorkflowID)
	if err != nil {
		return fmt.Errorf("get workflow: %w", err)
	}

	steps, err := s.getStepsForRun(run, workflow)
	if err != nil {
		return fmt.Errorf("get steps: %w", err)
	}

	nextIndex := run.CurrentStepIndex + 1
	if nextIndex >= int32(len(steps)) {
		// All steps completed
		run, err = s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
			ID:               runID,
			Status:           "completed",
			CurrentStepIndex: run.CurrentStepIndex,
		})
		if err != nil {
			return fmt.Errorf("complete workflow run: %w", err)
		}
		slog.Info("workflow run completed", "run_id", util.UUIDToString(runID))
		s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunUpdated, run)
		return nil
	}

	// Update run to next step
	run, err = s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
		ID:               runID,
		Status:           "running",
		CurrentStepIndex: nextIndex,
	})
	if err != nil {
		return fmt.Errorf("update workflow run: %w", err)
	}

	s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunUpdated, run)

	// Execute next step
	if err := s.executeStep(ctx, &run, steps, int(nextIndex)); err != nil {
		slog.Error("failed to execute step", "run_id", util.UUIDToString(runID), "step_index", nextIndex, "error", err)
		s.failRun(ctx, runID)
		return fmt.Errorf("execute step %d: %w", nextIndex, err)
	}

	return nil
}

// HandleApprovalDecision processes a reviewer's decision on an approval step.
func (s *WorkflowService) HandleApprovalDecision(ctx context.Context, stepRunID pgtype.UUID, decision, comment string) error {
	stepRun, err := s.Queries.UpdateStepRunDecision(ctx, db.UpdateStepRunDecisionParams{
		ID:              stepRunID,
		Decision:        pgtype.Text{String: decision, Valid: true},
		DecisionComment: pgtype.Text{String: comment, Valid: comment != ""},
	})
	if err != nil {
		return fmt.Errorf("update step decision: %w", err)
	}

	slog.Info("approval decision recorded",
		"step_run_id", util.UUIDToString(stepRunID),
		"decision", decision,
	)

	s.broadcastStepEvent(ctx, stepRun)

	switch decision {
	case "approved":
		// Advance to next step
		return s.AdvanceWorkflow(ctx, stepRun.WorkflowRunID)

	case "rejected":
		// Roll back to previous agent step and re-execute
		return s.rollbackToPreviousAgentStep(ctx, stepRun.WorkflowRunID, stepRun.StepIndex)

	case "stopped":
		// Cancel the entire workflow run
		run, err := s.Queries.CancelWorkflowRun(ctx, stepRun.WorkflowRunID)
		if err != nil {
			return fmt.Errorf("cancel workflow run: %w", err)
		}
		s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunUpdated, run)
		return nil

	default:
		return fmt.Errorf("unknown decision: %s", decision)
	}
}

// HandleReviewDecision processes a reviewer's decision on a review step, with redirect support.
func (s *WorkflowService) HandleReviewDecision(ctx context.Context, stepRunID pgtype.UUID, decision, comment string, redirectToStep *int) error {
	params := db.UpdateStepRunDecisionParams{
		ID:              stepRunID,
		Decision:        pgtype.Text{String: decision, Valid: true},
		DecisionComment: pgtype.Text{String: comment, Valid: comment != ""},
	}
	if decision == "redirect" && redirectToStep != nil {
		params.RedirectToStep = pgtype.Int4{Int32: int32(*redirectToStep), Valid: true}
	}

	stepRun, err := s.Queries.UpdateStepRunDecision(ctx, params)
	if err != nil {
		return fmt.Errorf("update step decision: %w", err)
	}

	slog.Info("review decision recorded",
		"step_run_id", util.UUIDToString(stepRunID),
		"decision", decision,
	)

	s.broadcastStepEvent(ctx, stepRun)

	switch decision {
	case "approved":
		return s.AdvanceWorkflow(ctx, stepRun.WorkflowRunID)

	case "rejected":
		// Post rejection feedback as a comment on the issue if comment is provided
		if comment != "" {
			run, err := s.Queries.GetWorkflowRun(ctx, stepRun.WorkflowRunID)
			if err == nil && run.IssueID.Valid {
				s.TaskService.createAgentComment(ctx, run.IssueID, pgtype.UUID{}, "Review rejected: "+comment, "system", pgtype.UUID{})
			}
		}
		return s.rollbackToPreviousAgentStep(ctx, stepRun.WorkflowRunID, stepRun.StepIndex)

	case "stopped":
		run, err := s.Queries.CancelWorkflowRun(ctx, stepRun.WorkflowRunID)
		if err != nil {
			return fmt.Errorf("cancel workflow run: %w", err)
		}
		s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunUpdated, run)
		return nil

	case "redirect":
		if redirectToStep == nil {
			return fmt.Errorf("redirect_to_step is required for redirect decision")
		}
		return s.redirectToStep(ctx, stepRun.WorkflowRunID, *redirectToStep)

	default:
		return fmt.Errorf("unknown decision: %s", decision)
	}
}

// OnTaskCompleted is called when an agent task completes. It checks if
// the task is part of a workflow and advances the workflow if so.
func (s *WorkflowService) OnTaskCompleted(ctx context.Context, taskID pgtype.UUID) {
	// Check if this is a planner task
	plannerRun, err := s.Queries.GetWorkflowRunByPlannerTask(ctx, taskID)
	if err == nil {
		s.onPlannerTaskCompleted(ctx, plannerRun)
		return
	}

	stepRun, err := s.Queries.GetStepRunByAgentTaskID(ctx, taskID)
	if err != nil {
		// Not a workflow task — ignore
		return
	}

	// For review steps with agent reviewer: check if the agent submitted a decision
	if stepRun.StepType == "review" {
		if stepRun.Decision.Valid {
			// Decision already submitted via CLI during execution — process it
			slog.Info("review agent task completed with decision",
				"step_run_id", util.UUIDToString(stepRun.ID),
				"decision", stepRun.Decision.String,
			)
			s.processReviewDecisionAfterCompletion(ctx, stepRun)
			return
		}
		// No decision submitted — treat as "approved" by default
		slog.Warn("review agent completed without submitting decision, defaulting to approved",
			"step_run_id", util.UUIDToString(stepRun.ID),
		)
		var noRedirect *int
		if err := s.HandleReviewDecision(ctx, stepRun.ID, "approved", "Auto-approved: reviewer agent did not submit explicit decision", noRedirect); err != nil {
			slog.Error("failed to auto-approve review step", "step_run_id", util.UUIDToString(stepRun.ID), "error", err)
		}
		return
	}

	// Mark step as completed
	stepRun, err = s.Queries.UpdateWorkflowStepRunStatus(ctx, db.UpdateWorkflowStepRunStatusParams{
		ID:     stepRun.ID,
		Status: "completed",
	})
	if err != nil {
		slog.Error("failed to mark step completed", "step_run_id", util.UUIDToString(stepRun.ID), "error", err)
		return
	}

	// Propagate the agent task's work_dir to the workflow_run so subsequent
	// steps can reuse the same execution directory (avoids cold-start).
	if stepRun.AgentTaskID.Valid {
		if task, err := s.Queries.GetAgentTask(ctx, stepRun.AgentTaskID); err == nil && task.WorkDir.Valid {
			run, err := s.Queries.GetWorkflowRun(ctx, stepRun.WorkflowRunID)
			if err == nil && !run.WorkDir.Valid {
				if _, err := s.Queries.SetWorkflowRunWorkDir(ctx, db.SetWorkflowRunWorkDirParams{
					ID:      stepRun.WorkflowRunID,
					WorkDir: task.WorkDir,
				}); err != nil {
					slog.Warn("failed to save work_dir to workflow run", "run_id", util.UUIDToString(stepRun.WorkflowRunID), "error", err)
				} else {
					slog.Info("workflow run work_dir set from step",
						"run_id", util.UUIDToString(stepRun.WorkflowRunID),
						"work_dir", task.WorkDir.String,
					)
				}
			}
		}
	}

	slog.Info("workflow step completed",
		"step_run_id", util.UUIDToString(stepRun.ID),
		"run_id", util.UUIDToString(stepRun.WorkflowRunID),
	)

	s.broadcastStepEvent(ctx, stepRun)

	// Advance workflow
	if err := s.AdvanceWorkflow(ctx, stepRun.WorkflowRunID); err != nil {
		slog.Error("failed to advance workflow after task completion",
			"run_id", util.UUIDToString(stepRun.WorkflowRunID), "error", err)
	}
}

// OnTaskFailed is called when an agent task fails. It checks if
// the task is part of a workflow and marks the run as failed if so.
func (s *WorkflowService) OnTaskFailed(ctx context.Context, taskID pgtype.UUID) {
	stepRun, err := s.Queries.GetStepRunByAgentTaskID(ctx, taskID)
	if err != nil {
		return // Not a workflow task
	}

	// Mark step as failed
	s.Queries.UpdateWorkflowStepRunStatus(ctx, db.UpdateWorkflowStepRunStatusParams{
		ID:     stepRun.ID,
		Status: "failed",
	})

	slog.Warn("workflow step failed",
		"step_run_id", util.UUIDToString(stepRun.ID),
		"run_id", util.UUIDToString(stepRun.WorkflowRunID),
	)

	s.failRun(ctx, stepRun.WorkflowRunID)
}

// executeStep runs a specific step in the workflow.
func (s *WorkflowService) executeStep(ctx context.Context, run *db.WorkflowRun, steps []WorkflowStep, index int) error {
	step := steps[index]

	// If skip_review is enabled, auto-skip review and approval steps.
	if run.SkipReview && (step.Type == "approval" || step.Type == "review") {
		stepRun, err := s.Queries.CreateWorkflowStepRun(ctx, db.CreateWorkflowStepRunParams{
			WorkflowRunID: run.ID,
			StepIndex:     int32(index),
			StepType:      step.Type,
			Status:        "completed",
		})
		if err != nil {
			return fmt.Errorf("create skipped step run: %w", err)
		}
		s.Queries.UpdateStepRunDecision(ctx, db.UpdateStepRunDecisionParams{
			ID:       stepRun.ID,
			Decision: pgtype.Text{String: "approved", Valid: true},
		})
		slog.Info("skip_review: auto-approved step",
			"run_id", util.UUIDToString(run.ID),
			"step_index", index,
			"step_type", step.Type,
		)
		s.broadcastStepEvent(ctx, stepRun)
		return s.AdvanceWorkflow(ctx, run.ID)
	}

	// Create step run record
	stepRun, err := s.Queries.CreateWorkflowStepRun(ctx, db.CreateWorkflowStepRunParams{
		WorkflowRunID: run.ID,
		StepIndex:     int32(index),
		StepType:      step.Type,
		Status:        "running",
	})
	if err != nil {
		return fmt.Errorf("create step run: %w", err)
	}

	switch step.Type {
	case "agent":
		return s.executeAgentStep(ctx, run, &stepRun, step)
	case "approval":
		return s.executeApprovalStep(ctx, run, &stepRun, step)
	case "review":
		return s.executeReviewStep(ctx, run, &stepRun, step)
	default:
		return fmt.Errorf("unknown step type: %s", step.Type)
	}
}

// executeAgentStep creates an issue (if needed) and queues an agent task.
func (s *WorkflowService) executeAgentStep(ctx context.Context, run *db.WorkflowRun, stepRun *db.WorkflowStepRun, step WorkflowStep) error {
	agentID := util.ParseUUID(step.AgentID)
	if !agentID.Valid {
		return fmt.Errorf("invalid agent_id: %s", step.AgentID)
	}

	// Get the workflow for title
	workflow, err := s.Queries.GetWorkflow(ctx, run.WorkflowID)
	if err != nil {
		return fmt.Errorf("get workflow: %w", err)
	}

	prompt := step.GetPrompt()

	// Resolve creatorID: use the trigger_id (user who triggered the workflow)
	// For manual triggers, trigger_id is the user_id.
	creatorID := run.TriggerID
	creatorType := "member"
	if !creatorID.Valid {
		// Fallback: use the workflow creator
		creatorID = workflow.CreatedBy
	}

	var issue db.Issue
	if run.IssueID.Valid {
		// Reuse existing issue — update assignee to this step's agent
		issue, err = s.Queries.GetIssue(ctx, run.IssueID)
		if err != nil {
			return fmt.Errorf("get issue: %w", err)
		}
		issue, err = s.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
			ID:           issue.ID,
			AssigneeType: pgtype.Text{String: "agent", Valid: true},
			AssigneeID:   agentID,
		})
		if err != nil {
			return fmt.Errorf("update issue assignee: %w", err)
		}
	} else {
		// Create a new issue in a transaction (needs IncrementIssueCounter)
		tx, err := s.TxStarter.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		defer tx.Rollback(ctx)

		qtx := s.Queries.WithTx(tx)

		issueNumber, err := qtx.IncrementIssueCounter(ctx, run.WorkspaceID)
		if err != nil {
			return fmt.Errorf("increment issue counter: %w", err)
		}

		title := fmt.Sprintf("[Workflow] %s", workflow.Name)
		description := prompt
		if description == "" {
			description = fmt.Sprintf("Automated workflow run - step %d", stepRun.StepIndex+1)
		}

		issue, err = qtx.CreateIssue(ctx, db.CreateIssueParams{
			WorkspaceID:  run.WorkspaceID,
			Title:        title,
			Description:  pgtype.Text{String: description, Valid: true},
			Status:       "todo",
			Priority:     "medium",
			AssigneeType: pgtype.Text{String: "agent", Valid: true},
			AssigneeID:   agentID,
			CreatorType:  creatorType,
			CreatorID:    creatorID,
			Number:       issueNumber,
		})
		if err != nil {
			return fmt.Errorf("create issue: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit tx: %w", err)
		}

		// Link issue to the workflow run
		ctxMap := map[string]any{"issue_id": util.UUIDToString(issue.ID)}
		newCtx, _ := json.Marshal(ctxMap)
		updatedRun, err := s.Queries.SetWorkflowRunIssue(ctx, db.SetWorkflowRunIssueParams{
			ID:      run.ID,
			IssueID: issue.ID,
			Context: newCtx,
		})
		if err != nil {
			slog.Warn("failed to link issue to workflow run", "run_id", util.UUIDToString(run.ID), "error", err)
		} else {
			*run = updatedRun
		}
	}

	// If there are prompt/instructions for this step, post them as a comment
	if prompt != "" {
		s.TaskService.createAgentComment(ctx, issue.ID, agentID, prompt, "system", pgtype.UUID{})
	}

	// Enqueue the agent task
	task, err := s.TaskService.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		return fmt.Errorf("enqueue agent task: %w", err)
	}

	// Link the task to the step run
	s.Queries.SetWorkflowStepRunAgentTask(ctx, db.SetWorkflowStepRunAgentTaskParams{
		ID:          stepRun.ID,
		AgentTaskID: task.ID,
	})

	s.broadcastStepEvent(ctx, *stepRun)

	return nil
}

// executeApprovalStep pauses the workflow and notifies reviewers.
func (s *WorkflowService) executeApprovalStep(ctx context.Context, run *db.WorkflowRun, stepRun *db.WorkflowStepRun, step WorkflowStep) error {
	// Pause the workflow run
	updatedRun, err := s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
		ID:               run.ID,
		Status:           "paused",
		CurrentStepIndex: run.CurrentStepIndex,
	})
	if err != nil {
		return fmt.Errorf("pause workflow run: %w", err)
	}

	// Set first reviewer (for now, use the first reviewer_id)
	if len(step.ReviewerIDs) > 0 {
		reviewerID := util.ParseUUID(step.ReviewerIDs[0])
		if reviewerID.Valid {
			s.Queries.SetWorkflowStepRunReviewer(ctx, db.SetWorkflowStepRunReviewerParams{
				ID:         stepRun.ID,
				ReviewerID: reviewerID,
			})
		}

		// Create inbox notification for each reviewer
		workflow, _ := s.Queries.GetWorkflow(ctx, run.WorkflowID)
		for _, rid := range step.ReviewerIDs {
			reviewerUUID := util.ParseUUID(rid)
			if !reviewerUUID.Valid {
				continue
			}
			// Find the member for this user in the workspace
			member, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
				UserID:      reviewerUUID,
				WorkspaceID: run.WorkspaceID,
			})
			if err != nil {
				slog.Warn("reviewer not found in workspace", "user_id", rid, "error", err)
				continue
			}

			s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
				WorkspaceID:   run.WorkspaceID,
				RecipientType: "member",
				RecipientID:   member.ID,
				Type:          "workflow_approval",
				Severity:      "info",
				Title:         fmt.Sprintf("Approval required: %s", workflow.Name),
				Body:          pgtype.Text{String: fmt.Sprintf("Step %d requires your approval", stepRun.StepIndex+1), Valid: true},
				ActorType:     pgtype.Text{String: "system", Valid: true},
				Details:       []byte(fmt.Sprintf(`{"step_run_id":"%s","workflow_run_id":"%s"}`, util.UUIDToString(stepRun.ID), util.UUIDToString(run.ID))),
			})

			s.Bus.Publish(events.Event{
				Type:        protocol.EventInboxNew,
				WorkspaceID: util.UUIDToString(run.WorkspaceID),
				ActorType:   "system",
				Payload: map[string]any{
					"item": map[string]any{
						"recipient_id": rid,
						"type":         "workflow_approval",
						"title":        fmt.Sprintf("Approval required: %s", workflow.Name),
					},
				},
			})
		}
	}

	s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunUpdated, updatedRun)
	s.broadcastStepEvent(ctx, *stepRun)

	return nil
}

// rollbackToPreviousAgentStep finds and re-executes the most recent agent step.
func (s *WorkflowService) rollbackToPreviousAgentStep(ctx context.Context, runID pgtype.UUID, currentStepIndex int32) error {
	run, err := s.Queries.GetWorkflowRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get workflow run: %w", err)
	}

	workflow, err := s.Queries.GetWorkflow(ctx, run.WorkflowID)
	if err != nil {
		return fmt.Errorf("get workflow: %w", err)
	}

	steps, err := s.getStepsForRun(run, workflow)
	if err != nil {
		return fmt.Errorf("get steps: %w", err)
	}

	// Find previous agent step
	targetIndex := -1
	for i := int(currentStepIndex) - 1; i >= 0; i-- {
		if steps[i].Type == "agent" {
			targetIndex = i
			break
		}
	}

	if targetIndex < 0 {
		// No previous agent step — fail the run
		slog.Warn("no previous agent step to roll back to", "run_id", util.UUIDToString(runID))
		s.failRun(ctx, runID)
		return nil
	}

	// Update run to target step
	run, err = s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
		ID:               runID,
		Status:           "running",
		CurrentStepIndex: int32(targetIndex),
	})
	if err != nil {
		return fmt.Errorf("update workflow run: %w", err)
	}

	s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunUpdated, run)

	// Execute the step again
	return s.executeStep(ctx, &run, steps, targetIndex)
}

// failRun marks a workflow run as failed.
func (s *WorkflowService) failRun(ctx context.Context, runID pgtype.UUID) {
	// Fetch current run to preserve current_step_index
	currentRun, err := s.Queries.GetWorkflowRun(ctx, runID)
	if err != nil {
		slog.Error("failed to get workflow run for failure", "run_id", util.UUIDToString(runID), "error", err)
		return
	}
	run, err := s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
		ID:               runID,
		Status:           "failed",
		CurrentStepIndex: currentRun.CurrentStepIndex,
	})
	if err != nil {
		slog.Error("failed to mark workflow run as failed", "run_id", util.UUIDToString(runID), "error", err)
		return
	}
	s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunUpdated, run)
}

// getStepsForRun returns the steps to use for a workflow run.
// For orchestrated workflows with a submitted plan, it uses dynamic_steps.
// Otherwise it falls back to the static workflow.Steps.
func (s *WorkflowService) getStepsForRun(run db.WorkflowRun, workflow db.Workflow) ([]WorkflowStep, error) {
	if len(run.DynamicSteps) > 0 {
		var steps []WorkflowStep
		if err := json.Unmarshal(run.DynamicSteps, &steps); err != nil {
			return nil, fmt.Errorf("parse dynamic steps: %w", err)
		}
		return steps, nil
	}
	var steps []WorkflowStep
	if err := json.Unmarshal(workflow.Steps, &steps); err != nil {
		return nil, fmt.Errorf("parse workflow steps: %w", err)
	}
	return steps, nil
}

// redirectToStep jumps the workflow run to a specific step index and re-executes it.
func (s *WorkflowService) redirectToStep(ctx context.Context, runID pgtype.UUID, targetIndex int) error {
	run, err := s.Queries.GetWorkflowRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get workflow run: %w", err)
	}

	workflow, err := s.Queries.GetWorkflow(ctx, run.WorkflowID)
	if err != nil {
		return fmt.Errorf("get workflow: %w", err)
	}

	steps, err := s.getStepsForRun(run, workflow)
	if err != nil {
		return fmt.Errorf("get steps: %w", err)
	}

	if targetIndex < 0 || targetIndex >= len(steps) {
		return fmt.Errorf("redirect_to_step %d out of range [0, %d)", targetIndex, len(steps))
	}

	// Update run to target step
	run, err = s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
		ID:               runID,
		Status:           "running",
		CurrentStepIndex: int32(targetIndex),
	})
	if err != nil {
		return fmt.Errorf("update workflow run: %w", err)
	}

	s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunUpdated, run)

	return s.executeStep(ctx, &run, steps, targetIndex)
}

// executeReviewStep handles a review gate step.
// If reviewer_type == "agent": enqueue a review task for the agent.
// If reviewer_type == "member": pause and notify (similar to approval).
func (s *WorkflowService) executeReviewStep(ctx context.Context, run *db.WorkflowRun, stepRun *db.WorkflowStepRun, step WorkflowStep) error {
	if step.ReviewerType == "member" || step.ReviewerType == "" {
		// Delegate to approval-like behavior with member reviewers
		approvalStep := WorkflowStep{
			Type:        "approval",
			ReviewerIDs: step.ReviewerIDs,
		}
		return s.executeApprovalStep(ctx, run, stepRun, approvalStep)
	}

	// Agent reviewer
	agentID := util.ParseUUID(step.ReviewAgentID)
	if !agentID.Valid {
		return fmt.Errorf("invalid review_agent_id: %s", step.ReviewAgentID)
	}

	if !run.IssueID.Valid {
		return fmt.Errorf("review step requires an issue (no issue linked to run)")
	}

	issue, err := s.Queries.GetIssue(ctx, run.IssueID)
	if err != nil {
		return fmt.Errorf("get issue for review: %w", err)
	}

	// Update issue assignee to the review agent
	issue, err = s.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
		ID:           issue.ID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   agentID,
	})
	if err != nil {
		return fmt.Errorf("update issue assignee for review: %w", err)
	}

	// Post review prompt as a system comment
	reviewComment := fmt.Sprintf("**Review Step %d**\n\n%s\n\n---\nPlease review the work above and submit your decision using:\n`multica workflow review %s --decision approve|reject|stop|redirect [--comment \"...\"] [--redirect-to N]`",
		stepRun.StepIndex+1, step.ReviewPrompt, util.UUIDToString(stepRun.ID))

	s.TaskService.createAgentComment(ctx, issue.ID, agentID, reviewComment, "system", pgtype.UUID{})

	// Enqueue agent task
	task, err := s.TaskService.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		return fmt.Errorf("enqueue review agent task: %w", err)
	}

	// Link the task to the step run
	s.Queries.SetWorkflowStepRunAgentTask(ctx, db.SetWorkflowStepRunAgentTaskParams{
		ID:          stepRun.ID,
		AgentTaskID: task.ID,
	})

	s.broadcastStepEvent(ctx, *stepRun)
	return nil
}

// processReviewDecisionAfterCompletion processes a review decision that was
// submitted by the agent during task execution (via CLI).
func (s *WorkflowService) processReviewDecisionAfterCompletion(ctx context.Context, stepRun db.WorkflowStepRun) {
	decision := stepRun.Decision.String
	comment := ""
	if stepRun.DecisionComment.Valid {
		comment = stepRun.DecisionComment.String
	}

	s.broadcastStepEvent(ctx, stepRun)

	var redirectTo *int
	if decision == "redirect" && stepRun.RedirectToStep.Valid {
		v := int(stepRun.RedirectToStep.Int32)
		redirectTo = &v
	}

	// The decision is already stored — just process the side effects
	switch decision {
	case "approved":
		if err := s.AdvanceWorkflow(ctx, stepRun.WorkflowRunID); err != nil {
			slog.Error("failed to advance workflow after review", "error", err)
		}
	case "rejected":
		if comment != "" {
			run, err := s.Queries.GetWorkflowRun(ctx, stepRun.WorkflowRunID)
			if err == nil && run.IssueID.Valid {
				s.TaskService.createAgentComment(ctx, run.IssueID, pgtype.UUID{}, "Review rejected: "+comment, "system", pgtype.UUID{})
			}
		}
		if err := s.rollbackToPreviousAgentStep(ctx, stepRun.WorkflowRunID, stepRun.StepIndex); err != nil {
			slog.Error("failed to rollback after review rejection", "error", err)
		}
	case "stopped":
		run, err := s.Queries.CancelWorkflowRun(ctx, stepRun.WorkflowRunID)
		if err != nil {
			slog.Error("failed to cancel workflow after stop decision", "error", err)
			return
		}
		s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunUpdated, run)
	case "redirect":
		if redirectTo != nil {
			if err := s.redirectToStep(ctx, stepRun.WorkflowRunID, *redirectTo); err != nil {
				slog.Error("failed to redirect workflow", "error", err)
			}
		}
	}
}

// --- Orchestrated workflow ---

// startOrchestratedWorkflow creates a run in "planning" status and enqueues a task for the planner agent.
func (s *WorkflowService) startOrchestratedWorkflow(ctx context.Context, workflow db.Workflow, triggerType string, triggerID pgtype.UUID, skipReview bool) (*db.WorkflowRun, error) {
	if !workflow.PlannerAgentID.Valid {
		return nil, fmt.Errorf("orchestrated workflow requires a planner_agent_id")
	}

	run, err := s.Queries.CreateWorkflowRun(ctx, db.CreateWorkflowRunParams{
		WorkflowID:       workflow.ID,
		WorkspaceID:      workflow.WorkspaceID,
		TriggerType:      triggerType,
		TriggerID:        triggerID,
		Status:           "planning",
		CurrentStepIndex: -1,
		Context:          []byte("{}"),
		SkipReview:       skipReview,
	})
	if err != nil {
		return nil, fmt.Errorf("create orchestrated workflow run: %w", err)
	}

	slog.Info("orchestrated workflow run started (planning)",
		"run_id", util.UUIDToString(run.ID),
		"workflow_id", util.UUIDToString(workflow.ID),
		"planner_agent_id", util.UUIDToString(workflow.PlannerAgentID),
	)

	s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunCreated, run)

	// Create a planner step run record
	stepRun, err := s.Queries.CreateWorkflowStepRun(ctx, db.CreateWorkflowStepRunParams{
		WorkflowRunID: run.ID,
		StepIndex:     -1,
		StepType:      "planner",
		Status:        "running",
	})
	if err != nil {
		slog.Error("failed to create planner step run", "error", err)
	}

	// Resolve creator for the issue
	creatorID := triggerID
	creatorType := "member"
	if !creatorID.Valid {
		creatorID = workflow.CreatedBy
	}

	// Create an issue for the workflow
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	issueNumber, err := qtx.IncrementIssueCounter(ctx, run.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("increment issue counter: %w", err)
	}

	prompt := workflow.Description
	if prompt == "" {
		prompt = fmt.Sprintf("Plan and execute workflow: %s", workflow.Name)
	}

	issue, err := qtx.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:  run.WorkspaceID,
		Title:        fmt.Sprintf("[Workflow] %s", workflow.Name),
		Description:  pgtype.Text{String: prompt, Valid: true},
		Status:       "todo",
		Priority:     "medium",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   workflow.PlannerAgentID,
		CreatorType:  creatorType,
		CreatorID:    creatorID,
		Number:       issueNumber,
	})
	if err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	// Link issue to run
	ctxMap := map[string]any{"issue_id": util.UUIDToString(issue.ID)}
	newCtx, _ := json.Marshal(ctxMap)
	updatedRun, err := s.Queries.SetWorkflowRunIssue(ctx, db.SetWorkflowRunIssueParams{
		ID:      run.ID,
		IssueID: issue.ID,
		Context: newCtx,
	})
	if err != nil {
		slog.Warn("failed to link issue to orchestrated run", "error", err)
	} else {
		run = updatedRun
	}

	// Post planning instructions as a comment
	skipReviewHint := ""
	if skipReview {
		skipReviewHint = "\n\n**NOTE: This run has skip_review enabled.** Do NOT add review or approval steps — only add agent steps. All work will proceed without human review."
	}
	planningComment := fmt.Sprintf("**Workflow Planning**\n\n%s%s\n\n---\nYou are the planner agent for this workflow. Your job is to create an execution plan.\n\n"+
		"1. Use `multica agent list --output json` to see available agents\n"+
		"2. Design a sequence of steps (agent work + review gates)\n"+
		"3. Submit your plan using:\n```\nmultica workflow plan %s --steps '[{\"type\":\"agent\",\"agent_id\":\"...\",\"prompt\":\"...\"},{\"type\":\"review\",\"reviewer_type\":\"agent\",\"review_agent_id\":\"...\",\"review_prompt\":\"...\"}]'\n```\n\n"+
		"Each step must have a `type` (agent/review). Agent steps need `agent_id` and `prompt`. Review steps need `reviewer_type` (agent/member), and if agent: `review_agent_id` + `review_prompt`.",
		prompt, skipReviewHint, util.UUIDToString(run.ID))

	s.TaskService.createAgentComment(ctx, issue.ID, workflow.PlannerAgentID, planningComment, "system", pgtype.UUID{})

	// Enqueue task for planner agent
	task, err := s.TaskService.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		return nil, fmt.Errorf("enqueue planner task: %w", err)
	}

	// Link planner task
	s.Queries.SetWorkflowRunPlannerTask(ctx, db.SetWorkflowRunPlannerTaskParams{
		ID:            run.ID,
		PlannerTaskID: task.ID,
	})

	// Link task to step run
	if stepRun.ID.Valid {
		s.Queries.SetWorkflowStepRunAgentTask(ctx, db.SetWorkflowStepRunAgentTaskParams{
			ID:          stepRun.ID,
			AgentTaskID: task.ID,
		})
	}

	return &run, nil
}

// SubmitPlan is called when a planner agent submits its execution plan.
func (s *WorkflowService) SubmitPlan(ctx context.Context, runID pgtype.UUID, steps []WorkflowStep) error {
	run, err := s.Queries.GetWorkflowRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get workflow run: %w", err)
	}

	if run.Status != "planning" {
		return fmt.Errorf("workflow run is not in planning status (current: %s)", run.Status)
	}

	// Validate steps
	for i, step := range steps {
		switch step.Type {
		case "agent":
			if step.AgentID == "" {
				return fmt.Errorf("step %d (agent) requires agent_id", i)
			}
		case "review":
			if step.ReviewerType == "agent" && step.ReviewAgentID == "" {
				return fmt.Errorf("step %d (review) with agent reviewer requires review_agent_id", i)
			}
		case "approval":
			// OK
		default:
			return fmt.Errorf("step %d has invalid type: %s", i, step.Type)
		}
	}

	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		return fmt.Errorf("marshal steps: %w", err)
	}

	_, err = s.Queries.SetWorkflowRunDynamicSteps(ctx, db.SetWorkflowRunDynamicStepsParams{
		ID:           runID,
		DynamicSteps: stepsJSON,
	})
	if err != nil {
		return fmt.Errorf("save dynamic steps: %w", err)
	}

	slog.Info("workflow plan submitted",
		"run_id", util.UUIDToString(runID),
		"step_count", len(steps),
	)

	return nil
}

// onPlannerTaskCompleted is called when the planner agent's task completes.
func (s *WorkflowService) onPlannerTaskCompleted(ctx context.Context, run db.WorkflowRun) {
	// Mark planner step run as completed
	stepRuns, err := s.Queries.ListWorkflowStepRuns(ctx, run.ID)
	if err == nil {
		for _, sr := range stepRuns {
			if sr.StepType == "planner" && sr.Status == "running" {
				s.Queries.UpdateWorkflowStepRunStatus(ctx, db.UpdateWorkflowStepRunStatusParams{
					ID:     sr.ID,
					Status: "completed",
				})
				s.broadcastStepEvent(ctx, sr)
				break
			}
		}
	}

	// Check if dynamic_steps is populated
	if len(run.DynamicSteps) == 0 {
		slog.Error("planner task completed but no plan was submitted", "run_id", util.UUIDToString(run.ID))
		s.failRun(ctx, run.ID)
		return
	}

	var steps []WorkflowStep
	if err := json.Unmarshal(run.DynamicSteps, &steps); err != nil || len(steps) == 0 {
		slog.Error("planner submitted invalid plan", "run_id", util.UUIDToString(run.ID), "error", err)
		s.failRun(ctx, run.ID)
		return
	}

	// Transition to running and start first step
	updatedRun, err := s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
		ID:               run.ID,
		Status:           "running",
		CurrentStepIndex: 0,
	})
	if err != nil {
		slog.Error("failed to start orchestrated run", "error", err)
		s.failRun(ctx, run.ID)
		return
	}

	slog.Info("orchestrated workflow plan accepted, executing step 0",
		"run_id", util.UUIDToString(run.ID),
		"steps", len(steps),
	)

	s.broadcastWorkflowRunEvent(ctx, protocol.EventWorkflowRunUpdated, updatedRun)

	if err := s.executeStep(ctx, &updatedRun, steps, 0); err != nil {
		slog.Error("failed to execute first orchestrated step", "error", err)
		s.failRun(ctx, run.ID)
	}
}

func (s *WorkflowService) broadcastWorkflowRunEvent(ctx context.Context, eventType string, run db.WorkflowRun) {
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(run.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"workflow_run": map[string]any{
				"id":                 util.UUIDToString(run.ID),
				"workflow_id":        util.UUIDToString(run.WorkflowID),
				"status":             run.Status,
				"current_step_index": run.CurrentStepIndex,
				"trigger_type":       run.TriggerType,
				"created_at":         util.TimestampToString(run.CreatedAt),
			},
		},
	})
}

// CEOCommandResult is the response payload for a CEO command execution.
type CEOCommandResult struct {
	WorkflowID    string `json:"workflow_id"`
	WorkflowRunID string `json:"workflow_run_id"`
	IssueID       string `json:"issue_id,omitempty"`
	PlannerTaskID string `json:"planner_task_id,omitempty"`
}

// StartCEOCommand creates an ad-hoc orchestrated workflow and immediately
// triggers it. This is the single entry-point for the CEO command API.
func (s *WorkflowService) StartCEOCommand(
	ctx context.Context,
	workspaceID, userID, ceoAgentID pgtype.UUID,
	message string,
	skipReview bool,
) (*CEOCommandResult, error) {
	// Truncate message for the workflow name (max 60 chars).
	name := message
	if len(name) > 60 {
		name = name[:60] + "…"
	}

	// 1. Create an ad-hoc orchestrated workflow record.
	workflow, err := s.Queries.CreateWorkflow(ctx, db.CreateWorkflowParams{
		WorkspaceID:    workspaceID,
		Name:           "[CEO] " + name,
		Description:    message,
		Steps:          []byte("[]"),
		CreatedBy:      userID,
		Mode:           "orchestrated",
		PlannerAgentID: ceoAgentID,
		IsCeoCommand:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("create ad-hoc workflow: %w", err)
	}

	slog.Info("CEO command: ad-hoc workflow created",
		"workflow_id", util.UUIDToString(workflow.ID),
		"agent_id", util.UUIDToString(ceoAgentID),
	)

	// 2. Trigger the orchestrated workflow (reuse existing planner flow).
	run, err := s.startOrchestratedWorkflow(ctx, workflow, "command", userID, skipReview)
	if err != nil {
		return nil, fmt.Errorf("start orchestrated workflow: %w", err)
	}

	result := &CEOCommandResult{
		WorkflowID:    util.UUIDToString(workflow.ID),
		WorkflowRunID: util.UUIDToString(run.ID),
	}
	if run.IssueID.Valid {
		result.IssueID = util.UUIDToString(run.IssueID)
	}
	if run.PlannerTaskID.Valid {
		result.PlannerTaskID = util.UUIDToString(run.PlannerTaskID)
	}

	return result, nil
}

func (s *WorkflowService) broadcastStepEvent(ctx context.Context, stepRun db.WorkflowStepRun) {
	run, err := s.Queries.GetWorkflowRun(ctx, stepRun.WorkflowRunID)
	if err != nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventWorkflowStepUpdated,
		WorkspaceID: util.UUIDToString(run.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"step_run": map[string]any{
				"id":              util.UUIDToString(stepRun.ID),
				"workflow_run_id": util.UUIDToString(stepRun.WorkflowRunID),
				"step_index":      stepRun.StepIndex,
				"step_type":       stepRun.StepType,
				"status":          stepRun.Status,
			},
		},
	})
}
