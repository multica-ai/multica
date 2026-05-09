package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fetchTimeline issues a GET /timeline request with the given query string and
// returns the decoded TimelineResponse + HTTP status.
func fetchTimeline(t *testing.T, issueID, query string) (TimelineResponse, int) {
	t.Helper()
	url := "/api/issues/" + issueID + "/timeline"
	if query != "" {
		url += "?" + query
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", url, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListTimeline(w, req)
	var resp TimelineResponse
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code
}

// createIssueForTimeline returns a freshly-created issue id and registers a
// cleanup so its timeline rows are deleted after the test.
func createIssueForTimeline(t *testing.T, title string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue.ID
}

// seedTimelineEntries inserts <commentN> comments + <activityN> activities for
// the given issue with descending timestamps (oldest first → newest last) so
// callers can reason about ordering. Returns the inserted comment + activity
// IDs in the order they were inserted (chronologically ascending).
func seedTimelineEntries(t *testing.T, issueID string, commentN, activityN int) (commentIDs, activityIDs []string) {
	t.Helper()
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Duration(commentN+activityN) * time.Minute)

	for i := 0; i < commentN; i++ {
		var id string
		ts := base.Add(time.Duration(i) * time.Minute)
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $5)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, fmt.Sprintf("comment %d", i), ts).Scan(&id); err != nil {
			t.Fatalf("seed comment %d: %v", i, err)
		}
		commentIDs = append(commentIDs, id)
	}
	for i := 0; i < activityN; i++ {
		var id string
		ts := base.Add(time.Duration(commentN+i) * time.Minute)
		if err := testPool.QueryRow(ctx, `
			INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
			VALUES ($1, $2, 'member', $3, 'status_changed', '{"from":"todo","to":"in_progress"}'::jsonb, $4)
			RETURNING id
		`, testWorkspaceID, issueID, testUserID, ts).Scan(&id); err != nil {
			t.Fatalf("seed activity %d: %v", i, err)
		}
		activityIDs = append(activityIDs, id)
	}
	return
}

func TestListTimeline_DefaultLatestPage(t *testing.T) {
	issueID := createIssueForTimeline(t, "Latest page test")
	// 80 comments triggers the comment overflow signal; activities are
	// excluded so the per-page count is unambiguous (#1857: activities don't
	// gate has_more_before).
	seedTimelineEntries(t, issueID, 80, 0)

	// Empty query string is now reserved for the legacy compat path; new
	// client always sends ?limit=... so emulate that here.
	resp, code := fetchTimeline(t, issueID, "limit=30")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(resp.Entries) != 30 {
		t.Fatalf("expected 30 entries on default page, got %d", len(resp.Entries))
	}
	if !resp.HasMoreBefore {
		t.Fatalf("expected has_more_before=true with 80 comments")
	}
	if resp.HasMoreAfter {
		t.Fatalf("latest page must report has_more_after=false")
	}
	if resp.NextCursor == nil {
		t.Fatalf("expected next_cursor on full page")
	}
	// DESC order: first entry's timestamp must be >= last entry's.
	if resp.Entries[0].CreatedAt < resp.Entries[len(resp.Entries)-1].CreatedAt {
		t.Fatalf("expected DESC order, first=%s last=%s",
			resp.Entries[0].CreatedAt, resp.Entries[len(resp.Entries)-1].CreatedAt)
	}
}

