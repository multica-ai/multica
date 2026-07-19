package loops

// dispatch.go is the egress half of the loop check transport: it sends an
// enqueued check out to the worker agent's runtime as a task so the runtime can
// run the argv in the checkout where the build happened and (a later slice)
// report the exit code back. It mirrors the workflow engine's
// actionRunSkill -> CreateQuickCreateTask pattern: a quick_create task whose
// context carries a prompt plus the structured loop_check fields, so no
// daemon-side change is needed to run it today while the structured fields pin
// the contract a deterministic daemon handler can target later.
//
// Dispatch never weakens the gate. The gate still decides only on the exit code
// the runtime reports — never the agent's opinion — and each check is sent
// exactly once (Store.PendingDispatch / MarkDispatched), so a gate that is
// re-evaluated while checks are in flight does not re-send them.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CheckDispatch is one check the engine asks a runtime to run.
type CheckDispatch struct {
	AgentID       string   // worker agent whose runtime runs the check
	IssueID       string   // issue the loop is building
	Gate          string   // stable per-gate key (the workflow id)
	Round         int32    // loop round the check belongs to
	Argv          []string // the check command, argv form (never a shell string)
	Model         string
	ThinkingLevel string
}

// CheckDispatcher sends a single enqueued check out to a runtime. It is the
// seam GateEvaluator calls when a gate enqueues new checks; the concrete
// TaskDispatcher enqueues an agent task. Optional and nil-safe on the
// evaluator: without it the gate only records pending checks (the pre-dispatch
// behavior).
//
// DispatchRevision is the parallel seam for a GateRevise decision: instead of
// running one check command, it re-dispatches the worker's build skill so the
// agent can fix whatever failed. Optional the same way — a nil dispatcher (or
// a config missing an agent/skill) leaves the caps tracker updated but does
// not re-dispatch anything.
//
// DispatchJudge is the seam for a judge-type check: instead of a runnable
// command, it sends the judge agent the rubric and asks for a structured
// pass/revise verdict. It is sent to cfg.JudgeAgentID (or cfg.AgentID as a
// self-graded fallback), never assumed to be the same session as the worker
// — the gate does not enforce blindness, but the caller should route a real
// judge agent here for the verdict to mean anything.
// DispatchHuman is the seam for a human-type check with an agent assignee:
// instead of a runnable command or a rubric, it asks the agent to review the
// delivered work and report an explicit approve/reject decision (via
// report_loop_human) — standing in for a person's sign-off. A member (person)
// assignee has no dispatch egress at all: they act through the
// approve-human-check HTTP endpoint when they choose to, so DispatchHuman is
// only ever called for an agent assignee (see GateEvaluator.dispatchPendingHuman).
type CheckDispatcher interface {
	DispatchCheck(ctx context.Context, d CheckDispatch) error
	DispatchRevision(ctx context.Context, d RevisionDispatch) error
	DispatchJudge(ctx context.Context, d JudgeDispatch) error
	DispatchHuman(ctx context.Context, d HumanDispatch) error
}

// HumanDispatch is one human check the engine asks an agent assignee to
// decide, standing in for a person's sign-off.
type HumanDispatch struct {
	AssigneeType string // AssigneeAgent — this is only sent for an agent assignee
	AssigneeID   string // agent whose runtime reviews and decides
	IssueID      string // issue the loop is building
	Gate         string // stable per-gate key (the workflow id)
	Round        int32  // loop round the check belongs to
	CheckID      string // the verification id from loop.yaml, echoed back on report
	Prompt       string // what the assignee is being asked to approve
}

// JudgeDispatch is one judge check the engine asks a judge agent to score.
type JudgeDispatch struct {
	AgentID       string // judge agent whose runtime scores the check
	IssueID       string // issue the loop is building
	Gate          string // stable per-gate key (the workflow id)
	Round         int32  // loop round the check belongs to
	CheckID       string // the verification id from loop.yaml, echoed back on report
	Rubric        string // what the judge scores the delivered work against
	SkillName     string // the skill dispatched to run the judgment
	Model         string
	ThinkingLevel string
}

