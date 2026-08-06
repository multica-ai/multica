package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Regression tests for MUL-5492: the per-issue timeline cap used to be applied
// with ORDER BY created_at ASC, so once an issue accumulated more than
// timelineHardCap rows the cap discarded the NEWEST ones and the timeline
// silently appeared to stop at some point in the past.
//
// These tests pin four properties:
//  1. the cap keeps the newest window, not the oldest;
//  2. each list is capped independently — activity truncation must never delete
//     comments, so the range may legitimately hold comments older than the
//     oldest activity;
//  3. every retained reply arrives with a parent chain that reaches a root in
//     the same response, because an orphaned reply is invisible in the UI;
//  4. truncation is reported instead of being silent, naming which kinds were
//     affected.

// fetchTimelineRecorder issues GET /timeline and returns the raw recorder so
// tests can assert on response headers as well as the body.
func fetchTimelineRecorder(t *testing.T, issueID, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/issues/" + issueID + "/timeline"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", target, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListTimeline(w, req)
	return w
}

func decodeTimelineEntries(t *testing.T, w *httptest.ResponseRecorder) []TimelineEntry {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var entries []TimelineEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	return entries
}

// bulkSeedActivities inserts n activities one second apart starting at start.
// One statement, because these tests need thousands of rows.
func bulkSeedActivities(t *testing.T, issueID string, start time.Time, n int) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
		SELECT $1, $2, 'member', $3, 'status_changed', '{"from":"todo","to":"in_progress"}'::jsonb,
		       $4::timestamptz + (g * interval '1 second')
		FROM generate_series(0, $5::int - 1) AS g
	`, testWorkspaceID, issueID, testUserID, start, n)
	if err != nil {
		t.Fatalf("bulk seed %d activities: %v", n, err)
	}
}

// bulkSeedComments inserts n comments one second apart starting at start.
func bulkSeedComments(t *testing.T, issueID string, start time.Time, n int) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
		SELECT $1, $2, 'member', $3, 'bulk comment ' || g, 'comment',
		       $4::timestamptz + (g * interval '1 second'),
		       $4::timestamptz + (g * interval '1 second')
		FROM generate_series(0, $5::int - 1) AS g
	`, issueID, testWorkspaceID, testUserID, start, n)
	if err != nil {
		t.Fatalf("bulk seed %d comments: %v", n, err)
	}
}

