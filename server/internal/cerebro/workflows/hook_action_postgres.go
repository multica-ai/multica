package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

var ErrHookActionPermissionDenied = errors.New("workflow hook creator no longer has permission to run this action")

// EvalStore is the hook↔eval seam. It is declared here (not imported from the
// evals package) because evals → loops → workflows would form an import cycle;
// *evals.Store satisfies it. LatestRunPassed backs eval.gate (read a verdict);
// RunForIssue backs eval.run (execute + persist a run).
type EvalStore interface {
	LatestRunPassed(ctx context.Context, workspaceID, evalID, issueID uuid.UUID) (bool, error)
	RunForIssue(ctx context.Context, workspaceID, actorID, evalID, issueID uuid.UUID, actorType string) (runID string, status string, err error)
	// EvalPassed backs the eval_passed deferred hook condition; it satisfies
	// HookConditionResolver. On *evals.Store it is an alias of LatestRunPassed.
	EvalPassed(ctx context.Context, workspaceID, evalID, issueID uuid.UUID) (bool, error)
}

type PostgresHookActionExecutor struct {
	db         cerebrodb.DBTX
	authorizer HookAuthorizer
	// CEREBRO(FIR-3496 Phase 4): hook↔eval coupling. evalStore reads the latest
	// eval verdict for eval.gate and executes+persists a run for eval.run. Nil-safe:
	// both actions fail closed when it is not wired (or has no runner), so the
	// feature stays invisible until deliberately enabled.
	evalStore EvalStore
}

type HookActionAuthorizer interface {
	CanAction(context.Context, string, HookPermissionActor, string) bool
}

func NewPostgresHookActionExecutor(db cerebrodb.DBTX, authorizer HookAuthorizer, evalStore EvalStore) *PostgresHookActionExecutor {
	return &PostgresHookActionExecutor{db: db, authorizer: authorizer, evalStore: evalStore}
}

