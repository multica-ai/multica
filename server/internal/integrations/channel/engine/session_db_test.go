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

	"github.com/multica-ai/multica/server/internal/channelmedia"
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
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM channel_media_pending_object WHERE workspace_id = $1`, f.workspaceID)
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

func TestChannelIssueCommandIsExcludedFromLaterChatTaskBatch(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := NewChatSession(db.New(pool), pool, channel.TypeFeishu, SessionTitles{})

	command, err := session.AppendUserMessage(context.Background(), AppendInput{
		SessionID: fixture.sessionID, Sender: fixture.userID,
		Body: "/issue handled once", CommandText: "/issue handled once",
	})
	if err != nil {
		t.Fatalf("append command: %v", err)
	}
	ordinary, err := session.AppendUserMessage(context.Background(), AppendInput{
		SessionID: fixture.sessionID, Sender: fixture.userID,
		Body: "next question", CommandText: "next question",
	})
	if err != nil {
		t.Fatalf("append ordinary message: %v", err)
	}

	var agentID, runtimeID pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT cs.agent_id, a.runtime_id
		FROM chat_session cs
		JOIN agent a ON a.id = cs.agent_id
		WHERE cs.id = $1`, fixture.sessionID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load task routing: %v", err)
	}
	var taskID pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, completed_at)
		VALUES ($1, $2, $3, 'completed', now()) RETURNING id`, agentID, runtimeID, fixture.sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	if err := db.New(pool).LinkUnownedChannelChatMessagesToTask(context.Background(), db.LinkUnownedChannelChatMessagesToTaskParams{
		TaskID: taskID, ChatSessionID: fixture.sessionID,
	}); err != nil {
		t.Fatalf("seal chat input: %v", err)
	}

	var commandOwner, ordinaryOwner pgtype.UUID
	if err := pool.QueryRow(context.Background(), `SELECT task_id FROM chat_message WHERE id = $1`, command.MessageID).Scan(&commandOwner); err != nil {
		t.Fatalf("load command owner: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT task_id FROM chat_message WHERE id = $1`, ordinary.MessageID).Scan(&ordinaryOwner); err != nil {
		t.Fatalf("load ordinary owner: %v", err)
	}
	if commandOwner.Valid {
		t.Fatalf("handled command was linked to a later task: %v", commandOwner)
	}
	if ordinaryOwner != taskID {
		t.Fatalf("ordinary message owner = %v, want %v", ordinaryOwner, taskID)
	}
}

