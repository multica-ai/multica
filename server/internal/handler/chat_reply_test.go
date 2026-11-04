package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// chatReplyEnv is a workspace whose Secretary owns one chat with the boss, plus a
// second agent that is not the Secretary.
type chatReplyEnv struct {
	secretary string
	other     string
	session   string
}

func newChatReplyEnv(t *testing.T) chatReplyEnv {
	t.Helper()
	runtimeID := handlerTestRuntimeID(t)
	secretary := dbfx.Agent(t, "Secretary "+uuid.NewString(), runtimeID, testutil.Cols{"workspace_id": testWorkspaceID})
	other := dbfx.Agent(t, "Other agent "+uuid.NewString(), runtimeID, testutil.Cols{"workspace_id": testWorkspaceID})
	dbfx.Exec(t, `UPDATE workspace SET secretary_agent_id = $1 WHERE id = $2`, secretary, testWorkspaceID)
	t.Cleanup(func() {
		dbfx.Exec(t, `UPDATE workspace SET secretary_agent_id = NULL WHERE id = $1`, testWorkspaceID)
	})
	session := dbfx.ChatSession(t, secretary, testutil.Cols{"creator_id": testUserID})
	return chatReplyEnv{secretary: secretary, other: other, session: session}
}

// runFor starts a run of the agent, bound to the chat session, originated by the given user.
func (e chatReplyEnv) runFor(t *testing.T, agentID, sessionID, originator string) string {
	t.Helper()
	return dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":          handlerTestRuntimeID(t),
		"chat_session_id":     sessionID,
		"status":              "running",
		"originator_user_id":  originator,
		"accountable_user_id": originator,
	})
}

func (e chatReplyEnv) reply(t *testing.T, sessionID, agentID, taskID, content string) *testutil.Response {
	t.Helper()
	req := newRequest("POST", "/api/chat/sessions/"+sessionID+"/reply", map[string]string{"content": content})
	req = asAgent(req, agentID, taskID)
	req = withURLParam(req, "sessionId", sessionID)
	return testutil.Call(t, testHandler.ReplyChatSession, req)
}

func TestReplyChatSession_SecretaryFromLinkedRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	env := newChatReplyEnv(t)
	task := env.runFor(t, env.secretary, env.session, testUserID)

	var out ChatMessageResponse
	env.reply(t, env.session, env.secretary, task, "## Answer\nAll squads reported.").Want(http.StatusCreated).JSON(&out)
	if out.Role != "assistant" || out.Content != "## Answer\nAll squads reported." {
		t.Fatalf("reply = %+v", out)
	}
	if out.TaskID != nil {
		t.Fatalf("reply carries task_id %q; a reply must not look like the turn's own outcome", *out.TaskID)
	}
	// Unread follows from the reply being newer than the session's read cursor.
	var unread int
	dbfx.QueryRow(t, `SELECT count(*) FROM chat_message m JOIN chat_session cs ON cs.id = m.chat_session_id
		WHERE cs.id = $1 AND m.role = 'assistant' AND m.created_at > cs.last_read_at`, env.session).Scan(&unread)
	if unread == 0 {
		t.Fatalf("session shows no unread reply after the reply was written")
	}
}

func TestReplyChatSession_Refusals(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	env := newChatReplyEnv(t)
	archived := dbfx.ChatSession(t, env.secretary, testutil.Cols{"creator_id": testUserID, "status": "archived"})
	otherSession := dbfx.ChatSession(t, env.secretary, testutil.Cols{"creator_id": testUserID})
	strangerSession := dbfx.ChatSession(t, env.secretary, testutil.Cols{"creator_id": dbfx.User(t, "Stranger "+uuid.NewString(), uuid.NewString()+"@example.test")})
	linked := env.runFor(t, env.secretary, env.session, testUserID)

	cases := []struct {
		name    string
		agent   string
		session string
		task    string
		want    int
	}{
		{"another agent refused", env.other, env.session, env.runFor(t, env.other, env.session, testUserID), http.StatusForbidden},
		{"run linked to another session refused", env.secretary, env.session, env.runFor(t, env.secretary, otherSession, testUserID), http.StatusForbidden},
		{"archived session refused", env.secretary, archived, env.runFor(t, env.secretary, archived, testUserID), http.StatusConflict},
		{"originator not owner refused", env.secretary, strangerSession, env.runFor(t, env.secretary, strangerSession, testUserID), http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env.reply(t, tc.session, tc.agent, tc.task, "hello").Want(tc.want)
		})
	}

	// The per-session cap: the limit is in replies per minute, so the next one is refused.
	for i := 0; i < service.ChatReplyPerMinuteLimit; i++ {
		env.reply(t, env.session, env.secretary, linked, "reply").Want(http.StatusCreated)
	}
	env.reply(t, env.session, env.secretary, linked, "one too many").Want(http.StatusTooManyRequests)
}

func TestGetCompanyOrgMap_WorkspaceScoped(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)
	leader := dbfx.Agent(t, "Leader "+uuid.NewString(), runtimeID, testutil.Cols{"workspace_id": testWorkspaceID})
	squad := dbfx.Squad(t, "Org map squad "+uuid.NewString(), leader)
	dbfx.SquadMember(t, squad, "agent", leader)

	// A squad in another workspace must never appear in this workspace's map.
	otherWS := dbfx.Workspace(t, "Other org map "+uuid.NewString(), "other-org-map-"+uuid.NewString())
	otherAgent := dbfx.Agent(t, "Foreign leader "+uuid.NewString(), runtimeID, testutil.Cols{"workspace_id": otherWS})
	foreign := dbfx.Squad(t, "Foreign squad "+uuid.NewString(), otherAgent, testutil.Cols{"workspace_id": otherWS})

	req := newRequest("GET", "/api/company/org-map", nil)
	var out service.OrgMap
	testutil.Call(t, testHandler.GetCompanyOrgMap, req).Want(http.StatusOK).JSON(&out)

	var found bool
	for _, s := range out.Squads {
		if s.ID == squad {
			found = true
			if s.Leader == nil || s.Leader.ID != leader {
				t.Fatalf("squad leader = %+v, want %s", s.Leader, leader)
			}
		}
		if s.ID == foreign {
			t.Fatalf("org map leaked a squad from another workspace")
		}
	}
	if !found {
		t.Fatalf("squad %s missing from org map", squad)
	}
}
