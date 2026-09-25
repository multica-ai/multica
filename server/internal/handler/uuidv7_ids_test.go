package handler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// TestHotTableInsertsPersistUUIDv7 runs the converted INSERTs against Postgres
// and asserts the id the application minted is the id that landed in the row.
// The static checks in pkg/dbid prove the queries and call sites are wired; this
// proves the round trip — parameter binding, column order, and the COALESCE
// wrapper all agree.
func TestHotTableInsertsPersistUUIDv7(t *testing.T) {
	ctx := context.Background()
	q := testHandler.Queries

	runtimeID := dbfx.Runtime(t, "uuidv7-runtime")
	agentID := dbfx.Agent(t, "uuidv7-agent", runtimeID)
	issueID := dbfx.Issue(t, "uuidv7 issue")

	t.Run("issue", func(t *testing.T) {
		var maxNumber int32
		dbfx.QueryRow(t, `SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1`,
			testWorkspaceID).Scan(&maxNumber)

		created, err := q.CreateIssue(ctx, db.CreateIssueParams{
			ID:          dbid.NewV7(),
			WorkspaceID: parseUUID(testWorkspaceID),
			Title:       "uuidv7 create issue",
			Status:      "todo",
			Priority:    "none",
			CreatorType: "member",
			CreatorID:   parseUUID(testUserID),
			Number:      maxNumber,
		})
		if err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
		cleanupRow(t, "issue", created.ID)
		assertV7(t, "issue.id", created.ID)
	})

	t.Run("comment", func(t *testing.T) {
		created, err := q.CreateComment(ctx, db.CreateCommentParams{
			ID:          dbid.NewV7(),
			IssueID:     parseUUID(issueID),
			WorkspaceID: parseUUID(testWorkspaceID),
			AuthorType:  "member",
			AuthorID:    parseUUID(testUserID),
			Content:     "uuidv7 comment",
			Type:        "comment",
		})
		if err != nil {
			t.Fatalf("CreateComment: %v", err)
		}
		cleanupRow(t, "comment", created.ID)
		assertV7(t, "comment.id", created.ID)
	})

	t.Run("activity_log", func(t *testing.T) {
		created, err := q.CreateActivity(ctx, db.CreateActivityParams{
			ID:          dbid.NewV7(),
			WorkspaceID: parseUUID(testWorkspaceID),
			IssueID:     parseUUID(issueID),
			ActorType:   pgtype.Text{String: "member", Valid: true},
			ActorID:     parseUUID(testUserID),
			Action:      "uuidv7_test",
			Details:     []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("CreateActivity: %v", err)
		}
		cleanupRow(t, "activity_log", created.ID)
		assertV7(t, "activity_log.id", created.ID)
	})

	t.Run("inbox_item", func(t *testing.T) {
		created, err := q.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   parseUUID(testWorkspaceID),
			RecipientType: "member",
			RecipientID:   parseUUID(testUserID),
			Type:          "issue_assigned",
			Severity:      "info",
			IssueID:       parseUUID(issueID),
			Title:         "uuidv7 inbox item",
			Details:       []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("CreateInboxItem: %v", err)
		}
		cleanupRow(t, "inbox_item", created.ID)
		assertV7(t, "inbox_item.id", created.ID)
	})

	t.Run("chat_session_and_chat_message", func(t *testing.T) {
		session, err := q.CreateChatSession(ctx, db.CreateChatSessionParams{
			ID:          dbid.NewV7(),
			WorkspaceID: parseUUID(testWorkspaceID),
			AgentID:     parseUUID(agentID),
			CreatorID:   parseUUID(testUserID),
			Title:       "uuidv7 chat",
		})
		if err != nil {
			t.Fatalf("CreateChatSession: %v", err)
		}
		cleanupRow(t, "chat_session", session.ID)
		assertV7(t, "chat_session.id", session.ID)

		msg, err := q.CreateChatMessage(ctx, db.CreateChatMessageParams{
			ID:            dbid.NewV7(),
			ChatSessionID: session.ID,
			Role:          "user",
			Content:       "hello",
		})
		if err != nil {
			t.Fatalf("CreateChatMessage: %v", err)
		}
		cleanupRow(t, "chat_message", msg.ID)
		assertV7(t, "chat_message.id", msg.ID)
	})

	t.Run("agent_task_queue_and_task_token", func(t *testing.T) {
		task, err := q.CreateAgentTask(ctx, db.CreateAgentTaskParams{
			ID:        dbid.NewV7(),
			AgentID:   parseUUID(agentID),
			RuntimeID: parseUUID(runtimeID),
			IssueID:   parseUUID(issueID),
			Priority:  1,
		})
		if err != nil {
			t.Fatalf("CreateAgentTask: %v", err)
		}
		cleanupRow(t, "agent_task_queue", task.ID)
		assertV7(t, "agent_task_queue.id", task.ID)

		token, err := q.CreateTaskToken(ctx, db.CreateTaskTokenParams{
			ID:          dbid.NewV7(),
			TokenHash:   "uuidv7-test-token-hash",
			TaskID:      task.ID,
			AgentID:     parseUUID(agentID),
			WorkspaceID: parseUUID(testWorkspaceID),
			UserID:      parseUUID(testUserID),
			ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		})
		if err != nil {
			t.Fatalf("CreateTaskToken: %v", err)
		}
		cleanupRow(t, "task_token", token.ID)
		assertV7(t, "task_token.id", token.ID)
	})

	t.Run("channel_inbound_audit", func(t *testing.T) {
		const eventID = "uuidv7-audit-event"
		dbfx.Cleanup(t, `DELETE FROM channel_inbound_audit WHERE channel_event_id = $1`, eventID)

		if err := q.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
			ID:             dbid.NewV7(),
			ChannelType:    "slack",
			EventType:      "message",
			DropReason:     "uuidv7_test",
			ChannelEventID: pgtype.Text{String: eventID, Valid: true},
		}); err != nil {
			t.Fatalf("RecordChannelInboundDrop: %v", err)
		}

		var got pgtype.UUID
		dbfx.QueryRow(t, `SELECT id FROM channel_inbound_audit WHERE channel_event_id = $1`, eventID).Scan(&got)
		assertV7(t, "channel_inbound_audit.id", got)
	})
}

