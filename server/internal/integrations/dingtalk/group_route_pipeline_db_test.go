package dingtalk

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDingTalkRejectedGroupCallbacksDoNotDiscoverRoutesDB(t *testing.T) {
	pool := dingtalkInstallTestDB(t)
	ctx := context.Background()
	var migrated bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.dingtalk_group_route') IS NOT NULL`).Scan(&migrated); err != nil || !migrated {
		t.Skip("dingtalk group route table not present (database not migrated)")
	}

	const (
		workspaceID  = "d2760000-0000-4000-8000-000000000001"
		runtimeID    = "d2760000-0000-4000-8000-000000000002"
		agentID      = "d2760000-0000-4000-8000-000000000003"
		installerID  = "d2760000-0000-4000-8000-000000000004"
		installation = "d2760000-0000-4000-8000-000000000005"
		boundUserID  = "d2760000-0000-4000-8000-000000000006"
		appKey       = "dingtalk_rejected_group_pipeline_app"
		staffID      = "staff-rejected-group-pipeline"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_inbound_audit WHERE installation_id = $1`, installation)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_inbound_message_dedup WHERE installation_id = $1`, installation)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_user_binding WHERE installation_id = $1`, installation)
		_, _ = pool.Exec(context.Background(), `DELETE FROM dingtalk_group_route WHERE installation_id = $1`, installation)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installation)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	}
	clean()
	t.Cleanup(clean)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed rejected callback fixture: %v", err)
		}
	}
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'DingTalk rejected routes', 'dingtalk-rejected-routes-db', '')`, workspaceID)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider) VALUES ($1, $2, 'DingTalk rejected route runtime', 'local', 'multica_daemon')`, runtimeID, workspaceID)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id) VALUES ($1, $2, 'DingTalk rejected route agent', 'local', $3)`, agentID, workspaceID, runtimeID)
	exec(`INSERT INTO channel_installation (id, workspace_id, agent_id, channel_type, config, installer_user_id) VALUES ($1, $2, $3, 'dingtalk', jsonb_build_object('app_id', $4::text), $5)`, installation, workspaceID, agentID, appKey, installerID)

	queries := db.New(pool)
	router := engine.NewRouter(nil, nil, nil, engine.RouterConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	router.Register(TypeDingTalk, NewDingTalkResolverSet(queries, pool, nil, nil, nil))
	raw, err := json.Marshal(dingtalkRawEvent{AppID: appKey, ConversationTitle: "Rejected group"})
	if err != nil {
		t.Fatal(err)
	}

	message := func(messageID, conversationID string, addressed bool) channel.InboundMessage {
		return channel.InboundMessage{
			EventID:        "event-" + messageID,
			MessageID:      messageID,
			Type:           channel.MsgTypeText,
			Text:           "hello",
			AddressedToBot: addressed,
			Raw:            raw,
			Source: channel.Source{
				ChannelType: TypeDingTalk,
				ChatID:      conversationID,
				ChatType:    channel.ChatTypeGroup,
				SenderID:    staffID,
			},
		}
	}
	assertNoRoutes := func(label string) {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM dingtalk_group_route WHERE installation_id = $1`, installation).Scan(&count); err != nil {
			t.Fatalf("%s: count routes: %v", label, err)
		}
		if count != 0 {
			t.Fatalf("%s persisted %d rejected group route(s)", label, count)
		}
	}

	if err := router.Handle(ctx, message("unmentioned", "cid-unmentioned", false)); err != nil {
		t.Fatalf("unmentioned callback: %v", err)
	}
	assertNoRoutes("unmentioned callback")

	if err := router.Handle(ctx, message("unbound", "cid-unbound", true)); err != nil {
		t.Fatalf("unbound callback: %v", err)
	}
	assertNoRoutes("unbound callback")

	exec(`INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id) VALUES ($1, $2, $3, 'dingtalk', $4)`, workspaceID, boundUserID, installation, staffID)
	if err := router.Handle(ctx, message("nonmember", "cid-nonmember", true)); err != nil {
		t.Fatalf("non-member callback: %v", err)
	}
	assertNoRoutes("non-member callback")
}
