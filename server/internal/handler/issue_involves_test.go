package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// involvesFixture seeds the rows every involves-user-id test variant needs:
// a second user, their owned agent, plus an issue counter helper for stable
// numbering. Tests scope further insertions (squads, members, more issues) on
// top of these baseline rows.
type involvesFixture struct {
	userID    string
	agentID   string
	otherWsID string
}

func setupInvolvesFixture(t *testing.T) *involvesFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Involves Test User", fmt.Sprintf("involves-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Agent owned by the involves user, in the test workspace, on the seeded runtime.
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Involves Agent %d", suffix), testRuntimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	// Sibling workspace + runtime so we can prove workspace scoping with
	// agents that share an owner across workspaces.
	var otherWsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'OTH')
		RETURNING id
	`, fmt.Sprintf("Involves Other WS %d", suffix), fmt.Sprintf("involves-other-%d", suffix)).Scan(&otherWsID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWsID)
	})

	return &involvesFixture{userID: userID, agentID: agentID, otherWsID: otherWsID}
}

// nextIssueNumber claims the next issue number in the test workspace using
// the same row update the production CreateIssue path runs, so direct INSERTs
// from these tests don't collide with anything else seeded in the suite.
func nextIssueNumber(t *testing.T, ctx context.Context, workspaceID string) int32 {
	t.Helper()
	var n int32
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(
			issue_counter,
			(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
		) + 1
		WHERE id = $1
		RETURNING issue_counter
	`, workspaceID).Scan(&n); err != nil {
		t.Fatalf("next issue number: %v", err)
	}
	return n
}

func insertIssueTo(t *testing.T, ctx context.Context, workspaceID, title, assigneeType, assigneeID string) string {
	t.Helper()
	number := nextIssueNumber(t, ctx, workspaceID)
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, description, status, priority,
			assignee_type, assignee_id, creator_type, creator_id,
			position, number
		)
		VALUES ($1, $2, NULL, 'todo', 'none', $3, $4, 'member', $5, 0, $6)
		RETURNING id
	`, workspaceID, title, assigneeType, assigneeID, testUserID, number).Scan(&id); err != nil {
		t.Fatalf("insert issue %q: %v", title, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
	})
	return id
}

func insertSquad(t *testing.T, ctx context.Context, workspaceID, name, leaderID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, workspaceID, name, leaderID, testUserID).Scan(&id); err != nil {
		t.Fatalf("insert squad: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, id)
	})
	return id
}

func insertSquadMember(t *testing.T, ctx context.Context, squadID, memberType, memberID string) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id, role)
		VALUES ($1, $2, $3, '')
	`, squadID, memberType, memberID); err != nil {
		t.Fatalf("insert squad_member: %v", err)
	}
}

func listIssuesInvolves(t *testing.T, involvesUserID string) []IssueResponse {
	t.Helper()
	// limit=1000 is way above anything any of these tests seed; we just want
	// to avoid relying on the 100-row default page size hiding rows the
	// assertion depends on.
	path := fmt.Sprintf(
		"/api/issues?workspace_id=%s&involves_user_id=%s&limit=1000",
		testWorkspaceID, involvesUserID,
	)
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Issues
}

func containsIssueID(issues []IssueResponse, id string) bool {
	for _, i := range issues {
		if i.ID == id {
			return true
		}
	}
	return false
}

func TestListIssues_InvolvesUserID_MatchesSquadMember(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	// Seeded test agent leads the squad; the involves user joins as a human member.
	var leaderAgentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 AND id <> $2 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID, fix.agentID).Scan(&leaderAgentID); err != nil {
		t.Fatalf("load seeded leader agent: %v", err)
	}
	squadID := insertSquad(t, ctx, testWorkspaceID, fmt.Sprintf("Squad Member %d", time.Now().UnixNano()), leaderAgentID)
	insertSquadMember(t, ctx, squadID, "member", fix.userID)

	issueID := insertIssueTo(t, ctx, testWorkspaceID, "involves member squad", "squad", squadID)
	if got := listIssuesInvolves(t, fix.userID); !containsIssueID(got, issueID) {
		t.Fatalf("expected involves match via squad_member.member, got %d issues", len(got))
	}
}

