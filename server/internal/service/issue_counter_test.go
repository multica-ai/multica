package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestIssueCreateHealsCounterDrift drives a real create against a workspace whose
// counter trails a positive MAX(issue.number): five issues hold 1..5 and the
// counter is left at 2, so the next allocation lands on 3 — a number this
// workspace already holds.
//
// Pre-fix, what fails here is the duplicate-key error itself: the create returns
// the 23505 on uq_issue_workspace_number and every later create in the workspace
// would retry the same taken number, because the failed transaction rolls its own
// counter increment back with the INSERT. With the heal, the counter is repaired
// to 5 and the create lands on 6.
func TestIssueCreateHealsCounterDrift(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	workspaceIDString, userIDString, _, _ := seedAttributionFixture(t, pool)
	workspaceID := util.MustParseUUID(workspaceIDString)
	userID := util.MustParseUUID(userIDString)

	for number := 1; number <= 5; number++ {
		var seededID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
			VALUES ($1, $2, 'member', $3, $4)
			RETURNING id`, workspaceID, fmt.Sprintf("drift seed %d", number), userID, number).Scan(&seededID); err != nil {
			t.Fatalf("seed numbered issue %d: %v", number, err)
		}
		t.Cleanup(func() {
			pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, seededID)
		})
	}
	if _, err := pool.Exec(ctx, `UPDATE workspace SET issue_counter = 2 WHERE id = $1`, workspaceID); err != nil {
		t.Fatalf("set issue_counter behind MAX(number): %v", err)
	}

	svc := NewIssueService(queries, pool, nil, nil, nil)
	result, err := svc.Create(ctx, IssueCreateParams{
		WorkspaceID: workspaceID,
		Title:       "created against a drifted counter",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   userID,
	}, IssueCreateOpts{})
	if err != nil {
		t.Fatalf("create against a drifted counter: %v", err)
	}
	created := result.Issue
	t.Cleanup(func() {
		if err := queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: created.ID, WorkspaceID: workspaceID}); err != nil {
			t.Errorf("delete created issue: %v", err)
		}
	})

	if created.Number != 6 {
		t.Fatalf("allocated number = %d, want 6: counter was 2 and 1..5 are already held", created.Number)
	}

	var counter int32
	if err := pool.QueryRow(ctx, `SELECT issue_counter FROM workspace WHERE id = $1`, workspaceID).Scan(&counter); err != nil {
		t.Fatalf("read issue_counter: %v", err)
	}
	if counter < 6 {
		t.Fatalf("issue_counter = %d after the create, want at least 6", counter)
	}
}

// TestIssueCreateOnHealthyWorkspaceAllocatesCounterPlusOne pins the healthy path:
// a fresh workspace (counter 0, its only issue at the number default 0) advances
// by exactly one and nothing else is consulted or rewritten.
func TestIssueCreateOnHealthyWorkspaceAllocatesCounterPlusOne(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	workspaceIDString, userIDString, _, _ := seedAttributionFixture(t, pool)
	workspaceID := util.MustParseUUID(workspaceIDString)
	userID := util.MustParseUUID(userIDString)

	svc := NewIssueService(queries, pool, nil, nil, nil)
	result, err := svc.Create(ctx, IssueCreateParams{
		WorkspaceID: workspaceID,
		Title:       "created against a healthy counter",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   userID,
	}, IssueCreateOpts{})
	if err != nil {
		t.Fatalf("create on a healthy workspace: %v", err)
	}
	created := result.Issue
	t.Cleanup(func() {
		if err := queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: created.ID, WorkspaceID: workspaceID}); err != nil {
			t.Errorf("delete created issue: %v", err)
		}
	})

	if created.Number != 1 {
		t.Fatalf("allocated number = %d, want 1 (issue_counter 0 + 1)", created.Number)
	}
	var counter int32
	if err := pool.QueryRow(ctx, `SELECT issue_counter FROM workspace WHERE id = $1`, workspaceID).Scan(&counter); err != nil {
		t.Fatalf("read issue_counter: %v", err)
	}
	if counter != 1 {
		t.Fatalf("issue_counter = %d, want 1", counter)
	}
}

// TestConcurrentIssueCreatesNeverShareANumber is the other half of the same
// guarantee: allocation is serialized on the workspace row, so two creates in
// flight at once cannot both be handed the number they are about to insert.
func TestConcurrentIssueCreatesNeverShareANumber(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	workspaceIDString, userIDString, _, _ := seedAttributionFixture(t, pool)
	workspaceID := util.MustParseUUID(workspaceIDString)
	userID := util.MustParseUUID(userIDString)

	const creates = 8
	svc := NewIssueService(queries, pool, nil, nil, nil)

	start := make(chan struct{})
	type createResult struct {
		issue db.Issue
		err   error
	}
	results := make(chan createResult, creates)
	var wg sync.WaitGroup
	for i := range creates {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			result, err := svc.Create(ctx, IssueCreateParams{
				WorkspaceID: workspaceID,
				Title:       fmt.Sprintf("concurrent create %d", index),
				Status:      "todo",
				Priority:    "none",
				CreatorType: "member",
				CreatorID:   userID,
			}, IssueCreateOpts{})
			results <- createResult{issue: result.Issue, err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	seen := make(map[int32]bool, creates)
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent create: %v", result.err)
		}
		issue := result.issue
		t.Cleanup(func() {
			if err := queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: issue.ID, WorkspaceID: workspaceID}); err != nil {
				t.Errorf("delete created issue: %v", err)
			}
		})
		if seen[issue.Number] {
			t.Fatalf("two concurrent creates were both handed number %d", issue.Number)
		}
		seen[issue.Number] = true
	}
	if len(seen) != creates {
		t.Fatalf("distinct numbers = %d, want %d", len(seen), creates)
	}
}
