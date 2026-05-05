package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EVAL for Phase 3 — WS audience filtering. Verifies the audience computation
// is correct for restricted projects, standalone-private issues, and the
// always-on workspace-admin governance rule.

func TestAudienceForIssue_RestrictedProject(t *testing.T) {
	f := setupFilterFixture(t)
	ctx := context.Background()

	issue, err := testHandler.Queries.GetIssue(ctx, util.ParseUUID(f.restrictIssID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	audience := testHandler.audienceForIssue(ctx, issue)
	if audience == nil {
		t.Fatalf("restricted-project issue: expected non-nil audience")
	}

	hasMember := false
	hasOutside := false
	for _, uid := range audience {
		if uid == f.memberUserID {
			hasMember = true
		}
		if uid == f.outsideUserID {
			hasOutside = true
		}
	}
	if !hasMember {
		t.Fatalf("project member missing from audience: %v", audience)
	}
	if hasOutside {
		t.Fatalf("outsider should NOT be in restricted-project audience: %v", audience)
	}

	// The handler test fixture's owner (testUserID) must be in the audience —
	// owners always have access to every project.
	hasOwner := false
	for _, uid := range audience {
		if uid == testUserID {
			hasOwner = true
		}
	}
	if !hasOwner {
		t.Fatalf("workspace owner missing from audience (always-on rule): %v", audience)
	}
}

func TestAudienceForIssue_OpenProjectNoFilter(t *testing.T) {
	f := setupFilterFixture(t)
	ctx := context.Background()

	issue, err := testHandler.Queries.GetIssue(ctx, util.ParseUUID(f.openIssueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	audience := testHandler.audienceForIssue(ctx, issue)
	if audience != nil {
		t.Fatalf("open project: expected nil audience (broadcast to all), got %v", audience)
	}
}

func TestAudienceForIssue_StandalonePrivate(t *testing.T) {
	f := setupFilterFixture(t)
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, is_private)
		VALUES ($1, 'ws-private', 'todo', 'medium', 'member', $2, 0, 9997, true)
		RETURNING id
	`, testWorkspaceID, f.memberUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	issue, err := testHandler.Queries.GetIssue(ctx, util.ParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	audience := testHandler.audienceForIssue(ctx, issue)
	if audience == nil {
		t.Fatalf("standalone private: expected non-nil audience")
	}
	hasCreator := false
	hasOutside := false
	for _, uid := range audience {
		if uid == f.memberUserID {
			hasCreator = true
		}
		if uid == f.outsideUserID {
			hasOutside = true
		}
	}
	if !hasCreator {
		t.Fatalf("creator missing from private-issue audience: %v", audience)
	}
	if hasOutside {
		t.Fatalf("outsider should NOT be in private-issue audience: %v", audience)
	}
}

func TestAudienceForIssue_StandalonePublicNoFilter(t *testing.T) {
	ctx := context.Background()
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, is_private)
		VALUES ($1, 'ws-public', 'todo', 'medium', 'member', $2, 0, 9996, false)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	issue, err := testHandler.Queries.GetIssue(ctx, util.ParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	if audience := testHandler.audienceForIssue(ctx, issue); audience != nil {
		t.Fatalf("standalone non-private: expected nil audience, got %v", audience)
	}
	_ = db.Issue{}
}
