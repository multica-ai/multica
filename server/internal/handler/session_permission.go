package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SessionPermissionResponse is returned by GET /api/sessions/{sessionId}/permission.
// cs-cloud uses it to decide whether a Casdoor-authenticated user may access the
// CSC session bound to a workflow node-run.
type SessionPermissionResponse struct {
	WorkspaceID string `json:"workspace_id"`
	NodeRunID   string `json:"node_run_id"`
	DeviceID    string `json:"device_id"`
	SessionID   string `json:"session_id"`
	HasAccess   bool   `json:"has_access"`
}

// GetSessionPermission resolves a CSC session_id to the bound workflow node-run
// and checks whether the authenticated user has access to that workspace.
// This is the cross-system permission seam for Design Two: CoStrict Web asks
// cs-cloud to proxy a session, and cs-cloud asks Multica "is this user allowed?"
// before routing through the device tunnel.
func (h *Handler) GetSessionPermission(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id required")
		return
	}

	nodeRun, err := h.Queries.GetWorkflowNodeRunBySessionID(ctx, pgtype.Text{String: sessionID, Valid: true})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "session not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	run, err := h.Queries.GetWorkflowRun(ctx, nodeRun.WorkflowRunID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	workflow, err := h.Queries.GetWorkflow(ctx, run.WorkflowID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	userID := requestUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	_, err = h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: workflow.WorkspaceID,
	})
	hasAccess := err == nil

	resp := SessionPermissionResponse{
		WorkspaceID: uuidToString(workflow.WorkspaceID),
		NodeRunID:   uuidToString(nodeRun.ID),
		DeviceID:    "",
		SessionID:   sessionID,
		HasAccess:   hasAccess,
	}
	if nodeRun.DeviceID.Valid {
		resp.DeviceID = nodeRun.DeviceID.String
	}

	writeJSON(w, http.StatusOK, resp)
}
