package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cursorQuery builds a properly URL-encoded query string for the recent +
// thread-cursor path. RFC3339Nano timestamps contain `:` and may contain `+`,
// both of which need escaping so they survive `(*url.URL).Query()` parsing on
// the server side.
//
// `before` and `beforeID` here name a *thread* (last_activity_at, root_id),
// not a single row — the recent path is thread-grouped (#2340).
func cursorQuery(recent int, before, beforeID string) string {
	v := url.Values{}
	if recent > 0 {
		v.Set("recent", strconv.Itoa(recent))
	}
	if before != "" {
		v.Set("before", before)
	}
	if beforeID != "" {
		v.Set("before_id", beforeID)
	}
	return v.Encode()
}

// nextThreadCursor reads the (before, before-id) headers the recent path
// emits when there is likely an older page to scroll to. Empty pair means
// the server signalled "no more threads".
func nextThreadCursor(w *httptest.ResponseRecorder) (string, string) {
	return w.Header().Get("X-Multica-Next-Before"), w.Header().Get("X-Multica-Next-Before-Id")
}

// commentListFixture seeds an issue with a known comment graph for the
// thread / recent / cursor tests. The shape:
//
//	root1 (oldest)
//	├── r1a
//	└── r1b
//	    └── r1b1   (nested reply — defends Elon's point 2: recursive root walk)
//	root2 (newer, separate thread)
//	├── r2a
//	└── r2b (newest overall)
//
// Each comment is inserted with an explicit created_at so ordering and
// cursor behavior are deterministic.
type commentListFixture struct {
	IssueID string
	Root1   string
	R1a     string
	R1b     string
	R1b1    string
	Root2   string
	R2a     string
	R2b     string
	Base    time.Time
}

func newCommentListFixture(t *testing.T) commentListFixture {
	t.Helper()
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "comment list fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	base := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)

	insert := func(parent *string, offset time.Duration, body string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $6)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, parent, base.Add(offset)).Scan(&id); err != nil {
			t.Fatalf("insert comment %q: %v", body, err)
		}
		return id
	}

	root1 := insert(nil, 0, "root1")
	r1a := insert(&root1, 1*time.Minute, "r1a")
	r1b := insert(&root1, 2*time.Minute, "r1b")
	r1b1 := insert(&r1b, 3*time.Minute, "r1b1") // nested reply: parent is a reply, not a root
	root2 := insert(nil, 10*time.Minute, "root2")
	r2a := insert(&root2, 11*time.Minute, "r2a")
	r2b := insert(&root2, 12*time.Minute, "r2b")

	return commentListFixture{
		IssueID: issueID,
		Root1:   root1, R1a: r1a, R1b: r1b, R1b1: r1b1,
		Root2: root2, R2a: r2a, R2b: r2b,
		Base: base,
	}
}

func decodeComments(t *testing.T, body []byte) []CommentResponse {
	t.Helper()
	var resp []CommentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode comments: %v", err)
	}
	return resp
}

func listComments(t *testing.T, issueID, query string) (*httptest.ResponseRecorder, []CommentResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	url := "/api/issues/" + issueID + "/comments"
	if query != "" {
		url += "?" + query
	}
	r := newRequest("GET", url, nil)
	r = withURLParam(r, "id", issueID)
	testHandler.ListComments(w, r)
	if w.Code != http.StatusOK {
		return w, nil
	}
	return w, decodeComments(t, w.Body.Bytes())
}

func ids(rows []CommentResponse) []string {
	out := make([]string, len(rows))
	for i, c := range rows {
		out[i] = c.ID
	}
	return out
}

func eqIDs(t *testing.T, got, want []string, ctx string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: ids len got=%d want=%d\ngot=%v\nwant=%v", ctx, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: ids[%d] got=%s want=%s\ngot=%v\nwant=%v", ctx, i, got[i], want[i], got, want)
		}
	}
}

// TestListComments_DefaultPreservesChronologicalOrder is a guard against
// silent regressions in the unparameterized list path — agents and the UI
// both depend on chronological order.
func TestListComments_DefaultPreservesChronologicalOrder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	_, rows := listComments(t, fx.IssueID, "")
	want := []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1, fx.Root2, fx.R2a, fx.R2b}
	eqIDs(t, ids(rows), want, "default order")
}

// TestListComments_RootsOnlyReturnsTopLevelComments pins the #3164 contract:
// issue-level orientation should fetch only root comments, leaving replies for
// a later thread-scoped read if the caller needs detail.
func TestListComments_RootsOnlyReturnsTopLevelComments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	for _, tc := range []struct {
		name  string
		query string
	}{
		{name: "underscore query", query: "roots_only=true"},
		{name: "hyphenated alias", query: "roots-only=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, rows := listComments(t, fx.IssueID, tc.query)
			eqIDs(t, ids(rows), []string{fx.Root1, fx.Root2}, tc.name)
			for _, row := range rows {
				if row.ParentID != nil {
					t.Fatalf("%s: expected root comment %s to have nil parent_id, got %q", tc.name, row.ID, *row.ParentID)
				}
			}
			nb, nbid := nextThreadCursor(w)
			if nb != "" || nbid != "" {
				t.Fatalf("%s: roots-only list should not emit cursor headers, got before=%q before_id=%q", tc.name, nb, nbid)
			}
		})
	}
}

// TestListComments_RootsOnlyWithSinceReturnsNewTopLevelComments proves the
// one allowed roots-only combination: `since` narrows the root list without
// pulling newer replies into the response.
func TestListComments_RootsOnlyWithSinceReturnsNewTopLevelComments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("roots_only", "true")
	v.Set("since", fx.Base.Add(5*time.Minute).UTC().Format(time.RFC3339Nano))
	_, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.Root2}, "roots_only + since")
	for _, row := range rows {
		if row.ParentID != nil {
			t.Fatalf("roots_only + since: expected root comment %s to have nil parent_id, got %q", row.ID, *row.ParentID)
		}
	}
}

