package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type IssueTimerEntryResponse struct {
	ID        string  `json:"id"`
	ActorType string  `json:"actor_type"`
	ActorID   string  `json:"actor_id"`
	Source    string  `json:"source"`
	TaskID    *string `json:"task_id"`
	StartedAt string  `json:"started_at"`
	StoppedAt *string `json:"stopped_at"`
}

type IssueTimerSummaryResponse struct {
	IssueID      string                   `json:"issue_id"`
	TotalSeconds int64                    `json:"total_seconds"`
	EntryCount   int64                    `json:"entry_count"`
	ActiveTimer  *IssueTimerEntryResponse `json:"active_timer"`
}

func issueTimerEntryToResponse(entry db.IssueTimeEntry) IssueTimerEntryResponse {
	return IssueTimerEntryResponse{
		ID:        uuidToString(entry.ID),
		ActorType: entry.ActorType,
		ActorID:   uuidToString(entry.ActorID),
		Source:    entry.Source,
		TaskID:    uuidToPtr(entry.TaskID),
		StartedAt: timestampToString(entry.StartedAt),
		StoppedAt: timestampToPtr(entry.StoppedAt),
	}
}

func (h *Handler) publishIssueTimerChanged(issueID, workspaceID, actorType, actorID string) {
	h.publish(protocol.EventIssueTimerChanged, workspaceID, actorType, actorID, map[string]any{
		"issue_id": issueID,
	})
}

func (h *Handler) issueTimerSummary(r *http.Request, issue db.Issue, actorType string, actorID pgtype.UUID) IssueTimerSummaryResponse {
	row, err := h.Queries.GetIssueTimeSummary(r.Context(), issue.ID)
	if err != nil {
		slog.Warn("get issue time summary failed", "issue_id", uuidToString(issue.ID), "error", err)
	}

	var active *IssueTimerEntryResponse
	entry, err := h.Queries.GetActiveIssueTimerForIssueAndActor(r.Context(), db.GetActiveIssueTimerForIssueAndActorParams{
		IssueID:   issue.ID,
		ActorType: actorType,
		ActorID:   actorID,
	})
	if err == nil {
		resp := issueTimerEntryToResponse(entry)
		active = &resp
	} else if !isNotFound(err) {
		slog.Warn("get active issue timer failed", "issue_id", uuidToString(issue.ID), "error", err)
	}

	return IssueTimerSummaryResponse{
		IssueID:      uuidToString(issue.ID),
		TotalSeconds: row.TotalSeconds,
		EntryCount:   row.EntryCount,
		ActiveTimer:  active,
	}
}

func (h *Handler) GetIssueTimer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.issueTimerSummary(r, issue, "member", actorID))
}

func (h *Handler) StartIssueTimer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	active, err := h.Queries.GetActiveIssueTimerForActor(r.Context(), db.GetActiveIssueTimerForActorParams{
		WorkspaceID: issue.WorkspaceID,
		ActorType:   "member",
		ActorID:     actorID,
	})
	if err == nil {
		if active.IssueID != issue.ID {
			writeError(w, http.StatusConflict, "stop your active timer before starting another issue")
			return
		}
		writeJSON(w, http.StatusOK, h.issueTimerSummary(r, issue, "member", actorID))
		return
	}
	if !isNotFound(err) {
		slog.Warn("get active issue timer failed", "issue_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start timer")
		return
	}

	if _, err := h.Queries.CreateIssueTimeEntry(r.Context(), db.CreateIssueTimeEntryParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		ActorType:   "member",
		ActorID:     actorID,
		Source:      "manual",
	}); err != nil {
		slog.Warn("create issue time entry failed", "issue_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start timer")
		return
	}

	h.publishIssueTimerChanged(uuidToString(issue.ID), uuidToString(issue.WorkspaceID), "member", userID)
	writeJSON(w, http.StatusOK, h.issueTimerSummary(r, issue, "member", actorID))
}

func (h *Handler) StopIssueTimer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	active, err := h.Queries.GetActiveIssueTimerForIssueAndActor(r.Context(), db.GetActiveIssueTimerForIssueAndActorParams{
		IssueID:   issue.ID,
		ActorType: "member",
		ActorID:   actorID,
	})
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, h.issueTimerSummary(r, issue, "member", actorID))
			return
		}
		slog.Warn("get active issue timer failed", "issue_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to stop timer")
		return
	}

	if _, err := h.Queries.StopIssueTimeEntry(r.Context(), active.ID); err != nil {
		slog.Warn("stop issue time entry failed", "issue_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to stop timer")
		return
	}

	h.publishIssueTimerChanged(uuidToString(issue.ID), uuidToString(issue.WorkspaceID), "member", userID)
	writeJSON(w, http.StatusOK, h.issueTimerSummary(r, issue, "member", actorID))
}

func (h *Handler) startAgentTaskTimer(r *http.Request, task db.AgentTaskQueue) {
	if !task.IssueID.Valid {
		return
	}
	issue, err := h.Queries.GetIssue(r.Context(), task.IssueID)
	if err != nil {
		return
	}
	if _, err := h.Queries.CreateIssueTimeEntry(r.Context(), db.CreateIssueTimeEntryParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     task.IssueID,
		ActorType:   "agent",
		ActorID:     task.AgentID,
		TaskID:      task.ID,
		Source:      "agent_task",
	}); err != nil && !isUniqueViolation(err) {
		slog.Warn("start agent task timer failed", "task_id", uuidToString(task.ID), "error", err)
	} else {
		h.publishIssueTimerChanged(uuidToString(task.IssueID), uuidToString(issue.WorkspaceID), "agent", uuidToString(task.AgentID))
	}
}

func (h *Handler) stopAgentTaskTimer(r *http.Request, task db.AgentTaskQueue) {
	entry, err := h.Queries.GetActiveIssueTimerForTask(r.Context(), task.ID)
	if err != nil {
		if !isNotFound(err) {
			slog.Warn("get agent task timer failed", "task_id", uuidToString(task.ID), "error", err)
		}
		return
	}
	if _, err := h.Queries.StopIssueTimeEntry(r.Context(), entry.ID); err != nil {
		slog.Warn("stop agent task timer failed", "task_id", uuidToString(task.ID), "error", err)
	} else {
		h.publishIssueTimerChanged(uuidToString(entry.IssueID), uuidToString(entry.WorkspaceID), "agent", uuidToString(entry.ActorID))
	}
}