func TestListTimeline_BeforeCursorWalksOlder(t *testing.T) {
	issueID := createIssueForTimeline(t, "Before cursor test")
	// Comments-only so the page-count assertions are stable. limit=20 →
	// 20 comments per page, no activities to inflate the totals.
	seedTimelineEntries(t, issueID, 60, 0)

	first, _ := fetchTimeline(t, issueID, "limit=20")
	if len(first.Entries) != 20 {
		t.Fatalf("first page: expected 20, got %d", len(first.Entries))
	}
	if first.NextCursor == nil {
		t.Fatalf("first page should have next_cursor")
	}

	second, code := fetchTimeline(t, issueID, "limit=20&before="+*first.NextCursor)
	if code != http.StatusOK {
		t.Fatalf("second page: expected 200, got %d", code)
	}
	if len(second.Entries) != 20 {
		t.Fatalf("second page: expected 20, got %d", len(second.Entries))
	}
	if !second.HasMoreAfter {
		t.Fatalf("second page must report has_more_after=true (we paged backward)")
	}
	// No overlap: oldest of first page must be strictly newer than newest of second.
	firstTail := first.Entries[len(first.Entries)-1]
	secondHead := second.Entries[0]
	if firstTail.CreatedAt < secondHead.CreatedAt {
		t.Fatalf("pages overlap: firstTail=%s secondHead=%s",
			firstTail.CreatedAt, secondHead.CreatedAt)
	}
}

func TestListTimeline_AfterCursorWalksNewer(t *testing.T) {
	issueID := createIssueForTimeline(t, "After cursor test")
	// Comments-only seed so cursor walking depends on comment overflow alone
	// (#1857: activities don't gate has_more_after either).
	seedTimelineEntries(t, issueID, 60, 0)

	first, _ := fetchTimeline(t, issueID, "limit=20")
	if first.NextCursor == nil {
		t.Fatalf("first page should have next_cursor")
	}
	older, _ := fetchTimeline(t, issueID, "limit=20&before="+*first.NextCursor)
	if older.PrevCursor == nil {
		t.Fatalf("older page should have prev_cursor")
	}

	// Walk back forward: ?after=older.prev_cursor should land on entries
	// newer than the older page's newest, i.e. overlap with first page.
	newer, code := fetchTimeline(t, issueID, "limit=20&after="+*older.PrevCursor)
	if code != http.StatusOK {
		t.Fatalf("after page: expected 200, got %d", code)
	}
	if len(newer.Entries) == 0 {
		t.Fatalf("after page should not be empty")
	}
	if !newer.HasMoreBefore {
		t.Fatalf("after page must report has_more_before=true")
	}
}

func TestListTimeline_AroundAnchorsOnTarget(t *testing.T) {
	issueID := createIssueForTimeline(t, "Around test")
	commentIDs, _ := seedTimelineEntries(t, issueID, 50, 0)
	// commentIDs[0] is the OLDEST. Pick the 2nd-oldest as the anchor — far
	// from the latest page so we can verify around mode actually works.
	target := commentIDs[1]

	resp, code := fetchTimeline(t, issueID, "around="+target+"&limit=20")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if resp.TargetIndex == nil {
		t.Fatalf("expected target_index in around mode")
	}
	if len(resp.Entries) == 0 || resp.Entries[*resp.TargetIndex].ID != target {
		t.Fatalf("target_index does not point at target id; got %s",
			resp.Entries[*resp.TargetIndex].ID)
	}
	// Should have entries on both sides of the anchor (the 2nd-oldest has
	// 1 older + many newer).
	if !resp.HasMoreAfter {
		t.Fatalf("around 2nd-oldest should report has_more_after=true")
	}
}

func TestListTimeline_AroundUnknownTarget(t *testing.T) {
	issueID := createIssueForTimeline(t, "Around 404 test")
	seedTimelineEntries(t, issueID, 5, 0)

	bogus := "00000000-0000-0000-0000-000000000001"
	_, code := fetchTimeline(t, issueID, "around="+bogus)
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown anchor, got %d", code)
	}
}

func TestListTimeline_LimitOverMaxRejected(t *testing.T) {
	issueID := createIssueForTimeline(t, "Limit cap test")
	seedTimelineEntries(t, issueID, 1, 0)

	_, code := fetchTimeline(t, issueID, "limit=500")
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for limit=500, got %d", code)
	}
}

func TestListTimeline_MutuallyExclusiveCursorParams(t *testing.T) {
	issueID := createIssueForTimeline(t, "Mutex test")
	seedTimelineEntries(t, issueID, 1, 0)

	_, code := fetchTimeline(t, issueID, "before=abc&after=def")
	if code != http.StatusBadRequest {
		t.Fatalf("before+after should 400, got %d", code)
	}
}