// TestListComments_RootsOnlyReturnsThreadStats pins the orientation metadata:
// each root returned by roots_only carries reply_count (recursive descendant
// count) and last_activity_at (MAX created_at over the subtree), so an agent
// can triage which thread to open without fetching any replies. The fixture's
// root1 has a nested reply (r1b1 → r1b → root1), proving the count walks deeper
// than one level. Stats are roots-only — the default list must not carry them.
func TestListComments_RootsOnlyReturnsThreadStats(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	_, rows := listComments(t, fx.IssueID, "roots_only=true")
	eqIDs(t, ids(rows), []string{fx.Root1, fx.Root2}, "roots stats order")

	byID := map[string]CommentResponse{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	// root1: r1a, r1b, r1b1 (nested) → 3 replies; newest activity is r1b1 at +3m.
	assertRootStat(t, byID[fx.Root1], "root1", 3, fx.Base.Add(3*time.Minute))
	// root2: r2a, r2b → 2 replies; newest activity is r2b at +12m.
	assertRootStat(t, byID[fx.Root2], "root2", 2, fx.Base.Add(12*time.Minute))

	// The default (non-roots) list must NOT leak the roots-only stats.
	_, all := listComments(t, fx.IssueID, "")
	for _, r := range all {
		if r.ReplyCount != nil || r.LastActivityAt != nil {
			t.Fatalf("default list leaked roots-only stats on %s (reply_count=%v last_activity=%v)", r.ID, r.ReplyCount, r.LastActivityAt)
		}
	}
}

func assertRootStat(t *testing.T, c CommentResponse, label string, wantReplies int, wantLastActivity time.Time) {
	t.Helper()
	if c.ReplyCount == nil {
		t.Fatalf("%s: reply_count missing", label)
	}
	if *c.ReplyCount != wantReplies {
		t.Fatalf("%s: reply_count got=%d want=%d", label, *c.ReplyCount, wantReplies)
	}
	if c.LastActivityAt == nil {
		t.Fatalf("%s: last_activity_at missing", label)
	}
	got, err := time.Parse(time.RFC3339, *c.LastActivityAt)
	if err != nil {
		t.Fatalf("%s: last_activity_at parse %q: %v", label, *c.LastActivityAt, err)
	}
	if !got.Equal(wantLastActivity) {
		t.Fatalf("%s: last_activity_at got=%s want=%s", label, got.UTC(), wantLastActivity.UTC())
	}
}

// TestListComments_SummaryClipsContent pins the summary projection: with
// summary=true a long body is clipped to summaryContentRunes (+ellipsis) and
// flagged content_truncated=true, a short body is returned verbatim with
// content_truncated=false, and without summary the field is omitted entirely so
// the default response shape is unchanged.
func TestListComments_SummaryClipsContent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "summary fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	insert := func(body string, offset time.Duration) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, base.Add(offset)).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		return id
	}
	longID := insert(strings.Repeat("x", 500), 0)
	shortID := insert("short", time.Minute)
	// Multi-byte body: clipping must land on a rune boundary, never mid-rune.
	cjkID := insert(strings.Repeat("你", 300), 2*time.Minute)

	// Baseline: no summary → full content, content_truncated omitted.
	_, full := listComments(t, issueID, "")
	for _, c := range full {
		if c.ContentTruncated != nil {
			t.Fatalf("baseline: content_truncated should be nil, got %v on %s", *c.ContentTruncated, c.ID)
		}
		if c.ID == longID && utf8.RuneCountInString(c.Content) != 500 {
			t.Fatalf("baseline: long content len got=%d want=500", utf8.RuneCountInString(c.Content))
		}
	}

	// summary=true → long clipped + truncated true; short verbatim + false.
	_, sum := listComments(t, issueID, "summary=true")
	byID := map[string]CommentResponse{}
	for _, c := range sum {
		byID[c.ID] = c
	}
	long := byID[longID]
	if long.ContentTruncated == nil || !*long.ContentTruncated {
		t.Fatalf("summary: long comment should be truncated, got %v", long.ContentTruncated)
	}
	if rc := utf8.RuneCountInString(long.Content); rc != summaryContentRunes+1 { // +1 for the ellipsis
		t.Fatalf("summary: long content rune count got=%d want=%d", rc, summaryContentRunes+1)
	}
	if !strings.HasSuffix(long.Content, "…") {
		t.Fatalf("summary: long content should end with ellipsis, got %q", long.Content)
	}
	short := byID[shortID]
	if short.ContentTruncated == nil || *short.ContentTruncated {
		t.Fatalf("summary: short comment should be untruncated (false), got %v", short.ContentTruncated)
	}
	if short.Content != "short" {
		t.Fatalf("summary: short content got=%q want=short", short.Content)
	}
	cjk := byID[cjkID]
	if cjk.ContentTruncated == nil || !*cjk.ContentTruncated {
		t.Fatalf("summary: cjk comment should be truncated, got %v", cjk.ContentTruncated)
	}
	if !utf8.ValidString(cjk.Content) {
		t.Fatalf("summary: cjk content was clipped mid-rune (invalid UTF-8): %q", cjk.Content)
	}
	if rc := utf8.RuneCountInString(cjk.Content); rc != summaryContentRunes+1 { // 200 CJK runes + ellipsis
		t.Fatalf("summary: cjk content rune count got=%d want=%d", rc, summaryContentRunes+1)
	}
	if !strings.HasSuffix(cjk.Content, "你…") {
		t.Fatalf("summary: cjk content should be whole 你-runes then ellipsis, got %q", cjk.Content)
	}
}

// TestListComments_RootsOnlySummaryComposes pins the spec's headline
// "table of contents" read: --roots-only --summary together must (a) clip each
// root's content and flag content_truncated, AND (b) still carry the roots-only
// orientation stats (reply_count over the full subtree, last_activity_at). The
// summary projection composes with roots_only rather than replacing its stats;
// a future refactor that moved the clip into a per-mode branch would silently
// break this composition, so both halves are asserted on the same response.
func TestListComments_RootsOnlySummaryComposes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "roots+summary fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	insert := func(parent *string, body string, offset time.Duration) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $6)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, parent, base.Add(offset)).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		return id
	}
	rootID := insert(nil, strings.Repeat("x", 500), 0)
	insert(&rootID, "a reply", 5*time.Minute) // pushes last_activity past the root's own time

	_, rows := listComments(t, issueID, "roots_only=true&summary=true")
	eqIDs(t, ids(rows), []string{rootID}, "roots_only+summary returns only the root")

	root := rows[0]
	// Summary half: the long root content is clipped + flagged.
	if root.ContentTruncated == nil || !*root.ContentTruncated {
		t.Fatalf("roots+summary: root should be truncated, got %v", root.ContentTruncated)
	}
	if rc := utf8.RuneCountInString(root.Content); rc != summaryContentRunes+1 { // +1 for the ellipsis
		t.Fatalf("roots+summary: clipped content rune count got=%d want=%d", rc, summaryContentRunes+1)
	}
	// Stats half: orientation metadata survives the summary projection. The
	// reply (1 descendant, +5m) drives both reply_count and last_activity_at.
	assertRootStat(t, root, "root", 1, base.Add(5*time.Minute))
}

