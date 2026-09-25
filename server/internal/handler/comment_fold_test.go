package handler

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// setCommentResolvedAt stamps resolved_at (and a member resolver) on a comment
// directly, so a fold test can choose exactly which comment in a thread is the
// resolution and control the resolved_at ordering. Going through SQL rather
// than the resolve handler lets a test deliberately leave two resolutions in
// one thread to exercise the projection's latest-wins tiebreak — the write-side
// invariant (ClearOtherThreadResolutions) would otherwise collapse them to one.
func setCommentResolvedAt(t *testing.T, id string, at time.Time) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE comment SET resolved_at = $2, resolved_by_type = 'member', resolved_by_id = $3 WHERE id = $1`,
		id, at, testUserID,
	); err != nil {
		t.Fatalf("set resolved_at for %s: %v", id, err)
	}
}

// byID indexes a response slice for per-comment fold-field assertions.
func byID(rows []CommentResponse) map[string]CommentResponse {
	m := make(map[string]CommentResponse, len(rows))
	for _, r := range rows {
		m[r.ID] = r
	}
	return m
}

// assertFolded asserts a comment carries the resolved-thread fold annotation
// with the expected dropped count.
func assertFolded(t *testing.T, c CommentResponse, wantCount int, ctx string) {
	t.Helper()
	if c.ThreadResolved == nil || !*c.ThreadResolved {
		t.Fatalf("%s: expected thread_resolved=true on %s, got %v", ctx, c.ID, c.ThreadResolved)
	}
	if c.FoldedCount == nil {
		t.Fatalf("%s: expected folded_count on %s, got nil", ctx, c.ID)
	}
	if *c.FoldedCount != wantCount {
		t.Fatalf("%s: folded_count on %s got %d want %d", ctx, c.ID, *c.FoldedCount, wantCount)
	}
}

// assertNotFolded asserts a comment carries no fold annotation (every comment
// outside a resolved thread, and every non-root comment).
func assertNotFolded(t *testing.T, c CommentResponse, ctx string) {
	t.Helper()
	if c.ThreadResolved != nil {
		t.Fatalf("%s: expected no thread_resolved on %s, got %v", ctx, c.ID, *c.ThreadResolved)
	}
	if c.FoldedCount != nil {
		t.Fatalf("%s: expected no folded_count on %s, got %v", ctx, c.ID, *c.FoldedCount)
	}
}

// TestListComments_FoldReplyResolvedKeepsRootAndConclusion pins the core
// reply-resolved contract: a thread whose CONCLUSION is a reply collapses to
// root + that reply, dropping every other reply (including ones posted after the
// conclusion — matches the human timeline's foldedReplies filter). The fixture's
// thread1 (root1 → r1a, r1b → r1b1) has its conclusion on r1b; r1a and the
// nested r1b1 fold away. The unresolved thread2 is returned in full.
func TestListComments_FoldReplyResolvedKeepsRootAndConclusion(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.R1b, fx.Base.Add(20*time.Minute))

	_, rows := listComments(t, fx.IssueID, "fold=true")
	// Order is the chronological default with the folded replies removed.
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.Root2, fx.R2a, fx.R2b}, "reply-resolved fold")

	m := byID(rows)
	assertFolded(t, m[fx.Root1], 2, "reply-resolved") // r1a + r1b1 dropped
	assertNotFolded(t, m[fx.R1b], "conclusion reply is not annotated")
	assertNotFolded(t, m[fx.Root2], "unresolved thread root")
	assertNotFolded(t, m[fx.R2a], "unresolved reply")
}

// TestListComments_FoldRootResolvedKeepsRootOnly pins the root-resolved
// contract: resolving the ROOT collapses the whole thread to a single line (the
// root), dropping every reply. thread2's root2 is resolved; r2a and r2b fold
// away. thread1 stays full.
func TestListComments_FoldRootResolvedKeepsRootOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.Root2, fx.Base.Add(20*time.Minute))

	_, rows := listComments(t, fx.IssueID, "fold=true")
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1, fx.Root2}, "root-resolved fold")

	m := byID(rows)
	assertFolded(t, m[fx.Root2], 2, "root-resolved") // r2a + r2b dropped
	for _, id := range []string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1} {
		assertNotFolded(t, m[id], "unresolved thread1 comment")
	}
}

// TestListComments_FoldNoOpWhenNothingResolved guards the cost-saving promise's
// floor: on an issue with no resolutions, fold=true returns the exact same set
// as the default list, with no annotations. Folding only ever removes settled
// discussion; it never changes an active issue's read.
func TestListComments_FoldNoOpWhenNothingResolved(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	_, rows := listComments(t, fx.IssueID, "fold=true")
	eqIDs(t, ids(rows),
		[]string{fx.Root1, fx.R1a, fx.R1b, fx.R1b1, fx.Root2, fx.R2a, fx.R2b},
		"fold no-op")
	for _, r := range rows {
		assertNotFolded(t, r, "no resolutions present")
	}
}

// TestListComments_FoldLatestReplyWins proves the projection is total even if
// the single-resolution-per-thread write invariant is ever violated: with both
// r1a and r1b resolved, the LATEST resolved_at (r1b) is THE conclusion, matching
// deriveThreadResolution. root1 + r1b survive; r1a (the earlier resolution) and
// the nested r1b1 fold away.
func TestListComments_FoldLatestReplyWins(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.R1a, fx.Base.Add(5*time.Minute))
	setCommentResolvedAt(t, fx.R1b, fx.Base.Add(20*time.Minute)) // later → wins

	_, rows := listComments(t, fx.IssueID, "fold=true")
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.Root2, fx.R2a, fx.R2b}, "latest reply wins")
	assertFolded(t, byID(rows)[fx.Root1], 2, "latest-wins fold")
}

// TestListComments_FoldComposesWithRecent confirms fold applies per thread under
// the --recent grouping (the assignment-entry read the prompt steers agents to).
// recent returns both threads; the reply-resolved thread1 folds to root1 + r1b
// while the unresolved thread2 stays full.
func TestListComments_FoldComposesWithRecent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.R1b, fx.Base.Add(20*time.Minute))

	v := url.Values{}
	v.Set("fold", "true")
	v.Set("recent", "10")
	_, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.Root2, fx.R2a, fx.R2b}, "fold + recent")
	assertFolded(t, byID(rows)[fx.Root1], 2, "fold + recent")
}

// TestListComments_FoldComposesWithSummary confirms the two agent-token
// projections stack: fold drops the settled replies, then summary's per-comment
// clip still runs over what remains. The surviving root carries BOTH the fold
// annotation and the summary's content_truncated marker.
func TestListComments_FoldComposesWithSummary(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.R1b, fx.Base.Add(20*time.Minute))

	v := url.Values{}
	v.Set("fold", "true")
	v.Set("summary", "true")
	_, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b, fx.Root2, fx.R2a, fx.R2b}, "fold + summary")

	root1 := byID(rows)[fx.Root1]
	assertFolded(t, root1, 2, "fold + summary")
	if root1.ContentTruncated == nil {
		t.Fatalf("fold + summary: expected content_truncated marker on root1, got nil")
	}
}

// TestListComments_FoldRejectsPartialThreadModes pins the safety boundary: fold
// needs whole threads to compute a resolution, so it is rejected on the
// partial-thread reads (since, tail) and on roots_only (no replies to fold).
// Returning 400 — rather than silently folding a partial thread — is what stops
// the projection from dropping a resolution that was never fetched.
func TestListComments_FoldRejectsPartialThreadModes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)

	since := fx.Base.Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	for _, tc := range []struct {
		name  string
		query func() string
	}{
		{name: "fold + since", query: func() string {
			v := url.Values{}
			v.Set("fold", "true")
			v.Set("since", since)
			return v.Encode()
		}},
		{name: "fold + tail", query: func() string {
			v := url.Values{}
			v.Set("fold", "true")
			v.Set("thread", fx.Root1)
			v.Set("tail", "2")
			return v.Encode()
		}},
		{name: "fold + roots_only", query: func() string {
			v := url.Values{}
			v.Set("fold", "true")
			v.Set("roots_only", "true")
			return v.Encode()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := listComments(t, fx.IssueID, tc.query())
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400, got %d: %s", tc.name, w.Code, w.Body.String())
			}
		})
	}
}

// TestListComments_FoldComposesWithThread confirms fold applies to an untailed
// single-thread read: reading just the reply-resolved thread1 returns root1 +
// conclusion, dropping the intermediate replies.
func TestListComments_FoldComposesWithThread(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newCommentListFixture(t)
	setCommentResolvedAt(t, fx.R1b, fx.Base.Add(20*time.Minute))

	v := url.Values{}
	v.Set("fold", "true")
	v.Set("thread", fx.R1a) // anchor on a reply; server resolves to root1
	_, rows := listComments(t, fx.IssueID, v.Encode())
	eqIDs(t, ids(rows), []string{fx.Root1, fx.R1b}, "fold + thread")
	assertFolded(t, byID(rows)[fx.Root1], 2, "fold + thread")
}
