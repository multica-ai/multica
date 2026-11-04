package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// seedDirectiveRun builds a workspace Secretary with a live chat run whose
// trigger is a message from an owner. The Secretary is restored on cleanup.
type directiveRunFixture struct {
	secretaryID string
	taskID      string
	messageID   string
}

func seedDirectiveRun(t *testing.T, quote string) directiveRunFixture {
	t.Helper()
	ownerID := dbfx.User(t, "Directive boss", fmt.Sprintf("directive-boss-%s@example.test", t.Name()))
	dbfx.Member(t, testWorkspaceID, ownerID, "admin")
	runtimeID := handlerTestRuntimeID(t)
	secretaryID := dbfx.Agent(t, "directive secretary", runtimeID, testutil.Cols{"owner_id": ownerID})
	dbfx.Exec(t, `UPDATE workspace SET secretary_agent_id = $1 WHERE id = $2`, secretaryID, testWorkspaceID)
	t.Cleanup(func() {
		dbfx.Exec(t, `UPDATE workspace SET secretary_agent_id = NULL WHERE id = $1`, testWorkspaceID)
	})
	sessionID := dbfx.ChatSession(t, secretaryID, testutil.Cols{"creator_id": ownerID})
	taskID := dbfx.Task(t, secretaryID, testutil.Cols{
		"runtime_id":          runtimeID,
		"chat_session_id":     sessionID,
		"status":              "running",
		"originator_user_id":  ownerID,
		"accountable_user_id": ownerID,
		"started_at":          testutil.Raw("now()"),
	})
	messageID := dbfx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id": sessionID,
		"role":            "user",
		"content":         quote,
		"task_id":         taskID,
	})
	return directiveRunFixture{secretaryID: secretaryID, taskID: taskID, messageID: messageID}
}

func directiveCreateRequest(title, agentID, taskID string) *http.Request {
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       title,
		"origin_type": "directive",
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func TestCreateIssueDirective_RefusedForNonSecretaryAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedDirectiveRun(t, "Launch the pilot")
	other := createHandlerTestAgent(t, "directive bystander agent", nil)
	testutil.Call(t, testHandler.CreateIssue, directiveCreateRequest("Pilot", other, f.taskID)).Want(http.StatusForbidden)
}

func TestCreateIssueDirective_RefusedFromFinishedRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedDirectiveRun(t, "Launch the pilot")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, f.taskID)
	testutil.Call(t, testHandler.CreateIssue, directiveCreateRequest("Pilot", f.secretaryID, f.taskID)).Want(http.StatusForbidden)
}

func TestCreateIssueDirective_StoresMessageIDAndQuote(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedDirectiveRun(t, "Launch the pilot with the two regions")
	var out struct {
		ID string `json:"id"`
	}
	testutil.Call(t, testHandler.CreateIssue, directiveCreateRequest("Pilot the onboarding", f.secretaryID, f.taskID)).
		Want(http.StatusCreated).JSON(&out)

	var originType, originID, quote string
	dbfx.QueryRow(t, `SELECT origin_type, origin_id::text, metadata->'directive'->>'quote'
		FROM issue WHERE id = $1`, out.ID).Scan(&originType, &originID, &quote)
	if originType != "directive" || originID != f.messageID {
		t.Errorf("origin = (%q, %q), want (directive, %s)", originType, originID, f.messageID)
	}
	if quote != "Launch the pilot with the two regions" {
		t.Errorf("quote = %q", quote)
	}
}

func TestCreateIssueDirective_CapOfFivePerBossMessage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedDirectiveRun(t, "Do all five things")
	for i := 1; i <= 5; i++ {
		testutil.Call(t, testHandler.CreateIssue, directiveCreateRequest(fmt.Sprintf("Directive %d", i), f.secretaryID, f.taskID)).
			Want(http.StatusCreated)
	}
	testutil.Call(t, testHandler.CreateIssue, directiveCreateRequest("Directive 6", f.secretaryID, f.taskID)).
		Want(http.StatusTooManyRequests)
}
