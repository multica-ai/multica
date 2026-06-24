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
// CSC session bound to a workflow node-run, and what operations they may perform.
type SessionPermissionResponse struct {
	WorkspaceID string              `json:"workspace_id"`
	NodeRunID   string              `json:"node_run_id"`
	DeviceID    string              `json:"device_id"`
	SessionID   string              `json:"session_id"`
	Role        string              `json:"role"`
	CanControl  bool                `json:"can_control"`
	CanObserve  bool                `json:"can_observe"`
}

// GetSessionPermission resolves a CSC session_id to the bound workflow node-run
// and returns the authenticated user's runtime-level capabilities. This is the
// cross-system permission seam for Design Two: CoStrict Web asks cs-cloud to
// proxy a session, and cs-cloud asks Multica "is this user allowed, and what
// can they do?" before routing through the device tunnel.
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

	if !nodeRun.RuntimeID.Valid {
		writeError(w, http.StatusBadRequest, "session is not bound to a runtime")
		return
	}

	rt, err := h.Queries.GetAgentRuntime(ctx, nodeRun.RuntimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load runtime")
		return
	}

	userID := requestUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	member, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: rt.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "you do not have access to this workspace")
		return
	}

	explicitRole := ""
	perm, err := h.Queries.GetRuntimePermission(ctx, db.GetRuntimePermissionParams{
		RuntimeID: rt.ID,
		UserID:    member.UserID,
	})
	if err == nil {
		explicitRole = perm.Role
	}

	role := resolveRuntimeRole(member, rt, explicitRole)
	caps := runtimeCapabilities(role)
	if !caps.Observe {
		writeError(w, http.StatusForbidden, "you do not have permission to observe this session")
		return
	}

	resp := SessionPermissionResponse{
		WorkspaceID: uuidToString(rt.WorkspaceID),
		NodeRunID:   uuidToString(nodeRun.ID),
		DeviceID:    "",
		SessionID:   sessionID,
		Role:        string(role),
		CanControl:  caps.Control,
		CanObserve:  caps.Observe,
	}
	if nodeRun.DeviceID.Valid {
		resp.DeviceID = nodeRun.DeviceID.String
	}

	writeJSON(w, http.StatusOK, resp)
}
