package handler

import (
	"context"
	"testing"
	"time"
)

// The comment-list endpoint shares ListCommentsForIssue with the timeline, so the
// newest-N window (MUL-5492) can cut a thread in half here too: an old root
// outside the window, a fresh reply inside it.
//
// Two separate properties are at stake:
//
//   - PARENT-CHAIN CLOSURE makes a reply renderable.
//   - THREAD COMPLETENESS is what foldResolvedThreads needs. Closure does NOT
//     provide it: older siblings and descendants of a retained reply stay outside
//     the window.
//
// completeCommentThreads now handles both: it restores a whole affected thread
// within explicit budgets or drops that thread as one unit. A completed default
// window is therefore safe to fold even though the endpoint still advertises
// that unrelated older comments were truncated.

// seedCommentRow inserts one comment at an exact timestamp and returns its id.
// resolvedAt marks it as the thread's resolution when non-nil.
func seedCommentRow(t *testing.T, issueID string, at time.Time, content string, parentID *string, resolvedAt *time.Time) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type,
		                     created_at, updated_at, parent_id, resolved_at, resolved_by_type, resolved_by_id)
		VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $5, $6, $7,
		        CASE WHEN $7::timestamptz IS NULL THEN NULL ELSE 'member' END,
		        CASE WHEN $7::timestamptz IS NULL THEN NULL ELSE $3::uuid END)
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, content, at, parentID, resolvedAt).Scan(&id)
	if err != nil {
		t.Fatalf("seed comment %q: %v", content, err)
	}
	return id
}

// assertNoOrphanCommentRows pins the invariant the UI depends on: every returned
// reply's parent is in the same response.
func assertNoOrphanCommentRows(t *testing.T, rows []CommentResponse) {
	t.Helper()
	present := make(map[string]struct{}, len(rows))
	for _, c := range rows {
		present[c.ID] = struct{}{}
	}
	for _, c := range rows {
		if c.ParentID == nil || *c.ParentID == "" {
			continue
		}
		if _, ok := present[*c.ParentID]; !ok {
			t.Errorf("comment %s references parent %s absent from the response", c.ID, *c.ParentID)
		}
	}
}

// TestListComments_ExactlyAtCapStillFolds pins the probe read. An issue holding
// exactly commentHardCap comments is complete, so the fold must still run —
// inferring truncation from the row count would silently stop folding here.
func TestListComments_ExactlyAtCapStillFolds(t *testing.T) {
	issueID := createIssueForTimeline(t, "exactly at cap still folds")

	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	rootID := seedCommentRow(t, issueID, base, "root question", nil, nil)
	seedCommentRow(t, issueID, base.Add(time.Second), "chatter", &rootID, nil)
	resolvedAt := base.Add(2 * time.Second)
	seedCommentRow(t, issueID, resolvedAt, "the conclusion", &rootID, &resolvedAt)
	// Fill to exactly the cap.
	bulkSeedComments(t, issueID, base.Add(time.Minute), commentHardCap-3)

	w, rows := listComments(t, issueID, "fold=true")
	if rows == nil {
		t.Fatal("ListComments returned no rows")
	}
	if got := w.Header().Get(HeaderCommentsTruncated); got != "" {
		t.Errorf("%s = %q, want unset at exact cap", HeaderCommentsTruncated, got)
	}
	var root *CommentResponse
	for i := range rows {
		if rows[i].ID == rootID {
			root = &rows[i]
		}
	}
	if root == nil {
		t.Fatal("thread root missing")
	}
	if root.ThreadResolved == nil || !*root.ThreadResolved {
		t.Error("thread_resolved not set: exactly-at-cap is complete, so the fold must run")
	}
}

