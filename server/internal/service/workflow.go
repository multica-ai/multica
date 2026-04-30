// Package service: WorkflowEngine — a unified engine that advances both
// agent workflows and (future) team workflows in response to bus events.
//
// # Why one engine
//
// Team workflows and agent workflows are the same concept at different
// granularities: a sequence of steps with explicit handoffs. A team
// workflow hands off between roles (each role optionally backed by an
// agent); an agent workflow hands off between steps owned by a single
// agent, possibly with human gates in between.
//
// Both flavors are advanced by the same kinds of events:
//
//   - task:completed   — an agent finished a step
//   - issue:resolved   — a human resolved an approval gate (agent only today)
//
// Keeping a single engine means there's one place that owns "what happens
// next" and one observable surface for status, retries, and audit.
//
// # Today's scope
//
// This file initially lands the engine subscriber, the agent_workflow run
// state machine, and a stub for the team-workflow path. Once team
// workflows have schema + queries to read from, the team-side dispatch
// fills in alongside the agent-side without changing the engine's
// surface. See docs/sebai-builder-tax-filing-design.md §8 in the Sebai
// fork for the full design context this contribution lands.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// WorkflowEngine listens for task and issue events on the bus and advances
// any in-flight workflow runs they correspond to.
//
// The engine is event-sourced: it never polls. Each Subscribe call below
// is the engine's only input edge. The output edges are:
//
//   - INSERT INTO agent_task_queue   (when dispatching the next agent step)
//   - UPDATE agent_workflow_run      (status / current_step / state /
//                                     last_task_id / last_issue_id)
//   - bus.Publish("team:workflow_blocked")  (team-workflow only;
//                                            currently a stub)
type WorkflowEngine struct {
	Pool        *pgxpool.Pool
	TaskService *TaskService
	Bus         *events.Bus
	Logger      *slog.Logger

	// agentStore is the data-access surface for the agent-workflow path.
	// Production callers leave this nil and the engine builds a pgxStore
	// from Pool. Tests may inject a fake via SetAgentStore to drive the
	// engine without a real database.
	agentStore agentWorkflowStore
}

// NewWorkflowEngine constructs the engine and subscribes it to
// task:completed + issue:resolved events.
//
// pool may be nil only in tests that swap in a fake store via
// SetAgentStore; production callers must pass a connected pgxpool.Pool.
func NewWorkflowEngine(
	pool *pgxpool.Pool,
	taskSvc *TaskService,
	bus *events.Bus,
	logger *slog.Logger,
) *WorkflowEngine {
	if logger == nil {
		logger = slog.Default()
	}
	engine := &WorkflowEngine{
		Pool:        pool,
		TaskService: taskSvc,
		Bus:         bus,
		Logger:      logger,
	}
	if pool != nil {
		engine.agentStore = &pgxAgentWorkflowStore{pool: pool}
	}
	if bus != nil {
		bus.Subscribe(protocol.EventTaskCompleted, engine.handleTaskCompleted)
		bus.Subscribe(protocol.EventIssueResolved, engine.handleIssueResolved)
	}
	return engine
}

// SetAgentStore swaps the agent-workflow data-access surface. Used by
// tests to inject a fake; production code does not call this.
func (e *WorkflowEngine) SetAgentStore(s agentWorkflowStore) {
	e.agentStore = s
}

// ---------------------------------------------------------------------------
// Event handlers
// ---------------------------------------------------------------------------

// handleTaskCompleted dispatches a task:completed event to whichever
// workflow path applies (team or agent). A single task can only belong to
// one workflow at a time, so the two paths are mutually exclusive at the
// per-event level.
func (e *WorkflowEngine) handleTaskCompleted(evt events.Event) {
	payload, ok := evt.Payload.(map[string]any)
	if !ok {
		return
	}

	// Team-workflow path: payload carries explicit team_id + role_id. This
	// is the existing intent of the engine and lands as a stub here — once
	// team_workflow has schema + queries upstream, fill in advanceTeam
	// alongside the agent path without changing the engine's interface.
	teamID, _ := payload["team_id"].(string)
	roleID, _ := payload["role_id"].(string)
	if teamID != "" && roleID != "" {
		e.advanceTeam(context.Background(), evt.WorkspaceID, teamID, roleID)
		return
	}

	// Agent-workflow path: lookup by workflow_run_id (preferred) or by
	// task_id (fall back; correlated via agent_workflow_run.last_task_id).
	runID, _ := payload["workflow_run_id"].(string)
	taskID, _ := payload["task_id"].(string)
	if runID == "" && taskID == "" {
		return
	}
	if e.agentStore == nil {
		return
	}

	ctx := context.Background()
	run, err := e.agentStore.FindRun(ctx, runID, taskID, "")
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Not an agent-workflow task — silent no-op.
			return
		}
		e.Logger.Warn("workflow engine: agent run lookup failed",
			"task_id", taskID, "run_id", runID, "error", err)
		return
	}
	e.advanceAgent(ctx, run, "")
}

