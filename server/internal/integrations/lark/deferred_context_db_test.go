package lark

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDeferredContextCrashRecoveryKeepsFallbackDB(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := channelScopeTestDB(t)
	fx := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	fx.UserID = fx.User(t, "Context sender", "context-"+suffix+"@example.invalid")
	fx.WorkspaceID = fx.Workspace(t, "Context recovery", "context-"+suffix)
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	runtimeID := fx.Runtime(t, "Context runtime")
	agentID := fx.Agent(t, "Context agent", runtimeID)
	sessionID := util.MustParseUUID(fx.ChatSession(t, agentID))
	installationID := util.MustParseUUID(fx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": fx.WorkspaceID, "agent_id": agentID, "channel_type": "feishu", "config": "{}", "installer_user_id": fx.UserID,
	}))
	q := db.New(pool)
	if _, err := q.CreateChannelChatSessionBinding(t.Context(), db.CreateChannelChatSessionBindingParams{
		ChatSessionID: sessionID, InstallationID: installationID, ChannelType: "feishu", ChannelChatID: suffix, ChatType: "p2p", Config: []byte("{}"),
	}); err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM channel_task_delivery WHERE installation_id=$1", installationID)
	fx.Cleanup(t, "DELETE FROM channel_chat_session_binding WHERE chat_session_id=$1", sessionID)
	fx.Cleanup(t, "DELETE FROM channel_chat_context_generation WHERE chat_session_id=$1", sessionID)
	fx.Cleanup(t, "DELETE FROM chat_message WHERE chat_session_id=$1", sessionID)
	fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE chat_session_id=$1", sessionID)
	client := newEnricherFake()
	prepared := NewInboundEnricher(client, InboundEnricherConfig{}).(*inboundEnricher).PrepareContext(InboundMessage{
		MessageID: "trigger", ParentID: "parent", Body: "inspect", CommandBody: "inspect",
	})
	if prepared.DeferredContext == nil || !strings.Contains(prepared.Body, "unable to fetch") {
		t.Fatal("preparation did not provide a durable fallback")
	}
	session := engine.NewChatSession(q, pool, channel.TypeFeishu, engine.SessionTitles{})
	appended, err := session.AppendUserMessage(t.Context(), engine.AppendInput{
		SessionID: sessionID, Sender: util.MustParseUUID(fx.UserID), Body: prepared.Body, MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	tasks := &service.TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	chat, err := q.GetChatSession(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.EnqueueChatTask(t.Context(), chat, util.MustParseUUID(fx.UserID), false)
	if err != nil || task.Status != "deferred" || !task.FireAt.Valid {
		t.Fatalf("context task was not durably deferred: %+v, %v", task, err)
	}
	// Recreate only the service: the connector and its in-memory job are gone.
	recovered := &service.TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	if err := recovered.PromoteDueDeferredTasksForRuntime(t.Context(), util.MustParseUUID(runtimeID)); err != nil {
		t.Fatal(err)
	}
	before, err := q.GetAgentTask(t.Context(), task.ID)
	if err != nil || before.Status != "deferred" {
		t.Fatalf("recovery released context before its deadline: %+v, %v", before, err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE agent_task_queue SET fire_at=now()-interval '1 second' WHERE id=$1", task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE chat_message SET channel_media_pending_until=now()-interval '1 second' WHERE id=$1", appended.MessageID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := recovered.PromoteDueDeferredTasksForRuntime(t.Context(), util.MustParseUUID(runtimeID)); err != nil {
			t.Fatal(err)
		}
	}
	var body, status string
	var owner string
	if err := pool.QueryRow(t.Context(), "SELECT content, task_id::text FROM chat_message WHERE id=$1", appended.MessageID).Scan(&body, &owner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT status FROM agent_task_queue WHERE id=$1", task.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1", sessionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if body != prepared.Body || status != "queued" || owner != util.UUIDToString(task.ID) || count != 1 || len(client.calls) != 0 {
		t.Fatal("recovery changed the source, replayed a remote read, or duplicated the accepted task")
	}
}

func TestDeferredClearPersistsFreshUntilNextTurnDB(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := channelScopeTestDB(t)
	fx := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	fx.UserID = fx.User(t, "Clear sender", "clear-"+suffix+"@example.invalid")
	fx.WorkspaceID = fx.Workspace(t, "Clear context", "clear-"+suffix)
	fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
	runtimeID := fx.Runtime(t, "Clear runtime")
	agentID := fx.Agent(t, "Clear agent", runtimeID)
	appID := "clear-" + suffix
	installationID := fx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": fx.WorkspaceID, "agent_id": agentID, "channel_type": "feishu",
		"config": `{"app_id":"` + appID + `","region":"feishu"}`, "installer_user_id": fx.UserID, "status": "active",
	})
	q := db.New(pool)
	store := NewChannelStore(q)
	if _, err := store.CreateLarkUserBinding(t.Context(), CreateUserBindingParams{
		WorkspaceID: util.MustParseUUID(fx.WorkspaceID), MulticaUserID: util.MustParseUUID(fx.UserID),
		InstallationID: util.MustParseUUID(installationID), ChannelUserID: "sender",
	}); err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM channel_task_delivery WHERE installation_id=$1", installationID)
	fx.Cleanup(t, "DELETE FROM channel_chat_context_generation WHERE chat_session_id IN (SELECT id FROM chat_session WHERE agent_id=$1)", agentID)
	fx.Cleanup(t, "DELETE FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE agent_id=$1)", agentID)
	fx.Cleanup(t, "DELETE FROM agent_task_queue WHERE agent_id=$1", agentID)
	fx.Cleanup(t, "DELETE FROM channel_user_binding WHERE installation_id=$1", installationID)
	fx.Cleanup(t, "DELETE FROM channel_chat_session_binding WHERE installation_id=$1", installationID)
	fx.Cleanup(t, "DELETE FROM channel_inbound_dedup WHERE installation_id=$1", installationID)
	client := newEnricherFake()
	client.byChat[ChatID(suffix)] = []LarkMessage{budgetImage("recent")}
	enricher := NewInboundEnricher(client, InboundEnricherConfig{RecentContextSize: DefaultRecentContextSize}).(*inboundEnricher)
	media := NewFeishuMediaResolver(&deferredContextClient{enricherFakeClient: client, media: &fakeSender{downloaded: DownloadedResource{Data: pngBytes, ContentType: "image/png"}}}, fakeCreds{secret: "plain"}, &fakeMediaStorage{}, engine.NewDBMediaIntentLedger(q), newDiscardLogger())
	session := engine.NewChatSession(q, pool, channel.TypeFeishu, engine.SessionTitles{})
	tasks := &service.TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	router := engine.NewRouter(nil, tasks, q, engine.RouterConfig{Logger: newDiscardLogger()})
	router.Register(channel.TypeFeishu, NewFeishuResolverSet(store, session, nil, nil, nil, media))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if !router.Drain(ctx) {
			t.Error("router did not drain")
		}
	})
	msg := InboundMessage{AppID: appID, MessageID: "clear", MessageType: "text", ChatID: ChatID(suffix), ChatType: ChatTypeGroup,
		SenderOpenID: "sender", AddressedToBot: true, Body: "/clear", CommandBody: "/clear", CreateTime: "2000"}
	if err := router.Handle(t.Context(), channelMessageFromLark(enricher.PrepareContext(msg))); err != nil {
		t.Fatal(err)
	}
	var messages, queued int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE agent_id=$1)", agentID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM agent_task_queue WHERE agent_id=$1", agentID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	var pending bool
	if err := pool.QueryRow(t.Context(), "SELECT pending_fresh FROM channel_chat_session_binding WHERE installation_id=$1", installationID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if messages != 0 || queued != 0 || !pending {
		t.Fatalf("bare clear created input or consumed fresh intent: messages=%d tasks=%d pending=%v", messages, queued, pending)
	}
	msg.MessageID, msg.Body, msg.CommandBody = "follow-up", "inspect", "inspect"
	if err := router.Handle(t.Context(), channelMessageFromLark(enricher.PrepareContext(msg))); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var unsettled int
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE agent_id=$1) AND channel_media_pending_until IS NOT NULL", agentID).Scan(&unsettled); err != nil {
			t.Fatal(err)
		}
		if unsettled == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("follow-up context did not finish before shutdown")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if !router.Drain(ctx) {
		t.Fatal("follow-up media did not drain")
	}
	var fresh bool
	if err := pool.QueryRow(t.Context(), "SELECT force_fresh_session FROM agent_task_queue WHERE agent_id=$1", agentID).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT pending_fresh FROM channel_chat_session_binding WHERE installation_id=$1", installationID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM agent_task_queue WHERE agent_id=$1", agentID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE agent_id=$1)", agentID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if !fresh || pending || queued != 1 || messages != 1 || len(client.listCalls) != 1 {
		t.Fatalf("next turn did not consume fresh intent exactly once: fresh=%v pending=%v tasks=%d messages=%d history=%d", fresh, pending, queued, messages, len(client.listCalls))
	}
}
