package lark

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Orphan-reclaim fixtures (#4810 / MUL-3937). Namespaced away from the rebind /
// scope tests so a shared test database never cross-contaminates. Unlike those
// FK-free fixtures, this test DOES create real workspace / runtime / agent rows,
// because the reclaim decision turns on whether the installation's owning
// workspace or agent still EXISTS.
const (
	orWS           = "0f9a0000-0000-4000-8000-000000000001"
	orRuntime      = "0f9a0000-0000-4000-8000-000000000002"
	orLiveAgent    = "0f9a0000-0000-4000-8000-00000000000a"
	orArchAgent    = "0f9a0000-0000-4000-8000-00000000000b"
	orAbsentWS     = "0f9a0000-0000-4000-8000-0000000000f1"
	orAbsentAgent  = "0f9a0000-0000-4000-8000-0000000000fa"
	orCallerWS     = "0f9a0000-0000-4000-8000-0000000000c1"
	orCallerAgent  = "0f9a0000-0000-4000-8000-0000000000ca"
	orInstaller    = "0f9a0000-0000-4000-8000-000000000005"
	orUser         = "0f9a0000-0000-4000-8000-000000000006"
	orChatSess     = "0f9a0000-0000-4000-8000-000000000007"
	orTokenHash    = "or_token_hash_cleanup"
	orAuditEventID = "ev_or_cleanup"

	orAppDead  = "cli_or_dead"  // ws + agent both gone      -> reclaim
	orAppWS    = "cli_or_ws"    // ws gone, agent alive      -> reclaim
	orAppAgent = "cli_or_agent" // ws alive, agent gone      -> reclaim
	orAppLive  = "cli_or_live"  // ws + agent both alive     -> survive
	orAppArch  = "cli_or_arch"  // agent archived (present)  -> survive
	orAppOwn   = "cli_or_own"   // the caller's own row      -> survive
	orAppDeps  = "cli_or_deps"  // orphan with dependents    -> reclaim + cleanup
)

// TestChannelStore_ReclaimOrphanedInstallationByAppID pins the dead-owner
// self-heal: the reclaim removes an installation that holds the (channel_type,
// app_id) slot only when its owning workspace or agent no longer exists, and
// never touches a live owner (including an archived-but-present agent) or the
// caller's own row.
func TestChannelStore_ReclaimOrphanedInstallationByAppID(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	store := NewChannelStore(db.New(pool))

	apps := []string{orAppDead, orAppWS, orAppAgent, orAppLive, orAppArch, orAppOwn, orAppDeps}
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`, apps)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_user_binding WHERE multica_user_id = $1`, orUser)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, orChatSess)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_binding_token WHERE token_hash = $1`, orTokenHash)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_inbound_audit WHERE channel_event_id = $1`, orAuditEventID)
		// Deleting the workspace cascades the runtime + both agents (FK).
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, orWS)
	}
	clean()
	t.Cleanup(clean)

	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed: %v\nquery: %s", err, q)
		}
	}
	// Real owner rows: one workspace, one runtime, a live agent and an archived agent.
	exec(`INSERT INTO workspace (id, name, slug) VALUES ($1, 'orphan-reclaim-test', 'orphan-reclaim-test-ws')`, orWS)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider) VALUES ($1, $2, 'or-rt', 'local', 'local')`, orRuntime, orWS)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id) VALUES ($1, $2, 'or-live', 'local', $3)`, orLiveAgent, orWS, orRuntime)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id, archived_at) VALUES ($1, $2, 'or-arch', 'local', $3, now())`, orArchAgent, orWS, orRuntime)

	insert := func(app, ws, agent string) pgtype.UUID {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4, 'active')
RETURNING id
`, ws, agent, app, orInstaller).Scan(&id); err != nil {
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

	callerWS := util.MustParseUUID(orCallerWS)
	callerAgent := util.MustParseUUID(orCallerAgent)
	reclaim := func(app string) {
		if err := store.ReclaimOrphanedInstallationByAppID(ctx, callerWS, callerAgent, app); err != nil {
			t.Fatalf("ReclaimOrphanedInstallationByAppID(%s): %v", app, err)
		}
	}

	t.Run("workspace and agent both gone is reclaimed", func(t *testing.T) {
		id := insert(orAppDead, orAbsentWS, orAbsentAgent)
		reclaim(orAppDead)
		if exists(id) {
			t.Fatal("orphan row (workspace + agent both deleted) was not reclaimed; it keeps blocking the app_id slot")
		}
	})

	t.Run("workspace gone (agent alive) is reclaimed", func(t *testing.T) {
		id := insert(orAppWS, orAbsentWS, orLiveAgent)
		reclaim(orAppWS)
		if exists(id) {
			t.Fatal("installation whose workspace was deleted was not reclaimed")
		}
	})

	t.Run("agent gone (workspace alive) is reclaimed", func(t *testing.T) {
		id := insert(orAppAgent, orWS, orAbsentAgent)
		reclaim(orAppAgent)
		if exists(id) {
			t.Fatal("installation whose agent was hard-deleted was not reclaimed")
		}
	})

	t.Run("live owner is never reclaimed", func(t *testing.T) {
		id := insert(orAppLive, orWS, orLiveAgent)
		reclaim(orAppLive)
		if !exists(id) {
			t.Fatal("a live owner's installation was reclaimed; only a dead owner may be reclaimed")
		}
	})

	t.Run("archived agent counts as a live owner and survives", func(t *testing.T) {
		id := insert(orAppArch, orWS, orArchAgent)
		reclaim(orAppArch)
		if !exists(id) {
			t.Fatal("an archived-but-present agent's installation was reclaimed; archival is reversible and must stay a live-owner conflict")
		}
	})

	t.Run("caller's own row is excluded even when orphaned", func(t *testing.T) {
		// The caller's (workspace, agent) do not exist in this test, so ONLY the
		// self-fence protects the row — the upsert reactivates it in place.
		id := insert(orAppOwn, orCallerWS, orCallerAgent)
		reclaim(orAppOwn)
		if !exists(id) {
			t.Fatal("the caller's own row was deleted; the upsert must be able to reactivate it in place")
		}
	})

	t.Run("reclaimed orphan clears its dependent rows", func(t *testing.T) {
		id := insert(orAppDeps, orAbsentWS, orAbsentAgent)
		exec(`INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', 'ou_or_user')`, orAbsentWS, orUser, id)
		exec(`INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
VALUES ($1, $2, 'feishu', 'oc_or_chat', 'p2p')`, orChatSess, id)
		exec(`INSERT INTO channel_binding_token (token_hash, workspace_id, installation_id, channel_type, channel_user_id, expires_at)
VALUES ($1, $2, $3, 'feishu', 'ou_or_user', now() + interval '10 minutes')`, orTokenHash, orAbsentWS, id)
		exec(`INSERT INTO channel_inbound_audit (installation_id, channel_type, event_type, channel_event_id, drop_reason)
VALUES ($1, 'feishu', 'im.message.receive_v1', $2, 'orphaned_installation')`, id, orAuditEventID)

		reclaim(orAppDeps)

		count := func(q string, args ...any) int {
			var n int
			if err := pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
				t.Fatalf("count: %v", err)
			}
			return n
		}
		if exists(id) {
			t.Fatal("orphan installation with dependents was not reclaimed")
		}
		if n := count(`SELECT count(*) FROM channel_user_binding WHERE installation_id = $1`, id); n != 0 {
			t.Fatalf("member links not cleaned: %d dangling rows", n)
		}
		if n := count(`SELECT count(*) FROM channel_chat_session_binding WHERE installation_id = $1`, id); n != 0 {
			t.Fatalf("chat-session bindings not cleaned: %d dangling rows", n)
		}
		if n := count(`SELECT count(*) FROM channel_binding_token WHERE installation_id = $1`, id); n != 0 {
			t.Fatalf("binding tokens not cleaned: %d dangling rows", n)
		}
		if n := count(`SELECT count(*) FROM channel_inbound_audit WHERE installation_id = $1`, id); n != 0 {
			t.Fatalf("audit rows still reference the reclaimed installation: %d", n)
		}
		if n := count(`SELECT count(*) FROM channel_inbound_audit WHERE channel_event_id = $1 AND installation_id IS NULL`, orAuditEventID); n != 1 {
			t.Fatalf("audit row should survive detached (installation_id NULL), got %d", n)
		}
	})
}