// TestListComments_ThreadResolvesFromAnyAnchor proves Elon's point 2:
// regardless of whether the anchor is a root, a direct reply, or a nested
// reply (parent_id points at another reply), the server walks up to the
// thread root and returns root + every descendant.
func TestListComments_ThreadResolvesFromAnyAnchor(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	wantThread1 := []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1}

	t.Run("anchor is root", func(t *testing.T) {
		_, rows := listComments(t, fx.IssueID, "thread="+fx.Root1)
		eqIDs(t, ids(rows), wantThread1, "anchor=root1")
	})

	t.Run("anchor is direct reply", func(t *testing.T) {
		_, rows := listComments(t, fx.IssueID, "thread="+fx.R1a)
		eqIDs(t, ids(rows), wantThread1, "anchor=r1a (direct reply)")
	})

	t.Run("anchor is nested reply", func(t *testing.T) {
		// r1b1.parent_id = r1b, which itself is a reply. The recursive CTE
		// must climb root1 → r1b → r1b1 to resolve the root.
		_, rows := listComments(t, fx.IssueID, "thread="+fx.R1b1)
		eqIDs(t, ids(rows), wantThread1, "anchor=r1b1 (nested reply)")
	})

	t.Run("anchor in other thread returns only that thread", func(t *testing.T) {
		_, rows := listComments(t, fx.IssueID, "thread="+fx.R2a)
		eqIDs(t, ids(rows), []string{fx.Root2, fx.R2a, fx.R2b}, "anchor=r2a")
	})
}