// TestListComments_TruncatedCompleteThreadStillFolds covers the review finding. The
// resolution reply is inside the window while older ordinary replies on the same
// thread are outside it. completeCommentThreads restores those replies before
// folding, so folded_count reflects the complete thread rather than a partial
// window.
func TestListComments_TruncatedCompleteThreadStillFolds(t *testing.T) {
	issueID := createIssueForTimeline(t, "truncated complete thread still folds")

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	rootID := seedCommentRow(t, issueID, base, "root question", nil, nil)
	// Older ordinary replies that the window will cut away.
	for i := 0; i < 3; i++ {
		seedCommentRow(t, issueID, base.Add(time.Duration(i+1)*time.Second), "old reply", &rootID, nil)
	}

	// Push root + the old replies out of the newest-cap window.
	bulkSeedComments(t, issueID, base.Add(time.Minute), commentHardCap+50)

	// Inside the window: an ordinary reply the fold would discard, plus the
	// resolution that would make the fold fire.
	now := time.Now().UTC().Truncate(time.Second)
	keptReplyID := seedCommentRow(t, issueID, now, "fresh ordinary reply", &rootID, nil)
	resolvedAt := now.Add(time.Second)
	conclusionID := seedCommentRow(t, issueID, resolvedAt, "the conclusion", &rootID, &resolvedAt)

	w, rows := listComments(t, issueID, "fold=true")
	if rows == nil {
		t.Fatal("ListComments returned no rows")
	}
	if got := w.Header().Get(HeaderCommentsTruncated); got != "true" {
		t.Errorf("%s = %q, want true", HeaderCommentsTruncated, got)
	}

	byID := make(map[string]CommentResponse, len(rows))
	for _, c := range rows {
		byID[c.ID] = c
	}

	// Thread completion restores the root and every old reply before folding.
	root, ok := byID[rootID]
	if !ok {
		t.Fatal("the old thread root was not backfilled")
	}
	if _, ok := byID[conclusionID]; !ok {
		t.Error("the resolution reply is missing")
	}
	if _, ok := byID[keptReplyID]; ok {
		t.Error("fresh ordinary reply was not folded from a completed resolved thread")
	}
	if root.ThreadResolved == nil || !*root.ThreadResolved {
		t.Error("thread_resolved missing after the truncated window was completed")
	}
	if root.FoldedCount == nil {
		t.Error("folded_count missing, want 4 complete ordinary replies")
	} else if *root.FoldedCount != 4 {
		t.Errorf("folded_count = %d, want 4 complete ordinary replies", *root.FoldedCount)
	}
	assertNoOrphanCommentRows(t, rows)
}

// TestListComments_RootResolvedTruncatedUsesCompleteFoldedCount is the
// root-resolved variant: completion restores all six replies before folding.
func TestListComments_RootResolvedTruncatedUsesCompleteFoldedCount(t *testing.T) {
	issueID := createIssueForTimeline(t, "root resolved truncated")

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	resolvedAt := base
	rootID := seedCommentRow(t, issueID, base, "settled topic", nil, &resolvedAt)
	for i := 0; i < 5; i++ {
		seedCommentRow(t, issueID, base.Add(time.Duration(i+1)*time.Second), "old reply", &rootID, nil)
	}

	bulkSeedComments(t, issueID, base.Add(time.Minute), commentHardCap+50)
	seedCommentRow(t, issueID, time.Now().UTC().Truncate(time.Second), "fresh reply", &rootID, nil)

	w, rows := listComments(t, issueID, "fold=true")
	if rows == nil {
		t.Fatal("ListComments returned no rows")
	}
	if got := w.Header().Get(HeaderCommentsTruncated); got != "true" {
		t.Errorf("%s = %q, want true", HeaderCommentsTruncated, got)
	}
	foundRoot := false
	for _, c := range rows {
		if c.ID != rootID {
			continue
		}
		foundRoot = true
		if c.FoldedCount == nil {
			t.Error("folded_count missing, want all 6 replies")
		} else if *c.FoldedCount != 6 {
			t.Errorf("folded_count = %d, want all 6 replies", *c.FoldedCount)
		}
		if c.ThreadResolved == nil || !*c.ThreadResolved {
			t.Error("thread_resolved missing after completion")
		}
	}
	if !foundRoot {
		t.Fatal("resolved root missing")
	}
	assertNoOrphanCommentRows(t, rows)
}

