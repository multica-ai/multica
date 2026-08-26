package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	slackintegration "github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	slackapi "github.com/slack-go/slack"
)

// This test keeps only the external IM transport outside the process. From the
// normalized inbound message onward it runs the production Router, shared chat
// session service, TaskService, PostgreSQL queue, and daemon claim handler.
// That is the repeatable server-side E2E boundary for a channel command: an
// adapter's only responsibility is translating its webhook/socket payload into
// channel.InboundMessage.
func TestChannelClearCommandE2EStartsFreshProviderSession(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler test fixture is required")
	}

	tests := []struct {
		name             string
		channelType      channel.Type
		text             string
		commandText      string
		forceFresh       bool
		followUpText     string
		wantStoredText   string
		wantForceFresh   bool
		wantPriorSession string
		wantPriorWorkDir string
	}{
		{
			name:             "normal message resumes existing provider context",
			channelType:      channel.Type("slack"),
			text:             "what model are you?",
			commandText:      "what model are you?",
			wantStoredText:   "what model are you?",
			wantPriorSession: "old-provider-session",
			wantPriorWorkDir: "/tmp/old-provider-workdir",
		},
		{
			name:           "bare /clear applies to the next real message",
			channelType:    channel.Type("slack"),
			text:           "/clear",
			commandText:    "/clear",
			followUpText:   "what model are you?",
			wantStoredText: "what model are you?",
			wantForceFresh: true,
		},
		{
			name:           "Slack /clear message starts without provider context",
			channelType:    channel.Type("slack"),
			text:           "/clear   what model are you?",
			commandText:    "/clear   what model are you?",
			wantStoredText: "what model are you?",
			wantForceFresh: true,
		},
		{
			name:           "Slack same-line /issue remains fresh-only",
			channelType:    channel.Type("slack"),
			text:           "/clear /issue investigate deploy",
			commandText:    "/clear /issue investigate deploy",
			wantStoredText: "/issue investigate deploy",
			wantForceFresh: true,
		},
		{
			name:           "Slack next-line /issue remains fresh-only",
			channelType:    channel.Type("slack"),
			text:           "/clear\n/issue investigate deploy",
			commandText:    "/clear\n/issue investigate deploy",
			wantStoredText: "/issue investigate deploy",
			wantForceFresh: true,
		},
		{
			name:           "Feishu same-line /issue remains fresh-only",
			channelType:    channel.TypeFeishu,
			text:           "/issue investigate deploy",
			commandText:    "/clear /issue investigate deploy",
			forceFresh:     true,
			wantStoredText: "/issue investigate deploy",
			wantForceFresh: true,
		},
		{
			name:           "Feishu next-line /issue remains fresh-only",
			channelType:    channel.TypeFeishu,
			text:           "/issue investigate deploy",
			commandText:    "/clear\n/issue investigate deploy",
			forceFresh:     true,
			wantStoredText: "/issue investigate deploy",
			wantForceFresh: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runChannelClearCommandE2E(t, tt.channelType, tt.text, tt.commandText, tt.forceFresh, tt.followUpText, tt.wantStoredText, tt.wantForceFresh, tt.wantPriorSession, tt.wantPriorWorkDir)
		})
	}
}