// RevisionDispatch is one round's worth of failed checks the engine asks the
// worker agent to fix by re-running its build skill.
type RevisionDispatch struct {
	AgentID       string // worker agent whose runtime re-runs the build
	IssueID       string // issue the loop is building
	Gate          string // stable per-gate key (the workflow id)
	Round         int32  // the new round this revision starts
	SkillName     string // the skill to re-dispatch (Compile sets the build skill)
	Model         string
	ThinkingLevel string
	Failures      []CheckOutcome // the outcomes that triggered this revision
	// PhaseLabel names the build phase this dispatch belongs to (e.g. "Build 2")
	// for a multi-phase loop (FIR-2283 followup point 6). When set, this is a
	// fresh next-phase build (not a fix-what-failed revision) — it is stamped
	// on the task as loop_phase so the run's session is named/badged for the
	// phase, and Failures is empty. Empty for a single-phase revision.
	PhaseLabel string
}

// DispatchQueries is the narrow slice of the upstream issue queries the task
// dispatcher needs: resolve the agent (for its runtime + workspace) and enqueue
// the task. Keeping it an interface mirrors workflows.IssueActions and lets the
// dispatcher be unit-tested without a live DB.
type DispatchQueries interface {
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	GetAgentRuntime(ctx context.Context, id pgtype.UUID) (db.AgentRuntime, error)
	CountRunningTasks(ctx context.Context, agentID pgtype.UUID) (int64, error)
	ListAgentSkills(ctx context.Context, agentID pgtype.UUID) ([]db.Skill, error)
	CreateQuickCreateTask(ctx context.Context, arg db.CreateQuickCreateTaskParams) (db.AgentTaskQueue, error)
	SetAgentTaskModelOverride(ctx context.Context, arg db.SetAgentTaskModelOverrideParams) error
	SetAgentTaskThinkingOverride(ctx context.Context, arg db.SetAgentTaskThinkingOverrideParams) error
	// Issue-bound phase dispatch (Tine live-test fix): a next-phase build run
	// is enqueued ON the loop's issue behind a visible kickoff comment, not as
	// a detached quick_create task. Both are satisfied by upstream db.Queries.
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	CreateComment(ctx context.Context, arg db.CreateCommentParams) (db.Comment, error)
	CreateAgentTask(ctx context.Context, arg db.CreateAgentTaskParams) (db.AgentTaskQueue, error)
	CreateInboxItem(ctx context.Context, arg db.CreateInboxItemParams) (db.InboxItem, error)
}

// SessionStamper badges the session thread a phase dispatch opens (name +
// phase on the cerebro_session row keyed by the thread root). Implemented by
// workflows.SessionPhaseStamper; optional and nil-safe on the dispatcher.
type SessionStamper interface {
	StampSession(ctx context.Context, issueID, rootCommentID pgtype.UUID, phase, name string) error
}

// BusyWakeupScheduler schedules the retry requested by a wakeup all-busy
// policy. The chain driver owns the waiting step; the scheduler only arranges
// for the workflow to be re-entered later.
type BusyWakeupScheduler interface {
	ScheduleBusyWakeup(context.Context, BlockDispatch) error
}

// EvalBlockRunner executes an eval through the trusted server-side eval
// engine. The dispatcher never accepts a caller-supplied verdict.
type EvalBlockRunner interface {
	RunEvalBlock(context.Context, BlockDispatch) (StepStatus, json.RawMessage, error)
}

// TaskDispatcher dispatches a check by enqueuing an agent task on the worker
// agent's runtime, reusing the quick_create task shape the daemon already runs.
type TaskDispatcher struct {
	queries DispatchQueries
	stamper SessionStamper
	wakeups BusyWakeupScheduler
	evals   EvalBlockRunner
}

func (t *TaskDispatcher) WithEvalBlockRunner(r EvalBlockRunner) *TaskDispatcher {
	t.evals = r
	return t
}

// NewTaskDispatcher builds a TaskDispatcher over the given issue queries.
func NewTaskDispatcher(queries DispatchQueries) *TaskDispatcher {
	return &TaskDispatcher{queries: queries}
}

// WithSessionStamper plugs in the dispatch-time session badge writer used by
// issue-bound phase dispatches (DispatchRevision with a PhaseLabel).
func (t *TaskDispatcher) WithSessionStamper(s SessionStamper) *TaskDispatcher {
	t.stamper = s
	return t
}

// WithBusyWakeupScheduler plugs in the workflow retry scheduler used by a
// block whose on_all_busy policy is wakeup.
func (t *TaskDispatcher) WithBusyWakeupScheduler(s BusyWakeupScheduler) *TaskDispatcher {
	t.wakeups = s
	return t
}

