package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issueevent"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// noRowsDBTX makes every read return pgx.ErrNoRows so getIssuePrefix's
// GetWorkspace lookup falls back to an empty prefix without needing a DB. The
// helper under test still publishes regardless of the prefix result.
type noRowsDBTX struct{}

func (noRowsDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}
func (noRowsDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, pgx.ErrNoRows
}
func (noRowsDBTX) QueryRow(context.Context, string, ...any) pgx.Row { return noRow{} }

type noRow struct{}

func (noRow) Scan(...any) error { return pgx.ErrNoRows }

// TestBroadcastIssueUpdated_EmitsStatusChange pins the realtime contract behind
// #4648 / MUL-3782: when a background path resets an issue's status (e.g. the
// failed-task handler flipping a stuck in_progress issue back to todo), it must
// publish issue:updated with status_changed=true and the new status so the
// frontend's onIssueUpdated reconcile moves the card between status columns /
// filters instead of leaving it stale until the next unrelated write.
func TestBroadcastIssueUpdated_EmitsStatusChange(t *testing.T) {
	bus := events.New()
	var got []events.Event
	bus.SubscribeAll(func(e events.Event) { got = append(got, e) })

	svc := &TaskService{
		Queries: db.New(noRowsDBTX{}),
		Bus:     bus,
	}

	issue := db.Issue{
		ID:          testUUID(1),
		WorkspaceID: testUUID(2),
		Number:      7,
		Status:      "todo",
	}
	svc.broadcastIssueUpdated(issue, "in_progress")

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 published event, got %d", len(got))
	}
	e := got[0]
	if e.Type != protocol.EventIssueUpdated {
		t.Fatalf("expected event type %q, got %q", protocol.EventIssueUpdated, e.Type)
	}
	if e.WorkspaceID != util.UUIDToString(issue.WorkspaceID) {
		t.Fatalf("workspace mismatch: got %q want %q", e.WorkspaceID, util.UUIDToString(issue.WorkspaceID))
	}

	payload, ok := e.Payload.(issueevent.IssueUpdatedPayload)
	if !ok {
		t.Fatalf("payload is not issueevent.IssueUpdatedPayload: %T", e.Payload)
	}
	if !payload.StatusChanged {
		t.Errorf("expected StatusChanged=true, got false")
	}
	if payload.PrevStatus != "in_progress" {
		t.Errorf("expected PrevStatus=in_progress, got %v", payload.PrevStatus)
	}
	// A background reset is realtime-only: the side-effect listeners must skip it.
	if payload.TriggerSideEffects {
		t.Error("a background status reset must not trigger activity / inbox side effects")
	}
	// The client wire keeps the issueToMap representation unchanged.
	issueMap, ok := payload.Issue.(map[string]any)
	if !ok {
		t.Fatalf("issue payload is not map[string]any: %T", payload.Issue)
	}
	if issueMap["status"] != "todo" {
		t.Errorf("expected issue.status=todo, got %v", issueMap["status"])
	}
	if issueMap["id"] != util.UUIDToString(issue.ID) {
		t.Errorf("issue.id mismatch: got %v want %q", issueMap["id"], util.UUIDToString(issue.ID))
	}
}

// TestBroadcastIssueUpdated_NoStatusChange guards the gate: a same-status
// broadcast reports status_changed=false so the client skips the status-bucket
// reconcile for non-status field updates.
func TestBroadcastIssueUpdated_NoStatusChange(t *testing.T) {
	bus := events.New()
	var got []events.Event
	bus.SubscribeAll(func(e events.Event) { got = append(got, e) })

	svc := &TaskService{
		Queries: db.New(noRowsDBTX{}),
		Bus:     bus,
	}

	issue := db.Issue{
		ID:          testUUID(1),
		WorkspaceID: testUUID(2),
		Status:      "todo",
	}
	svc.broadcastIssueUpdated(issue, "todo")

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 published event, got %d", len(got))
	}
	payload, ok := got[0].Payload.(issueevent.IssueUpdatedPayload)
	if !ok {
		t.Fatalf("payload is not issueevent.IssueUpdatedPayload: %T", got[0].Payload)
	}
	if payload.StatusChanged {
		t.Errorf("expected StatusChanged=false, got true")
	}
}