func (e *PostgresHookActionExecutor) ExecuteHookAction(ctx context.Context, policy HookPolicy, event HookEvent, action HookAction) (map[string]any, error) {
	actor := HookPermissionActor{Type: policy.CreatedByType, ID: policy.CreatedByID}
	if e.authorizer == nil || !e.authorizer.Can(ctx, event.WorkspaceID, actor, HookPermissionWrite) {
		return nil, ErrHookActionPermissionDenied
	}
	if capability := hookActionCapability(action.Type); capability != "" {
		actionAuthorizer, ok := e.authorizer.(HookActionAuthorizer)
		if !ok || !actionAuthorizer.CanAction(ctx, event.WorkspaceID, actor, capability) {
			return nil, fmt.Errorf("%w: %s requires %s", ErrHookActionPermissionDenied, action.Type, capability)
		}
	}
	workspaceID, err := util.ParseUUID(event.WorkspaceID)
	if err != nil {
		return nil, err
	}
	actorID, err := util.ParseUUID(policy.CreatedByID)
	if err != nil {
		return nil, err
	}
	configJSON, _ := json.Marshal(action.Config)

	switch action.Type {
	case "member.notify":
		memberID, err := hookConfigUUID(action.Config, "member_id")
		if err != nil {
			return nil, err
		}
		var id pgtype.UUID
		err = e.db.QueryRow(ctx, `INSERT INTO inbox_item
(workspace_id,recipient_type,recipient_id,type,severity,issue_id,title,body,actor_type,actor_id,details,route)
VALUES ($1,'member',$2,'workflow_hook','info',$3,$4,$5,$6,$7,$8,'inbox') RETURNING id`,
			workspaceID, memberID, optionalHookUUID(event.IssueID), hookConfigString(action.Config, "title", "Workflow hook"),
			hookConfigString(action.Config, "message", "A workflow hook matched."), policy.CreatedByType, actorID, configJSON).Scan(&id)
		return hookIDResult(id), err

	case "agent.dispatch", "squad.dispatch":
		agentID, err := hookConfigUUID(action.Config, "agent_id")
		if err != nil {
			return nil, err
		}
		var id, squadID pgtype.UUID
		if action.Type == "squad.dispatch" {
			squadID, err = hookConfigUUID(action.Config, "squad_id")
			if err != nil {
				return nil, err
			}
		}
		payload, _ := json.Marshal(map[string]any{"type": "workflow_hook", "prompt": hookConfigString(action.Config, "prompt", "Continue this workflow hook action."), "hook_policy_id": policy.ID})
		err = e.db.QueryRow(ctx, `INSERT INTO agent_task_queue
(agent_id,runtime_id,issue_id,status,priority,context,source_task_id,delegating_agent_id,squad_id)
SELECT a.id,a.runtime_id,$2,'queued',0,$3,$4,$5,$6 FROM agent a
WHERE a.id=$1 AND a.workspace_id=$7 RETURNING id`, agentID, optionalHookUUID(event.IssueID), payload,
			optionalHookUUID(hookConfigString(action.Config, "source_task_id", "")), actorID, squadID, workspaceID).Scan(&id)
		return hookIDResult(id), err

	case "skill.run":
		return e.runSkill(ctx, workspaceID, actorID, policy, event, action.Config)
	case "judge.gate":
		return e.startJudgeGate(ctx, workspaceID, actorID, policy, event, action.Config)
	case "quality.gate":
		return e.qualityGate(ctx, event, action.Config)
	case "eval.gate":
		return e.startEvalGate(ctx, workspaceID, event, action.Config)
	case "eval.run":
		return e.startEvalRun(ctx, workspaceID, actorID, policy, event, action.Config)

	case "wakeup.create":
		fireAt, err := time.Parse(time.RFC3339, hookConfigString(action.Config, "fire_at", ""))
		if err != nil {
			return nil, fmt.Errorf("wakeup.create fire_at: %w", err)
		}
		var id pgtype.UUID
		err = e.db.QueryRow(ctx, `INSERT INTO cerebro_agent_wakeup
(workspace_id,agent_id,issue_id,prompt,trigger_type,fire_at,created_by_id)
VALUES ($1,$2,$3,$4,'time',$5,$6) RETURNING id`, workspaceID,
			optionalHookUUID(hookConfigString(action.Config, "agent_id", event.AgentID)),
			optionalHookUUID(hookConfigString(action.Config, "issue_id", event.IssueID)),
			hookConfigString(action.Config, "prompt", "Continue the workflow."), fireAt, actorID).Scan(&id)
		return hookIDResult(id), err

	case "wakeup.cancel":
		return e.cancelWakeup(ctx, workspaceID, action.Config)
	case "session.handoff":
		return e.handoffSession(ctx, workspaceID, event, action.Config)
	case "task.cancel":
		return e.cancelTask(ctx, workspaceID, action.Config)
	case "task.retry":
		return e.retryTask(ctx, workspaceID, action.Config)
	case "artifact.create_or_update":
		return e.upsertArtifact(ctx, workspaceID, actorID, policy, event, action.Config)
	case "workflow.activate", "workflow.resume", "workflow.pause", "workflow.stop":
		return e.controlWorkflow(ctx, workspaceID, action.Type, action.Config)
	case "approval.require":
		var id pgtype.UUID
		err = e.db.QueryRow(ctx, `INSERT INTO cerebro_approval_request
(workspace_id,requester_type,requester_id,agent_id,capability,resource,reason,context)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, workspaceID, policy.CreatedByType, actorID,
			optionalHookUUID(event.AgentID), hookConfigString(action.Config, "capability", "workflow_hook_action"),
			hookConfigString(action.Config, "resource", policy.ID), hookConfigString(action.Config, "reason", "Workflow hook requires approval"), configJSON).Scan(&id)
		return hookIDResult(id), err
	case "issue.comment":
		issueID := optionalHookUUID(hookConfigString(action.Config, "issue_id", event.IssueID))
		if !issueID.Valid {
			return nil, errors.New("issue.comment requires an issue")
		}
		body := renderHookTemplate(hookConfigString(action.Config, "body", ""), event)
		// The failing run's agent authors the comment so the message reads as
		// that agent reporting its own stop; fall back to the policy creator
		// when the event has no agent (e.g. a test fire).
		authorType, authorID := "agent", optionalHookUUID(event.AgentID)
		if !authorID.Valid {
			authorType, authorID = policy.CreatedByType, actorID
		}
		var id pgtype.UUID
		err = e.db.QueryRow(ctx, `INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
SELECT i.id, i.workspace_id, $3, $4, $5, 'comment' FROM issue i WHERE i.id=$1 AND i.workspace_id=$2
RETURNING id`, issueID, workspaceID, authorType, authorID, body).Scan(&id)
		return hookIDResult(id), err

	case "issue.check_related":
		return e.checkRelated(ctx, workspaceID, policy, event, action.Config)

	case "issue.status":
		issueID := optionalHookUUID(hookConfigString(action.Config, "issue_id", event.IssueID))
		if !issueID.Valid {
			return nil, errors.New("issue.status requires an issue")
		}
		status := hookConfigString(action.Config, "status", "")
		valid := false
		for _, allowed := range issueStatusActionValues {
			if status == allowed {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("issue.status status %q is not a valid issue status", status)
		}
		var id pgtype.UUID
		err = e.db.QueryRow(ctx, `UPDATE issue SET status=$3, updated_at=now()
WHERE id=$1 AND workspace_id=$2 RETURNING id`, issueID, workspaceID, status).Scan(&id)
		return hookIDResult(id), err

	case "audit.record", "metric.increment":
		var id pgtype.UUID
		details, _ := json.Marshal(map[string]any{"policy_id": policy.ID, "event_id": event.EventID, "config": action.Config})
		err = e.db.QueryRow(ctx, `INSERT INTO activity_log
(workspace_id,issue_id,actor_type,actor_id,action,details) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			workspaceID, optionalHookUUID(event.IssueID), policy.CreatedByType, actorID, action.Type, details).Scan(&id)
		return hookIDResult(id), err
	default:
		return nil, ErrActionNotConfigured
	}
}

