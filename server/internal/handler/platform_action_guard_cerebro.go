package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaction"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	createIssuePlatformAction = "create_issue"
	updateIssuePlatformAction = "update_issue"
	addCommentPlatformAction  = "add_comment"
)

type platformActionAnswer struct {
	Allowed       bool
	Pending       bool
	MandateDenied bool
	Verdict       *taskmandate.Verdict
	ApprovalID    pgtype.UUID
	Reason        string
}

func (h *Handler) authorizePlatformAction(ctx context.Context, r *http.Request, workspaceID pgtype.UUID, actorType, actorID, capability, surface string, actionContext map[string]any, resourcePayload any, wait bool) platformActionAnswer {
	return h.authorizePlatformActionWithMandate(ctx, r, workspaceID, actorType, actorID, capability, surface, actionContext, resourcePayload, wait, true)
}

// authorizePlatformActionWithMandate is the shared implementation. A capability
// with registered ToolBindings is checked through the request's exact callable
// identity; a broad family key can never stand in for one of those bindings.
// Capabilities without ToolBindings remain governed only by the policy engine.
func (h *Handler) authorizePlatformActionWithMandate(ctx context.Context, r *http.Request, workspaceID pgtype.UUID, actorType, actorID, capability, surface string, actionContext map[string]any, resourcePayload any, wait, requireMandate bool) platformActionAnswer {
	if actorType != "agent" {
		return platformActionAnswer{Allowed: true}
	}
	agentID, err := util.ParseUUID(actorID)
	if err != nil || h.PlatformActionGate == nil {
		return platformActionAnswer{Reason: "platform action gate unavailable"}
	}
	mandate := h.authorizeExactPlatformTaskMandate(ctx, r, workspaceID, agentID, capability, requireMandate)
	if !mandate.Allowed {
		return mandate
	}
	taskID := optionalTaskID(r)
	var approvalID pgtype.UUID
	if raw := r.Header.Get("X-Platform-Approval-ID"); raw != "" {
		approvalID, err = util.ParseUUID(raw)
		if err != nil {
			return platformActionAnswer{Reason: "invalid platform approval id"}
		}
	}
	request := platformaction.Request{
		WorkspaceID: workspaceID, AgentID: agentID, TaskID: taskID,
		Capability: capability, Resource: platformActionResource(resourcePayload),
		Surface: surface, Context: actionContext, ApprovalID: approvalID,
	}
	var result permgate.Result
	if wait {
		result, err = h.PlatformActionGate.AuthorizeAndWait(ctx, request)
	} else {
		result, err = h.PlatformActionGate.Authorize(ctx, request)
	}
	if err != nil {
		if result.ApprovalID.Valid && result.Outcome == permgate.OutcomeTimedOut {
			return platformActionAnswer{Pending: true, ApprovalID: result.ApprovalID, Reason: result.Reason}
		}
		return platformActionAnswer{Reason: err.Error()}
	}
	switch result.Outcome {
	case permgate.OutcomeAllowed, permgate.OutcomeApproved:
		return platformActionAnswer{Allowed: true, ApprovalID: result.ApprovalID}
	case permgate.OutcomePending:
		return platformActionAnswer{Pending: true, ApprovalID: result.ApprovalID, Reason: result.Reason}
	default:
		return platformActionAnswer{Reason: result.Reason}
	}
}

func (h *Handler) authorizeExactPlatformTaskMandate(ctx context.Context, r *http.Request, workspaceID, agentID pgtype.UUID, capability string, required bool) platformActionAnswer {
	if !required || !h.taskMandateEnforcementEnabled(ctx, workspaceID) {
		return platformActionAnswer{Allowed: true}
	}
	taskID := optionalTaskID(r)
	if !taskID.Valid {
		verdict := taskmandate.VerdictForError(taskmandate.ErrMissing)
		return platformActionAnswer{MandateDenied: true, Verdict: &verdict, Reason: "valid task identity is required"}
	}
	callable, ok := exactPlatformCallable(r, capability)
	if !ok {
		verdict := taskmandate.VerdictForError(taskmandate.ErrToolDeny)
		return platformActionAnswer{MandateDenied: true, Verdict: &verdict, Reason: "exact platform callable identity is required"}
	}
	generation, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("X-Task-Mandate-Generation")), 10, 64)
	if err != nil || generation <= 0 {
		verdict := taskmandate.VerdictForError(taskmandate.ErrStaleClaimGeneration)
		return platformActionAnswer{MandateDenied: true, Verdict: &verdict, Reason: taskmandate.ErrStaleClaimGeneration.Error()}
	}
	if err := taskmandate.NewStoreDB(h.DB).AuthorizeClaimGeneration(ctx, taskID, workspaceID, agentID, generation, callable); err != nil {
		verdict := taskmandate.VerdictForError(err)
		return platformActionAnswer{MandateDenied: true, Verdict: &verdict, Reason: err.Error()}
	}
	return platformActionAnswer{Allowed: true}
}

func optionalTaskID(r *http.Request) pgtype.UUID {
	if r == nil {
		return pgtype.UUID{}
	}
	taskID, _ := util.ParseUUID(strings.TrimSpace(r.Header.Get("X-Task-ID")))
	return taskID
}

func exactPlatformCallable(r *http.Request, capability string) (string, bool) {
	callable := strings.TrimSpace(r.Header.Get("X-Multica-Callable"))
	expected := platformRouteCallables(r, capability)
	if len(expected) == 0 {
		return "", false
	}
	binding, ok := platformcatalog.ByToolBinding(callable)
	if !ok || binding.Key != capability {
		return "", false
	}
	for _, allowed := range expected {
		if callable == allowed {
			return callable, true
		}
	}
	return "", false
}

func (h *Handler) authorizeCreateIssue(ctx context.Context, r *http.Request, workspaceID pgtype.UUID, creatorType, creatorID, surface string, actionContext map[string]any, resourcePayload any, wait bool) platformActionAnswer {
	return h.authorizePlatformAction(ctx, r, workspaceID, creatorType, creatorID, createIssuePlatformAction, surface, actionContext, resourcePayload, wait)
}

func platformActionResource(payload any) string {
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return "request:" + hex.EncodeToString(sum[:])
}

func writePlatformAction(w http.ResponseWriter, capability string, answer platformActionAnswer) {
	if answer.Pending {
		writeJSON(w, http.StatusAccepted, map[string]any{"code": "platform_action_pending", "capability": capability, "approval_id": uuidToString(answer.ApprovalID)})
		return
	}
	if answer.MandateDenied {
		verdict := answer.Verdict
		if verdict == nil {
			fallback := taskmandate.VerdictForError(taskmandate.ErrToolDeny)
			verdict = &fallback
		}
		writeJSON(w, http.StatusForbidden, map[string]any{"code": "task_mandate_denied", "capability": capability, "error": "Action is outside Task Mandate", "verdict": verdict})
		return
	}
	writeJSON(w, http.StatusForbidden, map[string]any{"code": "platform_action_denied", "capability": capability, "error": "Action is blocked by Permissions"})
}

func writeCreateIssuePlatformAction(w http.ResponseWriter, answer platformActionAnswer) {
	writePlatformAction(w, createIssuePlatformAction, answer)
}