func TestBindMediaRefs_PersistsAndLinksAttachmentToDurableMessage(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := NewChatSession(db.New(pool), pool, channel.TypeFeishu, SessionTitles{})

	appendRes, err := session.AppendUserMessage(context.Background(), AppendInput{
		SessionID:           fixture.sessionID,
		Sender:              fixture.userID,
		Body:                "[Image]",
		MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	seedPendingMediaObject(t, pool, fixture, appendRes.MessageID, "workspaces/ws/lark/image", "https://cdn.example.test/image", "pending")
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
	if _, exists := pendingMediaObjectState(t, pool, "workspaces/ws/lark/image"); exists {
		t.Fatal("happy-path bind must clear the intent row in the same transaction")
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

func TestBindMediaRefs_IssueAttachmentSurvivesChatSessionDeletion(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := NewChatSession(db.New(pool), pool, channel.TypeFeishu, SessionTitles{})
	ctx := context.Background()

	appendRes, err := session.AppendUserMessage(ctx, AppendInput{
		SessionID:           fixture.sessionID,
		Sender:              fixture.userID,
		Body:                "/issue Fix broken layout [Image]",
		MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	var issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'Fix broken layout', 'todo', 'none', 'member', $2, 1)
		RETURNING id
	`, fixture.workspaceID, fixture.userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	const key = "workspaces/ws/lark/issue-image"
	const url = "https://cdn.example.test/issue-image"
	seedPendingMediaObject(t, pool, fixture, appendRes.MessageID, key, url, "pending")
	if err := session.BindMediaRefs(ctx, BindMediaInput{
		MessageID:   appendRes.MessageID,
		SessionID:   fixture.sessionID,
		WorkspaceID: fixture.workspaceID,
		Sender:      fixture.userID,
		IssueID:     issueID,
		MediaRefs: []channel.MediaRef{{
			Type:       channel.MsgTypeImage,
			StorageKey: key,
			StorageURL: url,
			Filename:   "issue-image.png",
			MimeType:   "image/png",
			SizeBytes:  3,
		}},
	}); err != nil {
		t.Fatalf("BindMediaRefs: %v", err)
	}
	if _, exists := pendingMediaObjectState(t, pool, key); exists {
		t.Fatal("issue bind must clear the intent row in the same transaction")
	}

	var attachmentID, gotIssueID pgtype.UUID
	var chatSessionID, chatMessageID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id, issue_id, chat_session_id, chat_message_id
		FROM attachment
		WHERE workspace_id = $1 AND url = $2
	`, fixture.workspaceID, url).Scan(&attachmentID, &gotIssueID, &chatSessionID, &chatMessageID); err != nil {
		t.Fatalf("load issue attachment: %v", err)
	}
	if gotIssueID != issueID || chatSessionID.Valid || chatMessageID.Valid {
		t.Fatalf("attachment ownership = issue:%v session:%v message:%v", gotIssueID, chatSessionID, chatMessageID)
	}
	var description string
	if err := pool.QueryRow(ctx, `SELECT description FROM issue WHERE id = $1`, issueID).Scan(&description); err != nil {
		t.Fatalf("load issue description: %v", err)
	}
	wantDescription := channelmedia.Block(uuidString(attachmentID), "issue-image.png", true)
	if description != wantDescription {
		t.Fatalf("issue description = %q, want %q", description, wantDescription)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, fixture.sessionID); err != nil {
		t.Fatalf("delete chat session: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE id = $1 AND issue_id = $2`, attachmentID, issueID).Scan(&remaining); err != nil {
		t.Fatalf("count issue attachment after chat deletion: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("issue attachment rows after chat deletion = %d, want 1", remaining)
	}
}

func TestBindMediaRefs_EmptyRefsCreateNoAttachmentAndClearPending(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := NewChatSession(db.New(pool), pool, channel.Type("dingtalk"), SessionTitles{})
	ctx := context.Background()

	var issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'Existing issue', 'todo', 'none', 'member', $2, 1)
		RETURNING id
	`, fixture.workspaceID, fixture.userID).Scan(&issueID); err != nil {
		t.Fatalf("create existing issue: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO attachment (
			workspace_id, issue_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		) VALUES ($1, $2, 'member', $3, 'original.png',
			'https://cdn.example.test/original-issue-image', 'image/png', 3)
	`, fixture.workspaceID, issueID, fixture.userID); err != nil {
		t.Fatalf("create original issue attachment: %v", err)
	}
	appendRes, err := session.AppendUserMessage(ctx, AppendInput{
		SessionID:           fixture.sessionID,
		Sender:              fixture.userID,
		Body:                "/issue Existing issue\n[Image]",
		CommandText:         "/issue Existing issue",
		MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatalf("append duplicate issue command: %v", err)
	}

	if err := session.BindMediaRefs(ctx, BindMediaInput{
		MessageID:   appendRes.MessageID,
		SessionID:   fixture.sessionID,
		WorkspaceID: fixture.workspaceID,
		Sender:      fixture.userID,
		Body:        "/issue Existing issue\n[Image]",
	}); err != nil {
		t.Fatalf("finalize duplicate issue media: %v", err)
	}

	var issueAttachmentCount, workspaceAttachmentCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE issue_id = $1`, issueID).Scan(&issueAttachmentCount); err != nil {
		t.Fatalf("count existing issue attachments: %v", err)
	}
	if issueAttachmentCount != 1 {
		t.Fatalf("existing issue attachment rows = %d, want unchanged count 1", issueAttachmentCount)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE workspace_id = $1`, fixture.workspaceID).Scan(&workspaceAttachmentCount); err != nil {
		t.Fatalf("count workspace attachments: %v", err)
	}
	if workspaceAttachmentCount != 1 {
		t.Fatalf("workspace attachment rows = %d, want no new rows beyond the original", workspaceAttachmentCount)
	}
	var mediaPendingUntil pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT channel_media_pending_until FROM chat_message WHERE id = $1`, appendRes.MessageID).Scan(&mediaPendingUntil); err != nil {
		t.Fatalf("load duplicate command media marker: %v", err)
	}
	if mediaPendingUntil.Valid {
		t.Fatalf("duplicate command kept media pending until %v", mediaPendingUntil.Time)
	}
}

func TestBindMediaRefs_MaterializesIssueImagesInOriginalRichTextOrder(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := NewChatSession(db.New(pool), pool, channel.Type("dingtalk"), SessionTitles{})
	ctx := context.Background()
	body := "/issue explain below questions\nWhat is this?\n[Image]\nAnd what is this?\n[Image]"
	commandText := "/issue explain below questions\nWhat is this?And what is this?"
	base := issueDescriptionFromCommandBody(body, commandText, "")

	appendRes, err := session.AppendUserMessage(ctx, AppendInput{
		SessionID:           fixture.sessionID,
		Sender:              fixture.userID,
		Body:                body,
		CommandText:         commandText,
		MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	var issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, description, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'explain below questions', $2, 'todo', 'none', 'member', $3, 4)
		RETURNING id
	`, fixture.workspaceID, base, fixture.userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	const firstKey = "workspaces/ws/dingtalk/issue-first"
	const secondKey = "workspaces/ws/dingtalk/issue-second"
	seedPendingMediaObject(t, pool, fixture, appendRes.MessageID, firstKey, "https://cdn.example.test/issue-first", "pending")
	seedPendingMediaObject(t, pool, fixture, appendRes.MessageID, secondKey, "https://cdn.example.test/issue-second", "pending")
	if err := session.BindMediaRefs(ctx, BindMediaInput{
		MessageID:            appendRes.MessageID,
		SessionID:            fixture.sessionID,
		WorkspaceID:          fixture.workspaceID,
		Sender:               fixture.userID,
		IssueID:              issueID,
		IssueDescriptionBase: pgtype.Text{String: base, Valid: true},
		IssueCommandText:     commandText,
		Body:                 body,
		MediaRefs: []channel.MediaRef{
			{
				Type: channel.MsgTypeImage, StorageKey: firstKey, StorageURL: "https://cdn.example.test/issue-first",
				Filename: "first.png", MimeType: "image/png", InlinePlaceholder: "[Image]", InlineIndex: 0,
			},
			{
				Type: channel.MsgTypeImage, StorageKey: secondKey, StorageURL: "https://cdn.example.test/issue-second",
				Filename: "second.png", MimeType: "image/png", InlinePlaceholder: "[Image]", InlineIndex: 1,
			},
		},
	}); err != nil {
		t.Fatalf("BindMediaRefs: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT filename, id::text
		FROM attachment
		WHERE issue_id = $1
	`, issueID)
	if err != nil {
		t.Fatalf("list issue attachments: %v", err)
	}
	defer rows.Close()
	ids := map[string]string{}
	for rows.Next() {
		var filename, id string
		if err := rows.Scan(&filename, &id); err != nil {
			t.Fatalf("scan issue attachment: %v", err)
		}
		ids[filename] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate issue attachments: %v", err)
	}

	var description string
	if err := pool.QueryRow(ctx, `SELECT description FROM issue WHERE id = $1`, issueID).Scan(&description); err != nil {
		t.Fatalf("load issue description: %v", err)
	}
	want := "What is this?\n" + channelmedia.Block(ids["first.png"], "first.png", true) +
		"\nAnd what is this?\n" + channelmedia.Block(ids["second.png"], "second.png", true)
	if description != want {
		t.Fatalf("issue description = %q, want %q", description, want)
	}
}

func TestMaterializeIssueChannelMediaMarkdownPreservesEditedDescription(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	ctx := context.Background()

	var issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, description, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'Keep description', 'Reproduction steps', 'todo', 'none', 'member', $2, 2)
		RETURNING id
	`, fixture.workspaceID, fixture.userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	const markdown = "![](/api/attachments/22222222-2222-4222-8222-222222222222/download)"
	issue, err := db.New(pool).MaterializeIssueChannelMediaMarkdown(ctx, db.MaterializeIssueChannelMediaMarkdownParams{
		ID:              issueID,
		WorkspaceID:     fixture.workspaceID,
		BaseDescription: pgtype.Text{String: "creation base", Valid: true},
		Description:     "inline layout",
		Markdown:        pgtype.Text{String: markdown, Valid: true},
	})
	if err != nil {
		t.Fatalf("MaterializeIssueChannelMediaMarkdown: %v", err)
	}
	want := "Reproduction steps\n\n" + markdown
	if !issue.Description.Valid || issue.Description.String != want {
		t.Fatalf("issue description = %#v, want %q", issue.Description, want)
	}
}

func TestMaterializeIssueChannelMediaMarkdownReplacesUnchangedBase(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	ctx := context.Background()

	const base = "What is this?\n[Image]\nAnd what is this?\n[Image]"
	const composed = "What is this?\n![](first)\n\nAnd what is this?\n![](second)"
	var issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, description, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'Keep layout', $2, 'todo', 'none', 'member', $3, 3)
		RETURNING id
	`, fixture.workspaceID, base, fixture.userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	issue, err := db.New(pool).MaterializeIssueChannelMediaMarkdown(ctx, db.MaterializeIssueChannelMediaMarkdownParams{
		ID:              issueID,
		WorkspaceID:     fixture.workspaceID,
		BaseDescription: pgtype.Text{String: base, Valid: true},
		Description:     composed,
		Markdown:        pgtype.Text{String: "fallback", Valid: true},
	})
	if err != nil {
		t.Fatalf("MaterializeIssueChannelMediaMarkdown: %v", err)
	}
	if !issue.Description.Valid || issue.Description.String != composed {
		t.Fatalf("issue description = %#v, want %q", issue.Description, composed)
	}
}

func TestIssueDeleteLockPreventsLateMediaBindFromOrphaningObject(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := NewChatSession(db.New(pool), pool, channel.TypeFeishu, SessionTitles{})
	ctx := context.Background()

	appendRes, err := session.AppendUserMessage(ctx, AppendInput{
		SessionID:           fixture.sessionID,
		Sender:              fixture.userID,
		Body:                "/issue Delete while binding [Image]",
		MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	var issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'Delete while binding', 'todo', 'none', 'member', $2, 2)
		RETURNING id`, fixture.workspaceID, fixture.userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	const key = "workspaces/ws/lark/delete-race-image"
	const url = "https://cdn.example.test/delete-race-image"
	seedPendingMediaObject(t, pool, fixture, appendRes.MessageID, key, url, "pending")

	deleteTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin delete transaction: %v", err)
	}
	defer deleteTx.Rollback(ctx)
	deleteQueries := db.New(pool).WithTx(deleteTx)
	if _, err := deleteQueries.LockIssueForDelete(ctx, db.LockIssueForDeleteParams{
		ID: issueID, WorkspaceID: fixture.workspaceID,
	}); err != nil {
		t.Fatalf("lock issue for delete: %v", err)
	}

	bindTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin bind transaction: %v", err)
	}
	defer bindTx.Rollback(ctx)
	var bindPID int32
	if err := bindTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&bindPID); err != nil {
		t.Fatalf("load bind backend pid: %v", err)
	}
	bindResult := make(chan error, 1)
	go func() {
		_, lockErr := db.New(pool).WithTx(bindTx).LockIssueForChannelMediaBind(context.Background(), db.LockIssueForChannelMediaBindParams{
			ID: issueID, WorkspaceID: fixture.workspaceID,
		})
		bindResult <- lockErr
	}()

	blocked := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if err := pool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, bindPID).Scan(&blocked); err != nil {
			t.Fatalf("inspect bind lock wait: %v", err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("media bind did not block behind the delete lock")
	}

	urls, err := deleteQueries.ListAttachmentURLsByIssueOrComments(ctx, issueID)
	if err != nil {
		t.Fatalf("list attachment URLs under delete lock: %v", err)
	}
	if len(urls) != 0 {
		t.Fatalf("attachment URLs before blocked bind = %v, want none", urls)
	}
	if err := deleteQueries.DeleteIssue(ctx, db.DeleteIssueParams{ID: issueID, WorkspaceID: fixture.workspaceID}); err != nil {
		t.Fatalf("delete issue: %v", err)
	}
	if err := deleteTx.Commit(ctx); err != nil {
		t.Fatalf("commit issue delete: %v", err)
	}

	select {
	case err := <-bindResult:
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("bind lock after delete = %v, want no target row", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("media bind stayed blocked after delete commit")
	}
	if state, exists := pendingMediaObjectState(t, pool, key); !exists || state != "pending" {
		t.Fatalf("intent after delete-first race = (%q, %v), want preserved pending", state, exists)
	}
	var attachmentCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM attachment WHERE url = $1`, url).Scan(&attachmentCount); err != nil {
		t.Fatalf("count leaked attachment rows: %v", err)
	}
	if attachmentCount != 0 {
		t.Fatalf("attachment rows after delete-first race = %d, want 0", attachmentCount)
	}
}

func TestBindMediaRefs_MaterializesInlineImagesInOriginalOrder(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := NewChatSession(db.New(pool), pool, channel.TypeFeishu, SessionTitles{})
	body := "[Image]\n这是啥?\n[Image]\n这又是啥?"
	appendRes, err := session.AppendUserMessage(context.Background(), AppendInput{
		SessionID:           fixture.sessionID,
		Sender:              fixture.userID,
		Body:                body,
		MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	seedPendingMediaObject(t, pool, fixture, appendRes.MessageID, "workspaces/ws/dingtalk/first", "https://cdn.example.test/first", "pending")
	seedPendingMediaObject(t, pool, fixture, appendRes.MessageID, "workspaces/ws/dingtalk/second", "https://cdn.example.test/second", "pending")
	err = session.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:   appendRes.MessageID,
		SessionID:   fixture.sessionID,
		WorkspaceID: fixture.workspaceID,
		Sender:      fixture.userID,
		Body:        body,
		MediaRefs: []channel.MediaRef{
			{
				Type: channel.MsgTypeImage, StorageKey: "workspaces/ws/dingtalk/first",
				StorageURL: "https://cdn.example.test/first", Filename: "first.png", MimeType: "image/png",
				InlinePlaceholder: "[Image]", InlineIndex: 0,
			},
			{
				Type: channel.MsgTypeImage, StorageKey: "workspaces/ws/dingtalk/second",
				StorageURL: "https://cdn.example.test/second", Filename: "second.png", MimeType: "image/png",
				InlinePlaceholder: "[Image]", InlineIndex: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("BindMediaRefs: %v", err)
	}

	var stored string
	if err := pool.QueryRow(context.Background(), `SELECT content FROM chat_message WHERE id = $1`, appendRes.MessageID).Scan(&stored); err != nil {
		t.Fatalf("load message content: %v", err)
	}
	rows, err := pool.Query(context.Background(), `
		SELECT filename, id::text
		FROM attachment
		WHERE chat_message_id = $1
		ORDER BY filename`, appendRes.MessageID)
	if err != nil {
		t.Fatalf("load attachment ids: %v", err)
	}
	defer rows.Close()
	ids := map[string]string{}
	for rows.Next() {
		var filename, id string
		if err := rows.Scan(&filename, &id); err != nil {
			t.Fatalf("scan attachment: %v", err)
		}
		ids[filename] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate attachments: %v", err)
	}
	want := "![](/api/attachments/" + ids["first.png"] + "/download)\n这是啥?\n" +
		"![](/api/attachments/" + ids["second.png"] + "/download)\n这又是啥?"
	if stored != want {
		t.Fatalf("stored inline body = %q, want %q", stored, want)
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
		SessionID:           fixture.sessionID,
		Sender:              fixture.userID,
		Body:                "rollback-media",
		MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	seedPendingMediaObject(t, pool, fixture, appendRes.MessageID, "workspaces/ws/lark/rollback", "https://cdn.example.test/rollback.png", "pending")
	err = session.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:   appendRes.MessageID,
		SessionID:   fixture.sessionID,
		WorkspaceID: fixture.workspaceID,
		Sender:      fixture.userID,
		MediaRefs: []channel.MediaRef{{
			Type:       channel.MsgTypeImage,
			StorageKey: "workspaces/ws/lark/rollback",
			StorageURL: "https://cdn.example.test/rollback.png",
			Filename:   "rollback.png",
			MimeType:   "image/png",
			SizeBytes:  4,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "injected attachment link failure") {
		t.Fatalf("BindMediaRefs error = %v", err)
	}
	if state, exists := pendingMediaObjectState(t, pool, "workspaces/ws/lark/rollback"); !exists || state != "pending" {
		t.Fatalf("intent row = (%q, %v), want preserved 'pending' after the rollback", state, exists)
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

// seedPendingMediaObject writes the intent-ledger row the resolver would have
// written before the upload. state defaults to 'pending'.
func seedPendingMediaObject(t *testing.T, pool *pgxpool.Pool, fixture sessionPersistenceFixture, messageID pgtype.UUID, key, url, state string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO channel_media_pending_object (storage_key, workspace_id, chat_message_id, storage_url, state)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (storage_key) DO UPDATE SET state = EXCLUDED.state, chat_message_id = EXCLUDED.chat_message_id, storage_url = EXCLUDED.storage_url
	`, key, fixture.workspaceID, messageID, url, state); err != nil {
		t.Fatalf("seed pending media object: %v", err)
	}
}

func pendingMediaObjectState(t *testing.T, pool *pgxpool.Pool, key string) (string, bool) {
	t.Helper()
	var state string
	err := pool.QueryRow(context.Background(), `
		SELECT state FROM channel_media_pending_object WHERE storage_key = $1
	`, key).Scan(&state)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return "", false
		}
		t.Fatalf("load pending media object: %v", err)
	}
	return state, true
}

