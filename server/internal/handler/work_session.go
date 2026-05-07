package handler

// CEREBRO-PATCH(work-session-handler): cerebro modification of upstream file

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// CreateWorkSession creates a new work session linking a user's Claude Code session to an issue.
func (h *Handler) CreateWorkSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IssueID     string `json:"issue_id"`
		SessionType string `json:"session_type"`
		WorkDir     string `json:"work_dir"`
		Branch      string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.IssueID == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	if req.SessionType == "" {
		req.SessionType = "claude-code"
	}

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	// Verify issue exists and user has access via workspace membership.
	issue, err := h.Queries.GetIssue(r.Context(), parseUUID(req.IssueID))
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	ws, err := h.Queries.CreateWorkSession(r.Context(), db.CreateWorkSessionParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     parseUUID(req.IssueID),
		UserID:      parseUUID(userID),
		SessionType: req.SessionType,
		WorkDir:     pgtype.Text{String: req.WorkDir, Valid: req.WorkDir != ""},
		Branch:      pgtype.Text{String: req.Branch, Valid: req.Branch != ""},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create work session")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           uuidToString(ws.ID),
		"issue_id":     uuidToString(ws.IssueID),
		"workspace_id": uuidToString(ws.WorkspaceID),
		"status":       ws.Status,
		"created_at":   timestampToString(ws.CreatedAt),
	})
}