// DispatchBlock is the block-chain egress. Every block reaches this one seam;
// task-backed blocks open a fresh issue session, while member approvals and
// evals wait for their external outcome without inventing an agent task.
func (t *TaskDispatcher) DispatchBlock(ctx context.Context, d BlockDispatch) (BlockDispatchResult, error) {
	if d.Block.ID == "" {
		return BlockDispatchResult{}, fmt.Errorf("dispatch block: empty block id")
	}
	switch d.Block.Type {
	case BlockEval:
		if t.evals == nil {
			return BlockDispatchResult{}, fmt.Errorf("dispatch block %s: eval runner is not wired", d.Block.ID)
		}
		status, outcome, err := t.evals.RunEvalBlock(ctx, d)
		return BlockDispatchResult{Status: status, Outcome: outcome}, err
	case BlockHuman:
		if d.Block.ApproverType == AssigneeMember {
			if err := t.notifyMember(ctx, d, d.Block.ApproverID, "workflow_human_approval", "Workflow approval needed", d.Block.Prompt); err != nil {
				return BlockDispatchResult{}, err
			}
			outcome, _ := json.Marshal(map[string]any{"approver_id": d.Block.ApproverID})
			return BlockDispatchResult{Status: StepWaiting, Outcome: outcome}, nil
		}
	}

	candidates := d.Block.Agents
	if len(candidates) == 0 {
		candidates = []AgentRef{{AgentID: d.Run.AgentID}}
	}
	if d.Block.Type == BlockHuman && d.Block.ApproverType == AssigneeAgent {
		candidates = []AgentRef{{AgentID: d.Block.ApproverID}}
	}
	if len(candidates) == 0 || candidates[0].AgentID == "" {
		return BlockDispatchResult{}, fmt.Errorf("dispatch block %s: no agent", d.Block.ID)
	}
	runner, agent, found, err := t.firstAvailableRunner(ctx, d.Block, candidates)
	if err != nil {
		return BlockDispatchResult{}, err
	}
	if !found {
		return t.applyAllBusyPolicy(ctx, d)
	}
	prompt, err := buildBlockPrompt(d.Block)
	if err != nil {
		return BlockDispatchResult{}, err
	}
	if err := t.dispatchIssueBoundBlock(ctx, d, runner, agent, prompt); err != nil {
		return BlockDispatchResult{}, err
	}
	outcome, _ := json.Marshal(map[string]any{"dispatched": true, "agent_id": runner.AgentID})
	return BlockDispatchResult{Status: StepRunning, Outcome: outcome}, nil
}

func (t *TaskDispatcher) firstAvailableRunner(ctx context.Context, block Block, candidates []AgentRef) (AgentRef, db.Agent, bool, error) {
	for _, runner := range candidates {
		agentID, err := util.ParseUUID(runner.AgentID)
		if err != nil {
			return AgentRef{}, db.Agent{}, false, fmt.Errorf("dispatch block: parse agent id: %w", err)
		}
		agent, err := t.queries.GetAgent(ctx, agentID)
		if err != nil {
			return AgentRef{}, db.Agent{}, false, fmt.Errorf("dispatch block: load agent: %w", err)
		}
		if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
			continue
		}
		runtime, err := t.queries.GetAgentRuntime(ctx, agent.RuntimeID)
		if err != nil {
			return AgentRef{}, db.Agent{}, false, fmt.Errorf("dispatch block: load agent runtime: %w", err)
		}
		if runtime.Status != "online" {
			continue
		}
		running, err := t.queries.CountRunningTasks(ctx, agentID)
		if err != nil {
			return AgentRef{}, db.Agent{}, false, fmt.Errorf("dispatch block: count running tasks: %w", err)
		}
		limit := agent.MaxConcurrentTasks
		if limit <= 0 {
			limit = 1
		}
		if running >= int64(limit) {
			continue
		}
		if block.Skill != "" {
			skills, err := t.queries.ListAgentSkills(ctx, agentID)
			if err != nil {
				return AgentRef{}, db.Agent{}, false, fmt.Errorf("dispatch block: list agent skills: %w", err)
			}
			if !hasAttachedSkill(skills, block.Skill) {
				return AgentRef{}, db.Agent{}, false, fmt.Errorf("dispatch block: agent does not have skill %q attached", block.Skill)
			}
		}
		return runner, agent, true, nil
	}
	return AgentRef{}, db.Agent{}, false, nil
}