func hookActionExecutorSupports(actionType string) bool {
	switch actionType {
	case "member.notify", "agent.dispatch", "squad.dispatch", "skill.run", "judge.gate", "quality.gate", "eval.gate", "eval.run",
		"wakeup.create", "wakeup.cancel", "session.handoff", "task.cancel", "task.retry",
		"artifact.create_or_update", "workflow.activate", "workflow.resume", "workflow.pause", "workflow.stop",
		"approval.require", "issue.comment", "issue.check_related", "issue.status", "audit.record", "metric.increment":
		return true
	default:
		return false
	}
}

func (e *PostgresHookActionExecutor) runSkill(ctx context.Context, workspaceID, actorID pgtype.UUID, policy HookPolicy, event HookEvent, config map[string]any) (map[string]any, error) {
	skillName := strings.TrimSpace(hookConfigString(config, "skill_name", ""))
	if skillName == "" {
		return nil, errors.New("skill.run skill_name is required")
	}
	agentValue := hookConfigString(config, "agent_id", event.AgentID)
	if agentValue == "" {
		return nil, errors.New("skill.run agent_id is required when the event has no agent")
	}
	agentID, err := util.ParseUUID(agentValue)
	if err != nil {
		return nil, fmt.Errorf("skill.run agent_id: %w", err)
	}
	prompt := hookConfigString(config, "prompt", "Use the "+skillName+" skill for this workflow hook event.")
	payload, _ := json.Marshal(map[string]any{
		"type": "quick_create", "prompt": prompt, "workspace_id": event.WorkspaceID,
		"requester_id": policy.CreatedByID, "workflow_hook_policy_id": policy.ID,
		"workflow_skill_name": skillName, "workflow_target_issue_id": event.IssueID,
	})
	var taskID pgtype.UUID
	delegatingAgentID := pgtype.UUID{}
	if policy.CreatedByType == "agent" {
		delegatingAgentID = actorID
	}
	err = e.db.QueryRow(ctx, `INSERT INTO agent_task_queue
(agent_id,runtime_id,issue_id,status,priority,context,delegating_agent_id)
SELECT a.id,a.runtime_id,$2,'queued',0,$3,$4 FROM agent a
JOIN agent_skill ask ON ask.agent_id=a.id JOIN skill s ON s.id=ask.skill_id
WHERE a.id=$1 AND a.workspace_id=$5 AND a.archived_at IS NULL AND a.runtime_id IS NOT NULL AND s.workspace_id=$5 AND s.name=$6
	RETURNING agent_task_queue.id`, agentID, optionalHookUUID(event.IssueID), payload, delegatingAgentID, workspaceID, skillName).Scan(&taskID)
	if err != nil {
		return nil, fmt.Errorf("skill.run enqueue attached skill: %w", err)
	}
	return map[string]any{"task_id": util.UUIDToString(taskID), "skill_name": skillName}, nil
}

