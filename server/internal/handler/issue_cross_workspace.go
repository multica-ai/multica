package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	crossWorkspaceDefaultLimit = 50
	crossWorkspaceMaxLimit     = 100
)

// validIssueStatuses gates the `status` query parameter. Mirrors the CHECK
// constraint on issue.status (migration 001) — kept in sync by code review.
var validIssueStatuses = map[string]struct{}{
	"backlog":     {},
	"todo":        {},
	"in_progress": {},
	"in_review":   {},
	"done":        {},
	"blocked":     {},
	"cancelled":   {},
}

var validIssuePriorities = map[string]struct{}{
	"urgent": {},
	"high":   {},
	"medium": {},
	"low":    {},
	"none":   {},
}

// CrossWorkspaceIssueResponse is the per-row shape for the cross-workspace
// endpoint. It mirrors IssueResponse but embeds a `workspace` block (including
// the server-derived color) so the global Kanban can render workspace badges
// without an N+1 lookup.
type CrossWorkspaceIssueResponse struct {
	ID            string                       `json:"id"`
	Number        int32                        `json:"number"`
	Identifier    string                       `json:"identifier"`
	Title         string                       `json:"title"`
	Description   *string                      `json:"description"`
	Status        string                       `json:"status"`
	Priority      string                       `json:"priority"`
	AssigneeType  *string                      `json:"assignee_type"`
	AssigneeID    *string                      `json:"assignee_id"`
	CreatorType   string                       `json:"creator_type"`
	CreatorID     string                       `json:"creator_id"`
	ParentIssueID *string                      `json:"parent_issue_id"`
	ProjectID     *string                      `json:"project_id"`
	Position      float64                      `json:"position"`
	DueDate       *string                      `json:"due_date"`
	CreatedAt     string                       `json:"created_at"`
	UpdatedAt     string                       `json:"updated_at"`
	Labels        []LabelResponse              `json:"labels"`
	Workspace     CrossWorkspaceBadgeResponse  `json:"workspace"`
}

// CrossWorkspaceBadgeResponse is the embedded workspace block. The frontend
// trusts `color`; it is derived deterministically from the workspace UUID via
// util.WorkspaceColor.
type CrossWorkspaceBadgeResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	IssuePrefix string `json:"issue_prefix"`
	Color       string `json:"color"`
}

// crossWorkspaceCursor is the keyset cursor payload, base64-encoded so callers
// treat it as opaque. Pagination orders by (created_at DESC, id DESC); the
// next page is "rows strictly less than this (created_at, id)".
type crossWorkspaceCursor struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

func encodeCursor(c crossWorkspaceCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (crossWorkspaceCursor, error) {
	var c crossWorkspaceCursor
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, errors.New("invalid cursor")
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, errors.New("invalid cursor")
	}
	if c.CreatedAt.IsZero() || c.ID == "" {
		return c, errors.New("invalid cursor")
	}
	if !util.ParseUUID(c.ID).Valid {
		return c, errors.New("invalid cursor")
	}
	return c, nil
}

// parseCSVUUIDs splits a comma-separated string into pgtype.UUIDs, dropping
// blank segments and silently skipping malformed UUIDs (the contract says
// "unknown IDs are silently dropped").
func parseCSVUUIDs(raw string) []pgtype.UUID {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]pgtype.UUID, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		u := util.ParseUUID(p)
		if u.Valid {
			out = append(out, u)
		}
	}
	return out
}

// parseCSVEnum splits and validates a comma-separated string against `allowed`.
// Returns (values, "") on success, or (nil, offendingToken) on the first
// invalid value — caller turns that into a 400.
func parseCSVEnum(raw string, allowed map[string]struct{}) ([]string, string) {
	if raw == "" {
		return nil, ""
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := allowed[p]; !ok {
			return nil, p
		}
		out = append(out, p)
	}
	return out, ""
}