// TestListComments_ThreadAnchorErrors covers the user-facing error surface
// for the thread path. The unknown-anchor case is what catches the typical
// "agent pasted a stale UUID" footgun — the server returns 404 instead of
// silently returning an empty list (which would otherwise be
// indistinguishable from a deleted thread).
func TestListComments_ThreadAnchorErrors(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("non-uuid thread returns 400", func(t *testing.T) {
		w, _ := listComments(t, fx.IssueID, "thread=not-a-uuid")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unknown thread anchor returns 404", func(t *testing.T) {
		w, _ := listComments(t, fx.IssueID, "thread=00000000-0000-0000-0000-000000000001")
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// TestListComments_RecentReturnsMostRecentlyActiveThreads pins the
// thread-grouped semantics from #2340. Row-based "newest N comments" would
// have surfaced [root2, r2a, r2b] for N=3 — a single thread's tail. The
// thread-grouped path treats the unit as a thread (root + descendants) and
// ranks threads by MAX(created_at) over the subtree, so:
//
//   - recent=1 → the single freshest-active thread (root2 thread) fully
//     expanded, oldest-active thread suppressed.
//   - recent=2 → both threads, with the older-active thread first so the
//     freshest sits at the prompt tail (closest to "now").
func TestListComments_RecentReturnsMostRecentlyActiveThreads(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("recent=1 returns the freshest-active thread fully", func(t *testing.T) {
		_, rows := listComments(t, fx.IssueID, "recent=1")
		eqIDs(t, ids(rows), []string{fx.Root2, fx.R2a, fx.R2b}, "recent=1")
	})

	t.Run("recent=2 returns both threads, older-active first", func(t *testing.T) {
		_, rows := listComments(t, fx.IssueID, "recent=2")
		// Threads sorted by (last_activity_at ASC, root_id ASC):
		//   root1 thread (last_activity = base + 3m via r1b1) FIRST
		//   root2 thread (last_activity = base + 12m via r2b) SECOND
		// In-thread ordering is chronological.
		want := []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1, fx.Root2, fx.R2a, fx.R2b}
		eqIDs(t, ids(rows), want, "recent=2")
	})
}

// TestListComments_RecentRanksStaleThreadAheadIfRecentlyReplied makes the
// MAX(created_at) ranking explicit: a thread whose root is old but which has
// a fresh reply must outrank a thread whose root is newer but quiet. Without
// this, "recent" decays into "most recent root" and misses the very signal
// that thread-grouping was meant to surface.
func TestListComments_RecentRanksStaleThreadAheadIfRecentlyReplied(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3) RETURNING id
	`, testWorkspaceID, testUserID, "stale-but-fresh fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	base := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	insert := func(parent *string, offset time.Duration, body string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $6) RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, parent, base.Add(offset)).Scan(&id); err != nil {
			t.Fatalf("insert: %v", err)
		}
		return id
	}

	// Stale-then-fresh: oldRoot was created at t=0 but received a reply at
	// t=30m. quietRoot was created at t=15m and never replied.
	oldRoot := insert(nil, 0, "oldRoot")
	quietRoot := insert(nil, 15*time.Minute, "quietRoot")
	freshReply := insert(&oldRoot, 30*time.Minute, "freshReply")

	_, rows := listComments(t, issueID, "recent=1")
	// Expected: only the oldRoot thread (oldRoot + freshReply). The
	// quietRoot thread is suppressed because its last_activity_at is older
	// than oldRoot's, even though its root was created later.
	eqIDs(t, ids(rows), []string{oldRoot, freshReply}, "recent=1 picks freshly replied stale thread")
	_ = quietRoot
}

// TestListComments_RecentEmitsThreadCursorWhenPageFull pins the header
// contract: a full page (threads in response == --recent N) emits the
// next-page cursor; an underfilled page emits nothing. The cursor points at
// the OLDEST thread in the page — that is the upper bound for the next
// (older) page.
func TestListComments_RecentEmitsThreadCursorWhenPageFull(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("underfilled page emits no cursor", func(t *testing.T) {
		// recent=5 with only 2 threads available — server has nothing older
		// to offer. No cursor headers means the client stops paginating.
		w, _ := listComments(t, fx.IssueID, "recent=5")
		nb, nbid := nextThreadCursor(w)
		if nb != "" || nbid != "" {
			t.Fatalf("expected no cursor, got before=%q before_id=%q", nb, nbid)
		}
	})

	t.Run("full page emits cursor pointing at oldest thread in page", func(t *testing.T) {
		// recent=1: page is full (1 thread returned, 1 requested). Cursor
		// must point at the (last_activity_at, root_id) of the only thread
		// in the page so the next request can fetch older threads.
		w, _ := listComments(t, fx.IssueID, "recent=1")
		nb, nbid := nextThreadCursor(w)
		if nbid != fx.Root2 {
			t.Fatalf("cursor before_id = %q, want %q (root2 — newest thread)", nbid, fx.Root2)
		}
		if nb == "" {
			t.Fatalf("cursor before is empty; expected RFC3339Nano timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, nb); err != nil {
			t.Fatalf("cursor before = %q is not RFC3339Nano: %v", nb, err)
		}
	})
}

// TestListComments_RecentWithThreadCursorScrollsOlderThreads walks the issue
// thread-by-thread using the cursor the server emits. Pinning this avoids
// the row-based regression where a "newest N comments" cursor would interleave
// rows from multiple threads and skip thread membership across pages.
func TestListComments_RecentWithThreadCursorScrollsOlderThreads(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	// Page 1: newest thread = root2.
	w1, page1 := listComments(t, fx.IssueID, "recent=1")
	eqIDs(t, ids(page1), []string{fx.Root2, fx.R2a, fx.R2b}, "page1 = root2 thread")
	nb, nbid := nextThreadCursor(w1)
	if nb == "" || nbid != fx.Root2 {
		t.Fatalf("page1 cursor = (%q, %q), want (non-empty, %q)", nb, nbid, fx.Root2)
	}

	// Page 2: cursor points at root2 → server returns the next older thread
	// (root1). All of root1's descendants come along.
	w2, page2 := listComments(t, fx.IssueID, cursorQuery(1, nb, nbid))
	eqIDs(t, ids(page2), []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1}, "page2 = root1 thread")

	// Page 3: cursor points at root1 → no older threads exist, page is
	// empty AND no cursor is emitted.
	nb2, nbid2 := nextThreadCursor(w2)
	if nb2 == "" || nbid2 != fx.Root1 {
		t.Fatalf("page2 cursor = (%q, %q), want (non-empty, %q)", nb2, nbid2, fx.Root1)
	}
	w3, page3 := listComments(t, fx.IssueID, cursorQuery(1, nb2, nbid2))
	if len(page3) != 0 {
		t.Fatalf("page3: expected empty (no older threads), got %d rows: %v", len(page3), ids(page3))
	}
	nb3, nbid3 := nextThreadCursor(w3)
	if nb3 != "" || nbid3 != "" {
		t.Fatalf("page3 cursor = (%q, %q), want both empty (end-of-list)", nb3, nbid3)
	}
}

// TestListComments_ThreadCursorStableUnderSameLastActivity locks the
// tie-break invariant for the thread cursor. Three threads with identical
// last_activity_at must paginate one-at-a-time without skips or duplicates,
// because (last_activity_at, root_id) — not just last_activity_at — is the
// total order. A timestamp-only cursor would either drop one thread or
// surface the same thread twice when ties land in the same microsecond.
func TestListComments_ThreadCursorStableUnderSameLastActivity(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3) RETURNING id
	`, testWorkspaceID, testUserID, "thread tie-break fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	ts := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Millisecond)
	insertRoot := func(body string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5) RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, ts).Scan(&id); err != nil {
			t.Fatalf("insert: %v", err)
		}
		return id
	}
	a := insertRoot("a")
	b := insertRoot("b")
	c := insertRoot("c")

	// All three threads have last_activity_at = ts (each root is also the
	// thread's only comment). Order is (ts, root_id) — UUID lex tie-break.
	// Build canonical order by sorting the root ids and reversing (the SQL
	// orders DESC for selection).
	sorted := []string{a, b, c}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	// Newest-first selection walks UUIDs DESC, so the page-1 thread is the
	// largest UUID; response is then ordered ASC (oldest-active first =
	// smallest-UUID-among-current-page) — with recent=1 there's only one
	// thread per page so the body shows that thread alone.
	wantOrder := []string{sorted[2], sorted[1], sorted[0]}

	var got []string
	w, page := listComments(t, issueID, "recent=1")
	if len(page) != 1 {
		t.Fatalf("page1: expected 1 thread (1 row), got %d", len(page))
	}
	got = append(got, page[0].ID)

	for i := 0; i < 2; i++ {
		nb, nbid := nextThreadCursor(w)
		if nb == "" || nbid == "" {
			t.Fatalf("page %d: missing cursor headers", i+1)
		}
		w, page = listComments(t, issueID, cursorQuery(1, nb, nbid))
		if len(page) != 1 {
			t.Fatalf("page %d: expected 1 thread (1 row), got %d", i+2, len(page))
		}
		got = append(got, page[0].ID)
	}

	eqIDs(t, got, wantOrder, "paginated walk")
}

