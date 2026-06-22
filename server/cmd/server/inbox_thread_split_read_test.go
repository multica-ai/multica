// CEREBRO-PATCH(inbox-thread-split-read-test): FIR-1854 — DB test for the
// cerebro-only thread-split read queries. Cerebro-prefixed test in the upstream
// cmd/server package; covers the query trio in isolation.
package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// insertInboxItemWithDetails inserts an inbox row carrying a details JSON blob,
// so a thread reply (details.thread_root_id set) can be distinguished from a
// top-level channel message (no thread_root_id) — the core of FIR-1854.
func insertInboxItemWithDetails(t *testing.T, workspaceID, recipientID, issueID, details string, read bool, createdAt time.Time) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id,
			type, severity, issue_id, title, route, read, archived, details, created_at)
		VALUES ($1, 'member', $2, 'new_comment', 'info', $3,
		        'thread-split fixture', 'inbox', $4, false, $5, $6)
	`, workspaceID, recipientID, issueID, read, details, createdAt)
	if err != nil {
		t.Fatalf("insertInboxItemWithDetails: %v", err)
	}
}

// TestInboxThreadSplitReadQueries verifies the FIR-1854 query trio that gives a
// channel/DM thread its own read state, independent of the channel:
//   - the channel unread count excludes thread replies,
//   - marking the channel read leaves thread replies unread,
//   - marking a thread read clears only that thread.
func TestInboxThreadSplitReadQueries(t *testing.T) {
	queries := db.New(testPool)
	ctx := context.Background()

	recipientEmail := "thread-split-recipient@multica.ai"
	recipientID := createTestUser(t, recipientEmail)
	t.Cleanup(func() { cleanupTestUser(t, recipientEmail) })

	issue := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupInboxForIssue(t, issue) })

	const threadRoot = "11111111-1111-1111-1111-111111111111"
	now := time.Now()

	// One top-level message (no thread_root_id) and one reply living inside a
	// thread (thread_root_id set), both unread.
	insertInboxItemWithDetails(t, testWorkspaceID, recipientID, issue,
		`{"comment_id":"c-top"}`, false, now)
	insertInboxItemWithDetails(t, testWorkspaceID, recipientID, issue,
		`{"comment_id":"c-reply","thread_root_id":"`+threadRoot+`"}`, false, now.Add(time.Second))

	recip := mustParseUUIDTest(t, recipientID)
	issueUUID := mustParseUUIDTest(t, issue)

	// Total unread = both rows; thread-excluding unread = only the top-level row.
	all, err := queries.CountUnreadInboxForChannel(ctx, db.CountUnreadInboxForChannelParams{RecipientID: recip, IssueID: issueUUID})
	if err != nil {
		t.Fatalf("CountUnreadInboxForChannel: %v", err)
	}
	if all != 2 {
		t.Fatalf("total unread = %d, want 2", all)
	}
	excl, err := queries.CountUnreadInboxForChannelExcludingThreads(ctx, db.CountUnreadInboxForChannelExcludingThreadsParams{RecipientID: recip, IssueID: issueUUID})
	if err != nil {
		t.Fatalf("CountUnreadInboxForChannelExcludingThreads: %v", err)
	}
	if excl != 1 {
		t.Fatalf("thread-excluding unread = %d, want 1 (thread reply must not inflate the channel badge)", excl)
	}

	// Marking the channel read (excluding threads) clears the top-level row but
	// must leave the thread reply unread.
	marked, err := queries.MarkInboxReadByIssueExcludingThreads(ctx, db.MarkInboxReadByIssueExcludingThreadsParams{
		WorkspaceID: mustParseUUIDTest(t, testWorkspaceID), RecipientType: "member", RecipientID: recip, IssueID: issueUUID,
	})
	if err != nil {
		t.Fatalf("MarkInboxReadByIssueExcludingThreads: %v", err)
	}
	if marked != 1 {
		t.Fatalf("channel mark-read affected %d rows, want 1 (only the top-level message)", marked)
	}
	if excl, _ := queries.CountUnreadInboxForChannelExcludingThreads(ctx, db.CountUnreadInboxForChannelExcludingThreadsParams{RecipientID: recip, IssueID: issueUUID}); excl != 0 {
		t.Fatalf("channel unread after mark = %d, want 0", excl)
	}
	if all, _ := queries.CountUnreadInboxForChannel(ctx, db.CountUnreadInboxForChannelParams{RecipientID: recip, IssueID: issueUUID}); all != 1 {
		t.Fatalf("total unread after channel mark-read = %d, want 1 (thread reply still unread)", all)
	}

	// Marking the thread read clears only the thread reply.
	tmarked, err := queries.MarkInboxReadByThreadRoot(ctx, db.MarkInboxReadByThreadRootParams{
		WorkspaceID: mustParseUUIDTest(t, testWorkspaceID), RecipientType: "member", RecipientID: recip, IssueID: issueUUID, ThreadRootID: threadRoot,
	})
	if err != nil {
		t.Fatalf("MarkInboxReadByThreadRoot: %v", err)
	}
	if tmarked != 1 {
		t.Fatalf("thread mark-read affected %d rows, want 1", tmarked)
	}
	if all, _ := queries.CountUnreadInboxForChannel(ctx, db.CountUnreadInboxForChannelParams{RecipientID: recip, IssueID: issueUUID}); all != 0 {
		t.Fatalf("total unread after thread mark-read = %d, want 0", all)
	}
}

func mustParseUUIDTest(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return u
}