// TestListComments_RootsOnlyHardCapKeepsNewestRoots pins the roots-only window:
// the API must not retain the oldest 2000 roots and silently lose new discussion.
func TestListComments_RootsOnlyHardCapKeepsNewestRoots(t *testing.T) {
	issueID := createIssueForTimeline(t, "roots only keeps newest")

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	oldestID := seedCommentRow(t, issueID, base, "oldest root", nil, nil)
	bulkSeedComments(t, issueID, base.Add(time.Second), commentHardCap+20)
	newestID := seedCommentRow(t, issueID, base.Add(2*time.Hour), "newest root", nil, nil)

	w, rows := listComments(t, issueID, "roots_only=true")
	if rows == nil {
		t.Fatal("ListComments returned no rows")
	}
	if got := w.Header().Get(HeaderCommentsTruncated); got != "true" {
		t.Errorf("%s = %q, want true", HeaderCommentsTruncated, got)
	}
	if len(rows) != commentHardCap {
		t.Fatalf("root count = %d, want %d", len(rows), commentHardCap)
	}
	present := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		present[row.ID] = struct{}{}
	}
	if _, ok := present[oldestID]; ok {
		t.Error("oldest root survived the newest-root cap")
	}
	if _, ok := present[newestID]; !ok {
		t.Error("newest root was dropped by the roots-only cap")
	}
}

func TestListComments_RootsOnlyExactCapIsNotTruncated(t *testing.T) {
	issueID := createIssueForTimeline(t, "roots only exact cap")
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	bulkSeedComments(t, issueID, base, commentHardCap)

	w, rows := listComments(t, issueID, "roots_only=true")
	if len(rows) != commentHardCap {
		t.Fatalf("root count = %d, want %d", len(rows), commentHardCap)
	}
	if got := w.Header().Get(HeaderCommentsTruncated); got != "" {
		t.Errorf("%s = %q, want unset at exact cap", HeaderCommentsTruncated, got)
	}
}

// TestListComments_UntailedThreadHardCapKeepsNewestReplies proves that a plain
// --thread read cannot lose the newest resolution reply. The returned thread is
// partial, so fold must remain suppressed even though the resolution is visible.
func TestListComments_UntailedThreadHardCapKeepsNewestReplies(t *testing.T) {
	issueID := createIssueForTimeline(t, "thread keeps newest replies")

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	rootID := seedCommentRow(t, issueID, base, "thread root", nil, nil)
	oldestReplyID := seedCommentRow(t, issueID, base.Add(time.Second), "oldest reply", &rootID, nil)
	bulkSeedReplies(t, issueID, rootID, base.Add(2*time.Second), commentHardCap)
	resolvedAt := base.Add(2 * time.Hour)
	newestReplyID := seedCommentRow(t, issueID, resolvedAt, "newest resolution", &rootID, &resolvedAt)

	w, rows := listComments(t, issueID, "thread="+rootID+"&fold=true")
	if rows == nil {
		t.Fatal("ListComments returned no rows")
	}
	if got := w.Header().Get(HeaderCommentsTruncated); got != "true" {
		t.Errorf("%s = %q, want true", HeaderCommentsTruncated, got)
	}
	if len(rows) != commentHardCap {
		t.Fatalf("thread row count = %d, want %d", len(rows), commentHardCap)
	}
	present := make(map[string]CommentResponse, len(rows))
	for _, row := range rows {
		present[row.ID] = row
	}
	if _, ok := present[rootID]; !ok {
		t.Error("thread root missing")
	}
	if _, ok := present[oldestReplyID]; ok {
		t.Error("oldest reply survived the newest-reply cap")
	}
	if _, ok := present[newestReplyID]; !ok {
		t.Error("newest resolution reply was dropped")
	}
	if root := present[rootID]; root.ThreadResolved != nil || root.FoldedCount != nil {
		t.Error("partial oversized thread was folded")
	}
}

func TestListComments_UntailedThreadExactCapStillFolds(t *testing.T) {
	issueID := createIssueForTimeline(t, "thread exact cap still folds")

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	resolvedAt := base
	rootID := seedCommentRow(t, issueID, base, "resolved root", nil, &resolvedAt)
	bulkSeedReplies(t, issueID, rootID, base.Add(time.Second), commentHardCap-1)

	w, rows := listComments(t, issueID, "thread="+rootID+"&fold=true")
	if got := w.Header().Get(HeaderCommentsTruncated); got != "" {
		t.Errorf("%s = %q, want unset at exact cap", HeaderCommentsTruncated, got)
	}
	if len(rows) != 1 || rows[0].ID != rootID {
		t.Fatalf("exact-cap resolved thread did not fold to its root: ids=%v", ids(rows))
	}
	if rows[0].FoldedCount == nil || *rows[0].FoldedCount != commentHardCap-1 {
		t.Fatalf("folded_count = %v, want %d", rows[0].FoldedCount, commentHardCap-1)
	}
}

