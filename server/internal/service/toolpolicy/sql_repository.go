package toolpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/toolaction"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type TxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PolicyAudit struct {
	WorkspaceID  string
	AgentID      string
	ActorUserID  string
	Revision     int64
	PolicyDigest string
	RuleCount    int
}

type AuditWriter interface {
	RecordPolicyReplacement(context.Context, *db.Queries, PolicyAudit) error
}

type SQLRepository struct {
	queries        *db.Queries
	txStarter      TxStarter
	actionRecorder toolaction.InTransactionRecorder
	auditWriter    AuditWriter
	now            func() time.Time
}

func NewSQLService(queries *db.Queries, txStarter TxStarter) *Service {
	actions := toolaction.NewSQLService(queries)
	return NewService(NewSQLRepository(queries, txStarter, actions, activityAuditWriter{}))
}

func NewSQLRepository(queries *db.Queries, txStarter TxStarter, actionRecorder toolaction.InTransactionRecorder, auditWriter AuditWriter) *SQLRepository {
	return &SQLRepository{
		queries:        queries,
		txStarter:      txStarter,
		actionRecorder: actionRecorder,
		auditWriter:    auditWriter,
		now:            func() time.Time { return time.Now().UTC() },
	}
}

func (r *SQLRepository) Get(ctx context.Context, request ReadRequest) (EffectivePolicy, error) {
	workspaceID, agentID, actorID, err := parseAccessIDs(request.WorkspaceID, request.AgentID, request.Actor.UserID)
	if err != nil {
		return EffectivePolicy{}, err
	}
	agent, err := r.queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceID})
	if err != nil || agent.Kind != "user" {
		return EffectivePolicy{}, ErrNotFound
	}
	if request.Actor.Kind == ActorHuman {
		member, err := r.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: actorID, WorkspaceID: workspaceID})
		if err != nil {
			return EffectivePolicy{}, ErrNotFound
		}
		if !isOperatorRole(member.Role) && agent.OwnerID != actorID {
			return EffectivePolicy{}, ErrForbidden
		}
	} else if request.Actor.AgentID != request.AgentID {
		return EffectivePolicy{}, ErrForbidden
	}
	policy, err := r.queries.GetAgentToolPolicy(ctx, db.GetAgentToolPolicyParams{WorkspaceID: workspaceID, AgentID: agentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return EffectivePolicy{Configured: false, Rules: []Rule{}}, nil
	}
	if err != nil {
		return EffectivePolicy{}, fmt.Errorf("get tool policy: %w", err)
	}
	rules, err := r.queries.ListAgentToolPolicyRules(ctx, db.ListAgentToolPolicyRulesParams{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		PolicyID:    policy.ID,
	})
	if err != nil {
		return EffectivePolicy{}, fmt.Errorf("list tool policy rules: %w", err)
	}
	return effectiveFromDB(policy, rules)
}