// TestListComments_FlagCombinationRules locks Elon's point 4. The matrix is
// tiny on purpose — the goal is to ensure conflicting flags are rejected
// loudly at the API surface so the CLI's local validation cannot drift.
func TestListComments_FlagCombinationRules(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	cases := []struct {
		name   string
		query  string
		status int
	}{
		{
			name:   "thread + recent rejected",
			query:  "thread=" + fx.Root1 + "&recent=5",
			status: http.StatusBadRequest,
		},
		{
			name: "thread + before rejected",
			query: (func() string {
				v := url.Values{}
				v.Set("thread", fx.Root1)
				v.Set("before", time.Now().UTC().Format(time.RFC3339))
				v.Set("before_id", uuid.NewString())
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
		{
			name: "before without before_id rejected",
			query: (func() string {
				v := url.Values{}
				v.Set("recent", "5")
				v.Set("before", time.Now().UTC().Format(time.RFC3339))
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
		{
			name: "before_id without before rejected",
			query: (func() string {
				v := url.Values{}
				v.Set("recent", "5")
				v.Set("before_id", uuid.NewString())
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
		{
			name: "before + before_id without recent rejected",
			// Cursor without --recent used to fall through to the default /
			// since path and silently return the full timeline (the gap Elon
			// called out in the PR #2787 second review). The 400 here pins
			// the documented "cursor scrolls within a recent window" rule.
			query: (func() string {
				v := url.Values{}
				v.Set("before", time.Now().UTC().Format(time.RFC3339))
				v.Set("before_id", uuid.NewString())
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
		{
			name:   "zero recent rejected",
			query:  "recent=0",
			status: http.StatusBadRequest,
		},
		{
			name:   "negative recent rejected",
			query:  "recent=-3",
			status: http.StatusBadRequest,
		},
		{
			name:   "non-numeric recent rejected",
			query:  "recent=lots",
			status: http.StatusBadRequest,
		},
		{
			name:   "non-boolean roots_only rejected",
			query:  "roots_only=yes",
			status: http.StatusBadRequest,
		},
		{
			name:   "non-boolean summary rejected",
			query:  "summary=yes",
			status: http.StatusBadRequest,
		},
		{
			name:   "roots_only + thread rejected",
			query:  "roots_only=true&thread=" + fx.Root1,
			status: http.StatusBadRequest,
		},
		{
			name:   "roots_only + recent rejected",
			query:  "roots_only=true&recent=1",
			status: http.StatusBadRequest,
		},
		{
			name:   "roots_only + tail rejected",
			query:  "roots_only=true&tail=1",
			status: http.StatusBadRequest,
		},
		{
			name: "roots_only + cursor rejected",
			query: (func() string {
				v := url.Values{}
				v.Set("roots_only", "true")
				v.Set("before", time.Now().UTC().Format(time.RFC3339))
				v.Set("before_id", uuid.NewString())
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := listComments(t, fx.IssueID, tc.query)
			if w.Code != tc.status {
				t.Fatalf("query=%q\n  got=%d want=%d body=%s", tc.query, w.Code, tc.status, w.Body.String())
			}
		})
	}
}

// TestListComments_RecentWithSinceFilteredEmptySuppressesCursor pins
// Elon's nit on PR #2787 / MUL-2340: a `recent + since` page whose every
// row gets dropped by the `since` filter must NOT emit a next-page cursor.
//
// Pagination walks threads in strictly decreasing last_activity_at. If the
// `since` filter empties this page, every comment in this page is <= since
// ⇒ head.last_activity_at <= since ⇒ every older thread (strictly less
// recent than head) also has last_activity_at < since ⇒ guaranteed empty.
// Emitting a cursor in that case would invite the caller into a wasted
// walk of pages that can never produce a row.
func TestListComments_RecentWithSinceFilteredEmptySuppressesCursor(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	// `since` = base + 1h (i.e. AFTER every comment in the fixture, where the
	// freshest row sits at base + 12m). With recent=1 the page is technically
	// "full" (1 thread out of 1 requested) so the legacy `len(seen) >= N`
	// check would emit a cursor — but every row in that page is <= since, so
	// the body is empty AND no older page can ever yield a >since row.
	v := url.Values{}
	v.Set("recent", "1")
	v.Set("since", fx.Base.Add(1*time.Hour).UTC().Format(time.RFC3339Nano))
	w, rows := listComments(t, fx.IssueID, v.Encode())
	if len(rows) != 0 {
		t.Fatalf("expected empty page after since-filter, got %d rows: %v", len(rows), ids(rows))
	}
	nb, nbid := nextThreadCursor(w)
	if nb != "" || nbid != "" {
		t.Fatalf("recent+since empty page must NOT emit cursor; got before=%q before_id=%q (this walks the caller into a guaranteed-empty pagination loop)", nb, nbid)
	}
}

// TestListComments_RecentWithSinceKeepsCursorWhenPageHasRows is the
// counterpoint to TestListComments_RecentWithSinceFilteredEmptySuppressesCursor:
// if the `since` filter leaves at least one row in the page, the cursor must
// still be emitted (the suppression rule is narrowly scoped to the empty
// case so it can't accidentally swallow legitimate next-page hints).
func TestListComments_RecentWithSinceKeepsCursorWhenPageHasRows(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	// since = base + 11m30s drops everything from root1 (last_activity = base+3m)
	// and from root2 except r2b (base+12m). With recent=1 (the freshest-active
	// thread, root2), the page keeps r2b and the cursor still points at root2
	// so the caller can scroll older threads if they want.
	v := url.Values{}
	v.Set("recent", "1")
	v.Set("since", fx.Base.Add(11*time.Minute+30*time.Second).UTC().Format(time.RFC3339Nano))
	w, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.R2b}, "recent=1 + since keeps r2b")
	nb, nbid := nextThreadCursor(w)
	if nb == "" || nbid != fx.Root2 {
		t.Fatalf("non-empty recent+since page must keep cursor; got before=%q before_id=%q want root_id=%q", nb, nbid, fx.Root2)
	}
}

// TestListComments_RecentWithSinceSuppressesCursorWhenHeadPastSince covers
// the mixed case from Elon's #2787 second review: the page is full (so the
// legacy `len(seen) >= N` check would emit a cursor) AND the `since` filter
// keeps rows from fresher threads (so the legacy `len(comments) == 0`
// suppression does NOT trip), but the head (oldest-active) thread already
// sits at or before `since`. Older pages walk strictly less-recent threads,
// so none of them can produce a `> since` comment — emitting a cursor here
// would still drag the caller into a guaranteed-empty next-page fetch.
//
// Fixture: root1 last_activity = base+3m, root2 last_activity = base+12m.
// recent=2 ⇒ both threads on the page, head = root1. since = base+5m drops
// everything in root1, keeps root2 entirely. Expect body = root2 thread,
// no cursor.
func TestListComments_RecentWithSinceSuppressesCursorWhenHeadPastSince(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("recent", "2")
	v.Set("since", fx.Base.Add(5*time.Minute).UTC().Format(time.RFC3339Nano))
	w, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.Root2, fx.R2a, fx.R2b}, "recent=2 + since keeps root2 thread")
	nb, nbid := nextThreadCursor(w)
	if nb != "" || nbid != "" {
		t.Fatalf("head thread (root1, last_activity = base+3m) is already <= since (base+5m); older pages can't beat it. cursor must be suppressed, got before=%q before_id=%q", nb, nbid)
	}
}

// TestListComments_ThreadWithSinceFiltersWithinThread proves the allowed
// combination from the rules: `thread + since` returns only comments in
// that thread newer than `since`. The since filter is applied in-memory
// after the thread CTE so the root membership semantics stay intact.
func TestListComments_ThreadWithSinceFiltersWithinThread(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	// since = base+1m30s → drop root1, r1a; keep r1b, r1b1.
	v := url.Values{}
	v.Set("thread", fx.Root1)
	v.Set("since", fx.Base.Add(90*time.Second).UTC().Format(time.RFC3339Nano))
	_, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.R1b, fx.R1b1}, "thread+since")
}

// nextReplyCursor reads the (before, before-id) headers the thread + tail
// path emits when there is likely an older page of replies inside the same
// thread. Same wire shape as the thread-cursor headers — context decides
// which (the caller knows whether they used --recent or --tail).
func nextReplyCursor(w *httptest.ResponseRecorder) (string, string) {
	return w.Header().Get("X-Multica-Next-Before"), w.Header().Get("X-Multica-Next-Before-Id")
}

// TestListComments_ThreadTailReturnsRootPlusNewestReplies pins the core
// MUL-2421 contract: `--thread X --tail N` returns the thread root + the N
// most recent replies in that thread. Body stays chronological, so the
// root sits at the head and the freshest reply at the tail (closest to
// "now" in an agent prompt).
func TestListComments_ThreadTailReturnsRootPlusNewestReplies(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("tail=2 keeps newest 2 replies + root", func(t *testing.T) {
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "2")
		// root1 has r1a, r1b, r1b1 in order. Newest 2 = r1b, r1b1.
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.R1b1}, "tail=2")
	})

	t.Run("tail larger than reply count returns full thread", func(t *testing.T) {
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "99")
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1}, "tail=99 (oversized)")
	})

	t.Run("tail=0 returns root only", func(t *testing.T) {
		// Per the issue: root must never be elided — even tail=0 keeps it,
		// so a reader landing on a long thread still gets the "what is this
		// about" context without dragging any replies into prompt context.
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "0")
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1}, "tail=0 (root only)")
	})

	t.Run("anchor on a nested reply still walks up to the root", func(t *testing.T) {
		// r1b1 is a reply-of-reply (parent_id = r1b, itself a reply).
		// The recursive root walk must climb to root1 regardless. tail
		// applies to *that* thread's replies, not to whichever subtree
		// the anchor happens to be in.
		v := url.Values{}
		v.Set("thread", fx.R1b1)
		v.Set("tail", "1")
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b1}, "tail=1 anchored at nested reply")
	})
}