func (e *PostgresHookActionExecutor) startJudgeGate(ctx context.Context, workspaceID, actorID pgtype.UUID, policy HookPolicy, event HookEvent, config map[string]any) (map[string]any, error) {
	if event.IssueID == "" {
		return nil, errors.New("judge.gate requires an issue event")
	}
	issueID, err := util.ParseUUID(event.IssueID)
	if err != nil {
		return nil, fmt.Errorf("judge.gate issue_id: %w", err)
	}
	agentID, err := hookConfigUUID(config, "agent_id")
	if err != nil {
		return nil, fmt.Errorf("judge.gate agent_id: %w", err)
	}
	rubric := strings.TrimSpace(hookConfigString(config, "rubric", ""))
	if rubric == "" {
		return nil, errors.New("judge.gate rubric is required")
	}
	gate := "hook:" + policy.ID + ":" + event.EventID
	checkID := hookConfigString(config, "check_id", "judge")
	round := int32(event.Attempt)
	if round < 1 {
		round = 1
	}
	if _, err = e.db.Exec(ctx, `INSERT INTO cerebro_loop_judge_run (issue_id,gate,round,check_id,rubric)
VALUES ($1,$2,$3,$4,$5) ON CONFLICT (issue_id,gate,round,check_id) DO NOTHING`, issueID, gate, round, checkID, rubric); err != nil {
		return nil, fmt.Errorf("judge.gate enqueue verdict: %w", err)
	}
	prompt := "Judge the linked issue against this rubric:\n\n" + rubric + "\n\nReport pass or reject for gate " + gate + ", round " + fmt.Sprint(round) + ", check " + checkID + "."
	payload, _ := json.Marshal(map[string]any{
		"type": "quick_create", "prompt": prompt, "workspace_id": event.WorkspaceID,
		"requester_id": policy.CreatedByID, "workflow_hook_policy_id": policy.ID,
		"judge_gate": gate, "judge_round": round, "judge_check_id": checkID,
	})
	var taskID pgtype.UUID
	delegatingAgentID := pgtype.UUID{}
	if policy.CreatedByType == "agent" {
		delegatingAgentID = actorID
	}
	err = e.db.QueryRow(ctx, `INSERT INTO agent_task_queue
(agent_id,runtime_id,issue_id,status,priority,context,delegating_agent_id)
SELECT a.id,a.runtime_id,$2,'queued',0,$3,$4 FROM agent a
	WHERE a.id=$1 AND a.workspace_id=$5 AND a.archived_at IS NULL AND a.runtime_id IS NOT NULL RETURNING id`, agentID, issueID, payload, delegatingAgentID, workspaceID).Scan(&taskID)
	if err != nil {
		return nil, fmt.Errorf("judge.gate dispatch judge: %w", err)
	}
	return map[string]any{"task_id": util.UUIDToString(taskID), "gate": gate, "round": round, "check_id": checkID}, nil
}

