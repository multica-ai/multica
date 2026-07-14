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
	"github.com/multica-ai/multica/server/internal/util"
)

const createIssuePlatformAction = "create_issue"

type platformActionAnswer struct {
	Allowed    bool
	Pending    bool
	ApprovalID pgtype.UUID
	Reason     string
}

func (h *Handler) authorizeCreateIssue(ctx context.Context, r *http.Request, workspaceID pgtype.UUID, creatorType, creatorID, surface string, actionContext map[string]any, resourcePayload any) platformActionAnswer {
	if creatorType != "agent" {
		return platformActionAnswer{Allowed: true}
	}
	agentID, err := util.ParseUUID(creatorID)
	if err != nil || h.PlatformActionGate == nil {
		return platformActionAnswer{Reason: "platform action gate unavailable"}
	}
	var taskID pgtype.UUID
	if raw := r.Header.Get("X-Task-ID"); raw != "" {
		taskID, _ = util.ParseUUID(raw)
	}
	result, err := h.PlatformActionGate.Authorize(ctx, platformaction.Request{
		WorkspaceID: workspaceID, AgentID: agentID, TaskID: taskID,
		Capability: createIssuePlatformAction, Resource: platformActionResource(resourcePayload),
		Surface: surface, Context: actionContext,
	})
	if err != nil {
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

func platformActionResource(payload any) string {
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return "request:" + hex.EncodeToString(sum[:])
}

func writeCreateIssuePlatformAction(w http.ResponseWriter, answer platformActionAnswer) {
	if answer.Pending {
		writeJSON(w, http.StatusAccepted, map[string]any{"code": "platform_action_pending", "capability": createIssuePlatformAction, "approval_id": uuidToString(answer.ApprovalID)})
		return
	}
	writeJSON(w, http.StatusForbidden, map[string]any{"code": "platform_action_denied", "capability": createIssuePlatformAction, "error": "Create issue is blocked by Permissions"})
}