// Pins the v3 semantics that the leader relation is sourced from canonical
// squad.leader_id, not from squad_member. Inserts a squad whose leader is the
// involves user's agent and deliberately skips the squad_member copy row that
// the production handler best-effort-creates (see squad.go:177-188).
func TestListIssues_InvolvesUserID_MatchesLeaderViaCanonicalRelation(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	squadID := insertSquad(t, ctx, testWorkspaceID, fmt.Sprintf("Canonical Leader %d", time.Now().UnixNano()), fix.agentID)
	// Deliberately no squad_member row for the leader.
	var sanity int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM squad_member
		WHERE squad_id = $1 AND member_type = 'agent' AND member_id = $2
	`, squadID, fix.agentID).Scan(&sanity); err != nil {
		t.Fatalf("sanity check: %v", err)
	}
	if sanity != 0 {
		t.Fatalf("test invariant broken: expected no leader squad_member row, found %d", sanity)
	}

	issueID := insertIssueTo(t, ctx, testWorkspaceID, "involves canonical leader", "squad", squadID)
	if got := listIssuesInvolves(t, fix.userID); !containsIssueID(got, issueID) {
		t.Fatalf("expected involves match via canonical squad.leader_id even without squad_member copy row, got %d issues", len(got))
	}
}

func TestListIssues_InvolvesUserID_MatchesSquadAgentMember(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	// Seeded test agent is the leader; user's own agent is added as a plain member.
	var leaderAgentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 AND id <> $2 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID, fix.agentID).Scan(&leaderAgentID); err != nil {
		t.Fatalf("load seeded leader agent: %v", err)
	}
	squadID := insertSquad(t, ctx, testWorkspaceID, fmt.Sprintf("Agent Member %d", time.Now().UnixNano()), leaderAgentID)
	insertSquadMember(t, ctx, squadID, "agent", fix.agentID)

	issueID := insertIssueTo(t, ctx, testWorkspaceID, "involves agent member", "squad", squadID)
	if got := listIssuesInvolves(t, fix.userID); !containsIssueID(got, issueID) {
		t.Fatalf("expected involves match via squad_member.agent, got %d issues", len(got))
	}
}

func TestListIssues_InvolvesUserID_MatchesOwnedAgentAssignee(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	issueID := insertIssueTo(t, ctx, testWorkspaceID, "involves agent direct", "agent", fix.agentID)
	if got := listIssuesInvolves(t, fix.userID); !containsIssueID(got, issueID) {
		t.Fatalf("expected involves match via owned agent assignee, got %d issues", len(got))
	}
}

func TestListIssues_InvolvesUserID_MatchesDirectMemberAssignee(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	issueID := insertIssueTo(t, ctx, testWorkspaceID, "involves member direct", "member", fix.userID)
	if got := listIssuesInvolves(t, fix.userID); !containsIssueID(got, issueID) {
		t.Fatalf("expected involves match via direct member assignee, got %d issues", len(got))
	}
}

// Pins workspace scoping on the UNION's agent branch (issue.sql:18). The
// issue itself sits in testWorkspaceID so the outer `i.workspace_id = $1`
// can't mask a missing `a.workspace_id = $1`; assignee_id is polymorphic and
// has no FK (server/migrations/001_init.up.sql:61), so DB-level it's fine to
// point at a foreign-workspace agent.
func TestListIssues_InvolvesUserID_ExcludesOtherWorkspaceAgent(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	var otherRuntimeID, otherAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'Other Runtime', 'cloud', 'other', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, fix.otherWsID).Scan(&otherRuntimeID); err != nil {
		t.Fatalf("create other runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Other WS Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, fix.otherWsID, otherRuntimeID, fix.userID).Scan(&otherAgentID); err != nil {
		t.Fatalf("create other-ws agent: %v", err)
	}

	leakID := insertIssueTo(t, ctx, testWorkspaceID, "current-ws issue, foreign agent assignee", "agent", otherAgentID)
	for _, issue := range listIssuesInvolves(t, fix.userID) {
		if issue.ID == leakID {
			t.Fatalf("involves match leaked: agent UNION branch did not enforce workspace scoping")
		}
	}
}

// Pins workspace scoping on the UNION's canonical-leader branch
// (issue.sql:36-37). Current-workspace issue points at a foreign squad whose
// leader is a foreign-workspace agent owned by the involves user; only
// `s.workspace_id = $1` / `a.workspace_id = $1` can drop it.
func TestListIssues_InvolvesUserID_ExcludesOtherWorkspaceLeader(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	var otherRuntimeID, otherAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'Other Runtime Leader', 'cloud', 'other2', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, fix.otherWsID).Scan(&otherRuntimeID); err != nil {
		t.Fatalf("create other runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Other WS Leader Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, fix.otherWsID, otherRuntimeID, fix.userID).Scan(&otherAgentID); err != nil {
		t.Fatalf("create other-ws leader agent: %v", err)
	}

	otherSquadID := insertSquad(t, ctx, fix.otherWsID, "Other WS Squad", otherAgentID)
	leakID := insertIssueTo(t, ctx, testWorkspaceID, "current-ws issue, foreign squad assignee (leader)", "squad", otherSquadID)

	for _, issue := range listIssuesInvolves(t, fix.userID) {
		if issue.ID == leakID {
			t.Fatalf("involves match leaked: canonical-leader UNION branch did not enforce workspace scoping")
		}
	}
}

// Pins workspace scoping on the UNION's squad_member.member branch
// (issue.sql:26). Current-workspace issue points at a foreign squad whose
// human member is the involves user; only `s.workspace_id = $1` can drop it.
func TestListIssues_InvolvesUserID_ExcludesOtherWorkspaceSquadMember(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	var otherRuntimeID, otherLeaderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'Other Runtime Member', 'cloud', 'other3', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, fix.otherWsID).Scan(&otherRuntimeID); err != nil {
		t.Fatalf("create other runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Other WS Leader (member test)', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, fix.otherWsID, otherRuntimeID, testUserID).Scan(&otherLeaderID); err != nil {
		t.Fatalf("create other leader agent: %v", err)
	}

	otherSquadID := insertSquad(t, ctx, fix.otherWsID, "Other WS Squad Member", otherLeaderID)
	insertSquadMember(t, ctx, otherSquadID, "member", fix.userID)
	leakID := insertIssueTo(t, ctx, testWorkspaceID, "current-ws issue, foreign squad assignee (member)", "squad", otherSquadID)

	for _, issue := range listIssuesInvolves(t, fix.userID) {
		if issue.ID == leakID {
			t.Fatalf("involves match leaked: squad_member.member UNION branch did not enforce workspace scoping")
		}
	}
}

// Pins workspace scoping on the UNION's squad_member.agent branch
// (issue.sql:45-47). Current-workspace issue points at a foreign squad whose
// agent member is a foreign-workspace agent owned by the involves user; only
// `s.workspace_id = $1` / `a.workspace_id = $1` can drop it.
func TestListIssues_InvolvesUserID_ExcludesOtherWorkspaceSquadAgentMember(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	var otherRuntimeID, otherLeaderID, otherAgentMemberID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'Other Runtime AgentMember', 'cloud', 'other4', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, fix.otherWsID).Scan(&otherRuntimeID); err != nil {
		t.Fatalf("create other runtime: %v", err)
	}
	// Foreign-workspace leader so the squad satisfies its leader_id workspace
	// CHECK; owned by testUserID so it can't itself match the involves filter.
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Other WS Leader (agent-member test)', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, fix.otherWsID, otherRuntimeID, testUserID).Scan(&otherLeaderID); err != nil {
		t.Fatalf("create other leader agent: %v", err)
	}
	// Foreign-workspace agent owned by the involves user — the row whose
	// owner_id alone would mistakenly match if `a.workspace_id = $1` were
	// dropped from the agent-member subquery.
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Other WS Agent (foreign squad member)', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, fix.otherWsID, otherRuntimeID, fix.userID).Scan(&otherAgentMemberID); err != nil {
		t.Fatalf("create other agent member: %v", err)
	}

	otherSquadID := insertSquad(t, ctx, fix.otherWsID, "Other WS Squad AgentMember", otherLeaderID)
	insertSquadMember(t, ctx, otherSquadID, "agent", otherAgentMemberID)
	leakID := insertIssueTo(t, ctx, testWorkspaceID, "current-ws issue, foreign squad assignee (agent member)", "squad", otherSquadID)

	for _, issue := range listIssuesInvolves(t, fix.userID) {
		if issue.ID == leakID {
			t.Fatalf("involves match leaked: squad_member.agent UNION branch did not enforce workspace scoping")
		}
	}
}

func TestListIssues_InvolvesUserID_CombinesWithCreatorID(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	// One issue matches both creator_id=testUserID (default) and involves
	// (assignee = involves user's agent). Another matches only involves
	// (assignee = involves user's agent, but creator is the involves user).
	// Asserting creator_id AND involves_user_id keeps the first, drops the second.
	hit := insertIssueTo(t, ctx, testWorkspaceID, "creator AND involves", "agent", fix.agentID)

	miss := nextIssueNumber(t, ctx, testWorkspaceID)
	var missID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, description, status, priority,
			assignee_type, assignee_id, creator_type, creator_id,
			position, number
		)
		VALUES ($1, 'involves only, different creator', NULL, 'todo', 'none', 'agent', $2, 'member', $3, 0, $4)
		RETURNING id
	`, testWorkspaceID, fix.agentID, fix.userID, miss).Scan(&missID); err != nil {
		t.Fatalf("insert miss: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, missID)
	})

	path := fmt.Sprintf(
		"/api/issues?workspace_id=%s&involves_user_id=%s&creator_id=%s&limit=1000",
		testWorkspaceID, fix.userID, testUserID,
	)
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !containsIssueID(resp.Issues, hit) {
		t.Fatalf("expected combined filter to keep hit issue %s", hit)
	}
	if containsIssueID(resp.Issues, missID) {
		t.Fatalf("expected combined filter to exclude issue created by involves user (id=%s)", missID)
	}
}