func TestListTimeline_InvalidCursorRejected(t *testing.T) {
	issueID := createIssueForTimeline(t, "Bad cursor test")
	seedTimelineEntries(t, issueID, 1, 0)

	_, code := fetchTimeline(t, issueID, "before=not-base64-json")
	if code != http.StatusBadRequest {
		t.Fatalf("invalid cursor should 400, got %d", code)
	}
}

func TestListTimeline_MergedCommentAndActivity(t *testing.T) {
	issueID := createIssueForTimeline(t, "Merge test")
	ctx := context.Background()

	// Use explicit, well-separated timestamps so the DESC ordering assertion
	// is deterministic regardless of clock granularity.
	older := time.Now().UTC().Add(-2 * time.Hour)
	newer := older.Add(1 * time.Hour)

	// Older row: activity.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
		VALUES ($1, $2, 'member', $3, 'created', '{}'::jsonb, $4)
	`, testWorkspaceID, issueID, testUserID, older); err != nil {
		t.Fatalf("seed activity: %v", err)
	}
	// Newer row: comment.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
		VALUES ($1, $2, 'member', $3, 'merge test comment', 'comment', $4, $4)
	`, issueID, testWorkspaceID, testUserID, newer); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	resp, code := fetchTimeline(t, issueID, "limit=50")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp.Entries))
	}
	// DESC: comment (newer) at index 0, activity (older) at index 1.
	if resp.Entries[0].Type != "comment" || resp.Entries[1].Type != "activity" {
		t.Fatalf("merge order wrong: got %s/%s, want comment/activity",
			resp.Entries[0].Type, resp.Entries[1].Type)
	}
	if !strings.Contains(*resp.Entries[0].Content, "merge test") {
		t.Fatalf("comment content lost in merge: %v", resp.Entries[0].Content)
	}
}

// TestListTimeline_LegacyShapeForPreCursorClients pins the backwards-compat
// contract for clients that predate cursor pagination (#2128). They call
// /timeline with no query string and read the body as TimelineEntry[]
// directly — returning the new wrapped shape there is what caused #2143 /
// #2147. Asserts: array shape, ASC order, "[]" (not "null") on empty issue.
func TestListTimeline_LegacyShapeForPreCursorClients(t *testing.T) {
	issueID := createIssueForTimeline(t, "Legacy compat test")
	seedTimelineEntries(t, issueID, 3, 2) // 5 total

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/timeline", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListTimeline(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Must decode as a bare array, not the wrapped TimelineResponse.
	var entries []TimelineEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("legacy response must be a JSON array: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries (3 comments + 2 activities), got %d", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].CreatedAt > entries[i].CreatedAt {
			t.Fatalf("legacy contract requires ASC order, got %s before %s",
				entries[i-1].CreatedAt, entries[i].CreatedAt)
		}
	}

	// Empty issue must render as "[]" (not "null") — old client does
	// `data: timeline = []` which defaults undefined but not null.
	emptyID := createIssueForTimeline(t, "Empty legacy test")
	w2 := httptest.NewRecorder()
	req2 := newRequest("GET", "/api/issues/"+emptyID+"/timeline", nil)
	req2 = withURLParam(req2, "id", emptyID)
	testHandler.ListTimeline(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("empty issue: expected 200, got %d", w2.Code)
	}
	if got := strings.TrimSpace(w2.Body.String()); got != "[]" {
		t.Fatalf("empty issue must render as [], got %q", got)
	}
}