func (r *SQLRepository) Replace(ctx context.Context, replacement CanonicalReplacement) (EffectivePolicy, error) {
	workspaceID, agentID, actorID, err := parseAccessIDs(replacement.WorkspaceID, replacement.AgentID, replacement.Actor.UserID)
	if err != nil {
		return EffectivePolicy{}, err
	}
	tx, err := r.txStarter.Begin(ctx)
	if err != nil {
		return EffectivePolicy{}, fmt.Errorf("begin tool policy replacement: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)

	agent, err := queries.GetAgentForUpdate(ctx, agentID)
	if err != nil || agent.WorkspaceID != workspaceID || agent.Kind != "user" {
		return EffectivePolicy{}, ErrNotFound
	}
	var currentRole string
	if err := tx.QueryRow(ctx, `
		SELECT role FROM member
		WHERE workspace_id = $1 AND user_id = $2
		FOR SHARE
	`, workspaceID, actorID).Scan(&currentRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EffectivePolicy{}, ErrNotFound
		}
		return EffectivePolicy{}, fmt.Errorf("lock tool policy actor membership: %w", err)
	}
	if !isOperatorRole(currentRole) {
		return EffectivePolicy{}, ErrForbidden
	}

	now := r.now()
	current, err := queries.LockAgentToolPolicy(ctx, db.LockAgentToolPolicyParams{WorkspaceID: workspaceID, AgentID: agentID})
	var policy db.AgentToolPolicy
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if replacement.ExpectedRevision != 0 {
			return EffectivePolicy{}, ErrRevisionConflict
		}
		policy, err = queries.CreateAgentToolPolicy(ctx, db.CreateAgentToolPolicyParams{
			WorkspaceID:     workspaceID,
			AgentID:         agentID,
			Revision:        1,
			Status:          "active",
			PolicyDigest:    replacement.PolicyDigest,
			CreatedByUserID: actorID,
			UpdatedByUserID: actorID,
		})
	case err != nil:
		return EffectivePolicy{}, fmt.Errorf("lock tool policy: %w", err)
	default:
		if current.Revision != replacement.ExpectedRevision {
			return EffectivePolicy{}, ErrRevisionConflict
		}
		if _, err := queries.DeleteAgentToolPolicyRulesForPolicy(ctx, db.DeleteAgentToolPolicyRulesForPolicyParams{
			WorkspaceID: workspaceID,
			AgentID:     agentID,
			PolicyID:    current.ID,
		}); err != nil {
			return EffectivePolicy{}, fmt.Errorf("delete replaced tool policy rules: %w", err)
		}
		policy, err = queries.UpdateAgentToolPolicyRevision(ctx, db.UpdateAgentToolPolicyRevisionParams{
			NextRevision:     current.Revision + 1,
			Status:           "active",
			PolicyDigest:     replacement.PolicyDigest,
			UpdatedByUserID:  actorID,
			UpdatedAt:        pgtype.Timestamptz{Time: now, Valid: true},
			WorkspaceID:      workspaceID,
			AgentID:          agentID,
			ExpectedRevision: replacement.ExpectedRevision,
		})
	}
	if err != nil {
		if isUniqueViolation(err) {
			return EffectivePolicy{}, ErrRevisionConflict
		}
		return EffectivePolicy{}, fmt.Errorf("write tool policy header: %w", err)
	}

	dbRules := make([]db.AgentToolPolicyRule, 0, len(replacement.Rules))
	for _, rule := range replacement.Rules {
		stored, err := queries.CreateAgentToolPolicyRule(ctx, db.CreateAgentToolPolicyRuleParams{
			WorkspaceID:   workspaceID,
			AgentID:       agentID,
			PolicyID:      policy.ID,
			TransportKind: rule.TransportKind,
			ServerKey:     rule.ServerKey,
			ToolName:      rule.ToolName,
			SchemaDigest:  rule.SchemaDigest,
			Effect:        rule.Effect,
		})
		if err != nil {
			return EffectivePolicy{}, fmt.Errorf("create tool policy rule: %w", err)
		}
		dbRules = append(dbRules, stored)
	}
	if _, err := queries.CreateAgentToolPolicyRevision(ctx, db.CreateAgentToolPolicyRevisionParams{
		WorkspaceID:    workspaceID,
		AgentID:        agentID,
		Revision:       policy.Revision,
		Status:         policy.Status,
		PolicyDigest:   policy.PolicyDigest,
		RuleIdentities: replacement.RuleIdentities,
		ActorUserID:    actorID,
		CreatedAt:      pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
		return EffectivePolicy{}, fmt.Errorf("create tool policy revision: %w", err)
	}

	cancelled, err := queries.CancelAgentToolApprovalRequestsBeforePolicyRevision(ctx, db.CancelAgentToolApprovalRequestsBeforePolicyRevisionParams{
		CancelledAt:          pgtype.Timestamptz{Time: now, Valid: true},
		WorkspaceID:          workspaceID,
		AgentID:              agentID,
		ActivePolicyRevision: policy.Revision,
	})
	if err != nil {
		return EffectivePolicy{}, fmt.Errorf("cancel approvals for replaced tool policy: %w", err)
	}
	for _, approval := range cancelled {
		if _, err := r.actionRecorder.RecordIn(ctx, queries, cancellationEvent(approval, now)); err != nil {
			return EffectivePolicy{}, fmt.Errorf("audit cancelled approval: %w", err)
		}
	}
	if err := r.auditWriter.RecordPolicyReplacement(ctx, queries, PolicyAudit{
		WorkspaceID:  replacement.WorkspaceID,
		AgentID:      replacement.AgentID,
		ActorUserID:  replacement.Actor.UserID,
		Revision:     policy.Revision,
		PolicyDigest: policy.PolicyDigest,
		RuleCount:    len(replacement.Rules),
	}); err != nil {
		return EffectivePolicy{}, fmt.Errorf("audit tool policy replacement: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return EffectivePolicy{}, fmt.Errorf("commit tool policy replacement: %w", err)
	}
	return effectiveFromDB(policy, dbRules)
}

type activityAuditWriter struct{}

func (activityAuditWriter) RecordPolicyReplacement(ctx context.Context, queries *db.Queries, audit PolicyAudit) error {
	details, err := json.Marshal(map[string]any{
		"agent_id":      audit.AgentID,
		"revision":      audit.Revision,
		"policy_digest": audit.PolicyDigest,
		"rule_count":    audit.RuleCount,
	})
	if err != nil {
		return err
	}
	_, err = queries.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: util.MustParseUUID(audit.WorkspaceID),
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     util.MustParseUUID(audit.ActorUserID),
		Action:      "agent_tool_policy_replaced",
		Details:     details,
	})
	return err
}