func hasAttachedSkill(skills []db.Skill, name string) bool {
	for _, skill := range skills {
		if skill.Name == name {
			return true
		}
	}
	return false
}

func (t *TaskDispatcher) applyAllBusyPolicy(ctx context.Context, d BlockDispatch) (BlockDispatchResult, error) {
	policy := d.Block.OnAllBusy
	if policy == "" {
		policy = BusyWait
	}
	outcome := map[string]any{"policy": policy, "all_agents_busy": true}
	status := StepWaiting
	switch policy {
	case BusyWait:
		outcome["max_wait_seconds"] = d.Phase.Limits.MaxWaitSeconds
		if d.Phase.Limits.MaxWaitSeconds > 0 && !d.Step.CreatedAt.IsZero() && time.Since(d.Step.CreatedAt) >= time.Duration(d.Phase.Limits.MaxWaitSeconds)*time.Second {
			outcome["error"] = fmt.Sprintf("max_wait_seconds exceeded (%d)", d.Phase.Limits.MaxWaitSeconds)
			status = StepFailed
		} else {
			status = StepPending
		}
	case BusyPause:
		outcome["paused"] = true
	case BusyWakeup:
		if t.wakeups == nil {
			return BlockDispatchResult{}, fmt.Errorf("dispatch block %s: wakeup policy needs a scheduler", d.Block.ID)
		}
		if err := t.wakeups.ScheduleBusyWakeup(ctx, d); err != nil {
			return BlockDispatchResult{}, fmt.Errorf("dispatch block %s: schedule busy wakeup: %w", d.Block.ID, err)
		}
		outcome["wakeup"] = true
		status = StepPending
	case BusyPingMember:
		issue, err := t.queries.GetIssue(ctx, d.Run.IssueID)
		if err != nil {
			return BlockDispatchResult{}, fmt.Errorf("dispatch block: load issue for busy notification: %w", err)
		}
		recipientID := issue.CreatorID
		if issue.AssigneeType.Valid && issue.AssigneeType.String == AssigneeMember && issue.AssigneeID.Valid {
			recipientID = issue.AssigneeID
		} else if issue.CreatorType != AssigneeMember || !issue.CreatorID.Valid {
			return BlockDispatchResult{}, fmt.Errorf("dispatch block %s: ping_member needs an issue with a member owner", d.Block.ID)
		}
		if err := t.notifyMember(ctx, d, util.UUIDToString(recipientID), "workflow_agents_busy", "Workflow is waiting for an agent", "Every configured agent is currently busy."); err != nil {
			return BlockDispatchResult{}, err
		}
		outcome["notified"] = true
	default:
		return BlockDispatchResult{}, fmt.Errorf("dispatch block %s: unsupported all-busy policy %q", d.Block.ID, policy)
	}
	raw, _ := json.Marshal(outcome)
	return BlockDispatchResult{Status: status, Outcome: raw}, nil
}

func (t *TaskDispatcher) notifyMember(ctx context.Context, d BlockDispatch, memberID, notificationType, title, body string) error {
	recipientID, err := util.ParseUUID(memberID)
	if err != nil {
		return fmt.Errorf("dispatch block: parse notification member id: %w", err)
	}
	issue, err := t.queries.GetIssue(ctx, d.Run.IssueID)
	if err != nil {
		return fmt.Errorf("dispatch block: load issue for notification: %w", err)
	}
	details, _ := json.Marshal(map[string]any{
		"workflow_id": util.UUIDToString(d.Run.WorkflowID),
		"phase_id":    d.Phase.ID,
		"block_id":    d.Block.ID,
		"step_number": d.Step.Number,
	})
	if _, err := t.queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   issue.WorkspaceID,
		RecipientType: AssigneeMember,
		RecipientID:   recipientID,
		Type:          notificationType,
		Severity:      "action_required",
		IssueID:       issue.ID,
		Title:         title,
		Body:          pgtype.Text{String: body, Valid: body != ""},
		ActorType:     pgtype.Text{String: "system", Valid: true},
		Details:       details,
		Route:         "inbox",
	}); err != nil {
		return fmt.Errorf("dispatch block: create member notification: %w", err)
	}
	return nil
}

