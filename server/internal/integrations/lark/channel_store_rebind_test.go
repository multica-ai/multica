package lark

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Rebind regression fixtures. Namespaced away from the scope test's ids so a
// shared test database never cross-contaminates. channel_* has no foreign keys,
// so these rows need no parent records; the test cleans up by deterministic key
// before and after (a killed prior run must not leave colliding rows behind).
const (
	rbWS         = "5c09e100-0000-4000-8000-000000000001"
	rbWS2        = "5c09e100-0000-4000-8000-000000000002"
	rbRuntime    = "5c09e100-0000-4000-8000-000000000003"
	rbAgentA     = "5c09e100-0000-4000-8000-00000000000a"
	rbAgentB     = "5c09e100-0000-4000-8000-00000000000b"
	rbAgentArch  = "5c09e100-0000-4000-8000-00000000000c"
	rbInstaller  = "5c09e100-0000-4000-8000-000000000005"
	rbUser       = "5c09e100-0000-4000-8000-000000000006"
	rbChatSess   = "5c09e100-0000-4000-8000-000000000007"
	rbGhostWS    = "5c09e100-0000-4000-8000-0000000000f1" // never seeded: a deleted workspace
	rbGhostAgent = "5c09e100-0000-4000-8000-0000000000f2" // never seeded: a hard-deleted agent

	rbAppSame        = "cli_rb_same"
	rbAppDiff        = "cli_rb_diff"
	rbAppActive      = "cli_rb_active"
	rbAppWsFence     = "cli_rb_wsfence"
	rbAppWsActive    = "cli_rb_wsactive"
	rbAppReactivate  = "cli_rb_reactivate"
	rbAppMove        = "cli_rb_move"
	rbAppOrphanWS    = "cli_rb_orphan_ws"
	rbAppOrphanAgent = "cli_rb_orphan_agent"
	rbAppArchived    = "cli_rb_archived"
	rbAppLive        = "cli_rb_live"
)

// seedRebindOwners inserts the workspace/runtime/agent rows the rebind fixtures
// reference. ReclaimDeadChannelInstallationByAppID now treats an installation
// whose owning workspace OR agent row is gone as a DEAD orphan to reclaim, so
// these tests must give their rows real owners — otherwise the orphan branch,
// not the revoked/same-agent/other-workspace fences, would decide every case.
// rbAgentArch is archived (a live-but-reversible owner). Idempotent; the matching
// teardown drops the workspaces, which cascades to the runtime and agents.
func seedRebindOwners(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed rebind owner: %v", err)
		}
	}
	for _, ws := range []string{rbWS, rbWS2} {
		exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, $2, $3, '') ON CONFLICT (id) DO NOTHING`,
			ws, "rebind "+ws, "rebind-"+ws)
	}
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
VALUES ($1, $2, 'rebind runtime', 'local', 'multica_daemon') ON CONFLICT (id) DO NOTHING`, rbRuntime, rbWS)
	// Names must be unique per workspace (agent_workspace_name_unique), so key
	// each on its id.
	for _, agent := range []string{rbAgentA, rbAgentB} {
		exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id)
VALUES ($1, $2, $3, 'local', $4) ON CONFLICT (id) DO NOTHING`, agent, rbWS, "rebind agent "+agent, rbRuntime)
	}
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id, archived_at)
VALUES ($1, $2, $3, 'local', $4, now()) ON CONFLICT (id) DO NOTHING`, rbAgentArch, rbWS, "rebind archived agent "+rbAgentArch, rbRuntime)
}

