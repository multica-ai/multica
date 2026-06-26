package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestRuntimeProviderFailureMonitor_AlertsOwnerOncePerLookback(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()
	startedAt := time.Now().Add(-1 * time.Second)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			DELETE FROM inbox_item
			WHERE workspace_id = $1
			  AND type = 'runtime_provider_capacity_alert'
			  AND created_at >= $2
		`, testWorkspaceID, startedAt)
	})

	for i := 0; i < 4; i++ {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, issue_id, status, priority,
				started_at, completed_at, created_at, failure_reason, error
			)
			VALUES ($1, $2, $3, 'failed', 0, now(), now(), now(), $4, $5)
		`, agentID, runtimeID, issueID,
			taskfailure.ReasonAgentProviderCapacityOrRateLimit.String(),
			fmt.Sprintf("provider capacity monitor fixture %d", i),
		); err != nil {
			t.Fatalf("seed provider-capacity task: %v", err)
		}
	}

	queries := db.New(testPool)
	bus := events.New()
	cfg := runtimeProviderFailureMonitorConfig{
		Interval:     time.Hour,
		Lookback:     24 * time.Hour,
		Threshold:    3,
		StartupDelay: 0,
	}

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	tickRuntimeProviderFailureMonitor(ctx, queries, bus, cfg)

	if len(inboxEvents) != 1 {
		t.Fatalf("expected 1 inbox:new event, got %d", len(inboxEvents))
	}
	item := inboxEvents[0].Payload.(map[string]any)["item"].(map[string]any)
	if got := item["type"]; got != "runtime_provider_capacity_alert" {
		t.Fatalf("expected inbox type runtime_provider_capacity_alert, got %v", got)
	}
	if got := item["severity"]; got != "attention" {
		t.Fatalf("expected severity attention, got %v", got)
	}
	if got := item["recipient_id"]; got != testUserID {
		t.Fatalf("expected recipient %s, got %v", testUserID, got)
	}

	tickRuntimeProviderFailureMonitor(ctx, queries, bus, cfg)

	if len(inboxEvents) != 1 {
		t.Fatalf("duplicate tick should not re-alert inside lookback, got %d events", len(inboxEvents))
	}
}
