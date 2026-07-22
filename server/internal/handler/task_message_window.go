package handler

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Task Message Window (MUL-5122) — the server-owned, additive read model behind
// the paged Execution Log. It encapsulates task authorization, opaque cursor
// parsing, the stable (seq, id) order, page bounds, full-Run OR filters, and
// raw/matched totals with type/tool facets. The legacy ListTaskMessagesByUser
// array endpoint is left untouched so Web Chat, Mobile and CLI stay compatible.

const (
	execLogDefaultLimit = 50
	execLogMaxLimit     = 100
)

// ExecutionLogFacet is one dynamic filter key computed across the complete Run,
// so a type or tool that exists only in not-yet-loaded history is still
// discoverable. Keys are open strings (provider-native tool names, event
// types), never a closed enum.
type ExecutionLogFacet struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// ExecutionLogPageResponse is one bounded, chronological page plus the full-Run
// context the client needs to render honest counts and continue paging.
//
//   - OlderCursor: pass as ?before= to load the next older history page. Absent
//     when no older events remain.
//   - LatestCursor: the newest edge of the client's knowledge; pass as ?after=
//     for bounded terminal catch-up. Absent on before-pages so loading history
//     never rewinds the live anchor.
//
// Cursors are opaque; clients echo them back and never parse them.
type ExecutionLogPageResponse struct {
	Messages     []protocol.TaskMessagePayload `json:"messages"`
	Limit        int                           `json:"limit"`
	OlderCursor  *string                       `json:"older_cursor,omitempty"`
	LatestCursor *string                       `json:"latest_cursor,omitempty"`
	RawTotal     int64                         `json:"raw_total"`
	MatchedTotal int64                         `json:"matched_total"`
	TypeFacets   []ExecutionLogFacet           `json:"type_facets"`
	ToolFacets   []ExecutionLogFacet           `json:"tool_facets"`
}

// encodeExecLogCursor packs a stable (seq, id) position into an opaque base64url
// token. The encoding is a server-owned detail; the API never promises its shape.
func encodeExecLogCursor(seq int32, id pgtype.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%s", seq, uuidToString(id))))
}

// decodeExecLogCursor is the inverse. Any malformed token is a single opaque
// "invalid cursor" error so the endpoint never leaks the encoding.
func decodeExecLogCursor(token string) (int32, pgtype.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, pgtype.UUID{}, errors.New("invalid cursor")
	}
	seqStr, idStr, found := strings.Cut(string(raw), ":")
	if !found {
		return 0, pgtype.UUID{}, errors.New("invalid cursor")
	}
	seq, err := strconv.Atoi(seqStr)
	if err != nil {
		return 0, pgtype.UUID{}, errors.New("invalid cursor")
	}
	id, err := util.ParseUUID(idStr)
	if err != nil {
		return 0, pgtype.UUID{}, errors.New("invalid cursor")
	}
	return int32(seq), id, nil
}