// cleanRebindOwners drops the seeded workspaces; the FK ON DELETE CASCADE takes
// the runtime and agents with them. channel_installation has no such FK — the
// bug under test — so those rows are cleaned by app_id separately.
func cleanRebindOwners(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = ANY($1)`, []string{rbWS, rbWS2})
}

// TestChannelStore_ReclaimDeadRevokedFences guards the REVOKED branch of the
// ReclaimDeadChannelInstallationByAppID gate: with live owners seeded, it must
// claim EVERY revoked row EXCEPT the caller's own (workspace, agent) pair —
// including a revoked row in another workspace (#4810: revoke is a self-serve
// "I'm done", so it must never leave a bot permanently un-rebindable). The same
// agent's own revoked row and any ACTIVE row (same or other workspace) must
// survive. (The orphan branch is covered by
// TestChannelStore_ReclaimDeadReclaimsOrphansRefusesLiveOwners.)
func TestChannelStore_ReclaimDeadRevokedFences(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	store := NewChannelStore(db.New(pool))

	apps := []string{rbAppSame, rbAppDiff, rbAppActive, rbAppWsFence, rbAppWsActive, rbAppReactivate, rbAppMove}
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`, apps)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_user_binding WHERE multica_user_id = $1`, rbUser)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, rbChatSess)
	}
	clean()
	seedRebindOwners(t, ctx, pool)
	t.Cleanup(func() { cleanRebindOwners(ctx, pool) })
	t.Cleanup(clean)

	// insert an installation and return its id.
	insert := func(app, ws, agent, status string) pgtype.UUID {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4, $5)
RETURNING id
`, ws, agent, app, rbInstaller, status).Scan(&id); err != nil {
			t.Fatalf("insert installation app=%s status=%s: %v", app, status, err)
		}
		return util.MustParseUUID(id)
	}
	exists := func(id pgtype.UUID) bool {
		_, err := store.GetLarkInstallation(ctx, id)
		if err == nil {
			return true
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return false
		}
		t.Fatalf("GetLarkInstallation: %v", err)
		return false
	}

	wsUUID := util.MustParseUUID(rbWS)
	agentAUUID := util.MustParseUUID(rbAgentA)
	agentBUUID := util.MustParseUUID(rbAgentB)

	t.Run("same agent revoked row is preserved", func(t *testing.T) {
		clean()
		id := insert(rbAppSame, rbWS, rbAgentA, "revoked")
		if err := store.ReclaimDeadInstallationByAppID(ctx, wsUUID, agentAUUID, rbAppSame); err != nil {
			t.Fatalf("ReclaimDeadInstallationByAppID: %v", err)
		}
		if !exists(id) {
			t.Fatal("same agent's own revoked row was deleted; it must be reactivated in place by the upsert, not orphaned")
		}
	})

	t.Run("different agent revoked row is deleted", func(t *testing.T) {
		clean()
		id := insert(rbAppDiff, rbWS, rbAgentA, "revoked")
		if err := store.ReclaimDeadInstallationByAppID(ctx, wsUUID, agentBUUID, rbAppDiff); err != nil {
			t.Fatalf("ReclaimDeadInstallationByAppID: %v", err)
		}
		if exists(id) {
			t.Fatal("a different agent's revoked row was not deleted; it would keep blocking the app_id unique slot")
		}
	})

	t.Run("active row is never deleted", func(t *testing.T) {
		clean()
		id := insert(rbAppActive, rbWS, rbAgentA, "active")
		if err := store.ReclaimDeadInstallationByAppID(ctx, wsUUID, agentBUUID, rbAppActive); err != nil {
			t.Fatalf("ReclaimDeadInstallationByAppID: %v", err)
		}
		if !exists(id) {
			t.Fatal("an active installation was deleted through the revoked-cleanup path")
		}
	})

	t.Run("other workspace revoked row is reclaimed", func(t *testing.T) {
		clean()
		id := insert(rbAppWsFence, rbWS2, rbAgentA, "revoked")
		if err := store.ReclaimDeadInstallationByAppID(ctx, wsUUID, agentBUUID, rbAppWsFence); err != nil {
			t.Fatalf("ReclaimDeadInstallationByAppID: %v", err)
		}
		if exists(id) {
			t.Fatal("a revoked row in another workspace was not reclaimed; a disconnected bot must be rebindable from any workspace that controls it (#4810)")
		}
	})

	t.Run("other workspace active row is preserved", func(t *testing.T) {
		clean()
		id := insert(rbAppWsActive, rbWS2, rbAgentA, "active")
		if err := store.ReclaimDeadInstallationByAppID(ctx, wsUUID, agentBUUID, rbAppWsActive); err != nil {
			t.Fatalf("ReclaimDeadInstallationByAppID: %v", err)
		}
		if !exists(id) {
			t.Fatal("an ACTIVE row in another workspace was reclaimed; only revoked/orphan owners are dead — a live owner must never be stolen")
		}
	})
}

