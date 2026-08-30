package toolapproval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/toolaction"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	ErrIdentityConflict = errors.New("tool approval immutable identity conflict")
	ErrPolicyConflict   = errors.New("tool approval policy changed")
	ErrStateConflict    = errors.New("tool approval state conflict")
	ErrNotFound         = errors.New("tool approval not found")
)

type TxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

type SQLRepository struct {
	queries        *db.Queries
	txStarter      TxStarter
	actionRecorder toolaction.InTransactionRecorder
	decisionAudit  DecisionAuditWriter
}

type DecisionAudit struct {
	WorkspaceID string
	Approval    Approval
	ActorID     string
}

type DecisionAuditWriter interface {
	RecordDecision(context.Context, *db.Queries, DecisionAudit) error
}

func NewSQLService(queries *db.Queries, txStarter TxStarter) *Service {
	actions := toolaction.NewSQLService(queries)
	return NewService(NewSQLRepository(queries, txStarter, actions))
}

func NewSQLRepository(queries *db.Queries, txStarter TxStarter, actionRecorder toolaction.InTransactionRecorder) *SQLRepository {
	return NewSQLRepositoryWithAudit(queries, txStarter, actionRecorder, activityDecisionAuditWriter{})
}

func NewSQLRepositoryWithAudit(queries *db.Queries, txStarter TxStarter, actionRecorder toolaction.InTransactionRecorder, decisionAudit DecisionAuditWriter) *SQLRepository {
	return &SQLRepository{queries: queries, txStarter: txStarter, actionRecorder: actionRecorder, decisionAudit: decisionAudit}
}