func (t *TaskDispatcher) dispatchIssueBoundBlock(ctx context.Context, d BlockDispatch, runner AgentRef, agent db.Agent, prompt string) error {
	agentID, err := util.ParseUUID(runner.AgentID)
	if err != nil {
		return fmt.Errorf("dispatch block: parse agent id: %w", err)
	}
	issue, err := t.queries.GetIssue(ctx, d.Run.IssueID)
	if err != nil {
		return fmt.Errorf("dispatch block: load issue: %w", err)
	}
	comment, err := t.queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    agentID,
		Content:     prompt,
		Type:        "comment",
		ParentID:    pgtype.UUID{},
	})
	if err != nil {
		return fmt.Errorf("dispatch block: create kickoff comment: %w", err)
	}

	contextJSON, err := json.Marshal(map[string]any{
		"type":                     "workflow_block",
		"workflow_target_issue_id": util.UUIDToString(issue.ID),
		"workflow_skill_name":      d.Block.Skill,
		"loop_step": map[string]any{
			"workflow_id":  util.UUIDToString(d.Run.WorkflowID),
			"phase_id":     d.Phase.ID,
			"block_id":     d.Block.ID,
			"step_number":  d.Step.Number,
			"block_type":   d.Block.Type,
			"steps":        d.Block.Steps,
			"phase_limits": d.Phase.Limits,
		},
	})
	if err != nil {
		return fmt.Errorf("dispatch block: marshal context: %w", err)
	}
	name := d.Block.Name
	if name == "" {
		name = d.Block.ID
	}
	task, err := t.queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		AgentID:           agentID,
		RuntimeID:         agent.RuntimeID,
		IssueID:           issue.ID,
		Priority:          0,
		TriggerCommentID:  comment.ID,
		Context:           contextJSON,
		TriggerSummary:    pgtype.Text{String: name, Valid: true},
		Title:             pgtype.Text{String: name + ": " + issue.Title, Valid: true},
		ForceFreshSession: pgtype.Bool{Bool: true, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("dispatch block: enqueue task: %w", err)
	}
	return t.applyTaskOverrides(ctx, task.ID, runner.Model, runner.ThinkingLevel, "dispatch block")
}

func buildBlockPrompt(block Block) (string, error) {
	switch block.Type {
	case BlockSession:
		prompt := fmt.Sprintf("Use the %q skill to run workflow block %q on this issue.", block.Skill, block.ID)
		if block.Goal != "" {
			prompt += "\n\nGoal:\n" + block.Goal
		}
		return appendOpenStepInstruction(prompt, block), nil
	case BlockCommand:
		return appendOpenStepInstruction(fmt.Sprintf("Run this workflow command exactly as given and leave the result on this issue:\n\n    %s", strings.Join(block.Check, " ")), block), nil
	case BlockReview:
		prompt := fmt.Sprintf("Review the actual delivered work on this issue against workflow block %q. Do not accept the builder's summary as proof.", block.ID)
		if block.Skill != "" {
			prompt = fmt.Sprintf("Use the %q skill to review the actual delivered work on this issue against workflow block %q. Do not accept the builder's summary as proof.", block.Skill, block.ID)
		}
		return appendOpenStepInstruction(prompt+"\n\nRubric:\n"+block.Rubric, block), nil
	case BlockHuman:
		return appendOpenStepInstruction("Review the actual delivered work on this issue and make the requested approval decision.\n\nApproval:\n"+block.Prompt, block), nil
	default:
		return "", fmt.Errorf("dispatch block %s: unsupported type %q", block.ID, block.Type)
	}
}

func appendOpenStepInstruction(prompt string, block Block) string {
	if !block.Steps.Allowed {
		return prompt
	}
	return prompt + fmt.Sprintf("\n\nThis block may contain up to %d steps. Call open_loop_step before finishing if the block needs another step of the same kind.", block.Steps.Max)
}

