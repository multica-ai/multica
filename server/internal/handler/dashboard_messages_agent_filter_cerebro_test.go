package handler

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestDashboardMessageQueriesFilterByRecipientAgent(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("fetch test agent: %v", err)
	}

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, 'dashboard agent filter', 'active')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, sessionID)
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})

	const content = "dashboard-agent-filter-message"
	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', $2)
	`, sessionID, content); err != nil {
		t.Fatalf("create chat message: %v", err)
	}

	queries := cerebrodb.New(testPool)
	now := time.Now().UTC()
	workspaceUUID := parseUUID(testWorkspaceID)
	agentUUID := parseUUID(agentID)
	from := pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}
	to := pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true}

	rows, err := queries.DashboardAllChatMessages(ctx, cerebrodb.DashboardAllChatMessagesParams{
		WorkspaceID: workspaceUUID,
		CreatedAt:   from,
		CreatedAt_2: to,
		Column4:     "agent",
		Column5:     agentUUID,
	})
	if err != nil {
		t.Fatalf("query all messages by agent: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.Content == content {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("agent-filtered all messages omitted the selected agent's conversation")
	}

	days, err := queries.DashboardCountMessagesByDay(ctx, cerebrodb.DashboardCountMessagesByDayParams{
		WorkspaceID: workspaceUUID,
		CreatedAt:   from,
		CreatedAt_2: to,
		Column4:     "agent",
		Column5:     agentUUID,
	})
	if err != nil {
		t.Fatalf("query message timeline by agent: %v", err)
	}
	if len(days) == 0 {
		t.Fatal("agent-filtered message timeline omitted the selected agent's conversation")
	}
}