func (r *SQLRepository) CreateOrGet(ctx context.Context, creation Creation) (Approval, error) {
	workspaceID, err := parseRequiredUUID(creation.WorkspaceID, "workspace_id")
	if err != nil {
		return Approval{}, err
	}
	agentID, err := parseRequiredUUID(creation.AgentID, "agent_id")
	if err != nil {
		return Approval{}, err
	}
	taskID, err := parseRequiredUUID(creation.TaskID, "task_id")
	if err != nil {
		return Approval{}, err
	}
	invocationID, err := parseRequiredUUID(creation.InvocationID, "invocation_id")
	if err != nil {
		return Approval{}, err
	}

	tx, err := r.txStarter.Begin(ctx)
	if err != nil {
		return Approval{}, fmt.Errorf("begin tool approval creation: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)

	var exists int
	if err := tx.QueryRow(ctx, `
		SELECT 1
		FROM agent_task_queue AS task
		JOIN agent ON agent.id = task.agent_id
		WHERE task.id = $1
		  AND task.agent_id = $2
		  AND agent.workspace_id = $3
		FOR SHARE OF task, agent
	`, taskID, agentID, workspaceID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrNotFound
		}
		return Approval{}, fmt.Errorf("lock tool approval task identity: %w", err)
	}
	var existingID pgtype.UUID
	var identical bool
	err = tx.QueryRow(ctx, `
		SELECT id,
		       agent_id = $3
		   AND issue_id IS NOT DISTINCT FROM $4::uuid
		   AND chat_session_id IS NOT DISTINCT FROM $5::uuid
		   AND invocation_id = $6
		   AND transport_kind = $7
		   AND server_key = $8
		   AND tool_name = $9
		   AND schema_digest = $10
		   AND policy_revision = $11
		   AND schema_field_names = $12::text[]
		   AND argument_bytes = $13
		FROM agent_tool_approval_request
		WHERE workspace_id = $1 AND task_id = $2 AND idempotency_key = $14
		FOR SHARE
	`, workspaceID, taskID, agentID, optionalUUID(creation.IssueID), optionalUUID(creation.ChatSessionID),
		invocationID, creation.TransportKind, creation.ServerKey, creation.ToolName, creation.SchemaDigest,
		creation.PolicyRevision, creation.SchemaFieldNames, creation.ArgumentBytes, creation.IdempotencyKey).Scan(&existingID, &identical)
	if err == nil {
		if !identical {
			return Approval{}, ErrIdentityConflict
		}
		existing, getErr := queries.GetAgentToolApprovalRequest(ctx, db.GetAgentToolApprovalRequestParams{WorkspaceID: workspaceID, ID: existingID})
		if getErr != nil {
			return Approval{}, fmt.Errorf("get idempotent tool approval: %w", getErr)
		}
		return approvalFromRow(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Approval{}, fmt.Errorf("check idempotent tool approval: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT 1
		FROM agent_tool_policy AS policy
		JOIN agent_tool_policy_rule AS rule
		  ON rule.workspace_id = policy.workspace_id
		 AND rule.agent_id = policy.agent_id
		 AND rule.policy_id = policy.id
		WHERE policy.workspace_id = $1
		  AND policy.agent_id = $2
		  AND policy.status = 'active'
		  AND policy.revision = $3
		  AND rule.transport_kind = $4
		  AND rule.server_key = $5
		  AND rule.tool_name = $6
		  AND rule.schema_digest = $7
		  AND rule.effect = 'require_approval'
		FOR SHARE OF policy, rule
	`, workspaceID, agentID, creation.PolicyRevision, creation.TransportKind, creation.ServerKey, creation.ToolName, creation.SchemaDigest).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrPolicyConflict
		}
		return Approval{}, fmt.Errorf("lock tool approval policy identity: %w", err)
	}

	row, err := queries.CreateOrGetAgentToolApprovalRequest(ctx, db.CreateOrGetAgentToolApprovalRequestParams{
		WorkspaceID:      workspaceID,
		AgentID:          agentID,
		TaskID:           taskID,
		IssueID:          optionalUUID(creation.IssueID),
		ChatSessionID:    optionalUUID(creation.ChatSessionID),
		InvocationID:     invocationID,
		IdempotencyKey:   creation.IdempotencyKey,
		TransportKind:    creation.TransportKind,
		ServerKey:        creation.ServerKey,
		ToolName:         creation.ToolName,
		SchemaDigest:     creation.SchemaDigest,
		PolicyRevision:   creation.PolicyRevision,
		SchemaFieldNames: creation.SchemaFieldNames,
		ArgumentBytes:    creation.ArgumentBytes,
		RequestedAt:      pgtype.Timestamptz{Time: creation.RequestedAt, Valid: true},
		ExpiresAt:        pgtype.Timestamptz{Time: creation.ExpiresAt.UTC(), Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrIdentityConflict
		}
		return Approval{}, fmt.Errorf("create or get tool approval: %w", err)
	}
	approval := approvalFromCreateRow(row)
	argumentBytes := row.ArgumentBytes
	if _, err := r.actionRecorder.RecordIn(ctx, queries, toolaction.Event{
		WorkspaceID:       creation.WorkspaceID,
		AgentID:           creation.AgentID,
		TaskID:            creation.TaskID,
		IssueID:           creation.IssueID,
		InvocationID:      creation.InvocationID,
		ApprovalRequestID: approval.ID,
		TransportKind:     creation.TransportKind,
		ServerKey:         creation.ServerKey,
		ToolName:          creation.ToolName,
		SchemaDigest:      creation.SchemaDigest,
		CoverageKind:      creation.TransportKind,
		EventType:         "approval_requested",
		ArgumentBytes:     &argumentBytes,
		OutcomeCode:       "approval_required",
		CreatedAt:         row.RequestedAt.Time,
	}); err != nil {
		return Approval{}, fmt.Errorf("audit tool approval request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Approval{}, fmt.Errorf("commit tool approval creation: %w", err)
	}
	return approval, nil
}

func (r *SQLRepository) Decide(ctx context.Context, decision Decision) (Approval, error) {
	workspaceID, err := parseRequiredUUID(decision.WorkspaceID, "workspace_id")
	if err != nil {
		return Approval{}, err
	}
	approvalID, err := parseRequiredUUID(decision.ApprovalID, "approval_id")
	if err != nil {
		return Approval{}, err
	}
	actorID, err := parseRequiredUUID(decision.Actor.UserID, "actor_user_id")
	if err != nil {
		return Approval{}, err
	}
	tx, err := r.txStarter.Begin(ctx)
	if err != nil {
		return Approval{}, fmt.Errorf("begin tool approval decision: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)

	var role string
	if err := tx.QueryRow(ctx, `
		SELECT role
		FROM member
		WHERE workspace_id = $1 AND user_id = $2
		FOR SHARE
	`, workspaceID, actorID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrForbidden
		}
		return Approval{}, fmt.Errorf("lock tool approval actor membership: %w", err)
	}
	if role != "owner" && role != "admin" {
		return Approval{}, ErrForbidden
	}
	locked, err := queries.LockAgentToolApprovalRequest(ctx, db.LockAgentToolApprovalRequestParams{WorkspaceID: workspaceID, ID: approvalID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrNotFound
		}
		return Approval{}, fmt.Errorf("lock tool approval decision: %w", err)
	}
	identity := approvalIdentity(locked)
	var decided db.AgentToolApprovalRequest
	if decision.Decision == DecisionApproved {
		decided, err = queries.ApproveAgentToolApprovalRequest(ctx, db.ApproveAgentToolApprovalRequestParams{
			DecidedAt:       pgtype.Timestamptz{Time: decision.DecidedAt, Valid: true},
			DecidedByUserID: actorID,
			WorkspaceID:     workspaceID,
			ID:              approvalID,
			AgentID:         identity.AgentID,
			TaskID:          identity.TaskID,
			InvocationID:    identity.InvocationID,
			TransportKind:   identity.TransportKind,
			ServerKey:       identity.ServerKey,
			ToolName:        identity.ToolName,
			SchemaDigest:    identity.SchemaDigest,
			PolicyRevision:  identity.PolicyRevision,
		})
	} else {
		decided, err = queries.DenyAgentToolApprovalRequest(ctx, db.DenyAgentToolApprovalRequestParams{
			ReasonCode:      pgtype.Text{String: decision.ReasonCode, Valid: true},
			DecidedAt:       pgtype.Timestamptz{Time: decision.DecidedAt, Valid: true},
			DecidedByUserID: actorID,
			WorkspaceID:     workspaceID,
			ID:              approvalID,
			AgentID:         identity.AgentID,
			TaskID:          identity.TaskID,
			InvocationID:    identity.InvocationID,
			TransportKind:   identity.TransportKind,
			ServerKey:       identity.ServerKey,
			ToolName:        identity.ToolName,
			SchemaDigest:    identity.SchemaDigest,
			PolicyRevision:  identity.PolicyRevision,
		})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrStateConflict
		}
		return Approval{}, fmt.Errorf("write tool approval decision: %w", err)
	}
	approval := approvalFromRow(decided)
	eventType, outcome := "approval_approved", "approved"
	if decision.Decision == DecisionDenied {
		eventType, outcome = "approval_denied", "denied"
	}
	argumentBytes := decided.ArgumentBytes
	if _, err := r.actionRecorder.RecordIn(ctx, queries, toolaction.Event{
		WorkspaceID:       decision.WorkspaceID,
		AgentID:           approval.AgentID,
		TaskID:            approval.TaskID,
		IssueID:           approval.IssueID,
		InvocationID:      approval.InvocationID,
		ApprovalRequestID: approval.ID,
		TransportKind:     approval.TransportKind,
		ServerKey:         approval.ServerKey,
		ToolName:          approval.ToolName,
		SchemaDigest:      approval.SchemaDigest,
		CoverageKind:      approval.TransportKind,
		EventType:         eventType,
		ArgumentBytes:     &argumentBytes,
		OutcomeCode:       outcome,
		ActorUserID:       decision.Actor.UserID,
		CreatedAt:         decision.DecidedAt,
	}); err != nil {
		return Approval{}, fmt.Errorf("audit tool approval action decision: %w", err)
	}
	if err := r.decisionAudit.RecordDecision(ctx, queries, DecisionAudit{WorkspaceID: decision.WorkspaceID, Approval: approval, ActorID: decision.Actor.UserID}); err != nil {
		return Approval{}, fmt.Errorf("audit tool approval activity decision: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Approval{}, fmt.Errorf("commit tool approval decision: %w", err)
	}
	return approval, nil
}

func (r *SQLRepository) Consume(ctx context.Context, consumption Consumption) (Approval, error) {
	workspaceID, err := parseRequiredUUID(consumption.WorkspaceID, "workspace_id")
	if err != nil {
		return Approval{}, err
	}
	taskID, err := parseRequiredUUID(consumption.TaskID, "task_id")
	if err != nil {
		return Approval{}, err
	}
	approvalID, err := parseRequiredUUID(consumption.ApprovalID, "approval_id")
	if err != nil {
		return Approval{}, err
	}
	tx, err := r.txStarter.Begin(ctx)
	if err != nil {
		return Approval{}, fmt.Errorf("begin tool approval consumption: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)

	current, err := queries.GetAgentToolApprovalRequest(ctx, db.GetAgentToolApprovalRequestParams{WorkspaceID: workspaceID, ID: approvalID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrNotFound
		}
		return Approval{}, fmt.Errorf("get tool approval for consumption: %w", err)
	}
	if current.TaskID != taskID {
		return Approval{}, ErrNotFound
	}
	if consumption.InvocationID != "" && consumption.InvocationID != util.UUIDToString(current.InvocationID) ||
		consumption.TransportKind != "" && consumption.TransportKind != current.TransportKind ||
		consumption.ServerKey != "" && consumption.ServerKey != current.ServerKey ||
		consumption.ToolName != "" && consumption.ToolName != current.ToolName ||
		consumption.SchemaDigest != "" && consumption.SchemaDigest != current.SchemaDigest ||
		consumption.PolicyRevision != 0 && consumption.PolicyRevision != current.PolicyRevision {
		return Approval{}, ErrIdentityConflict
	}
	var exists int
	if err := tx.QueryRow(ctx, `
		SELECT 1
		FROM agent_task_queue AS task
		JOIN agent ON agent.id = task.agent_id
		WHERE task.id = $1
		  AND task.agent_id = $2
		  AND agent.workspace_id = $3
		FOR SHARE OF task, agent
	`, taskID, current.AgentID, workspaceID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrNotFound
		}
		return Approval{}, fmt.Errorf("lock tool approval consumption task: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT 1
		FROM agent_tool_policy AS policy
		JOIN agent_tool_policy_rule AS rule
		  ON rule.workspace_id = policy.workspace_id
		 AND rule.agent_id = policy.agent_id
		 AND rule.policy_id = policy.id
		WHERE policy.workspace_id = $1
		  AND policy.agent_id = $2
		  AND policy.status = 'active'
		  AND policy.revision = $3
		  AND rule.transport_kind = $4
		  AND rule.server_key = $5
		  AND rule.tool_name = $6
		  AND rule.schema_digest = $7
		  AND rule.effect = 'require_approval'
		FOR SHARE OF policy, rule
	`, workspaceID, current.AgentID, current.PolicyRevision, current.TransportKind, current.ServerKey, current.ToolName, current.SchemaDigest).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrStateConflict
		}
		return Approval{}, fmt.Errorf("revalidate tool approval policy: %w", err)
	}
	identity := approvalIdentity(current)
	consumed, err := queries.ConsumeAgentToolApprovalRequest(ctx, db.ConsumeAgentToolApprovalRequestParams{
		ConsumedAt:     pgtype.Timestamptz{Time: consumption.ConsumedAt, Valid: true},
		WorkspaceID:    workspaceID,
		ID:             approvalID,
		AgentID:        identity.AgentID,
		TaskID:         identity.TaskID,
		InvocationID:   identity.InvocationID,
		TransportKind:  identity.TransportKind,
		ServerKey:      identity.ServerKey,
		ToolName:       identity.ToolName,
		SchemaDigest:   identity.SchemaDigest,
		PolicyRevision: identity.PolicyRevision,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrStateConflict
		}
		return Approval{}, fmt.Errorf("consume tool approval: %w", err)
	}
	approval := approvalFromRow(consumed)
	argumentBytes := consumed.ArgumentBytes
	if _, err := r.actionRecorder.RecordIn(ctx, queries, toolaction.Event{
		WorkspaceID:       consumption.WorkspaceID,
		AgentID:           approval.AgentID,
		TaskID:            approval.TaskID,
		IssueID:           approval.IssueID,
		InvocationID:      approval.InvocationID,
		ApprovalRequestID: approval.ID,
		TransportKind:     approval.TransportKind,
		ServerKey:         approval.ServerKey,
		ToolName:          approval.ToolName,
		SchemaDigest:      approval.SchemaDigest,
		CoverageKind:      approval.TransportKind,
		EventType:         "approval_consumed",
		ArgumentBytes:     &argumentBytes,
		OutcomeCode:       "consumed",
		CreatedAt:         consumption.ConsumedAt,
	}); err != nil {
		return Approval{}, fmt.Errorf("audit tool approval consumption: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Approval{}, fmt.Errorf("commit tool approval consumption: %w", err)
	}
	return approval, nil
}

func (r *SQLRepository) Cancel(ctx context.Context, cancellation Cancellation) (Approval, error) {
	workspaceID, err := parseRequiredUUID(cancellation.WorkspaceID, "workspace_id")
	if err != nil {
		return Approval{}, err
	}
	taskID, err := parseRequiredUUID(cancellation.TaskID, "task_id")
	if err != nil {
		return Approval{}, err
	}
	approvalID, err := parseRequiredUUID(cancellation.ApprovalID, "approval_id")
	if err != nil {
		return Approval{}, err
	}
	tx, err := r.txStarter.Begin(ctx)
	if err != nil {
		return Approval{}, fmt.Errorf("begin tool approval cancellation: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)
	current, err := queries.GetAgentToolApprovalRequest(ctx, db.GetAgentToolApprovalRequestParams{WorkspaceID: workspaceID, ID: approvalID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrNotFound
		}
		return Approval{}, fmt.Errorf("get tool approval for cancellation: %w", err)
	}
	if current.TaskID != taskID {
		return Approval{}, ErrNotFound
	}
	var exists int
	if err := tx.QueryRow(ctx, `
		SELECT 1
		FROM agent_task_queue AS task
		JOIN agent ON agent.id = task.agent_id
		WHERE task.id = $1
		  AND task.agent_id = $2
		  AND agent.workspace_id = $3
		FOR SHARE OF task, agent
	`, taskID, current.AgentID, workspaceID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrNotFound
		}
		return Approval{}, fmt.Errorf("lock tool approval cancellation task: %w", err)
	}
	identity := approvalIdentity(current)
	cancelled, err := queries.CancelAgentToolApprovalRequest(ctx, db.CancelAgentToolApprovalRequestParams{
		ReasonCode:     pgtype.Text{String: cancellation.ReasonCode, Valid: true},
		CancelledAt:    pgtype.Timestamptz{Time: cancellation.CancelledAt, Valid: true},
		WorkspaceID:    workspaceID,
		ID:             approvalID,
		AgentID:        identity.AgentID,
		TaskID:         identity.TaskID,
		InvocationID:   identity.InvocationID,
		TransportKind:  identity.TransportKind,
		ServerKey:      identity.ServerKey,
		ToolName:       identity.ToolName,
		SchemaDigest:   identity.SchemaDigest,
		PolicyRevision: identity.PolicyRevision,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrStateConflict
		}
		return Approval{}, fmt.Errorf("cancel tool approval: %w", err)
	}
	approval := approvalFromRow(cancelled)
	argumentBytes := cancelled.ArgumentBytes
	if _, err := r.actionRecorder.RecordIn(ctx, queries, toolaction.Event{
		WorkspaceID:       cancellation.WorkspaceID,
		AgentID:           approval.AgentID,
		TaskID:            approval.TaskID,
		IssueID:           approval.IssueID,
		InvocationID:      approval.InvocationID,
		ApprovalRequestID: approval.ID,
		TransportKind:     approval.TransportKind,
		ServerKey:         approval.ServerKey,
		ToolName:          approval.ToolName,
		SchemaDigest:      approval.SchemaDigest,
		CoverageKind:      approval.TransportKind,
		EventType:         "cancelled",
		ArgumentBytes:     &argumentBytes,
		OutcomeCode:       "cancelled",
		CreatedAt:         cancellation.CancelledAt,
	}); err != nil {
		return Approval{}, fmt.Errorf("audit tool approval cancellation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Approval{}, fmt.Errorf("commit tool approval cancellation: %w", err)
	}
	return approval, nil
}

func (r *SQLRepository) Get(ctx context.Context, lookup Lookup) (Approval, error) {
	workspaceID, err := parseRequiredUUID(lookup.WorkspaceID, "workspace_id")
	if err != nil {
		return Approval{}, err
	}
	taskID, err := parseRequiredUUID(lookup.TaskID, "task_id")
	if err != nil {
		return Approval{}, err
	}
	approvalID, err := parseRequiredUUID(lookup.ApprovalID, "approval_id")
	if err != nil {
		return Approval{}, err
	}
	row, err := r.queries.GetAgentToolApprovalRequest(ctx, db.GetAgentToolApprovalRequestParams{WorkspaceID: workspaceID, ID: approvalID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrNotFound
		}
		return Approval{}, fmt.Errorf("get tool approval: %w", err)
	}
	if row.TaskID != taskID {
		return Approval{}, ErrNotFound
	}
	return approvalFromRow(row), nil
}

func (r *SQLRepository) GetOperator(ctx context.Context, lookup OperatorLookup) (Approval, error) {
	workspaceID, err := parseRequiredUUID(lookup.WorkspaceID, "workspace_id")
	if err != nil {
		return Approval{}, err
	}
	actorID, err := parseRequiredUUID(lookup.Actor.UserID, "actor_user_id")
	if err != nil {
		return Approval{}, err
	}
	approvalID, err := parseRequiredUUID(lookup.ApprovalID, "approval_id")
	if err != nil {
		return Approval{}, err
	}
	member, err := r.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: actorID, WorkspaceID: workspaceID})
	if err != nil || (member.Role != "owner" && member.Role != "admin") {
		return Approval{}, ErrForbidden
	}
	row, err := r.queries.GetAgentToolApprovalRequest(ctx, db.GetAgentToolApprovalRequestParams{WorkspaceID: workspaceID, ID: approvalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Approval{}, ErrNotFound
	}
	if err != nil {
		return Approval{}, fmt.Errorf("get operator tool approval: %w", err)
	}
	return approvalFromRow(row), nil
}

func (r *SQLRepository) ListPending(ctx context.Context, query PendingQuery) ([]Approval, error) {
	workspaceID, err := parseRequiredUUID(query.WorkspaceID, "workspace_id")
	if err != nil {
		return nil, err
	}
	actorID, err := parseRequiredUUID(query.Actor.UserID, "actor_user_id")
	if err != nil {
		return nil, err
	}
	member, err := r.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: actorID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("get tool approval queue membership: %w", err)
	}
	if member.Role != "owner" && member.Role != "admin" {
		return nil, ErrForbidden
	}
	rows, err := r.queries.ListPendingAgentToolApprovalRequests(ctx, db.ListPendingAgentToolApprovalRequestsParams{
		WorkspaceID:   workspaceID,
		AsOf:          pgtype.Timestamptz{Time: query.AsOf, Valid: true},
		FilterAgentID: optionalUUID(query.AgentID),
		PageSize:      query.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list pending tool approvals: %w", err)
	}
	approvals := make([]Approval, 0, len(rows))
	for _, row := range rows {
		approvals = append(approvals, approvalFromRow(row))
	}
	return approvals, nil
}

type activityDecisionAuditWriter struct{}

func (activityDecisionAuditWriter) RecordDecision(ctx context.Context, queries *db.Queries, audit DecisionAudit) error {
	details, err := json.Marshal(map[string]any{
		"approval_id":     audit.Approval.ID,
		"agent_id":        audit.Approval.AgentID,
		"task_id":         audit.Approval.TaskID,
		"invocation_id":   audit.Approval.InvocationID,
		"transport_kind":  audit.Approval.TransportKind,
		"server_key":      audit.Approval.ServerKey,
		"tool_name":       audit.Approval.ToolName,
		"schema_digest":   audit.Approval.SchemaDigest,
		"policy_revision": audit.Approval.PolicyRevision,
		"reason_code":     audit.Approval.ReasonCode,
	})
	if err != nil {
		return err
	}
	_, err = queries.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: util.MustParseUUID(audit.WorkspaceID),
		IssueID:     optionalUUID(audit.Approval.IssueID),
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     util.MustParseUUID(audit.ActorID),
		Action:      "agent_tool_approval_" + audit.Approval.Status,
		Details:     details,
	})
	return err
}

type approvalIdentityFields struct {
	AgentID        pgtype.UUID
	TaskID         pgtype.UUID
	InvocationID   pgtype.UUID
	TransportKind  string
	ServerKey      string
	ToolName       string
	SchemaDigest   string
	PolicyRevision int64
}

func approvalIdentity(approval db.AgentToolApprovalRequest) approvalIdentityFields {
	return approvalIdentityFields{
		AgentID:        approval.AgentID,
		TaskID:         approval.TaskID,
		InvocationID:   approval.InvocationID,
		TransportKind:  approval.TransportKind,
		ServerKey:      approval.ServerKey,
		ToolName:       approval.ToolName,
		SchemaDigest:   approval.SchemaDigest,
		PolicyRevision: approval.PolicyRevision,
	}
}

func approvalFromCreateRow(row db.CreateOrGetAgentToolApprovalRequestRow) Approval {
	approval := Approval{
		ID:               util.UUIDToString(row.ID),
		WorkspaceID:      util.UUIDToString(row.WorkspaceID),
		AgentID:          util.UUIDToString(row.AgentID),
		TaskID:           util.UUIDToString(row.TaskID),
		IssueID:          util.UUIDToString(row.IssueID),
		ChatSessionID:    util.UUIDToString(row.ChatSessionID),
		InvocationID:     util.UUIDToString(row.InvocationID),
		TransportKind:    row.TransportKind,
		ServerKey:        row.ServerKey,
		ToolName:         row.ToolName,
		SchemaDigest:     row.SchemaDigest,
		PolicyRevision:   row.PolicyRevision,
		SchemaFieldNames: append([]string(nil), row.SchemaFieldNames...),
		ArgumentBytes:    row.ArgumentBytes,
		Status:           row.Status,
		ReasonCode:       row.ReasonCode.String,
		RequestedAt:      row.RequestedAt.Time,
		DecidedAt:        optionalTimePointer(row.DecidedAt),
		ConsumedAt:       optionalTimePointer(row.ConsumedAt),
		ExpiresAt:        row.ExpiresAt.Time,
		DeciderUserID:    util.UUIDToString(row.DecidedByUserID),
		DecidedByUserID:  util.UUIDToString(row.DecidedByUserID),
	}
	if approval.Status == StatusCancelled {
		approval.CancelledAt = approval.DecidedAt
	}
	return approval
}

func approvalFromRow(row db.AgentToolApprovalRequest) Approval {
	approval := Approval{
		ID:               util.UUIDToString(row.ID),
		WorkspaceID:      util.UUIDToString(row.WorkspaceID),
		AgentID:          util.UUIDToString(row.AgentID),
		TaskID:           util.UUIDToString(row.TaskID),
		IssueID:          util.UUIDToString(row.IssueID),
		ChatSessionID:    util.UUIDToString(row.ChatSessionID),
		InvocationID:     util.UUIDToString(row.InvocationID),
		TransportKind:    row.TransportKind,
		ServerKey:        row.ServerKey,
		ToolName:         row.ToolName,
		SchemaDigest:     row.SchemaDigest,
		PolicyRevision:   row.PolicyRevision,
		SchemaFieldNames: append([]string(nil), row.SchemaFieldNames...),
		ArgumentBytes:    row.ArgumentBytes,
		Status:           row.Status,
		ReasonCode:       row.ReasonCode.String,
		RequestedAt:      row.RequestedAt.Time,
		DecidedAt:        optionalTimePointer(row.DecidedAt),
		ConsumedAt:       optionalTimePointer(row.ConsumedAt),
		ExpiresAt:        row.ExpiresAt.Time,
		DeciderUserID:    util.UUIDToString(row.DecidedByUserID),
		DecidedByUserID:  util.UUIDToString(row.DecidedByUserID),
	}
	if approval.Status == StatusCancelled {
		approval.CancelledAt = approval.DecidedAt
	}
	return approval
}

func parseRequiredUUID(value, field string) (pgtype.UUID, error) {
	parsed, err := util.ParseUUID(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("%w: %s", ErrInvalidMetadata, field)
	}
	return parsed, nil
}

func optionalUUID(value string) pgtype.UUID {
	if value == "" {
		return pgtype.UUID{}
	}
	parsed, err := util.ParseUUID(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return parsed
}

func optionalTimePointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}
