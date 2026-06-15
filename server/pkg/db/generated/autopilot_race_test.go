package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUpdateAutopilotTriggerPreservesConcurrentAdvance(t *testing.T) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("database not reachable: %v", err)
	}

	queries := New(pool)
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("autopilot-race-%d@multica.ai", suffix)
	slug := fmt.Sprintf("autopilot-race-%d", suffix)

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id::text
	`, "Autopilot Race User", email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text
	`, "Autopilot Race Workspace", slug, "autopilot trigger race regression", "ATR").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID); err != nil {
			t.Logf("cleanup workspace: %v", err)
		}
		if _, err := pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
		RETURNING id::text
	`, workspaceID, "Autopilot Race Runtime", "db_test_runtime", "Autopilot race runtime").Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id::text
	`, workspaceID, "Autopilot Race Agent", runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	autopilot, err := queries.CreateAutopilot(ctx, CreateAutopilotParams{
		WorkspaceID:        mustUUID(t, workspaceID),
		Title:              "Autopilot trigger race regression",
		AssigneeType:       "agent",
		AssigneeID:         mustUUID(t, agentID),
		Status:             "active",
		ExecutionMode:      "run_only",
		CreatedByType:      "member",
		CreatedByID:        mustUUID(t, userID),
		Description:        pgtype.Text{String: "regression test", Valid: true},
		IssueTitleTemplate: pgtype.Text{},
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}

	initialNextRun := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	trigger, err := queries.CreateAutopilotTrigger(ctx, CreateAutopilotTriggerParams{
		AutopilotID:    autopilot.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: "2 * * * *", Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
		NextRunAt:      pgtype.Timestamptz{Time: initialNextRun, Valid: true},
		WebhookToken:   pgtype.Text{},
		Label:          pgtype.Text{String: "before-race", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE autopilot_trigger SET next_run_at = NULL WHERE id = $1`, trigger.ID); err != nil {
		t.Fatalf("claim trigger: %v", err)
	}

	prev, err := queries.GetAutopilotTrigger(ctx, trigger.ID)
	if err != nil {
		t.Fatalf("GetAutopilotTrigger: %v", err)
	}
	if prev.NextRunAt.Valid {
		t.Fatalf("expected claimed trigger to have NULL next_run_at, got %v", prev.NextRunAt.Time)
	}

	advancedNextRun := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Microsecond)
	if err := queries.AdvanceTriggerNextRun(ctx, AdvanceTriggerNextRunParams{
		ID:        trigger.ID,
		NextRunAt: pgtype.Timestamptz{Time: advancedNextRun, Valid: true},
	}); err != nil {
		t.Fatalf("AdvanceTriggerNextRun: %v", err)
	}

	updated, err := queries.UpdateAutopilotTrigger(ctx, UpdateAutopilotTriggerParams{
		ID:             prev.ID,
		CronExpression: prev.CronExpression,
		Timezone:       prev.Timezone,
		NextRunAt:      prev.NextRunAt,
		Label:          pgtype.Text{String: "after-race", Valid: true},
	})
	if err != nil {
		t.Fatalf("UpdateAutopilotTrigger: %v", err)
	}

	if !updated.NextRunAt.Valid {
		t.Fatal("expected concurrent advance to preserve next_run_at, got NULL")
	}
	if !updated.NextRunAt.Time.UTC().Equal(advancedNextRun) {
		t.Fatalf("expected next_run_at %s, got %s", advancedNextRun.Format(time.RFC3339Nano), updated.NextRunAt.Time.UTC().Format(time.RFC3339Nano))
	}
	if updated.Label.String != "after-race" {
		t.Fatalf("expected label update to persist, got %q", updated.Label.String)
	}
}

func mustUUID(t *testing.T, raw string) pgtype.UUID {
	t.Helper()

	parsed, err := uuid.Parse(raw)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", raw, err)
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}
}
