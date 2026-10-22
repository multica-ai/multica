package handler

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Migration 467 fills a legacy trigger's missing creator from its autopilot's
// creator, so schedule/webhook dispatch has a principal again instead of
// skipping every run (#8284).
//
// The shipped SQL runs twice inside a transaction that is rolled back: the
// UPDATE is table-wide, and committing it would rewrite the NULL-creator
// triggers that other packages' tests build concurrently in the same database.
func TestMigration467BackfillsTriggerCreatorFromAutopilot(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	otherMemberID := dbfx.User(t, "Backfill Other Member", "mul7267-member@multica.test")
	dbfx.Member(t, testWorkspaceID, otherMemberID, "member")
	departedID := dbfx.User(t, "Backfill Departed Member", "mul7267-departed@multica.test")
	agentID := dbfx.Agent(t, "Backfill Agent", handlerTestRuntimeID(t))

	autopilot := func(creatorID string) string {
		return dbfx.Insert(t, "autopilot", testutil.Cols{
			"workspace_id":    testWorkspaceID,
			"title":           "Trigger creator backfill",
			"assignee_type":   "agent",
			"assignee_id":     agentID,
			"status":          "active",
			"execution_mode":  "run_only",
			"created_by_type": "member",
			"created_by_id":   creatorID,
		})
	}
	scheduleTrigger := func(autopilotID string, createdByType, createdByID any) string {
		return dbfx.Insert(t, "autopilot_trigger", testutil.Cols{
			"autopilot_id":    autopilotID,
			"kind":            "schedule",
			"enabled":         true,
			"cron_expression": "0 9 * * *",
			"created_by_type": createdByType,
			"created_by_id":   createdByID,
		})
	}

	liveAutopilotID := autopilot(testUserID)
	token, err := generateWebhookToken()
	if err != nil {
		t.Fatal(err)
	}
	// The #8284 shape: created before published_by existed and never edited, so
	// migration 449 had nothing to backfill created_by from.
	legacyWebhookID := dbfx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id":  liveAutopilotID,
		"kind":          "webhook",
		"enabled":       true,
		"webhook_token": token,
	})
	agentCreatedID := scheduleTrigger(liveAutopilotID, "agent", agentID)
	memberCreatedID := scheduleTrigger(liveAutopilotID, "member", otherMemberID)
	departedCreatorID := scheduleTrigger(autopilot(departedID), nil, nil)

	w := postWebhook(t, token, map[string]any{"ref": "refs/heads/main"}, map[string]string{"X-GitHub-Event": "push"})
	var before map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &before); err != nil {
		t.Fatalf("decode pre-backfill response: %v", err)
	}
	if before["status"] != "skipped" {
		t.Fatalf("pre-backfill webhook = %v, want skipped: the legacy trigger has no principal yet", before)
	}

	migration, err := os.ReadFile("../../migrations/467_autopilot_trigger_creator_from_autopilot.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for range 2 {
		if _, err := tx.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	creatorOf := func(triggerID string) (createdByType, createdByID *string) {
		t.Helper()
		if err := tx.QueryRow(ctx,
			`SELECT created_by_type, created_by_id::text FROM autopilot_trigger WHERE id = $1`, triggerID,
		).Scan(&createdByType, &createdByID); err != nil {
			t.Fatal(err)
		}
		return createdByType, createdByID
	}
	for _, tc := range []struct {
		name, triggerID string
		wantType        *string
		wantID          *string
	}{
		{"no creator takes the autopilot creator", legacyWebhookID, ptr("member"), ptr(testUserID)},
		{"agent creator takes the autopilot creator", agentCreatedID, ptr("member"), ptr(testUserID)},
		{"member creator is never rewritten", memberCreatedID, ptr("member"), ptr(otherMemberID)},
		{"departed autopilot creator is not written in", departedCreatorID, nil, nil},
	} {
		gotType, gotID := creatorOf(tc.triggerID)
		if orNULL(gotType) != orNULL(tc.wantType) || orNULL(gotID) != orNULL(tc.wantID) {
			t.Errorf("%s: created_by = %s/%s, want %s/%s",
				tc.name, orNULL(gotType), orNULL(gotID), orNULL(tc.wantType), orNULL(tc.wantID))
		}
	}
	filledType, filledID := creatorOf(legacyWebhookID)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// Commit the value the migration computed for this trigger alone: the next
	// delivery must run instead of being skipped.
	dbfx.Exec(t, `UPDATE autopilot_trigger SET created_by_type = $2, created_by_id = $3 WHERE id = $1`,
		legacyWebhookID, filledType, filledID)
	w = postWebhook(t, token, map[string]any{"ref": "refs/heads/main"}, map[string]string{"X-GitHub-Event": "push"})
	delivery := processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w))
	run, err := testHandler.Queries.GetAutopilotRun(ctx, delivery.AutopilotRunID)
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != "running" || !run.TaskID.Valid {
		t.Fatalf("post-backfill run status=%s task=%v failure=%q, want running with a task",
			run.Status, run.TaskID.Valid, run.FailureReason.String)
	}
}

func orNULL(s *string) string {
	if s == nil {
		return "NULL"
	}
	return *s
}
