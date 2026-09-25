package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const dingtalkRoutingRemovalTestSchema = "dingtalk_routing_removal_test"

func TestDingTalkGroupRoutingRemovalMigrations(t *testing.T) {
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

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire Postgres connection: %v", err)
	}
	defer conn.Release()

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+dingtalkRoutingRemovalTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+dingtalkRoutingRemovalTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, dingtalkRoutingRemovalTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		CREATE TABLE channel_installation (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			channel_type TEXT NOT NULL,
			agent_id UUID NOT NULL,
			config JSONB NOT NULL DEFAULT '{}'::jsonb,
			status TEXT NOT NULL DEFAULT 'active',
			installed_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE chat_session (
			id UUID PRIMARY KEY,
			agent_id UUID
		);
		CREATE TABLE channel_chat_session_binding (
			id UUID PRIMARY KEY,
			chat_session_id UUID NOT NULL,
			installation_id UUID NOT NULL,
			channel_type TEXT NOT NULL,
			channel_chat_id TEXT NOT NULL,
			chat_type TEXT NOT NULL
		);
		CREATE TABLE channel_outbound_card_message (
			id UUID PRIMARY KEY,
			chat_session_id UUID NOT NULL
		);
	`); err != nil {
		t.Fatalf("create removal fixtures: %v", err)
	}

	for _, name := range []string{
		"304_dingtalk_group_route.up.sql",
		"305_dingtalk_group_route_installation_conversation_unique.up.sql",
		"306_dingtalk_group_route_workspace_index.up.sql",
		"307_dingtalk_group_route_id_unique.up.sql",
	} {
		applyMigrationFile(t, ctx, conn.Conn(), name)
	}

	const (
		workspaceID  = "d3480000-0000-4000-8000-000000000000"
		defaultAgent = "d3480000-0000-4000-8000-000000000001"
		otherAgent   = "d3480000-0000-4000-8000-000000000002"
		dingtalkInst = "d3480000-0000-4000-8000-000000000003"
		slackInst    = "d3480000-0000-4000-8000-000000000004"
		defaultGroup = "d3480000-0000-4000-8000-000000000005"
		staleGroup   = "d3480000-0000-4000-8000-000000000006"
		staleP2P     = "d3480000-0000-4000-8000-000000000007"
		otherChannel = "d3480000-0000-4000-8000-000000000008"
	)
	if _, err := conn.Exec(ctx, `
		INSERT INTO channel_installation (id, workspace_id, channel_type, agent_id, config) VALUES
			($1, $4, 'dingtalk', $3, '{"group_bot_names":{"stale-group":"Migration Bot"}}'),
			($2, $4, 'slack', $3, '{}')
	`, dingtalkInst, slackInst, defaultAgent, workspaceID); err != nil {
		t.Fatalf("seed installations: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO chat_session (id, agent_id) VALUES
			($1, $2),
			($3, $4),
			($5, $4),
			($6, $4)
	`, defaultGroup, defaultAgent, staleGroup, otherAgent, staleP2P, otherChannel); err != nil {
		t.Fatalf("seed chat sessions: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO dingtalk_group_route (
			workspace_id, installation_id, conversation_id, conversation_title, agent_id, discovered_at
		) VALUES
			($1, $2, 'default-group', 'Default group', $3, '2026-01-01T00:00:00Z'),
			($1, $2, 'stale-group', 'Stale group', $4, '2026-02-01T00:00:00Z')
	`, workspaceID, dingtalkInst, defaultAgent, otherAgent); err != nil {
		t.Fatalf("seed legacy routes: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO channel_chat_session_binding (
			id, chat_session_id, installation_id, channel_type, channel_chat_id, chat_type
		) VALUES
			(gen_random_uuid(), $3, $1, 'dingtalk', 'default-group', 'group'),
			(gen_random_uuid(), $4, $1, 'dingtalk', 'stale-group', 'group'),
			(gen_random_uuid(), $5, $1, 'dingtalk', 'stale-p2p', 'p2p'),
			(gen_random_uuid(), $6, $2, 'slack', 'other-group', 'group')
	`, dingtalkInst, slackInst, defaultGroup, staleGroup, staleP2P, otherChannel); err != nil {
		t.Fatalf("seed chat bindings: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO channel_outbound_card_message (id, chat_session_id) VALUES
			(gen_random_uuid(), $1),
			(gen_random_uuid(), $2),
			(gen_random_uuid(), $3),
			(gen_random_uuid(), $4)
	`, defaultGroup, staleGroup, staleP2P, otherChannel); err != nil {
		t.Fatalf("seed outbound cards: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "382_remove_dingtalk_group_routing_bindings.up.sql")

	assertMigrationRowCount(t, ctx, conn, "channel_chat_session_binding", 3)
	assertMigrationRowCount(t, ctx, conn, "channel_outbound_card_message", 3)
	var staleBindingExists, staleCardExists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM channel_chat_session_binding WHERE chat_session_id = $1
	)`, staleGroup).Scan(&staleBindingExists); err != nil {
		t.Fatalf("inspect stale group binding: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM channel_outbound_card_message WHERE chat_session_id = $1
	)`, staleGroup).Scan(&staleCardExists); err != nil {
		t.Fatalf("inspect stale group card: %v", err)
	}
	if staleBindingExists || staleCardExists {
		t.Fatalf("stale group state remains: binding=%t card=%t", staleBindingExists, staleCardExists)
	}

	for _, name := range []string{
		"383_create_dingtalk_group_presence.up.sql",
		"384_create_dingtalk_group_presence_identity_index.up.sql",
		"385_create_dingtalk_group_presence_activity_index.up.sql",
		"386_backfill_dingtalk_group_presence.up.sql",
		"387_create_dingtalk_bot_identity.up.sql",
		"388_create_dingtalk_bot_identity_installation_index.up.sql",
		"389_backfill_dingtalk_bot_identity.up.sql",
	} {
		applyMigrationFile(t, ctx, conn.Conn(), name)
	}
	var routeTableExists, presenceTableExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, dingtalkRoutingRemovalTestSchema+".dingtalk_group_route").Scan(&routeTableExists); err != nil {
		t.Fatalf("inspect retained route table: %v", err)
	}
	if !routeTableExists {
		t.Fatal("legacy route table was removed during the compatibility window")
	}
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, dingtalkRoutingRemovalTestSchema+".dingtalk_group_presence").Scan(&presenceTableExists); err != nil {
		t.Fatalf("inspect presence table: %v", err)
	}
	if !presenceTableExists {
		t.Fatal("dingtalk_group_presence was not created")
	}
	var identityTableExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, dingtalkRoutingRemovalTestSchema+".dingtalk_bot_identity").Scan(&identityTableExists); err != nil {
		t.Fatalf("inspect Bot identity table: %v", err)
	}
	if !identityTableExists {
		t.Fatal("dingtalk_bot_identity was not created")
	}
	assertMigrationRowCount(t, ctx, conn, "dingtalk_group_presence", 2)
	assertMigrationRowCount(t, ctx, conn, "dingtalk_bot_identity", 1)
	var title string
	var lastActiveAt *string
	var mentionCount int64
	if err := conn.QueryRow(ctx, `
		SELECT conversation_title, last_active_at::text, mention_count
		FROM dingtalk_group_presence
		WHERE installation_id = $1 AND conversation_id = 'stale-group'
	`, dingtalkInst).Scan(&title, &lastActiveAt, &mentionCount); err != nil {
		t.Fatalf("inspect backfilled presence: %v", err)
	}
	if title != "Stale group" || lastActiveAt != nil || mentionCount != 0 {
		t.Fatalf("backfill = title %q, active %v, mentions %d", title, lastActiveAt, mentionCount)
	}
	var botName string
	if err := conn.QueryRow(ctx, `
		SELECT bot_name FROM dingtalk_bot_identity WHERE installation_id = $1
	`, dingtalkInst).Scan(&botName); err != nil {
		t.Fatalf("inspect backfilled Bot identity: %v", err)
	}
	if botName != "Migration Bot" {
		t.Fatalf("backfilled Bot name = %q, want Migration Bot", botName)
	}

	// An older process keeps writing the route table after the backfill. The
	// compatibility trigger must mirror discoveries without interpreting an
	// admin route revision as message activity.
	if _, err := conn.Exec(ctx, `
		INSERT INTO dingtalk_group_route (
			workspace_id, installation_id, conversation_id, conversation_title, agent_id
		) VALUES ($1, $2, 'mixed-rollout', 'Mixed rollout', $3)
	`, workspaceID, dingtalkInst, defaultAgent); err != nil {
		t.Fatalf("simulate old-process discovery: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		SELECT last_active_at IS NOT NULL, mention_count
		FROM dingtalk_group_presence
		WHERE installation_id = $1 AND conversation_id = 'mixed-rollout'
	`, dingtalkInst).Scan(&presenceTableExists, &mentionCount); err != nil {
		t.Fatalf("inspect mirrored discovery: %v", err)
	}
	if !presenceTableExists || mentionCount != 1 {
		t.Fatalf("mirrored discovery activity = %t/%d, want true/1", presenceTableExists, mentionCount)
	}
	if _, err := conn.Exec(ctx, `
		UPDATE dingtalk_group_route
		SET agent_id = $2, revision = revision + 1
		WHERE installation_id = $1 AND conversation_id = 'mixed-rollout'
	`, dingtalkInst, otherAgent); err != nil {
		t.Fatalf("simulate legacy route reassignment: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		SELECT mention_count FROM dingtalk_group_presence
		WHERE installation_id = $1 AND conversation_id = 'mixed-rollout'
	`, dingtalkInst).Scan(&mentionCount); err != nil {
		t.Fatalf("inspect route-only update: %v", err)
	}
	if mentionCount != 1 {
		t.Fatalf("route reassignment incremented activity to %d", mentionCount)
	}

	// A process from the previous group-backed draft may still update identity
	// on a presence row during rollout. The compatibility trigger must preserve
	// that installation-level update.
	if _, err := conn.Exec(ctx, `
		UPDATE dingtalk_group_presence
		SET bot_name = 'Old Draft Bot'
		WHERE installation_id = $1 AND conversation_id = 'mixed-rollout'
	`, dingtalkInst); err != nil {
		t.Fatalf("simulate old-draft identity update: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		SELECT bot_name FROM dingtalk_bot_identity WHERE installation_id = $1
	`, dingtalkInst).Scan(&botName); err != nil {
		t.Fatalf("inspect mirrored Bot identity: %v", err)
	}
	if botName != "Old Draft Bot" {
		t.Fatalf("mirrored Bot name = %q, want Old Draft Bot", botName)
	}

	for _, name := range []string{
		"389_backfill_dingtalk_bot_identity.down.sql",
		"388_create_dingtalk_bot_identity_installation_index.down.sql",
		"387_create_dingtalk_bot_identity.down.sql",
		"386_backfill_dingtalk_group_presence.down.sql",
		"385_create_dingtalk_group_presence_activity_index.down.sql",
		"384_create_dingtalk_group_presence_identity_index.down.sql",
		"383_create_dingtalk_group_presence.down.sql",
		"382_remove_dingtalk_group_routing_bindings.down.sql",
	} {
		applyMigrationFile(t, ctx, conn.Conn(), name)
	}
	assertDingTalkRoutingRemovalIndex(t, ctx, conn, "idx_dingtalk_group_route_installation_conversation", true)
	assertDingTalkRoutingRemovalIndex(t, ctx, conn, "idx_dingtalk_group_route_workspace", false)
	assertDingTalkRoutingRemovalIndex(t, ctx, conn, "idx_dingtalk_group_route_id_unique", true)
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, dingtalkRoutingRemovalTestSchema+".dingtalk_group_presence").Scan(&presenceTableExists); err != nil {
		t.Fatalf("inspect rolled-back presence table: %v", err)
	}
	if presenceTableExists {
		t.Fatal("presence table remains after rollback")
	}
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, dingtalkRoutingRemovalTestSchema+".dingtalk_bot_identity").Scan(&identityTableExists); err != nil {
		t.Fatalf("inspect rolled-back Bot identity table: %v", err)
	}
	if identityTableExists {
		t.Fatal("Bot identity table remains after rollback")
	}
}

func assertMigrationRowCount(t *testing.T, ctx context.Context, conn *pgxpool.Conn, table string, want int) {
	t.Helper()
	var got int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s row count = %d, want %d", table, got, want)
	}
}

func assertDingTalkRoutingRemovalIndex(t *testing.T, ctx context.Context, conn *pgxpool.Conn, name string, wantUnique bool) {
	t.Helper()
	var unique bool
	err := conn.QueryRow(ctx, `
		SELECT i.indisunique
		FROM pg_index i
		JOIN pg_class idx ON idx.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = idx.relnamespace
		WHERE n.nspname = $1 AND idx.relname = $2
	`, dingtalkRoutingRemovalTestSchema, name).Scan(&unique)
	if err != nil {
		t.Fatalf("inspect index %s: %v", name, err)
	}
	if unique != wantUnique {
		t.Fatalf("index %s unique = %t, want %t", name, unique, wantUnique)
	}
}