// startEvalRun runs the configured eval for real when the hook fires: it executes
// the eval via the store's wired runner (the same server executor as eval
// Run-now) and persists the resulting run, linked to the event's issue so a
// later eval.gate on the same issue reads this verdict. It fails closed when
// evals are not wired for this server, so eval.run stays inert until the feature
// is deliberately enabled.
func (e *PostgresHookActionExecutor) startEvalRun(ctx context.Context, workspaceID, actorID pgtype.UUID, policy HookPolicy, event HookEvent, config map[string]any) (map[string]any, error) {
	if e.evalStore == nil {
		return nil, errors.New("eval.run: evals not configured")
	}
	if event.IssueID == "" {
		return nil, errors.New("eval.run requires an issue event")
	}
	issueID, err := util.ParseUUID(event.IssueID)
	if err != nil {
		return nil, fmt.Errorf("eval.run issue_id: %w", err)
	}
	evalID, err := hookConfigUUID(config, "eval_id")
	if err != nil {
		return nil, fmt.Errorf("eval.run eval_id: %w", err)
	}
	runID, status, err := e.evalStore.RunForIssue(ctx, uuid.UUID(workspaceID.Bytes), uuid.UUID(actorID.Bytes), uuid.UUID(evalID.Bytes), uuid.UUID(issueID.Bytes), policy.CreatedByType)
	if err != nil {
		return nil, err
	}
	return map[string]any{"run_id": runID, "status": status, "eval_id": util.UUIDToString(evalID)}, nil
}

// ErrHookGateBlocked is returned by eval.gate when the eval's latest run did not
// pass. Returning an error makes the engine record the action as failed; a
// fail-closed policy then blocks the hook (hook_engine.go FailMode handling).
var ErrHookGateBlocked = errors.New("eval gate blocked: latest run did not pass")

// startEvalGate fails closed on the eval's latest run for the event's issue: it
// blocks the hook unless the newest run passed. Unlike eval.run it does not
// execute the eval — it reads the persisted verdict, so it needs no runner.
func (e *PostgresHookActionExecutor) startEvalGate(ctx context.Context, workspaceID pgtype.UUID, event HookEvent, config map[string]any) (map[string]any, error) {
	if e.evalStore == nil {
		return nil, errors.New("eval.gate: evals not configured")
	}
	if event.IssueID == "" {
		return nil, errors.New("eval.gate requires an issue event")
	}
	issueID, err := util.ParseUUID(event.IssueID)
	if err != nil {
		return nil, fmt.Errorf("eval.gate issue_id: %w", err)
	}
	evalID, err := hookConfigUUID(config, "eval_id")
	if err != nil {
		return nil, fmt.Errorf("eval.gate eval_id: %w", err)
	}
	passed, err := e.evalStore.LatestRunPassed(ctx, uuid.UUID(workspaceID.Bytes), uuid.UUID(evalID.Bytes), uuid.UUID(issueID.Bytes))
	if err != nil {
		return nil, err
	}
	if !passed {
		return map[string]any{"eval_id": util.UUIDToString(evalID), "passed": false}, ErrHookGateBlocked
	}
	return map[string]any{"eval_id": util.UUIDToString(evalID), "passed": true}, nil
}

func (e *PostgresHookActionExecutor) cancelWakeup(ctx context.Context, workspaceID pgtype.UUID, config map[string]any) (map[string]any, error) {
	id, err := hookConfigUUID(config, "wakeup_id")
	if err != nil {
		return nil, err
	}
	tag, err := e.db.Exec(ctx, `UPDATE cerebro_agent_wakeup SET state='cancelled',cancelled_at=now(),updated_at=now() WHERE id=$1 AND workspace_id=$2 AND state='pending'`, id, workspaceID)
	return map[string]any{"id": util.UUIDToString(id), "cancelled": tag.RowsAffected() == 1}, err
}

