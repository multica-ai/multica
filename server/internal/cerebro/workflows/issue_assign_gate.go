package workflows

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// issue_assign_gate.go dispatches the before.issue.assigned hook event
// (FIR-4183), so the platform checks whether an issue on the same matter
// already exists instead of hoping every agent remembers to look.
//
// The gate is deliberately thin: it builds the event, lets the engine run
// whatever policies the workspace has bound to it, and translates the outcome
// into "start this agent" / "do not start this agent yet".
//
// Two ways in, and the difference matters:
//
//   - Attach(bus) — the whole trigger lives inside the workflow engine. The
//     issue:updated broadcast already carries assignee_changed, so no upstream
//     call site is needed. It fires just AFTER the assignee is written, which
//     is early enough to link and comment but too late to hold the agent back.
//   - CheckIssueAssignment — called synchronously from wherever a task is
//     enqueued for an agent. This is the only path that can actually stop a
//     hand-over, and the only one that needs a line outside this package.

// IssueAssignment describes one issue about to be handed to an agent.
type IssueAssignment struct {
	WorkspaceID string
	ProjectID   string
	IssueID     string
	AgentID     string
	ActorType   string
	ActorID     string
	// Reason is why the task is being enqueued ("assignment", "comment",
	// "wakeup"); policies can condition on it.
	Reason string
	// Nonce uniquifies the idempotency key. A second hand-over of the same
	// issue must be evaluated against the issue as it is now, not served the
	// first run's cached verdict.
	Nonce string
}

// IssueAssignDecision is the gate's verdict. Allowed=false means the agent
// should not be started yet; Reason says why, in words meant for a human
// reading the issue.
type IssueAssignDecision struct {
	Allowed bool
	Reason  string
	// Blocked reports that a hook ACTION (issue.check_related finding a
	// duplicate) asked for the stop, as opposed to a policy handler decision.
	Blocked bool
}

// IssueAssignGate evaluates hook policies for issue hand-overs. Nil-safe: a nil
// gate or nil engine allows everything, and the engine returns allow when
// CEREBRO_WORKFLOW_HOOKS_ENABLED is off — so the feature stays dark until a
// workspace deliberately binds a policy to the event.
type IssueAssignGate struct {
	engine HookEvaluator
}

func NewIssueAssignGate(engine HookEvaluator) *IssueAssignGate {
	return &IssueAssignGate{engine: engine}
}

// Attach subscribes the gate to the issue:updated broadcast, which is what
// makes the trigger self-contained: the payload already reports whether the
// assignee changed and who it changed to, so nothing outside this package has
// to call in. Safe to call once at server startup.
//
// This path never stops a hand-over — by the time the broadcast lands the
// assignee is written and the task may already be queued — so the verdict is
// logged, not enforced. What it does deliver is the actual point of the
// feature: the related links and the comment are on the issue by the time the
// agent reads it.
func (g *IssueAssignGate) Attach(bus *events.Bus) {
	if g == nil || bus == nil {
		return
	}
	bus.Subscribe(protocol.EventIssueUpdated, g.onIssueUpdated)
}

func (g *IssueAssignGate) onIssueUpdated(e events.Event) {
	if g == nil || g.engine == nil {
		return
	}
	payload, ok := payloadToMap(e.Payload)
	if !ok {
		return
	}
	if assigneeChanged, _ := payload["assignee_changed"].(bool); !assigneeChanged {
		return
	}
	issue, ok := payload["issue"].(map[string]any)
	if !ok {
		return
	}
	// Only agent hand-overs. Assigning a human, or clearing the assignee, is
	// not the moment this feature is about.
	if assigneeType, _ := issue["assignee_type"].(string); assigneeType != "agent" {
		return
	}
	agentID, _ := issue["assignee_id"].(string)
	issueID, _ := issue["id"].(string)
	if agentID == "" || issueID == "" {
		return
	}
	projectID, _ := issue["project_id"].(string)

	var nonce [8]byte
	_, _ = rand.Read(nonce[:])

	decision, err := g.CheckIssueAssignment(context.Background(), IssueAssignment{
		WorkspaceID: e.WorkspaceID,
		ProjectID:   projectID,
		IssueID:     issueID,
		AgentID:     agentID,
		Reason:      "assignment",
		Nonce:       hex.EncodeToString(nonce[:]),
	})
	if err != nil {
		slog.Warn("before.issue.assigned dispatch failed",
			"issue_id", issueID,
			"workspace_id", e.WorkspaceID,
			"error", err,
		)
		return
	}
	if !decision.Allowed {
		slog.Info("before.issue.assigned wanted to hold the hand-over",
			"issue_id", issueID,
			"workspace_id", e.WorkspaceID,
			"reason", decision.Reason,
		)
	}
}

// CheckIssueAssignment evaluates the hand-over. Only agent assignees are gated.
// Engine errors allow the hand-over: a hook transport problem must never stop
// the platform from putting agents to work.
func (g *IssueAssignGate) CheckIssueAssignment(ctx context.Context, assignment IssueAssignment) (IssueAssignDecision, error) {
	allowed := IssueAssignDecision{Allowed: true}
	if g == nil || g.engine == nil {
		return allowed, nil
	}
	if strings.TrimSpace(assignment.IssueID) == "" || strings.TrimSpace(assignment.AgentID) == "" {
		return allowed, nil
	}

	reason := assignment.Reason
	if reason == "" {
		reason = "assignment"
	}
	event := HookEvent{
		EventID:     fmt.Sprintf("%s:assigned:%s:%s", assignment.IssueID, assignment.AgentID, assignment.Nonce),
		Type:        HookBeforeIssueAssigned,
		WorkspaceID: assignment.WorkspaceID,
		ProjectID:   assignment.ProjectID,
		IssueID:     assignment.IssueID,
		AgentID:     assignment.AgentID,
		Actor:       HookActor{Type: assignment.ActorType, ID: assignment.ActorID},
		Context: map[string]any{
			"assignment": map[string]any{"agent_id": assignment.AgentID, "reason": reason},
			"actor":      map[string]any{"type": assignment.ActorType, "id": assignment.ActorID},
		},
	}

	result, err := g.engine.Evaluate(ctx, event)
	if err != nil {
		return allowed, err
	}

	// An action can ask for the stop even when the policy handler itself only
	// says "allow" — that is how issue.check_related stops a hand-over on a
	// duplicate it just discovered, without blocking every other assignment
	// the same policy matches.
	for _, action := range result.ActionResults {
		if action.Type != CheckRelatedActionType || action.Status != HookActionSuccess {
			continue
		}
		if blocked, _ := action.Result[ResultKeyBlocked].(bool); blocked {
			return IssueAssignDecision{
				Allowed: false,
				Blocked: true,
				Reason:  "An issue on the same matter already exists — see the related issues on this issue.",
			}, nil
		}
	}

	if result.Decision != HookBlock && result.Decision != HookRequire {
		return allowed, nil
	}
	reasonText := result.ReasonWithHook(strings.Join(result.Requirements, " "))
	if strings.TrimSpace(reasonText) == "" {
		reasonText = "This hand-over was blocked by a workflow hook policy."
	}
	return IssueAssignDecision{Allowed: false, Blocked: true, Reason: reasonText}, nil
}