func TestListIssues_InvolvesUserID_InvalidUUIDReturns400(t *testing.T) {
	path := fmt.Sprintf("/api/issues?workspace_id=%s&involves_user_id=not-a-uuid", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed involves_user_id, got %d: %s", w.Code, w.Body.String())
	}
}

// Grouped path mirrors the same UNION fragment; this test pins that the
// dynamic SQL injection at issue.go also respects involves_user_id and stays
// workspace-scoped.
func TestListGroupedIssues_InvolvesUserID_MatchesLeaderViaCanonicalRelation(t *testing.T) {
	ctx := context.Background()
	fix := setupInvolvesFixture(t)

	squadID := insertSquad(t, ctx, testWorkspaceID, fmt.Sprintf("Grouped Canonical %d", time.Now().UnixNano()), fix.agentID)
	issueID := insertIssueTo(t, ctx, testWorkspaceID, "grouped involves leader", "squad", squadID)

	path := fmt.Sprintf(
		"/api/issues/grouped?workspace_id=%s&group_by=assignee&statuses=todo&involves_user_id=%s",
		testWorkspaceID, fix.userID,
	)
	w := httptest.NewRecorder()
	testHandler.ListGroupedIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListGroupedIssues: %d %s", w.Code, w.Body.String())
	}
	var resp GroupedIssuesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, group := range resp.Groups {
		for _, i := range group.Issues {
			if i.ID == issueID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected grouped involves match for canonical leader, response: %+v", resp.Groups)
	}
}