// TestListComments_ThreadTailEmitsReplyCursorWhenPageFull pins the cursor
// header contract for the thread + tail path. A full page (replyCount ==
// tail) emits a reply cursor pointing at the oldest reply in the page; an
// underfilled page emits nothing so the caller stops paginating.
func TestListComments_ThreadTailEmitsReplyCursorWhenPageFull(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("underfilled page emits no cursor", func(t *testing.T) {
		// tail=5 on a thread with 3 replies (r1a, r1b, r1b1). The SQL
		// returns all 3 — no older page exists.
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "5")
		w, _ := listComments(t, fx.IssueID, v.Encode())
		nb, nbid := nextReplyCursor(w)
		if nb != "" || nbid != "" {
			t.Fatalf("expected no cursor, got before=%q before_id=%q", nb, nbid)
		}
	})

	t.Run("tail=0 emits no cursor (no replies were requested)", func(t *testing.T) {
		// tail=0 is the "I just want the root context" mode. There is no
		// reply page to scroll, so the server must not invite the caller
		// to walk one.
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "0")
		w, _ := listComments(t, fx.IssueID, v.Encode())
		nb, nbid := nextReplyCursor(w)
		if nb != "" || nbid != "" {
			t.Fatalf("tail=0 must not emit cursor, got before=%q before_id=%q", nb, nbid)
		}
	})

	t.Run("full page emits cursor pointing at oldest reply", func(t *testing.T) {
		// tail=2 on root1 ⇒ page = r1b, r1b1. Oldest reply on page = r1b.
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "2")
		w, _ := listComments(t, fx.IssueID, v.Encode())
		nb, nbid := nextReplyCursor(w)
		if nbid != fx.R1b {
			t.Fatalf("cursor before_id = %q, want %q (r1b — oldest reply on page)", nbid, fx.R1b)
		}
		if nb == "" {
			t.Fatalf("cursor before is empty; expected RFC3339Nano timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, nb); err != nil {
			t.Fatalf("cursor before = %q is not RFC3339Nano: %v", nb, err)
		}
	})

	t.Run("exact-boundary page emits no cursor", func(t *testing.T) {
		// root1 has exactly 3 replies (r1a, r1b, r1b1). Requesting --tail 3
		// returns the entire reply set; there is nothing older to scroll
		// to, so no cursor must be emitted. Pre-fix the server used
		// `replyCount >= tail` and falsely sent a cursor here — the next
		// page then returned just the root, wasting a round-trip.
		// (MUL-2421 review fix.)
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "3")
		w, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1}, "tail==replyCount returns full thread")
		nb, nbid := nextReplyCursor(w)
		if nb != "" || nbid != "" {
			t.Fatalf("exact-boundary page must not emit cursor, got before=%q before_id=%q", nb, nbid)
		}
	})
}

