package platformaction

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaccess"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type Request struct {
	WorkspaceID  pgtype.UUID
	AgentID      pgtype.UUID
	MemberID     pgtype.UUID
	TaskID       pgtype.UUID
	OnBehalfOfID pgtype.UUID
	SystemID     pgtype.UUID
	Capability   string
	Resource     string
	Surface      string
	Context      map[string]any
	ApprovalID   pgtype.UUID
	IsSystem     bool
}

type approvalConsumer interface {
	ConsumeApproved(context.Context, approvals.ConsumeParams) (cerebrodb.CerebroApprovalRequest, error)
}

// Gate is the server-owned permission floor shared by platform mutation surfaces.
type Gate struct {
	Policy    *toolpolicy.Store
	Queries   *db.Queries
	Approvals *permgate.Gate
	Consumer  approvalConsumer
}

func New(policy *toolpolicy.Store, queries *db.Queries, approvalGate *permgate.Gate) *Gate {
	return &Gate{Policy: policy, Queries: queries, Approvals: approvalGate}
}

func NewDefault(policy *toolpolicy.Store, queries *db.Queries, cerebro *cerebrodb.Queries, tx approvals.TxStarter, bus *events.Bus) *Gate {
	svc := approvals.New(cerebro, tx, bus)
	return &Gate{Policy: policy, Queries: queries, Approvals: &permgate.Gate{Approvals: svc}, Consumer: svc}
}

