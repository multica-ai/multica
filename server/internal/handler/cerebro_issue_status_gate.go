package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	cerebroworkflows "github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cerebro_issue_status_gate.go (FIR-3659) is the handler-side seam for the
// before.issue.status_change hook gate. UpdateIssue calls
// cerebroGateIssueStatusChange via one marked CEREBRO-PATCH block; everything
// else lives here so the upstream diff stays minimal.
//
// Contract: only AGENT actors are ever gated — a member's status change always
// passes. With no gate wired (nil field) or the hook feature disabled
// (CEREBRO_WORKFLOW_HOOKS_ENABLED unset) the check is a no-op.

// IssueStatusChangeGate is the seam satisfied by
// *cerebroworkflows.IssueStatusGate; declared as an interface so handler tests
// can fake it.
type IssueStatusChangeGate interface {
	CheckIssueStatusChange(ctx context.Context, change cerebroworkflows.IssueStatusChange) (cerebroworkflows.IssueStatusDecision, error)
}

// cerebroGateIssueStatusChange evaluates hook policies for an attempted status
// change. Returns (0, "") to allow, or an HTTP status + message to reject.
// Engine errors fail open with a log line: the gate must never brick issue
// updates on infrastructure trouble (per-policy fail_mode already covers
// deliberate fail-closed behaviour inside the engine).
func (h *Handler) cerebroGateIssueStatusChange(r *http.Request, prevIssue db.Issue, newStatus *string, actorType, actorID string) (int, string) {
	if h.IssueStatusGate == nil || newStatus == nil {
		return 0, ""
	}
	if actorType != "agent" {
		return 0, ""
	}
	if prevIssue.Status == *newStatus {
		return 0, ""
	}
	decision, err := h.IssueStatusGate.CheckIssueStatusChange(r.Context(), cerebroworkflows.IssueStatusChange{
		WorkspaceID: uuidToString(prevIssue.WorkspaceID),
		ProjectID:   uuidToString(prevIssue.ProjectID),
		IssueID:     uuidToString(prevIssue.ID),
		ActorType:   actorType,
		ActorID:     actorID,
		FromStatus:  prevIssue.Status,
		ToStatus:    *newStatus,
		Nonce:       fmt.Sprintf("%d", time.Now().UnixNano()),
	})
	if err != nil {
		slog.Warn("issue status gate evaluation failed; allowing", append(logger.RequestAttrs(r),
			"error", err, "issue_id", uuidToString(prevIssue.ID))...)
		return 0, ""
	}
	if decision.Allowed {
		return 0, ""
	}
	return http.StatusUnprocessableEntity, fmt.Sprintf(
		"status change to %q was blocked by a workflow hook policy. Requirement: %s",
		*newStatus, decision.Requirement)
}