// DispatchCheck enqueues a quick_create task asking the worker agent to run the
// check argv in its checkout and report the exit code back. It resolves the
// agent's runtime the same way actionRunSkill does and fails loudly if the
// agent is archived or has no runtime — a check with nowhere to run must not be
// silently dropped, or the gate would wait forever.
func (t *TaskDispatcher) DispatchCheck(ctx context.Context, d CheckDispatch) error {
	if len(d.Argv) == 0 {
		return fmt.Errorf("dispatch check: empty argv")
	}
	agentID, err := util.ParseUUID(d.AgentID)
	if err != nil {
		return fmt.Errorf("dispatch check: parse agent id: %w", err)
	}
	agent, err := t.queries.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("dispatch check: load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("dispatch check: agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return fmt.Errorf("dispatch check: agent has no runtime")
	}

	contextJSON, err := json.Marshal(map[string]any{
		"type":         "quick_create",
		"prompt":       buildCheckPrompt(d),
		"workspace_id": util.UUIDToString(agent.WorkspaceID),
		// Structured loop_check fields: bookkeeping the daemon ignores today,
		// but the ingress matches a reported exit code back to this check on
		// (issue_id, gate, round, argv).
		"loop_check": map[string]any{
			"issue_id": d.IssueID,
			"gate":     d.Gate,
			"round":    d.Round,
			"argv":     d.Argv,
		},
	})
	if err != nil {
		return fmt.Errorf("dispatch check: marshal context: %w", err)
	}

	task, err := t.queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   agentID,
		RuntimeID: agent.RuntimeID,
		Priority:  0,
		Context:   contextJSON,
	})
	if err != nil {
		return fmt.Errorf("dispatch check: enqueue task: %w", err)
	}
	if err := t.applyTaskOverrides(ctx, task.ID, d.Model, d.ThinkingLevel, "dispatch check"); err != nil {
		return err
	}
	return nil
}

// buildCheckPrompt renders the instruction the worker agent's model sees. The
// gate decides on the reported exit code alone, so the prompt's only job is to
// get the exact argv run and its exit code reported back unchanged.
func buildCheckPrompt(d CheckDispatch) string {
	return fmt.Sprintf(
		"Run this loop verification check in the repository checkout, exactly as given, and report its exit code back to the loop gate (gate %s, round %d) without judging the result yourself:\n\n    %s",
		d.Gate, d.Round, strings.Join(d.Argv, " "),
	)
}

// DispatchRevision enqueues a quick_create task asking the worker agent to
// re-run its build skill and fix the checks that failed, starting a new
// round. It resolves the agent's runtime the same way DispatchCheck does and
// fails loudly rather than silently dropping a revision the caps tracker has
// already counted.
func (t *TaskDispatcher) DispatchRevision(ctx context.Context, d RevisionDispatch) error {
	if d.SkillName == "" {
		return fmt.Errorf("dispatch revision: empty skill name")
	}
	agentID, err := util.ParseUUID(d.AgentID)
	if err != nil {
		return fmt.Errorf("dispatch revision: parse agent id: %w", err)
	}
	agent, err := t.queries.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("dispatch revision: load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("dispatch revision: agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return fmt.Errorf("dispatch revision: agent has no runtime")
	}

	issueID, err := util.ParseUUID(d.IssueID)
	if err != nil {
		return fmt.Errorf("dispatch revision: parse issue id: %w", err)
	}
	issue, err := t.queries.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("dispatch revision: load issue: %w", err)
	}

	// Issue-bound dispatch (Tine live-test fix): the revision / next-phase
	// build runs ON the loop's issue. The kickoff comment opens the session
	// thread the user watches; the task is comment-triggered so the agent's
	// whole run lands on the issue timeline — never a detached quick_create
	// task with no issue link.
	prompt := buildRevisionPrompt(d)
	comment, err := t.queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    agentID,
		Content:     prompt,
		Type:        "comment",
		ParentID:    pgtype.UUID{}, // top-level == new session root
	})
	if err != nil {
		return fmt.Errorf("dispatch revision: create kickoff comment: %w", err)
	}

	// Session name: the phase label for a multi-phase build ("Build 2"),
	// otherwise the revision round ("Build round 3"). Badge is always "build".
	name := d.PhaseLabel
	if name == "" {
		name = fmt.Sprintf("Build round %d", d.Round)
	}
	if t.stamper != nil {
		if err := t.stamper.StampSession(ctx, issue.ID, comment.ID, "build", name); err != nil {
			slog.Warn("dispatch revision: dispatch-time session badge failed",
				"issue_id", d.IssueID, "error", err)
		}
	}

	taskContext := map[string]any{
		"type": "workflow_phase",
		// Structured loop_revision fields, mirroring loop_check: bookkeeping
		// the daemon ignores today, pinned for a later deterministic handler.
		"loop_revision": map[string]any{
			"issue_id": d.IssueID,
			"gate":     d.Gate,
			"round":    d.Round,
		},
		"workflow_target_issue_id": d.IssueID,
		"loop_phase":               "build",
		"loop_session_name":        name,
	}
	contextJSON, err := json.Marshal(taskContext)
	if err != nil {
		return fmt.Errorf("dispatch revision: marshal context: %w", err)
	}

	task, err := t.queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		AgentID:           agentID,
		RuntimeID:         agent.RuntimeID,
		IssueID:           issue.ID,
		Priority:          0,
		TriggerCommentID:  comment.ID,
		Context:           contextJSON,
		TriggerSummary:    pgtype.Text{String: name + " — " + d.SkillName, Valid: true},
		Title:             pgtype.Text{String: name + ": " + issue.Title, Valid: true},
		ForceFreshSession: pgtype.Bool{Bool: true, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("dispatch revision: enqueue task: %w", err)
	}
	if err := t.applyTaskOverrides(ctx, task.ID, d.Model, d.ThinkingLevel, "dispatch revision"); err != nil {
		return err
	}
	return nil
}