func cancellationEvent(approval db.AgentToolApprovalRequest, createdAt time.Time) toolaction.Event {
	return toolaction.Event{
		WorkspaceID:       util.UUIDToString(approval.WorkspaceID),
		AgentID:           util.UUIDToString(approval.AgentID),
		TaskID:            util.UUIDToString(approval.TaskID),
		IssueID:           util.UUIDToString(approval.IssueID),
		InvocationID:      util.UUIDToString(approval.InvocationID),
		ApprovalRequestID: util.UUIDToString(approval.ID),
		TransportKind:     approval.TransportKind,
		ServerKey:         approval.ServerKey,
		ToolName:          approval.ToolName,
		SchemaDigest:      approval.SchemaDigest,
		CoverageKind:      approval.TransportKind,
		EventType:         "cancelled",
		OutcomeCode:       "cancelled",
		CreatedAt:         createdAt,
	}
}

func effectiveFromDB(policy db.AgentToolPolicy, rules []db.AgentToolPolicyRule) (EffectivePolicy, error) {
	out := EffectivePolicy{
		Configured:    true,
		Revision:      policy.Revision,
		Status:        policy.Status,
		PolicyDigest:  policy.PolicyDigest,
		DefaultEffect: policy.DefaultEffect,
		Rules:         make([]Rule, 0, len(rules)),
	}
	for _, rule := range rules {
		out.Rules = append(out.Rules, Rule{
			TransportKind: rule.TransportKind,
			ServerKey:     rule.ServerKey,
			ToolName:      rule.ToolName,
			SchemaDigest:  rule.SchemaDigest,
			Effect:        rule.Effect,
		})
	}
	_, _, digest, err := canonicalize(out.Rules)
	validStatus := policy.Status == "active" || policy.Status == "draft"
	if err != nil || policy.Revision < 1 || !validStatus || policy.DefaultEffect != "deny" || !digestPattern.MatchString(policy.PolicyDigest) || digest != policy.PolicyDigest {
		return EffectivePolicy{}, ErrStoredPolicy
	}
	return out, nil
}

func parseAccessIDs(workspace, agent, actor string) (pgtype.UUID, pgtype.UUID, pgtype.UUID, error) {
	workspaceID, err := util.ParseUUID(workspace)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, ErrNotFound
	}
	agentID, err := util.ParseUUID(agent)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, ErrNotFound
	}
	actorID, err := util.ParseUUID(actor)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, ErrForbidden
	}
	return workspaceID, agentID, actorID, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
