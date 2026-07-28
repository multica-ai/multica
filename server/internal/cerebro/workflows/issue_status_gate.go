package workflows

import (
	"context"
	"fmt"
	"strings"
)

// issue_status_gate.go dispatches the before.issue.status_change hook event
// (FIR-3659). The event type has existed in the catalog since FIR-3101 but was
// never fired; this gate is the wiring. It is invoked from the upstream
// UpdateIssue handler (via the cerebro sibling file in the handler package)
// for AGENT actors only — members are never gated, by design.
//
// A policy listening on before.issue.status_change with a block/require
// handler turns a matching status change into a 4xx for the agent, carrying
// the handler's Requirement text so the agent can self-correct (the workpad
// start-gate: "no in_progress without a workpad in the description").

// IssueStatusChange describes one attempted status transition.
type IssueStatusChange struct {
	WorkspaceID string
	ProjectID   string
	IssueID     string
	ActorType   string
	ActorID     string
	FromStatus  string
	ToStatus    string
	// Nonce uniquifies the engine's idempotency key so every attempt is
	// re-evaluated against the CURRENT issue state — a blocked agent that
	// fixes the issue and retries must not be served the cached block.
	Nonce string
}

// IssueStatusDecision is the gate's verdict. Requirement is only set when
// Allowed is false and tells the agent exactly what to do before retrying.
type IssueStatusDecision struct {
	Allowed     bool
	Requirement string
}

// IssueStatusGate evaluates hook policies for issue status changes. Nil-safe:
// a nil gate or nil engine allows everything, and the engine itself returns
// allow when CEREBRO_WORKFLOW_HOOKS_ENABLED is off — the feature is dark
// until deliberately enabled.
type IssueStatusGate struct {
	engine HookEvaluator
}

func NewIssueStatusGate(engine HookEvaluator) *IssueStatusGate {
	return &IssueStatusGate{engine: engine}
}

// CheckIssueStatusChange evaluates the change against workspace hook policies.
// Only agent actors are gated; anything else is allowed outright. Engine
// errors allow the change (matching the completion gate's store-error
// behaviour): per-policy fail_mode already governs action failures inside the
// engine, and a transport error must not brick issue updates for everyone.
func (g *IssueStatusGate) CheckIssueStatusChange(ctx context.Context, change IssueStatusChange) (IssueStatusDecision, error) {
	allowed := IssueStatusDecision{Allowed: true}
	if g == nil || g.engine == nil {
		return allowed, nil
	}
	if change.ActorType != "agent" {
		return allowed, nil
	}
	if change.FromStatus == change.ToStatus || change.ToStatus == "" {
		return allowed, nil
	}
	agentID := ""
	if change.ActorType == "agent" {
		agentID = change.ActorID
	}
	event := HookEvent{
		EventID:     fmt.Sprintf("%s:status:%s>%s:%s", change.IssueID, change.FromStatus, change.ToStatus, change.Nonce),
		Type:        HookBeforeIssueStatus,
		WorkspaceID: change.WorkspaceID,
		ProjectID:   change.ProjectID,
		IssueID:     change.IssueID,
		AgentID:     agentID,
		Actor:       HookActor{Type: change.ActorType, ID: change.ActorID},
		Context: map[string]any{
			"status": map[string]any{"from": change.FromStatus, "to": change.ToStatus},
			"actor":  map[string]any{"type": change.ActorType, "id": change.ActorID},
		},
	}
	result, err := g.engine.Evaluate(ctx, event)
	if err != nil {
		return allowed, err
	}
	if result.Decision != HookBlock && result.Decision != HookRequire {
		return allowed, nil
	}
	requirement := strings.Join(result.Requirements, " ")
	if strings.TrimSpace(requirement) == "" {
		requirement = "This status change was blocked by a workflow hook policy."
	}
	return IssueStatusDecision{Allowed: false, Requirement: requirement}, nil
}
