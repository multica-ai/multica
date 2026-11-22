package handler

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func seedActorInboxRow(t *testing.T, issueID string) {
	t.Helper()
	dbfx.Exec(t, `
		INSERT INTO inbox_item (id, workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, body, actor_type, actor_id, details)
		VALUES ($1, $2, 'member', $3, 'new_comment', 'info', $4, 'auto archive test', 'body', 'member', $3, '{}'::jsonb)
	`, uuidToString(dbid.NewV7()), testWorkspaceID, testUserID, issueID)
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
}

func activeInboxCount(t *testing.T, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM inbox_item
		WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2
		  AND issue_id = $3 AND NOT archived
	`, testWorkspaceID, testUserID, issueID).Scan(&n); err != nil {
		t.Fatalf("count active inbox: %v", err)
	}
	return n
}

func TestAutoArchiveOnAgentTrigger_UpdateIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := seededReadyAgentID(t)
	noRuntimeAgentID := dbfx.Agent(t, "AutoArchive No Runtime", "", testutil.Cols{"owner_id": testUserID})
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM notification_preference WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	})

	var mu sync.Mutex
	archivedReasons := map[string][]any{}
	testHandler.Bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		if payload["recipient_id"] == testUserID {
			if id, ok := payload["issue_id"].(string); ok {
				mu.Lock()
				archivedReasons[id] = append(archivedReasons[id], payload["reason"])
				mu.Unlock()
			}
		}
	})

	t.Run("backlog unassigned to todo keeps inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "backlog unassigned", "status": "backlog"})
		seedActorInboxRow(t, issue.ID)
		updateIssueForOutcome(t, issue.ID, map[string]any{"status": "todo"})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox = %d, want 1", got)
		}
	})

	t.Run("backlog assigned to agent promoted to todo archives inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "backlog promote agent", "status": "backlog"})
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID})
		seedActorInboxRow(t, issue.ID)
		updateIssueForOutcome(t, issue.ID, map[string]any{"status": "todo"})
		if got := activeInboxCount(t, issue.ID); got != 0 {
			t.Fatalf("active inbox = %d, want 0", got)
		}
		mu.Lock()
		reasons := archivedReasons[issue.ID]
		mu.Unlock()
		if len(reasons) != 1 || reasons[0] != "actor_acted" {
			t.Fatalf("reasons = %v, want [actor_acted]", reasons)
		}
	})

	t.Run("assigning agent in backlog defers run and keeps inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "backlog assign agent", "status": "backlog"})
		seedActorInboxRow(t, issue.ID)
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox = %d, want 1", got)
		}
	})

	t.Run("assigning agent in todo starts run and archives inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "todo assign agent", "status": "todo"})
		seedActorInboxRow(t, issue.ID)
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID})
		if got := activeInboxCount(t, issue.ID); got != 0 {
			t.Fatalf("active inbox = %d, want 0", got)
		}
	})

	t.Run("assigning agent with suppress_run keeps inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "todo suppress", "status": "todo"})
		seedActorInboxRow(t, issue.ID)
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID, "suppress_run": true})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox = %d, want 1", got)
		}
	})

	t.Run("assigning blocked agent without runtime keeps inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "todo blocked agent", "status": "todo"})
		seedActorInboxRow(t, issue.ID)
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "agent", "assignee_id": noRuntimeAgentID})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox = %d, want 1", got)
		}
	})

	t.Run("assigning member or unassigning keeps inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "todo member", "status": "todo"})
		seedActorInboxRow(t, issue.ID)
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "member", "assignee_id": testUserID})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox after member assign = %d, want 1", got)
		}
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": nil, "assignee_id": nil})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox after unassign = %d, want 1", got)
		}
	})

	t.Run("muted preference keeps inbox even when agent starts", func(t *testing.T) {
		dbfx.Exec(t, `
			INSERT INTO notification_preference (workspace_id, user_id, preferences)
			VALUES ($1, $2, '{"auto_archive_on_agent_trigger": "muted"}'::jsonb)
			ON CONFLICT (workspace_id, user_id) DO UPDATE SET preferences = EXCLUDED.preferences
		`, testWorkspaceID, testUserID)
		defer func() {
			_, _ = testPool.Exec(context.Background(), `
				DELETE FROM notification_preference WHERE workspace_id = $1 AND user_id = $2
			`, testWorkspaceID, testUserID)
		}()

		issue := createIssueForTest(t, map[string]any{"title": "muted agent trigger", "status": "todo"})
		seedActorInboxRow(t, issue.ID)
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox = %d, want 1 when muted", got)
		}
	})
}

func TestAutoArchiveOnAgentTrigger_CommentAndRerun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := seededReadyAgentID(t)
	noRuntimeAgentID := dbfx.Agent(t, "AutoArchive Comment No Runtime", "", testutil.Cols{"owner_id": testUserID})
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM notification_preference WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	})

	postComment := func(t *testing.T, issueID string, body map[string]any) {
		t.Helper()
		testutil.Call(t, testHandler.CreateComment,
			withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", body), "id", issueID)).Want(http.StatusCreated)
	}

	t.Run("plain comment on unassigned issue keeps inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "comment unassigned", "status": "todo"})
		seedActorInboxRow(t, issue.ID)
		postComment(t, issue.ID, map[string]any{"content": "just a human note"})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox = %d, want 1", got)
		}
	})

	t.Run("note comment on agent issue keeps inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "comment /note", "status": "backlog"})
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID})
		seedActorInboxRow(t, issue.ID)
		postComment(t, issue.ID, map[string]any{"content": "/note internal memo"})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox = %d, want 1", got)
		}
	})

	t.Run("suppressed comment trigger keeps inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "comment suppressed", "status": "backlog"})
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID})
		seedActorInboxRow(t, issue.ID)
		postComment(t, issue.ID, map[string]any{
			"content":            "reply without waking agent",
			"suppress_agent_ids": []string{agentID},
		})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox = %d, want 1", got)
		}
	})

	t.Run("comment mentioning blocked agent keeps inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "comment blocked", "status": "todo"})
		seedActorInboxRow(t, issue.ID)
		postComment(t, issue.ID, map[string]any{
			"content": "[@NoRuntime](mention://agent/" + noRuntimeAgentID + ") please check",
		})
		if got := activeInboxCount(t, issue.ID); got != 1 {
			t.Fatalf("active inbox = %d, want 1", got)
		}
	})

	t.Run("comment triggering assigned agent archives inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "comment wakes agent", "status": "backlog"})
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID})
		seedActorInboxRow(t, issue.ID)
		postComment(t, issue.ID, map[string]any{"content": "please take a look"})
		if got := activeInboxCount(t, issue.ID); got != 0 {
			t.Fatalf("active inbox = %d, want 0", got)
		}
	})

	t.Run("rerunning issue archives inbox", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "rerun wakes agent", "status": "backlog"})
		updateIssueForOutcome(t, issue.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID})
		seedActorInboxRow(t, issue.ID)
		testutil.Call(t, testHandler.RerunIssue,
			withURLParam(newRequest("POST", "/api/issues/"+issue.ID+"/rerun", map[string]any{}), "id", issue.ID)).Want(http.StatusAccepted)
		if got := activeInboxCount(t, issue.ID); got != 0 {
			t.Fatalf("active inbox = %d, want 0", got)
		}
	})
}
