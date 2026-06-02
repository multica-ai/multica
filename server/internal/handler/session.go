package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/dwickyfp/wallts/server/pkg/session"
)

// SessionResponse is the JSON shape for a session resource.
type SessionResponse struct {
	ID                  string    `json:"id"`
	IssueID             string    `json:"issue_id"`
	AgentID             string    `json:"agent_id"`
	RunNumber           int       `json:"run_number"`
	IsActive            bool      `json:"is_active"`
	ConversationSummary *string   `json:"conversation_summary,omitempty"`
	WorkingDirectory    *string   `json:"working_directory,omitempty"`
	Branch              *string   `json:"branch,omitempty"`
	FilesModified       []string  `json:"files_modified,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	LastActiveAt        time.Time `json:"last_active_at"`
	Version             int       `json:"version"`
}

// sessionToResponse converts a session.Session to the handler's JSON shape.
func sessionToResponse(s *session.Session) SessionResponse {
	return SessionResponse{
		ID:                  s.ID.String(),
		IssueID:             s.IssueID.String(),
		AgentID:             s.AgentID.String(),
		RunNumber:           s.RunNumber,
		IsActive:            s.IsActive,
		ConversationSummary: s.ConversationSummary,
		WorkingDirectory:    s.WorkingDirectory,
		Branch:              s.Branch,
		FilesModified:       s.FilesModified,
		CreatedAt:           s.CreatedAt,
		LastActiveAt:        s.LastActiveAt,
		Version:             s.Version,
	}
}

// ListSessions returns all sessions for a given issue.
// GET /api/agent-sessions?issue_id=<uuid>
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	if h.SessionService == nil {
		writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	issueIDRaw := r.URL.Query().Get("issue_id")
	if issueIDRaw == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, issueIDRaw, "issue_id")
	if !ok {
		return
	}

	sessions, err := h.SessionService.ListSessions(r.Context(), uuid.UUID(issueID.Bytes))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query sessions: "+err.Error())
		return
	}

	out := make([]SessionResponse, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionToResponse(s))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetSession returns a single session by ID.
// GET /api/agent-sessions/{sessionId}
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	if h.SessionService == nil {
		writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	sessionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "sessionId"), "sessionId")
	if !ok {
		return
	}

	s, err := h.SessionService.GetSession(r.Context(), uuid.UUID(sessionID.Bytes))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sessionToResponse(s))
}

// ResetSession deactivates all active sessions for a given issue+agent.
// POST /api/agent-sessions/reset
// Body: { "issue_id": "uuid", "agent_id": "uuid" }
func (h *Handler) ResetSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IssueID string `json:"issue_id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, body.IssueID, "issue_id")
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, body.AgentID, "agent_id")
	if !ok {
		return
	}

	if h.SessionService == nil {
		writeError(w, http.StatusInternalServerError, "session service not available")
		return
	}

	if err := h.SessionService.ResetSession(r.Context(), uuid.UUID(issueID.Bytes), uuid.UUID(agentID.Bytes)); err != nil {
		writeError(w, http.StatusInternalServerError, "reset session: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