// TestListComments_ThreadTailCursorScrollsOlderReplies walks a long thread
// reply-by-reply via the cursor the server emits. This is the analogue of
// TestListComments_RecentWithThreadCursorScrollsOlderThreads but inside
// one thread: cursor returns are reply cursors, not thread cursors, and
// the root must keep showing up on every page.
func TestListComments_ThreadTailCursorScrollsOlderReplies(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	// Page 1: tail=1 on root1 → newest reply = r1b1.
	v := url.Values{}
	v.Set("thread", fx.Root1)
	v.Set("tail", "1")
	w1, page1 := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(page1), []string{fx.Root1, fx.R1b1}, "page1 = root + r1b1")
	nb, nbid := nextReplyCursor(w1)
	if nb == "" || nbid != fx.R1b1 {
		t.Fatalf("page1 cursor = (%q, %q), want (non-empty, %q)", nb, nbid, fx.R1b1)
	}

	// Page 2: cursor points at r1b1 → server returns the next older reply
	// (r1b). Root is re-emitted on every page so the agent never loses the
	// thread context.
	v.Set("before", nb)
	v.Set("before_id", nbid)
	w2, page2 := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(page2), []string{fx.Root1, fx.R1b}, "page2 = root + r1b")
	nb2, nbid2 := nextReplyCursor(w2)
	if nb2 == "" || nbid2 != fx.R1b {
		t.Fatalf("page2 cursor = (%q, %q), want (non-empty, %q)", nb2, nbid2, fx.R1b)
	}

	// Page 3: cursor points at r1b → server returns r1a (the last reply).
	// r1a is the oldest reply in the thread, so the server must NOT emit a
	// cursor — the next page would return just the root, which is a wasted
	// round-trip. (MUL-2421 review: probe `reply_limit + 1` to detect
	// has-more instead of trusting replyCount >= tail.)
	v.Set("before", nb2)
	v.Set("before_id", nbid2)
	w3, page3 := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(page3), []string{fx.Root1, fx.R1a}, "page3 = root + r1a (last reply)")
	nb3, nbid3 := nextReplyCursor(w3)
	if nb3 != "" || nbid3 != "" {
		t.Fatalf("page3 cursor = (%q, %q), want both empty (end-of-thread, no older replies after r1a)", nb3, nbid3)
	}
}

// TestListComments_ThreadTailWithSinceFiltersAfterTail proves the documented
// combination: `--thread + --tail + --since` applies `tail` first (newest N
// replies in the thread) and then drops anything <= since from that page.
// The root is exempt — it is always returned so the reader keeps the thread
// context even when the page would otherwise be empty.
func TestListComments_ThreadTailWithSinceFiltersAfterTail(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	t.Run("since drops older replies, keeps root + fresher", func(t *testing.T) {
		// tail=3 on root1 → all 3 replies (r1a, r1b, r1b1) + root1.
		// since = base + 90s → drops r1a (base+1m); keeps r1b (base+2m),
		// r1b1 (base+3m).
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "3")
		v.Set("since", fx.Base.Add(90*time.Second).UTC().Format(time.RFC3339Nano))
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.R1b1}, "tail=3+since")
	})

	t.Run("since drops every reply but root stays", func(t *testing.T) {
		// since past every comment in the fixture — root is still emitted
		// for context.
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "3")
		v.Set("since", fx.Base.Add(1*time.Hour).UTC().Format(time.RFC3339Nano))
		_, rows := listComments(t, fx.IssueID, v.Encode())
		eqIDs(t, ids(rows), []string{fx.Root1}, "since past everything keeps root")
	})

	t.Run("tail overflow with since past oldest retained reply suppresses cursor", func(t *testing.T) {
		// Recreate Elon's MUL-2421 v2 case: long thread, tail=2 overflows
		// (3 replies exist, only top 2 kept), the page body still retains
		// a fresher reply (r1b1 at base+3m), but the oldest reply on the
		// retained page (r1b at base+2m) is already <= since (base+2m30s).
		// Older replies are strictly older than r1b → strictly older than
		// since → all guaranteed-filtered. Server must NOT emit a reply
		// cursor; otherwise the agent walks root-only pages until the
		// thread bottoms out.
		v := url.Values{}
		v.Set("thread", fx.Root1)
		v.Set("tail", "2")
		v.Set("since", fx.Base.Add(150*time.Second).UTC().Format(time.RFC3339Nano))
		w, rows := listComments(t, fx.IssueID, v.Encode())
		// Sanity-check the body precondition: we kept the fresher reply
		// (r1b1, base+3m) but dropped r1b (base+2m) via since. If this
		// assertion ever shifts, the cursor assertion below would be
		// testing a different shape than the bug Elon described.
		eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b1}, "body keeps root + fresher reply only")
		nb, nbid := nextReplyCursor(w)
		if nb != "" || nbid != "" {
			t.Fatalf("expected no cursor (older page is guaranteed-empty under since), got before=%q before_id=%q", nb, nbid)
		}
	})
}

// TestListComments_ThreadTailFlagCombinationRules locks the API-surface
// rules for the new --tail flag. The matrix is narrow on purpose — the
// only legal combinations are: (thread + tail), (thread + tail + since),
// (thread + tail + before + before_id), and a thread/recent/cursor matrix
// that does NOT include tail.
func TestListComments_ThreadTailFlagCombinationRules(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	cases := []struct {
		name   string
		query  string
		status int
	}{
		{
			// tail is a thread-scoped limit; outside of --thread there is
			// no defined behavior so it must be rejected at the API surface.
			name:   "tail without thread rejected",
			query:  "tail=5",
			status: http.StatusBadRequest,
		},
		{
			// Negative tail would round-trip to LIMIT -N which Postgres
			// flags as a syntax error. Catch it at the boundary.
			name:   "negative tail rejected",
			query:  "thread=" + fx.Root1 + "&tail=-1",
			status: http.StatusBadRequest,
		},
		{
			name:   "non-numeric tail rejected",
			query:  "thread=" + fx.Root1 + "&tail=lots",
			status: http.StatusBadRequest,
		},
		{
			// Cursor without --tail in the thread path used to be rejected
			// outright; now it requires --tail so the cursor's "scroll
			// older replies" meaning has somewhere to land. Without --tail
			// the server returns the whole thread anyway, so the cursor is
			// meaningless.
			name: "thread + before without tail rejected",
			query: (func() string {
				v := url.Values{}
				v.Set("thread", fx.Root1)
				v.Set("before", time.Now().UTC().Format(time.RFC3339))
				v.Set("before_id", uuid.NewString())
				return v.Encode()
			})(),
			status: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := listComments(t, fx.IssueID, tc.query)
			if w.Code != tc.status {
				t.Fatalf("query=%q\n  got=%d want=%d body=%s", tc.query, w.Code, tc.status, w.Body.String())
			}
		})
	}
}

