package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type PostgresHookRepository struct {
	db cerebrodb.DBTX
}

func NewPostgresHookRepository(db cerebrodb.DBTX) *PostgresHookRepository {
	return &PostgresHookRepository{db: db}
}

const hookPolicyColumns = `id, family_id, workspace_id, name, description, policy_version,
mode, fail_mode, event_types, conditions, baseline_at, published_at,
created_by_id, created_by_type, published_by_id, created_at, updated_at`

func (r *PostgresHookRepository) List(ctx context.Context, workspaceID string) ([]HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+hookPolicyColumns+` FROM (
		SELECT DISTINCT ON (family_id) `+hookPolicyColumns+`
		FROM cerebro_workflow_hook_policy
		WHERE workspace_id=$1
		ORDER BY family_id, policy_version DESC, updated_at DESC
	) latest_policy ORDER BY updated_at DESC`, wsID)
	if err != nil {
		return nil, err
	}
	var policies []HookPolicy
	for rows.Next() {
		policy, _, err := scanHookPolicy(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for index := range policies {
		if err := r.loadPolicyParts(ctx, &policies[index]); err != nil {
			return nil, err
		}
		if err := r.loadPolicyMetrics(ctx, &policies[index]); err != nil {
			return nil, err
		}
	}
	return policies, nil
}

func (r *PostgresHookRepository) Get(ctx context.Context, workspaceID, id string) (HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookPolicy{}, err
	}
	policyID, err := util.ParseUUID(id)
	if err != nil {
		return HookPolicy{}, err
	}
	policy, _, err := scanHookPolicy(r.db.QueryRow(ctx, `SELECT `+hookPolicyColumns+` FROM cerebro_workflow_hook_policy WHERE id=$1 AND workspace_id=$2`, policyID, wsID))
	if errors.Is(err, pgx.ErrNoRows) {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	if err != nil {
		return HookPolicy{}, err
	}
	if err := r.loadPolicyParts(ctx, &policy); err != nil {
		return HookPolicy{}, err
	}
	if err := r.loadPolicyMetrics(ctx, &policy); err != nil {
		return HookPolicy{}, err
	}
	return policy, nil
}

func (r *PostgresHookRepository) Create(ctx context.Context, workspaceID string, actor HookPermissionActor, policy HookPolicy) (HookPolicy, error) {
	return r.insertVersion(ctx, workspaceID, actor, uuid.NewString(), 1, policy)
}

func (r *PostgresHookRepository) Update(ctx context.Context, workspaceID string, actor HookPermissionActor, id string, policy HookPolicy) (HookPolicy, error) {
	current, err := r.Get(ctx, workspaceID, id)
	if err != nil {
		return HookPolicy{}, err
	}
	if current.Mode == HookModeManaged && !actor.IsOwner {
		return HookPolicy{}, ErrManagedHookLocked
	}
	policyID, _ := util.ParseUUID(id)
	var familyID pgtype.UUID
	if err := r.db.QueryRow(ctx, `SELECT family_id FROM cerebro_workflow_hook_policy WHERE id=$1`, policyID).Scan(&familyID); err != nil {
		return HookPolicy{}, err
	}
	return r.insertVersion(ctx, workspaceID, actor, util.UUIDToString(familyID), current.Version+1, policy)
}

func (r *PostgresHookRepository) Disable(ctx context.Context, workspaceID string, actor HookPermissionActor, id string) (HookPolicy, error) {
	current, err := r.Get(ctx, workspaceID, id)
	if err != nil {
		return HookPolicy{}, err
	}
	if current.Mode == HookModeManaged && !actor.IsOwner {
		return HookPolicy{}, ErrManagedHookLocked
	}
	policyID, _ := util.ParseUUID(id)
	wsID, _ := util.ParseUUID(workspaceID)
	if _, err := r.db.Exec(ctx, `UPDATE cerebro_workflow_hook_policy SET mode='off', updated_at=now() WHERE id=$1 AND workspace_id=$2`, policyID, wsID); err != nil {
		return HookPolicy{}, err
	}
	current.Mode = HookModeOff
	current.UpdatedAt = time.Now().UTC()
	return current, nil
}

func (r *PostgresHookRepository) Delete(ctx context.Context, workspaceID string, actor HookPermissionActor, id string) error {
	current, err := r.Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if current.Mode == HookModeManaged && !actor.IsOwner {
		return ErrManagedHookLocked
	}
	policyID, _ := util.ParseUUID(id)
	wsID, _ := util.ParseUUID(workspaceID)
	query := `DELETE FROM cerebro_workflow_hook_policy WHERE workspace_id=$1 AND family_id=(SELECT family_id FROM cerebro_workflow_hook_policy WHERE id=$2 AND workspace_id=$1) AND mode <> 'managed'`
	if current.Mode == HookModeManaged && actor.IsOwner {
		query = `DELETE FROM cerebro_workflow_hook_policy WHERE workspace_id=$1 AND family_id=(SELECT family_id FROM cerebro_workflow_hook_policy WHERE id=$2 AND workspace_id=$1)`
	}
	result, err := r.db.Exec(ctx, query, wsID, policyID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrHookPolicyNotFound
	}
	return nil
}

func (r *PostgresHookRepository) insertVersion(ctx context.Context, workspaceID string, actor HookPermissionActor, familyID string, version int, policy HookPolicy) (HookPolicy, error) {
	if policy.FailMode == HookFailMode("open") {
		policy.FailMode = HookFailWarn
	}
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookPolicy{}, err
	}
	familyUUID, err := util.ParseUUID(familyID)
	if err != nil {
		return HookPolicy{}, err
	}
	actorID, err := util.ParseUUID(actor.ID)
	if err != nil {
		return HookPolicy{}, err
	}
	eventsJSON, _ := json.Marshal(policy.Events)
	conditionsJSON, _ := json.Marshal(policy.Conditions)
	insert := `INSERT INTO cerebro_workflow_hook_policy
(family_id, workspace_id, name, description, policy_version, mode, fail_mode, event_types, conditions, created_by_id, created_by_type)
VALUES ($1,$2,$3,$4,$5,'dry_run',$6,$7,$8,$9,$10) RETURNING ` + hookPolicyColumns
	created, _, err := scanHookPolicy(r.db.QueryRow(ctx, insert, familyUUID, wsID, policy.Name, policy.Description, version, policy.FailMode, eventsJSON, conditionsJSON, actorID, actor.Type))
	if err != nil {
		return HookPolicy{}, err
	}
	if err := r.insertPolicyParts(ctx, created.ID, policy.Bindings, policy.Handlers); err != nil {
		return HookPolicy{}, err
	}
	created.Bindings = policy.Bindings
	created.Handlers = policy.Handlers
	return created, nil
}