// lostAckTxStarter simulates a lost commit ack: the transaction durably
// commits, but the client is handed an error — the result-uncertain window
// the compensation protocol must not treat as "nothing landed".
type lostAckTxStarter struct{ pool *pgxpool.Pool }

func (s *lostAckTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &lostAckTx{Tx: tx}, nil
}

type lostAckTx struct{ pgx.Tx }

func (t *lostAckTx) Commit(ctx context.Context) error {
	if err := t.Tx.Commit(ctx); err != nil {
		return err
	}
	return errors.New("injected lost commit ack")
}

// rolledBackCommitTxStarter simulates a commit failure whose rollback is
// definite: nothing landed, so the caller may safely reclaim the uploads.
type rolledBackCommitTxStarter struct{ pool *pgxpool.Pool }

func (s *rolledBackCommitTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &rolledBackCommitTx{Tx: tx}, nil
}

type rolledBackCommitTx struct{ pgx.Tx }

func (t *rolledBackCommitTx) Commit(ctx context.Context) error {
	_ = t.Tx.Rollback(ctx)
	return errors.New("injected commit failure")
}

// bindOneMediaRef appends the user message through a healthy session, seeds
// the intent-ledger row the resolver would have written before the upload,
// then binds one ref through bindSession — the seam tests inject commit
// faults into. Returns the bind error and the storage key used.
func bindOneMediaRef(t *testing.T, pool *pgxpool.Pool, bindSession *ChatSession, fixture sessionPersistenceFixture, key, url string) error {
	t.Helper()
	appendSession := NewChatSession(db.New(pool), pool, channel.TypeFeishu, SessionTitles{})
	appendRes, err := appendSession.AppendUserMessage(context.Background(), AppendInput{
		SessionID:           fixture.sessionID,
		Sender:              fixture.userID,
		Body:                "[Image]",
		MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	seedPendingMediaObject(t, pool, fixture, appendRes.MessageID, key, url, "pending")
	return bindSession.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:   appendRes.MessageID,
		SessionID:   fixture.sessionID,
		WorkspaceID: fixture.workspaceID,
		Sender:      fixture.userID,
		MediaRefs: []channel.MediaRef{{
			Type:       channel.MsgTypeImage,
			StorageKey: key,
			StorageURL: url,
			Filename:   "ack.png",
			MimeType:   "image/png",
			SizeBytes:  1,
		}},
	})
}

