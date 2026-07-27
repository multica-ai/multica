package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file proves Task 10's core deliverable end-to-end over a real test
// database: a channelRouter (the same engine.Router built in router.go) with
// a real NewTelegramResolverSet wired against Postgres, fed an inbound
// Telegram webhook update, correctly resolves installation + identity,
// creates (or reuses) the chat session, and routes /issue vs plain chat to
// the shared IssueCreator / TaskEnqueuer seams — with IssueCreator and
// TaskEnqueuer faked so the test never shells out to a real agent CLI.
//
// The DB harness mirrors
// internal/integrations/channel/engine/session_db_test.go's
// sessionPersistenceTestDB: connect to DATABASE_URL (or the local default),
// skip the whole test if unreachable/unmigrated.

func e2eTestDB(t *testing.T) *pgxpool.Pool {
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
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.channel_user_binding') IS NOT NULL`).Scan(&migrated); err != nil || !migrated {
		pool.Close()
		t.Skip("channel_user_binding table not present (database not migrated)")
	}
	t.Cleanup(pool.Close)
	return pool
}

// e2eFixture is the seeded state for one test: a workspace with a member
// (the Telegram sender's bound Multica account) and an agent behind a
// Telegram BYO installation, plus the channel_user_binding that proves
// membership for the identity resolver.
type e2eFixture struct {
	workspaceID      pgtype.UUID
	memberUserID     pgtype.UUID
	agentID          pgtype.UUID
	installationID   pgtype.UUID
	botID            string
	telegramSenderID string
}

