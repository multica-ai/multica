package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TimelineEntry represents a single entry in the issue timeline, which can be
// either an activity log record or a comment.
type TimelineEntry struct {
	Type string `json:"type"` // "activity" or "comment"
	ID   string `json:"id"`

	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`
	CreatedAt string `json:"created_at"`

	// Activity-only fields
	Action  *string         `json:"action,omitempty"`
	Details json.RawMessage `json:"details,omitempty"`

	// Comment-only fields
	Content     *string              `json:"content,omitempty"`
	ParentID    *string              `json:"parent_id,omitempty"`
	UpdatedAt   *string              `json:"updated_at,omitempty"`
	CommentType *string              `json:"comment_type,omitempty"`
	Reactions   []ReactionResponse   `json:"reactions,omitempty"`
	Attachments []AttachmentResponse `json:"attachments,omitempty"`
}

// TimelineResponse wraps the cursor-paginated timeline. Entries are sorted
// newest-first (created_at DESC, id DESC). NextCursor / PrevCursor are opaque
// strings; clients pass them back as ?before= / ?after= without inspection.
// HasMoreBefore indicates more entries older than the last in the page;
// HasMoreAfter indicates more entries newer than the first in the page.
type TimelineResponse struct {
	Entries       []TimelineEntry `json:"entries"`
	NextCursor    *string         `json:"next_cursor"`
	PrevCursor    *string         `json:"prev_cursor"`
	HasMoreBefore bool            `json:"has_more_before"`
	HasMoreAfter  bool            `json:"has_more_after"`
	// TargetIndex is set only in ?around=<id> mode, locating the anchor entry
	// within Entries so the client can scroll/highlight without searching.
	TargetIndex *int `json:"target_index,omitempty"`
}

const (
	timelineDefaultLimit = 50
	timelineMaxLimit     = 100
)

// timelineCursor encodes a (created_at, id) keyset position as opaque base64
// JSON. The format is intentionally hidden from clients so future schema
// evolution (e.g. switching to a sequence column) can replace the cursor
// payload without breaking API consumers.
type timelineCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"i"`
}

func encodeTimelineCursor(t pgtype.Timestamptz, id pgtype.UUID) string {
	c := timelineCursor{CreatedAt: t.Time, ID: uuidToString(id)}
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeTimelineCursor(s string) (pgtype.Timestamptz, pgtype.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, err
	}
	var c timelineCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, err
	}
	id, err := parseUUIDStrict(c.ID)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, err
	}
	return pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}, id, nil
}

// parseUUIDStrict mirrors util.ParseUUID but returns a pgtype.UUID directly
// without panicking on bad input. Used for cursor decoding where invalid data
// is a 400, not a 500.
func parseUUIDStrict(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	if !u.Valid {
		return pgtype.UUID{}, errors.New("invalid uuid")
	}
	return u, nil
}

// ListTimeline returns a cursor-paginated, newest-first slice of the issue
// timeline (comments + activities merged). The query string accepts at most
// one of: ?before=<cursor>, ?after=<cursor>, ?around=<entry_id>. With none,
// the latest page is returned.
func (h *Handler) ListTimeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	q := r.URL.Query()

	// Backwards-compat: pre-#2128 clients (Multica.app ≤ v0.2.25 and any cached
	// web build older than the matching server) call /timeline with no query
	// string and consume the response body as TimelineEntry[] directly. The
	// new client always sends ?limit=..., so absence of every pagination param
	// uniquely identifies a legacy caller. Drop this branch once the desktop
	// auto-update has rolled the user base past v0.2.26.
	if q.Get("limit") == "" && q.Get("before") == "" &&
		q.Get("after") == "" && q.Get("around") == "" {
		h.listTimelineLegacy(w, r, issue)
		return
	}

	limit := timelineDefaultLimit
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if n > timelineMaxLimit {
			writeError(w, http.StatusBadRequest, "limit exceeds maximum of 100")
			return
		}
		limit = n
	}

	before, after, around := q.Get("before"), q.Get("after"), q.Get("around")
	modes := 0
	for _, s := range []string{before, after, around} {
		if s != "" {
			modes++
		}
	}
	if modes > 1 {
		writeError(w, http.StatusBadRequest, "before, after, and around are mutually exclusive")
		return
	}

	switch {
	case around != "":
		h.listTimelineAround(w, r, issue, around, limit)
	case before != "":
		h.listTimelineBefore(w, r, issue, before, limit)
	case after != "":
		h.listTimelineAfter(w, r, issue, after, limit)
	default:
		h.listTimelineLatest(w, r, issue, limit)
	}
}

// listTimelineLegacy serves clients that predate cursor pagination (#2128) —
// notably Multica.app ≤ v0.2.25, where the renderer reads the response body
// as TimelineEntry[] directly and would crash with "timeline.filter is not a
// function" against the new wrapped shape (#2143, #2147). Returned bounded
// at legacyTimelineCap to honour the spirit of #1968 — old clients couldn't
// render thousands of entries without freezing the tab anyway.
func (h *Handler) listTimelineLegacy(w http.ResponseWriter, r *http.Request, issue db.Issue) {
	const legacyTimelineCap = 200
	ctx := r.Context()
	comments, err := h.Queries.ListCommentsLatest(ctx, db.ListCommentsLatestParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, Limit: legacyTimelineCap,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	activities, err := h.Queries.ListActivitiesLatest(ctx, db.ListActivitiesLatestParams{
		IssueID: issue.ID, Limit: legacyTimelineCap,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}
	entries := h.mergeTimelineDesc(r, comments, activities, legacyTimelineCap)
	// Old contract: ASC (oldest → newest).
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	// Old client does `data: timeline = []` which defaults undefined, not
	// null — render an empty issue as "[]" not "null".
	if entries == nil {
		entries = []TimelineEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// listTimelineLatest fetches the most recent <limit> entries (no cursor).
// Both tables are queried for <limit> rows each; the merge picks the top
// <limit> overall. Any item the merge didn't include cannot rank higher than
// the worst kept item in either pool, so this is exact, not approximate.
func (h *Handler) listTimelineLatest(w http.ResponseWriter, r *http.Request, issue db.Issue, limit int) {
	ctx := r.Context()
	comments, err := h.Queries.ListCommentsLatest(ctx, db.ListCommentsLatestParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, Limit: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	activities, err := h.Queries.ListActivitiesLatest(ctx, db.ListActivitiesLatestParams{
		IssueID: issue.ID, Limit: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}

	entries := h.mergeTimelineDesc(r, comments, activities, limit)
	resp := TimelineResponse{Entries: entries}
	resp.HasMoreBefore = hasMoreBeyond(len(comments), len(activities), len(entries), limit)
	if resp.HasMoreBefore && len(entries) > 0 {
		c := encodeTimelineCursor(entryTimestamp(entries[len(entries)-1]), entryID(entries[len(entries)-1]))
		resp.NextCursor = &c
	}
	if len(entries) > 0 {
		c := encodeTimelineCursor(entryTimestamp(entries[0]), entryID(entries[0]))
		resp.PrevCursor = &c
	}
	// has_more_after is always false on the latest page by definition.
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listTimelineBefore(w http.ResponseWriter, r *http.Request, issue db.Issue, cursor string, limit int) {
	ctx := r.Context()
	t, id, err := decodeTimelineCursor(cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}

	comments, err := h.Queries.ListCommentsBefore(ctx, db.ListCommentsBeforeParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		Column3: t, Column4: id, Limit: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	activities, err := h.Queries.ListActivitiesBefore(ctx, db.ListActivitiesBeforeParams{
		IssueID: issue.ID, Column2: t, Column3: id, Limit: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}

	entries := h.mergeTimelineDesc(r, comments, activities, limit)
	resp := TimelineResponse{
		Entries:      entries,
		HasMoreAfter: true, // we're paging older from a known position, so newer exists
	}
	resp.HasMoreBefore = hasMoreBeyond(len(comments), len(activities), len(entries), limit)
	if resp.HasMoreBefore && len(entries) > 0 {
		c := encodeTimelineCursor(entryTimestamp(entries[len(entries)-1]), entryID(entries[len(entries)-1]))
		resp.NextCursor = &c
	}
	if len(entries) > 0 {
		c := encodeTimelineCursor(entryTimestamp(entries[0]), entryID(entries[0]))
		resp.PrevCursor = &c
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listTimelineAfter(w http.ResponseWriter, r *http.Request, issue db.Issue, cursor string, limit int) {
	ctx := r.Context()
	t, id, err := decodeTimelineCursor(cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}

	comments, err := h.Queries.ListCommentsAfter(ctx, db.ListCommentsAfterParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		Column3: t, Column4: id, Limit: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	activities, err := h.Queries.ListActivitiesAfter(ctx, db.ListActivitiesAfterParams{
		IssueID: issue.ID, Column2: t, Column3: id, Limit: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}

	// Both queries returned ASC (older→newer). Merge ASC, take the limit
	// closest to the cursor (i.e. the oldest of the "after" set), then
	// reverse to DESC for the response.
	entries := h.mergeTimelineAscThenReverse(r, comments, activities, limit)
	resp := TimelineResponse{Entries: entries, HasMoreBefore: true}
	resp.HasMoreAfter = hasMoreBeyond(len(comments), len(activities), len(entries), limit)
	if resp.HasMoreAfter && len(entries) > 0 {
		c := encodeTimelineCursor(entryTimestamp(entries[0]), entryID(entries[0]))
		resp.PrevCursor = &c
	}
	if len(entries) > 0 {
		c := encodeTimelineCursor(entryTimestamp(entries[len(entries)-1]), entryID(entries[len(entries)-1]))
		resp.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, resp)
}

// listTimelineAround anchors a window of size <limit> on a target entry,
// returning roughly half before and half after plus the target itself.
// This is the Inbox-jump / deep-link path: the target entry can be deep in
// the timeline, but the response is bounded so the browser never freezes.
func (h *Handler) listTimelineAround(w http.ResponseWriter, r *http.Request, issue db.Issue, targetID string, limit int) {
	ctx := r.Context()
	target, err := parseUUIDStrict(targetID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid around id")
		return
	}

	// Resolve the target's (created_at, id). It can be either a comment or
	// an activity; we don't ask the client to disambiguate.
	var anchorTime pgtype.Timestamptz
	var anchorID pgtype.UUID
	if c, cErr := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID: target, WorkspaceID: issue.WorkspaceID,
	}); cErr == nil && c.IssueID == issue.ID {
		anchorTime, anchorID = c.CreatedAt, c.ID
	} else if a, aErr := h.Queries.GetActivity(ctx, target); aErr == nil &&
		a.IssueID == issue.ID && a.WorkspaceID == issue.WorkspaceID {
		anchorTime, anchorID = a.CreatedAt, a.ID
	} else {
		// Neither comment nor activity matched (or wrong workspace/issue).
		// Don't leak existence — return 404 like other resource lookups.
		if cErr != nil && !errors.Is(cErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to resolve target")
			return
		}
		writeError(w, http.StatusNotFound, "timeline entry not found")
		return
	}

	half := limit / 2
	if half < 1 {
		half = 1
	}
	beforeLimit := half
	afterLimit := limit - half - 1 // -1 for the anchor itself
	if afterLimit < 0 {
		afterLimit = 0
	}

	// Older half: keyset Before (anchor exclusive).
	olderComments, err := h.Queries.ListCommentsBefore(ctx, db.ListCommentsBeforeParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		Column3: anchorTime, Column4: anchorID, Limit: int32(beforeLimit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	olderActivities, err := h.Queries.ListActivitiesBefore(ctx, db.ListActivitiesBeforeParams{
		IssueID: issue.ID, Column2: anchorTime, Column3: anchorID, Limit: int32(beforeLimit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}
	olderEntries := h.mergeTimelineDesc(r, olderComments, olderActivities, beforeLimit)

	// Newer half: keyset After (anchor exclusive).
	newerComments, err := h.Queries.ListCommentsAfter(ctx, db.ListCommentsAfterParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		Column3: anchorTime, Column4: anchorID, Limit: int32(afterLimit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	newerActivities, err := h.Queries.ListActivitiesAfter(ctx, db.ListActivitiesAfterParams{
		IssueID: issue.ID, Column2: anchorTime, Column3: anchorID, Limit: int32(afterLimit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}
	newerEntries := h.mergeTimelineAscThenReverse(r, newerComments, newerActivities, afterLimit)

	// Build the anchor entry inline using the existing single-entry path.
	anchorEntry, ok := h.fetchSingleEntry(r, issue, target)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to fetch anchor")
		return
	}

	// Final stitch: newer (DESC) + anchor + older (DESC).
	entries := make([]TimelineEntry, 0, len(newerEntries)+1+len(olderEntries))
	entries = append(entries, newerEntries...)
	entries = append(entries, anchorEntry)
	entries = append(entries, olderEntries...)
	targetIdx := len(newerEntries)

	resp := TimelineResponse{
		Entries:       entries,
		HasMoreBefore: hasMoreBeyond(len(olderComments), len(olderActivities), len(olderEntries), beforeLimit),
		HasMoreAfter:  hasMoreBeyond(len(newerComments), len(newerActivities), len(newerEntries), afterLimit),
		TargetIndex:   &targetIdx,
	}
	if resp.HasMoreBefore {
		c := encodeTimelineCursor(entryTimestamp(entries[len(entries)-1]), entryID(entries[len(entries)-1]))
		resp.NextCursor = &c
	}
	if resp.HasMoreAfter {
		c := encodeTimelineCursor(entryTimestamp(entries[0]), entryID(entries[0]))
		resp.PrevCursor = &c
	}
	writeJSON(w, http.StatusOK, resp)
}

// fetchSingleEntry materializes a single TimelineEntry (comment or activity)
// for the around-mode anchor. Reactions/attachments come from the same batch
// helpers so the rendering is identical to the merge path.
func (h *Handler) fetchSingleEntry(r *http.Request, issue db.Issue, id pgtype.UUID) (TimelineEntry, bool) {
	ctx := r.Context()
	if c, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID: id, WorkspaceID: issue.WorkspaceID,
	}); err == nil && c.IssueID == issue.ID {
		return h.commentsToEntries(r, []db.Comment{c})[0], true
	}
	if a, err := h.Queries.GetActivity(ctx, id); err == nil &&
		a.IssueID == issue.ID && a.WorkspaceID == issue.WorkspaceID {
		return activityToEntry(a), true
	}
	return TimelineEntry{}, false
}

// hasMoreBeyond reports whether entries exist beyond the page on the side the
// caller is paginating away from (older for "before", newer for "after").
//
// Three independent signals, any of which means "more rows exist":
//  1. comments >= limit — the comments query was capped, DB has more.
//  2. activities >= limit — the activities query was capped, DB has more.
//  3. comments+activities > entries — the in-memory merge dropped rows that
//     could not all fit in the page (#2192). This is the case the original
//     formula missed, which made older comments unreachable when neither
//     individual query hit the limit but their combined total exceeded it.
func hasMoreBeyond(comments, activities, entries, limit int) bool {
	if limit <= 0 {
		return false
	}
	return comments >= limit || activities >= limit || comments+activities > entries
}

// mergeTimelineDesc takes comments + activities sorted DESC by (created_at, id)
// and returns the top <limit> merged entries, also DESC. Items the merge does
// not include cannot rank higher than the worst kept item in either pool, so
// the result is exact.
func (h *Handler) mergeTimelineDesc(r *http.Request, comments []db.Comment, activities []db.ActivityLog, limit int) []TimelineEntry {
	out := make([]TimelineEntry, 0, len(comments)+len(activities))
	out = append(out, h.commentsToEntries(r, comments)...)
	for _, a := range activities {
		out = append(out, activityToEntry(a))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// mergeTimelineAscThenReverse takes comments + activities sorted ASC by
// (created_at, id) — the natural shape of an "after" keyset query — picks
// the <limit> closest to the cursor (i.e. earliest of the after-set), and
// returns them DESC for response consistency.
func (h *Handler) mergeTimelineAscThenReverse(r *http.Request, comments []db.Comment, activities []db.ActivityLog, limit int) []TimelineEntry {
	out := make([]TimelineEntry, 0, len(comments)+len(activities))
	out = append(out, h.commentsToEntries(r, comments)...)
	for _, a := range activities {
		out = append(out, activityToEntry(a))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	// Reverse to DESC.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// commentsToEntries fetches reactions + attachments for the given comments in
// one batch each and returns enriched TimelineEntry slices preserving order.
func (h *Handler) commentsToEntries(r *http.Request, comments []db.Comment) []TimelineEntry {
	if len(comments) == 0 {
		return nil
	}
	ids := make([]pgtype.UUID, len(comments))
	for i, c := range comments {
		ids[i] = c.ID
	}
	reactions := h.groupReactions(r, ids)
	attachments := h.groupAttachments(r, ids)

	out := make([]TimelineEntry, len(comments))
	for i, c := range comments {
		content := c.Content
		commentType := c.Type
		updatedAt := timestampToString(c.UpdatedAt)
		cid := uuidToString(c.ID)
		out[i] = TimelineEntry{
			Type:        "comment",
			ID:          cid,
			ActorType:   c.AuthorType,
			ActorID:     uuidToString(c.AuthorID),
			Content:     &content,
			CommentType: &commentType,
			ParentID:    uuidToPtr(c.ParentID),
			CreatedAt:   timestampToString(c.CreatedAt),
			UpdatedAt:   &updatedAt,
			Reactions:   reactions[cid],
			Attachments: attachments[cid],
		}
	}
	return out
}

func activityToEntry(a db.ActivityLog) TimelineEntry {
	action := a.Action
	actorType := ""
	if a.ActorType.Valid {
		actorType = a.ActorType.String
	}
	return TimelineEntry{
		Type:      "activity",
		ID:        uuidToString(a.ID),
		ActorType: actorType,
		ActorID:   uuidToString(a.ActorID),
		Action:    &action,
		Details:   a.Details,
		CreatedAt: timestampToString(a.CreatedAt),
	}
}

// entryTimestamp / entryID extract the cursor components for an emitted
// TimelineEntry. CreatedAt is already an RFC3339 string at this point;
// re-parse it for cursor encoding.
func entryTimestamp(e TimelineEntry) pgtype.Timestamptz {
	t, _ := time.Parse(time.RFC3339Nano, e.CreatedAt)
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func entryID(e TimelineEntry) pgtype.UUID {
	id, _ := parseUUIDStrict(e.ID)
	return id
}

// AssigneeFrequencyEntry represents how often a user assigns to a specific target.
type AssigneeFrequencyEntry struct {
	AssigneeType string `json:"assignee_type"`
	AssigneeID   string `json:"assignee_id"`
	Frequency    int64  `json:"frequency"`
}

// GetAssigneeFrequency returns assignee usage frequency for the current user,
// combining data from assignee change activities and initial issue assignments.
func (h *Handler) GetAssigneeFrequency(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	// Aggregate frequency from both data sources.
	freq := map[string]int64{} // key: "type:id"

	// Source 1: assignee_changed activities by this user.
	activityCounts, err := h.Queries.CountAssigneeChangesByActor(r.Context(), db.CountAssigneeChangesByActorParams{
		WorkspaceID: parseUUID(workspaceID),
		ActorID:     parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get assignee frequency")
		return
	}
	for _, row := range activityCounts {
		aType, _ := row.AssigneeType.(string)
		aID, _ := row.AssigneeID.(string)
		if aType != "" && aID != "" {
			freq[aType+":"+aID] += row.Frequency
		}
	}

	// Source 2: issues created by this user with an assignee.
	issueCounts, err := h.Queries.CountCreatedIssueAssignees(r.Context(), db.CountCreatedIssueAssigneesParams{
		WorkspaceID: parseUUID(workspaceID),
		CreatorID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get assignee frequency")
		return
	}
	for _, row := range issueCounts {
		if !row.AssigneeType.Valid || !row.AssigneeID.Valid {
			continue
		}
		key := row.AssigneeType.String + ":" + uuidToString(row.AssigneeID)
		freq[key] += row.Frequency
	}

	// Build sorted response.
	result := make([]AssigneeFrequencyEntry, 0, len(freq))
	for key, count := range freq {
		// Split "type:id" — type is always "member" or "agent" (no colons).
		var aType, aID string
		for i := 0; i < len(key); i++ {
			if key[i] == ':' {
				aType = key[:i]
				aID = key[i+1:]
				break
			}
		}
		result = append(result, AssigneeFrequencyEntry{
			AssigneeType: aType,
			AssigneeID:   aID,
			Frequency:    count,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Frequency > result[j].Frequency
	})

	writeJSON(w, http.StatusOK, result)
}