func (e *PostgresHookActionExecutor) handoffSession(ctx context.Context, workspaceID pgtype.UUID, event HookEvent, config map[string]any) (map[string]any, error) {
	input, err := validateHandoffAction(event, config)
	if err != nil {
		return nil, err
	}
	issueID, err := util.ParseUUID(event.IssueID)
	if err != nil {
		return nil, fmt.Errorf("session.handoff issue_id: %w", err)
	}
	handoff, _ := json.Marshal(map[string]any{"summary": input.Summary, "done": input.Done, "remaining": input.Remaining, "plan_ref": input.PlanRef})
	sessionID := optionalHookUUID(hookConfigString(config, "session_id", event.SessionID))
	updated := false
	if sessionID.Valid {
		tag, updateErr := e.db.Exec(ctx, `UPDATE cerebro_session s SET handoff=$1,updated_at=now() FROM issue i WHERE s.id=$2 AND s.issue_id=i.id AND i.workspace_id=$3`, handoff, sessionID, workspaceID)
		if updateErr != nil {
			return nil, updateErr
		}
		updated = tag.RowsAffected() == 1
	}
	if !input.StartNew {
		return map[string]any{"session_id": util.UUIDToString(sessionID), "updated": updated, "started_new": false}, nil
	}
	contextJSON, _ := json.Marshal(map[string]any{"type": "workflow_hook_handoff", "hook_depth": event.HookDepth + 1, "plan_ref": input.PlanRef})
	var taskID pgtype.UUID
	err = e.db.QueryRow(ctx, `INSERT INTO agent_task_queue
(agent_id,runtime_id,issue_id,status,priority,context,force_fresh_session,handoff_note,source_task_id,delegating_agent_id)
SELECT a.id,a.runtime_id,$2,'queued',0,$3,TRUE,$4,$5,$6 FROM agent a
WHERE a.id=$1 AND a.workspace_id=$7 RETURNING id`, input.Target, issueID, contextJSON, string(handoff),
		optionalHookUUID(hookConfigString(config, "source_task_id", "")), optionalHookUUID(event.AgentID), workspaceID).Scan(&taskID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"session_id": util.UUIDToString(sessionID), "updated": updated, "started_new": true, "task_id": util.UUIDToString(taskID)}, nil
}

type handoffActionInput struct {
	Target    pgtype.UUID
	Summary   string
	Done      string
	Remaining string
	PlanRef   string
	StartNew  bool
}

func validateHandoffAction(event HookEvent, config map[string]any) (handoffActionInput, error) {
	maxDepth := hookConfigInt(config, "max_depth", MaxHookDepth)
	if maxDepth < 1 || maxDepth > MaxHookDepth || event.HookDepth >= maxDepth {
		return handoffActionInput{}, ErrHookDepthExceeded
	}
	target, err := hookConfigUUID(config, "target")
	if err != nil {
		return handoffActionInput{}, fmt.Errorf("session.handoff target: %w", err)
	}
	input := handoffActionInput{Target: target, Summary: strings.TrimSpace(hookConfigString(config, "summary", "")), Done: strings.TrimSpace(hookConfigString(config, "done", "")), Remaining: strings.TrimSpace(hookConfigString(config, "remaining", "")), PlanRef: strings.TrimSpace(hookConfigString(config, "plan_ref", "")), StartNew: true}
	if value, ok := config["start_new"].(bool); ok {
		input.StartNew = value
	}
	for key, value := range map[string]string{"summary": input.Summary, "done": input.Done, "remaining": input.Remaining} {
		if value == "" {
			return handoffActionInput{}, fmt.Errorf("session.handoff %s is required", key)
		}
	}
	return input, nil
}

func (e *PostgresHookActionExecutor) cancelTask(ctx context.Context, workspaceID pgtype.UUID, config map[string]any) (map[string]any, error) {
	id, err := hookConfigUUID(config, "task_id")
	if err != nil {
		return nil, err
	}
	tag, err := e.db.Exec(ctx, `UPDATE agent_task_queue t SET status='cancelled',completed_at=now() FROM agent a WHERE t.id=$1 AND t.agent_id=a.id AND a.workspace_id=$2 AND t.status IN ('queued','dispatched','running')`, id, workspaceID)
	return map[string]any{"id": util.UUIDToString(id), "cancelled": tag.RowsAffected() == 1}, err
}