func (g *Gate) Authorize(ctx context.Context, in Request) (permgate.Result, error) {
	if g == nil || g.Policy == nil || g.Queries == nil || !in.WorkspaceID.Valid || (!in.AgentID.Valid && !in.MemberID.Valid) {
		return denied("platform action gate not configured"), errors.New("platformaction: gate not configured")
	}
	if in.MemberID.Valid {
		member, err := g.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: in.MemberID, WorkspaceID: in.WorkspaceID})
		if err != nil {
			return denied("member identity lookup failed"), errors.New("platformaction: invalid member identity")
		}
		if member.Role == "owner" || member.Role == "admin" {
			return permgate.Result{Outcome: permgate.OutcomeAllowed, Reason: "workspace owner or administrator"}, nil
		}
		effective, err := g.Policy.ResolvePermission(ctx, toolpolicy.Query{
			WorkspaceID: in.WorkspaceID, ToolKey: in.Capability, UserID: in.MemberID,
			Base: toolpolicy.SettingAllow, RequestContext: toolpolicy.RequestContext{Action: toolpolicy.ActionOf(in.Capability)},
		}, platformaccess.Actor{Authenticated: true})
		if err != nil {
			return denied("permission lookup failed"), err
		}
		g.Policy.RecordUsage(ctx, toolpolicy.UsageParams{
			WorkspaceID: in.WorkspaceID, ToolKey: in.Capability, EnforcementPoint: in.Surface,
			SubjectType: "member", SubjectID: in.MemberID, Resource: in.Resource,
			Decision: effective.Setting, DecidedBy: string(effective.DecidedBy),
		})
		decision := decisionFor(effective)
		if decision.Kind == permissions.DecisionAllow {
			return permgate.Result{Outcome: permgate.OutcomeAllowed, Decision: decision, Reason: decision.Reason}, nil
		}
		return permgate.Result{Outcome: permgate.OutcomeDenied, Decision: decision, Reason: decision.Reason}, nil
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
		in.Context = authoritativeTaskContext(ctx, g.Queries, in.Context, task)
	}

	effective, err := g.Policy.ResolvePermission(ctx, toolpolicy.Query{
		WorkspaceID: in.WorkspaceID, ToolKey: in.Capability, RuntimeID: agent.RuntimeID,
		AgentID: in.AgentID, UserID: agent.OwnerID, OnBehalfOfID: onBehalfOfID,
		SystemID: systemID, Base: toolpolicy.SettingAllow, IsSystem: isSystem,
		RequestContext: toolpolicy.RequestContext{Action: toolpolicy.ActionOf(in.Capability)},
	}, platformaccess.Actor{Authenticated: true, Agent: true})
	if err != nil {
		return denied("permission lookup failed"), err
	}
	g.Policy.RecordUsage(ctx, toolpolicy.UsageParams{
		WorkspaceID: in.WorkspaceID, ToolKey: in.Capability, EnforcementPoint: in.Surface,
		SubjectType: "agent", SubjectID: in.AgentID, Resource: in.Resource,
		Decision: effective.Setting, DecidedBy: string(effective.DecidedBy),
	})

	decision := decisionFor(effective)
	if decision.Kind == permissions.DecisionDeny {
		return permgate.Result{Outcome: permgate.OutcomeDenied, Decision: decision, Reason: decision.Reason}, nil
	}
	if in.ApprovalID.Valid {
		if isSystem {
			return denied("system actions cannot consume approval"), nil
		}
		if g.Consumer == nil {
			return denied("approval consumer not configured"), errors.New("platformaction: approval consumer not configured")
		}
		row, consumeErr := g.Consumer.ConsumeApproved(ctx, approvals.ConsumeParams{
			ID: in.ApprovalID, WorkspaceID: in.WorkspaceID, Agent: in.AgentID,
			Capability: in.Capability, Resource: in.Resource,
		})
		if consumeErr != nil {
			if errors.Is(consumeErr, approvals.ErrNotConsumable) {
				return denied("approval is rejected, expired, mismatched, or already consumed"), nil
			}
			return denied("approval consumption failed"), consumeErr
		}
		return permgate.Result{Outcome: permgate.OutcomeApproved, Decision: decision, ApprovalID: row.ID, Reason: row.Reason}, nil
	}
	if decision.Kind == permissions.DecisionAllow {
		return permgate.Result{Outcome: permgate.OutcomeAllowed, Decision: decision, Reason: decision.Reason}, nil
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

// AuthorizeAndWait keeps an in-process tool call suspended until its exact
// approval is decided, then consumes that approval before the mutation runs.
func (g *Gate) AuthorizeAndWait(ctx context.Context, in Request) (permgate.Result, error) {
	result, err := g.Authorize(ctx, in)
	if err != nil || result.Outcome != permgate.OutcomePending {
		return result, err
	}
	outcome, waitErr := g.Approvals.Await(ctx, in.WorkspaceID, result.ApprovalID)
	if waitErr != nil || outcome != permgate.OutcomeApproved {
		result.Outcome = outcome
		return result, waitErr
	}
	in.ApprovalID = result.ApprovalID
	return g.Authorize(ctx, in)
}

func authoritativeTaskContext(ctx context.Context, queries *db.Queries, supplied map[string]any, task db.AgentTaskQueue) map[string]any {
	out := make(map[string]any, len(supplied)+5)
	for key, value := range supplied {
		out[key] = value
	}
	delete(out, "issue_id")
	delete(out, "chat_session_id")
	delete(out, "trigger_comment_id")
	out["task_id"] = uuidString(task.ID)
	surface := "issue"
	if task.ChatSessionID.Valid {
		out["chat_session_id"] = uuidString(task.ChatSessionID)
		surface = "chat"
	} else if task.IssueID.Valid {
		out["issue_id"] = uuidString(task.IssueID)
		if issue, err := queries.GetIssue(ctx, task.IssueID); err == nil {
			switch issue.Kind {
			case "channel", "dm":
				surface = issue.Kind
			}
		}
	} else if task.AutopilotRunID.Valid {
		surface = "autopilot"
	}
	if task.TriggerCommentID.Valid {
		out["trigger_comment_id"] = uuidString(task.TriggerCommentID)
	}
	out["surface"] = surface
	return out
}

func uuidString(id pgtype.UUID) string {
	return util.UUIDToString(id)
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