// DispatchJudge enqueues a quick_create task asking the judge agent to score
// the delivered work against the check's rubric and report a structured
// verdict back. It resolves the agent's runtime the same way DispatchCheck
// does and fails loudly if the judge agent is archived or has no runtime — a
// judge check with nowhere to run must not be silently dropped, or the gate
// would wait forever.
func (t *TaskDispatcher) DispatchJudge(ctx context.Context, d JudgeDispatch) error {
	if d.Rubric == "" {
		return fmt.Errorf("dispatch judge: empty rubric")
	}
	agentID, err := util.ParseUUID(d.AgentID)
	if err != nil {
		return fmt.Errorf("dispatch judge: parse agent id: %w", err)
	}
	agent, err := t.queries.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("dispatch judge: load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("dispatch judge: agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return fmt.Errorf("dispatch judge: agent has no runtime")
	}

	contextJSON, err := json.Marshal(map[string]any{
		"type":         "quick_create",
		"prompt":       buildJudgePrompt(d),
		"workspace_id": util.UUIDToString(agent.WorkspaceID),
		// Structured loop_judge fields, mirroring loop_check: bookkeeping the
		// daemon ignores today, but the ingress matches a reported verdict
		// back to this check on (issue_id, gate, round, check_id).
		"loop_judge": map[string]any{
			"issue_id": d.IssueID,
			"gate":     d.Gate,
			"round":    d.Round,
			"check_id": d.CheckID,
		},
	})
	if err != nil {
		return fmt.Errorf("dispatch judge: marshal context: %w", err)
	}

	task, err := t.queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   agentID,
		RuntimeID: agent.RuntimeID,
		Priority:  0,
		Context:   contextJSON,
	})
	if err != nil {
		return fmt.Errorf("dispatch judge: enqueue task: %w", err)
	}
	if err := t.applyTaskOverrides(ctx, task.ID, d.Model, d.ThinkingLevel, "dispatch judge"); err != nil {
		return err
	}
	return nil
}

// buildJudgePrompt renders the instruction the judge agent's model sees. The
// gate decides on the reported structured verdict alone, so the prompt makes
// the rubric the only thing that matters and tells the judge to read the
// issue's own delivered work (not to trust a worker's summary of it) before
// scoring — the same "blind, evidence-based" standard the delivery gate
// programmatic checks get from a raw exit code.
func buildJudgePrompt(d JudgeDispatch) string {
	return fmt.Sprintf(
		"Score this loop delivery against the rubric below (gate %s, round %d, check %q). "+
			"Read the actual delivered work on the issue yourself — do not take the builder's own summary as proof. "+
			"Then report your verdict back to the loop gate via the report_loop_judge tool with issue_id, gate, round, check_id=%q, "+
			"pass (true only if the rubric is fully met), and blocking_issues (the specific, concrete reasons if pass is false; empty if true).\n\n"+
			"Rubric:\n%s",
		d.Gate, d.Round, d.CheckID, d.CheckID, d.Rubric,
	)
}

