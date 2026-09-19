package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ResumeWarningPriorSessionUnavailable is the only continuity code the daemon
// endpoint accepts. It means: this runtime expected to resume a prior provider
// session, proved locally that the session cannot be resumed, and continued the
// task with a fresh one.
const ResumeWarningPriorSessionUnavailable = "prior_session_resume_unavailable"

// ReportRuntimeResumeWarning records one session-continuity warning on the
// runtime that reports it
// (POST /api/daemon/runtimes/{runtimeId}/resume-warning).
//
// The daemon already tells the agent, and the task payload already carries the
// decision; this is the user-visible half of it, so an operator looking at the
// runtime knows why a task restarted cold instead of having to read daemon logs.
//
// Deliberately narrow: one fixed code and a non-empty task id, with the server
// building the metadata object and stamping the time. That is also why the
// request never carries a workspace id or daemon id — the authenticated daemon
// context and the runtime path are authoritative (requireDaemonRuntimeAccess),
// and the endpoint cannot become a general metadata write API.
//
// Semantics are "the most recent continuity warning on this runtime", not "the
// runtime is degraded": the row keeps its status and health, and a later
// warning replaces an older one. Nothing here can fail a task — the daemon
// treats every outcome as best effort.
func (h *Handler) ReportRuntimeResumeWarning(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	var req struct {
		Code   string `json:"code"`
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Code) != ResumeWarningPriorSessionUnavailable {
		writeError(w, http.StatusBadRequest, "unsupported resume warning code")
		return
	}
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	payload, err := json.Marshal(map[string]any{
		"code":        ResumeWarningPriorSessionUnavailable,
		"task_id":     taskID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode resume warning")
		return
	}
	if _, err := h.Queries.SetAgentRuntimeResumeWarning(r.Context(), db.SetAgentRuntimeResumeWarningParams{
		ID:            rt.ID,
		ResumeWarning: payload,
	}); err != nil {
		slog.Error("set runtime resume warning failed", "runtime_id", runtimeID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record resume warning")
		return
	}

	// The same invalidation channel the runtime list and detail already subscribe
	// to, so an open runtime page refetches instead of waiting for a poll.
	h.publish(protocol.EventDaemonRegister, uuidToString(rt.WorkspaceID), "system", "", map[string]any{
		"runtime_id": uuidToString(rt.ID),
		"action":     "resume_warning",
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