// handleIssueResolved is the bus handler for issue:resolved events. Only
// the agent-workflow path consumes these today — team workflows have no
// human-gate concept yet.
func (e *WorkflowEngine) handleIssueResolved(evt events.Event) {
	payload, ok := evt.Payload.(map[string]any)
	if !ok {
		return
	}

	resolution, _ := payload["resolution"].(string)
	if resolution != "approved" && resolution != "rejected" {
		return
	}

	runID, _ := payload["workflow_run_id"].(string)
	issueID, _ := payload["issue_id"].(string)
	if runID == "" && issueID == "" {
		return
	}
	if e.agentStore == nil {
		return
	}

	ctx := context.Background()
	run, err := e.agentStore.FindRun(ctx, runID, "", issueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		e.Logger.Warn("workflow engine: agent run lookup failed",
			"issue_id", issueID, "run_id", runID, "error", err)
		return
	}
	e.advanceAgent(ctx, run, resolution)
}

// ---------------------------------------------------------------------------
// Agent workflow path
// ---------------------------------------------------------------------------

// agentWorkflowStore is the data-access surface the engine's agent path
// needs. The production implementation is pgxAgentWorkflowStore (below);
// tests substitute a fake to drive the engine without a real database.
type agentWorkflowStore interface {
	// FindRun resolves an agent_workflow_run + its workflow definition.
	// At most one of runID / taskID / issueID is non-empty; the
	// implementation looks up the run by the first non-empty field.
	// Returns pgx.ErrNoRows when nothing matches.
	FindRun(ctx context.Context, runID, taskID, issueID string) (*agentWorkflowRun, error)

	// CreateAgentTask inserts a queued agent_task_queue row for the
	// run's agent and returns the new task id. The instructions string
	// is stored in agent_task_queue.context alongside workflow_run_id.
	CreateAgentTask(ctx context.Context, run *agentWorkflowRun, instructions string) (string, error)

	// UpdateRun persists status / current_step / state.
	UpdateRun(ctx context.Context, runID, status, step string, state map[string]any) error

	// MarkDone flips the run's status to 'done'.
	MarkDone(ctx context.Context, runID string) error

	// MarkFailed flips the run's status to 'failed' and stashes the
	// reason on the row's `error` column.
	MarkFailed(ctx context.Context, runID, reason string) error

	// SetRunCurrentTask writes the given task id onto last_task_id
	// (clearing last_issue_id).
	SetRunCurrentTask(ctx context.Context, runID, taskID string) error

	// SetRunCurrentIssue writes the given issue id onto last_issue_id
	// (clearing last_task_id). Used by callers that open an approval
	// issue for the run.
	SetRunCurrentIssue(ctx context.Context, runID, issueID string) error
}

// workflowStep mirrors the JSON shape stored under
// agent_workflow.definition->steps. All fields are optional —
// actor='agent' steps use tool/skill+next; actor='human' steps use
// gate+next_on_approve+next_on_reject.
type workflowStep struct {
	ID            string `json:"id"`
	Actor         string `json:"actor"`           // "agent" | "human"
	Tool          string `json:"tool,omitempty"`  // e.g. "service.fetch_x"
	Skill         string `json:"skill,omitempty"` // skill name to apply
	Gate          string `json:"gate,omitempty"`  // e.g. "issue_approval"
	Next          string `json:"next,omitempty"`  // empty / null = terminal
	NextOnApprove string `json:"next_on_approve,omitempty"`
	NextOnReject  string `json:"next_on_reject,omitempty"`
}

// agentWorkflowRun is the row shape used internally by the engine.
type agentWorkflowRun struct {
	ID          string
	WorkflowID  string
	WorkspaceID string
	AgentID     string
	Status      string
	CurrentStep string
	State       map[string]any
	Definition  map[string]any // raw definition JSON from agent_workflow
}