// /new changes only the future inbound route. A task already queued from the
// old generation must keep both its Multica Chat and exact external reply
// target, while the new command gets a distinct Chat and delivery snapshot.
func TestChannelChatCommandE2ERotatesRouteAndFreezesTaskDelivery(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler test fixture is required")
	}

	ctx := context.Background()
	agentID, runtimeID, _ := createRuntimeGuardAgent(t, ctx)
	queries := db.New(testPool)
	channelType := channel.Type("slack")
	bindingKey := "chat-route-" + t.Name()

	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, config, status, installer_user_id
		) VALUES ($1, $2, $3, '{}'::jsonb, 'active', $4)
		RETURNING id
	`, testWorkspaceID, agentID, string(channelType), testUserID).Scan(&installationID); err != nil {
		t.Fatalf("create channel installation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_task_delivery WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_outbound_message WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_inbound_message_dedup WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})

	installation := engine.ResolvedInstallation{
		ID: util.MustParseUUID(installationID), WorkspaceID: util.MustParseUUID(testWorkspaceID),
		AgentID: util.MustParseUUID(agentID), InstallerUserID: util.MustParseUUID(testUserID), Active: true,
	}
	chatSession := engine.NewChatSession(queries, testPool, channelType, engine.SessionTitles{
		Direct: "Channel E2E conversation", Group: "Channel E2E group", Fallback: "Channel E2E conversation",
	})
	binder := &channelNewE2ESessionBinder{session: chatSession}
	router := engine.NewRouter(nil, testHandler.TaskService, queries, engine.RouterConfig{})
	// Hold ordinary runs in the debounce window until after /new retires the
	// old route. Drain below then proves that the captured old generation still
	// seals and receives the pending input instead of silently dropping it.
	router.EnableRunBatching(time.Hour)
	router.Register(channelType, engine.ResolverSet{
		Installation: channelNewE2EInstallationResolver{installation: installation},
		Identity:     channelNewE2EIdentityResolver{userID: util.MustParseUUID(testUserID)},
		Dedup:        channelNewE2EDeduper{queries: queries}, Session: binder,
		Audit: channelNewE2EAuditor{}, OriginType: "test_e2e_chat",
	})

	oldSessionID, err := binder.EnsureSession(ctx, engine.EnsureSessionParams{
		Installation: installation, Sender: util.MustParseUUID(testUserID),
		Message: channel.InboundMessage{Source: channel.Source{
			ChannelType: channelType, ChatID: bindingKey, ChatType: channel.ChatTypeP2P,
		}},
	})
	if err != nil {
		t.Fatalf("seed old channel Chat: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE chat_session
		SET title = 'Old topic', session_id = 'old-provider-session',
		    work_dir = '/tmp/old-provider-workdir', runtime_id = $2
		WHERE id = $1
	`, oldSessionID, runtimeID); err != nil {
		t.Fatalf("seed old Chat state: %v", err)
	}

	send := func(eventID, messageID, text string) {
		t.Helper()
		if err := router.Handle(ctx, channel.InboundMessage{
			EventID: eventID, MessageID: messageID, Type: channel.MsgTypeText,
			Text: text, CommandText: text,
			Source: channel.Source{ChannelType: channelType, ChatID: bindingKey,
				ChatType: channel.ChatTypeP2P, SenderID: "platform-user-e2e"},
		}); err != nil {
			t.Fatalf("route %q: %v", text, err)
		}
	}
	send("event-old", "message-old", "finish the old topic")
	send("event-chat", "message-chat", "/new investigate the new topic")

	var newSessionID, newBindingID string
	var newRevision int64
	if err := testPool.QueryRow(ctx, `
		SELECT chat_session_id, id, route_revision
		FROM channel_chat_session_binding
		WHERE installation_id = $1 AND channel_chat_id = $2 AND retired_at IS NULL
	`, installationID, bindingKey).Scan(&newSessionID, &newBindingID, &newRevision); err != nil {
		t.Fatalf("load current route: %v", err)
	}
	if newSessionID == util.UUIDToString(oldSessionID) {
		t.Fatal("/new kept the old chat_session instead of creating a new one")
	}
	if newRevision != 2 {
		t.Fatalf("new route revision = %d, want 2", newRevision)
	}

	var oldBindingID, oldEndMessageID string
	var oldRevision int64
	if err := testPool.QueryRow(ctx, `
		SELECT id, route_revision, history_end_message_id
		FROM channel_chat_session_binding
		WHERE chat_session_id = $1 AND retired_at IS NOT NULL
	`, oldSessionID).Scan(&oldBindingID, &oldRevision, &oldEndMessageID); err != nil {
		t.Fatalf("load retired route: %v", err)
	}
	if oldRevision != 1 || oldEndMessageID != "message-chat" {
		t.Fatalf("retired route revision/end = %d/%q, want 1/message-chat", oldRevision, oldEndMessageID)
	}
	if !router.Drain(ctx) {
		t.Fatal("drain timed out while flushing the pre-/new debounce window")
	}

	assertTaskDelivery := func(sessionID, wantBindingID, wantTarget string) {
		t.Helper()
		var taskID, taskStatus, bindingID, target string
		if err := testPool.QueryRow(ctx, `
			SELECT task.id, task.status, delivery.binding_id, delivery.channel_message_id
			FROM agent_task_queue AS task
			JOIN channel_task_delivery AS delivery ON delivery.task_id = task.id
			WHERE task.chat_session_id = $1
			ORDER BY task.created_at DESC LIMIT 1
		`, sessionID).Scan(&taskID, &taskStatus, &bindingID, &target); err != nil {
			t.Fatalf("load task delivery for %s: %v", sessionID, err)
		}
		if bindingID != wantBindingID || target != wantTarget {
			t.Fatalf("task %s delivery = %s/%s, want %s/%s", taskID, bindingID, target, wantBindingID, wantTarget)
		}
		if taskStatus != "queued" {
			t.Fatalf("task %s status = %s, want queued; /new must not cancel old work", taskID, taskStatus)
		}
	}
	assertTaskDelivery(util.UUIDToString(oldSessionID), oldBindingID, "message-old")
	assertTaskDelivery(newSessionID, newBindingID, "message-chat")

	var oldTitle, oldProviderSession, oldWorkDir, oldStatus string
	if err := testPool.QueryRow(ctx, `
		SELECT title, session_id, work_dir, status FROM chat_session WHERE id = $1
	`, oldSessionID).Scan(&oldTitle, &oldProviderSession, &oldWorkDir, &oldStatus); err != nil {
		t.Fatalf("load old Chat: %v", err)
	}
	if oldTitle != "finish the old topic" || oldProviderSession != "old-provider-session" || oldWorkDir != "/tmp/old-provider-workdir" || oldStatus != "active" {
		t.Fatalf("old Chat mutated: title=%q session=%q workdir=%q status=%q", oldTitle, oldProviderSession, oldWorkDir, oldStatus)
	}

	var newTitle string
	var newProviderSession, newWorkDir pgtype.Text
	var explicitlyCreated bool
	if err := testPool.QueryRow(ctx, `
		SELECT title, session_id, work_dir, explicitly_created_at IS NOT NULL
		FROM chat_session WHERE id = $1
	`, newSessionID).Scan(&newTitle, &newProviderSession, &newWorkDir, &explicitlyCreated); err != nil {
		t.Fatalf("load new Chat: %v", err)
	}
	if newTitle != "investigate the new topic" || newProviderSession.Valid || newWorkDir.Valid || !explicitlyCreated {
		t.Fatalf("new Chat state: title=%q session=%v workdir=%v explicit=%t", newTitle, newProviderSession, newWorkDir, explicitlyCreated)
	}
	var storedNewText string
	if err := testPool.QueryRow(ctx, `
		SELECT content FROM chat_message
		WHERE chat_session_id = $1 AND role = 'user' ORDER BY created_at ASC LIMIT 1
	`, newSessionID).Scan(&storedNewText); err != nil {
		t.Fatalf("load new Chat first message: %v", err)
	}
	if storedNewText != "investigate the new topic" {
		t.Fatalf("new Chat first message = %q", storedNewText)
	}

	// A bare /new is also a durable route rotation, but it creates neither a
	// synthetic user message nor a task. Its explicit-create marker is the
	// authority that makes the empty Chat visible in normal list queries.
	send("event-empty-chat", "message-empty-chat", "/new")
	var emptySessionID string
	var emptyRevision int64
	if err := testPool.QueryRow(ctx, `
		SELECT chat_session_id, route_revision
		FROM channel_chat_session_binding
		WHERE installation_id = $1 AND channel_chat_id = $2 AND retired_at IS NULL
	`, installationID, bindingKey).Scan(&emptySessionID, &emptyRevision); err != nil {
		t.Fatalf("load bare /new route: %v", err)
	}
	if emptyRevision != 3 || emptySessionID == newSessionID {
		t.Fatalf("bare /new route = session %s revision %d, want a third Chat/revision", emptySessionID, emptyRevision)
	}
	var emptyTitle string
	var emptyExplicit bool
	var emptyMessages, emptyTasks int
	if err := testPool.QueryRow(ctx, `
		SELECT session.title, session.explicitly_created_at IS NOT NULL,
		       (SELECT count(*) FROM chat_message WHERE chat_session_id = session.id),
		       (SELECT count(*) FROM agent_task_queue WHERE chat_session_id = session.id)
		FROM chat_session AS session WHERE session.id = $1
	`, emptySessionID).Scan(&emptyTitle, &emptyExplicit, &emptyMessages, &emptyTasks); err != nil {
		t.Fatalf("load bare /new Chat: %v", err)
	}
	if emptyTitle != "" || !emptyExplicit || emptyMessages != 0 || emptyTasks != 0 {
		t.Fatalf("bare /new state = title %q explicit %t messages %d tasks %d", emptyTitle, emptyExplicit, emptyMessages, emptyTasks)
	}
	listed, err := queries.ListChatSessionsByCreator(ctx, db.ListChatSessionsByCreatorParams{
		WorkspaceID: util.MustParseUUID(testWorkspaceID), CreatorID: util.MustParseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("list Chats after bare /new: %v", err)
	}
	visible := false
	for _, item := range listed {
		if util.UUIDToString(item.ID) == emptySessionID {
			visible = true
			break
		}
	}
	if !visible {
		t.Fatal("bare /new Chat is absent from the normal Chat list")
	}
	metadata := []ChatSessionResponse{
		{ID: util.UUIDToString(oldSessionID)},
		{ID: newSessionID},
		{ID: emptySessionID},
	}
	if err := testHandler.hydrateChatSessionChannelMetadata(ctx, metadata); err != nil {
		t.Fatalf("hydrate Chat channel metadata: %v", err)
	}
	for i, wantRevision := range []int64{1, 2, 3} {
		if metadata[i].ChannelSource == nil ||
			metadata[i].ChannelSource.ChannelType != string(channelType) ||
			metadata[i].ChannelSource.InstallationID != installationID ||
			metadata[i].ChannelSource.RouteRevision != wantRevision {
			t.Fatalf("Chat %d source metadata = %+v, want revision %d", i, metadata[i].ChannelSource, wantRevision)
		}
		wantCurrent := i == 2
		if metadata[i].IsCurrentChannelRoute == nil || *metadata[i].IsCurrentChannelRoute != wantCurrent {
			t.Fatalf("Chat %d current route = %v, want %t", i, metadata[i].IsCurrentChannelRoute, wantCurrent)
		}
	}

	// The empty channel-created Chat remains writable from Multica. Its first
	// direct turn initializes the same deterministic title, while the direct
	// task deliberately gets no external delivery snapshot.
	emptySessionUUID := util.MustParseUUID(emptySessionID)
	emptySession, err := queries.GetChatSession(ctx, emptySessionUUID)
	if err != nil {
		t.Fatalf("load empty Chat for direct send: %v", err)
	}
	agent, err := queries.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent for direct send: %v", err)
	}
	direct, err := testHandler.TaskService.SendDirectChatMessage(ctx, emptySession, agent, util.MustParseUUID(testUserID), "continue from the web client", nil, "member", util.MustParseUUID(testUserID))
	if err != nil {
		t.Fatalf("direct send to channel-created Chat: %v", err)
	}
	var directTitle string
	var deliveryCount int
	if err := testPool.QueryRow(ctx, `
		SELECT session.title,
		       (SELECT count(*) FROM channel_task_delivery WHERE task_id = $2)
		FROM chat_session AS session WHERE session.id = $1
	`, emptySessionUUID, direct.Task.ID).Scan(&directTitle, &deliveryCount); err != nil {
		t.Fatalf("load direct-send title/delivery: %v", err)
	}
	if directTitle != "continue from the web client" || direct.InitialTitle != directTitle || deliveryCount != 0 {
		t.Fatalf("direct continuation title=%q initial=%q delivery=%d", directTitle, direct.InitialTitle, deliveryCount)
	}

	// The same cross-client path must also title an attachment-only first
	// message from the first file that was actually bound.
	send("event-attachment-chat", "message-attachment-chat", "/new")
	var attachmentSessionID string
	if err := testPool.QueryRow(ctx, `
		SELECT chat_session_id FROM channel_chat_session_binding
		WHERE installation_id = $1 AND channel_chat_id = $2 AND retired_at IS NULL
	`, installationID, bindingKey).Scan(&attachmentSessionID); err != nil {
		t.Fatalf("load attachment-only Chat: %v", err)
	}
	var attachmentID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (
			workspace_id, chat_session_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		) VALUES ($1, $2, 'member', $3, 'quarterly-plan.pdf', 'file:///tmp/quarterly-plan.pdf', 'application/pdf', 1)
		RETURNING id
	`, testWorkspaceID, attachmentSessionID, testUserID).Scan(&attachmentID); err != nil {
		t.Fatalf("create direct attachment: %v", err)
	}
	attachmentSession, err := queries.GetChatSession(ctx, util.MustParseUUID(attachmentSessionID))
	if err != nil {
		t.Fatalf("load attachment-only Chat session: %v", err)
	}
	attachmentSend, err := testHandler.TaskService.SendDirectChatMessage(
		ctx, attachmentSession, agent, util.MustParseUUID(testUserID), "", []pgtype.UUID{attachmentID},
		"member", util.MustParseUUID(testUserID),
	)
	if err != nil {
		t.Fatalf("send attachment-only first message: %v", err)
	}
	var attachmentTitle string
	if err := testPool.QueryRow(ctx, `SELECT title FROM chat_session WHERE id = $1`, attachmentSessionID).Scan(&attachmentTitle); err != nil {
		t.Fatalf("load attachment-only title: %v", err)
	}
	if attachmentTitle != "quarterly-plan.pdf" || attachmentSend.InitialTitle != attachmentTitle {
		t.Fatalf("attachment-only title=%q initial=%q", attachmentTitle, attachmentSend.InitialTitle)
	}
}

func TestChannelChatStaleTaskPreparationLeavesRouteAndChatUntouched(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler test fixture is required")
	}

	ctx := context.Background()
	agentID, _, _ := createRuntimeGuardAgent(t, ctx)
	queries := db.New(testPool)
	bindingKey := "chat-atomic-" + t.Name()
	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, config, status, installer_user_id
		) VALUES ($1, $2, 'slack', '{}'::jsonb, 'active', $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&installationID); err != nil {
		t.Fatalf("create channel installation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_task_delivery WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})

	sessions := engine.NewChatSession(queries, testPool, channel.Type("slack"), engine.SessionTitles{})
	oldSessionID, err := sessions.EnsureSession(ctx, engine.EnsureSessionInput{
		WorkspaceID: util.MustParseUUID(testWorkspaceID), AgentID: util.MustParseUUID(agentID),
		InstallationID: util.MustParseUUID(installationID), Sender: util.MustParseUUID(testUserID),
		BindingKey: bindingKey, ChatType: channel.ChatTypeP2P,
	})
	if err != nil {
		t.Fatalf("seed current Chat: %v", err)
	}
	var beforeCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM chat_session WHERE agent_id = $1`, agentID).Scan(&beforeCount); err != nil {
		t.Fatalf("count Chats before start: %v", err)
	}

	prepared, err := testHandler.TaskService.PrepareChatTaskEnqueue(ctx, util.MustParseUUID(agentID), util.MustParseUUID(testUserID))
	if err != nil {
		t.Fatalf("prepare task before runtime change: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, agentID); err != nil {
		t.Fatalf("detach agent runtime after preparation: %v", err)
	}
	_, err = sessions.StartSession(ctx, engine.StartSessionInput{
		EnsureSessionInput: engine.EnsureSessionInput{
			WorkspaceID: util.MustParseUUID(testWorkspaceID), AgentID: util.MustParseUUID(agentID),
			InstallationID: util.MustParseUUID(installationID), Sender: util.MustParseUUID(testUserID),
			BindingKey: bindingKey, ChatType: channel.ChatTypeP2P,
		},
		Initiator: util.MustParseUUID(testUserID),
		Body:      "new topic", MessageID: "message-new-topic", PersistMessage: true,
		BeforeCommit: func(ctx context.Context, tx pgx.Tx, session db.ChatSession) error {
			_, enqueueErr := testHandler.TaskService.EnqueuePreparedChannelChatTaskInTx(
				ctx, tx, session, util.MustParseUUID(testUserID), false, 1, prepared,
			)
			return enqueueErr
		},
	})
	if !errors.Is(err, service.ErrChatTaskAgentNoRuntime) {
		t.Fatalf("start with stale preparation error = %v, want no-runtime rejection", err)
	}

	var currentSessionID string
	var revision int64
	if err := testPool.QueryRow(ctx, `
		SELECT chat_session_id, route_revision
		FROM channel_chat_session_binding
		WHERE installation_id = $1 AND channel_chat_id = $2 AND retired_at IS NULL
	`, installationID, bindingKey).Scan(&currentSessionID, &revision); err != nil {
		t.Fatalf("load current route after rollback: %v", err)
	}
	var afterCount, taskCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM chat_session WHERE agent_id = $1`, agentID).Scan(&afterCount); err != nil {
		t.Fatalf("count Chats after start: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, agentID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks after start: %v", err)
	}
	if currentSessionID != util.UUIDToString(oldSessionID) || revision != 1 || afterCount != beforeCount || taskCount != 0 {
		t.Fatalf("partial commit: current=%s revision=%d chats=%d/%d tasks=%d", currentSessionID, revision, afterCount, beforeCount, taskCount)
	}
}

func TestSlackNativeNewCommandCreatesMessageTaskAndDeliveryAtomically(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler test fixture is required")
	}

	ctx := context.Background()
	agentID, _, _ := createRuntimeGuardAgent(t, ctx)
	queries := db.New(testPool)
	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, config, status, installer_user_id
		) VALUES ($1, $2, 'slack', '{}'::jsonb, 'active', $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&installationID); err != nil {
		t.Fatalf("create channel installation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_task_delivery WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_inbound_message_dedup WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})

	installation := engine.ResolvedInstallation{
		ID: util.MustParseUUID(installationID), WorkspaceID: util.MustParseUUID(testWorkspaceID),
		AgentID: util.MustParseUUID(agentID), InstallerUserID: util.MustParseUUID(testUserID), Active: true,
	}
	sessions := engine.NewChatSession(queries, testPool, channel.Type("slack"), engine.SessionTitles{})
	starter := slackintegration.NewSlackDMControlStarter(queries, testPool, testHandler.TaskService, testHandler)
	if err := starter.StartSlackDMChat(ctx, installation, util.MustParseUUID(testUserID), slackapi.SlashCommand{
		ChannelID: "D-native-chat", Text: "investigate native command",
	}, "envelope-native-chat"); err != nil {
		t.Fatalf("StartSlackDMChat: %v", err)
	}

	var sessionID, title, content string
	var routeRevision int64
	var taskCount, deliveryCount int
	if err := testPool.QueryRow(ctx, `
		SELECT binding.chat_session_id, binding.route_revision, session.title,
		       message.content,
		       (SELECT count(*) FROM agent_task_queue AS task WHERE task.chat_session_id = session.id),
		       (SELECT count(*) FROM channel_task_delivery AS delivery
		        JOIN agent_task_queue AS task ON task.id = delivery.task_id
		        WHERE task.chat_session_id = session.id)
		FROM channel_chat_session_binding AS binding
		JOIN chat_session AS session ON session.id = binding.chat_session_id
		JOIN chat_message AS message ON message.chat_session_id = session.id AND message.role = 'user'
		WHERE binding.installation_id = $1 AND binding.channel_chat_id = 'D-native-chat'
		  AND binding.retired_at IS NULL
	`, installationID).Scan(&sessionID, &routeRevision, &title, &content, &taskCount, &deliveryCount); err != nil {
		t.Fatalf("load native /new state: %v", err)
	}
	if sessionID == "" || routeRevision != 1 || title != "investigate native command" || content != "investigate native command" || taskCount != 1 || deliveryCount != 1 {
		t.Fatalf("native /new state: session=%s revision=%d title=%q content=%q tasks=%d deliveries=%d", sessionID, routeRevision, title, content, taskCount, deliveryCount)
	}
	var dedupProcessed bool
	if err := testPool.QueryRow(ctx, `
		SELECT processed_at IS NOT NULL FROM channel_inbound_message_dedup
		WHERE installation_id = $1 AND message_id = 'envelope-native-chat'
	`, installationID).Scan(&dedupProcessed); err != nil {
		t.Fatalf("load native /new dedup: %v", err)
	}
	if !dedupProcessed {
		t.Fatal("native /new dedup was not marked processed")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, envelopeID := range []string{"envelope-native-chat-2", "envelope-native-chat-3"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			errs <- starter.StartSlackDMChat(ctx, installation, util.MustParseUUID(testUserID), slackapi.SlashCommand{
				ChannelID: "D-native-chat",
			}, id)
		}(envelopeID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent bare /new: %v", err)
		}
	}
	var finalRevision int64
	var finalSessionID string
	var generations, activeRoutes int
	if err := testPool.QueryRow(ctx, `
		SELECT max(route_revision), count(*), count(*) FILTER (WHERE retired_at IS NULL),
		       max(chat_session_id::text) FILTER (WHERE retired_at IS NULL)
		FROM channel_chat_session_binding
		WHERE installation_id = $1 AND channel_chat_id = 'D-native-chat'
	`, installationID).Scan(&finalRevision, &generations, &activeRoutes, &finalSessionID); err != nil {
		t.Fatalf("load concurrent /new generations: %v", err)
	}
	if finalRevision != 3 || generations != 3 || activeRoutes != 1 {
		t.Fatalf("concurrent /new generations: max=%d count=%d active=%d", finalRevision, generations, activeRoutes)
	}
	if _, err := sessions.AppendUserMessage(ctx, engine.AppendInput{
		SessionID: util.MustParseUUID(finalSessionID), Sender: util.MustParseUUID(testUserID),
		Body: "first public message", MessageID: "1700000000.000001",
	}); err != nil {
		t.Fatalf("append first message after native /new: %v", err)
	}
	var startID string
	var pending bool
	var closedRetired, openRetired int
	if err := testPool.QueryRow(ctx, `
		SELECT current.history_start_message_id, current.history_boundary_pending,
		       count(*) FILTER (
		           WHERE previous.retired_at IS NOT NULL
		             AND previous.history_end_message_id = current.history_start_message_id
		       ),
		       count(*) FILTER (
		           WHERE previous.retired_at IS NOT NULL
		             AND previous.history_end_message_id IS NULL
		       )
		FROM channel_chat_session_binding AS current
		JOIN channel_chat_session_binding AS previous
		  ON previous.installation_id = current.installation_id
		 AND previous.channel_chat_id = current.channel_chat_id
		WHERE current.chat_session_id = $1
		GROUP BY current.history_start_message_id, current.history_boundary_pending
	`, finalSessionID).Scan(&startID, &pending, &closedRetired, &openRetired); err != nil {
		t.Fatalf("load established native history boundary: %v", err)
	}
	if startID != "1700000000.000001" || pending || closedRetired != 2 || openRetired != 0 {
		t.Fatalf("native boundary start=%q pending=%t closed_retired=%d open_retired=%d", startID, pending, closedRetired, openRetired)
	}
}

func TestSlackNativeClearCommandKeepsChatAndAdvancesContextAtomically(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler test fixture is required")
	}

	ctx := context.Background()
	agentID, _, _ := createRuntimeGuardAgent(t, ctx)
	queries := db.New(testPool)
	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, config, status, installer_user_id
		) VALUES ($1, $2, 'slack', '{}'::jsonb, 'active', $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&installationID); err != nil {
		t.Fatalf("create channel installation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_task_delivery WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_inbound_message_dedup WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})

	installation := engine.ResolvedInstallation{
		ID: util.MustParseUUID(installationID), WorkspaceID: util.MustParseUUID(testWorkspaceID),
		AgentID: util.MustParseUUID(agentID), InstallerUserID: util.MustParseUUID(testUserID), Active: true,
	}
	starter := slackintegration.NewSlackDMControlStarter(queries, testPool, testHandler.TaskService, testHandler)
	if err := starter.ClearSlackDMContext(ctx, installation, util.MustParseUUID(testUserID), slackapi.SlashCommand{
		ChannelID: "D-native-clear", Text: "/issue investigate native clear",
	}, "envelope-native-clear-message"); err != nil {
		t.Fatalf("ClearSlackDMContext with message: %v", err)
	}

	var sessionID, content, messageKind string
	var routeRevision, contextRevision int64
	var forceFresh, boundaryPending, dedupProcessed bool
	var taskCount, deliveryCount, routeCount int
	if err := testPool.QueryRow(ctx, `
		SELECT binding.chat_session_id, binding.route_revision, binding.context_revision,
		       message.content, COALESCE(message.message_kind, ''), task.force_fresh_session,
		       generation.history_boundary_pending,
		       (SELECT count(*) FROM agent_task_queue AS queued WHERE queued.chat_session_id = binding.chat_session_id),
		       (SELECT count(*) FROM channel_task_delivery AS delivery WHERE delivery.task_id = task.id),
		       (SELECT count(*) FROM channel_chat_session_binding AS routes
		        WHERE routes.installation_id = binding.installation_id
		          AND routes.channel_chat_id = binding.channel_chat_id),
		       dedup.processed_at IS NOT NULL
		FROM channel_chat_session_binding AS binding
		JOIN channel_chat_context_generation AS generation
		  ON generation.chat_session_id = binding.chat_session_id
		 AND generation.revision = binding.context_revision
		JOIN chat_message AS message ON message.chat_session_id = binding.chat_session_id AND message.role = 'user'
		JOIN agent_task_queue AS task ON task.chat_session_id = binding.chat_session_id
		JOIN channel_inbound_message_dedup AS dedup
		  ON dedup.installation_id = binding.installation_id
		 AND dedup.message_id = 'envelope-native-clear-message'
		WHERE binding.installation_id = $1 AND binding.channel_chat_id = 'D-native-clear'
		  AND binding.retired_at IS NULL
	`, installationID).Scan(
		&sessionID, &routeRevision, &contextRevision, &content, &messageKind, &forceFresh,
		&boundaryPending, &taskCount, &deliveryCount, &routeCount, &dedupProcessed,
	); err != nil {
		t.Fatalf("load native /clear message state: %v", err)
	}
	if sessionID == "" || routeRevision != 1 || contextRevision != 2 || content != "/issue investigate native clear" || messageKind != "message" || !forceFresh || !boundaryPending || taskCount != 1 || deliveryCount != 1 || routeCount != 1 || !dedupProcessed {
		t.Fatalf("native /clear message state: session=%s route=%d context=%d content=%q kind=%q fresh=%t pending=%t tasks=%d deliveries=%d routes=%d dedup=%t", sessionID, routeRevision, contextRevision, content, messageKind, forceFresh, boundaryPending, taskCount, deliveryCount, routeCount, dedupProcessed)
	}

	if err := starter.ClearSlackDMContext(ctx, installation, util.MustParseUUID(testUserID), slackapi.SlashCommand{
		ChannelID: "D-native-clear",
	}, "envelope-native-clear-bare"); err != nil {
		t.Fatalf("bare ClearSlackDMContext: %v", err)
	}
	var currentSessionID string
	var currentContextRevision int64
	var currentBoundaryPending, bareDedupProcessed bool
	var currentTaskCount, currentMessageCount, currentRouteCount int
	if err := testPool.QueryRow(ctx, `
		SELECT binding.chat_session_id, binding.context_revision,
		       generation.history_boundary_pending,
		       (SELECT count(*) FROM agent_task_queue AS task WHERE task.chat_session_id = binding.chat_session_id),
		       (SELECT count(*) FROM chat_message AS message WHERE message.chat_session_id = binding.chat_session_id AND message.role = 'user'),
		       (SELECT count(*) FROM channel_chat_session_binding AS routes
		        WHERE routes.installation_id = binding.installation_id
		          AND routes.channel_chat_id = binding.channel_chat_id),
		       dedup.processed_at IS NOT NULL
		FROM channel_chat_session_binding AS binding
		JOIN channel_chat_context_generation AS generation
		  ON generation.chat_session_id = binding.chat_session_id
		 AND generation.revision = binding.context_revision
		JOIN channel_inbound_message_dedup AS dedup
		  ON dedup.installation_id = binding.installation_id
		 AND dedup.message_id = 'envelope-native-clear-bare'
		WHERE binding.installation_id = $1 AND binding.channel_chat_id = 'D-native-clear'
		  AND binding.retired_at IS NULL
	`, installationID).Scan(
		&currentSessionID, &currentContextRevision, &currentBoundaryPending,
		&currentTaskCount, &currentMessageCount, &currentRouteCount, &bareDedupProcessed,
	); err != nil {
		t.Fatalf("load bare native /clear state: %v", err)
	}
	if currentSessionID != sessionID || currentContextRevision != 3 || !currentBoundaryPending || currentTaskCount != 1 || currentMessageCount != 1 || currentRouteCount != 1 || !bareDedupProcessed {
		t.Fatalf("bare native /clear state: session=%s/%s context=%d pending=%t tasks=%d messages=%d routes=%d dedup=%t", currentSessionID, sessionID, currentContextRevision, currentBoundaryPending, currentTaskCount, currentMessageCount, currentRouteCount, bareDedupProcessed)
	}
}

func runChannelClearCommandE2E(t *testing.T, channelType channel.Type, text, commandText string, adapterForceFresh bool, followUpText, wantStoredText string, wantForceFresh bool, wantPriorSession, wantPriorWorkDir string) {
	t.Helper()

	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	queries := db.New(testPool)

	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (
			workspace_id, agent_id, channel_type, config, status, installer_user_id
		)
		VALUES ($1, $2, $3, '{}'::jsonb, 'active', $4)
		RETURNING id
	`, testWorkspaceID, agentID, string(channelType), testUserID).Scan(&installationID); err != nil {
		t.Fatalf("create channel installation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_task_delivery WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_outbound_message WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_inbound_message_dedup WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})

	installation := engine.ResolvedInstallation{
		ID:              util.MustParseUUID(installationID),
		WorkspaceID:     util.MustParseUUID(testWorkspaceID),
		AgentID:         util.MustParseUUID(agentID),
		InstallerUserID: util.MustParseUUID(testUserID),
		Active:          true,
	}
	chatSession := engine.NewChatSession(queries, testPool, channelType, engine.SessionTitles{
		Direct:   "Channel E2E conversation",
		Group:    "Channel E2E group",
		Fallback: "Channel E2E conversation",
	})
	binder := &channelNewE2ESessionBinder{session: chatSession}

	// Seed the same channel conversation with a provider session/workdir. A
	// normal message resumes both; /clear must leave them out of the daemon
	// claim while retaining the durable chat transcript.
	sessionID, err := binder.EnsureSession(ctx, engine.EnsureSessionParams{
		Installation: installation,
		Sender:       util.MustParseUUID(testUserID),
		Message: channel.InboundMessage{Source: channel.Source{
			ChannelType: channelType,
			ChatID:      t.Name(),
			ChatType:    channel.ChatTypeP2P,
		}},
	})
	if err != nil {
		t.Fatalf("seed channel chat session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE origin_type = 'test_e2e_chat' AND origin_id = $1`, sessionID)
	})
	if _, err := testPool.Exec(ctx, `
		UPDATE chat_session
		SET session_id = 'old-provider-session',
		    work_dir = '/tmp/old-provider-workdir',
		    runtime_id = $2
		WHERE id = $1
	`, sessionID, runtimeID); err != nil {
		t.Fatalf("seed prior provider context: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, chat_session_id, status, priority,
			started_at, completed_at, session_id, work_dir,
			channel_context_revision
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(),
		        'old-provider-session', '/tmp/old-provider-workdir', 1)
	`, agentID, runtimeID, sessionID); err != nil {
		t.Fatalf("seed prior provider task: %v", err)
	}

	router := engine.NewRouter(nil, testHandler.TaskService, queries, engine.RouterConfig{})
	router.Register(channelType, engine.ResolverSet{
		Installation: channelNewE2EInstallationResolver{installation: installation},
		Identity:     channelNewE2EIdentityResolver{userID: util.MustParseUUID(testUserID)},
		Dedup:        channelNewE2EDeduper{queries: queries},
		Session:      binder,
		Audit:        channelNewE2EAuditor{},
		OriginType:   "test_e2e_chat",
	})

	if err := router.Handle(ctx, channel.InboundMessage{
		EventID:     "event-" + t.Name(),
		MessageID:   "message-" + t.Name(),
		Type:        channel.MsgTypeText,
		Text:        text,
		CommandText: commandText,
		ForceFresh:  adapterForceFresh,
		Source: channel.Source{
			ChannelType: channelType,
			ChatID:      t.Name(),
			ChatType:    channel.ChatTypeP2P,
			SenderID:    "platform-user-e2e",
		},
	}); err != nil {
		t.Fatalf("route channel message: %v", err)
	}
	if followUpText != "" {
		var taskCount, userMessageCount int
		var pendingFresh bool
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_task_queue
			WHERE chat_session_id = $1 AND status NOT IN ('completed', 'failed', 'cancelled')
		`, sessionID).Scan(&taskCount); err != nil {
			t.Fatalf("count tasks after bare /clear: %v", err)
		}
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM chat_message WHERE chat_session_id = $1 AND role = 'user'`, sessionID).Scan(&userMessageCount); err != nil {
			t.Fatalf("count messages after bare /clear: %v", err)
		}
		if err := testPool.QueryRow(ctx, `SELECT pending_fresh FROM channel_chat_session_binding WHERE chat_session_id = $1`, sessionID).Scan(&pendingFresh); err != nil {
			t.Fatalf("load pending fresh after bare /clear: %v", err)
		}
		if taskCount != 0 || userMessageCount != 0 || !pendingFresh {
			t.Fatalf("bare /clear state: tasks=%d messages=%d pending_fresh=%t, want 0/0/true", taskCount, userMessageCount, pendingFresh)
		}

		if err := router.Handle(ctx, channel.InboundMessage{
			EventID:     "event-follow-up-" + t.Name(),
			MessageID:   "message-follow-up-" + t.Name(),
			Type:        channel.MsgTypeText,
			Text:        followUpText,
			CommandText: followUpText,
			Source: channel.Source{
				ChannelType: channelType,
				ChatID:      t.Name(),
				ChatType:    channel.ChatTypeP2P,
				SenderID:    "platform-user-e2e",
			},
		}); err != nil {
			t.Fatalf("route follow-up channel message: %v", err)
		}
	}

	var taskID string
	var forceFresh bool
	var contextRevision int64
	if err := testPool.QueryRow(ctx, `
		SELECT id, force_fresh_session, channel_context_revision
		FROM agent_task_queue
		WHERE chat_session_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, sessionID).Scan(&taskID, &forceFresh, &contextRevision); err != nil {
		t.Fatalf("load queued chat task: %v", err)
	}
	if forceFresh != wantForceFresh {
		t.Fatalf("queued task %s: force_fresh_session = %t, want %t", taskID, forceFresh, wantForceFresh)
	}
	wantRevision := int64(1)
	if wantForceFresh {
		wantRevision = 2
	}
	if contextRevision != wantRevision {
		t.Fatalf("queued task %s: channel_context_revision = %d, want %d", taskID, contextRevision, wantRevision)
	}
	var pendingFresh bool
	if err := testPool.QueryRow(ctx, `SELECT pending_fresh FROM channel_chat_session_binding WHERE chat_session_id = $1`, sessionID).Scan(&pendingFresh); err != nil {
		t.Fatalf("load pending fresh after task enqueue: %v", err)
	}
	if pendingFresh {
		t.Fatal("successful task enqueue did not consume pending_fresh")
	}

	var issueCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM issue
		WHERE workspace_id = $1
		  AND origin_type = 'test_e2e_chat'
		  AND origin_id = $2
	`, testWorkspaceID, sessionID).Scan(&issueCount); err != nil {
		t.Fatalf("count command-created issues: %v", err)
	}
	if issueCount != 0 {
		t.Fatalf("created issues = %d, want 0; /clear must be mutually exclusive with /issue", issueCount)
	}

	var storedText string
	var storedRevision int64
	if err := testPool.QueryRow(ctx, `
		SELECT content, channel_context_revision FROM chat_message
		WHERE chat_session_id = $1 AND role = 'user'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, sessionID).Scan(&storedText, &storedRevision); err != nil {
		t.Fatalf("load persisted chat message: %v", err)
	}
	if storedText != wantStoredText {
		t.Fatalf("persisted message = %q, want %q", storedText, wantStoredText)
	}
	if storedRevision != wantRevision {
		t.Fatalf("persisted message context revision = %d, want %d", storedRevision, wantRevision)
	}

	claimed := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if claimed.ChatMessage != wantStoredText {
		t.Fatalf("claimed chat_message = %q, want %q", claimed.ChatMessage, wantStoredText)
	}
	if claimed.PriorSessionID != wantPriorSession {
		t.Fatalf("claimed prior_session_id = %q, want %q", claimed.PriorSessionID, wantPriorSession)
	}
	if claimed.PriorWorkDir != wantPriorWorkDir {
		t.Fatalf("claimed prior_work_dir = %q, want %q", claimed.PriorWorkDir, wantPriorWorkDir)
	}
}

