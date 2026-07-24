package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issueevent"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Nit (a′ review): an end-to-end regression that a background task status reset —
// TriggerSideEffects=false — writes NOTHING through the real registered listeners.
//
// The producer (TaskService.broadcastIssueUpdated) is pinned to emit
// TriggerSideEffects=false by TestBroadcastIssueUpdated_* in internal/service; this
// test drives that exact listener-visible payload through the four DB-writing
// listeners and asserts none of them write. Together they cover the full chain:
// producer sets the gate, listeners honor it. Before the typed contract this skip
// was implicit (the listeners type-asserted handler.IssueResponse and a map fell
// through); now it is the explicit TriggerSideEffects gate every listener checks
// first (#4648 / MUL-3782).
func TestTaskResetPayloadTriggersNoDatabaseSideEffects(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := &service.TaskService{Queries: queries, Bus: bus}
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	// Every issue:updated DB-writing listener is live.
	registerActivityListeners(bus, queries)
	registerNotificationListeners(bus, queries)
	registerSubscriberListeners(bus, queries)
	registerAutopilotListeners(bus, autopilotSvc)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, issueID)
		cleanupInboxForIssue(t, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue_subscriber WHERE issue_id = $1`, issueID)
		cleanupTestIssue(t, issueID)
	})
	// A subscriber, so notifySubscribers WOULD write an inbox row if the gate let it.
	addTestSubscriber(t, issueID, "member", testUserID, "creator")

	// Reproduce the task-reset producer's listener-visible payload: a status change
	// (todo -> done) with side effects OFF. The `issue` wire value is not read by any
	// listener, so nil stands in for the producer's issueToMap.
	before, err := queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	after := before
	after.Status = "done"
	payload := issueevent.Build(before, after, nil, false)
	if payload.TriggerSideEffects || !payload.StatusChanged {
		t.Fatalf("fixture wrong: TriggerSideEffects=%v StatusChanged=%v", payload.TriggerSideEffects, payload.StatusChanged)
	}

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		Payload:     payload,
	})

	// None of the four DB-writing listeners wrote anything.
	if n := len(listActivitiesForIssue(t, queries, issueID)); n != 0 {
		t.Errorf("activity listener wrote %d rows for a realtime-only reset, want 0", n)
	}
	assertNoIssueRows(t, `SELECT count(*) FROM inbox_item WHERE issue_id = $1`, issueID, "inbox")
	assertNoIssueRows(t, `SELECT count(*) FROM issue_subscriber WHERE issue_id = $1 AND reason <> 'creator'`, issueID, "subscriber")
}

func assertNoIssueRows(t *testing.T, query, issueID, label string) {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), query, issueID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", label, err)
	}
	if n != 0 {
		t.Errorf("%s listener wrote %d rows for a realtime-only reset, want 0", label, n)
	}
}