// advanceAgent picks the next step (based on the current step + resolution
// if any) and dispatches it. An empty next pointer marks the run done.
func (e *WorkflowEngine) advanceAgent(ctx context.Context, run *agentWorkflowRun, resolution string) {
	steps, err := parseSteps(run.Definition)
	if err != nil {
		e.markFailed(ctx, run.ID, fmt.Sprintf("definition parse: %v", err))
		return
	}

	current := findStep(steps, run.CurrentStep)
	if current == nil {
		e.markFailed(ctx, run.ID, fmt.Sprintf("current step %q not found in definition", run.CurrentStep))
		return
	}

	var nextID string
	switch {
	case current.Actor == "human" && resolution == "approved":
		nextID = current.NextOnApprove
	case current.Actor == "human" && resolution == "rejected":
		nextID = current.NextOnReject
	default:
		nextID = current.Next
	}

	if nextID == "" {
		e.markDone(ctx, run.ID)
		return
	}

	nextStep := findStep(steps, nextID)
	if nextStep == nil {
		e.markFailed(ctx, run.ID, fmt.Sprintf("next step %q not found in definition", nextID))
		return
	}

	e.dispatchAgent(ctx, run, nextStep)
}

// dispatchAgent executes one step. For agent-actor steps it builds an
// instruction string, creates an agent_task, and updates the run. For
// human-actor steps (gates) it just parks the run in awaiting_approval —
// the prior step is expected to have already opened the approval issue
// via a domain-specific tool and called SetRunCurrentIssue.
func (e *WorkflowEngine) dispatchAgent(ctx context.Context, run *agentWorkflowRun, step *workflowStep) {
	if e.agentStore == nil {
		return
	}
	if step.Actor == "human" {
		// Human gates are pass-through. The step that PRECEDED this one
		// in the workflow is responsible for opening the approval issue
		// and calling SetRunCurrentIssue so an inbound issue:resolved
		// event can be correlated back to this run.
		if err := e.agentStore.UpdateRun(ctx, run.ID, "awaiting_approval", step.ID, run.State); err != nil {
			e.Logger.Warn("workflow engine: park failed", "run_id", run.ID, "error", err)
		}
		return
	}

	// actor == "agent" — build instructions, create a task, advance.
	instructions := buildAgentInstruction(step, run)

	taskID, err := e.agentStore.CreateAgentTask(ctx, run, instructions)
	if err != nil {
		e.Logger.Warn("workflow engine: dispatch failed",
			"run_id", run.ID, "step", step.ID, "error", err)
		e.markFailed(ctx, run.ID, fmt.Sprintf("dispatch step %s: %v", step.ID, err))
		return
	}

	state := cloneState(run.State)
	if err := e.agentStore.UpdateRun(ctx, run.ID, "running", step.ID, state); err != nil {
		e.Logger.Warn("workflow engine: state update failed",
			"run_id", run.ID, "step", step.ID, "error", err)
	}
	if err := e.agentStore.SetRunCurrentTask(ctx, run.ID, taskID); err != nil {
		e.Logger.Warn("workflow engine: last_task_id update failed",
			"run_id", run.ID, "step", step.ID, "error", err)
	}
}

// buildAgentInstruction renders the natural-language instructions the
// daemon will pipe into the agent runtime for one workflow step.
//
// The shape is intentionally generic — domain-specific arguments are
// pulled from run.State rather than hard-coded so the engine has no
// knowledge of any particular workflow's semantics.
func buildAgentInstruction(step *workflowStep, run *agentWorkflowRun) string {
	switch {
	case step.Tool != "":
		return fmt.Sprintf(
			"Workflow step %q: call MCP tool %s. "+
				"workflow_run_id=%s. Use the workflow run state for arguments. "+
				"On completion, the workflow engine will advance to the next step.",
			step.ID, step.Tool, run.ID,
		)
	case step.Skill != "":
		return fmt.Sprintf(
			"Workflow step %q: apply skill %s. workflow_run_id=%s.",
			step.ID, step.Skill, run.ID,
		)
	default:
		return fmt.Sprintf(
			"Workflow step %q (no tool / skill specified). "+
				"workflow_run_id=%s. Inspect the run state and decide.",
			step.ID, run.ID,
		)
	}
}

func (e *WorkflowEngine) markDone(ctx context.Context, runID string) {
	if e.agentStore == nil {
		return
	}
	if err := e.agentStore.MarkDone(ctx, runID); err != nil {
		e.Logger.Warn("workflow engine: markDone failed", "run_id", runID, "error", err)
	}
}

