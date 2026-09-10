package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/events"
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

// TestBroadcastIssueUpdated_EmitsStatusChange pins both sides of the additive
// lifecycle event contract: legacy clients still receive issue:updated, while
// status-specific consumers receive issue:transitioned with the same payload.
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
	svc.broadcastIssueUpdated(context.Background(), issue, "in_progress")

	if len(got) != 2 {
		t.Fatalf("expected exactly 2 published events, got %d", len(got))
	}
	e := got[0]
	if e.Type != protocol.EventIssueUpdated {
		t.Fatalf("expected event type %q, got %q", protocol.EventIssueUpdated, e.Type)
	}
	if e.WorkspaceID != util.UUIDToString(issue.WorkspaceID) {
		t.Fatalf("workspace mismatch: got %q want %q", e.WorkspaceID, util.UUIDToString(issue.WorkspaceID))
	}

	payload, ok := e.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map[string]any: %T", e.Payload)
	}
	if payload["status_changed"] != true {
		t.Errorf("expected status_changed=true, got %v", payload["status_changed"])
	}
	if payload["prev_status"] != "in_progress" {
		t.Errorf("expected prev_status=in_progress, got %v", payload["prev_status"])
	}
	issueMap, ok := payload["issue"].(map[string]any)
	if !ok {
		t.Fatalf("issue payload is not map[string]any: %T", payload["issue"])
	}
	if issueMap["status"] != "todo" {
		t.Errorf("expected issue.status=todo, got %v", issueMap["status"])
	}
	if issueMap["id"] != util.UUIDToString(issue.ID) {
		t.Errorf("issue.id mismatch: got %v want %q", issueMap["id"], util.UUIDToString(issue.ID))
	}
	transitioned := got[1]
	if transitioned.Type != protocol.EventIssueTransitioned {
		t.Fatalf("expected event type %q, got %q", protocol.EventIssueTransitioned, transitioned.Type)
	}
	if !reflect.DeepEqual(transitioned.Payload, e.Payload) {
		t.Fatal("expected legacy and transition events to share the canonical payload")
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
	svc.broadcastIssueUpdated(context.Background(), issue, "todo")

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 published event, got %d", len(got))
	}
	payload, ok := got[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map[string]any: %T", got[0].Payload)
	}
	if payload["status_changed"] != false {
		t.Errorf("expected status_changed=false, got %v", payload["status_changed"])
	}
}
