package lark

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Delete-time channel cleanup fixtures. These pin the #4810 fix on the OTHER
// half of "auto-reclaim on delete": deleting a workspace, or hard-deleting an
// archived agent on runtime teardown, must sweep the channel_installation rows
// (and every dependent row) their owners left behind — channel_* has no FK to
// workspace/agent (MUL-3515 §4), so nothing else would.
const (
	ccWS         = "cc000000-0000-4000-8000-000000000001"
	ccRuntime    = "cc000000-0000-4000-8000-000000000003"
	ccAgentArch  = "cc000000-0000-4000-8000-00000000000a"
	ccAgentLive  = "cc000000-0000-4000-8000-00000000000b"
	ccInstaller  = "cc000000-0000-4000-8000-000000000005"
	ccUser       = "cc000000-0000-4000-8000-000000000006"
	ccChatArch   = "cc000000-0000-4000-8000-0000000000c1"
	ccChatLive   = "cc000000-0000-4000-8000-0000000000c2"
	ccTokenArch  = "cc_token_archived"
	ccTokenLive  = "cc_token_live"
	ccAuditArch  = "ev_cc_archived"
	ccAuditLive  = "ev_cc_live"
	ccAppArchive = "cli_cc_archived"
	ccAppLive    = "cli_cc_live"

	// Workspace-delete fixture (its own workspace so the DeleteWorkspace can
	// remove it, cascading the runtime + agent).
	ccWSDel      = "cc000000-0000-4000-8000-000000000002"
	ccRuntimeDel = "cc000000-0000-4000-8000-000000000004"
	ccAgentDel   = "cc000000-0000-4000-8000-00000000000d"
	ccChatWs     = "cc000000-0000-4000-8000-0000000000c3"
	ccTokenWs    = "cc_token_ws"
	ccAuditWs    = "ev_cc_ws"
	ccAppWs      = "cli_cc_ws"
)

// seedFullInstallation inserts one active installation plus the full spread of
// dependents (member link, chat-session binding, pending binding token, inbound
// audit) so a cleanup path can be shown to sweep all of them.
func seedFullInstallation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ws, agent, app, chatSess, tokenHash, auditEvent string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4, 'active')
RETURNING id
`, ws, agent, app, ccInstaller).Scan(&id); err != nil {
		t.Fatalf("seed installation app=%s: %v", app, err)
	}
	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed dependent for app=%s: %v", app, err)
		}
	}
	exec(`INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', 'ou_cc_user')`, ws, ccUser, id)
	exec(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
VALUES ($1, $2, 'feishu', 'oc_cc_chat', 'p2p')`, chatSess, id)
	exec(`INSERT INTO channel_binding_token (token_hash, workspace_id, installation_id, channel_type, channel_user_id, expires_at)
VALUES ($1, $2, $3, 'feishu', 'ou_cc_user', now() + interval '10 minutes')`, tokenHash, ws, id)
	exec(`INSERT INTO channel_outbound_card_message (chat_session_id, channel_type, channel_chat_id, channel_card_message_id, status)
VALUES ($1, 'feishu', 'oc_cc_chat', 'om_cc_card', 'final')`, chatSess)
	exec(`INSERT INTO channel_inbound_message_dedup (installation_id, message_id)
VALUES ($1, 'msg_cc_dedup')`, id)
	exec(`INSERT INTO channel_inbound_audit (installation_id, channel_type, event_type, channel_event_id, drop_reason)
VALUES ($1, 'feishu', 'im.message.receive_v1', $2, 'test')`, id, auditEvent)
	return id
}

func ccCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// assertInstallationSwept checks an installation and all its dependents are gone.
// On these HARD-delete paths (workspace delete / runtime teardown) the inbound-
// audit row is PURGED, not detached: channel_inbound_audit has no workspace_id and
// no reaper, so a detached (NULL) row would be permanently unattributable.
func assertInstallationSwept(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, chatSess, auditEvent string) {
	t.Helper()
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_installation WHERE id = $1`, id); n != 0 {
		t.Fatalf("installation not swept: %d rows remain (its bot's app_id slot stays occupied)", n)
	}
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_user_binding WHERE installation_id = $1`, id); n != 0 {
		t.Fatalf("member links not swept: %d dangling rows", n)
	}
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_chat_session_binding WHERE installation_id = $1`, id); n != 0 {
		t.Fatalf("chat-session bindings not swept: %d dangling rows", n)
	}
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_binding_token WHERE installation_id = $1`, id); n != 0 {
		t.Fatalf("binding tokens not swept: %d dangling rows", n)
	}
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_outbound_card_message WHERE chat_session_id = $1`, chatSess); n != 0 {
		t.Fatalf("outbound card messages not swept: %d dangling rows (no reaper would ever collect them)", n)
	}
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_inbound_message_dedup WHERE installation_id = $1`, id); n != 0 {
		t.Fatalf("inbound dedup rows not swept: %d dangling rows (PurgeChannelInboundDedup has no caller)", n)
	}
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_inbound_audit WHERE channel_event_id = $1`, auditEvent); n != 0 {
		t.Fatalf("audit row should be purged on a hard delete, got %d remaining", n)
	}
}