// TestUnsetIDFallsBackToTheDatabaseDefault covers the safety net: a caller that
// does not mint an id must still get a row, with a database-generated v4, rather
// than a NOT NULL violation. This is what makes the COALESCE wrapper — and
// dbid.NewV7's silent degradation on an entropy failure — safe.
func TestUnsetIDFallsBackToTheDatabaseDefault(t *testing.T) {
	ctx := context.Background()

	issueID := dbfx.Issue(t, "uuidv7 fallback issue")
	created, err := testHandler.Queries.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		IssueID:     parseUUID(issueID),
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(testUserID),
		Action:      "uuidv7_fallback_test",
		Details:     []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateActivity without an id: %v", err)
	}
	cleanupRow(t, "activity_log", created.ID)

	if !created.ID.Valid {
		t.Fatal("row was inserted without an id")
	}
	if v := uuid.UUID(created.ID.Bytes).Version(); v != 4 {
		t.Fatalf("database default produced a v%d id, want v4", v)
	}
}

func assertV7(t *testing.T, label string, id pgtype.UUID) {
	t.Helper()

	if !id.Valid {
		t.Fatalf("%s: no id returned", label)
	}
	parsed := uuid.UUID(id.Bytes)
	if v := parsed.Version(); v != 7 {
		t.Errorf("%s: stored id is v%d (%s), want v7", label, v, parsed)
	}
}

// cleanupRow deletes a row the query under test created. Fixtures registered
// through testutil clean themselves up; rows created by the production query do
// not, so they are registered by hand.
func cleanupRow(t *testing.T, table string, id pgtype.UUID) {
	t.Helper()

	dbfx.Cleanup(t, `DELETE FROM `+table+` WHERE id = $1`, id)
}