// TestTimelineCursor_RoundTrip pins the dual-pool cursor format. Cursors carry
// independent comment and activity positions (#1857 follow-up) so future
// pages walk each pool past its own boundary instead of skipping rows when
// one pool's oldest is older than the other's.
func TestTimelineCursor_RoundTrip(t *testing.T) {
	cT := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	aT := time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC)
	cID, _ := parseUUIDStrict("11111111-1111-1111-1111-111111111111")
	aID, _ := parseUUIDStrict("22222222-2222-2222-2222-222222222222")

	in := struct {
		comment, activity cursorPos
	}{
		comment:  cursorPos{T: pgtype.Timestamptz{Time: cT, Valid: true}, ID: cID},
		activity: cursorPos{T: pgtype.Timestamptz{Time: aT, Valid: true}, ID: aID},
	}

	encoded := encodeTimelineCursor(in.comment, in.activity)
	gotC, gotA, err := decodeTimelineCursor(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !gotC.T.Time.Equal(cT) || !gotA.T.Time.Equal(aT) {
		t.Fatalf("timestamps did not round-trip: comment=%s activity=%s", gotC.T.Time, gotA.T.Time)
	}
	if uuidToString(gotC.ID) != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("comment id did not round-trip: %s", uuidToString(gotC.ID))
	}
	if uuidToString(gotA.ID) != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("activity id did not round-trip: %s", uuidToString(gotA.ID))
	}

	// Garbage cursor → error, never panics.
	if _, _, err := decodeTimelineCursor("not-base64"); err == nil {
		t.Fatalf("expected decode error for garbage input")
	}
}

// TestCommentOverflow pins the over-fetch / trim contract that gates "Show
// older". Callers query the SQL with limit+1 and pass the raw rows in; the
// helper trims to <limit> and reports hasMore. The boundary the user flagged
// — exactly <limit> comments exist — must report hasMore=false so no
// affordance appears for content that doesn't exist.
func TestCommentOverflow(t *testing.T) {
	mk := func(n int) []db.Comment {
		out := make([]db.Comment, n)
		return out
	}
	cases := []struct {
		name        string
		fetched     int // rows the SQL returned (caller asked for limit+1)
		limit       int
		wantTrimmed int
		wantMore    bool
	}{
		{"empty page", 0, 30, 0, false},
		{"partial page", 5, 30, 5, false},
		// Issue has exactly limit comments — caller asked for limit+1 and got
		// only limit back. No older content; "Show older" must NOT appear.
		{"exactly limit comments", 30, 30, 30, false},
		// Issue has more than limit — caller asked for limit+1 and got
		// limit+1 back. Trim the probe row, set hasMore=true.
		{"one over limit", 31, 30, 30, true},
		{"well over limit", 100, 30, 30, true},
		{"limit zero rejects", 100, 0, 100, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, more := commentOverflow(mk(tc.fetched), tc.limit)
			if len(rows) != tc.wantTrimmed {
				t.Fatalf("trimmed length: got %d, want %d", len(rows), tc.wantTrimmed)
			}
			if more != tc.wantMore {
				t.Fatalf("hasMore: got %v, want %v", more, tc.wantMore)
			}
		})
	}
}

// TestListTimeline_PerPoolCursorWalksAllComments reproduces the GPT-Boy
// blocker on PR #2253: when activities sit older than every fetched comment,
// a single shared cursor anchored on the merged-page boundary points at the
// oldest activity, and the next "show older" call's `ListCommentsBefore` hits
// activity time → skips every unreturned comment in between. The dual-pool
// cursor walks each pool independently so the full comment list stays
// reachable.
func TestListTimeline_PerPoolCursorWalksAllComments(t *testing.T) {
	issueID := createIssueForTimeline(t, "GPT-Boy per-pool cursor regression")
	ctx := context.Background()

	// Seed 30 activities in the older block, then 80 comments strictly newer
	// than every activity. seedTimelineEntries inserts comments first then
	// activities, which is the wrong order for this scenario, so seed manually.
	const activityN, commentN = 30, 80
	base := time.Now().UTC().Add(-time.Duration(activityN+commentN) * time.Minute)

	for i := 0; i < activityN; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		if _, err := testPool.Exec(ctx, `
			INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
			VALUES ($1, $2, 'member', $3, 'status_changed', '{"from":"todo","to":"in_progress"}'::jsonb, $4)
		`, testWorkspaceID, issueID, testUserID, ts); err != nil {
			t.Fatalf("seed activity %d: %v", i, err)
		}
	}
	commentIDs := make([]string, 0, commentN)
	for i := 0; i < commentN; i++ {
		var id string
		ts := base.Add(time.Duration(activityN+i) * time.Minute)
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $5)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, fmt.Sprintf("comment %d", i), ts).Scan(&id); err != nil {
			t.Fatalf("seed comment %d: %v", i, err)
		}
		commentIDs = append(commentIDs, id)
	}

	// Walk older pages until exhausted, collecting every comment id seen.
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ { // safety bound — true exit is has_more_before=false
		query := "limit=30"
		if cursor != "" {
			query += "&before=" + cursor
		}
		resp, code := fetchTimeline(t, issueID, query)
		if code != http.StatusOK {
			t.Fatalf("page %d: expected 200, got %d", page, code)
		}
		for _, e := range resp.Entries {
			if e.Type == "comment" {
				seen[e.ID] = true
			}
		}
		if !resp.HasMoreBefore {
			break
		}
		if resp.NextCursor == nil {
			t.Fatalf("page %d: has_more_before=true but next_cursor missing", page)
		}
		cursor = *resp.NextCursor
	}

	// All 80 seeded comments must be reachable through the cursor walk —
	// pre-fix, the 50 unreturned comments after page 1 stayed hidden because
	// the shared cursor skipped past them via the activity timestamp.
	if len(seen) != commentN {
		missing := []string{}
		for _, id := range commentIDs {
			if !seen[id] {
				missing = append(missing, id)
			}
		}
		t.Fatalf("expected to see all %d comments via cursor walk, saw %d. Missing: %v",
			commentN, len(seen), missing)
	}
}

