package wecom

// installation_bot_swap_test.go — a bot swap must not leave the previous bot's
// rows behind (#6547). DB-backed, because the whole claim is about what
// survives an upsert that reuses the installation row; skips when no migrated
// database is reachable, like installation_bot_name_test.go beside it.
//
// Why it matters: a WeCom aibot userid is anonymized per (bot, user), so a
// channel_user_binding that outlives the bot it was made under holds an id the
// new bot does not share. Outbound.tryDeliverInbox finds that binding by
// (workspace, member, channel_type), resolves the sender by its reused
// installation_id — the LIVE NEW bot — and sends to the OLD bot's userid.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	wcSwapWS      = "5c09e202-0000-4000-8000-000000000001"
	wcSwapRuntime = "5c09e202-0000-4000-8000-000000000003"
	wcSwapAgent   = "5c09e202-0000-4000-8000-00000000000a"
	wcSwapUser    = "5c09e202-0000-4000-8000-000000000005"
	wcSwapChat    = "5c09e202-0000-4000-8000-000000000007"

	wcSwapBotA = "bot_swap_a"
	wcSwapBotB = "bot_swap_b"

	// The anonymized userid bot A knows this member by. Bot B has never
	// issued it and cannot address it.
	wcSwapUserIDUnderA = "TBOT_A_USERID"
)

func setupBotSwap(t *testing.T) (context.Context, *pgxpool.Pool, *InstallationService) {
	t.Helper()
	pool := reclaimTestDB(t)
	ctx := context.Background()
	// The seeded rows are cleaned explicitly, by their own identifiers: none of
	// these tables has a foreign key to installation or workspace (MUL-3515 §4),
	// so dropping those rows does not take them with it — and one of the tests
	// below asserts its rows SURVIVE, which would otherwise leave a
	// channel_chat_session_binding behind to collide with the next run on the
	// UNIQUE (chat_session_id).
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, wcSwapChat)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_user_binding WHERE workspace_id = $1`, wcSwapWS)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_binding_token WHERE workspace_id = $1`, wcSwapWS)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_inbound_message_dedup WHERE message_id = $1`, "msg_under_bot_a")
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`,
			[]string{wcSwapBotA, wcSwapBotB})
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wcSwapWS)
	}
	clean()
	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed bot-swap fixture: %v", err)
		}
	}
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'wecom bot swap', 'wecom-bot-swap', '') ON CONFLICT (id) DO NOTHING`, wcSwapWS)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
VALUES ($1, $2, 'wecom bot swap runtime', 'local', 'multica_daemon') ON CONFLICT (id) DO NOTHING`, wcSwapRuntime, wcSwapWS)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id)
VALUES ($1, $2, 'wecom bot swap agent', 'local', $3) ON CONFLICT (id) DO NOTHING`, wcSwapAgent, wcSwapWS, wcSwapRuntime)
	t.Cleanup(clean)
	svc, _ := newReclaimSvc(t, pool)
	return ctx, pool, svc
}

func botSwapParams(bot string) InstallationParams {
	return InstallationParams{
		WorkspaceID:     mustUUID(wcSwapWS),
		AgentID:         mustUUID(wcSwapAgent),
		InstallerUserID: mustUUID(wcSwapUser),
		BotID:           bot,
		Secret:          "s3cr3t",
	}
}

// seedBotScopedRows writes one row of each kind a member's traffic leaves
// under an installation. None of these tables has a foreign key (MUL-3515 §4),
// which is exactly why the cleanup has to be explicit.
func seedBotScopedRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, installationID pgtype.UUID) {
	t.Helper()
	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed bot-scoped row: %v", err)
		}
	}
	exec(`INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id, config)
VALUES ($1, $2, $3, 'wecom', $4, '{}'::jsonb)`, wcSwapWS, wcSwapUser, installationID, wcSwapUserIDUnderA)
	exec(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, config)
VALUES ($1, $2, 'wecom', $3, 'p2p', '{}'::jsonb)`, wcSwapChat, installationID, wcSwapUserIDUnderA)
	exec(`INSERT INTO channel_binding_token (token_hash, workspace_id, installation_id, channel_type, channel_user_id, expires_at)
VALUES ($1, $2, $3, 'wecom', $4, now() + interval '10 minutes')`, "hash_"+wcSwapUserIDUnderA, wcSwapWS, installationID, wcSwapUserIDUnderA)
	exec(`INSERT INTO channel_inbound_message_dedup (installation_id, message_id)
VALUES ($1, $2)`, installationID, "msg_under_bot_a")
}

// countBotScopedRows totals the rows still hanging off an installation.
func countBotScopedRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, installationID pgtype.UUID) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range []string{
		"channel_user_binding",
		"channel_chat_session_binding",
		"channel_binding_token",
		"channel_inbound_message_dedup",
	} {
		var n int
		// The table name is a constant from the list above, never input.
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE installation_id = $1`, installationID,
		).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = n
	}
	return counts
}

// TestSwappingTheBotClearsThePreviousBotsRows is #6547: the installation row is
// reused, so without an explicit purge every dependent row crosses over into a
// bot whose userid namespace does not contain them.
func TestSwappingTheBotClearsThePreviousBotsRows(t *testing.T) {
	ctx, pool, svc := setupBotSwap(t)

	under, err := svc.Upsert(ctx, botSwapParams(wcSwapBotA))
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	seedBotScopedRows(t, ctx, pool, under.ID)

	swapped, err := svc.Upsert(ctx, botSwapParams(wcSwapBotB))
	if err != nil {
		t.Fatalf("bot swap: %v", err)
	}
	// The premise of the bug. If the id ever stops being reused this test is
	// asserting nothing, so state it rather than assume it.
	if swapped.ID != under.ID {
		t.Fatalf("installation id changed on swap (%v -> %v); this test no longer covers #6547",
			under.ID, swapped.ID)
	}

	for table, n := range countBotScopedRows(t, ctx, pool, swapped.ID) {
		if n != 0 {
			t.Errorf("%s still has %d row(s) from the previous bot — the new bot would "+
				"address userids from a namespace it does not share", table, n)
		}
	}
}

// TestRotatingTheSecretKeepsTheBindings is the other half, and the one a
// too-eager purge would break: pasting a fresh secret for the SAME bot is a
// credential rotation, not a swap. Clearing there would silently unbind every
// member and make them link their account again.
func TestRotatingTheSecretKeepsTheBindings(t *testing.T) {
	ctx, pool, svc := setupBotSwap(t)

	under, err := svc.Upsert(ctx, botSwapParams(wcSwapBotA))
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	seedBotScopedRows(t, ctx, pool, under.ID)

	rotated, err := svc.Upsert(ctx, botSwapParams(wcSwapBotA))
	if err != nil {
		t.Fatalf("secret rotation: %v", err)
	}
	for table, n := range countBotScopedRows(t, ctx, pool, rotated.ID) {
		if n != 1 {
			t.Errorf("%s has %d row(s) after a same-bot rotation, want the 1 seeded — "+
				"a rotation must not unbind anybody", table, n)
		}
	}
}
