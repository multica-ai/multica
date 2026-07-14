package platformaction

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type Request struct {
	WorkspaceID  pgtype.UUID
	AgentID      pgtype.UUID
	TaskID       pgtype.UUID
	OnBehalfOfID pgtype.UUID
	SystemID     pgtype.UUID
	Capability   string
	Resource     string
	Surface      string
	Context      map[string]any
	IsSystem     bool
}

// Gate is the server-owned permission floor shared by platform mutation surfaces.
type Gate struct {
	Policy    *toolpolicy.Store
	Queries   *db.Queries
	Approvals *permgate.Gate
}

func New(policy *toolpolicy.Store, queries *db.Queries, approvalGate *permgate.Gate) *Gate {
	return &Gate{Policy: policy, Queries: queries, Approvals: approvalGate}
}

func NewDefault(policy *toolpolicy.Store, queries *db.Queries, cerebro *cerebrodb.Queries, tx approvals.TxStarter, bus *events.Bus) *Gate {
	return New(policy, queries, &permgate.Gate{Approvals: approvals.New(cerebro, tx, bus)})
}

func (g *Gate) Authorize(ctx context.Context, in Request) (permgate.Result, error) {
	if g == nil || g.Policy == nil || g.Queries == nil || !in.AgentID.Valid || !in.WorkspaceID.Valid {
		return denied("platform action gate not configured"), errors.New("platformaction: gate not configured")
	}
	agent, err := g.Queries.GetAgent(ctx, in.AgentID)
	if err != nil || agent.WorkspaceID != in.WorkspaceID {
		return denied("agent identity lookup failed"), errors.New("platformaction: invalid agent identity")
	}
	onBehalfOfID, systemID, isSystem := in.OnBehalfOfID, in.SystemID, in.IsSystem
	if in.TaskID.Valid {
		task, taskErr := g.Queries.GetAgentTask(ctx, in.TaskID)
		if taskErr != nil || task.AgentID != in.AgentID {
			return denied("agent task identity lookup failed"), errors.New("platformaction: invalid agent task identity")
		}
		onBehalfOfID = task.OriginalUserID
		if !task.OriginalUserID.Valid && task.AutopilotRunID.Valid {
			isSystem = true
			systemID = task.AutopilotRunID
		}
	}

	effective, err := g.Policy.ResolveGeneral(ctx, toolpolicy.Query{
		WorkspaceID: in.WorkspaceID, ToolKey: in.Capability, RuntimeID: agent.RuntimeID,
		AgentID: in.AgentID, UserID: agent.OwnerID, OnBehalfOfID: onBehalfOfID,
		SystemID: systemID, Base: toolpolicy.SettingAllow, IsSystem: isSystem,
		RequestContext: toolpolicy.RequestContext{Action: toolpolicy.ActionOf(in.Capability)},
	}, g.Policy.MemberOverrideEnabled(ctx, in.WorkspaceID))
	if err != nil {
		return denied("permission lookup failed"), err
	}
	g.Policy.RecordUsage(ctx, toolpolicy.UsageParams{
		WorkspaceID: in.WorkspaceID, ToolKey: in.Capability, EnforcementPoint: in.Surface,
		SubjectType: "agent", SubjectID: in.AgentID, Resource: in.Resource,
		Decision: effective.Setting, DecidedBy: string(effective.DecidedBy),
	})

	decision := decisionFor(effective)
	if decision.Kind == permissions.DecisionAllow {
		return permgate.Result{Outcome: permgate.OutcomeAllowed, Decision: decision, Reason: decision.Reason}, nil
	}
	if decision.Kind == permissions.DecisionDeny {
		return permgate.Result{Outcome: permgate.OutcomeDenied, Decision: decision, Reason: decision.Reason}, nil
	}
	if isSystem {
		return denied("system actions cannot await approval"), nil
	}
	if g.Approvals == nil {
		return denied("approval gate not configured"), errors.New("platformaction: approval gate not configured")
	}
	return g.Approvals.EvaluateDecisionReusing(ctx, permgate.Request{
		Permission:    permissions.Request{WorkspaceID: in.WorkspaceID, Actor: permissions.Actor{Type: "agent", ID: in.AgentID}, Agent: in.AgentID, Capability: in.Capability, Resource: in.Resource},
		RequesterType: approvals.RequesterAgent, RequesterID: in.AgentID, Surface: in.Surface, Context: in.Context,
	}, decision)
}

func decisionFor(effective toolpolicy.Effective) permissions.Decision {
	switch effective.Setting {
	case toolpolicy.SettingAllow:
		return permissions.Decision{Kind: permissions.DecisionAllow, Reason: effective.Reason}
	case toolpolicy.SettingAsk:
		return permissions.Decision{Kind: permissions.DecisionNeedsApproval, Reason: effective.Reason}
	default:
		return permissions.Decision{Kind: permissions.DecisionDeny, Reason: effective.Reason}
	}
}

func denied(reason string) permgate.Result {
	return permgate.Result{Outcome: permgate.OutcomeDenied, Reason: reason, Decision: permissions.Decision{Kind: permissions.DecisionDeny, Reason: reason}}
}