// parseExecLogFilterParam splits a comma-separated filter param into a
// non-nil (possibly empty) slice — the SQL uses cardinality(...) = 0 as the
// "no filter" test, which requires a non-null array.
func parseExecLogFilterParam(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ListTaskMessagesPage serves GET /api/tasks/{taskId}/messages/page — the paged
// Execution Log. Authorization mirrors ListTaskMessagesByUser exactly: the task
// must resolve to the caller's workspace, otherwise 404.
func (h *Handler) ListTaskMessagesPage(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
	if !ok {
		return
	}

	limit := execLogDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > execLogMaxLimit {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	beforeRaw := r.URL.Query().Get("before")
	afterRaw := r.URL.Query().Get("after")
	if beforeRaw != "" && afterRaw != "" {
		writeError(w, http.StatusBadRequest, "before and after are mutually exclusive")
		return
	}

	var (
		beforeSeq pgtype.Int4
		beforeID  pgtype.UUID
		afterSeq  int32
		afterID   pgtype.UUID
	)
	if beforeRaw != "" {
		seq, id, err := decodeExecLogCursor(beforeRaw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		beforeSeq = pgtype.Int4{Int32: seq, Valid: true}
		beforeID = id
	}
	if afterRaw != "" {
		seq, id, err := decodeExecLogCursor(afterRaw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		afterSeq = seq
		afterID = id
	}

	types := parseExecLogFilterParam(r.URL.Query().Get("types"))
	tools := parseExecLogFilterParam(r.URL.Query().Get("tools"))

	// Authorization: identical contract to ListTaskMessagesByUser — resolve the
	// task's workspace and require it to equal the caller's, else 404 (no
	// enumeration oracle).
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" || wsID != middleware.WorkspaceIDFromContext(r.Context()) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	issueID := uuidToString(task.IssueID)

	var (
		rows         []db.TaskMessage
		olderCursor  *string
		latestCursor *string
	)

	if afterRaw != "" {
		rows, err = h.Queries.ListTaskMessagesAfter(r.Context(), db.ListTaskMessagesAfterParams{
			TaskID:   taskUUID,
			AfterSeq: afterSeq,
			AfterID:  afterID,
			Types:    types,
			Tools:    tools,
			Lim:      int32(limit),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list task messages")
			return
		}
		// Ascending rows are already chronological. Advance the catch-up anchor
		// to the newest returned row; when empty, echo the caller's anchor so a
		// later poll resumes from the same point.
		if len(rows) > 0 {
			newest := rows[len(rows)-1]
			c := encodeExecLogCursor(newest.Seq, newest.ID)
			latestCursor = &c
		} else {
			c := afterRaw
			latestCursor = &c
		}
	} else {
		rows, err = h.Queries.ListTaskMessagesPage(r.Context(), db.ListTaskMessagesPageParams{
			TaskID:    taskUUID,
			BeforeSeq: beforeSeq,
			BeforeID:  beforeID,
			Types:     types,
			Tools:     tools,
			Lim:       int32(limit + 1),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list task messages")
			return
		}
		hasMore := len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
		// Rows are newest-first; the last one is the oldest in the window.
		if hasMore && len(rows) > 0 {
			oldest := rows[len(rows)-1]
			c := encodeExecLogCursor(oldest.Seq, oldest.ID)
			olderCursor = &c
		}
		// Reverse the newest-first window into chronological page order.
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
		// A no-cursor page opens fresh at the live edge, so hand back a catch-up
		// anchor. A before-page (loading history) leaves the caller's anchor
		// untouched.
		if beforeRaw == "" && len(rows) > 0 {
			newest := rows[len(rows)-1]
			c := encodeExecLogCursor(newest.Seq, newest.ID)
			latestCursor = &c
		}
	}

	rawTotal, err := h.Queries.CountTaskMessages(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count task messages")
		return
	}
	matchedTotal, err := h.Queries.CountTaskMessagesMatched(r.Context(), db.CountTaskMessagesMatchedParams{
		TaskID: taskUUID,
		Types:  types,
		Tools:  tools,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count matched task messages")
		return
	}
	typeFacetRows, err := h.Queries.TaskMessageTypeFacets(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load type facets")
		return
	}
	toolFacetRows, err := h.Queries.TaskMessageToolFacets(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tool facets")
		return
	}

	messages := make([]protocol.TaskMessagePayload, len(rows))
	for i, m := range rows {
		messages[i] = taskMessageToPayload(m, taskID, issueID)
	}
	typeFacets := make([]ExecutionLogFacet, len(typeFacetRows))
	for i, f := range typeFacetRows {
		typeFacets[i] = ExecutionLogFacet{Key: f.Type, Count: f.Count}
	}
	toolFacets := make([]ExecutionLogFacet, len(toolFacetRows))
	for i, f := range toolFacetRows {
		toolFacets[i] = ExecutionLogFacet{Key: f.Tool.String, Count: f.Count}
	}

	writeJSON(w, http.StatusOK, ExecutionLogPageResponse{
		Messages:     messages,
		Limit:        limit,
		OlderCursor:  olderCursor,
		LatestCursor: latestCursor,
		RawTotal:     rawTotal,
		MatchedTotal: matchedTotal,
		TypeFacets:   typeFacets,
		ToolFacets:   toolFacets,
	})
}
