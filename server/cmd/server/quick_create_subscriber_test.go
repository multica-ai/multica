package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Subscribing the quick-create requester moved OFF the completion path in
// MUL-5483. It now happens at issue-creation time in the shared
// delegated-subscriber rule, which resolves the human from
// origin_type='quick_create' + the origin task's originator_user_id — the same
// origin waterfall attribution uses. That behavior is covered by
// TestDelegatedSubscribe_QuickCreateKeepsDirectReason in
// delegated_subscriber_test.go, including the assertion that quick create keeps
// the direct 'creator' tier rather than the reduced delegated one.
//
// The failure-path test below stays here: "no issue created ⇒ no subscriber
// row" is a property of the quick-create completion flow itself and holds
// regardless of which layer writes the subscription.

// TestQuickCreateFailure_DoesNotSubscribeRequester confirms the failure path
// (agent finished without producing an issue) does not invent a subscriber
// row — there is nothing to subscribe to.
func TestQuickCreateFailure_DoesNotSubscribeRequester(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"another bug",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	// No issue with origin_type=quick_create + this task id exists. Completion
	// hits the failure branch and writes a failure inbox; no subscriber row.
	if _, err := taskSvc.CompleteTask(ctx, task.ID, []byte(`{"output":"done"}`), "", "", "", false, ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	var leaked int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM issue_subscriber s
		JOIN issue i ON i.id = s.issue_id
		WHERE s.user_type = 'member' AND s.user_id = $1
		  AND i.origin_type = 'quick_create' AND i.origin_id = $2
	`, testUserID, task.ID).Scan(&leaked); err != nil {
		t.Fatalf("count leaked subscribers: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("expected no subscriber rows for failed quick-create, got %d", leaked)
	}
}

// TestQuickCreateFailure_SurfacesAgentOutput locks in the fix for GH #5885: when
// a quick-create agent's `multica issue create` call fails (e.g. the active-
// duplicate guard rejects it), the failure inbox must carry the agent's real
// final output — which the prompt requires to be the CLI error — instead of the
// opaque "agent finished without creating an issue".
func TestQuickCreateFailure_SurfacesAgentOutput(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"file that same bug again",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
		deleteInboxForTask(task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	// No issue is created; the agent exits with the CLI duplicate error as its
	// only output (per the quick-create prompt contract).
	const agentErr = "Error: an active issue already exists: JKY-30 (blocked). Pass --allow-duplicate to override."
	result, _ := json.Marshal(map[string]any{"output": agentErr})
	if _, err := taskSvc.CompleteTask(ctx, task.ID, result, "", "", "", false, ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	body, errDetail, _ := requireQuickCreateOutcomeInbox(t, task.ID, "quick_create_failed")

	if !strings.Contains(errDetail, "JKY-30") {
		t.Fatalf("expected failure detail to carry the agent's real error, got %q", errDetail)
	}
	if strings.Contains(errDetail, "agent finished without creating an issue") {
		t.Fatalf("failure detail regressed to the generic message: %q", errDetail)
	}
	if !strings.Contains(body, "JKY-30") {
		t.Fatalf("expected inbox body to carry the agent's real error, got %q", body)
	}
}

// TestQuickCreateLookupFault_WritesUnconfirmedInbox is the regression for the
// silent-drop path: when the completion lookup itself faults (transient DB
// error rather than pgx.ErrNoRows), the task is already completed and nothing
// re-runs this reconciliation. Returning without writing would strand the
// requester with NO inbox result at all. The run must still end with a
// terminal notification — and one that does NOT assert a failure we never
// observed, nor reuse the agent's output as if it were the confirmed reason.
func TestQuickCreateLookupFault_WritesUnconfirmedInbox(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"file a bug while the db is flaky",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
		deleteInboxForTask(task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	// Completion runs against a Queries whose origin lookup always faults. The
	// completion transaction is unaffected (runInTx binds queries to the tx),
	// and the inbox write still goes to the real pool — so a missing inbox row
	// can only mean the code dropped the notification.
	faulting := service.NewTaskService(
		db.New(failGetIssueByOriginDB{DBTX: testPool, err: errors.New("simulated transient db fault")}),
		testPool, nil, bus,
	)

	// The agent DID emit a duplicate-style error, but with the lookup faulted we
	// cannot confirm it is the reason — it must not be presented as such.
	result, _ := json.Marshal(map[string]any{
		"output": "Error: an active issue already exists: JKY-30 (blocked).",
	})
	if _, err := faulting.CompleteTask(ctx, task.ID, result, "", "", "", false, ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	body, errDetail, title := requireQuickCreateOutcomeInbox(t, task.ID, "quick_create_unconfirmed")

	if !strings.Contains(body, "Couldn't confirm") {
		t.Fatalf("expected the neutral unconfirmed wording, got body %q", body)
	}
	if title == "Quick create failed" {
		t.Fatalf("unconfirmed outcome must not be titled as a definite failure, got %q", title)
	}
	// The agent's output is not evidence of the outcome here — the lookup never
	// completed, so surfacing it would assert a cause we did not verify.
	if strings.Contains(body, "JKY-30") || strings.Contains(errDetail, "JKY-30") {
		t.Fatalf("unconfirmed outcome must not reuse the agent output as the reason: body=%q detail=%q", body, errDetail)
	}
}

// TestQuickCreateFailure_RedactsAgentOutput locks in that the newly-surfaced
// data source (the agent's final output) is scrubbed before it is stored on the
// inbox row. Agent output is untrusted text that can contain whatever the run
// echoed, so credentials must never land in the notification.
func TestQuickCreateFailure_RedactsAgentOutput(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(ctx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"file a bug and leak a token",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
		deleteInboxForTask(task.ID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(ctx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	// Fake, non-functional token shaped like a real GitHub PAT.
	const fakeToken = "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"
	result, _ := json.Marshal(map[string]any{
		"output": "Error: create failed while authenticating with " + fakeToken,
	})
	if _, err := taskSvc.CompleteTask(ctx, task.ID, result, "", "", "", false, ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	body, errDetail, _ := requireQuickCreateOutcomeInbox(t, task.ID, "quick_create_failed")

	if strings.Contains(body, fakeToken) || strings.Contains(errDetail, fakeToken) {
		t.Fatal("agent output reached the inbox row unredacted")
	}
	if !strings.Contains(body, "[REDACTED GITHUB TOKEN]") {
		t.Fatalf("expected the token to be replaced by a redaction placeholder, got %q", body)
	}
}

// TestQuickCreateLookupCancelled_StillWritesUnconfirmedInbox covers the
// cancel/timeout shape of the lookup fault, which the plain-error case cannot:
// when GetIssueByOrigin fails because the caller's context was cancelled or
// timed out, reusing that same context for the notification write would fail it
// for the identical reason and leave the user with nothing. The terminal write
// must be detached from the caller's cancellation.
func TestQuickCreateLookupCancelled_StillWritesUnconfirmedInbox(t *testing.T) {
	setupCtx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	var agentID string
	if err := testPool.QueryRow(setupCtx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	task, err := taskSvc.EnqueueQuickCreateTask(setupCtx,
		parseUUID(testWorkspaceID),
		parseUUID(testUserID),
		parseUUID(agentID),
		pgtype.UUID{},
		"file a bug while the request is cancelled",
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
		deleteInboxForTask(task.ID)
	})

	if _, err := testPool.Exec(setupCtx,
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
		task.ID,
	); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := queries.StartAgentTask(setupCtx, task.ID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}

	// The completion transaction runs on a healthy context; the context dies at
	// the moment of the origin lookup, exactly as a cancelled request would.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	faulting := service.NewTaskService(
		db.New(cancelOnGetIssueByOriginDB{DBTX: testPool, cancel: cancel}),
		testPool, nil, bus,
	)

	result, _ := json.Marshal(map[string]any{"output": "Error: something went wrong"})
	if _, err := faulting.CompleteTask(ctx, task.ID, result, "", "", "", false, ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("test setup is wrong: the context should have been cancelled during the lookup")
	}

	body, _, _ := requireQuickCreateOutcomeInbox(t, task.ID, "quick_create_unconfirmed")
	if !strings.Contains(body, "Couldn't confirm") {
		t.Fatalf("expected the neutral unconfirmed wording, got %q", body)
	}
}

// requireQuickCreateOutcomeInbox returns (body, details.error, title) for the
// quick-create outcome notification of the given type belonging to taskID,
// failing the test when none exists. Scoping by task id keeps
// concurrent/ordering-sensitive cases from reading a sibling test's row; the
// type is asserted because the failed and unconfirmed outcomes must never
// collapse into one another (clients frame them differently).
func requireQuickCreateOutcomeInbox(t *testing.T, taskID pgtype.UUID, inboxType string) (string, string, string) {
	t.Helper()
	var body, errDetail, title string
	err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(body, ''), COALESCE(details->>'error', ''), title
		FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1
		  AND type = $3
		  AND details->>'task_id' = $2::text
		ORDER BY created_at DESC
		LIMIT 1
	`, testUserID, taskID, inboxType).Scan(&body, &errDetail, &title)
	if err != nil {
		t.Fatalf("expected a %s notification for the task, got none: %v", inboxType, err)
	}
	return body, errDetail, title
}