// TestListTimeline_ExactlyLimitCommentsHidesShowOlder pins the boundary the
// user flagged: an issue with exactly <limit> comments must NOT report
// has_more_before. Pre-fix the gate was `len(comments) >= limit`, which
// returned true and rendered a "Show older" button that revealed nothing —
// older clicks fetched zero rows. The over-fetch + trim probe makes the
// boundary exact.
func TestListTimeline_ExactlyLimitCommentsHidesShowOlder(t *testing.T) {
	issueID := createIssueForTimeline(t, "exactly limit comments boundary")
	seedTimelineEntries(t, issueID, 30, 0)

	resp, code := fetchTimeline(t, issueID, "limit=30")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(resp.Entries) != 30 {
		t.Fatalf("expected 30 entries on first page, got %d", len(resp.Entries))
	}
	if resp.HasMoreBefore {
		t.Fatalf("has_more_before must be false when comments == limit (issue has nothing older)")
	}
	if resp.NextCursor != nil {
		t.Fatalf("next_cursor must be nil when has_more_before is false, got %q", *resp.NextCursor)
	}
}

// TestListTimeline_DenseActivityDoesNotHideComments reproduces #1857: an issue
// with sparse comments but dense activity (status flips, agent runs) used to
// trigger has_more_before because activities consumed the same page budget.
// Real comments would get pushed off the visible page and users would think
// the discussion had vanished. Post-fix, has_more_before is gated on comments
// alone, so the entire conversation stays visible without "show older".
func TestListTimeline_DenseActivityDoesNotHideComments(t *testing.T) {
	issueID := createIssueForTimeline(t, "1857 sparse comments dense activity")

	// 10 comments — well under the 30-comment page budget — paired with 60
	// activities (an agent that flipped status / completed runs many times).
	// seedTimelineEntries inserts comments first (older block), then activities
	// (newer block), matching the typical "issue created → discussion → many
	// agent runs" timeline shape.
	commentIDs, _ := seedTimelineEntries(t, issueID, 10, 60)

	resp, code := fetchTimeline(t, issueID, "limit=30")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if resp.HasMoreBefore {
		t.Fatalf("has_more_before must be false when comments < limit, even if activities are dense (#1857)")
	}

	// Every seeded comment must be on the first page — none should be hidden
	// behind a "show older" gate on an issue with so few comments.
	commentSeen := map[string]bool{}
	for _, e := range resp.Entries {
		if e.Type == "comment" {
			commentSeen[e.ID] = true
		}
	}
	for _, id := range commentIDs {
		if !commentSeen[id] {
			t.Fatalf("comment %s missing from latest page — #1857 regressed", id)
		}
	}
}
