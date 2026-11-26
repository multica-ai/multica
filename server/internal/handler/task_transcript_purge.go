package handler

import (
	"log/slog"
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TaskTranscriptPurgeResponse is the record a purge leaves on the run.
type TaskTranscriptPurgeResponse struct {
	TranscriptPurgedAt string `json:"transcript_purged_at"`
	TranscriptPurgedBy string `json:"transcript_purged_by"`
	MessagesDeleted    int64  `json:"messages_deleted"`
}

// PurgeTaskTranscript clears a finished run's log: every transcript row and
// the run's final output. Redaction at ingest only catches secrets it can
// recognize, so this is how an admin removes one it missed. The run itself,
// its status, timing and usage stay.
//
// Only a workspace owner or admin who can read the run may do it, and only
// once the run has finished, so no step is written after the purge.
func (h *Handler) PurgeTaskTranscript(w http.ResponseWriter, r *http.Request) {
	task, ok := h.loadTaskForUser(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "task not found", "owner", "admin")
	if !ok {
		return
	}
	if !isTerminalTaskStatus(task.Status) {
		writeError(w, http.StatusConflict, "only a finished run's log can be cleared")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear run log")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	purged, err := qtx.MarkTaskTranscriptPurged(r.Context(), db.MarkTaskTranscriptPurgedParams{
		ID:       task.ID,
		PurgedBy: member.UserID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusConflict, "only a finished run's log can be cleared")
			return
		}
		slog.Error("purge task transcript: mark failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to clear run log")
		return
	}
	deleted, err := qtx.DeleteTaskMessages(r.Context(), task.ID)
	if err != nil {
		slog.Error("purge task transcript: delete failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to clear run log")
		return
	}
	if task.AutopilotRunID.Valid {
		if err := qtx.DropAutopilotRunOutput(r.Context(), task.AutopilotRunID); err != nil {
			slog.Error("purge task transcript: autopilot run output failed", "task_id", uuidToString(task.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to clear run log")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear run log")
		return
	}

	slog.Info("task transcript purged",
		"workspace_id", workspaceID,
		"task_id", uuidToString(task.ID),
		"agent_id", uuidToString(task.AgentID),
		"purged_by", uuidToString(member.UserID),
		"messages_deleted", deleted,
	)
	writeJSON(w, http.StatusOK, TaskTranscriptPurgeResponse{
		TranscriptPurgedAt: timestampToString(purged.TranscriptPurgedAt),
		TranscriptPurgedBy: uuidToString(purged.TranscriptPurgedBy),
		MessagesDeleted:    deleted,
	})
}
