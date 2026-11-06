package localhost

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

type sessionBrowseCursor struct {
	Version    int                         `json:"version"`
	Generation string                      `json:"generation"`
	ScopeID    int64                       `json:"scope_id"`
	Filter     store.SessionBrowseFilter   `json:"filter"`
	Position   store.SessionBrowsePosition `json:"position"`
}

func sessionBrowseFilter(r *http.Request) (store.SessionBrowseFilter, error) {
	query := r.URL.Query()
	filter := store.SessionBrowseFilter{Search: strings.TrimSpace(query.Get("q")), ExcludeID: query.Get("exclude_id"), SourceID: query.Get("side_source_id")}
	if len(filter.Search) > 1000 || len(filter.ExcludeID) > 200 || len(filter.SourceID) > 200 {
		return filter, AppError("invalid_request", "Session filter is too long")
	}
	if query.Has("folder_id") {
		id := query.Get("folder_id")
		filter.GroupID = &id
	}
	if value := query.Get("archived"); value != "" {
		archived, err := strconv.ParseBool(value)
		if err != nil {
			return filter, AppError("invalid_request", "Invalid archive filter")
		}
		filter.Archived = archived
	}
	return filter, nil
}

func (v *V2) sessionGroups(w http.ResponseWriter, r *http.Request, scopeID int64) {
	filter, err := sessionBrowseFilter(r)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	groups, seq, err := v.svc.Store().SessionGroups(r.Context(), scopeID, filter)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scope_seq": seq, "groups": groups})
}

func sameSessionFilter(a, b store.SessionBrowseFilter) bool {
	groupEqual := (a.GroupID == nil && b.GroupID == nil) || (a.GroupID != nil && b.GroupID != nil && *a.GroupID == *b.GroupID)
	return groupEqual && a.Archived == b.Archived && a.Search == b.Search && a.ExcludeID == b.ExcludeID && a.SourceID == b.SourceID
}

func (v *V2) browseSessions(w http.ResponseWriter, r *http.Request, scopeID int64) {
	filter, err := sessionBrowseFilter(r)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	limit := 8
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			writeV2Error(w, AppError("invalid_request", "Session page size must be between 1 and 100"))
			return
		}
	}
	if filter.GroupID != nil && *filter.GroupID != "" {
		if _, err := v.svc.Store().FolderProject(r.Context(), scopeID, *filter.GroupID); err != nil {
			writeV2Error(w, err)
			return
		}
	}
	position := store.SessionBrowsePosition{}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		if len(raw) > 4096 {
			writeV2Error(w, AppError("invalid_request", "Invalid session cursor"))
			return
		}
		data, err := base64.RawURLEncoding.DecodeString(raw)
		var cursor sessionBrowseCursor
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Version != 2 || cursor.Generation != v.svc.Generation() || cursor.ScopeID != scopeID || !sameSessionFilter(cursor.Filter, filter) || cursor.Position.ID == "" || (cursor.Position.Pinned != 0 && cursor.Position.Pinned != 1) || cursor.Position.SortAt == "" {
			writeV2Error(w, AppError("invalid_request", "Invalid session cursor"))
			return
		}
		if _, err := time.Parse(time.RFC3339Nano, cursor.Position.SortAt); err != nil {
			writeV2Error(w, AppError("invalid_request", "Invalid session cursor"))
			return
		}
		position = cursor.Position
	}
	rows, seq, hasMore, err := v.svc.Store().SessionPage(r.Context(), scopeID, filter, position, limit)
	if err != nil {
		writeV2Error(w, err)
		return
	}
	var next *string
	if hasMore {
		last := rows[len(rows)-1]
		sortAt := last.LastTurnSentAt
		if sortAt == "" {
			sortAt = last.CreatedAt
		}
		data, err := json.Marshal(sessionBrowseCursor{Version: 2, Generation: v.svc.Generation(), ScopeID: scopeID, Filter: filter, Position: store.SessionBrowsePosition{ID: last.ID, Pinned: last.Pinned, SortAt: sortAt}})
		if err != nil {
			writeV2Error(w, err)
			return
		}
		value := base64.RawURLEncoding.EncodeToString(data)
		next = &value
	}
	writeJSON(w, http.StatusOK, map[string]any{"scope_seq": seq, "summaries": v.summaries(r.Context(), rows), "has_more": hasMore, "next_cursor": next})
}
