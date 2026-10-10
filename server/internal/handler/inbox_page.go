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

// Paged inbox lists. The active inbox (GET /api/inbox/page, /facets) and its
// archive (GET /api/inbox/archived/page, /facets) share one contract: each
// page lists issue groups — one row per group, its newest notification in that
// view — under the same filters, keyset cursor and response shapes. Only the
// queries and the cursor scope differ.
//
// GET /api/inbox keeps returning the unbounded array: installed clients still
// read it, so the paged active list is a separate endpoint rather than a
// change to that one.

type inboxPageCursor struct {
	Time  string `json:"time"`
	ID    string `json:"id"`
	Scope string `json:"scope"`
}

type inboxPageResponse struct {
	Items      []InboxItemResponse `json:"items"`
	NextCursor *string             `json:"next_cursor"`
	HasMore    bool                `json:"has_more"`
}

type inboxFacetsResponse struct {
	Statuses    map[string]int64 `json:"statuses"`
	Priorities  map[string]int64 `json:"priorities"`
	Actors      map[string]int64 `json:"actors"`
	UnreadCount int64            `json:"unread_count"`
}

// The archived endpoints answer with the same shapes.
type (
	archivedInboxPageResponse   = inboxPageResponse
	archivedInboxFacetsResponse = inboxFacetsResponse
)

// inboxFilters is the filter selection both views page and facet by. It is
// the archived facet params type itself rather than a copy with the same
// fields, because its JSON form is hashed into every archived cursor (see
// inboxPageScope): changing it would void archived cursors already issued.
type inboxFilters = db.ArchivedInboxFacetsParams

// inboxView names the list a page request reads. It is part of the cursor
// scope, so a cursor from one view is rejected by the other.
type inboxView string

const (
	// Empty on purpose: archived cursors were scoped before the active view
	// existed, without a view in the hash, and must keep verifying.
	archivedInboxView inboxView = ""
	activeInboxView   inboxView = "active"
)

// cursorNoun names the view in cursor validation errors.
func (v inboxView) cursorNoun() string {
	if v == archivedInboxView {
		return "archive"
	}
	return "inbox"
}

func parseInboxFilters(w http.ResponseWriter, r *http.Request) (inboxFilters, bool) {
	var p inboxFilters
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

// A cursor is tied to the view, recipient and filters, so it cannot
// accidentally continue a different list or a changed filter selection. The
// archived view hashes exactly the bytes it always has: an empty view is
// omitted from the JSON.
func inboxPageScope(view inboxView, p inboxFilters, groupID pgtype.UUID) string {
	encoded, _ := json.Marshal(struct {
		View    inboxView `json:",omitempty"`
		Filters inboxFilters
		Group   pgtype.UUID
	}{view, p, groupID})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

// parseInboxPageRequest reads the filters, group lookup, limit and cursor of a
// page request. The returned params ask for one row beyond the limit, which is
// how writeInboxPage learns whether another page exists.
func parseInboxPageRequest(w http.ResponseWriter, r *http.Request, view inboxView) (p db.ListInboxPageParams, limit int, scope string, ok bool) {
	filters, ok := parseInboxFilters(w, r)
	if !ok {
		return p, 0, "", false
	}
	p = db.ListInboxPageParams{
		WorkspaceID: filters.WorkspaceID, RecipientID: filters.RecipientID,
		Statuses: filters.Statuses, Priorities: filters.Priorities, Actors: filters.Actors, UnreadOnly: filters.UnreadOnly,
	}
	limit = 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return p, 0, "", false
		}
	}
	if raw := r.URL.Query().Get("group_id"); raw != "" {
		p.GroupID, ok = parseUUIDOrBadRequest(w, raw, "group_id")
		if !ok {
			return p, 0, "", false
		}
	}
	scope = inboxPageScope(view, filters, p.GroupID)
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		invalid := "invalid " + view.cursorNoun() + " cursor"
		if len(raw) > 2048 {
			writeError(w, http.StatusBadRequest, invalid)
			return p, 0, "", false
		}
		var cursor inboxPageCursor
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Scope != scope {
			writeError(w, http.StatusBadRequest, invalid)
			return p, 0, "", false
		}
		before, err := time.Parse(time.RFC3339Nano, cursor.Time)
		if err != nil {
			writeError(w, http.StatusBadRequest, invalid+" time")
			return p, 0, "", false
		}
		p.BeforeID, ok = parseUUIDOrBadRequest(w, cursor.ID, "cursor id")
		if !ok {
			return p, 0, "", false
		}
		p.BeforeTime = pgtype.Timestamptz{Time: before, Valid: true}
	}
	p.PageLimit = int32(limit + 1)
	return p, limit, scope, true
}

// writeInboxPage answers a page request with up to limit groups. Bodies get
// the list preview, and each group carries its newest comment anchor in
// `details.comment_id`, so a client lands on the comment without fetching
// older notifications of the group.
func writeInboxPage(w http.ResponseWriter, rows []db.ListInboxPageRow, limit int, scope, detailsError string) {
	resp := inboxPageResponse{Items: make([]InboxItemResponse, 0, limit), HasMore: len(rows) > limit}
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
				writeError(w, http.StatusInternalServerError, detailsError)
				return
			}
			details["comment_id"], _ = json.Marshal(row.CommentID)
			item.Details, _ = json.Marshal(details)
		}
		resp.Items = append(resp.Items, item)
	}
	if resp.HasMore {
		last := rows[len(rows)-1].InboxItem
		encoded, _ := json.Marshal(inboxPageCursor{Time: last.CreatedAt.Time.Format(time.RFC3339Nano), ID: uuidToString(last.ID), Scope: scope})
		cursor := base64.RawURLEncoding.EncodeToString(encoded)
		resp.NextCursor = &cursor
	}
	writeJSON(w, http.StatusOK, resp)
}

func newInboxFacetsResponse() inboxFacetsResponse {
	return inboxFacetsResponse{Statuses: map[string]int64{}, Priorities: map[string]int64{}, Actors: map[string]int64{}}
}

func (f *inboxFacetsResponse) add(dimension, key string, count int64) {
	switch dimension {
	case "statuses":
		f.Statuses[key] = count
	case "priorities":
		f.Priorities[key] = count
	case "actors":
		f.Actors[key] = count
	case "unread":
		f.UnreadCount = count
	}
}

// ListInboxPage pages through the active inbox by issue group, the bounded
// replacement for ListInbox. A group with both archived and active rows is
// listed here, by its newest active row, and never in the archive.
func (h *Handler) ListInboxPage(w http.ResponseWriter, r *http.Request) {
	p, limit, scope, ok := parseInboxPageRequest(w, r, activeInboxView)
	if !ok {
		return
	}
	rows, err := h.Queries.ListInboxPage(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load inbox page")
		return
	}
	writeInboxPage(w, rows, limit, scope, "invalid notification details")
}

// GetInboxFacets counts the active inbox's groups per filter value; each
// dimension respects the other active dimensions but not its own.
func (h *Handler) GetInboxFacets(w http.ResponseWriter, r *http.Request) {
	p, ok := parseInboxFilters(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.InboxFacets(r.Context(), db.InboxFacetsParams(p))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load inbox filters")
		return
	}
	resp := newInboxFacetsResponse()
	for _, row := range rows {
		resp.add(row.Dimension, row.Key, row.Count)
	}
	writeJSON(w, http.StatusOK, resp)
}
