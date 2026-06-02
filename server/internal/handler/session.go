package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
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

// scanSession scans a single session row from pgx.Rows into a SessionResponse.
func scanSession(row interface {
	Scan(dest ...any) error
}) (SessionResponse, error) {
	var (
		id         pgtype.UUID
		issueID    pgtype.UUID
		agentID    pgtype.UUID
		runNum     int
		isActive   bool
		summary    pgtype.Text
		workDir    pgtype.Text
		branch     pgtype.Text
		filesMod   []string
		createdAt  time.Time
		lastActive time.Time
		ver        int
	)
	if err := row.Scan(&id, &issueID, &agentID, &runNum, &isActive,
		&summary, &workDir, &branch, &filesMod,
		&createdAt, &lastActive, &ver); err != nil {
		return SessionResponse{}, err
	}
	return SessionResponse{
		ID:                  uuidToString(id),
		IssueID:             uuidToString(issueID),
		AgentID:             uuidToString(agentID),
		RunNumber:           runNum,
		IsActive:            isActive,
		ConversationSummary: textToPtr(summary),
		WorkingDirectory:    textToPtr(workDir),
		Branch:              textToPtr(branch),
		FilesModified:       filesMod,
		CreatedAt:           createdAt,
		LastActiveAt:        lastActive,
		Version:             ver,
	}, nil
}

// ListSessions returns all sessions for a given issue.
// GET /api/agent-sessions?issue_id=<uuid>
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	issueIDRaw := r.URL.Query().Get("issue_id")
	if issueIDRaw == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, issueIDRaw, "issue_id")
	if !ok {
		return
	}

	rows, qErr := h.DB.Query(r.Context(), `
		SELECT id, issue_id, agent_id, run_number, is_active,
			conversation_summary, working_directory, branch,
			files_modified, created_at, last_active_at, version
		FROM agent_sessions
		WHERE issue_id = $1
		ORDER BY agent_id, run_number DESC
	`, issueID)
	if qErr != nil {
		writeError(w, http.StatusInternalServerError, "query sessions: "+qErr.Error())
		return
	}
	defer rows.Close()

	out := []SessionResponse{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan session: "+err.Error())
			return
		}
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "iterate sessions: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GetSession returns a single session by ID.
// GET /api/agent-sessions/{sessionId}
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "sessionId"), "sessionId")
	if !ok {
		return
	}

	row := h.DB.QueryRow(r.Context(), `
		SELECT id, issue_id, agent_id, run_number, is_active,
			conversation_summary, working_directory, branch,
			files_modified, created_at, last_active_at, version
		FROM agent_sessions
		WHERE id = $1
	`, sessionID)
	sess, err := scanSession(row)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sess)
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