func (e *PostgresHookActionExecutor) retryTask(ctx context.Context, workspaceID pgtype.UUID, config map[string]any) (map[string]any, error) {
	id, err := hookConfigUUID(config, "task_id")
	if err != nil {
		return nil, err
	}
	var created pgtype.UUID
	err = e.db.QueryRow(ctx, `INSERT INTO agent_task_queue
(agent_id,runtime_id,issue_id,status,priority,context,parent_task_id,source_task_id,original_user_id)
SELECT t.agent_id,t.runtime_id,t.issue_id,'queued',t.priority,t.context,t.id,t.id,t.original_user_id
FROM agent_task_queue t JOIN agent a ON a.id=t.agent_id WHERE t.id=$1 AND a.workspace_id=$2 RETURNING id`, id, workspaceID).Scan(&created)
	return hookIDResult(created), err
}

func (e *PostgresHookActionExecutor) upsertArtifact(ctx context.Context, workspaceID, actorID pgtype.UUID, policy HookPolicy, event HookEvent, config map[string]any) (map[string]any, error) {
	var id pgtype.UUID
	var err error
	if rawID := hookConfigString(config, "artifact_id", ""); rawID != "" {
		id, err = util.ParseUUID(rawID)
		if err != nil {
			return nil, err
		}
		err = e.db.QueryRow(ctx, `UPDATE artifact SET title=$1,body=$2,updated_at=now() WHERE id=$3 AND workspace_id=$4 RETURNING id`, hookConfigString(config, "title", "Workflow hook artifact"), hookConfigString(config, "body", ""), id, workspaceID).Scan(&id)
	} else {
		err = e.db.QueryRow(ctx, `INSERT INTO artifact
(workspace_id,issue_id,kind,title,body,author_type,author_id) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, workspaceID,
			optionalHookUUID(event.IssueID), hookConfigString(config, "kind", "note"), hookConfigString(config, "title", "Workflow hook artifact"),
			hookConfigString(config, "body", ""), policy.CreatedByType, actorID).Scan(&id)
	}
	return hookIDResult(id), err
}

func (e *PostgresHookActionExecutor) controlWorkflow(ctx context.Context, workspaceID pgtype.UUID, actionType string, config map[string]any) (map[string]any, error) {
	id, err := hookConfigUUID(config, "workflow_id")
	if err != nil {
		return nil, err
	}
	enabled := actionType == "workflow.activate" || actionType == "workflow.resume"
	tag, err := e.db.Exec(ctx, `UPDATE cerebro_workflow SET enabled=$1,updated_at=now() WHERE id=$2 AND workspace_id=$3`, enabled, id, workspaceID)
	return map[string]any{"id": util.UUIDToString(id), "enabled": enabled, "updated": tag.RowsAffected() == 1}, err
}

func hookConfigString(config map[string]any, key, fallback string) string {
	if value, ok := config[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func hookConfigInt(config map[string]any, key string, fallback int) int {
	switch value := config[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func hookConfigUUID(config map[string]any, key string) (pgtype.UUID, error) {
	value := hookConfigString(config, key, "")
	if value == "" {
		return pgtype.UUID{}, fmt.Errorf("%s is required", key)
	}
	return util.ParseUUID(value)
}

func optionalHookUUID(value string) pgtype.UUID {
	id, err := util.ParseUUID(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}

func hookIDResult(id pgtype.UUID) map[string]any {
	return map[string]any{"id": util.UUIDToString(id)}
}

// hookTemplateToken matches {{dotted.path}} placeholders in action config text.
var hookTemplateToken = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

// renderHookTemplate substitutes {{dotted.path}} placeholders with values from
// the hook event's condition context (e.g. {{task.failure_reason}},
// {{issue.id}}, {{attempt}}). Unknown placeholders are left verbatim so a
// typo is visible in the output instead of silently vanishing.
func renderHookTemplate(input string, event HookEvent) string {
	if !strings.Contains(input, "{{") {
		return input
	}
	ctx := hookConditionContext(event)
	return hookTemplateToken.ReplaceAllStringFunc(input, func(token string) string {
		key := strings.TrimSpace(strings.Trim(token, "{}"))
		if value, ok := lookup(key, ctx); ok && value != nil {
			return fmt.Sprint(value)
		}
		return token
	})
}
