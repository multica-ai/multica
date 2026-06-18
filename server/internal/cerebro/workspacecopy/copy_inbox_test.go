package workspacecopy

// Integration tests for CopyInbox (TECH-3766 "copy my inbox"), run against the
// same real Postgres as store_test.go. They assert that the pass copies exactly
// the issues in the caller's inbox feed — not archived items, not the
// notifications route, not another user's inbox — and that a re-run is idempotent.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// seedInboxItem inserts an inbox_item row for the given recipient pointing at an
// issue, with explicit archived/route so a test can prove the filter.
func seedInboxItem(t *testing.T, ctx context.Context, ws, recipient, issue pgtype.UUID, archived bool, route string) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO inbox_item (id, workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, archived, route)
		VALUES ($1,$2,'member',$3,'new_comment','info',$4,'n',$5,$6)`,
		newID(), ws, recipient, issue, archived, route); err != nil {
		t.Fatalf("seed inbox item: %v", err)
	}
}

// issuesInWorkspace counts issues copied into a workspace.
func issuesInWorkspace(t *testing.T, ctx context.Context, ws pgtype.UUID) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1`, ws).Scan(&n); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	return n
}

func TestCopyInbox_CopiesExactlyTheInboxIssues(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	srcWS, tgtWS := freshWorkspaces(t, ctx)
	other := newID() // a different recipient whose inbox must be ignored

	// Two issues in the caller's inbox; one archived; one on the notifications
	// route; one belonging to another user — only the first two must be copied.
	inboxA := seedIssue(t, ctx, srcWS, 4001, "todo", "", pgtype.UUID{}, pgtype.UUID{})
	inboxB := seedIssue(t, ctx, srcWS, 4002, "in_progress", "", pgtype.UUID{}, pgtype.UUID{})
	archivedC := seedIssue(t, ctx, srcWS, 4003, "todo", "", pgtype.UUID{}, pgtype.UUID{})
	notifD := seedIssue(t, ctx, srcWS, 4004, "todo", "", pgtype.UUID{}, pgtype.UUID{})
	otherE := seedIssue(t, ctx, srcWS, 4005, "todo", "", pgtype.UUID{}, pgtype.UUID{})

	seedInboxItem(t, ctx, srcWS, testUser, inboxA, false, "inbox")
	seedInboxItem(t, ctx, srcWS, testUser, inboxB, false, "inbox")
	// A second inbox item for the SAME issue proves DISTINCT (no double copy).
	seedInboxItem(t, ctx, srcWS, testUser, inboxA, false, "inbox")
	seedInboxItem(t, ctx, srcWS, testUser, archivedC, true, "inbox")
	seedInboxItem(t, ctx, srcWS, testUser, notifD, false, "notifications")
	seedInboxItem(t, ctx, srcWS, other, otherE, false, "inbox")

	res, err := s.CopyInbox(ctx, newID(), srcWS, tgtWS, testUser)
	if err != nil {
		t.Fatalf("CopyInbox: %v", err)
	}
	if res.InboxIssues != 2 || res.IssuesCopied != 2 {
		t.Fatalf("got InboxIssues=%d IssuesCopied=%d, want 2/2", res.InboxIssues, res.IssuesCopied)
	}
	if n := issuesInWorkspace(t, ctx, tgtWS); n != 2 {
		t.Fatalf("target has %d issues, want exactly 2 (inbox A+B only)", n)
	}

	// Idempotent: a re-run copies nothing new but still reports the same 2 found.
	res2, err := s.CopyInbox(ctx, newID(), srcWS, tgtWS, testUser)
	if err != nil {
		t.Fatalf("CopyInbox re-run: %v", err)
	}
	if res2.InboxIssues != 2 || res2.IssuesCopied != 0 {
		t.Fatalf("re-run got InboxIssues=%d IssuesCopied=%d, want 2/0", res2.InboxIssues, res2.IssuesCopied)
	}
	if n := issuesInWorkspace(t, ctx, tgtWS); n != 2 {
		t.Fatalf("after re-run target has %d issues, want 2", n)
	}

	// Source is untouched.
	if n := issuesInWorkspace(t, ctx, srcWS); n != 5 {
		t.Fatalf("source has %d issues, want 5 (untouched)", n)
	}
	_ = inboxB
}

func TestCopyInbox_EmptyInbox(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	s := New(testPool)
	srcWS, tgtWS := freshWorkspaces(t, ctx)

	res, err := s.CopyInbox(ctx, newID(), srcWS, tgtWS, testUser)
	if err != nil {
		t.Fatalf("CopyInbox on empty inbox: %v", err)
	}
	if res.InboxIssues != 0 || res.IssuesCopied != 0 {
		t.Fatalf("got %d/%d, want 0/0 for an empty inbox", res.InboxIssues, res.IssuesCopied)
	}
}