// A Commit error is not a rollback guarantee — and with the intent rows
// cleared inside the SAME transaction, nobody has to adjudicate it: when the
// commit durably landed despite the error report, the attachment exists AND
// the ledger row is gone, so the reconciler has nothing to delete. The bind
// still reports the error; the router only logs it.
func TestBindMediaRefs_LostCommitAckClearsIntentWithAttachment(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := newChatSessionWith(dbSessionQueries{q: db.New(pool)}, &lostAckTxStarter{pool: pool}, channel.TypeFeishu, SessionTitles{})

	key := "workspaces/ws/lark/lost-ack"
	err := bindOneMediaRef(t, pool, session, fixture, key, "https://cdn.example.test/lost-ack.png")
	if err == nil || !strings.Contains(err.Error(), "injected lost commit ack") {
		t.Fatalf("lost-ack bind error = %v", err)
	}
	var attachments int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM attachment WHERE chat_session_id = $1 AND url = 'https://cdn.example.test/lost-ack.png'
	`, fixture.sessionID).Scan(&attachments); err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	if attachments != 1 {
		t.Fatalf("attachments = %d, want the durably committed row", attachments)
	}
	if _, exists := pendingMediaObjectState(t, pool, key); exists {
		t.Fatal("intent row must be gone when the commit landed — atomic with the attachment")
	}
}

// The mirror case: a commit failure whose transaction rolled back leaves the
// intent row in place (also atomically), so the reconciler reclaims the
// object after the settle delay. No attachment exists.
func TestBindMediaRefs_RolledBackCommitKeepsIntentRow(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := newChatSessionWith(dbSessionQueries{q: db.New(pool)}, &rolledBackCommitTxStarter{pool: pool}, channel.TypeFeishu, SessionTitles{})

	key := "workspaces/ws/lark/rolled-back"
	err := bindOneMediaRef(t, pool, session, fixture, key, "https://cdn.example.test/rolled-back.png")
	if err == nil || !strings.Contains(err.Error(), "injected commit failure") {
		t.Fatalf("rolled-back commit error = %v", err)
	}
	var attachments int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM attachment WHERE chat_session_id = $1 AND url = 'https://cdn.example.test/rolled-back.png'
	`, fixture.sessionID).Scan(&attachments); err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	if attachments != 0 {
		t.Fatalf("attachments = %d, want none after rollback", attachments)
	}
	if state, exists := pendingMediaObjectState(t, pool, key); !exists || state != "pending" {
		t.Fatalf("intent row = (%q, %v), want it preserved in 'pending' for the reconciler", state, exists)
	}
}

