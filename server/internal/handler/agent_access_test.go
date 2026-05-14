package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestMemberAllowedForPrivateAgent_Pure exercises the pure predicate that
// drives the private-agent gate. The gate must allow:
//   - workspace owner / admin (regardless of agent ownership)
//   - the agent owner (regardless of role)
//
// And deny everyone else. This test runs without a database.
func TestMemberAllowedForPrivateAgent_Pure(t *testing.T) {
	ownerUserID := "11111111-1111-1111-1111-111111111111"
	otherUserID := "22222222-2222-2222-2222-222222222222"

	agent := db.Agent{
		OwnerID: util.MustParseUUID(ownerUserID),
	}

	cases := []struct {
		name   string
		userID string
		role   string
		want   bool
	}{
		{"workspace owner, not agent owner", otherUserID, "owner", true},
		{"workspace admin, not agent owner", otherUserID, "admin", true},
		{"agent owner with member role", ownerUserID, "member", true},
		{"agent owner with admin role", ownerUserID, "admin", true},
		{"plain member, not agent owner", otherUserID, "member", false},
		{"plain member with no role string", otherUserID, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := memberAllowedForPrivateAgent(agent, tc.userID, tc.role)
			if got != tc.want {
				t.Fatalf("memberAllowedForPrivateAgent(userID=%s, role=%s) = %v; want %v",
					tc.userID, tc.role, got, tc.want)
			}
		})
	}
}

// privateAgentTestFixture sets up a private agent owned by a freshly created
// user, plus a second non-admin member in the workspace. Returns the agent
// id, the owner's user id, and the unrelated member's user id. The caller's
// own testUserID stays workspace owner so it can act as the privileged
// admin path.
func privateAgentTestFixture(t *testing.T) (agentID, ownerID, memberID string) {
	t.Helper()

	ctx := context.Background()
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Private Agent Owner', 'private-agent-owner@multica.test')
		RETURNING id
	`).Scan(&ownerID); err != nil {
		t.Fatalf("create owner user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = 'private-agent-owner@multica.test'`)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, ownerID); err != nil {
		t.Fatalf("add owner as member: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Plain Member', 'plain-member@multica.test')
		RETURNING id
	`).Scan(&memberID); err != nil {
		t.Fatalf("create plain member user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = 'plain-member@multica.test'`)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, memberID); err != nil {
		t.Fatalf("add plain member: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'private-access-test-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, handlerTestRuntimeID(t), ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create private agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE id = $1`, agentID)
	})

	return agentID, ownerID, memberID
}

func newRequestAs(userID, method, path string, body any) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-User-ID", userID)
	return req
}

// TestGetAgent_PrivateAgentForbidsPlainMember verifies the private-agent
// visibility gate at the read-detail endpoint: a workspace member who is
// neither the agent owner nor a workspace owner/admin gets 403, while the
// agent owner and workspace owner both succeed. Mirrors the four-entry-point
// gate (chat, history, edit, delete) on its read surface.
func TestGetAgent_PrivateAgentForbidsPlainMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, ownerID, memberID := privateAgentTestFixture(t)

	// Workspace owner (testUserID): allowed via role.
	w := httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequest("GET", "/api/agents/"+agentID, nil), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent as workspace owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Agent owner (plain member who happens to own the agent): allowed.
	w = httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequestAs(ownerID, "GET", "/api/agents/"+agentID, nil), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent as agent owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Plain member (not in allowed_principals): denied with 403.
	w = httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequestAs(memberID, "GET", "/api/agents/"+agentID, nil), "id", agentID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("GetAgent as plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListAgents_FiltersPrivateForPlainMember verifies that the workspace
// agents listing hides private agents from members who lack access. This is
// what makes the @-mention autocomplete picker (which feeds off this list)
// drop unreachable private agents without any client-side logic.
func TestListAgents_FiltersPrivateForPlainMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, _, memberID := privateAgentTestFixture(t)

	// Workspace owner sees the agent.
	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newRequest("GET", "/api/agents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents as owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !listContainsAgent(t, w.Body.Bytes(), agentID) {
		t.Fatalf("ListAgents as owner did not include private agent %s", agentID)
	}

	// Plain member does NOT see the agent.
	w = httptest.NewRecorder()
	testHandler.ListAgents(w, newRequestAs(memberID, "GET", "/api/agents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents as plain member: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if listContainsAgent(t, w.Body.Bytes(), agentID) {
		t.Fatalf("ListAgents as plain member leaked private agent %s", agentID)
	}
}

func listContainsAgent(t *testing.T, body []byte, agentID string) bool {
	t.Helper()
	var resp []AgentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode ListAgents response: %v", err)
	}
	for _, a := range resp {
		if a.ID == agentID {
			return true
		}
	}
	return false
}

