package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A real end-to-end regression for the activity-actor contract of an automated status
// change: run the hook executor with the ACTUAL activity listener registered, and
// assert the status-change activity row is written. Before the fix the executor
// published a "hook" bus actor, the listener wrote it into activity_log.actor_type,
// and the member|agent|system CHECK dropped the row while only logging an error — so
// "an automated change records activity like a manual one" was false. This asserts
// the row lands, under the normalized system actor, with the hook identity preserved
// in the details for audit.
func TestHookExecutorStatusChangeWritesActivityUnderSystemActor(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	// The hook's authorization principal must be a live workspace member.
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner') ON CONFLICT DO NOTHING`,
		testWorkspaceID, testUserID); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	issueID := createTestIssue(t, testWorkspaceID, testUserID) // status 'todo'
	hookID, revID, execID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	actions := fmt.Sprintf(`[{"type":"set_issue_status","issue_id":%q,"status":"done"}]`, issueID)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO hook (id, workspace_id, name, enabled, active_revision_id, scope_type, origin,
			creator_actor_type, creator_actor_id, authorization_principal_user_id)
		VALUES ($1, $2, 'actor contract hook', true, $3, 'workspace', 'user', 'member', $4, $4)`,
		hookID, testWorkspaceID, revID, testUserID); err != nil {
		t.Fatalf("seed hook: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO hook_revision (id, hook_id, revision, event_type, match, conditions, fire_mode, actions, created_by_type, created_by_id)
		VALUES ($1, $2, 1, 'issue.status_changed', '{}'::jsonb, '[]'::jsonb, 'per_event', $3::jsonb, 'member', $4)`,
		revID, hookID, actions, testUserID); err != nil {
		t.Fatalf("seed revision: %v", err)
	}
	var eventID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO domain_event (workspace_id, type, schema_version, subject_type, subject_id, actor_type, actor_id, payload, correlation_id, hop_count)
		VALUES ($1, 'issue.status_changed', 1, 'issue', $2, 'member', $3, '{}'::jsonb, gen_random_uuid(), 0)
		RETURNING id`, testWorkspaceID, issueID, testUserID).Scan(&eventID); err != nil {
		t.Fatalf("seed domain_event: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO hook_execution (id, workspace_id, hook_id, hook_revision_id, event_id, correlation_id, status)
		VALUES ($1, $2, $3, $4, $5, (SELECT correlation_id FROM domain_event WHERE id = $5), 'queued')`,
		execID, testWorkspaceID, hookID, revID, eventID); err != nil {
		t.Fatalf("seed execution: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM hook_action_effect WHERE execution_id = $1`, execID)
		testPool.Exec(bg, `DELETE FROM hook_execution WHERE id = $1`, execID)
		testPool.Exec(bg, `DELETE FROM hook_revision WHERE id = $1`, revID)
		testPool.Exec(bg, `DELETE FROM hook WHERE id = $1`, hookID)
		testPool.Exec(bg, `DELETE FROM domain_event WHERE subject_id = $1`, issueID)
		cleanupActivities(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	svc := service.NewHookService(queries, testPool, bus)

	// The executor claims the OLDEST queued execution on a shared DB, so drain until
	// ours reaches a terminal state.
	var status string
	for i := 0; i < 60; i++ {
		if _, err := svc.ClaimAndRun(ctx, 500); err != nil {
			t.Fatalf("claim and run: %v", err)
		}
		testPool.QueryRow(ctx, `SELECT status FROM hook_execution WHERE id = $1`, execID).Scan(&status)
		if status == "succeeded" {
			break
		}
	}
	if status != "succeeded" {
		t.Fatalf("execution status = %q after draining, want succeeded", status)
	}
	// Listeners run synchronously on the in-memory bus.
	time.Sleep(50 * time.Millisecond)

	// The status change actually landed on the issue.
	var issueStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&issueStatus); err != nil {
		t.Fatal(err)
	}
	if issueStatus != "done" {
		t.Fatalf("issue status = %q, want done", issueStatus)
	}

	// The activity row was written (not swallowed by the actor_type CHECK), under the
	// system actor, with the hook identity preserved in the details.
	activities := listActivitiesForIssue(t, queries, issueID)
	var statusActivity *db.ActivityLog
	for i := range activities {
		if activities[i].Action == "status_changed" {
			statusActivity = &activities[i]
		}
	}
	if statusActivity == nil {
		t.Fatal("no status_changed activity was written for the automated change — " +
			"the hook actor hit the activity_log actor_type CHECK and the row was dropped")
	}
	if statusActivity.ActorType.String != "system" {
		t.Errorf("activity actor_type = %q, want system", statusActivity.ActorType.String)
	}
	var details map[string]string
	if err := json.Unmarshal(statusActivity.Details, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if details["from"] != "todo" || details["to"] != "done" {
		t.Errorf("details from/to = %q/%q, want todo/done", details["from"], details["to"])
	}
	if details["automation_source"] != "hook" {
		t.Errorf("details automation_source = %q, want hook", details["automation_source"])
	}
	if details["automation_id"] != hookID {
		t.Errorf("details automation_id = %q, want the firing hook %q", details["automation_id"], hookID)
	}
}
