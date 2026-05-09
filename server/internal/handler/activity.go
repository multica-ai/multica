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
	// timelineDefaultLimit governs the per-page COMMENT budget. Activities are
	// fetched at the same per-call cap but do not consume the budget (#1857) —
	// they decorate the comment stream. Without that split, an issue with
	// sparse comments but dense activity (agent runs, status flips) triggered
	// "show older" prematurely and felt like comments had vanished.
	timelineDefaultLimit = 50
	timelineMaxLimit     = 100
)

// cursorPos is a single (created_at, id) keyset position. Used per-pool —
// see timelineCursor.
type cursorPos struct {
	T  pgtype.Timestamptz
	ID pgtype.UUID
}

// timelineCursor encodes per-pool keyset positions as opaque base64 JSON.
// Comments and activities walk independently (#1857 follow-up): a single
// shared cursor anchored on the merged-page boundary would let an activity
// older than every visible comment hide all unreturned comments behind it,
// since `ListCommentsBefore(activityCursor)` would skip the in-between rows.
// The format is intentionally hidden from clients so future schema evolution
// can replace the payload without breaking API consumers.
type timelineCursor struct {
	CommentT   time.Time `json:"ct"`
	CommentID  string    `json:"ci"`
	ActivityT  time.Time `json:"at"`
	ActivityID string    `json:"ai"`
}

func encodeTimelineCursor(comment, activity cursorPos) string {
	c := timelineCursor{
		CommentT:   comment.T.Time,
		CommentID:  uuidToString(comment.ID),
		ActivityT:  activity.T.Time,
		ActivityID: uuidToString(activity.ID),
	}
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeTimelineCursor(s string) (comment, activity cursorPos, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursorPos{}, cursorPos{}, err
	}
	var c timelineCursor
	if err = json.Unmarshal(raw, &c); err != nil {
		return cursorPos{}, cursorPos{}, err
	}
	cid, err := parseUUIDStrict(c.CommentID)
	if err != nil {
		return cursorPos{}, cursorPos{}, err
	}
	aid, err := parseUUIDStrict(c.ActivityID)
	if err != nil {
		return cursorPos{}, cursorPos{}, err
	}
	return cursorPos{T: pgtype.Timestamptz{Time: c.CommentT, Valid: true}, ID: cid},
		cursorPos{T: pgtype.Timestamptz{Time: c.ActivityT, Valid: true}, ID: aid},
		nil
}

// commentBoundsDesc returns (oldest, newest) cursor positions from a DESC-
// ordered comment slice. If the slice is empty, returns the supplied carry
// position so the cursor walker keeps advancing the empty pool past
// boundaries the caller already paged through.
func commentBoundsDesc(rows []db.Comment, carry cursorPos) (oldest, newest cursorPos) {
	if len(rows) == 0 {
		return carry, carry
	}
	return cursorPos{T: rows[len(rows)-1].CreatedAt, ID: rows[len(rows)-1].ID},
		cursorPos{T: rows[0].CreatedAt, ID: rows[0].ID}
}

func commentBoundsAsc(rows []db.Comment, carry cursorPos) (oldest, newest cursorPos) {
	if len(rows) == 0 {
		return carry, carry
	}
	return cursorPos{T: rows[0].CreatedAt, ID: rows[0].ID},
		cursorPos{T: rows[len(rows)-1].CreatedAt, ID: rows[len(rows)-1].ID}
}

func activityBoundsDesc(rows []db.ActivityLog, carry cursorPos) (oldest, newest cursorPos) {
	if len(rows) == 0 {
		return carry, carry
	}
	return cursorPos{T: rows[len(rows)-1].CreatedAt, ID: rows[len(rows)-1].ID},
		cursorPos{T: rows[0].CreatedAt, ID: rows[0].ID}
}

func activityBoundsAsc(rows []db.ActivityLog, carry cursorPos) (oldest, newest cursorPos) {
	if len(rows) == 0 {
		return carry, carry
	}
	return cursorPos{T: rows[0].CreatedAt, ID: rows[0].ID},
		cursorPos{T: rows[len(rows)-1].CreatedAt, ID: rows[len(rows)-1].ID}
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
	entries := h.mergeTimelineDesc(r, comments, activities)
	if len(entries) > legacyTimelineCap {
		entries = entries[:legacyTimelineCap]
	}
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

// listTimelineLatest fetches the latest page (no cursor). <limit> is the
// COMMENT page size (#1857); activity rows ride along at the same per-call
// SQL cap but do not consume the page budget — has_more_before is gated on
// comments alone, so a chatty agent's status flips can't push real comments
// off-page.
func (h *Handler) listTimelineLatest(w http.ResponseWriter, r *http.Request, issue db.Issue, limit int) {
	ctx := r.Context()
	// Over-fetch comments by one so commentOverflow can distinguish "exactly
	// <limit> comments exist" (no Show older needed) from ">limit comments
	// exist" (Show older required).
	rawComments, err := h.Queries.ListCommentsLatest(ctx, db.ListCommentsLatestParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, Limit: int32(limit + 1),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	comments, hasMoreComments := commentOverflow(rawComments, limit)
	activities, err := h.Queries.ListActivitiesLatest(ctx, db.ListActivitiesLatestParams{
		IssueID: issue.ID, Limit: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}

	entries := h.mergeTimelineDesc(r, comments, activities)
	resp := TimelineResponse{Entries: entries}
	resp.HasMoreBefore = hasMoreComments

	// Per-pool boundaries. For latest mode there is no input cursor; if a
	// pool returned no rows it carries from the other pool so the encoded
	// payload stays self-contained. Future calls won't fetch new rows for
	// the empty pool anyway (latest with 0 of one type means the issue has
	// none), so the carry value is purely cosmetic.
	cOldest, cNewest := commentBoundsDesc(comments, cursorPos{})
	aOldest, aNewest := activityBoundsDesc(activities, cursorPos{})
	if len(comments) == 0 {
		cOldest, cNewest = aOldest, aNewest
	}
	if len(activities) == 0 {
		aOldest, aNewest = cOldest, cNewest
	}

	if resp.HasMoreBefore && len(entries) > 0 {
		c := encodeTimelineCursor(cOldest, aOldest)
		resp.NextCursor = &c
	}
	if len(entries) > 0 {
		c := encodeTimelineCursor(cNewest, aNewest)
		resp.PrevCursor = &c
	}
	// has_more_after is always false on the latest page by definition.
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listTimelineBefore(w http.ResponseWriter, r *http.Request, issue db.Issue, cursor string, limit int) {
	ctx := r.Context()
	inComment, inActivity, err := decodeTimelineCursor(cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}

	rawComments, err := h.Queries.ListCommentsBefore(ctx, db.ListCommentsBeforeParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		Column3: inComment.T, Column4: inComment.ID, Limit: int32(limit + 1),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	comments, hasMoreComments := commentOverflow(rawComments, limit)
	activities, err := h.Queries.ListActivitiesBefore(ctx, db.ListActivitiesBeforeParams{
		IssueID: issue.ID, Column2: inActivity.T, Column3: inActivity.ID, Limit: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}

	entries := h.mergeTimelineDesc(r, comments, activities)
	resp := TimelineResponse{
		Entries:      entries,
		HasMoreAfter: true, // we're paging older from a known position, so newer exists
	}
	resp.HasMoreBefore = hasMoreComments

	// Per-pool boundaries. Empty pool carries forward from the input cursor
	// so subsequent older pages keep advancing past previously-paginated rows
	// in that pool.
	cOldest, cNewest := commentBoundsDesc(comments, inComment)
	aOldest, aNewest := activityBoundsDesc(activities, inActivity)

	if resp.HasMoreBefore && len(entries) > 0 {
		c := encodeTimelineCursor(cOldest, aOldest)
		resp.NextCursor = &c
	}
	if len(entries) > 0 {
		c := encodeTimelineCursor(cNewest, aNewest)
		resp.PrevCursor = &c
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listTimelineAfter(w http.ResponseWriter, r *http.Request, issue db.Issue, cursor string, limit int) {
	ctx := r.Context()
	inComment, inActivity, err := decodeTimelineCursor(cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}

	rawComments, err := h.Queries.ListCommentsAfter(ctx, db.ListCommentsAfterParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		Column3: inComment.T, Column4: inComment.ID, Limit: int32(limit + 1),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	// ASC fetch returns oldest-first; trimming to the first <limit> keeps
	// the rows closest to the cursor and drops the (limit+1)th newest as the
	// overflow probe.
	comments, hasMoreComments := commentOverflow(rawComments, limit)
	activities, err := h.Queries.ListActivitiesAfter(ctx, db.ListActivitiesAfterParams{
		IssueID: issue.ID, Column2: inActivity.T, Column3: inActivity.ID, Limit: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}

	// Both queries returned ASC (older→newer); reverse to DESC for the
	// response. No outer truncation: each pool is already capped by the SQL
	// LIMIT, and dropping rows here would re-introduce the comments-pushed-
	// off-page bug (#1857).
	entries := h.mergeTimelineAscThenReverse(r, comments, activities)
	resp := TimelineResponse{Entries: entries, HasMoreBefore: true}
	resp.HasMoreAfter = hasMoreComments

	cOldest, cNewest := commentBoundsAsc(comments, inComment)
	aOldest, aNewest := activityBoundsAsc(activities, inActivity)

	if resp.HasMoreAfter && len(entries) > 0 {
		c := encodeTimelineCursor(cNewest, aNewest)
		resp.PrevCursor = &c
	}
	if len(entries) > 0 {
		c := encodeTimelineCursor(cOldest, aOldest)
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

	// Older half: keyset Before (anchor exclusive). Over-fetch comments by
	// one to detect overflow exactly.
	rawOlderComments, err := h.Queries.ListCommentsBefore(ctx, db.ListCommentsBeforeParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		Column3: anchorTime, Column4: anchorID, Limit: int32(beforeLimit + 1),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	olderComments, hasMoreOlderComments := commentOverflow(rawOlderComments, beforeLimit)
	olderActivities, err := h.Queries.ListActivitiesBefore(ctx, db.ListActivitiesBeforeParams{
		IssueID: issue.ID, Column2: anchorTime, Column3: anchorID, Limit: int32(beforeLimit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}
	olderEntries := h.mergeTimelineDesc(r, olderComments, olderActivities)

	// Newer half: keyset After (anchor exclusive).
	rawNewerComments, err := h.Queries.ListCommentsAfter(ctx, db.ListCommentsAfterParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		Column3: anchorTime, Column4: anchorID, Limit: int32(afterLimit + 1),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	newerComments, hasMoreNewerComments := commentOverflow(rawNewerComments, afterLimit)
	newerActivities, err := h.Queries.ListActivitiesAfter(ctx, db.ListActivitiesAfterParams{
		IssueID: issue.ID, Column2: anchorTime, Column3: anchorID, Limit: int32(afterLimit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}
	newerEntries := h.mergeTimelineAscThenReverse(r, newerComments, newerActivities)

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
		HasMoreBefore: hasMoreOlderComments,
		HasMoreAfter:  hasMoreNewerComments,
		TargetIndex:   &targetIdx,
	}

	// Per-pool boundaries on each half. Empty pools fall back to the anchor
	// position, which is exclusive on both sides — so a follow-up Before /
	// After call against the anchor returns no duplicates.
	anchor := cursorPos{T: anchorTime, ID: anchorID}
	olderCommentOldest, _ := commentBoundsDesc(olderComments, anchor)
	olderActivityOldest, _ := activityBoundsDesc(olderActivities, anchor)
	_, newerCommentNewest := commentBoundsAsc(newerComments, anchor)
	_, newerActivityNewest := activityBoundsAsc(newerActivities, anchor)

	if resp.HasMoreBefore {
		c := encodeTimelineCursor(olderCommentOldest, olderActivityOldest)
		resp.NextCursor = &c
	}
	if resp.HasMoreAfter {
		c := encodeTimelineCursor(newerCommentNewest, newerActivityNewest)
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

// commentOverflow trims an over-fetched comment slice to <limit> and reports
// whether the SQL returned more rows than the visible budget. Callers
// over-fetch by one (limit+1) so the boolean is exact even when the issue has
// EXACTLY <limit> comments — the prior `len >= limit` check returned true in
// that case and rendered a "Show older" affordance that revealed nothing.
//
// Activity rows do not gate pagination (#1857): a dense activity stream from
// agent runs / status flips would otherwise trigger "show older" on issues
// with only a handful of real comments. Activities therefore stay capped at
// <limit> with no overflow probe.
func commentOverflow(rows []db.Comment, limit int) ([]db.Comment, bool) {
	if limit <= 0 {
		return rows, false
	}
	if len(rows) > limit {
		return rows[:limit], true
	}
	return rows, false
}

// mergeTimelineDesc returns comments + activities merged DESC by
// (created_at, id). No truncation: both pools are individually capped at the
// SQL layer, and dropping rows here would re-introduce the bug where dense
// activity pushed real comments off-page (#1857). Callers that need an outer
// safety cap (legacy compat path) apply it themselves.
func (h *Handler) mergeTimelineDesc(r *http.Request, comments []db.Comment, activities []db.ActivityLog) []TimelineEntry {
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
	return out
}

// mergeTimelineAscThenReverse takes comments + activities sorted ASC by
// (created_at, id) — the natural shape of an "after" keyset query — and
// returns them DESC for response consistency. No truncation, same reason as
// mergeTimelineDesc.
func (h *Handler) mergeTimelineAscThenReverse(r *http.Request, comments []db.Comment, activities []db.ActivityLog) []TimelineEntry {
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
