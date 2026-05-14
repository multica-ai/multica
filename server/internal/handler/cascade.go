package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/cascade"
)

// cascadeResponseItem is the JSON shape the dashboard consumes.
// Keeping field names snake_case to match the rest of the multica API
// surface; new fields here MUST also be added to the zod schema in
// packages/core/api/schema.ts so the frontend parses safely
// (per CLAUDE.md "Parse, don't cast" rule). The frontend page is a
// follow-up — backend ships first so manual testers can exercise
// the endpoint via curl.
type cascadeResponseItem struct {
	IssueID            string  `json:"issue_id"`
	IssueNumber        int32   `json:"issue_number"`
	IssueTitle         string  `json:"issue_title"`
	IssueStatus        string  `json:"issue_status"`
	IssueAssigneeID    string  `json:"issue_assignee_id,omitempty"`
	IssueAssigneeType  string  `json:"issue_assignee_type,omitempty"`
	CascadeState       string  `json:"cascade_state"`
	CascadeStartedAt   string  `json:"cascade_started_at"`
	CascadeLastEventAt string  `json:"cascade_last_event_at,omitempty"`
	TotalPRs           int     `json:"total_prs,omitempty"`
	CurrentStep        int     `json:"current_step,omitempty"`
	LastPRNumber       int     `json:"last_pr_number,omitempty"`
	LastEventType      string  `json:"last_event_type,omitempty"`
}

type cascadeResponse struct {
	Items   []cascadeResponseItem `json:"items"`
	Page    int                   `json:"page"`
	PerPage int                   `json:"per_page"`
	HasMore bool                  `json:"has_more"`
}

// ListCascades GET /api/cascades — workspace-scoped dashboard data.
// Filters: cascade_state, agent (assignee uuid). Pagination: page,
// per_page (capped at 50).
//
// Auth: standard workspace-scoped path; uses resolveWorkspaceID for
// the requesting workspace context, same as SearchIssues and friends.
func (h *Handler) ListCascades(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	filters := cascade.ListFilters{
		WorkspaceID:  uuid.UUID(wsUUID.Bytes),
		CascadeState: r.URL.Query().Get("cascade_state"),
	}
	if agentStr := r.URL.Query().Get("agent"); agentStr != "" {
		agentUUID, ok := parseUUIDOrBadRequest(w, agentStr, "agent")
		if !ok {
			return
		}
		filters.AgentID = uuid.UUID(agentUUID.Bytes)
	}

	page := cascade.ListPage{Page: 1, PerPage: 50}
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page.Page = v
		}
	}
	if pp := r.URL.Query().Get("per_page"); pp != "" {
		if v, err := strconv.Atoi(pp); err == nil && v > 0 {
			page.PerPage = v
		}
	}

	// Fetch one extra row to populate has_more without a second query.
	// The dashboard's "load more" trigger uses this flag.
	fetchPerPage := page.PerPage + 1
	rows, err := cascade.ListCascades(ctx, h.DB, cascade.ListFilters{
		WorkspaceID:  filters.WorkspaceID,
		CascadeState: filters.CascadeState,
		AgentID:      filters.AgentID,
	}, cascade.ListPage{Page: page.Page, PerPage: fetchPerPage})
	if err != nil {
		slog.Warn("cascade list failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to list cascades")
		return
	}

	hasMore := false
	if len(rows) > page.PerPage {
		rows = rows[:page.PerPage]
		hasMore = true
	}

	resp := cascadeResponse{
		Items:   make([]cascadeResponseItem, 0, len(rows)),
		Page:    page.Page,
		PerPage: page.PerPage,
		HasMore: hasMore,
	}
	for _, r := range rows {
		item := cascadeResponseItem{
			IssueID:          r.IssueID.String(),
			IssueNumber:      r.IssueNumber,
			IssueTitle:       r.IssueTitle,
			IssueStatus:      r.IssueStatus,
			CascadeState:     r.CascadeState,
			CascadeStartedAt: r.CascadeStartedAt.Format(time.RFC3339),
		}
		if r.IssueAssigneeID != nil {
			item.IssueAssigneeID = r.IssueAssigneeID.String()
		}
		item.IssueAssigneeType = r.IssueAssigneeType
		if r.CascadeLastEventAt != nil {
			item.CascadeLastEventAt = r.CascadeLastEventAt.Format(time.RFC3339)
		}
		if r.Progress != nil {
			item.TotalPRs = r.Progress.TotalPRs
			item.CurrentStep = r.Progress.CurrentStep
			item.LastPRNumber = r.Progress.LastPRNumber
			item.LastEventType = r.Progress.LastEventType
		}
		resp.Items = append(resp.Items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
