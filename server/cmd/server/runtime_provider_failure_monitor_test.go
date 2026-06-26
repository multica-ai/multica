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

	workspaceID, ownerID, issueID, agentID, runtimeID := setupRuntimeProviderFailureMonitorFixture(t)

	ctx := context.Background()
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
	if got := item["recipient_id"]; got != ownerID {
		t.Fatalf("expected recipient %s, got %v", ownerID, got)
	}
	if got := item["workspace_id"]; got != workspaceID {
		t.Fatalf("expected workspace %s, got %v", workspaceID, got)
	}

	tickRuntimeProviderFailureMonitor(ctx, queries, bus, cfg)

	if len(inboxEvents) != 1 {
		t.Fatalf("duplicate tick should not re-alert inside lookback, got %d events", len(inboxEvents))
	}
}

func setupRuntimeProviderFailureMonitorFixture(t *testing.T) (workspaceID, ownerID, issueID, agentID, runtimeID string) {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("runtime-provider-monitor-%d@multica.test", suffix)
	slug := fmt.Sprintf("runtime-provider-monitor-%d", suffix)

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		testPool.Exec(cleanupCtx, `DELETE FROM workspace WHERE slug = $1`, slug)
		testPool.Exec(cleanupCtx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Runtime Provider Monitor Owner', $1)
		RETURNING id::text
	`, email).Scan(&ownerID); err != nil {
		t.Fatalf("create monitor owner: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ('Runtime Provider Monitor Test', $1, 'isolated runtime provider failure monitor fixture')
		RETURNING id::text
	`, slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create monitor workspace: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, ownerID); err != nil {
		t.Fatalf("create monitor member: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, NULL, 'Runtime Provider Monitor Runtime', 'cloud',
			'runtime_provider_monitor_test', 'online', 'test runtime', '{}'::jsonb, now(), 'private', $2)
		RETURNING id::text
	`, workspaceID, ownerID).Scan(&runtimeID); err != nil {
		t.Fatalf("create monitor runtime: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Runtime Provider Monitor Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3)
		RETURNING id::text
	`, workspaceID, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create monitor agent: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority,
			creator_type, creator_id, assignee_type, assignee_id, number
		)
		VALUES ($1, 'Runtime provider monitor issue', 'todo', 'none',
			'member', $2, 'agent', $3, 1)
		RETURNING id::text
	`, workspaceID, ownerID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create monitor issue: %v", err)
	}

	return workspaceID, ownerID, issueID, agentID, runtimeID
}