type channelNewE2EInstallationResolver struct {
	installation engine.ResolvedInstallation
}

func (r channelNewE2EInstallationResolver) ResolveInstallation(context.Context, channel.InboundMessage) (engine.ResolvedInstallation, error) {
	return r.installation, nil
}

type channelNewE2EIdentityResolver struct {
	userID pgtype.UUID
}

func (r channelNewE2EIdentityResolver) ResolveSender(context.Context, engine.ResolvedInstallation, channel.InboundMessage) (engine.ResolvedIdentity, error) {
	return engine.ResolvedIdentity{UserID: r.userID}, nil
}

type channelNewE2EDeduper struct {
	queries *db.Queries
}

func (d channelNewE2EDeduper) Claim(ctx context.Context, installationID pgtype.UUID, messageID string) (pgtype.UUID, error) {
	claim, err := d.queries.ClaimChannelInboundDedup(ctx, db.ClaimChannelInboundDedupParams{
		InstallationID: installationID,
		MessageID:      messageID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, engine.ErrDuplicate
	}
	return claim.ClaimToken, err
}

func (d channelNewE2EDeduper) Mark(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := d.queries.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
		InstallationID: installationID,
		MessageID:      messageID,
		ClaimToken:     claimToken,
	})
	return err
}

func (d channelNewE2EDeduper) Release(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := d.queries.ReleaseChannelInboundDedup(ctx, db.ReleaseChannelInboundDedupParams{
		InstallationID: installationID,
		MessageID:      messageID,
		ClaimToken:     claimToken,
	})
	return err
}

