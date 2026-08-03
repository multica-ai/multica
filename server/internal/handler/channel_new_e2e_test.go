package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This test keeps only the external IM transport outside the process. From the
// normalized inbound message onward it runs the production Router, shared chat
// session service, TaskService, PostgreSQL queue, and daemon claim handler.
// That is the repeatable server-side E2E boundary for a channel command: an
// adapter's only responsibility is translating its webhook/socket payload into
// channel.InboundMessage.
func TestChannelNewCommandE2EStartsFreshProviderSession(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler test fixture is required")
	}

	tests := []struct {
		name             string
		channelType      channel.Type
		text             string
		commandText      string
		forceFresh       bool
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
			name:           "Slack /new message starts without provider context",
			channelType:    channel.Type("slack"),
			text:           "/new   what model are you?",
			commandText:    "/new   what model are you?",
			wantStoredText: "what model are you?",
			wantForceFresh: true,
		},
		{
			name:           "Slack same-line /issue remains fresh-only",
			channelType:    channel.Type("slack"),
			text:           "/new /issue investigate deploy",
			commandText:    "/new /issue investigate deploy",
			wantStoredText: "/issue investigate deploy",
			wantForceFresh: true,
		},
		{
			name:           "Slack next-line /issue remains fresh-only",
			channelType:    channel.Type("slack"),
			text:           "/new\n/issue investigate deploy",
			commandText:    "/new\n/issue investigate deploy",
			wantStoredText: "/issue investigate deploy",
			wantForceFresh: true,
		},
		{
			name:           "Feishu same-line /issue remains fresh-only",
			channelType:    channel.TypeFeishu,
			text:           "/issue investigate deploy",
			commandText:    "/new /issue investigate deploy",
			forceFresh:     true,
			wantStoredText: "/issue investigate deploy",
			wantForceFresh: true,
		},
		{
			name:           "Feishu next-line /issue remains fresh-only",
			channelType:    channel.TypeFeishu,
			text:           "/issue investigate deploy",
			commandText:    "/new\n/issue investigate deploy",
			forceFresh:     true,
			wantStoredText: "/issue investigate deploy",
			wantForceFresh: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runChannelNewCommandE2E(t, tt.channelType, tt.text, tt.commandText, tt.forceFresh, tt.wantStoredText, tt.wantForceFresh, tt.wantPriorSession, tt.wantPriorWorkDir)
		})
	}
}

func runChannelNewCommandE2E(t *testing.T, channelType channel.Type, text, commandText string, adapterForceFresh bool, wantStoredText string, wantForceFresh bool, wantPriorSession, wantPriorWorkDir string) {
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
	// normal message resumes both; /new must leave them out of the daemon
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

	var taskID string
	var forceFresh bool
	if err := testPool.QueryRow(ctx, `
		SELECT id, force_fresh_session
		FROM agent_task_queue
		WHERE chat_session_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, sessionID).Scan(&taskID, &forceFresh); err != nil {
		t.Fatalf("load queued chat task: %v", err)
	}
	if forceFresh != wantForceFresh {
		t.Fatalf("queued task %s: force_fresh_session = %t, want %t", taskID, forceFresh, wantForceFresh)
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
		t.Fatalf("created issues = %d, want 0; /new must be mutually exclusive with /issue", issueCount)
	}

	messages, err := queries.ListChatMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("list persisted chat messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != wantStoredText {
		t.Fatalf("persisted messages = %#v, want one user message %q", messages, wantStoredText)
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
	})
}

func (b *channelNewE2ESessionBinder) BindMedia(ctx context.Context, p engine.BindMediaParams) error {
	return b.session.BindMediaRefs(ctx, engine.BindMediaInput{
		MessageID:   p.MessageID,
		SessionID:   p.SessionID,
		WorkspaceID: p.WorkspaceID,
		Sender:      p.Sender,
		MediaRefs:   p.MediaRefs,
	})
}

type channelNewE2EAuditor struct{}

func (channelNewE2EAuditor) RecordDrop(context.Context, pgtype.UUID, channel.InboundMessage, engine.DropReason) error {
	return nil
}
