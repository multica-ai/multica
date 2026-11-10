package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
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

func TestAutopilotCreateIssueHealsCounterDrift(t *testing.T) {
	for _, tc := range []struct {
		name    string
		numbers []int
		counter int
		want    int32
	}{
		{name: "numbered issues", numbers: []int{1, 2, 3, 4, 5}, counter: 2, want: 6},
		{name: "reported workspace", numbers: []int{1095, 1096}, counter: 1094, want: 1097},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx, ownerID := newPrincipalFixture(t)
			agentID := fx.privateAgentOwnedBy(t, ownerID, "counter-drift")
			autopilotID, triggerID := fx.autopilotWithTrigger(t, agentID, ownerID, ownerID)
			fx.Exec(t, `UPDATE autopilot SET execution_mode = 'create_issue' WHERE id = $1`, autopilotID)
			for _, number := range tc.numbers {
				fx.Issue(t, fmt.Sprintf("drift seed %d", number), testutil.Cols{"number": number})
			}
			fx.Exec(t, `UPDATE workspace SET issue_counter = $2 WHERE id = $1`, fx.WorkspaceID, tc.counter)
			fx.Cleanup(t, `DELETE FROM issue WHERE workspace_id = $1 AND origin_type = 'autopilot' AND origin_id = $2`, fx.WorkspaceID, autopilotID)
			fx.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, autopilotID)
			fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)

			run := fx.dispatch(t, autopilotID, triggerID)
			if run.Status != "issue_created" || !run.IssueID.Valid || !run.TaskID.Valid {
				t.Fatalf("dispatch = %+v; want an issue_created run linked to an issue and task", run)
			}

			var number, counter int32
			fx.QueryRow(t, `SELECT number FROM issue WHERE id = $1 AND workspace_id = $2`, run.IssueID, fx.WorkspaceID).Scan(&number)
			if number != tc.want {
				t.Fatalf("created issue number = %d, want %d", number, tc.want)
			}
			fx.QueryRow(t, `SELECT issue_counter FROM workspace WHERE id = $1`, fx.WorkspaceID).Scan(&counter)
			if counter != tc.want {
				t.Errorf("issue_counter = %d, want %d", counter, tc.want)
			}
			if n := fx.Count(t, `SELECT count(*) FROM issue WHERE workspace_id = $1 AND origin_type = 'autopilot' AND origin_id = $2`, fx.WorkspaceID, autopilotID); n != 1 {
				t.Errorf("dispatch created %d issues, want 1", n)
			}
			if n := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, run.IssueID, agentID); n != 1 {
				t.Errorf("dispatch enqueued %d tasks, want 1", n)
			}
		})
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