func deleteInboxForTask(taskID pgtype.UUID) {
	testPool.Exec(context.Background(),
		`DELETE FROM inbox_item WHERE details->>'task_id' = $1::text`, taskID)
}

// failGetIssueByOriginDB delegates every statement to the real pool except the
// quick-create completion lookup, which it fails with a non-ErrNoRows error to
// simulate a transient DB fault on exactly that query. Everything else —
// including the inbox write the assertion depends on — still hits the real DB.
type failGetIssueByOriginDB struct {
	db.DBTX
	err error
}

func (f failGetIssueByOriginDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	// sqlc keeps the `-- name: <Query>` header in the generated SQL constant,
	// so this matches that one query and nothing else.
	if strings.Contains(sql, "name: GetIssueByOrigin") {
		return errRow{err: f.err}
	}
	return f.DBTX.QueryRow(ctx, sql, args...)
}

// cancelOnGetIssueByOriginDB cancels the caller's context at the moment of the
// origin lookup and fails it with context.Canceled — the shape a cancelled or
// timed-out request actually produces, where every later use of that same
// context is doomed too.
type cancelOnGetIssueByOriginDB struct {
	db.DBTX
	cancel context.CancelFunc
}

func (f cancelOnGetIssueByOriginDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if strings.Contains(sql, "name: GetIssueByOrigin") {
		f.cancel()
		return errRow{err: context.Canceled}
	}
	return f.DBTX.QueryRow(ctx, sql, args...)
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }
