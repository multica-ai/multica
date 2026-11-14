package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newDuplicateGuardFixture creates a create_issue autopilot with a constant
// title template, so every dispatch renders the same issue title.
func newDuplicateGuardFixture(t *testing.T, guard service.AutopilotDuplicateGuard) (*service.AutopilotService, db.Autopilot, pgtype.UUID, string) {
	t.Helper()
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	autopilotSvc.DuplicateGuard = guard

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	title := "Autopilot duplicate guard " + string(guard) + " " + time.Now().UTC().Format("20060102150405.000000000")
	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Duplicate guard " + string(guard),
		Description:        pgtype.Text{String: "Duplicate guard mode test", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "create_issue",
		IssueTitleTemplate: pgtype.Text{String: title, Valid: true},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DELETE FROM autopilot WHERE id = $1`, ap.ID)
		_, _ = testPool.Exec(bg, `DELETE FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title)
	})
	return autopilotSvc, ap, seedAutopilotScheduleTrigger(t, queries, ap), title
}

func webhookEnvelope(eventPayload, receivedAt string) []byte {
	return []byte(`{"event":"docs.update","eventPayload":` + eventPayload +
		`,"request":{"receivedAt":"` + receivedAt + `","contentType":"application/json"}}`)
}

func countIssuesWithTitle(t *testing.T, title string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, title,
	).Scan(&count); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	return count
}

func TestDispatchAutopilotPayloadGuardKeepsDistinctPayloads(t *testing.T) {
	ctx := context.Background()
	svc, ap, trigger, title := newDuplicateGuardFixture(t, service.AutopilotDuplicateGuardPayload)

	first, err := svc.DispatchAutopilot(ctx, ap, trigger, "webhook",
		webhookEnvelope(`{"project":"group/repo-a","sha":"aaa"}`, "2026-11-09T12:47:35Z"))
	if err != nil || first == nil || first.Status != "issue_created" {
		t.Fatalf("first dispatch = %+v, err %v; want issue_created", first, err)
	}
	second, err := svc.DispatchAutopilot(ctx, ap, trigger, "webhook",
		webhookEnvelope(`{"project":"group/repo-b","sha":"bbb"}`, "2026-11-09T12:47:38Z"))
	if err != nil || second == nil || second.Status != "issue_created" {
		t.Fatalf("second dispatch = %+v, err %v; want issue_created for a distinct payload", second, err)
	}
	if got := countIssuesWithTitle(t, title); got != 2 {
		t.Fatalf("distinct payloads should create 2 issues, got %d", got)
	}
}

func TestDispatchAutopilotPayloadGuardSuppressesSamePayload(t *testing.T) {
	ctx := context.Background()
	svc, ap, trigger, title := newDuplicateGuardFixture(t, service.AutopilotDuplicateGuardPayload)

	payload := `{"project":"group/repo-a","sha":"aaa"}`
	first, err := svc.DispatchAutopilot(ctx, ap, trigger, "webhook", webhookEnvelope(payload, "2026-11-09T12:47:35Z"))
	if err != nil || first == nil || first.Status != "issue_created" {
		t.Fatalf("first dispatch = %+v, err %v; want issue_created", first, err)
	}
	// Same event and payload delivered again: only the envelope's receivedAt differs.
	second, err := svc.DispatchAutopilot(ctx, ap, trigger, "webhook", webhookEnvelope(payload, "2026-11-09T12:47:36Z"))
	if err != nil || second == nil || second.Status != "skipped" {
		t.Fatalf("second dispatch = %+v, err %v; want skipped duplicate", second, err)
	}
	if got := countIssuesWithTitle(t, title); got != 1 {
		t.Fatalf("a repeated payload should leave 1 issue, got %d", got)
	}
}

func TestDispatchAutopilotDuplicateGuardOff(t *testing.T) {
	ctx := context.Background()
	svc, ap, trigger, title := newDuplicateGuardFixture(t, service.AutopilotDuplicateGuardOff)

	for i := 0; i < 2; i++ {
		run, err := svc.DispatchAutopilot(ctx, ap, trigger, "manual", nil)
		if err != nil || run == nil || run.Status != "issue_created" {
			t.Fatalf("dispatch %d = %+v, err %v; want issue_created with the guard off", i+1, run, err)
		}
	}
	if got := countIssuesWithTitle(t, title); got != 2 {
		t.Fatalf("guard off should create 2 issues, got %d", got)
	}
}
