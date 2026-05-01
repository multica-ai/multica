package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork-specific: workspace-level kill switch endpoints.
// Kept in a dedicated file so upstream-merges don't conflict on workspace.go.

type pauseWorkspaceTasksRequest struct {
	Paused bool `json:"paused"`
}

type pauseWorkspaceTasksResponse struct {
	Workspace       WorkspaceResponse `json:"workspace"`
	CancelledCount  int               `json:"cancelled_count"`
}

// PauseWorkspaceTasks toggles the workspace kill switch.
//
// When `paused: true`, every queued/dispatched/running task in the workspace
// is cancelled and new task creation is blocked until the switch is released.
// When `paused: false`, the switch is released; cancelled tasks stay cancelled
// and new triggers (assignments, mentions, chats) work normally again.
//
// Mounted under the workspace admin route group; middleware enforces owner/admin.
func (h *Handler) PauseWorkspaceTasks(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID := parseUUID(workspaceID)

	var req pauseWorkspaceTasksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cancelled := 0
	if req.Paused {
		count, err := h.TaskService.PauseWorkspaceTasks(r.Context(), wsUUID)
		if err != nil {
			slog.Error("pause workspace tasks failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to pause workspace tasks")
			return
		}
		cancelled = count
	}

	ws, err := h.Queries.SetWorkspaceTasksPaused(r.Context(), db.SetWorkspaceTasksPausedParams{
		WorkspaceID: wsUUID,
		Paused:      req.Paused,
	})
	if err != nil {
		slog.Error("set workspace paused flag failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update workspace")
		return
	}

	slog.Info("workspace kill switch toggled",
		append(logger.RequestAttrs(r),
			"workspace_id", workspaceID,
			"paused", req.Paused,
			"cancelled_count", cancelled,
		)...)

	userID := requestUserID(r)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"workspace": workspaceToResponse(ws)})

	writeJSON(w, http.StatusOK, pauseWorkspaceTasksResponse{
		Workspace:      workspaceToResponse(ws),
		CancelledCount: cancelled,
	})
}