// TestChannelStore_ReinstallReactivationSemantics exercises the full
// finishSuccess ordering (cleanup-then-upsert) against a real database and
// pins the product behavior the fix protects:
//
//   - SAME agent reconnect: the revoked row is reactivated in place, keeping its
//     installation_id and every member/chat binding hanging off it.
//   - DIFFERENT agent rebind: a fresh installation_id is created and the old
//     agent's revoked row is removed so it no longer blocks the app_id slot.
func TestChannelStore_ReinstallReactivationSemantics(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	store := NewChannelStore(db.New(pool))

	apps := []string{rbAppReactivate, rbAppMove}
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`, apps)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_user_binding WHERE multica_user_id = $1`, rbUser)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, rbChatSess)
	}
	clean()
	seedRebindOwners(t, ctx, pool)
	t.Cleanup(func() { cleanRebindOwners(ctx, pool) })
	t.Cleanup(clean)

	insertRevoked := func(app, agent string) pgtype.UUID {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4, 'revoked')
RETURNING id
`, rbWS, agent, app, rbInstaller).Scan(&id); err != nil {
			t.Fatalf("insert revoked installation: %v", err)
		}
		return util.MustParseUUID(id)
	}
	// Attach a member binding + chat-session binding to an installation, the way
	// a real workspace accumulates them while the bot is connected.
	attachBindings := func(installID pgtype.UUID) {
		if _, err := pool.Exec(ctx, `
INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', 'ou_rb_user')
`, rbWS, rbUser, installID); err != nil {
			t.Fatalf("insert user binding: %v", err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
VALUES ($1, $2, 'feishu', 'oc_rb_chat', 'p2p')
`, rbChatSess, installID); err != nil {
			t.Fatalf("insert chat-session binding: %v", err)
		}
	}
	countBindingsOn := func(installID pgtype.UUID) (users, chats int) {
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_user_binding WHERE installation_id = $1`, installID).Scan(&users); err != nil {
			t.Fatalf("count user bindings: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_chat_session_binding WHERE installation_id = $1`, installID).Scan(&chats); err != nil {
			t.Fatalf("count chat bindings: %v", err)
		}
		return
	}

	upsert := func(agent, app string) Installation {
		inst, err := store.UpsertLarkInstallation(ctx, UpsertInstallationParams{
			WorkspaceID:        util.MustParseUUID(rbWS),
			AgentID:            util.MustParseUUID(agent),
			AppID:              app,
			AppSecretEncrypted: []byte{1, 2, 3},
			BotOpenID:          "ou_rb_bot",
			InstallerUserID:    util.MustParseUUID(rbInstaller),
			Region:             "feishu",
		})
		if err != nil {
			t.Fatalf("UpsertLarkInstallation: %v", err)
		}
		return inst
	}

	t.Run("same agent reconnect keeps installation_id and bindings", func(t *testing.T) {
		clean()
		oldID := insertRevoked(rbAppReactivate, rbAgentA)
		attachBindings(oldID)

		// finishSuccess order: cleanup for the current agent (a no-op for the
		// same agent), then upsert.
		if err := store.ReclaimDeadInstallationByAppID(ctx, util.MustParseUUID(rbWS), util.MustParseUUID(rbAgentA), rbAppReactivate); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		inst := upsert(rbAgentA, rbAppReactivate)

		if inst.ID != oldID {
			t.Fatalf("same agent reconnect changed installation_id: got %v, want %v (in-place reactivation lost)", inst.ID, oldID)
		}
		if inst.Status != "active" {
			t.Fatalf("reactivated installation status=%q, want active", inst.Status)
		}
		if users, chats := countBindingsOn(oldID); users != 1 || chats != 1 {
			t.Fatalf("bindings not preserved on reconnect: users=%d chats=%d, want 1/1", users, chats)
		}
	})

	t.Run("different agent rebind gets a fresh installation_id", func(t *testing.T) {
		clean()
		oldID := insertRevoked(rbAppMove, rbAgentA)
		attachBindings(oldID)

		if err := store.ReclaimDeadInstallationByAppID(ctx, util.MustParseUUID(rbWS), util.MustParseUUID(rbAgentB), rbAppMove); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		inst := upsert(rbAgentB, rbAppMove)

		if inst.ID == oldID {
			t.Fatal("different agent rebind reused the old installation_id; the blocking revoked row was not cleared")
		}
		if inst.Status != "active" {
			t.Fatalf("new installation status=%q, want active", inst.Status)
		}
		// The old revoked row is gone (its unique app_id slot is freed for B).
		if _, err := store.GetLarkInstallation(ctx, oldID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("old agent's revoked row still present after rebind: err=%v", err)
		}
	})
}

