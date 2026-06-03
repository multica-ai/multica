package handler

// CEREBRO-PATCH(repo-approval-cerebro): FIR-2586 — couple daemon repo checkout
// to the shared approval inbox.
//
// Before this file a repo checkout that resolved to "Ask" was a silent block:
// CheckDaemonRepoCapability returned allowed=false and the daemon failed the
// checkout, with nothing landing in the /approvals inbox. This file routes that
// "Ask" through the same permgate.Gate the agent tool loop and credential
// governance use, so every "ask" decision in the platform creates one inbox
// request via one seam.
//
// The flow is non-blocking on purpose. The daemon's HTTP client has a 30s
// timeout, so the server must NOT hold the /repo/check connection while a human
// decides. Instead CheckDaemonRepoCapability creates the ask and returns its id
// immediately ("decision":"pending"); the daemon long-polls
// PollDaemonRepoApproval until the ask reaches a terminal status.

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/permissions"
)

// repoCheckoutAsk resolves a repo checkout that hit "Ask" against the shared
// inbox and reports the outcome the daemon should act on: allowed plus a
// decision string ("allow" | "pending" | "deny") and the relevant approval id.
//
// It uses EvaluateDecisionReusing (non-blocking), so the daemon's short-timeout
// HTTP request is never held open AND a retried checkout does not pile up
// duplicate asks: an open ask for the same (agent, repo, capability) is rejoined
// ("pending" + its id), and an ask the human approved after the daemon's poll
// budget elapsed is honoured ("allow") on the retry instead of silently raising
// a fresh one. The requester is the agent attempting the checkout; the resource
// is the repo URL.
func (h *Handler) repoCheckoutAsk(ctx context.Context, wsID, agentID pgtype.UUID, repoURL, capability, reason string) (allowed bool, decision string, approvalID pgtype.UUID, err error) {
	req := permgate.Request{
		Permission: permissions.Request{
			WorkspaceID: wsID,
			Actor:       permissions.Actor{Type: "agent", ID: agentID},
			Agent:       agentID,
			Capability:  capability,
			Resource:    repoURL,
		},
		RequesterType: approvals.RequesterAgent,
		RequesterID:   agentID,
		Surface:       approvals.SurfaceSystem,
		Context: map[string]any{
			"repo_url":   repoURL,
			"capability": capability,
		},
	}
	res, err := h.ApprovalGate.EvaluateDecisionReusing(ctx, req, permissions.Decision{
		Kind:   permissions.DecisionNeedsApproval,
		Reason: reason,
	})
	if err != nil {
		return false, "", pgtype.UUID{}, err
	}
	switch res.Outcome {
	case permgate.OutcomeApproved, permgate.OutcomeAllowed:
		return true, "allow", res.ApprovalID, nil
	case permgate.OutcomePending:
		return false, "pending", res.ApprovalID, nil
	default:
		return false, "deny", res.ApprovalID, nil
	}
}

// PollDaemonRepoApproval reports the current status of a repo-checkout approval
// so the daemon can long-poll it. Single-shot and non-blocking: it returns the
// ask's status as it stands right now ("pending" while open, "allow" once
// approved, "deny" once rejected/expired/cancelled/timed-out). The daemon ticks
// this endpoint on its own schedule, bounded by its checkout timeout.
//
// GET /api/daemon/workspaces/{workspaceId}/repo/check/{approvalId}
func (h *Handler) PollDaemonRepoApproval(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")
	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	approvalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "approvalId"), "approval_id")
	if !ok {
		return
	}
	if h.ApprovalGate == nil {
		// Gate disabled: nothing to poll. Fail closed — the daemon treats a
		// non-allow as a blocked checkout.
		writeJSON(w, http.StatusOK, map[string]any{"allowed": false, "decision": "deny", "reason": "approval gate disabled"})
		return
	}

	outcome, err := h.ApprovalGate.Status(r.Context(), wsUUID, approvalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "approval status lookup failed")
		return
	}

	allowed, decision, reason := repoApprovalOutcomeToAnswer(outcome)
	writeJSON(w, http.StatusOK, map[string]any{
		"allowed":  allowed,
		"decision": decision,
		"reason":   reason,
	})
}

// repoApprovalOutcomeToAnswer maps a gate outcome to the daemon's
// allowed/decision/reason answer. Pending stays pending so the daemon keeps
// waiting; every non-approved terminal outcome is a deny (fail-closed).
func repoApprovalOutcomeToAnswer(outcome permgate.Outcome) (bool, string, string) {
	switch outcome {
	case permgate.OutcomeApproved, permgate.OutcomeAllowed:
		return true, "allow", ""
	case permgate.OutcomePending:
		return false, "pending", "awaiting approval"
	default:
		return false, "deny", "approval " + string(outcome)
	}
}