// TestListComments_ThreadTailZeroReplyCountIsAllowed pins tail=0 as a valid
// caller intent (root-only). The split between "did not pass --tail" and
// "passed --tail 0" lives in the handler (ThreadTailSet bool); collapsing
// them into a single int would silently downgrade tail=0 to the full
// thread path.
func TestListComments_ThreadTailZeroReplyCountIsAllowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("thread", fx.Root1)
	v.Set("tail", "0")
	w, rows := listComments(t, fx.IssueID, v.Encode())
	if w.Code != http.StatusOK {
		t.Fatalf("tail=0 should succeed, got %d: %s", w.Code, w.Body.String())
	}
	eqIDs(t, ids(rows), []string{fx.Root1}, "tail=0 returns only root")
}

// TestListComments_ThreadTailNotFoundReturns404 makes the not-found surface
// of the paged path match the legacy thread path. A stale anchor is a
// realistic agent footgun (mention a comment that was later deleted), and
// returning [] would be indistinguishable from "the thread really does
// have no comments".
func TestListComments_ThreadTailNotFoundReturns404(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	v := url.Values{}
	v.Set("thread", "00000000-0000-0000-0000-000000000001")
	v.Set("tail", "5")
	w, _ := listComments(t, fx.IssueID, v.Encode())
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown anchor, got %d: %s", w.Code, w.Body.String())
	}
	_ = fx
}

// resolveCommentRow marks a comment resolved directly in the DB (test helper —
// the public path goes through ResolveComment, but for list-filter tests we
// just need the column set).
func resolveCommentRow(t *testing.T, commentID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE comment SET resolved_at = now(), resolved_by_type = 'member', resolved_by_id = $2 WHERE id = $1`,
		commentID, testUserID,
	); err != nil {
		t.Fatalf("resolve comment row: %v", err)
	}
}

// TestCountNewCommentsSince_IssueWideExcludesAgentOwnAndTrigger pins the
// claim-side count query: it counts comments created after the given anchor
// ACROSS THE WHOLE ISSUE (every thread, not just the triggering one), excludes
// the triggering comment because it is injected into the prompt, and excludes
// the agent's own comments so a chatty agent does not inflate its own
// new-comment count.
func TestCountNewCommentsSince_IssueWideExcludesAgentOwnAndTrigger(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	agentID := createHandlerTestAgent(t, "count-agent", []byte("[]"))

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
		VALUES ($1, $2, 'agent', $3, 'agent reply', 'comment', $4, $5)
	`, fx.IssueID, testWorkspaceID, agentID, fx.Root1, fx.Base.Add(4*time.Minute)); err != nil {
		t.Fatalf("insert agent comment: %v", err)
	}

	ctx := context.Background()

	// Anchor after r1a: root1/r1a are stale. Newer than the anchor are r1b
	// (trigger thread), r1b1 (the trigger itself — excluded), the agent reply
	// (excluded), and root2's whole thread root2/r2a/r2b (unrelated thread, now
	// counted because the count is issue-wide). So r1b + root2 + r2a + r2b = 4.
	got, err := testHandler.Queries.CountNewCommentsSince(ctx, db.CountNewCommentsSinceParams{
		AnchorID:    parseUUID(fx.R1b1),
		IssueID:     parseUUID(fx.IssueID),
		WorkspaceID: parseUUID(testWorkspaceID),
		Since:       pgtype.Timestamptz{Time: fx.Base.Add(90 * time.Second), Valid: true},
		AuthorID:    parseUUID(agentID),
	})
	if err != nil {
		t.Fatalf("count new since anchor: %v", err)
	}
	if got != 4 {
		t.Fatalf("expected issue-wide count of 4 (r1b + root2 + r2a + r2b), got %d", got)
	}

	// Anchor in the future: nothing is newer.
	got, err = testHandler.Queries.CountNewCommentsSince(ctx, db.CountNewCommentsSinceParams{
		AnchorID:    parseUUID(fx.R1b1),
		IssueID:     parseUUID(fx.IssueID),
		WorkspaceID: parseUUID(testWorkspaceID),
		Since:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		AuthorID:    parseUUID(agentID),
	})
	if err != nil {
		t.Fatalf("count new since future: %v", err)
	}
	if got != 0 {
		t.Fatalf("expected 0 new comments after a future anchor, got %d", got)
	}
}

// TestCreateCommentPreservesDirectParent pins the write-boundary invariant:
// parent_id stores the exact comment being replied to. Thread readers discover
// the root separately via recursive queries, so storing a reply-to-reply must
// not destroy the direct-parent signal that trigger logic needs.
func TestCreateCommentPreservesDirectParent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires DB")
	}
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "direct parent fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	create := func(parentID, body string) CommentResponse {
		t.Helper()
		payload := map[string]any{"content": body}
		if parentID != "" {
			payload["parent_id"] = parentID
		}
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/comments", payload)
		req = withURLParam(req, "id", issueID)
		testHandler.CreateComment(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(%q): expected 201, got %d: %s", body, w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode created comment %q: %v", body, err)
		}
		return resp
	}

	root := create("", "root")
	if root.ParentID != nil {
		t.Fatalf("root comment must have nil parent_id, got %v", *root.ParentID)
	}

	// A direct reply to the root stays parented at the root.
	reply := create(root.ID, "reply")
	if reply.ParentID == nil || *reply.ParentID != root.ID {
		t.Fatalf("direct reply parent_id: want root %s, got %v", root.ID, reply.ParentID)
	}

	// A reply whose parent is itself a reply must keep that direct parent. The
	// thread root is recoverable through recursive thread reads; the direct
	// parent is not recoverable once overwritten.
	nested := create(reply.ID, "nested")
	if nested.ParentID == nil || *nested.ParentID != reply.ID {
		t.Fatalf("reply-to-reply parent_id: want direct parent %s, got %v (root was %s)", reply.ID, nested.ParentID, root.ID)
	}
}