// TestChannelStore_RebindCleansDependentRows pins the application-layer cleanup:
// channel_* has no FK/cascade, so hard-deleting the blocking revoked
// installation of a DIFFERENT agent must also clear every row that references it
// (chat-session bindings, pending binding tokens, member links) and detach
// inbound-audit rows by NULLing installation_id — not leave dangling dead rows.
func TestChannelStore_RebindCleansDependentRows(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	store := NewChannelStore(db.New(pool))

	const (
		app        = "cli_rb_cleanup"
		tokenHash  = "rb_token_hash_cleanup"
		auditEvent = "ev_rb_cleanup"
		dedupMsg   = "msg_rb_cleanup"
	)
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = $1`, app)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_user_binding WHERE multica_user_id = $1`, rbUser)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, rbChatSess)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_outbound_card_message WHERE chat_session_id = $1`, rbChatSess)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_binding_token WHERE token_hash = $1`, tokenHash)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_inbound_message_dedup WHERE message_id = $1`, dedupMsg)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_inbound_audit WHERE channel_event_id = $1`, auditEvent)
	}
	clean()
	seedRebindOwners(t, ctx, pool)
	t.Cleanup(func() { cleanRebindOwners(ctx, pool) })
	t.Cleanup(clean)

	// A revoked installation for agent A carrying the full spread of dependents.
	var oldID string
	if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4, 'revoked')