// DispatchHuman enqueues a quick_create task asking an agent assignee to
// review the delivered work and report an explicit approve/reject decision,
// standing in for a person's sign-off. Only ever called for an agent
// assignee — a member (person) assignee has no dispatch egress, see
// HumanDispatch's doc comment. Fails loudly the same way DispatchJudge does
// rather than silently dropping a check the gate is waiting on.
func (t *TaskDispatcher) DispatchHuman(ctx context.Context, d HumanDispatch) error {
	if d.Prompt == "" {
		return fmt.Errorf("dispatch human: empty prompt")
	}
	agentID, err := util.ParseUUID(d.AssigneeID)
	if err != nil {
		return fmt.Errorf("dispatch human: parse assignee id: %w", err)
	}
	agent, err := t.queries.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("dispatch human: load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("dispatch human: agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return fmt.Errorf("dispatch human: agent has no runtime")
	}

	contextJSON, err := json.Marshal(map[string]any{
		"type":         "quick_create",
		"prompt":       buildHumanPrompt(d),
		"workspace_id": util.UUIDToString(agent.WorkspaceID),
		"loop_human": map[string]any{
			"issue_id": d.IssueID,
			"gate":     d.Gate,
			"round":    d.Round,
			"check_id": d.CheckID,
		},
	})
	if err != nil {
		return fmt.Errorf("dispatch human: marshal context: %w", err)
	}

	if _, err := t.queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   agentID,
		RuntimeID: agent.RuntimeID,
		Priority:  0,
		Context:   contextJSON,
	}); err != nil {
		return fmt.Errorf("dispatch human: enqueue task: %w", err)
	}
	return nil
}

func (t *TaskDispatcher) applyTaskOverrides(ctx context.Context, taskID pgtype.UUID, model, thinking, label string) error {
	model = strings.TrimSpace(model)
	thinking = strings.TrimSpace(thinking)
	if model != "" {
		if err := t.queries.SetAgentTaskModelOverride(ctx, db.SetAgentTaskModelOverrideParams{
			ID:            taskID,
			ModelOverride: model,
		}); err != nil {
			return fmt.Errorf("%s: set model override: %w", label, err)
		}
	}
	if thinking != "" {
		if err := t.queries.SetAgentTaskThinkingOverride(ctx, db.SetAgentTaskThinkingOverrideParams{
			ID:               taskID,
			ThinkingOverride: thinking,
		}); err != nil {
			return fmt.Errorf("%s: set thinking override: %w", label, err)
		}
	}
	return nil
}

// buildHumanPrompt renders the instruction an agent standing in for a human
// sign-off sees. Unlike a judge check (score a rubric), this asks for an
// explicit approve/reject decision plus a note explaining it either way.
func buildHumanPrompt(d HumanDispatch) string {
	return fmt.Sprintf(
		"Review the delivered work for this issue yourself — do not take the builder's own summary as proof — "+
			"then report your decision back to the loop gate via the report_loop_human tool with issue_id, gate, "+
			"round, check_id=%q, approved (true only if you sign off), and note (why, either way).\n\n"+
			"What you are being asked to approve (gate %s, round %d):\n%s",
		d.CheckID, d.Gate, d.Round, d.Prompt,
	)
}

// buildRevisionPrompt renders the instruction the worker agent's model sees
// on a revision: which checks failed and with what exit code, so the agent
// does not have to re-discover the failure before it can fix it.
func buildRevisionPrompt(d RevisionDispatch) string {
	// A fresh next-phase build (multi-phase): no failures to fix — the previous
	// phase's gate passed, this is the start of the next build phase.
	if d.PhaseLabel != "" && len(d.Failures) == 0 {
		return fmt.Sprintf(
			"The previous build phase passed its review. Start build phase %q for this issue: run the phase's build skill, then move the issue to review when done so the loop can verify this phase.",
			d.PhaseLabel,
		)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Loop gate %s failed and needs round %d: the previous round's verification checks did not all pass. Fix the issue, then let the loop re-verify.\n\nFailed checks:\n", d.Gate, d.Round)
	for _, o := range d.Failures {
		if o.Ran && o.ExitCode != 0 {
			fmt.Fprintf(&b, "  - exit %d: %s\n", o.ExitCode, strings.Join(o.Argv, " "))
		}
	}
	return b.String()
}