func (e *WorkflowEngine) markFailed(ctx context.Context, runID, reason string) {
	e.Logger.Warn("workflow engine: marking run failed", "run_id", runID, "reason", reason)
	if e.agentStore == nil {
		return
	}
	if err := e.agentStore.MarkFailed(ctx, runID, reason); err != nil {
		e.Logger.Warn("workflow engine: markFailed update failed", "run_id", runID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Team workflow path (stub — fills in once team_workflow lands upstream)
// ---------------------------------------------------------------------------

// advanceTeam is the team-workflow analog of advanceAgent. Today's
// payload carries team_id + role_id; the engine should:
//
//  1. Load the team and its workflow definition.
//  2. Find the workflow step where FromRole matches the completed role.
//  3. Check the step condition (always, on_approval, on_finding, manual).
//  4. If the next role has an assigned agent, create the next task.
//  5. If the next role is unassigned, emit a team:workflow_blocked event.
//
// Until team_workflow has schema + queries upstream, this is a logging
// stub. The interface is intentionally fixed so the agent path doesn't
// have to change when teams arrive.
func (e *WorkflowEngine) advanceTeam(ctx context.Context, workspaceID, teamID, completedRoleID string) {
	_ = ctx
	e.Logger.Info("workflow engine: team-advance stub — team workflow schema not yet upstream",
		"workspace_id", workspaceID,
		"team_id", teamID,
		"completed_role_id", completedRoleID,
	)
}

// EmitTeamWorkflowBlocked publishes a team:workflow_blocked event when
// the next role in a team workflow has no assigned agent. Exposed so the
// future team-workflow advance loop can call it without re-implementing
// the publish shape.
func (e *WorkflowEngine) EmitTeamWorkflowBlocked(workspaceID, teamID, roleName, reason string) {
	if e.Bus == nil {
		return
	}
	e.Bus.Publish(events.Event{
		Type:        "team:workflow_blocked",
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"team_id":   teamID,
			"role_name": roleName,
			"reason":    reason,
		},
	})
	e.Logger.Warn("workflow blocked",
		"team_id", teamID,
		"role_name", roleName,
		"reason", reason,
	)
}

// ---------------------------------------------------------------------------
// Helpers — parsing + stepwalking
// ---------------------------------------------------------------------------

// parseSteps unmarshals the steps[] array out of a workflow definition
// JSON blob. Returns an error if the shape doesn't look right.
func parseSteps(definition map[string]any) ([]workflowStep, error) {
	raw, ok := definition["steps"]
	if !ok {
		return nil, errors.New("definition missing 'steps' array")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("re-marshal steps: %w", err)
	}
	var steps []workflowStep
	if err := json.Unmarshal(encoded, &steps); err != nil {
		return nil, fmt.Errorf("unmarshal steps: %w", err)
	}
	return steps, nil
}

func findStep(steps []workflowStep, id string) *workflowStep {
	for i := range steps {
		if steps[i].ID == id {
			return &steps[i]
		}
	}
	return nil
}

func cloneState(state map[string]any) map[string]any {
	out := make(map[string]any, len(state))
	for k, v := range state {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Production data-access implementation
// ---------------------------------------------------------------------------

// pgxAgentWorkflowStore is the production implementation of
// agentWorkflowStore. It uses raw SQL via pgxpool against
// agent_workflow / agent_workflow_run / agent_task_queue.
type pgxAgentWorkflowStore struct {
	pool *pgxpool.Pool
}

const pgxAgentStoreFindRunSelect = `
	SELECT r.id::text, r.workflow_id::text, r.workspace_id::text,
	       w.agent_id::text, r.status, r.current_step, r.state,
	       w.definition
	FROM agent_workflow_run r
	JOIN agent_workflow w ON w.id = r.workflow_id
`

func (s *pgxAgentWorkflowStore) FindRun(ctx context.Context, runID, taskID, issueID string) (*agentWorkflowRun, error) {
	// Casts use ::uuid so a caller-supplied UUID-string compares cleanly
	// against the UUID column.
	var row pgx.Row
	switch {
	case runID != "":
		row = s.pool.QueryRow(ctx, pgxAgentStoreFindRunSelect+" WHERE r.id = $1::uuid", runID)
	case taskID != "":
		row = s.pool.QueryRow(ctx,
			pgxAgentStoreFindRunSelect+" WHERE r.last_task_id = $1::uuid", taskID)
	case issueID != "":
		row = s.pool.QueryRow(ctx,
			pgxAgentStoreFindRunSelect+" WHERE r.last_issue_id = $1::uuid", issueID)
	default:
		return nil, pgx.ErrNoRows
	}

	var (
		run        agentWorkflowRun
		stateBytes []byte
		defBytes   []byte
	)
	if err := row.Scan(
		&run.ID, &run.WorkflowID, &run.WorkspaceID,
		&run.AgentID, &run.Status, &run.CurrentStep,
		&stateBytes, &defBytes,
	); err != nil {
		return nil, err
	}

	run.State = map[string]any{}
	if len(stateBytes) > 0 {
		_ = json.Unmarshal(stateBytes, &run.State)
	}
	run.Definition = map[string]any{}
	if len(defBytes) > 0 {
		_ = json.Unmarshal(defBytes, &run.Definition)
	}
	return &run, nil
}

// CreateAgentTask inserts a row in agent_task_queue for the run's agent.
// Raw SQL (rather than db.Queries.CreateAgentTask) so the engine can
// stash workflow_run_id + step instructions in the task.context JSONB in
// a single statement. Returns the new task id.
func (s *pgxAgentWorkflowStore) CreateAgentTask(ctx context.Context, run *agentWorkflowRun, instructions string) (string, error) {
	taskCtx, err := json.Marshal(map[string]any{
		"workflow_run_id": run.ID,
		"workflow_id":     run.WorkflowID,
		"step":            run.CurrentStep,
		"instructions":    instructions,
	})
	if err != nil {
		return "", fmt.Errorf("marshal task context: %w", err)
	}

	const insertSQL = `
		INSERT INTO agent_task_queue
		    (agent_id, runtime_id, status, priority, context)
		SELECT a.id, a.runtime_id, 'queued', 2, $2::jsonb
		FROM agent a
		WHERE a.id = $1
		RETURNING id::text
	`

	var taskID string
	if err := s.pool.QueryRow(ctx, insertSQL, run.AgentID, taskCtx).Scan(&taskID); err != nil {
		return "", fmt.Errorf("insert agent_task_queue: %w", err)
	}
	return taskID, nil
}

func (s *pgxAgentWorkflowStore) UpdateRun(ctx context.Context, runID, status, step string, state map[string]any) error {
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	const sql = `
		UPDATE agent_workflow_run
		SET status = $2, current_step = $3, state = $4::jsonb, updated_at = now()
		WHERE id = $1
	`
	_, err = s.pool.Exec(ctx, sql, runID, status, step, stateBytes)
	return err
}

func (s *pgxAgentWorkflowStore) MarkDone(ctx context.Context, runID string) error {
	const sql = `UPDATE agent_workflow_run SET status='done', updated_at=now() WHERE id=$1`
	_, err := s.pool.Exec(ctx, sql, runID)
	return err
}

func (s *pgxAgentWorkflowStore) MarkFailed(ctx context.Context, runID, reason string) error {
	const sql = `
		UPDATE agent_workflow_run
		SET status='failed',
		    error = $2,
		    updated_at = now()
		WHERE id=$1
	`
	_, err := s.pool.Exec(ctx, sql, runID, reason)
	return err
}

// SetRunCurrentTask writes the given task id onto last_task_id and
// clears last_issue_id (a run is correlated by exactly one of the two
// at a time).
func (s *pgxAgentWorkflowStore) SetRunCurrentTask(ctx context.Context, runID, taskID string) error {
	const sql = `
		UPDATE agent_workflow_run
		SET last_task_id = $2::uuid,
		    last_issue_id = NULL,
		    updated_at = now()
		WHERE id = $1::uuid
	`
	_, err := s.pool.Exec(ctx, sql, runID, taskID)
	return err
}

// SetRunCurrentIssue writes the given issue id onto last_issue_id and
// clears last_task_id.
func (s *pgxAgentWorkflowStore) SetRunCurrentIssue(ctx context.Context, runID, issueID string) error {
	const sql = `
		UPDATE agent_workflow_run
		SET last_issue_id = $2::uuid,
		    last_task_id = NULL,
		    updated_at = now()
		WHERE id = $1::uuid
	`
	_, err := s.pool.Exec(ctx, sql, runID, issueID)
	return err
}