type channelNewE2ESessionBinder struct {
	session *engine.ChatSession
}

func (b *channelNewE2ESessionBinder) EnsureSession(ctx context.Context, p engine.EnsureSessionParams) (pgtype.UUID, error) {
	return b.session.EnsureSession(ctx, engine.EnsureSessionInput{
		WorkspaceID:    p.Installation.WorkspaceID,
		AgentID:        p.Installation.AgentID,
		InstallationID: p.Installation.ID,
		Sender:         p.Sender,
		BindingKey:     p.Message.Source.ChatID,
		ChatType:       p.Message.Source.ChatType,
	})
}

func (b *channelNewE2ESessionBinder) StartSession(ctx context.Context, p engine.StartSessionParams) (engine.StartSessionResult, error) {
	started, err := b.session.StartSession(ctx, engine.StartSessionInput{
		EnsureSessionInput: engine.EnsureSessionInput{
			WorkspaceID: p.Installation.WorkspaceID, AgentID: p.Installation.AgentID,
			InstallationID: p.Installation.ID, Sender: p.Creator,
			BindingKey: p.Message.Source.ChatID, ChatType: p.Message.Source.ChatType,
		},
		Initiator: p.Sender,
		Body:      p.Message.Text, MessageID: p.Message.MessageID,
		ThreadID: p.Message.Source.ThreadID, ClaimToken: p.ClaimToken,
		MediaPendingSeconds: p.MediaPendingSeconds, PersistMessage: p.PersistMessage,
		HistoryBoundaryPending: p.HistoryBoundaryPending,
		BeforeCommit:           p.BeforeCommit,
	})
	return engine.StartSessionResult{
		SessionID: started.SessionID, BindingID: started.BindingID,
		RouteRevision: started.RouteRevision, Append: started.Append,
	}, err
}

