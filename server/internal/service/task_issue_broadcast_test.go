package service

import (
	"context"
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
	payload, ok := got[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map[string]any: %T", got[0].Payload)
	}
	if payload["status_changed"] != false {
		t.Errorf("expected status_changed=false, got %v", payload["status_changed"])
	}
}

// TestBroadcastTaskFailedEvent pins the task:failed payload contract the
// channel outbounds rely on: `error` mirrors the transcript's failure message
// and is omitted while an auto-retry is pending (the transcript suppresses the
// failure then too — the retry reports its own outcome), so error-present
// means deliverable and subscribers stay consistent with the web chat panel
// without retry logic of their own.
func TestBroadcastTaskFailedEvent_CarriesErrorAndRetryPending(t *testing.T) {
	pool := newTaskClaimRacePool(t)
	ctx := context.Background()
	_, _, agentID, sessionID := createQuickCreateReplyFixture(t, ctx, pool)

	bus := events.New()
	var got []events.Event
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) { got = append(got, e) })

	svc := &TaskService{Queries: db.New(pool), Bus: bus}
	task := db.AgentTaskQueue{
		ID:            testUUID(11),
		AgentID:       util.MustParseUUID(agentID),
		ChatSessionID: util.MustParseUUID(sessionID),
		Status:        "failed",
	}
	svc.broadcastTaskFailedEvent(ctx, task, "task timed out", "timeout", false)
	svc.broadcastTaskFailedEvent(ctx, task, "task timed out", "timeout", true)

	if len(got) != 2 {
		t.Fatalf("events = %d, want 2", len(got))
	}

	terminal, ok := got[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map[string]any: %T", got[0].Payload)
	}
	if terminal["error"] != "task timed out" {
		t.Errorf("error = %v, want %q", terminal["error"], "task timed out")
	}
	if terminal["retry_pending"] != false {
		t.Errorf("retry_pending = %v, want false", terminal["retry_pending"])
	}
	if terminal["failure_reason"] != "timeout" {
		t.Errorf("failure_reason = %v, want %q", terminal["failure_reason"], "timeout")
	}
	if terminal["chat_session_id"] != sessionID {
		t.Errorf("chat_session_id = %v, want %q", terminal["chat_session_id"], sessionID)
	}
	// Envelope scope hints let subscribers route without digging the payload.
	if got[0].ChatSessionID != sessionID {
		t.Errorf("envelope ChatSessionID = %q, want %q", got[0].ChatSessionID, sessionID)
	}
	if got[0].TaskID != util.UUIDToString(task.ID) {
		t.Errorf("envelope TaskID = %q, want %q", got[0].TaskID, util.UUIDToString(task.ID))
	}

	retrying, ok := got[1].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map[string]any: %T", got[1].Payload)
	}
	if _, present := retrying["error"]; present {
		t.Errorf("error = %v, want omitted while a retry is pending", retrying["error"])
	}
	if retrying["retry_pending"] != true {
		t.Errorf("retry_pending = %v, want true", retrying["retry_pending"])
	}
}
