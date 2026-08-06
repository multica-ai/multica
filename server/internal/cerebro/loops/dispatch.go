package loops

// dispatch.go is the Chain v2 egress. Workflow selects the current block; this
// package dispatches the trusted block definition without making policy
// decisions of its own.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DispatchQueries is the narrow slice of the upstream issue queries the task
// dispatcher needs: resolve the agent (for its runtime + workspace) and enqueue
// the task. Keeping it an interface mirrors workflows.IssueActions and lets the
// dispatcher be unit-tested without a live DB.
type DispatchQueries interface {
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	GetAgentRuntime(ctx context.Context, id pgtype.UUID) (db.AgentRuntime, error)
	CountRunningTasks(ctx context.Context, agentID pgtype.UUID) (int64, error)
	ListAgentSkills(ctx context.Context, agentID pgtype.UUID) ([]db.ListAgentSkillsRow, error)
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

// TaskDispatcher opens the task, approval, or eval required by a Chain block.
type TaskDispatcher struct {
	queries DispatchQueries
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
			prompt := renderApprovalPrompt(d.Block.Prompt, d.PreviousSteps)
			if err := t.notifyMember(ctx, d, d.Block.ApproverID, "workflow_human_approval", "Workflow approval needed", prompt); err != nil {
				return BlockDispatchResult{}, err
			}
			outcome, _ := json.Marshal(map[string]any{"approver_id": d.Block.ApproverID, "prompt": prompt})
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
	prompt, err := buildBlockPrompt(d.Block, d.PreviousSteps)
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
		configuredSkills := block.ConfiguredSkills()
		if len(configuredSkills) > 0 {
			skills, err := t.queries.ListAgentSkills(ctx, agentID)
			if err != nil {
				return AgentRef{}, db.Agent{}, false, fmt.Errorf("dispatch block: list agent skills: %w", err)
			}
			for _, required := range configuredSkills {
				if !hasAttachedSkill(skills, required) {
					return AgentRef{}, db.Agent{}, false, fmt.Errorf("dispatch block: agent does not have skill %q attached", required)
				}
			}
		}
		return runner, agent, true, nil
	}
	return AgentRef{}, db.Agent{}, false, nil
}

func hasAttachedSkill(skills []db.ListAgentSkillsRow, name string) bool {
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
		"workflow_skill_names":     d.Block.ConfiguredSkills(),
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

func buildBlockPrompt(block Block, previousSteps []ChainStep) (string, error) {
	switch block.Type {
	case BlockSession:
		skills := block.ConfiguredSkills()
		prompt := fmt.Sprintf("Run workflow block %q on this issue using every explicitly selected skill:\n- %s", block.ID, strings.Join(skills, "\n- "))
		if block.Goal != "" {
			prompt += "\n\nGoal:\n" + block.Goal
		}
		return appendOpenStepInstruction(prompt, block), nil
	case BlockCommand:
		return appendOpenStepInstruction(fmt.Sprintf("Run this workflow command exactly as given and leave the result on this issue:\n\n    %s", strings.Join(block.Check, " ")), block), nil
	case BlockReview:
		prompt := fmt.Sprintf("Review the actual delivered work on this issue against workflow block %q. Do not accept the builder's summary as proof.", block.ID)
		if skills := block.ConfiguredSkills(); len(skills) > 0 {
			prompt = fmt.Sprintf("Review the actual delivered work on this issue against workflow block %q using every explicitly selected skill:\n- %s\n\nDo not accept the builder's summary as proof.", block.ID, strings.Join(skills, "\n- "))
		}
		return appendOpenStepInstruction(prompt+"\n\nRubric:\n"+block.Rubric, block), nil
	case BlockHuman:
		return appendOpenStepInstruction("Review the actual delivered work on this issue and make the requested approval decision.\n\nApproval:\n"+renderApprovalPrompt(block.Prompt, previousSteps), block), nil
	default:
		return "", fmt.Errorf("dispatch block %s: unsupported type %q", block.ID, block.Type)
	}
}

var approvalPlaceholderRE = regexp.MustCompile(`\{\{[^{}]+\}\}`)

// renderApprovalPrompt turns the author-defined template into the concrete
// request an approver sees. Outcomes are generated by the preceding agent task;
// missing or unknown placeholders disappear instead of leaking template syntax.
func renderApprovalPrompt(template string, steps []ChainStep) string {
	var previous *ChainStep
	for i := range steps {
		if steps[i].Status == StepCompleted {
			previous = &steps[i]
		}
	}
	values := map[string]string{}
	if previous != nil {
		values["{{previous.block}}"] = previous.BlockID
		values["{{previous.outcome}}"] = formatOutcome(previous.Outcome)
		var outcome map[string]any
		if json.Unmarshal(previous.Outcome, &outcome) == nil {
			values["{{previous.output}}"] = formatTemplateValue(firstOutcomeValue(outcome, "output", "result", "summary"))
			values["{{previous.evidence}}"] = formatTemplateValue(outcome["evidence"])
		}
	}
	result := template
	for placeholder, value := range values {
		result = strings.ReplaceAll(result, placeholder, value)
	}
	result = approvalPlaceholderRE.ReplaceAllString(result, "")
	lines := strings.Split(result, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func firstOutcomeValue(outcome map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := outcome[key]; ok {
			return value
		}
	}
	return nil
}

func formatOutcome(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return strings.TrimSpace(string(raw))
	}
	return formatTemplateValue(value)
}

func formatTemplateValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	raw, _ := json.MarshalIndent(value, "", "  ")
	return string(raw)
}

func approvalPromptFromOutcome(fallback string, outcome json.RawMessage) string {
	var value struct {
		Prompt string `json:"prompt"`
	}
	if json.Unmarshal(outcome, &value) == nil && value.Prompt != "" {
		return value.Prompt
	}
	return fallback
}

func appendOpenStepInstruction(prompt string, block Block) string {
	if !block.Steps.Allowed {
		return prompt
	}
	return prompt + fmt.Sprintf("\n\nThis block may contain up to %d steps. Call open_loop_step before finishing if the block needs another step of the same kind.", block.Steps.Max)
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