RETURNING id
`, rbWS, rbAgentA, app, rbInstaller).Scan(&oldID); err != nil {
		t.Fatalf("insert revoked installation: %v", err)
	}
	seed := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed dependent row: %v", err)
		}
	}
	seed(`INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', 'ou_rb_user')`, rbWS, rbUser, oldID)
	seed(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
VALUES ($1, $2, 'feishu', 'oc_rb_chat', 'p2p')`, rbChatSess, oldID)
	seed(`INSERT INTO channel_outbound_card_message (chat_session_id, channel_type, channel_chat_id, channel_card_message_id, status)
VALUES ($1, 'feishu', 'oc_rb_chat', 'om_rb_card', 'final')`, rbChatSess)
	seed(`INSERT INTO channel_binding_token (token_hash, workspace_id, installation_id, channel_type, channel_user_id, expires_at)
VALUES ($1, $2, $3, 'feishu', 'ou_rb_user', now() + interval '10 minutes')`, tokenHash, rbWS, oldID)
	seed(`INSERT INTO channel_inbound_message_dedup (installation_id, message_id)
VALUES ($1, $2)`, oldID, dedupMsg)
	seed(`INSERT INTO channel_inbound_audit (installation_id, channel_type, event_type, channel_event_id, drop_reason)
VALUES ($1, 'feishu', 'im.message.receive_v1', $2, 'revoked_installation')`, oldID, auditEvent)

	// Rebind the app to a DIFFERENT agent.
	if err := store.ReclaimDeadInstallationByAppID(ctx, util.MustParseUUID(rbWS), util.MustParseUUID(rbAgentB), app); err != nil {
		t.Fatalf("ReclaimDeadInstallationByAppID: %v", err)
	}

	count := func(q string, args ...any) int {
		var n int
		if err := pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM channel_installation WHERE id = $1`, oldID); n != 0 {
		t.Fatalf("blocking installation not deleted: %d rows remain", n)
	}
	if n := count(`SELECT count(*) FROM channel_user_binding WHERE installation_id = $1`, oldID); n != 0 {
		t.Fatalf("member links not cleaned: %d dangling rows", n)
	}
	if n := count(`SELECT count(*) FROM channel_chat_session_binding WHERE installation_id = $1`, oldID); n != 0 {
		t.Fatalf("chat-session bindings not cleaned: %d dangling rows (outbound patcher would error)", n)
	}
	if n := count(`SELECT count(*) FROM channel_binding_token WHERE installation_id = $1`, oldID); n != 0 {
		t.Fatalf("binding tokens not cleaned: %d redeemable rows into a deleted installation", n)
	}
	if n := count(`SELECT count(*) FROM channel_outbound_card_message WHERE chat_session_id = $1`, rbChatSess); n != 0 {
		t.Fatalf("outbound card messages not cleaned: %d rows keyed on the removed session (no reaper would ever collect them)", n)
	}
	if n := count(`SELECT count(*) FROM channel_inbound_message_dedup WHERE installation_id = $1`, oldID); n != 0 {
		t.Fatalf("inbound dedup rows not cleaned: %d dangling rows (PurgeChannelInboundDedup has no caller)", n)
	}
	// Audit history is preserved but detached: no row still points at the
	// deleted installation, and our audit row survives with a NULL reference
	// (reclaim keeps the workspace, so the detached row stays meaningful).
	if n := count(`SELECT count(*) FROM channel_inbound_audit WHERE installation_id = $1`, oldID); n != 0 {
		t.Fatalf("audit rows still reference the deleted installation: %d dangling ids", n)
	}
	if n := count(`SELECT count(*) FROM channel_inbound_audit WHERE channel_event_id = $1 AND installation_id IS NULL`, auditEvent); n != 1 {
		t.Fatalf("audit row should survive detached (installation_id NULL), got %d", n)
	}
}

// TestChannelStore_RebindGuardedDeleteRaceWithReactivation exercises the real
// concurrency the guarded delete protects against. Two transactions race on one
// revoked installation:
//
//   - txReconnect (agent A reconnecting to the SAME agent) reactivates the row
//     to 'active' but holds the row lock uncommitted;
//   - txRebind (agent B rebinding to a DIFFERENT agent) runs the full cleanup
//     via ReclaimDeadInstallationByAppID.
//
// The old read-then-clean-then-delete shape would read the still-committed
// 'revoked' row, wipe its dependents, then no-op on the fenced delete — losing
// A's bindings even though A's installation survives. The guarded delete instead
// blocks on A's row lock; once A commits the reactivation it re-checks
// status='revoked' (EvalPlanQual under READ COMMITTED), claims nothing, and the
// cleanup keyed off the RETURNING id never runs. The installation and every
// binding must be intact. The assertion is timing-independent — it holds for the
// fixed code in every interleaving — so the test can never fail spuriously.
func TestChannelStore_RebindGuardedDeleteRaceWithReactivation(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	store := NewChannelStore(db.New(pool))

	const (
		app        = "cli_rb_race"
		tokenHash  = "rb_token_hash_race"
		auditEvent = "ev_rb_race"
	)
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = $1`, app)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_user_binding WHERE multica_user_id = $1`, rbUser)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, rbChatSess)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_binding_token WHERE token_hash = $1`, tokenHash)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_inbound_audit WHERE channel_event_id = $1`, auditEvent)
	}
	clean()
	seedRebindOwners(t, ctx, pool)
	t.Cleanup(func() { cleanRebindOwners(ctx, pool) })
	t.Cleanup(clean)

	// A revoked installation for agent A, with the full spread of dependents.
	var idStr string
	if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4, 'revoked')
