package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func sessionPersistenceTestDB(t *testing.T) *pgxpool.Pool {
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
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.attachment') IS NOT NULL`).Scan(&migrated); err != nil || !migrated {
		pool.Close()
		t.Skip("attachment table not present (database not migrated)")
	}
	t.Cleanup(pool.Close)
	return pool
}

type sessionPersistenceFixture struct {
	workspaceID pgtype.UUID
	userID      pgtype.UUID
	sessionID   pgtype.UUID
}

func seedSessionPersistenceFixture(t *testing.T, pool *pgxpool.Pool) sessionPersistenceFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var f sessionPersistenceFixture
	var runtimeID, agentID pgtype.UUID

	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Channel media test", fmt.Sprintf("channel-media-%d@multica.test", suffix)).Scan(&f.userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if f.workspaceID.Valid {
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, f.workspaceID)
		}
		if f.userID.Valid {
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, f.userID)
		}
	})
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description) VALUES ($1, $2, '') RETURNING id`,
		"Channel media test", fmt.Sprintf("channel-media-%d", suffix)).Scan(&f.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, f.workspaceID, f.userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, owner_id)
		VALUES ($1, $2, 'local', 'multica_daemon', $3)
		RETURNING id`, f.workspaceID, fmt.Sprintf("channel-media-runtime-%d", suffix), f.userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'local', $3, $4)
		RETURNING id`, f.workspaceID, fmt.Sprintf("channel-media-agent-%d", suffix), runtimeID, f.userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'Channel media test')
		RETURNING id`, f.workspaceID, agentID, f.userID).Scan(&f.sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	return f
}

func TestBindMediaRefs_PersistsAndLinksAttachmentToDurableMessage(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := NewChatSession(db.New(pool), pool, channel.TypeFeishu, SessionTitles{})

	appendRes, err := session.AppendUserMessage(context.Background(), AppendInput{
		SessionID:         fixture.sessionID,
		Sender:            fixture.userID,
		Body:              "[Image]",
		MediaPendingUntil: pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	err = session.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:   appendRes.MessageID,
		SessionID:   fixture.sessionID,
		WorkspaceID: fixture.workspaceID,
		Sender:      fixture.userID,
		MediaRefs: []channel.MediaRef{{
			Type:       channel.MsgTypeImage,
			StorageKey: "workspaces/ws/lark/image",
			StorageURL: "https://cdn.example.test/image",
			Filename:   "image.png",
			MimeType:   "image/png",
			SizeBytes:  3,
		}},
	})
	if err != nil {
		t.Fatalf("BindMediaRefs: %v", err)
	}

	var content, filename, url, contentType string
	var mediaPendingUntil pgtype.Timestamptz
	var sizeBytes int64
	var channelIngested bool
	if err := pool.QueryRow(context.Background(), `
		SELECT m.content, a.filename, a.url, a.content_type, a.size_bytes, m.channel_media_pending_until, m.channel_ingested
		FROM chat_message m
		JOIN attachment a ON a.chat_message_id = m.id
		WHERE m.chat_session_id = $1 AND a.chat_session_id = $1`, fixture.sessionID).
		Scan(&content, &filename, &url, &contentType, &sizeBytes, &mediaPendingUntil, &channelIngested); err != nil {
		t.Fatalf("load linked attachment: %v", err)
	}
	if content != "[Image]" || filename != "image.png" || url != "https://cdn.example.test/image" || contentType != "image/png" || sizeBytes != 3 {
		t.Fatalf("persisted media mismatch: content=%q filename=%q url=%q content_type=%q size=%d", content, filename, url, contentType, sizeBytes)
	}
	if mediaPendingUntil.Valid {
		t.Fatalf("media pending deadline was not cleared: %v", mediaPendingUntil.Time)
	}
	if !channelIngested {
		t.Fatal("channel append must stamp channel_ingested for the cancel-path provenance gate")
	}
}

type failingLinkSessionQueries struct {
	SessionQueries
}

func (q failingLinkSessionQueries) WithTx(tx pgx.Tx) SessionQueries {
	return failingLinkSessionQueries{SessionQueries: q.SessionQueries.WithTx(tx)}
}

func (failingLinkSessionQueries) LinkAttachmentsToChatMessage(context.Context, db.LinkAttachmentsToChatMessageParams) ([]pgtype.UUID, error) {
	return nil, errors.New("injected attachment link failure")
}

func TestBindMediaRefs_LinkFailureKeepsMessageAndRollsBackAttachment(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	queries := failingLinkSessionQueries{SessionQueries: dbSessionQueries{q: db.New(pool)}}
	session := newChatSessionWith(queries, pool, channel.TypeFeishu, SessionTitles{})

	appendRes, err := session.AppendUserMessage(context.Background(), AppendInput{
		SessionID:         fixture.sessionID,
		Sender:            fixture.userID,
		Body:              "rollback-media",
		MediaPendingUntil: pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	err = session.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:   appendRes.MessageID,
		SessionID:   fixture.sessionID,
		WorkspaceID: fixture.workspaceID,
		Sender:      fixture.userID,
		MediaRefs: []channel.MediaRef{{
			Type:       channel.MsgTypeImage,
			StorageURL: "https://cdn.example.test/rollback.png",
			Filename:   "rollback.png",
			MimeType:   "image/png",
			SizeBytes:  4,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "injected attachment link failure") {
		t.Fatalf("BindMediaRefs error = %v", err)
	}

	var messageCount, attachmentCount int
	var mediaPendingUntil pgtype.Timestamptz
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM chat_message WHERE chat_session_id = $1 AND content = 'rollback-media'`, fixture.sessionID).Scan(&messageCount); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE chat_session_id = $1 AND filename = 'rollback.png'`, fixture.sessionID).Scan(&attachmentCount); err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT channel_media_pending_until FROM chat_message WHERE id = $1`, appendRes.MessageID).Scan(&mediaPendingUntil); err != nil {
		t.Fatalf("load fallback marker: %v", err)
	}
	if messageCount != 1 || attachmentCount != 0 {
		t.Fatalf("fallback persistence mismatch: messages=%d attachments=%d", messageCount, attachmentCount)
	}
	if mediaPendingUntil.Valid {
		t.Fatalf("failed attachment kept media pending until %v", mediaPendingUntil.Time)
	}
}
