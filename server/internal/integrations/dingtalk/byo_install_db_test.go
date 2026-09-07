package dingtalk

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func dingtalkInstallTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	var migrated bool
	if err := pool.QueryRow(ctx, `
SELECT to_regclass('public.channel_installation') IS NOT NULL
   AND to_regclass('public.channel_media_pending_object') IS NOT NULL
`).Scan(&migrated); err != nil || !migrated {
		pool.Close()
		t.Skip("channel installation tables not present (database not migrated)")
	}
	t.Cleanup(pool.Close)
	return pool
}

// A DingTalk senderStaffId is only unique inside one organization. Swapping an
// agent from one AppKey to another must therefore produce a new installation
// identity, require the colliding staff id to bind again, and make the old chat
// session unavailable to the new robot.
func TestRegisterBYO_DifferentAppKey_IsolatesIdentityStateDB(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	const (
		workspaceID    = "d1470000-0000-4000-8000-000000000001"
		agentID        = "d1470000-0000-4000-8000-000000000002"
		installerID    = "d1470000-0000-4000-8000-000000000003"
		multicaUserID  = "d1470000-0000-4000-8000-000000000004"
		oldInstallID   = "d1470000-0000-4000-8000-000000000010"
		chatSessionID  = "d1470000-0000-4000-8000-000000000020"
		bindingID      = "d1470000-0000-4000-8000-000000000021"
		taskID         = "d1470000-0000-4000-8000-000000000022"
		mediaMessageID = "d1470000-0000-4000-8000-000000000030"
		oldAppKey      = "dingtalk_identity_old_app"
		newAppKey      = "dingtalk_identity_new_app"
		staffID        = "staff-id-collision"
		chatID         = "cid-old-organization-chat"
		tokenHash      = "dingtalk_identity_old_token"
		auditEventID   = "dingtalk_identity_old_audit"
		storageKey     = "test/dingtalk/identity-old-image"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_outbound_card_message WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_outbound_message WHERE installation_id = $1`, oldInstallID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_task_delivery WHERE installation_id = $1`, oldInstallID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_chat_context_generation WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_binding_token WHERE token_hash = $1`, tokenHash)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_user_binding WHERE workspace_id = $1 AND channel_user_id = $2`, workspaceID, staffID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_inbound_message_dedup WHERE message_id = 'dingtalk-identity-old-message'`)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_inbound_audit WHERE channel_event_id = $1`, auditEventID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_media_pending_object WHERE storage_key = $1`, storageKey)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_installation WHERE workspace_id = $1 AND agent_id = $2 AND channel_type = 'dingtalk'`, workspaceID, agentID)
	}
	clean()
	t.Cleanup(clean)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed replacement fixture: %v", err)
		}
	}
	exec(`
INSERT INTO channel_installation (id, workspace_id, agent_id, channel_type, config, installer_user_id)
VALUES ($1, $2, $3, 'dingtalk', jsonb_build_object('app_id', $4::text), $5)
`, oldInstallID, workspaceID, agentID, oldAppKey, installerID)
	exec(`
INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'dingtalk', $4)
`, workspaceID, multicaUserID, oldInstallID, staffID)
	exec(`
INSERT INTO channel_chat_session_binding (id, chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
VALUES ($1, $2, $3, 'dingtalk', $4, 'p2p')
`, bindingID, chatSessionID, oldInstallID, chatID)
	exec(`
INSERT INTO channel_chat_context_generation (chat_session_id, revision)
VALUES ($1, 1)
`, chatSessionID)
	exec(`
INSERT INTO channel_binding_token (token_hash, workspace_id, installation_id, channel_type, channel_user_id, expires_at)
VALUES ($1, $2, $3, 'dingtalk', $4, now() + interval '10 minutes')
`, tokenHash, workspaceID, oldInstallID, staffID)
	exec(`
INSERT INTO channel_outbound_card_message (chat_session_id, channel_type, channel_chat_id, channel_card_message_id)
VALUES ($1, 'dingtalk', $2, 'old-outbound-message')
`, chatSessionID, chatID)
	exec(`
INSERT INTO channel_task_delivery (
    task_id, binding_id, installation_id, channel_type, channel_chat_id,
    chat_type, route_revision
) VALUES ($1, $2, $3, 'dingtalk', $4, 'p2p', 1)
`, taskID, bindingID, oldInstallID, chatID)
	exec(`
INSERT INTO channel_outbound_message (
    installation_id, channel_type, channel_message_id, binding_id,
    route_revision, task_id, outbound_kind
) VALUES ($1, 'dingtalk', 'old-route-reply', $2, 1, $3, 'task_reply')
`, oldInstallID, bindingID, taskID)
	exec(`
INSERT INTO channel_inbound_message_dedup (installation_id, message_id)
VALUES ($1, 'dingtalk-identity-old-message')
`, oldInstallID)
	exec(`
INSERT INTO channel_inbound_audit (installation_id, channel_type, event_type, channel_event_id, drop_reason)
VALUES ($1, 'dingtalk', 'chatbot-message', $2, 'test')
`, oldInstallID, auditEventID)
	exec(`
INSERT INTO channel_media_pending_object (storage_key, workspace_id, chat_message_id, storage_url, installation_id)
VALUES ($1, $2, $3, 'https://storage.example.test/old-image', $4)
`, storageKey, workspaceID, mediaMessageID, oldInstallID)

	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	svc, err := NewInstallService(db.New(pool), pool, testBox(t), nil)
	if err != nil {
		t.Fatalf("NewInstallService: %v", err)
	}
	svc.apiBase = srv.URL
	params := byoParams(workspaceID, agentID)
	params.AppKey = newAppKey
	row, err := svc.RegisterBYO(ctx, params)
	if err != nil {
		t.Fatalf("RegisterBYO replacement: %v", err)
	}
	oldUUID := util.MustParseUUID(oldInstallID)
	if row.ID == oldUUID {
		t.Fatalf("different AppKey reused installation id %s", oldInstallID)
	}

	resolver := identityResolver{q: db.New(pool)}
	_, err = resolver.ResolveSender(ctx, engine.ResolvedInstallation{
		ID:          row.ID,
		WorkspaceID: util.MustParseUUID(workspaceID),
	}, channel.InboundMessage{Source: channel.Source{SenderID: staffID}})
	if !errors.Is(err, engine.ErrSenderUnbound) {
		t.Fatalf("colliding staff id resolved after AppKey swap: %v, want ErrSenderUnbound", err)
	}

	queries := db.New(pool)
	if _, err := queries.GetChannelChatSessionBinding(ctx, db.GetChannelChatSessionBindingParams{
		InstallationID: row.ID,
		ChannelChatID:  chatID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old chat session reused by replacement: %v, want pgx.ErrNoRows", err)
	}

	assertCount := func(name, query string, want int, args ...any) {
		t.Helper()
		var got int
		if err := pool.QueryRow(ctx, query, args...).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		if got != want {
			t.Fatalf("%s rows = %d, want %d", name, got, want)
		}
	}
	assertCount("old installation", `SELECT count(*) FROM channel_installation WHERE id = $1`, 0, oldInstallID)
	assertCount("old user binding", `SELECT count(*) FROM channel_user_binding WHERE installation_id = $1`, 0, oldInstallID)
	assertCount("old chat binding", `SELECT count(*) FROM channel_chat_session_binding WHERE installation_id = $1`, 0, oldInstallID)
	assertCount("old task delivery", `SELECT count(*) FROM channel_task_delivery WHERE installation_id = $1`, 0, oldInstallID)
	assertCount("old route ledger", `SELECT count(*) FROM channel_outbound_message WHERE installation_id = $1`, 0, oldInstallID)
	assertCount("old chat context", `SELECT count(*) FROM channel_chat_context_generation WHERE chat_session_id = $1`, 1, chatSessionID)
	assertCount("old binding token", `SELECT count(*) FROM channel_binding_token WHERE installation_id = $1`, 0, oldInstallID)
	assertCount("old outbound state", `SELECT count(*) FROM channel_outbound_card_message WHERE chat_session_id = $1`, 0, chatSessionID)
	assertCount("old dedup state", `SELECT count(*) FROM channel_inbound_message_dedup WHERE installation_id = $1`, 0, oldInstallID)
	assertCount("detached audit", `SELECT count(*) FROM channel_inbound_audit WHERE channel_event_id = $1 AND installation_id IS NULL`, 1, auditEventID)
	assertCount("detached media intent", `SELECT count(*) FROM channel_media_pending_object WHERE storage_key = $1 AND installation_id IS NULL`, 1, storageKey)
}