// CompleteWorkSession marks a work session as complete.
func (h *Handler) CompleteWorkSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Summary   string `json:"summary"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Verify ownership.
	ws, err := h.Queries.GetWorkSession(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "work session not found")
		return
	}
	userID := r.Header.Get("X-User-ID")
	if uuidToString(ws.UserID) != userID {
		writeError(w, http.StatusForbidden, "not your work session")
		return
	}

	updated, err := h.Queries.CompleteWorkSession(r.Context(), db.CompleteWorkSessionParams{
		ID:        parseUUID(id),
		Summary:   pgtype.Text{String: req.Summary, Valid: req.Summary != ""},
		SessionID: pgtype.Text{String: req.SessionID, Valid: req.SessionID != ""},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete work session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":           uuidToString(updated.ID),
		"status":       updated.Status,
		"completed_at": timestampToPtr(updated.CompletedAt),
	})
}

// UpdateWorkSessionName sets the name of a work session.
func (h *Handler) UpdateWorkSessionName(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ws, err := h.Queries.GetWorkSession(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "work session not found")
		return
	}
	userID := r.Header.Get("X-User-ID")
	if uuidToString(ws.UserID) != userID {
		writeError(w, http.StatusForbidden, "not your work session")
		return
	}
	updated, err := h.Queries.UpdateWorkSessionName(r.Context(), db.UpdateWorkSessionNameParams{
		ID:   parseUUID(id),
		Name: pgtype.Text{String: req.Name, Valid: req.Name != ""},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update name")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":   uuidToString(updated.ID),
		"name": textToPtr(updated.Name),
	})
}

// ResumeWorkSession re-opens a completed or failed work session.
func (h *Handler) ResumeWorkSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	ws, err := h.Queries.GetWorkSession(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "work session not found")
		return
	}
	userID := r.Header.Get("X-User-ID")
	if uuidToString(ws.UserID) != userID {
		writeError(w, http.StatusForbidden, "not your work session")
		return
	}
	if ws.Status == "active" {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":       uuidToString(ws.ID),
			"issue_id": uuidToString(ws.IssueID),
			"status":   ws.Status,
		})
		return
	}

	updated, err := h.Queries.ResumeWorkSession(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resume work session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":       uuidToString(updated.ID),
		"issue_id": uuidToString(updated.IssueID),
		"status":   updated.Status,
	})
}

// ForkWorkSession creates a new work session on the same issue, referencing the original.
func (h *Handler) ForkWorkSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	original, err := h.Queries.GetWorkSession(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "work session not found")
		return
	}

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	ws, err := h.Queries.CreateWorkSession(r.Context(), db.CreateWorkSessionParams{
		WorkspaceID: original.WorkspaceID,
		IssueID:     original.IssueID,
		UserID:      parseUUID(userID),
		SessionType: original.SessionType,
		WorkDir:     original.WorkDir,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fork work session")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           uuidToString(ws.ID),
		"issue_id":     uuidToString(ws.IssueID),
		"forked_from":  uuidToString(original.ID),
		"status":       ws.Status,
		"created_at":   timestampToString(ws.CreatedAt),
	})
}

// GetActiveWorkSession returns the user's active work session in this workspace.
func (h *Handler) GetActiveWorkSession(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	ws, err := h.Queries.GetActiveWorkSessionForUser(r.Context(), db.GetActiveWorkSessionForUserParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"session": nil})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session": map[string]any{
			"id":           uuidToString(ws.ID),
			"issue_id":     uuidToString(ws.IssueID),
			"session_type": ws.SessionType,
			"work_dir":     textToPtr(ws.WorkDir),
			"status":       ws.Status,
			"created_at":   timestampToString(ws.CreatedAt),
		},
	})
}

// ListWorkSessions returns all work sessions for an issue.
func (h *Handler) ListWorkSessions(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")

	sessions, err := h.Queries.ListWorkSessionsByIssue(r.Context(), parseUUID(issueID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list work sessions")
		return
	}

	result := make([]map[string]any, 0, len(sessions))
	for _, ws := range sessions {
		result = append(result, map[string]any{
			"id":           uuidToString(ws.ID),
			"issue_id":     uuidToString(ws.IssueID),
			"user_id":      uuidToString(ws.UserID),
			"session_type": ws.SessionType,
			"work_dir":     textToPtr(ws.WorkDir),
			"branch":       textToPtr(ws.Branch),
			"name":         textToPtr(ws.Name),
			"status":       ws.Status,
			"summary":      textToPtr(ws.Summary),
			"created_at":   timestampToString(ws.CreatedAt),
			"completed_at": timestampToPtr(ws.CompletedAt),
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// GetWorkSessionMessages returns the messages for a work session.
func (h *Handler) GetWorkSessionMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	messages, err := h.Queries.ListWorkSessionMessages(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}

	result := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		result = append(result, map[string]any{
			"seq":     msg.Seq,
			"type":    msg.Type,
			"tool":    textToPtr(msg.Tool),
			"content": textToPtr(msg.Content),
			"output":  textToPtr(msg.Output),
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// ReportWorkSessionMessages receives a batch of messages for a work session.
func (h *Handler) ReportWorkSessionMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req TaskMessageBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Verify ownership.
	ws, err := h.Queries.GetWorkSession(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "work session not found")
		return
	}
	userID := r.Header.Get("X-User-ID")
	if uuidToString(ws.UserID) != userID {
		writeError(w, http.StatusForbidden, "not your work session")
		return
	}

	workspaceID := uuidToString(ws.WorkspaceID)
	issueID := uuidToString(ws.IssueID)

	for _, msg := range req.Messages {
		msg.Content = redact.Text(msg.Content)
		msg.Output = redact.Text(msg.Output)
		msg.Input = redact.InputMap(msg.Input)

		var inputJSON []byte
		if msg.Input != nil {
			inputJSON, _ = json.Marshal(msg.Input)
		}

		h.Queries.CreateWorkSessionMessage(r.Context(), db.CreateWorkSessionMessageParams{
			WorkSessionID: parseUUID(id),
			Seq:           int32(msg.Seq),
			Type:          msg.Type,
			Tool:          pgtype.Text{String: msg.Tool, Valid: msg.Tool != ""},
			Content:       pgtype.Text{String: msg.Content, Valid: msg.Content != ""},
			Input:         inputJSON,
			Output:        pgtype.Text{String: msg.Output, Valid: msg.Output != ""},
		})

		h.publish(protocol.EventTaskMessage, workspaceID, "member", userID, protocol.TaskMessagePayload{
			TaskID:  id, // Use work session ID as task ID for the event.
			IssueID: issueID,
			Seq:     msg.Seq,
			Type:    msg.Type,
			Tool:    msg.Tool,
			Content: msg.Content,
			Input:   msg.Input,
			Output:  msg.Output,
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