// TestListComments_DeepChainBeyondBudgetIsPrunedNotOrphaned exercises the depth
// budget. A chain deeper than commentThreadMaxDepth cannot be closed, so the
// affected reply is dropped rather than returned as an unrenderable orphan. The
// response must stay bounded and must terminate.
func TestListComments_DeepChainBeyondBudgetIsPrunedNotOrphaned(t *testing.T) {
	issueID := createIssueForTimeline(t, "deep chain beyond budget")

	base := time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Second)
	// A chain deeper than the walk is allowed to climb.
	depth := commentThreadMaxDepth + 6
	rootID := seedCommentRow(t, issueID, base, "chain root", nil, nil)
	parent := rootID
	for i := 0; i < depth; i++ {
		parent = seedCommentRow(t, issueID, base.Add(time.Duration(i+1)*time.Second), "chain link", &parent, nil)
	}
	// Push the whole chain out of the window.
	bulkSeedComments(t, issueID, base.Add(time.Hour), commentHardCap+50)
	// The newest comment is a reply at the bottom of that chain.
	deepReplyID := seedCommentRow(t, issueID, time.Now().UTC().Truncate(time.Second), "deep reply", &parent, nil)

	_, rows := listComments(t, issueID, "")
	if rows == nil {
		t.Fatal("ListComments returned no rows")
	}

	// Bounded: window plus at most the ancestor budget.
	if len(rows) > commentHardCap+commentThreadContextBudget {
		t.Errorf("returned %d comments, exceeds the provable bound of %d",
			len(rows), commentHardCap+commentThreadContextBudget)
	}
	// The chain could not be closed, so the deep reply is dropped rather than
	// emitted as an orphan.
	for _, c := range rows {
		if c.ID == deepReplyID {
			t.Error("the deep reply survived without a closed parent chain")
		}
	}
	assertNoOrphanCommentRows(t, rows)
}