// bulkSeedReplies inserts n direct replies to parentID one second apart.
func bulkSeedReplies(t *testing.T, issueID, parentID string, start time.Time, n int) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type,
		                     created_at, updated_at, parent_id)
		SELECT $1, $2, 'member', $3, 'bulk reply ' || g, 'comment',
		       $4::timestamptz + (g * interval '1 second'),
		       $4::timestamptz + (g * interval '1 second'),
		       $5
		FROM generate_series(0, $6::int - 1) AS g
	`, issueID, testWorkspaceID, testUserID, start, parentID, n)
	if err != nil {
		t.Fatalf("bulk seed %d replies: %v", n, err)
	}
}

// seedComment inserts a single comment at an exact timestamp, optionally as a
// reply, and returns its id.
func seedComment(t *testing.T, issueID string, at time.Time, content string, parentID *string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at, parent_id)
		VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $5, $6)
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, content, at, parentID).Scan(&id)
	if err != nil {
		t.Fatalf("seed comment %q: %v", content, err)
	}
	return id
}

func countByType(entries []TimelineEntry) (comments, activities int) {
	for _, e := range entries {
		switch e.Type {
		case "comment":
			comments++
		case "activity":
			activities++
		}
	}
	return
}

// mustParseTS parses a timeline timestamp into an instant. Tests compare
// instants, never strings: the API renders timestamps in the DB session's
// offset, so two equal instants can have different textual forms and a string
// comparison would silently pass for the wrong reason.
func mustParseTS(t *testing.T, label, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse %s timestamp %q: %v", label, raw, err)
	}
	return parsed
}

// TestListTimeline_HardCapKeepsNewestActivities is the direct regression: with
// more activities than the cap, the response must be the newest window. Before
// the fix the LAST seeded activity was missing and the FIRST one was present.
func TestListTimeline_HardCapKeepsNewestActivities(t *testing.T) {
	issueID := createIssueForTimeline(t, "hard cap keeps newest")

	// 100 rows past the cap, ending "now" so the newest row is unambiguous.
	const total = timelineHardCap + 100
	start := time.Now().UTC().Add(-time.Duration(total) * time.Second).Truncate(time.Second)
	bulkSeedActivities(t, issueID, start, total)

	oldest := start
	newest := start.Add(time.Duration(total-1) * time.Second)

	w := fetchTimelineRecorder(t, issueID, "")
	entries := decodeTimelineEntries(t, w)
	_, activityCount := countByType(entries)

	if activityCount != timelineHardCap {
		t.Errorf("activity count = %d, want %d (the cap)", activityCount, timelineHardCap)
	}

	// The newest row must be present: this is the assertion that failed before.
	if got := mustParseTS(t, "last entry", entries[len(entries)-1].CreatedAt); !got.Equal(newest) {
		t.Errorf("last entry created_at = %s, want newest seeded %s", got, newest)
	}
	// ...and the oldest must have been the one dropped.
	if got := mustParseTS(t, "first entry", entries[0].CreatedAt); got.Equal(oldest) {
		t.Errorf("oldest seeded row %s survived the cap; the cap is still trimming the wrong end", got)
	}

	if got := w.Header().Get(HeaderTimelineTruncated); got != "activity" {
		t.Errorf("%s = %q, want \"activity\": truncation must not be silent", HeaderTimelineTruncated, got)
	}
}

// TestListTimeline_ActivityTruncationDoesNotDropComments replaces an earlier
// test that asserted the opposite. That test pinned a cross-kind clamp: both
// lists trimmed to a shared floor so the window was provably contiguous. The
// clamp was wrong — comments essentially never reach the cap while activity does,
// so the shared floor was almost always the activity floor deleting comments that
// had been fetched successfully. Deleting content to buy a cosmetic property is
// the worse trade, and it is what created orphaned replies.
//
// The window is now allowed to hold comments older than the oldest activity. That
// costs activity density in the older range, which is metadata, and it is
// reported rather than hidden.
func TestListTimeline_ActivityTruncationDoesNotDropComments(t *testing.T) {
	issueID := createIssueForTimeline(t, "activity truncation keeps comments")

	const activityTotal = timelineHardCap + 100
	activityStart := time.Now().UTC().Add(-time.Duration(activityTotal) * time.Second).Truncate(time.Second)
	// Comments reach far further back than the activity window can.
	commentStart := activityStart.Add(-2 * time.Hour)

	bulkSeedActivities(t, issueID, activityStart, activityTotal)
	bulkSeedComments(t, issueID, commentStart, 50)

	w := fetchTimelineRecorder(t, issueID, "")
	entries := decodeTimelineEntries(t, w)
	commentCount, activityCount := countByType(entries)

	// Every comment survives even though all 50 predate the oldest activity.
	if commentCount != 50 {
		t.Errorf("comment count = %d, want 50: activity truncation must not delete comments", commentCount)
	}
	if activityCount != timelineHardCap {
		t.Errorf("activity count = %d, want %d", activityCount, timelineHardCap)
	}
	// Only the activity list was truncated, and the header says exactly that.
	if got := w.Header().Get(HeaderTimelineTruncated); got != "activity" {
		t.Errorf("%s = %q, want \"activity\"", HeaderTimelineTruncated, got)
	}
}

// TestListTimeline_NoOrphanedReplies is the regression for the review finding.
// Scenario: a thread root older than the comment window, a fresh reply to it
// inside the window. The reply must not come back without its parent — an orphan
// is invisible in the UI, not merely mis-nested (MUL-1847 / #2263).
func TestListTimeline_NoOrphanedReplies(t *testing.T) {
	issueID := createIssueForTimeline(t, "no orphaned replies")

	// An old root, then enough newer comments to push it out of the window,
	// then a fresh reply to that old root.
	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	rootID := seedComment(t, issueID, base, "old thread root", nil)
	bulkSeedComments(t, issueID, base.Add(time.Minute), timelineHardCap+50)
	replyID := seedComment(t, issueID, time.Now().UTC().Truncate(time.Second), "fresh reply to old root", &rootID)

	w := fetchTimelineRecorder(t, issueID, "")
	entries := decodeTimelineEntries(t, w)

	ids := make(map[string]TimelineEntry, len(entries))
	for _, e := range entries {
		ids[e.ID] = e
	}

	if _, ok := ids[replyID]; !ok {
		t.Fatal("the fresh reply is missing entirely")
	}
	if _, ok := ids[rootID]; !ok {
		t.Error("the reply's parent (an old thread root) was not backfilled: the reply would be invisible in the UI")
	}
	assertNoOrphanedReplies(t, entries)
}

// TestListTimeline_RootResolvedThreadIsComplete is the root-resolution variant
// of the partial-thread regression. The timeline UI folds immediately when the
// root has resolved_at, so returning only a backfilled root plus the newest
// reply would make its collapsed count and author list look complete while five
// older replies were missing.
func TestListTimeline_RootResolvedThreadIsComplete(t *testing.T) {
	issueID := createIssueForTimeline(t, "root-resolved thread is complete")

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	resolvedAt := base
	rootID := seedCommentRow(t, issueID, base, "settled topic", nil, &resolvedAt)
	oldReplyIDs := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		oldReplyIDs = append(oldReplyIDs, seedCommentRow(
			t,
			issueID,
			base.Add(time.Duration(i+1)*time.Second),
			"old reply",
			&rootID,
			nil,
		))
	}

	bulkSeedComments(t, issueID, base.Add(time.Minute), timelineHardCap+50)
	freshReplyID := seedCommentRow(
		t,
		issueID,
		time.Now().UTC().Truncate(time.Second),
		"fresh reply",
		&rootID,
		nil,
	)

	w := fetchTimelineRecorder(t, issueID, "")
	entries := decodeTimelineEntries(t, w)
	byID := make(map[string]TimelineEntry, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
	}

	if _, ok := byID[rootID]; !ok {
		t.Fatal("resolved root is missing")
	}
	for _, id := range append(oldReplyIDs, freshReplyID) {
		if _, ok := byID[id]; !ok {
			t.Errorf("resolved thread is partial: comment %s is missing", id)
		}
	}
	assertNoOrphanedReplies(t, entries)
}

// TestListTimeline_ReplyResolutionOutsideWindowIsRestored covers the opposite
// stale-state failure. When the resolution reply falls outside the newest
// window but a later reply remains, deriveThreadResolution would otherwise see
// no resolution and render an already-resolved thread as unresolved.
func TestListTimeline_ReplyResolutionOutsideWindowIsRestored(t *testing.T) {
	issueID := createIssueForTimeline(t, "reply resolution is restored")

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	rootID := seedCommentRow(t, issueID, base, "root question", nil, nil)
	resolvedAt := base.Add(time.Second)
	resolutionID := seedCommentRow(
		t,
		issueID,
		resolvedAt,
		"the conclusion",
		&rootID,
		&resolvedAt,
	)

	bulkSeedComments(t, issueID, base.Add(time.Minute), timelineHardCap+50)
	freshReplyID := seedCommentRow(
		t,
		issueID,
		time.Now().UTC().Truncate(time.Second),
		"later follow-up",
		&rootID,
		nil,
	)

	w := fetchTimelineRecorder(t, issueID, "")
	entries := decodeTimelineEntries(t, w)
	byID := make(map[string]TimelineEntry, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
	}

	resolution, ok := byID[resolutionID]
	if !ok {
		t.Fatal("resolution reply outside the newest window was not restored")
	}
	if resolution.ResolvedAt == nil {
		t.Fatal("restored resolution reply lost resolved_at")
	}
	if _, ok := byID[freshReplyID]; !ok {
		t.Fatal("fresh reply is missing")
	}
	assertNoOrphanedReplies(t, entries)
}

// TestListTimeline_OversizedAffectedThreadIsDroppedWhole pins the bounded
// fallback. If one cut thread cannot fit in the shared context budget, returning
// its root plus an arbitrary suffix would let old clients fold partial data.
// The whole affected thread must disappear while truncation remains explicit.
func TestListTimeline_OversizedAffectedThreadIsDroppedWhole(t *testing.T) {
	issueID := createIssueForTimeline(t, "oversized affected thread is dropped")

	base := time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Second)
	rootID := seedComment(t, issueID, base, "oversized thread root", nil)
	bulkSeedReplies(
		t,
		issueID,
		rootID,
		base.Add(time.Second),
		commentHardCap+commentThreadContextBudget+1,
	)

	w := fetchTimelineRecorder(t, issueID, "")
	entries := decodeTimelineEntries(t, w)
	commentCount, _ := countByType(entries)
	if commentCount != 0 {
		t.Errorf("comment count = %d, want 0: an over-budget affected thread must be dropped whole", commentCount)
	}
	if got := w.Header().Get(HeaderTimelineTruncated); got != "comment" {
		t.Errorf("%s = %q, want \"comment\"", HeaderTimelineTruncated, got)
	}
}

// assertNoOrphanedReplies pins the invariant issue-detail.tsx relies on and
// foldResolvedThreads requires: every returned reply's parent is in the same set.
// More valuable than any single scenario — it holds for every shape of response.
func assertNoOrphanedReplies(t *testing.T, entries []TimelineEntry) {
	t.Helper()
	present := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		present[e.ID] = struct{}{}
	}
	for _, e := range entries {
		if e.Type != "comment" || e.ParentID == nil || *e.ParentID == "" {
			continue
		}
		if _, ok := present[*e.ParentID]; !ok {
			t.Errorf("comment %s references parent %s which is absent from the response", e.ID, *e.ParentID)
		}
	}
}

// TestListTimeline_BackfilledRootKeepsQuickActionID guards the projection of
// newly-added comment columns through parent-chain completion. An earlier
// revision hand-copied SQL rows into db.Comment field by field, which silently
// dropped quick_action_id when main added it — a backfilled quick-action root
// would have rendered as a raw prompt instead of a quick-action card. The query
// now returns db.Comment directly so new columns cannot be lost this way.
func TestListTimeline_BackfilledRootKeepsQuickActionID(t *testing.T) {
	issueID := createIssueForTimeline(t, "backfilled root keeps quick_action_id")

	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	quickActionID := "11111111-2222-3333-4444-555555555555"
	var rootID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type,
		                     created_at, updated_at, quick_action_id)
		VALUES ($1, $2, 'member', $3, 'quick action root', 'comment', $4, $4, $5)
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, base, quickActionID).Scan(&rootID)
	if err != nil {
		t.Fatalf("seed quick-action root: %v", err)
	}

	// Push the root out of the window, then reply to it from inside the window.
	bulkSeedComments(t, issueID, base.Add(time.Minute), timelineHardCap+50)
	seedComment(t, issueID, time.Now().UTC().Truncate(time.Second), "reply to quick action root", &rootID)

	w := fetchTimelineRecorder(t, issueID, "")
	entries := decodeTimelineEntries(t, w)

	var root *TimelineEntry
	for i := range entries {
		if entries[i].ID == rootID {
			root = &entries[i]
		}
	}
	if root == nil {
		t.Fatal("the quick-action root was not backfilled")
	}
	if root.QuickActionID == nil {
		t.Fatal("quick_action_id was dropped on the backfilled root: the UI would show a raw prompt instead of a quick-action card")
	}
	if *root.QuickActionID != quickActionID {
		t.Errorf("quick_action_id = %s, want %s", *root.QuickActionID, quickActionID)
	}
	assertNoOrphanedReplies(t, entries)
}

// TestListTimeline_ExactlyAtCapIsNotTruncated pins the cap+1 probe read. An
// issue that happens to hold exactly timelineHardCap rows is complete, so
// reporting it as truncated would be a lie and would trigger a needless
// ancestor-backfill query.
func TestListTimeline_ExactlyAtCapIsNotTruncated(t *testing.T) {
	issueID := createIssueForTimeline(t, "exactly at cap")

	start := time.Now().UTC().Add(-time.Duration(timelineHardCap) * time.Second).Truncate(time.Second)
	bulkSeedActivities(t, issueID, start, timelineHardCap)

	w := fetchTimelineRecorder(t, issueID, "")
	entries := decodeTimelineEntries(t, w)

	if got := w.Header().Get(HeaderTimelineTruncated); got != "" {
		t.Errorf("%s = %q, want unset: exactly-at-cap is a complete timeline", HeaderTimelineTruncated, got)
	}
	_, activityCount := countByType(entries)
	if activityCount != timelineHardCap {
		t.Errorf("activity count = %d, want %d", activityCount, timelineHardCap)
	}
}

// TestListTimeline_WrappedShapeReportsHasMoreBefore checks the legacy wrapped
// response also stops claiming the timeline is complete. has_more_before was
// hardcoded false.
func TestListTimeline_WrappedShapeReportsHasMoreBefore(t *testing.T) {
	issueID := createIssueForTimeline(t, "wrapped has_more_before")

	const total = timelineHardCap + 10
	start := time.Now().UTC().Add(-time.Duration(total) * time.Second).Truncate(time.Second)
	bulkSeedActivities(t, issueID, start, total)

	w := fetchTimelineRecorder(t, issueID, "limit=50")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp timelinePaginatedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode wrapped response: %v", err)
	}
	if !resp.HasMoreBefore {
		t.Error("has_more_before = false, want true when the cap clamped the window")
	}
	if resp.HasMoreAfter {
		t.Error("has_more_after = true, want false: the newest end is always complete now")
	}
	// The wrapped shape is newest-first, so entry 0 is the newest row.
	newest := start.Add(time.Duration(total-1) * time.Second)
	if len(resp.Entries) == 0 {
		t.Fatal("wrapped response returned no entries")
	}
	if got := mustParseTS(t, "newest entry", resp.Entries[0].CreatedAt); !got.Equal(newest) {
		t.Errorf("first (newest-first) entry created_at = %s, want %s", got, newest)
	}
}

// TestListCommentsForIssue_KeepsNewestWindow covers the same query from the
// comment-list endpoint's side, since ListCommentsForIssue is shared. The
// ascending contract must be preserved while the cap now bites at the old end.
func TestListCommentsForIssue_KeepsNewestWindow(t *testing.T) {
	issueID := createIssueForTimeline(t, "comment cap keeps newest")

	const total = 30
	start := time.Now().UTC().Add(-time.Duration(total) * time.Second).Truncate(time.Second)
	bulkSeedComments(t, issueID, start, total)

	issueUUID := parseUUID(issueID)
	wsUUID := parseUUID(testWorkspaceID)

	const limit = 10
	rows, err := testHandler.Queries.ListCommentsForIssue(context.Background(), db.ListCommentsForIssueParams{
		IssueID:     issueUUID,
		WorkspaceID: wsUUID,
		Limit:       limit,
	})
	if err != nil {
		t.Fatalf("ListCommentsForIssue: %v", err)
	}
	if len(rows) != limit {
		t.Fatalf("row count = %d, want %d", len(rows), limit)
	}
	// Newest window: the last `limit` rows of the seeded range.
	wantFirst := start.Add(time.Duration(total-limit) * time.Second)
	wantLast := start.Add(time.Duration(total-1) * time.Second)
	if got := rows[0].CreatedAt.Time.UTC(); !got.Equal(wantFirst) {
		t.Errorf("first row created_at = %s, want %s (newest window, ascending)", got, wantFirst)
	}
	if got := rows[len(rows)-1].CreatedAt.Time.UTC(); !got.Equal(wantLast) {
		t.Errorf("last row created_at = %s, want %s (newest row)", got, wantLast)
	}
	// Ascending order preserved for every existing caller.
	for i := 1; i < len(rows); i++ {
		if rows[i].CreatedAt.Time.Before(rows[i-1].CreatedAt.Time) {
			t.Fatalf("row %d is older than row %d: ascending contract broken", i, i-1)
		}
	}
}