// Reconciler-wins interleaving: a key already claimed to 'deleting' must not
// be attached — the object is being deleted, the placeholder stays — and the
// bind still succeeds for the rest of the batch (here: empty) and clears the
// media marker.
func TestBindMediaRefs_ReconcilerOwnedKeySkipsAttach(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := NewChatSession(db.New(pool), pool, channel.TypeFeishu, SessionTitles{})

	appendRes, err := session.AppendUserMessage(context.Background(), AppendInput{
		SessionID:           fixture.sessionID,
		Sender:              fixture.userID,
		Body:                "[Image]",
		MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	key := "workspaces/ws/lark/reconciler-owned"
	seedPendingMediaObject(t, pool, fixture, appendRes.MessageID, key, "https://cdn.example.test/owned.png", "deleting")

	if err := session.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:   appendRes.MessageID,
		SessionID:   fixture.sessionID,
		WorkspaceID: fixture.workspaceID,
		Sender:      fixture.userID,
		MediaRefs: []channel.MediaRef{{
			Type:       channel.MsgTypeImage,
			StorageKey: key,
			StorageURL: "https://cdn.example.test/owned.png",
			Filename:   "owned.png",
			MimeType:   "image/png",
			SizeBytes:  1,
		}},
	}); err != nil {
		t.Fatalf("BindMediaRefs: %v", err)
	}

	var attachments int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM attachment WHERE chat_session_id = $1
	`, fixture.sessionID).Scan(&attachments); err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	if attachments != 0 {
		t.Fatalf("attachments = %d, want none for a reconciler-owned key", attachments)
	}
	if state, exists := pendingMediaObjectState(t, pool, key); !exists || state != "deleting" {
		t.Fatalf("intent row = (%q, %v), want untouched 'deleting'", state, exists)
	}
	var mediaPendingUntil pgtype.Timestamptz
	if err := pool.QueryRow(context.Background(), `SELECT channel_media_pending_until FROM chat_message WHERE id = $1`, appendRes.MessageID).Scan(&mediaPendingUntil); err != nil {
		t.Fatalf("load marker: %v", err)
	}
	if mediaPendingUntil.Valid {
		t.Fatalf("media marker must still clear when attach is skipped, got %v", mediaPendingUntil.Time)
	}
}

// Tenancy must never trust the key string: a cross-workspace collision on the
// same storage_key (impossible via the derived key, exactly why it must be
// enforced in the query) must neither steal the row's ownership nor let the
// caller believe it holds an intent — RecordPendingMediaObject reports
// ok=false and the caller skips the upload.
func TestDBMediaIntentLedger_CrossWorkspaceKeyCannotBeStolen(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	ledger := NewDBMediaIntentLedger(db.New(pool))

	key := "workspaces/ws/lark/cross-tenant"
	ok, err := ledger.RecordPendingMediaObject(context.Background(), RecordPendingMediaObjectParams{
		StorageKey:    key,
		WorkspaceID:   fixture.workspaceID,
		ChatMessageID: fixture.sessionID, // any UUID; no FK on the ledger
		StorageURL:    "https://cdn.example.test/cross-a",
	})
	if err != nil || !ok {
		t.Fatalf("first record: ok=%v err=%v", ok, err)
	}

	var otherWorkspace pgtype.UUID
	if err := pool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&otherWorkspace); err != nil {
		t.Fatalf("gen other workspace: %v", err)
	}
	ok, err = ledger.RecordPendingMediaObject(context.Background(), RecordPendingMediaObjectParams{
		StorageKey:    key,
		WorkspaceID:   otherWorkspace,
		ChatMessageID: fixture.sessionID,
		StorageURL:    "https://cdn.example.test/cross-b",
	})
	if err != nil {
		t.Fatalf("cross-workspace record: %v", err)
	}
	if ok {
		t.Fatal("cross-workspace upsert must not claim the key")
	}

	var gotWorkspace pgtype.UUID
	var gotURL string
	if err := pool.QueryRow(context.Background(), `
		SELECT workspace_id, storage_url FROM channel_media_pending_object WHERE storage_key = $1
	`, key).Scan(&gotWorkspace, &gotURL); err != nil {
		t.Fatalf("load row: %v", err)
	}
	if gotWorkspace != fixture.workspaceID || gotURL != "https://cdn.example.test/cross-a" {
		t.Fatalf("row ownership changed: workspace=%v url=%q", gotWorkspace, gotURL)
	}
}

// The persisted media deadline must come from the DATABASE clock: every
// consumer compares it against SQL now(), so an application-clock timestamp
// would let a skewed app node shrink or stretch the fallback window. The
// budget is passed as a relative duration and anchored server-side.
func TestAppendUserMessage_MediaDeadlineUsesDatabaseClock(t *testing.T) {
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	session := NewChatSession(db.New(pool), pool, channel.TypeFeishu, SessionTitles{})

	appendRes, err := session.AppendUserMessage(context.Background(), AppendInput{
		SessionID:           fixture.sessionID,
		Sender:              fixture.userID,
		Body:                "[Image]",
		MediaPendingSeconds: 60,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	var remaining float64
	if err := pool.QueryRow(context.Background(), `
		SELECT EXTRACT(EPOCH FROM (channel_media_pending_until - now())) FROM chat_message WHERE id = $1
	`, appendRes.MessageID).Scan(&remaining); err != nil {
		t.Fatalf("load deadline: %v", err)
	}
	// Anchored to DB now() at insert time: the remaining budget measured by
	// the SAME clock must be the requested 60s minus round-trip slack — a
	// window no application clock skew can distort.
	if remaining < 55 || remaining > 60 {
		t.Fatalf("deadline remaining = %.2fs (DB clock), want ~60s budget", remaining)
	}
}
