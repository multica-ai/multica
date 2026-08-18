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
			channel_type TEXT NOT NULL,
			agent_id UUID NOT NULL
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
		INSERT INTO channel_installation (id, channel_type, agent_id) VALUES
			($1, 'dingtalk', $3),
			($2, 'slack', $3)
	`, dingtalkInst, slackInst, defaultAgent); err != nil {
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

	var tableExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, dingtalkRoutingRemovalTestSchema+".dingtalk_group_route").Scan(&tableExists); err != nil {
		t.Fatalf("inspect retained route table: %v", err)
	}
	if !tableExists {
		t.Fatal("legacy route table was removed during the compatibility window")
	}

	applyMigrationFile(t, ctx, conn.Conn(), "382_remove_dingtalk_group_routing_bindings.down.sql")
	assertDingTalkRoutingRemovalIndex(t, ctx, conn, "idx_dingtalk_group_route_installation_conversation", true)
	assertDingTalkRoutingRemovalIndex(t, ctx, conn, "idx_dingtalk_group_route_workspace", false)
	assertDingTalkRoutingRemovalIndex(t, ctx, conn, "idx_dingtalk_group_route_id_unique", true)
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
