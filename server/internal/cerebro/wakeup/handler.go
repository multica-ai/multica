package wakeup

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

type Handler struct {
	Service *Service
}

type createWakeupRequest struct {
	AgentID       string  `json:"agent_id"`
	IssueID       string  `json:"issue_id"`
	Prompt        string  `json:"prompt"`
	TriggerType   string  `json:"trigger_type"`
	FireAt        *string `json:"fire_at,omitempty"`
	WatchIssueID  *string `json:"watch_issue_id,omitempty"`
	WatchStatus   *string `json:"watch_status,omitempty"`
	OnIssueStatus *string `json:"on_issue_status,omitempty"`
}

type wakeupResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	AgentID      string  `json:"agent_id"`
	IssueID      string  `json:"issue_id"`
	Prompt       string  `json:"prompt"`
	TriggerType  string  `json:"trigger_type"`
	FireAt       *string `json:"fire_at,omitempty"`
	WatchIssueID *string `json:"watch_issue_id,omitempty"`
	WatchStatus  *string `json:"watch_status,omitempty"`
	State        string  `json:"state"`
	Failure      *string `json:"failure,omitempty"`
	CreatedByID  *string `json:"created_by_id,omitempty"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	ClaimedAt    *string `json:"claimed_at,omitempty"`
	DispatchedAt *string `json:"dispatched_at,omitempty"`
	CancelledAt  *string `json:"cancelled_at,omitempty"`
}

func NewHandler(svc *Service) *Handler {
	return &Handler{Service: svc}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspace(w, r)
	if !ok {
		return
	}
	var agentID pgtype.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("agent_id")); raw != "" {
		var err error
		agentID, err = util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid agent_id")
			return
		}
	}
	var state pgtype.Text
	if raw := strings.TrimSpace(r.URL.Query().Get("state")); raw != "" {
		state = pgtype.Text{String: raw, Valid: true}
	}
	limit := int32(50)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = int32(n)
	}
	rows, err := h.Service.List(r.Context(), workspaceID, agentID, state, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list wakeups failed")
		return
	}
	out := make([]wakeupResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"wakeups": out})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspace(w, r)
	if !ok {
		return
	}
	var req createWakeupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	agentID, err := util.ParseUUID(req.AgentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	issueID, err := util.ParseUUID(req.IssueID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue_id")
		return
	}
	var fireAt pgtype.Timestamptz
	if req.FireAt != nil && strings.TrimSpace(*req.FireAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.FireAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid fire_at; use RFC3339")
			return
		}
		fireAt = pgtype.Timestamptz{Time: t, Valid: true}
	}
	var watchIssueID pgtype.UUID
	rawWatchIssue := ""
	if req.WatchIssueID != nil {
		rawWatchIssue = *req.WatchIssueID
	} else if req.OnIssueStatus != nil {
		rawWatchIssue = *req.OnIssueStatus
	}
	if strings.TrimSpace(rawWatchIssue) != "" {
		watchIssueID, err = util.ParseUUID(strings.TrimSpace(rawWatchIssue))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid watch_issue_id")
			return
		}
	}
	var watchStatus pgtype.Text
	if req.WatchStatus != nil && strings.TrimSpace(*req.WatchStatus) != "" {
		watchStatus = pgtype.Text{String: strings.TrimSpace(*req.WatchStatus), Valid: true}
	}
	var createdBy pgtype.UUID
	if raw := strings.TrimSpace(r.Header.Get("X-User-ID")); raw != "" {
		createdBy, _ = util.ParseUUID(raw)
	}
	row, err := h.Service.Create(r.Context(), workspaceID, CreateRequest{
		AgentID:      agentID,
		IssueID:      issueID,
		Prompt:       req.Prompt,
		TriggerType:  req.TriggerType,
		FireAt:       fireAt,
		WatchIssueID: watchIssueID,
		WatchStatus:  watchStatus,
		CreatedByID:  createdBy,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(row))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspace(w, r)
	if !ok {
		return
	}
	id, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	row, err := h.Service.Get(r.Context(), workspaceID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "wakeup not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get wakeup failed")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(row))
}

func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseWorkspace(w, r)
	if !ok {
		return
	}
	id, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	row, err := h.Service.Cancel(r.Context(), workspaceID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "pending wakeup not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "cancel wakeup failed")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(row))
}

func parseWorkspace(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	raw := middleware.WorkspaceIDFromContext(r.Context())
	if raw == "" {
		writeError(w, http.StatusBadRequest, "workspace required")
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace")
		return pgtype.UUID{}, false
	}
	return id, true
}

func toResponse(row cerebrodb.CerebroAgentWakeup) wakeupResponse {
	return wakeupResponse{
		ID:           util.UUIDToString(row.ID),
		WorkspaceID:  util.UUIDToString(row.WorkspaceID),
		AgentID:      util.UUIDToString(row.AgentID),
		IssueID:      util.UUIDToString(row.IssueID),
		Prompt:       row.Prompt,
		TriggerType:  row.TriggerType,
		FireAt:       timePtr(row.FireAt),
		WatchIssueID: uuidPtr(row.WatchIssueID),
		WatchStatus:  textPtr(row.WatchStatus),
		State:        row.State,
		Failure:      textPtr(row.Failure),
		CreatedByID:  uuidPtr(row.CreatedByID),
		CreatedAt:    row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:    row.UpdatedAt.Time.Format(time.RFC3339),
		ClaimedAt:    timePtr(row.ClaimedAt),
		DispatchedAt: timePtr(row.DispatchedAt),
		CancelledAt:  timePtr(row.CancelledAt),
	}
}

func uuidPtr(v pgtype.UUID) *string {
	if !v.Valid {
		return nil
	}
	s := util.UUIDToString(v)
	return &s
}

func textPtr(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func timePtr(v pgtype.Timestamptz) *string {
	if !v.Valid {
		return nil
	}
	s := v.Time.Format(time.RFC3339)
	return &s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