func (b *channelNewE2ESessionBinder) MarkPendingFresh(
	ctx context.Context, sessionID pgtype.UUID, messageID string,
) error {
	return b.session.MarkPendingFresh(ctx, sessionID, messageID)
}

func (b *channelNewE2ESessionBinder) AppendMessage(ctx context.Context, p engine.AppendParams) (engine.AppendResult, error) {
	return b.session.AppendUserMessage(ctx, engine.AppendInput{
		SessionID:           p.SessionID,
		Sender:              p.Sender,
		InstallationID:      p.InstallationID,
		Body:                p.Message.Text,
		CommandText:         p.Message.CommandText,
		MessageID:           p.Message.MessageID,
		ThreadID:            p.Message.Source.ThreadID,
		ClaimToken:          p.ClaimToken,
		MediaPendingSeconds: p.MediaPendingSeconds,
		ForceFresh:          p.Message.ForceFresh,
	})
}

func (b *channelNewE2ESessionBinder) BindMedia(ctx context.Context, p engine.BindMediaParams) (engine.BindMediaResult, error) {
	return b.session.BindMediaRefsWithResult(ctx, engine.BindMediaInput{
		MessageID:   p.MessageID,
		SessionID:   p.SessionID,
		WorkspaceID: p.WorkspaceID,
		Sender:      p.Sender,
		IssueID:     p.IssueID,
		MediaRefs:   p.MediaRefs,
	})
}

type channelNewE2EAuditor struct{}

func (channelNewE2EAuditor) RecordDrop(context.Context, pgtype.UUID, channel.InboundMessage, engine.DropReason) error {
	return nil
}