RETURNING id
`, rbWS, rbAgentA, app, rbInstaller).Scan(&idStr); err != nil {
		t.Fatalf("insert revoked installation: %v", err)
	}
	seed := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed dependent row: %v", err)
		}
	}
	seed(`INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', 'ou_rb_user')`, rbWS, rbUser, idStr)
	seed(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
VALUES ($1, $2, 'feishu', 'oc_rb_chat', 'p2p')`, rbChatSess, idStr)
	seed(`INSERT INTO channel_binding_token (token_hash, workspace_id, installation_id, channel_type, channel_user_id, expires_at)
VALUES ($1, $2, $3, 'feishu', 'ou_rb_user', now() + interval '10 minutes')`, tokenHash, rbWS, idStr)
	seed(`INSERT INTO channel_inbound_audit (installation_id, channel_type, event_type, channel_event_id, drop_reason)
VALUES ($1, 'feishu', 'im.message.receive_v1', $2, 'revoked_installation')`, idStr, auditEvent)

	// txReconnect: agent A reactivates the row and holds the lock uncommitted.
	txReconnect, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin txReconnect: %v", err)
	}
	defer txReconnect.Rollback(ctx)
	if _, err := txReconnect.Exec(ctx, `UPDATE channel_installation SET status = 'active' WHERE id = $1`, idStr); err != nil {
		t.Fatalf("reactivate in txReconnect: %v", err)
	}

	// txRebind: agent B's cleanup runs on its own transaction. Its guarded delete
	// blocks on txReconnect's row lock.
	done := make(chan error, 1)
	go func() {
		txRebind, err := pool.Begin(ctx)
		if err != nil {
			done <- err
			return
		}
		defer txRebind.Rollback(ctx)
		if err := store.WithTx(txRebind).ReclaimDeadInstallationByAppID(ctx, util.MustParseUUID(rbWS), util.MustParseUUID(rbAgentB), app); err != nil {
			done <- err
			return
		}
		done <- txRebind.Commit(ctx)
	}()

	// Let txRebind reach and block on the guarded delete, then let A win the race.
	time.Sleep(300 * time.Millisecond)
	if err := txReconnect.Commit(ctx); err != nil {
		t.Fatalf("commit txReconnect: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("txRebind: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("txRebind did not complete — possible deadlock in the guarded delete")
	}

	count := func(q string, args ...any) int {
		var n int
		if err := pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM channel_installation WHERE id = $1 AND status = 'active'`, idStr); n != 1 {
		t.Fatalf("reactivated installation was deleted by the racing rebind: %d active rows", n)
	}
	if n := count(`SELECT count(*) FROM channel_user_binding WHERE installation_id = $1`, idStr); n != 1 {
		t.Fatalf("member link wiped by the racing rebind: got %d, want 1", n)
	}
	if n := count(`SELECT count(*) FROM channel_chat_session_binding WHERE installation_id = $1`, idStr); n != 1 {
		t.Fatalf("chat-session binding wiped by the racing rebind: got %d, want 1", n)
	}
	if n := count(`SELECT count(*) FROM channel_binding_token WHERE installation_id = $1`, idStr); n != 1 {
		t.Fatalf("binding token wiped by the racing rebind: got %d, want 1", n)
	}
	if n := count(`SELECT count(*) FROM channel_inbound_audit WHERE installation_id = $1`, idStr); n != 1 {
		t.Fatalf("audit reference detached by the racing rebind: got %d, want 1", n)
	}
}

