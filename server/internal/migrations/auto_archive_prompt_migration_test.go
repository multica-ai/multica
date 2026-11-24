package migrations

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAutoArchivePromptAsksOnlyMembersWhoArchived(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	defer pool.Close()

	schema := "auto_archive_prompt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}

	const (
		ws        = "11111111-1111-1111-1111-111111111111"
		otherWS   = "22222222-2222-2222-2222-222222222222"
		archiver  = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		chooser   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
		reader    = "cccccccc-cccc-cccc-cccc-cccccccccccc"
		formerMem = "dddddddd-dddd-dddd-dddd-dddddddddddd"
		agent     = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	)
	if _, err := conn.Exec(ctx, `
		CREATE TABLE member (
			workspace_id UUID NOT NULL,
			user_id UUID NOT NULL,
			UNIQUE (workspace_id, user_id)
		);
		CREATE TABLE inbox_item (
			workspace_id UUID NOT NULL,
			recipient_type TEXT NOT NULL,
			recipient_id UUID NOT NULL,
			archived BOOLEAN NOT NULL DEFAULT FALSE
		);
		CREATE TABLE notification_preference (
			workspace_id UUID NOT NULL,
			user_id UUID NOT NULL,
			preferences JSONB NOT NULL DEFAULT '{}',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (workspace_id, user_id)
		);
	`); err != nil {
		t.Fatalf("create fixture tables: %v", err)
	}
	seed := strings.NewReplacer(
		"$ws", ws, "$other", otherWS, "$archiver", archiver, "$chooser", chooser,
		"$reader", reader, "$former", formerMem, "$agent", agent,
	).Replace(`
		INSERT INTO member (workspace_id, user_id) VALUES
			('$ws', '$archiver'), ('$ws', '$chooser'), ('$ws', '$reader'), ('$other', '$archiver');
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, archived) VALUES
			('$ws', 'member', '$archiver', TRUE),
			('$other', 'member', '$archiver', FALSE),
			('$ws', 'member', '$chooser', TRUE),
			('$ws', 'member', '$reader', FALSE),
			('$ws', 'member', '$former', TRUE),
			('$ws', 'agent', '$agent', TRUE);
		INSERT INTO notification_preference (workspace_id, user_id, preferences) VALUES
			('$ws', '$chooser', '{"auto_archive_completed": "muted", "auto_archive_on_comment": "all", "updates": "muted"}');
	`)
	if _, err := conn.Exec(ctx, seed); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	if _, err := conn.Exec(ctx, readMigrationFile(t, "718_auto_archive_prompt.up.sql")); err != nil {
		t.Fatalf("apply 718 up: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigrationFile(t, "722_auto_archive_agent_trigger_prompt.up.sql")); err != nil {
		t.Fatalf("apply 722 up: %v", err)
	}

	stored := func() map[string]map[string]string {
		t.Helper()
		rows, err := conn.Query(ctx, `SELECT workspace_id::text || '/' || user_id::text, preferences FROM notification_preference`)
		if err != nil {
			t.Fatalf("load preferences: %v", err)
		}
		defer rows.Close()
		result := map[string]map[string]string{}
		for rows.Next() {
			var key string
			var raw []byte
			if err := rows.Scan(&key, &raw); err != nil {
				t.Fatalf("scan preference: %v", err)
			}
			var prefs map[string]string
			if err := json.Unmarshal(raw, &prefs); err != nil {
				t.Fatalf("decode preference: %v", err)
			}
			result[key] = prefs
		}
		return result
	}

	got := stored()
	want := map[string]map[string]string{
		ws + "/" + archiver: {
			"auto_archive_on_agent_trigger": "muted",
			"auto_archive_on_comment":       "muted",
			"auto_archive_on_status":        "muted",
			"auto_archive_on_assignee":      "muted",
			"auto_archive_prompt":           "pending",
		},
		ws + "/" + chooser: {
			"auto_archive_completed":        "muted",
			"auto_archive_on_agent_trigger": "muted",
			"auto_archive_on_comment":       "all",
			"auto_archive_on_status":        "muted",
			"auto_archive_on_assignee":      "muted",
			"auto_archive_prompt":           "pending",
			"updates":                       "muted",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("preference rows = %v, want only %v", got, want)
	}
	for key, prefs := range want {
		for k, v := range prefs {
			if got[key][k] != v {
				t.Errorf("%s %s = %q, want %q (row %v)", key, k, got[key][k], v, got[key])
			}
		}
		if len(got[key]) != len(prefs) {
			t.Errorf("%s = %v, want %v", key, got[key], prefs)
		}
	}

	if _, err := conn.Exec(ctx, readMigrationFile(t, "722_auto_archive_agent_trigger_prompt.down.sql")); err != nil {
		t.Fatalf("apply 722 down: %v", err)
	}
	for key, prefs := range stored() {
		if _, ok := prefs["auto_archive_on_agent_trigger"]; ok {
			t.Errorf("%s still has auto_archive_on_agent_trigger after 722 down: %v", key, prefs)
		}
	}

	if _, err := conn.Exec(ctx, readMigrationFile(t, "718_auto_archive_prompt.down.sql")); err != nil {
		t.Fatalf("apply 718 down: %v", err)
	}
	for key, prefs := range stored() {
		if _, ok := prefs["auto_archive_prompt"]; ok {
			t.Errorf("%s still has the prompt after down: %v", key, prefs)
		}
	}
}