func (r *PostgresHookRepository) insertPolicyParts(ctx context.Context, policyID string, bindings []HookBinding, handlers []HookHandler) error {
	id, err := util.ParseUUID(policyID)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if _, err := r.db.Exec(ctx, `INSERT INTO cerebro_workflow_hook_binding (policy_id, scope_kind, scope_id, priority) VALUES ($1,$2,$3,$4)`, id, binding.Kind, binding.ID, binding.Priority); err != nil {
			return err
		}
	}
	for position, handler := range handlers {
		modifications, _ := json.Marshal(handler.Modifications)
		actions, _ := json.Marshal(handler.Actions)
		if _, err := r.db.Exec(ctx, `INSERT INTO cerebro_workflow_hook_handler (policy_id, position, decision, requirement, modifications, actions) VALUES ($1,$2,$3,$4,$5,$6)`, id, position, handler.Decision, handler.Requirement, modifications, actions); err != nil {
			return err
		}
	}
	return nil
}

func (r *PostgresHookRepository) loadPolicyParts(ctx context.Context, policy *HookPolicy) error {
	id, err := util.ParseUUID(policy.ID)
	if err != nil {
		return err
	}
	bindingRows, err := r.db.Query(ctx, `SELECT scope_kind, scope_id, priority FROM cerebro_workflow_hook_binding WHERE policy_id=$1 ORDER BY priority DESC, created_at`, id)
	if err != nil {
		return err
	}
	for bindingRows.Next() {
		var binding HookBinding
		if err := bindingRows.Scan(&binding.Kind, &binding.ID, &binding.Priority); err != nil {
			bindingRows.Close()
			return err
		}
		policy.Bindings = append(policy.Bindings, binding)
	}
	if err := bindingRows.Err(); err != nil {
		bindingRows.Close()
		return err
	}
	bindingRows.Close()

	handlerRows, err := r.db.Query(ctx, `SELECT id, decision, requirement, modifications, actions FROM cerebro_workflow_hook_handler WHERE policy_id=$1 ORDER BY position`, id)
	if err != nil {
		return err
	}
	for handlerRows.Next() {
		var handlerID pgtype.UUID
		var handler HookHandler
		var modifications, actions []byte
		if err := handlerRows.Scan(&handlerID, &handler.Decision, &handler.Requirement, &modifications, &actions); err != nil {
			handlerRows.Close()
			return err
		}
		handler.ID = util.UUIDToString(handlerID)
		_ = json.Unmarshal(modifications, &handler.Modifications)
		_ = json.Unmarshal(actions, &handler.Actions)
		policy.Handlers = append(policy.Handlers, handler)
	}
	if err := handlerRows.Err(); err != nil {
		handlerRows.Close()
		return err
	}
	handlerRows.Close()
	return nil
}

