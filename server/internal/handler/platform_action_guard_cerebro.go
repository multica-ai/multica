package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaction"
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
	ApprovalID    pgtype.UUID
	Reason        string
}

func (h *Handler) authorizePlatformAction(ctx context.Context, r *http.Request, workspaceID pgtype.UUID, actorType, actorID, capability, surface string, actionContext map[string]any, resourcePayload any, wait bool) platformActionAnswer {
	if actorType != "agent" {
		return platformActionAnswer{Allowed: true}
	}
	agentID, err := util.ParseUUID(actorID)
	if err != nil || h.PlatformActionGate == nil {
		return platformActionAnswer{Reason: "platform action gate unavailable"}
	}
	var taskID pgtype.UUID
	if raw := r.Header.Get("X-Task-ID"); raw != "" {
		taskID, err = util.ParseUUID(raw)
	}
	if err != nil || !taskID.Valid {
		return platformActionAnswer{MandateDenied: true, Reason: "valid task identity is required"}
	}
	if err := taskmandate.NewStoreDB(h.DB).Authorize(ctx, taskID, workspaceID, agentID, capability); err != nil {
		return platformActionAnswer{MandateDenied: true, Reason: err.Error()}
	}
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
		writeJSON(w, http.StatusForbidden, map[string]any{"code": "task_mandate_denied", "capability": capability, "error": "Action is outside Task Mandate"})
		return
	}
	writeJSON(w, http.StatusForbidden, map[string]any{"code": "platform_action_denied", "capability": capability, "error": "Action is blocked by Permissions"})
}

func writeCreateIssuePlatformAction(w http.ResponseWriter, answer platformActionAnswer) {
	writePlatformAction(w, createIssuePlatformAction, answer)
}