// ListCrossWorkspaceIssues serves GET /api/issues/cross-workspace.
//
// Membership is enforced inside the SQL JOIN, not in middleware: the route
// sits in the user-scoped group because there is no workspace ID in the URL.
// A caller passing a workspace_id they don't belong to gets that workspace
// silently filtered (status 200, no 403, no information leak about whether
// the workspace exists). See ADR 0001 §1.
func (h *Handler) ListCrossWorkspaceIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := time.Now()

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID := parseUUID(userID)

	q := r.URL.Query()

	// limit — default 50, hard cap 100, clamp out-of-range values.
	limit := crossWorkspaceDefaultLimit
	if raw := q.Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > crossWorkspaceMaxLimit {
		limit = crossWorkspaceMaxLimit
	}

	// open_only short-circuits the status filter when true.
	openOnly := q.Get("open_only") == "true"
	var openOnlyArg pgtype.Bool
	if openOnly {
		openOnlyArg = pgtype.Bool{Bool: true, Valid: true}
	}

	// status — comma-separated, validated against the CHECK set.
	var statuses []string
	if !openOnly {
		var bad string
		statuses, bad = parseCSVEnum(q.Get("status"), validIssueStatuses)
		if bad != "" {
			writeError(w, http.StatusBadRequest, "invalid status: "+bad)
			return
		}
	}

	// priority — same shape.
	priorities, bad := parseCSVEnum(q.Get("priority"), validIssuePriorities)
	if bad != "" {
		writeError(w, http.StatusBadRequest, "invalid priority: "+bad)
		return
	}

	// assignee_id (single) merges into assignee_ids (multi).
	assigneeIDs := parseCSVUUIDs(q.Get("assignee_ids"))
	if single := strings.TrimSpace(q.Get("assignee_id")); single != "" {
		if u := util.ParseUUID(single); u.Valid {
			assigneeIDs = append(assigneeIDs, u)
		}
	}

	workspaceIDs := parseCSVUUIDs(q.Get("workspace_ids"))

	// Cursor: optional. Malformed → 400.
	var cursorCreated pgtype.Timestamptz
	var cursorID pgtype.UUID
	if raw := q.Get("after"); raw != "" {
		c, err := decodeCursor(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		cursorCreated = pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}
		cursorID = util.ParseUUID(c.ID)
	}

	// Request limit+1 so we can detect has_more without a COUNT(*).
	rows, err := h.Queries.ListCrossWorkspaceIssues(ctx, db.ListCrossWorkspaceIssuesParams{
		UserID:        userUUID,
		Limit:         int32(limit + 1),
		WorkspaceIds:  workspaceIDs,
		OpenOnly:      openOnlyArg,
		Statuses:      statuses,
		Priorities:    priorities,
		AssigneeIds:   assigneeIDs,
		CursorCreated: cursorCreated,
		CursorID:      cursorID,
	})
	if err != nil {
		slog.Error("list cross-workspace issues failed",
			"user_id", userID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "failed to list cross-workspace issues")
		return
	}

	hasMore := false
	if len(rows) > limit {
		rows = rows[:limit]
		hasMore = true
	}

	// Bulk-load labels keyed by (workspace_id, issue_id) so we don't N+1 across
	// every row. Each workspace has its own labels table scoping; we group
	// issue IDs by workspace and call labelsByIssue once per workspace.
	idsByWS := map[string][]pgtype.UUID{}
	wsKey := map[string]pgtype.UUID{}
	for _, row := range rows {
		k := uuidToString(row.WorkspaceID)
		idsByWS[k] = append(idsByWS[k], row.ID)
		wsKey[k] = row.WorkspaceID
	}
	labelsByID := map[string][]LabelResponse{}
	for k, ids := range idsByWS {
		for issueID, labels := range h.labelsByIssue(ctx, wsKey[k], ids) {
			labelsByID[issueID] = labels
		}
	}

	resp := make([]CrossWorkspaceIssueResponse, len(rows))
	for i, row := range rows {
		identifier := row.WorkspaceIssuePrefix + "-" + strconv.Itoa(int(row.Number))
		issueID := uuidToString(row.ID)
		labels := labelsByID[issueID]
		if labels == nil {
			labels = []LabelResponse{}
		}
		resp[i] = CrossWorkspaceIssueResponse{
			ID:            issueID,
			Number:        row.Number,
			Identifier:    identifier,
			Title:         row.Title,
			Description:   textToPtr(row.Description),
			Status:        row.Status,
			Priority:      row.Priority,
			AssigneeType:  textToPtr(row.AssigneeType),
			AssigneeID:    uuidToPtr(row.AssigneeID),
			CreatorType:   row.CreatorType,
			CreatorID:     uuidToString(row.CreatorID),
			ParentIssueID: uuidToPtr(row.ParentIssueID),
			ProjectID:     uuidToPtr(row.ProjectID),
			Position:      row.Position,
			DueDate:       timestampToPtr(row.DueDate),
			CreatedAt:     timestampToString(row.CreatedAt),
			UpdatedAt:     timestampToString(row.UpdatedAt),
			Labels:        labels,
			Workspace: CrossWorkspaceBadgeResponse{
				ID:          uuidToString(row.WorkspaceID),
				Name:        row.WorkspaceName,
				Slug:        row.WorkspaceSlug,
				IssuePrefix: row.WorkspaceIssuePrefix,
				Color:       util.WorkspaceColor(row.WorkspaceID),
			},
		}
	}

	var nextCursor *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		c := encodeCursor(crossWorkspaceCursor{
			CreatedAt: last.CreatedAt.Time,
			ID:        uuidToString(last.ID),
		})
		nextCursor = &c
	}

	// result_workspace_count is the number of distinct workspaces the result
	// page touched, NOT the user's full membership cardinality — a user with
	// many memberships and a tight filter still produces a small number here.
	// Logged for telemetry; the authoritative membership count is at
	// /api/workspaces.
	resultWSCount := len(idsByWS)

	durationMs := time.Since(start).Milliseconds()

	slog.Info("list cross-workspace issues",
		"endpoint", "/api/issues/cross-workspace",
		"user_id", userID,
		"result_workspace_count", resultWSCount,
		"filter_workspace_ids", len(workspaceIDs),
		"result_count", len(resp),
		"duration_ms", durationMs,
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"issues":         resp,
		"next_cursor":    nextCursor,
		"has_more":       hasMore,
		"total_returned": len(resp),
	})
}