func (r *PostgresHookRepository) loadPolicyMetrics(ctx context.Context, policy *HookPolicy) error {
	id, err := util.ParseUUID(policy.ID)
	if err != nil {
		return err
	}
	var count int
	var lastRun pgtype.Timestamptz
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*), MAX(created_at) FROM cerebro_workflow_hook_run WHERE policy_id=$1`, id).Scan(&count, &lastRun); err != nil {
		return err
	}
	policy.ObservedRuns = count
	if lastRun.Valid {
		value := lastRun.Time
		policy.LastRunAt = &value
	}
	policy.CanPublish = policy.Mode == HookModeDryRun && count > 0 && policy.BaselineAt != nil
	return nil
}

func (r *PostgresHookRepository) Publish(ctx context.Context, workspaceID, id, actorID string) (HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookPolicy{}, err
	}
	policyID, err := util.ParseUUID(id)
	if err != nil {
		return HookPolicy{}, err
	}
	publisherID, err := util.ParseUUID(actorID)
	if err != nil {
		return HookPolicy{}, err
	}
	query := `UPDATE cerebro_workflow_hook_policy p SET mode='enforce', published_at=now(), published_by_id=$3, updated_at=now()
WHERE p.id=$1 AND p.workspace_id=$2 AND p.mode='dry_run' AND p.baseline_at IS NOT NULL
AND EXISTS (SELECT 1 FROM cerebro_workflow_hook_run r WHERE r.policy_id=p.id) RETURNING ` + hookPolicyColumns
	policy, _, err := scanHookPolicy(r.db.QueryRow(ctx, query, policyID, wsID, publisherID))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.Get(ctx, workspaceID, id); getErr != nil {
			return HookPolicy{}, getErr
		}
		return HookPolicy{}, ErrHookPublishPrerequisite
	}
	if err != nil {
		return HookPolicy{}, err
	}
	if err := r.loadPolicyParts(ctx, &policy); err != nil {
		return HookPolicy{}, err
	}
	return policy, nil
}

func (r *PostgresHookRepository) Runs(ctx context.Context, workspaceID, policyID string) ([]HookRunRecord, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, err
	}
	id, err := util.ParseUUID(policyID)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT id, policy_version, input_event, source_scope, decision, would_decision, matched_conditions, fail_mode, remediation, latency_ms, timed_out, created_at FROM cerebro_workflow_hook_run WHERE workspace_id=$1 AND policy_id=$2 ORDER BY created_at DESC LIMIT 200`, wsID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HookRunRecord
	for rows.Next() {
		var runID pgtype.UUID
		var eventJSON, scopeJSON, matchesJSON []byte
		var decision string
		var wouldDecision pgtype.Text
		var remediation string
		var timedOut bool
		var created pgtype.Timestamptz
		var run HookRunRecord
		if err := rows.Scan(&runID, &run.PolicyVersion, &eventJSON, &scopeJSON, &decision, &wouldDecision, &matchesJSON, &run.FailMode, &remediation, &run.LatencyMS, &timedOut, &created); err != nil {
			return nil, err
		}
		run.ID = util.UUIDToString(runID)
		run.PolicyID = policyID
		run.CreatedAt = created.Time
		_ = json.Unmarshal(eventJSON, &run.Event)
		_ = json.Unmarshal(scopeJSON, &run.SourceScope)
		run.Result = HookResult{Decision: HookDecision(decision), WouldDecision: HookDecision(wouldDecision.String), TimedOut: timedOut}
		_ = json.Unmarshal(matchesJSON, &run.Result.MatchedConditions)
		if remediation != "" {
			run.Result.Requirements = strings.Split(remediation, "\n")
		}
		actionRows, actionErr := r.db.Query(ctx, `SELECT action_index,action_type,status,result,error FROM cerebro_workflow_hook_action_run WHERE hook_run_id=$1 ORDER BY action_index`, runID)
		if actionErr != nil {
			return nil, actionErr
		}
		for actionRows.Next() {
			var action HookActionResult
			var resultJSON []byte
			if err := actionRows.Scan(&action.ActionIndex, &action.Type, &action.Status, &resultJSON, &action.Error); err != nil {
				actionRows.Close()
				return nil, err
			}
			_ = json.Unmarshal(resultJSON, &action.Result)
			run.Result.ActionResults = append(run.Result.ActionResults, action)
		}
		if err := actionRows.Err(); err != nil {
			actionRows.Close()
			return nil, err
		}
		actionRows.Close()
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *PostgresHookRepository) RecordRun(ctx context.Context, workspaceID string, run HookRunRecord) error {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return err
	}
	policyID, err := util.ParseUUID(run.PolicyID)
	if err != nil {
		return err
	}
	policy, err := r.Get(ctx, workspaceID, run.PolicyID)
	if err != nil {
		return err
	}
	eventJSON, _ := json.Marshal(run.Event)
	scopeJSON, _ := json.Marshal(run.SourceScope)
	conditionsJSON, _ := json.Marshal(run.Result.MatchedConditions)
	would := pgtype.Text{String: string(run.Result.WouldDecision), Valid: run.Result.WouldDecision != ""}
	key := fmt.Sprintf("%s:%d:test", run.Event.EventID, run.PolicyVersion)
	var hookRunID pgtype.UUID
	err = r.db.QueryRow(ctx, `INSERT INTO cerebro_workflow_hook_run
(workspace_id,policy_id,policy_version,event_id,event_type,source_scope,input_event,matched_conditions,decision,would_decision,fail_mode,remediation,latency_ms,timed_out,idempotency_key)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (workspace_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key RETURNING id`, wsID, policyID, run.PolicyVersion, run.Event.EventID, run.Event.Type, scopeJSON, eventJSON, conditionsJSON, run.Result.Decision, would, policy.FailMode, strings.Join(run.Result.Requirements, "\n"), run.LatencyMS, run.Result.TimedOut, key).Scan(&hookRunID)
	if err != nil {
		return err
	}
	for _, action := range run.Result.ActionResults {
		configJSON, _ := json.Marshal(action.Config)
		resultJSON, _ := json.Marshal(action.Result)
		_, err = r.db.Exec(ctx, `INSERT INTO cerebro_workflow_hook_action_run
(hook_run_id,handler_id,action_index,action_type,action_config,status,result,error,started_at,finished_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now(),now()) ON CONFLICT (hook_run_id,action_index) DO NOTHING`,
			hookRunID, optionalHookUUID(action.HandlerID), action.ActionIndex, action.Type, configJSON, action.Status, resultJSON, action.Error)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *PostgresHookRepository) RefreshBaseline(ctx context.Context, workspaceID, policyID string) (time.Time, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return time.Time{}, err
	}
	id, err := util.ParseUUID(policyID)
	if err != nil {
		return time.Time{}, err
	}
	var baseline pgtype.Timestamptz
	err = r.db.QueryRow(ctx, `UPDATE cerebro_workflow_hook_policy p SET baseline_at=now(), updated_at=now()
WHERE p.id=$1 AND p.workspace_id=$2 AND p.mode='dry_run'
AND EXISTS (SELECT 1 FROM cerebro_workflow_hook_run r WHERE r.policy_id=p.id)
RETURNING p.baseline_at`, id, wsID).Scan(&baseline)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrHookPublishPrerequisite
	}
	if err != nil {
		return time.Time{}, err
	}
	return baseline.Time, nil
}