// TestListComments_SharedAncestorFetchedOnce checks the dedup in the walk: many
// replies pointing at one old root must not duplicate that root.
func TestListComments_SharedAncestorFetchedOnce(t *testing.T) {
	issueID := createIssueForTimeline(t, "shared ancestor")

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	rootID := seedCommentRow(t, issueID, base, "shared root", nil, nil)
	bulkSeedComments(t, issueID, base.Add(time.Minute), commentHardCap+50)

	now := time.Now().UTC().Truncate(time.Second)
	var replies []string
	for i := 0; i < 3; i++ {
		replies = append(replies, seedCommentRow(t, issueID, now.Add(time.Duration(i)*time.Second), "sibling reply", &rootID, nil))
	}

	_, rows := listComments(t, issueID, "")
	if rows == nil {
		t.Fatal("ListComments returned no rows")
	}

	seen := 0
	present := make(map[string]struct{}, len(rows))
	for _, c := range rows {
		present[c.ID] = struct{}{}
		if c.ID == rootID {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("shared root appears %d times, want exactly 1", seen)
	}
	for _, id := range replies {
		if _, ok := present[id]; !ok {
			t.Errorf("reply %s missing", id)
		}
	}
	assertNoOrphanCommentRows(t, rows)
}

// TestListComments_CrossIssueParentNeverCrossesBoundary is the negative tenant
// test. parent_id carries a foreign key to comment(id) but NOT to a matching
// issue, so a stray cross-issue parent reference is representable in stored data.
// The walk filters on issue_id and workspace_id at every level, so the foreign
// comment must never appear and the anomalous reply must be pruned.
func TestListComments_CrossIssueParentNeverCrossesBoundary(t *testing.T) {
	otherIssueID := createIssueForTimeline(t, "other issue")
	issueID := createIssueForTimeline(t, "cross issue parent")

	foreignID := seedCommentRow(t, otherIssueID, time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
		"comment belonging to another issue", nil, nil)

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	bulkSeedComments(t, issueID, base, commentHardCap+50)
	// A reply in THIS issue whose parent lives in the other issue.
	strayID := seedCommentRow(t, issueID, time.Now().UTC().Truncate(time.Second),
		"reply with a foreign parent", &foreignID, nil)

	_, rows := listComments(t, issueID, "")
	if rows == nil {
		t.Fatal("ListComments returned no rows")
	}

	for _, c := range rows {
		if c.ID == foreignID {
			t.Error("a comment from another issue leaked into this issue's response")
		}
		if c.ID == strayID {
			t.Error("the reply with an out-of-issue parent was returned as an orphan")
		}
	}
	assertNoOrphanCommentRows(t, rows)
}

// TestListComments_CrossWorkspaceParentNeverCrossesBoundary independently pins
// the workspace predicate. The parent deliberately carries the target issue_id
// but a different workspace_id, so retaining issue_id filtering while removing
// workspace_id filtering must still make this test fail.
func TestListComments_CrossWorkspaceParentNeverCrossesBoundary(t *testing.T) {
	ctx := context.Background()
	issueID := createIssueForTimeline(t, "cross workspace parent")

	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Comment parent boundary', 'comment-parent-boundary-' || gen_random_uuid()::text,
		        'Foreign workspace', 'CPB')
		RETURNING id
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("insert foreign workspace: %v", err)
	}

	var foreignID string
	t.Cleanup(func() {
		if foreignID != "" {
			// Deleting the foreign parent cascades to the anomalous reply below.
			testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, foreignID)
		}
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type,
		                     created_at, updated_at)
		VALUES ($1, $2, 'member', $3, 'comment belonging to another workspace', 'comment',
		        $4, $4)
		RETURNING id
	`, issueID, otherWorkspaceID, testUserID, time.Now().UTC().Add(-time.Hour)).Scan(&foreignID); err != nil {
		t.Fatalf("seed foreign-workspace comment: %v", err)
	}

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	bulkSeedComments(t, issueID, base, commentHardCap+50)
	strayID := seedCommentRow(
		t,
		issueID,
		time.Now().UTC().Truncate(time.Second),
		"reply with a foreign-workspace parent",
		&foreignID,
		nil,
	)

	_, rows := listComments(t, issueID, "")
	if rows == nil {
		t.Fatal("ListComments returned no rows")
	}

	for _, c := range rows {
		if c.ID == foreignID {
			t.Error("a comment from another workspace leaked into this workspace's response")
		}
		if c.ID == strayID {
			t.Error("the reply with an out-of-workspace parent was returned as an orphan")
		}
	}
	assertNoOrphanCommentRows(t, rows)
}

// TestListComments_CrossWorkspaceDescendantNeverCrossesBoundary covers the
// downward half of completeCommentThreads. The restored root belongs to this
// workspace, but one of its stored children does not; the batched descendant
// query must not pull that child into the response.
func TestListComments_CrossWorkspaceDescendantNeverCrossesBoundary(t *testing.T) {
	ctx := context.Background()
	issueID := createIssueForTimeline(t, "cross workspace descendant")

	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Comment descendant boundary', 'comment-descendant-boundary-' || gen_random_uuid()::text,
		        'Foreign workspace', 'CDB')
		RETURNING id
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("insert foreign workspace: %v", err)
	}

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Second)
	rootID := seedCommentRow(t, issueID, base, "old local root", nil, nil)
	var foreignChildID string
	t.Cleanup(func() {
		if foreignChildID != "" {
			testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, foreignChildID)
		}
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type,
		                     created_at, updated_at, parent_id)
		VALUES ($1, $2, 'member', $3, 'foreign-workspace child', 'comment', $4, $4, $5)
		RETURNING id
	`, issueID, otherWorkspaceID, testUserID, base.Add(time.Second), rootID).Scan(&foreignChildID); err != nil {
		t.Fatalf("seed foreign-workspace child: %v", err)
	}

	bulkSeedComments(t, issueID, base.Add(time.Minute), commentHardCap+50)
	freshReplyID := seedCommentRow(
		t,
		issueID,
		time.Now().UTC().Truncate(time.Second),
		"fresh local reply",
		&rootID,
		nil,
	)

	_, rows := listComments(t, issueID, "")
	if rows == nil {
		t.Fatal("ListComments returned no rows")
	}

	present := make(map[string]struct{}, len(rows))
	for _, c := range rows {
		present[c.ID] = struct{}{}
	}
	if _, leaked := present[foreignChildID]; leaked {
		t.Error("a descendant from another workspace leaked into the response")
	}
	if _, ok := present[rootID]; !ok {
		t.Error("the same-workspace root was not restored")
	}
	if _, ok := present[freshReplyID]; !ok {
		t.Error("the same-workspace fresh reply is missing")
	}
	assertNoOrphanCommentRows(t, rows)
}
