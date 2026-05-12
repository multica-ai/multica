// Package tasks exposes the cerebro tasks page HTTP endpoint. The page
// shows every agent task across the workspace (any agent, any status,
// optional time-range / type filters) with cursor-free offset pagination.
//
// Backed by the queries in server/internal/cerebro/queries/tasks.sql which
// generate into the cerebrodb package.
package tasks

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler exposes /api/cerebro/tasks. Holds the cerebro sqlc queries — all
// data the page needs lives in agent_task_queue + agent + issue and is
// joined inside the SQL query.
type Handler struct {
	Cerebro *cerebrodb.Queries
}

func New(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Cerebro: cerebro}
}

const (
	defaultLimit = 50
	maxLimit     = 200
)

// validStatus returns true if s is one of the agent_task_queue.status enum
// values from the schema (queued/dispatched/running/completed/failed/
// cancelled). Anything else is rejected with 400 — the alternative is a
// silent empty result that masks frontend bugs.
func validStatus(s string) bool {
	switch s {
	case "queued", "dispatched", "running", "completed", "failed", "cancelled":
		return true
	}
	return false
}

type taskResponse struct {
	TaskID         string `json:"task_id"`
	AgentID        string `json:"agent_id"`
	AgentName      string `json:"agent_name"`
	AgentAvatarURL string `json:"agent_avatar_url,omitempty"`
	TaskTitle      string `json:"task_title,omitempty"`
	IssueID        string `json:"issue_id,omitempty"`
	IssueTitle     string `json:"issue_title,omitempty"`
	IssueNumber    int32  `json:"issue_number,omitempty"`
	ChatSessionID  string `json:"chat_session_id,omitempty"`
	Status         string `json:"status"`
	DispatchedAt   string `json:"dispatched_at,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type listResponse struct {
	Tasks  []taskResponse `json:"tasks"`
	Total  int32          `json:"total"`
	Limit  int32          `json:"limit"`
	Offset int32          `json:"offset"`
}

// List handles GET /api/cerebro/tasks. Supported query params:
//
//	agent_id   — UUID of a single agent
//	status     — one of queued/dispatched/running/completed/failed/cancelled
//	type       — "issue" (chat_session_id IS NULL) or "chat" (IS NOT NULL)
//	since      — RFC3339 timestamp; only tasks newer than this point
//	limit      — page size, default 50, capped at 200
//	offset     — pagination offset, default 0
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	q := r.URL.Query()

	var agentID pgtype.UUID
	if raw := q.Get("agent_id"); raw != "" {
		parsed, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid agent_id")
			return
		}
		agentID = parsed
	}

	var status pgtype.Text
	if raw := q.Get("status"); raw != "" {
		if !validStatus(raw) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		status = pgtype.Text{String: raw, Valid: true}
	}

	var taskType pgtype.Text
	if raw := q.Get("type"); raw != "" {
		if raw != "issue" && raw != "chat" {
			writeError(w, http.StatusBadRequest, "invalid type")
			return
		}
		taskType = pgtype.Text{String: raw, Valid: true}
	}

	var since pgtype.Timestamptz
	if raw := q.Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since (expected RFC3339)")
			return
		}
		since = pgtype.Timestamptz{Time: t, Valid: true}
	}

	limit := parseIntOr(q.Get("limit"), defaultLimit)
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset := parseIntOr(q.Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}

	ctx := r.Context()

	rows, err := h.Cerebro.ListCerebroTasks(ctx, cerebrodb.ListCerebroTasksParams{
		WorkspaceID: wsUUID,
		Limit:       int32(limit),
		Offset:      int32(offset),
		AgentID:     agentID,
		Status:      status,
		TaskType:    taskType,
		Since:       since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}

	total, err := h.Cerebro.CountCerebroTasks(ctx, cerebrodb.CountCerebroTasksParams{
		WorkspaceID: wsUUID,
		AgentID:     agentID,
		Status:      status,
		TaskType:    taskType,
		Since:       since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count tasks")
		return
	}

	out := listResponse{
		Tasks:  make([]taskResponse, 0, len(rows)),
		Total:  total,
		Limit:  int32(limit),
		Offset: int32(offset),
	}
	for _, row := range rows {
		t := taskResponse{
			TaskID:    util.UUIDToString(row.TaskID),
			AgentID:   util.UUIDToString(row.AgentID),
			AgentName: row.AgentName,
			Status:    row.Status,
			CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339),
		}
		if row.AgentAvatarUrl.Valid {
			t.AgentAvatarURL = row.AgentAvatarUrl.String
		}
		if row.TaskTitle.Valid {
			t.TaskTitle = row.TaskTitle.String
		}
		if row.IssueID.Valid {
			t.IssueID = util.UUIDToString(row.IssueID)
		}
		if row.IssueTitle.Valid {
			t.IssueTitle = row.IssueTitle.String
		}
		if row.IssueNumber.Valid {
			t.IssueNumber = row.IssueNumber.Int32
		}
		if row.ChatSessionID.Valid {
			t.ChatSessionID = util.UUIDToString(row.ChatSessionID)
		}
		if row.DispatchedAt.Valid {
			t.DispatchedAt = row.DispatchedAt.Time.UTC().Format(time.RFC3339)
		}
		if row.StartedAt.Valid {
			t.StartedAt = row.StartedAt.Time.UTC().Format(time.RFC3339)
		}
		if row.CompletedAt.Valid {
			t.CompletedAt = row.CompletedAt.Time.UTC().Format(time.RFC3339)
		}
		out.Tasks = append(out.Tasks, t)
	}

	writeJSON(w, http.StatusOK, out)
}

func parseIntOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