// seedE2EFixture writes the rows directly (no FK constraints in this schema,
// per repo convention — see migrations/124_channel_generalization.up.sql),
// mirroring session_db_test.go's seedSessionPersistenceFixture plus the
// telegram-specific channel_installation and channel_user_binding rows.
func seedE2EFixture(t *testing.T, pool *pgxpool.Pool) e2eFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var f e2eFixture
	var runtimeID pgtype.UUID
	var installerID pgtype.UUID

	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Telegram E2E member", fmt.Sprintf("telegram-e2e-%d@multica.test", suffix)).Scan(&f.memberUserID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	installerID = f.memberUserID
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if f.workspaceID.Valid {
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM channel_user_binding WHERE workspace_id = $1`, f.workspaceID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM channel_chat_session_binding WHERE installation_id = $1`, f.installationID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM channel_inbound_message_dedup WHERE installation_id = $1`, f.installationID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id = $1)`, f.workspaceID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM chat_session WHERE workspace_id = $1`, f.workspaceID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM issue WHERE workspace_id = $1`, f.workspaceID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM channel_installation WHERE workspace_id = $1`, f.workspaceID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM agent WHERE workspace_id = $1`, f.workspaceID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM agent_runtime WHERE workspace_id = $1`, f.workspaceID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM member WHERE workspace_id = $1`, f.workspaceID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, f.workspaceID)
		}
		if f.memberUserID.Valid {
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, f.memberUserID)
		}
	})

	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description) VALUES ($1, $2, '') RETURNING id`,
		"Telegram E2E workspace", fmt.Sprintf("telegram-e2e-%d", suffix)).Scan(&f.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, f.workspaceID, f.memberUserID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, owner_id)
		VALUES ($1, $2, 'local', 'multica_daemon', $3)
		RETURNING id`, f.workspaceID, fmt.Sprintf("telegram-e2e-runtime-%d", suffix), f.memberUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'local', $3, $4)
		RETURNING id`, f.workspaceID, fmt.Sprintf("telegram-e2e-agent-%d", suffix), runtimeID, f.memberUserID).Scan(&f.agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	f.botID = fmt.Sprintf("%d", suffix%1_000_000_000)
	f.telegramSenderID = "555000111"
	cfg, err := json.Marshal(installConfig{
		AppID:             f.botID,
		BotUsername:       "e2e_test_bot",
		BotTokenEncrypted: "",
		WebhookSecret:     "e2e-webhook-secret",
	})
	if err != nil {
		t.Fatalf("marshal installation config: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, status, installer_user_id)
		VALUES ($1, $2, 'telegram', $3, 'active', $4)
		RETURNING id`, f.workspaceID, f.agentID, cfg, installerID).Scan(&f.installationID); err != nil {
		t.Fatalf("create channel_installation: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
		VALUES ($1, $2, $3, 'telegram', $4)`,
		f.workspaceID, f.memberUserID, f.installationID, f.telegramSenderID); err != nil {
		t.Fatalf("create channel_user_binding: %v", err)
	}

	return f
}

// fakeIssueCreator/fakeTaskEnqueuer are local, minimal fakes satisfying
// engine.IssueCreator / engine.TaskEnqueuer — deliberately NOT the real
// service.IssueService/TaskService, so this test never executes a real agent
// CLI or writes an actual issue/task row. They record every call for
// assertion, mirroring engine/router_test.go's fakeIssues/fakeTasks.
type fakeIssueCreator struct {
	calls []service.IssueCreateParams
}

func (f *fakeIssueCreator) Create(_ context.Context, p service.IssueCreateParams, _ service.IssueCreateOpts) (service.IssueCreateResult, error) {
	f.calls = append(f.calls, p)
	return service.IssueCreateResult{Issue: db.Issue{ID: pgtype.UUID{Valid: true}, Number: 1, Title: p.Title}}, nil
}

type fakeTaskEnqueuer struct {
	enqueueCalls int
}

func (f *fakeTaskEnqueuer) EnqueueChatTask(_ context.Context, _ db.ChatSession, _ pgtype.UUID, _ bool) (db.AgentTaskQueue, error) {
	f.enqueueCalls++
	return db.AgentTaskQueue{}, nil
}

func (f *fakeTaskEnqueuer) PromoteChannelChatTasksIfMediaReady(_ context.Context, _ pgtype.UUID) error {
	return nil
}

// telegramUpdate builds a Telegram webhook Update carrying text from
// fixture's bound sender in a private chat — the shape TelegramWebhook itself
// decodes before handing off to InboundFromUpdate.
func telegramUpdate(senderID int64, text string) Update {
	return Update{
		UpdateID: time.Now().UnixNano(),
		Message: &Message{
			MessageID: time.Now().UnixNano() % 1_000_000,
			From:      &User{ID: senderID, IsBot: false, Username: "e2e_sender"},
			Chat:      Chat{ID: senderID, Type: "private"},
			Text:      text,
		},
	}
}

// TestE2E_TelegramInboundOverRealDB is the Task 10 end-to-end proof: a real
// engine.Router + telegram.NewTelegramResolverSet, wired exactly as
// router.go wires them in production, driven over a real Postgres test
// database. /issue creates an issue attributed to the bound member and the
// installation's agent; a plain message triggers a chat task enqueue and
// does NOT create an issue.
func TestE2E_TelegramInboundOverRealDB(t *testing.T) {
	pool := e2eTestDB(t)
	fixture := seedE2EFixture(t, pool)
	queries := db.New(pool)

	issues := &fakeIssueCreator{}
	tasks := &fakeTaskEnqueuer{}
	channelRouter := engine.NewRouter(issues, tasks, queries, engine.RouterConfig{Logger: slog.Default()})
	channelRouter.Register(TypeTelegram, NewTelegramResolverSet(queries, pool, nil))

	senderID := int64(555000111)
	if got := fmt.Sprintf("%d", senderID); got != fixture.telegramSenderID {
		t.Fatalf("test setup: sender id mismatch, got %q want %q", got, fixture.telegramSenderID)
	}

	t.Run("/issue creates an issue attributed to the bound member and the installation's agent", func(t *testing.T) {
		update := telegramUpdate(senderID, "/issue Fix login")
		msg, ok := InboundFromUpdate(update, fixture.botID, "e2e_test_bot")
		if !ok {
			t.Fatal("InboundFromUpdate: want ok=true for /issue message")
		}
		if msg.Source.ChannelType != channel.Type(TypeTelegram) {
			t.Fatalf("unexpected channel type: %v", msg.Source.ChannelType)
		}

		if err := channelRouter.Handle(context.Background(), msg); err != nil {
			t.Fatalf("Handle: %v", err)
		}

		if len(issues.calls) != 1 {
			t.Fatalf("IssueCreator.Create call count = %d, want 1", len(issues.calls))
		}
		got := issues.calls[0]
		if got.Title != "Fix login" {
			t.Errorf("Title = %q, want %q", got.Title, "Fix login")
		}
		if got.WorkspaceID != fixture.workspaceID {
			t.Errorf("WorkspaceID = %v, want %v", got.WorkspaceID, fixture.workspaceID)
		}
		if !got.AssigneeType.Valid || got.AssigneeType.String != "agent" {
			t.Errorf("AssigneeType = %+v, want agent", got.AssigneeType)
		}
		if got.AssigneeID != fixture.agentID {
			t.Errorf("AssigneeID = %v, want %v", got.AssigneeID, fixture.agentID)
		}
		if got.CreatorType != "member" {
			t.Errorf("CreatorType = %q, want %q", got.CreatorType, "member")
		}
		if got.CreatorID != fixture.memberUserID {
			t.Errorf("CreatorID = %v, want %v", got.CreatorID, fixture.memberUserID)
		}
	})

	t.Run("plain message enqueues a chat task and does not create an issue", func(t *testing.T) {
		// Every ingested message (issue command or not) also schedules the
		// per-session run trigger (step 8 of the pipeline), so the /issue
		// subtest above already bumped enqueueCalls to 1. The assertion here
		// is the delta this message causes, not an absolute reset to zero.
		enqueueCallsBefore := tasks.enqueueCalls
		issueCallsBefore := len(issues.calls)

		update := telegramUpdate(senderID, "hello")
		msg, ok := InboundFromUpdate(update, fixture.botID, "e2e_test_bot")
		if !ok {
			t.Fatal("InboundFromUpdate: want ok=true for plain message")
		}

		if err := channelRouter.Handle(context.Background(), msg); err != nil {
			t.Fatalf("Handle: %v", err)
		}

		if len(issues.calls) != issueCallsBefore {
			t.Fatalf("IssueCreator.Create call count = %d, want unchanged at %d (no new issue for a plain message)", len(issues.calls), issueCallsBefore)
		}
		if tasks.enqueueCalls != enqueueCallsBefore+1 {
			t.Fatalf("TaskEnqueuer.EnqueueChatTask call count = %d, want %d", tasks.enqueueCalls, enqueueCallsBefore+1)
		}
	})
}
