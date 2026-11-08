package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type archivedInboxCursor struct {
	Time  string `json:"time"`
	ID    string `json:"id"`
	Scope string `json:"scope"`
}

type archivedInboxPageResponse struct {
	Items      []InboxItemResponse `json:"items"`
	NextCursor *string             `json:"next_cursor"`
	HasMore    bool                `json:"has_more"`
}

type archivedInboxFacetsResponse struct {
	Statuses    map[string]int64 `json:"statuses"`
	Priorities  map[string]int64 `json:"priorities"`
	Actors      map[string]int64 `json:"actors"`
	UnreadCount int64            `json:"unread_count"`
}

func parseArchivedInboxFilters(w http.ResponseWriter, r *http.Request) (db.ArchivedInboxFacetsParams, bool) {
	var p db.ArchivedInboxFacetsParams
	userID, ok := requireUserID(w, r)
	if !ok {
		return p, false
	}
	p.WorkspaceID, ok = parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return p, false
	}
	p.RecipientID = parseUUID(userID)
	q := r.URL.Query()
	for name, target := range map[string]*[]string{"statuses": &p.Statuses, "priorities": &p.Priorities, "actors": &p.Actors} {
		*target = []string{}
		if raw := q.Get(name); raw != "" {
			if len(raw) > 8192 {
				writeError(w, http.StatusBadRequest, "filter is too long")
				return p, false
			}
			*target = strings.Split(raw, ",")
			if len(*target) > 100 {
				writeError(w, http.StatusBadRequest, "too many filter values")
				return p, false
			}
			for _, value := range *target {
				if value == "" {
					writeError(w, http.StatusBadRequest, "empty filter value")
					return p, false
				}
			}
			slices.Sort(*target)
			*target = slices.Compact(*target)
		}
	}
	if raw := q.Get("unread_only"); raw != "" {
		if raw != "true" && raw != "false" {
			writeError(w, http.StatusBadRequest, "invalid unread_only")
			return p, false
		}
		p.UnreadOnly = raw == "true"
	}
	return p, true
}

// A cursor is tied to the recipient and filters, so it cannot accidentally
// continue a different archive or a changed filter selection.
func archivedInboxScope(p db.ArchivedInboxFacetsParams, groupID pgtype.UUID) string {
	encoded, _ := json.Marshal(struct {
		Filters db.ArchivedInboxFacetsParams
		Group   pgtype.UUID
	}{p, groupID})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func (h *Handler) ListArchivedInboxPage(w http.ResponseWriter, r *http.Request) {
	filters, ok := parseArchivedInboxFilters(w, r)
	if !ok {
		return
	}
	p := db.ListArchivedInboxPageParams{
		WorkspaceID: filters.WorkspaceID, RecipientID: filters.RecipientID,
		Statuses: filters.Statuses, Priorities: filters.Priorities, Actors: filters.Actors, UnreadOnly: filters.UnreadOnly,
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
	}
	if raw := r.URL.Query().Get("group_id"); raw != "" {
		p.GroupID, ok = parseUUIDOrBadRequest(w, raw, "group_id")
		if !ok {
			return
		}
	}
	scope := archivedInboxScope(filters, p.GroupID)
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		if len(raw) > 2048 {
			writeError(w, http.StatusBadRequest, "invalid archive cursor")
			return
		}
		var cursor archivedInboxCursor
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Scope != scope {
			writeError(w, http.StatusBadRequest, "invalid archive cursor")
			return
		}
		before, err := time.Parse(time.RFC3339Nano, cursor.Time)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid archive cursor time")
			return
		}
		p.BeforeID, ok = parseUUIDOrBadRequest(w, cursor.ID, "cursor id")
		if !ok {
			return
		}
		p.BeforeTime = pgtype.Timestamptz{Time: before, Valid: true}
	}
	p.PageLimit = int32(limit + 1)
	rows, err := h.Queries.ListArchivedInboxPage(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load archived inbox page")
		return
	}
	resp := archivedInboxPageResponse{Items: make([]InboxItemResponse, 0, limit), HasMore: len(rows) > limit}
	if resp.HasMore {
		rows = rows[:limit]
	}
	for _, row := range rows {
		item := inboxToResponse(row.InboxItem)
		item.Body = inboxListBody(row.InboxItem.Type, row.InboxItem.IssueID, row.InboxItem.Body)
		item.IssueStatus, item.IssuePriority = textToPtr(row.IssueStatus), textToPtr(row.IssuePriority)
		if row.CommentID != "" {
			var details map[string]json.RawMessage
			if len(item.Details) == 0 || string(item.Details) == "null" {
				details = map[string]json.RawMessage{}
			} else if err := json.Unmarshal(item.Details, &details); err != nil {
				writeError(w, http.StatusInternalServerError, "invalid archived notification details")
				return
			}
			details["comment_id"], _ = json.Marshal(row.CommentID)
			item.Details, _ = json.Marshal(details)
		}
		resp.Items = append(resp.Items, item)
	}
	if resp.HasMore {
		last := rows[len(rows)-1].InboxItem
		encoded, _ := json.Marshal(archivedInboxCursor{Time: last.CreatedAt.Time.Format(time.RFC3339Nano), ID: uuidToString(last.ID), Scope: scope})
		cursor := base64.RawURLEncoding.EncodeToString(encoded)
		resp.NextCursor = &cursor
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetArchivedInboxFacets(w http.ResponseWriter, r *http.Request) {
	p, ok := parseArchivedInboxFilters(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ArchivedInboxFacets(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load archived inbox filters")
		return
	}
	resp := archivedInboxFacetsResponse{Statuses: map[string]int64{}, Priorities: map[string]int64{}, Actors: map[string]int64{}}
	for _, row := range rows {
		switch row.Dimension {
		case "statuses":
			resp.Statuses[row.Key] = row.Count
		case "priorities":
			resp.Priorities[row.Key] = row.Count
		case "actors":
			resp.Actors[row.Key] = row.Count
		case "unread":
			resp.UnreadCount = row.Count
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