func assertInstallationIntact(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, chatSess, auditEvent string) {
	t.Helper()
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_installation WHERE id = $1`, id); n != 1 {
		t.Fatalf("a live-owner installation was swept: %d rows (want 1)", n)
	}
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_user_binding WHERE installation_id = $1`, id); n != 1 {
		t.Fatalf("live-owner member link was swept: %d (want 1)", n)
	}
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_outbound_card_message WHERE chat_session_id = $1`, chatSess); n != 1 {
		t.Fatalf("live-owner outbound card was swept: %d (want 1)", n)
	}
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_inbound_message_dedup WHERE installation_id = $1`, id); n != 1 {
		t.Fatalf("live-owner dedup row was swept: %d (want 1)", n)
	}
	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM channel_inbound_audit WHERE channel_event_id = $1 AND installation_id = $2`, auditEvent, id); n != 1 {
		t.Fatalf("live-owner audit ref was removed: %d (want 1)", n)
	}
}

// TestDeleteChannelInstallationsByArchivedRuntimeAgents: runtime teardown hard-
// deletes archived agents; this cleanup must sweep exactly those agents'
// installations (and dependents), leaving live agents on the runtime untouched.
func TestDeleteChannelInstallationsByArchivedRuntimeAgents(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	q := db.New(pool)

	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`, []string{ccAppArchive, ccAppLive})
		_, _ = pool.Exec(ctx, `DELETE FROM channel_user_binding WHERE multica_user_id = $1`, ccUser)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = ANY($1)`, []string{ccChatArch, ccChatLive})
		_, _ = pool.Exec(ctx, `DELETE FROM channel_binding_token WHERE token_hash = ANY($1)`, []string{ccTokenArch, ccTokenLive})
		_, _ = pool.Exec(ctx, `DELETE FROM channel_outbound_card_message WHERE chat_session_id = ANY($1)`, []string{ccChatArch, ccChatLive})
		_, _ = pool.Exec(ctx, `DELETE FROM channel_inbound_message_dedup WHERE message_id = 'msg_cc_dedup'`)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_inbound_audit WHERE channel_event_id = ANY($1)`, []string{ccAuditArch, ccAuditLive})
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, ccWS)
	}
	clean()
	t.Cleanup(clean)

	exec := func(query string, args ...any) {
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed owner: %v", err)
		}
	}
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'cc ws', 'cc-ws', '')`, ccWS)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
VALUES ($1, $2, 'cc runtime', 'local', 'multica_daemon')`, ccRuntime, ccWS)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id, archived_at)
VALUES ($1, $2, 'cc archived agent', 'local', $3, now())`, ccAgentArch, ccWS, ccRuntime)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id)
VALUES ($1, $2, 'cc live agent', 'local', $3)`, ccAgentLive, ccWS, ccRuntime)

	archivedID := seedFullInstallation(t, ctx, pool, ccWS, ccAgentArch, ccAppArchive, ccChatArch, ccTokenArch, ccAuditArch)
	liveID := seedFullInstallation(t, ctx, pool, ccWS, ccAgentLive, ccAppLive, ccChatLive, ccTokenLive, ccAuditLive)

	if err := q.DeleteChannelInstallationsByArchivedRuntimeAgents(ctx, util.MustParseUUID(ccRuntime)); err != nil {
		t.Fatalf("DeleteChannelInstallationsByArchivedRuntimeAgents: %v", err)
	}

	assertInstallationSwept(t, ctx, pool, archivedID, ccChatArch, ccAuditArch)
	assertInstallationIntact(t, ctx, pool, liveID, ccChatLive, ccAuditLive)
}

// TestDeleteWorkspace_SweepsChannelInstallations: deleting a workspace must sweep
// its channel installations (and dependents) so no orphan keeps occupying its
// bot's routing slot after the workspace — and its cascade-deleted agents — are
// gone.
func TestDeleteWorkspace_SweepsChannelInstallations(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	q := db.New(pool)

	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = $1`, ccAppWs)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_user_binding WHERE multica_user_id = $1`, ccUser)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, ccChatWs)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_binding_token WHERE token_hash = $1`, ccTokenWs)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_outbound_card_message WHERE chat_session_id = $1`, ccChatWs)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_inbound_message_dedup WHERE message_id = 'msg_cc_dedup'`)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_inbound_audit WHERE channel_event_id = $1`, ccAuditWs)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, ccWSDel)
	}
	clean()
	t.Cleanup(clean)

	exec := func(query string, args ...any) {
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed owner: %v", err)
		}
	}
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'cc ws del', 'cc-ws-del', '')`, ccWSDel)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
VALUES ($1, $2, 'cc runtime del', 'local', 'multica_daemon')`, ccRuntimeDel, ccWSDel)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id)
VALUES ($1, $2, 'cc agent del', 'local', $3)`, ccAgentDel, ccWSDel, ccRuntimeDel)

	id := seedFullInstallation(t, ctx, pool, ccWSDel, ccAgentDel, ccAppWs, ccChatWs, ccTokenWs, ccAuditWs)

	if err := q.DeleteWorkspace(ctx, util.MustParseUUID(ccWSDel)); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	if n := ccCount(t, ctx, pool, `SELECT count(*) FROM workspace WHERE id = $1`, ccWSDel); n != 0 {
		t.Fatalf("workspace not deleted: %d rows", n)
	}
	assertInstallationSwept(t, ctx, pool, id, ccChatWs, ccAuditWs)
}