// TestChannelStore_ReclaimDeadReclaimsOrphansRefusesLiveOwners covers the ORPHAN
// branch of the reclaim gate — the #4810 fix. An installation whose owning
// workspace or agent has been hard-deleted is a dead orphan still occupying the
// (channel_type, app_id) routing slot; the reclaim must clear it so the bot can
// be rebound. A live owner — an active agent, INCLUDING an archived one — must be
// left in place so the follow-up upsert refuses the rebind rather than stealing
// the bot, and InstallationOwnerByAppID must report who holds it (so the caller
// can build an accurate conflict message).
func TestChannelStore_ReclaimDeadReclaimsOrphansRefusesLiveOwners(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	store := NewChannelStore(db.New(pool))

	apps := []string{rbAppOrphanWS, rbAppOrphanAgent, rbAppLive, rbAppArchived}
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`, apps)
	}
	clean()
	seedRebindOwners(t, ctx, pool)
	t.Cleanup(func() { cleanRebindOwners(ctx, pool) })
	t.Cleanup(clean)

	insert := func(app, ws, agent, status string) pgtype.UUID {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4, $5)
RETURNING id
`, ws, agent, app, rbInstaller, status).Scan(&id); err != nil {
			t.Fatalf("insert installation app=%s: %v", app, err)
		}
		return util.MustParseUUID(id)
	}
	exists := func(id pgtype.UUID) bool {
		_, err := store.GetLarkInstallation(ctx, id)
		if err == nil {
			return true
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return false
		}
		t.Fatalf("GetLarkInstallation: %v", err)
		return false
	}
	// A NEW agent B, in a live workspace, is the one rebinding each app.
	wsUUID := util.MustParseUUID(rbWS)
	agentBUUID := util.MustParseUUID(rbAgentB)

	t.Run("orphan from a deleted workspace is reclaimed", func(t *testing.T) {
		clean()
		id := insert(rbAppOrphanWS, rbGhostWS, rbAgentA, "active")
		if err := store.ReclaimDeadInstallationByAppID(ctx, wsUUID, agentBUUID, rbAppOrphanWS); err != nil {
			t.Fatalf("ReclaimDeadInstallationByAppID: %v", err)
		}
		if exists(id) {
			t.Fatal("an installation whose workspace no longer exists was not reclaimed; the bot would stay un-rebindable")
		}
	})

	t.Run("orphan from a hard-deleted agent is reclaimed", func(t *testing.T) {
		clean()
		id := insert(rbAppOrphanAgent, rbWS, rbGhostAgent, "active")
		if err := store.ReclaimDeadInstallationByAppID(ctx, wsUUID, agentBUUID, rbAppOrphanAgent); err != nil {
			t.Fatalf("ReclaimDeadInstallationByAppID: %v", err)
		}
		if exists(id) {
			t.Fatal("an installation whose agent was hard-deleted was not reclaimed")
		}
	})

	t.Run("live active owner is refused, not stolen", func(t *testing.T) {
		clean()
		id := insert(rbAppLive, rbWS, rbAgentA, "active")
		if err := store.ReclaimDeadInstallationByAppID(ctx, wsUUID, agentBUUID, rbAppLive); err != nil {
			t.Fatalf("ReclaimDeadInstallationByAppID: %v", err)
		}
		if !exists(id) {
			t.Fatal("a live active owner was reclaimed; agent B would silently steal the bot")
		}
		owner, err := store.InstallationOwnerByAppID(ctx, rbAppLive)
		if err != nil {
			t.Fatalf("InstallationOwnerByAppID: %v", err)
		}
		if owner.WorkspaceID != wsUUID || owner.AgentID != util.MustParseUUID(rbAgentA) || owner.AgentArchivedAt.Valid {
			t.Fatalf("owner mismatch: ws=%v agent=%v archived=%v", owner.WorkspaceID, owner.AgentID, owner.AgentArchivedAt.Valid)
		}
	})

	t.Run("archived agent owner is refused and reported archived", func(t *testing.T) {
		clean()
		id := insert(rbAppArchived, rbWS, rbAgentArch, "active")
		if err := store.ReclaimDeadInstallationByAppID(ctx, wsUUID, agentBUUID, rbAppArchived); err != nil {
			t.Fatalf("ReclaimDeadInstallationByAppID: %v", err)
		}
		if !exists(id) {
			t.Fatal("an archived agent's installation was reclaimed; archiving is reversible so the bot must stay owned")
		}
		owner, err := store.InstallationOwnerByAppID(ctx, rbAppArchived)
		if err != nil {
			t.Fatalf("InstallationOwnerByAppID: %v", err)
		}
		if !owner.AgentArchivedAt.Valid {
			t.Fatal("owner lookup did not report the agent as archived; the conflict message could not distinguish it")
		}
	})
}