// TestListAgentTasks_PrivateAgentForbidsPlainMember verifies that the agent
// task history endpoint (the "查看历史会话" surface) is also gated.
func TestListAgentTasks_PrivateAgentForbidsPlainMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, ownerID, memberID := privateAgentTestFixture(t)

	w := httptest.NewRecorder()
	testHandler.ListAgentTasks(w, withURLParam(newRequestAs(ownerID, "GET", "/api/agents/"+agentID+"/tasks", nil), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgentTasks as owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.ListAgentTasks(w, withURLParam(newRequestAs(memberID, "GET", "/api/agents/"+agentID+"/tasks", nil), "id", agentID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("ListAgentTasks as plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssue_AssignToPrivateAgentForbidsPlainMember verifies that the
// issue-assignment surface is gated by the same predicate. Without this gate
// a plain workspace member could side-step chat/@-mention by assigning a
// private agent to an issue and letting normal task dispatch run it.
func TestCreateIssue_AssignToPrivateAgentForbidsPlainMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, ownerID, memberID := privateAgentTestFixture(t)

	body := func(actorID string) map[string]any {
		return map[string]any{
			"title":         "assign-to-private-agent test " + actorID,
			"status":        "todo",
			"priority":      "medium",
			"assignee_type": "agent",
			"assignee_id":   agentID,
		}
	}

	// Workspace owner (testUserID): allowed.
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body(testUserID)))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue as workspace owner: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Agent owner (plain member who happens to own the agent): allowed.
	w = httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequestAs(ownerID, "POST", "/api/issues?workspace_id="+testWorkspaceID, body(ownerID)))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue as agent owner: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Plain member: denied with 403 — closes the back door where issue
	// assignment would otherwise hand the agent a task without going
	// through chat / @-mention.
	w = httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequestAs(memberID, "POST", "/api/issues?workspace_id="+testWorkspaceID, body(memberID)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateIssue as plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateChatSession_PrivateAgentForbidsPlainMember verifies that members
// who can't access the private agent cannot start a chat session against it.
// The chat handler reads workspace context from middleware, so we set it
// explicitly via middleware.SetMemberContext before invoking the handler
// (the test harness doesn't run the real middleware chain).
func TestCreateChatSession_PrivateAgentForbidsPlainMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, _, memberID := privateAgentTestFixture(t)

	// Load the plain member's row so we can build a realistic context.
	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(memberID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load plain member row: %v", err)
	}

	body := map[string]any{
		"agent_id": agentID,
		"title":    "should be denied",
	}
	w := httptest.NewRecorder()
	req := newRequestAs(memberID, "POST", "/api/chat/sessions", body)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
	testHandler.CreateChatSession(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateChatSession as plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetAgent_RejectsForgedAgentIDHeader is the regression test for the
// #2359 review finding "X-Agent-ID can be forged by a plain member to bypass
// the private gate". A workspace member sets X-Agent-ID to any visible
// agent's UUID without supplying a valid X-Task-ID — resolveActor must now
// fall back to the member identity, so the private-agent gate stays effective.
func TestGetAgent_RejectsForgedAgentIDHeader(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, _, memberID := privateAgentTestFixture(t)

	w := httptest.NewRecorder()
	req := newRequestAs(memberID, "GET", "/api/agents/"+agentID, nil)
	// Forge X-Agent-ID without X-Task-ID. Pre-fix this would have made
	// resolveActor return ("agent", agentID) and canAccessPrivateAgent
	// would have unconditionally allowed the read.
	req.Header.Set("X-Agent-ID", agentID)
	req = withURLParam(req, "id", agentID)
	testHandler.GetAgent(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("GetAgent with forged X-Agent-ID: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListChatMessages_PrivateAgentForbidsAfterAccessRevoked is the regression
// test for the #2359 review finding "chat history read path doesn't re-gate".
// A member who created a chat session is later denied access to the agent
// (here simulated by the member never being on the allowlist for a private
// agent owned by someone else; the equivalent of an after-the-fact ownership
// transfer). The session row still names them as creator, but the read
// endpoints must refuse to surface the transcript.
func TestListChatMessages_PrivateAgentForbidsAfterAccessRevoked(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID, _, memberID := privateAgentTestFixture(t)

	// Insert a chat session row directly with the plain member as creator,
	// bypassing CreateChatSession's own gate. This represents a session
	// that existed before the member lost access (or before the gate
	// landed).
	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, 'pre-revocation session', 'active')
		RETURNING id
	`, testWorkspaceID, agentID, memberID).Scan(&sessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})

	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(memberID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load plain member row: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequestAs(memberID, "GET", "/api/chat/sessions/"+sessionID+"/messages", nil)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
	req = withURLParam(req, "sessionId", sessionID)
	testHandler.ListChatMessages(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("ListChatMessages on stale session: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestMentionAgent_RejectsCrossWorkspaceAgentUUID is the regression test for
// the #2359 review finding "@mention path doesn't constrain the mentioned
// agent to the current workspace". A plain member in workspace A who happens
// to be owner of workspace B should NOT be able to @mention a private agent
// in workspace B from a comment on a workspace-A issue and have it pass the
// gate (the gate was being applied against the wrong workspace's roles).
func TestMentionAgent_RejectsCrossWorkspaceAgentUUID(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	// Create a separate workspace + agent runtime + private agent.
	var foreignWorkspaceID, foreignUserID, foreignRuntimeID, foreignAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Foreign Owner', 'cross-ws-foreign@multica.test')
		RETURNING id
	`).Scan(&foreignUserID); err != nil {
		t.Fatalf("create foreign user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = 'cross-ws-foreign@multica.test'`)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Cross-WS Foreign', 'cross-ws-foreign', '', 'XWF')
		RETURNING id
	`).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM workspace WHERE slug = 'cross-ws-foreign'`)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, foreignWorkspaceID, foreignUserID); err != nil {
		t.Fatalf("add foreign member: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'Foreign Runtime', 'cloud', 'foreign_test', 'online', 'Foreign', '{}'::jsonb, now())
		RETURNING id
	`, foreignWorkspaceID).Scan(&foreignRuntimeID); err != nil {
		t.Fatalf("create foreign runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'foreign-private-agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, foreignWorkspaceID, foreignRuntimeID, foreignUserID).Scan(&foreignAgentID); err != nil {
		t.Fatalf("create foreign agent: %v", err)
	}

	// Create an issue in OUR workspace and a comment that @mentions the
	// foreign agent's UUID. testUserID is owner of our workspace; pre-fix
	// the gate would have applied our-workspace-owner status to the foreign
	// agent and enqueued a task.
	var issueID, commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'cross-ws mention test', 'todo', 'medium', 'member', $2,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Multica's mention format is markdown-linked: [@Name](mention://agent/<uuid>).
	mention := "[@Foreign](mention://agent/" + foreignAgentID + ")"
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, $4)
		RETURNING id
	`, testWorkspaceID, issueID, testUserID, mention).Scan(&commentID); err != nil {
		t.Fatalf("create test comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load test issue: %v", err)
	}
	comment, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(commentID))
	if err != nil {
		t.Fatalf("load test comment: %v", err)
	}

	// Count tasks for the foreign agent before. Calling the dispatcher
	// directly bypasses HTTP-layer concerns and exercises only the
	// workspace-scoping check.
	var beforeCount int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM agent_task_queue WHERE agent_id = $1`,
		foreignAgentID,
	).Scan(&beforeCount); err != nil {
		t.Fatalf("count tasks before: %v", err)
	}

	testHandler.enqueueMentionedAgentTasks(ctx, issue, comment, nil, "member", testUserID)

	var afterCount int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM agent_task_queue WHERE agent_id = $1`,
		foreignAgentID,
	).Scan(&afterCount); err != nil {
		t.Fatalf("count tasks after: %v", err)
	}
	if afterCount != beforeCount {
		t.Fatalf("foreign agent task count changed: before=%d after=%d — cross-workspace mention was not rejected",
			beforeCount, afterCount)
	}
}