type hookRowScanner interface {
	Scan(...any) error
}

func scanHookPolicy(row hookRowScanner) (HookPolicy, pgtype.UUID, error) {
	var id, familyID, workspaceID, createdByID, publishedByID pgtype.UUID
	var version int32
	var mode, failMode, name, description, createdByType string
	var eventJSON, conditionsJSON []byte
	var baselineAt, publishedAt, createdAt, updatedAt pgtype.Timestamptz
	if err := row.Scan(&id, &familyID, &workspaceID, &name, &description, &version, &mode, &failMode, &eventJSON, &conditionsJSON, &baselineAt, &publishedAt, &createdByID, &createdByType, &publishedByID, &createdAt, &updatedAt); err != nil {
		return HookPolicy{}, pgtype.UUID{}, err
	}
	policy := HookPolicy{ID: util.UUIDToString(id), Version: int(version), Name: name, Description: description, Mode: HookMode(mode), FailMode: HookFailMode(failMode), CreatedByID: util.UUIDToString(createdByID), CreatedByType: createdByType, UpdatedAt: updatedAt.Time}
	if baselineAt.Valid {
		baseline := baselineAt.Time
		policy.BaselineAt = &baseline
	}
	if err := json.Unmarshal(eventJSON, &policy.Events); err != nil {
		return HookPolicy{}, pgtype.UUID{}, err
	}
	if err := json.Unmarshal(conditionsJSON, &policy.Conditions); err != nil {
		return HookPolicy{}, pgtype.UUID{}, err
	}
	return policy, familyID, nil
}
