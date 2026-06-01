package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

// issueFilterFixture seeds a small, self-contained issue set used by the
// filter-contract tests: it returns the second member, the agent owned by the
// test user, and the ids of the four issues created.
type issueFilterFixture struct {
	otherUserID string
	agentID     string
	mineTodo    string // assignee member testUserID, creator testUserID, todo
	otherTodo   string // assignee member otherUser,  creator testUserID, todo
	agentProg   string // assignee agent (owned by me), creator otherUser, in_progress
	noAssignee  string // no assignee,                 creator testUserID, todo
}

func seedIssueFilterFixture(t *testing.T) issueFilterFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var otherUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Filter Other User", fmt.Sprintf("filter-other-%d@multica.ai", suffix)).Scan(&otherUserID); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherUserID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUserID); err != nil {
		t.Fatalf("create other member: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Filter Agent %d", suffix), testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	create := func(title, status, assigneeType, assigneeID, creatorID string) string {
		t.Helper()
		var number int32
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(
				issue_counter,
				(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
			) + 1
			WHERE id = $1
			RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		var atype, aid any
		if assigneeType != "" {
			atype, aid = assigneeType, assigneeID
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, description, status, priority,
				assignee_type, assignee_id, creator_type, creator_id, position, number
			)
			VALUES ($1, $2, NULL, $3, 'none', $4, $5, 'member', $6, 0, $7)
			RETURNING id
		`, testWorkspaceID, title, status, atype, aid, creatorID, number).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}

	return issueFilterFixture{
		otherUserID: otherUserID,
		agentID:     agentID,
		mineTodo:    create(fmt.Sprintf("mine todo %d", suffix), "todo", "member", testUserID, testUserID),
		otherTodo:   create(fmt.Sprintf("other todo %d", suffix), "todo", "member", otherUserID, testUserID),
		agentProg:   create(fmt.Sprintf("agent prog %d", suffix), "in_progress", "agent", agentID, otherUserID),
		noAssignee:  create(fmt.Sprintf("no assignee %d", suffix), "todo", "", "", testUserID),
	}
}

func listIssueIDs(t *testing.T, query string) []string {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", "/api/issues?"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues(%s): expected 200, got %d: %s", query, w.Code, w.Body.String())
	}
	var resp struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	ids := make([]string, 0, len(resp.Issues))
	for _, i := range resp.Issues {
		ids = append(ids, i.ID)
	}
	sort.Strings(ids)
	return ids
}

func groupedIssueIDs(t *testing.T, query string) []string {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListGroupedIssues(w, newRequest("GET", "/api/issues/grouped?group_by=assignee&"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListGroupedIssues(%s): expected 200, got %d: %s", query, w.Code, w.Body.String())
	}
	var resp GroupedIssuesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode grouped response: %v", err)
	}
	ids := []string{}
	for _, g := range resp.Groups {
		for _, i := range g.Issues {
			ids = append(ids, i.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func expectStatus(t *testing.T, query string, want int) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", "/api/issues?"+query, nil))
	if w.Code != want {
		t.Fatalf("ListIssues(%s): expected %d, got %d: %s", query, want, w.Code, w.Body.String())
	}
}

// assertIDSet requires an exact match. Use only for entity-scoped queries
// whose result is bounded to freshly-created fixture ids — the shared test
// workspace accumulates issues from other tests, so class-level queries
// (assignee_types, creator=me) must use assertContains / assertExcludes.
func assertIDSet(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("id set mismatch: got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("id set mismatch: got %v want %v", got, want)
		}
	}
}

func containsID(set []string, id string) bool {
	for _, s := range set {
		if s == id {
			return true
		}
	}
	return false
}

func assertContains(t *testing.T, got []string, want ...string) {
	t.Helper()
	for _, id := range want {
		if !containsID(got, id) {
			t.Fatalf("expected id %s present in %v", id, got)
		}
	}
}

func assertExcludes(t *testing.T, got []string, notWant ...string) {
	t.Helper()
	for _, id := range notWant {
		if containsID(got, id) {
			t.Fatalf("expected id %s absent from %v", id, got)
		}
	}
}

// TestListIssuesAssigneeTypeFilter verifies the new orthogonal assignee_types
// param filters by assignee class on GET /api/issues.
func TestListIssuesAssigneeTypeFilter(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedIssueFilterFixture(t)
	ws := "workspace_id=" + testWorkspaceID

	// The shared test workspace holds issues from other tests, so assert on the
	// presence/absence of our own fixtures rather than the full set.
	got := listIssueIDs(t, ws+"&statuses=todo,in_progress&assignee_types=member")
	assertContains(t, got, f.mineTodo, f.otherTodo)
	assertExcludes(t, got, f.agentProg, f.noAssignee)

	got = listIssueIDs(t, ws+"&statuses=todo,in_progress&assignee_types=agent")
	assertContains(t, got, f.agentProg)
	assertExcludes(t, got, f.mineTodo, f.otherTodo, f.noAssignee)
}

// TestListIssuesMeToken verifies {me} expands to the requesting user inside
// assignee_filters / creator_filters, and is rejected for non-member actors.
func TestListIssuesMeToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedIssueFilterFixture(t)
	ws := "workspace_id=" + testWorkspaceID

	// assignee is me → our issue assigned to testUserID, never the others.
	got := listIssueIDs(t, ws+"&assignee_filters=member:{me}")
	assertContains(t, got, f.mineTodo)
	assertExcludes(t, got, f.otherTodo, f.agentProg, f.noAssignee)

	// creator is me → the three fixtures created by testUserID, never agentProg.
	got = listIssueIDs(t, ws+"&creator_filters=member:{me}")
	assertContains(t, got, f.mineTodo, f.otherTodo, f.noAssignee)
	assertExcludes(t, got, f.agentProg)

	// {me} is only meaningful for members.
	expectStatus(t, ws+"&assignee_filters=agent:{me}", http.StatusBadRequest)
}

// TestListIssuesIncludeNoAssignee verifies include_no_assignee unions the
// unassigned bucket with the specific assignee filters.
func TestListIssuesIncludeNoAssignee(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedIssueFilterFixture(t)
	ws := "workspace_id=" + testWorkspaceID

	got := listIssueIDs(t, ws+"&statuses=todo&assignee_filters=member:"+testUserID+"&include_no_assignee=true")
	assertContains(t, got, f.mineTodo, f.noAssignee)
	assertExcludes(t, got, f.otherTodo, f.agentProg)
}

// TestListIssuesFilterValidation pins the 400 contract for malformed filters.
func TestListIssuesFilterValidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	_ = seedIssueFilterFixture(t)
	ws := "workspace_id=" + testWorkspaceID

	expectStatus(t, ws+"&assignee_types=bogus", http.StatusBadRequest)
	expectStatus(t, ws+"&assignee_filters=member:not-a-uuid", http.StatusBadRequest)
	// assignee_types (class-level) and assignee_filters (entity-level) are
	// mutually exclusive.
	expectStatus(t, ws+"&assignee_types=member&assignee_filters=member:"+testUserID, http.StatusBadRequest)
}

// TestListGroupedFilterParity verifies GET /api/issues and GET
// /api/issues/grouped return the same issue id set for identical filters —
// the contract that lets a saved view drive either rendering mode.
func TestListGroupedFilterParity(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedIssueFilterFixture(t)
	ws := "workspace_id=" + testWorkspaceID

	// Scope to freshly-created entity ids (otherUser / agent) so the result is
	// bounded to our fixtures: list applies the page limit flat while grouped
	// applies it per group, so an unbounded class-level query could legitimately
	// diverge on a large shared workspace. Each case asserts (a) list == grouped
	// and (b) the known fixture is present, so an empty==empty pass can't hide a
	// broken filter.
	cases := []struct {
		query string
		want  string
	}{
		{ws + "&assignee_filters=member:" + f.otherUserID, f.otherTodo},
		{ws + "&assignee_filters=agent:" + f.agentID, f.agentProg},
		{ws + "&creator_filters=member:" + f.otherUserID, f.agentProg},
		{ws + "&statuses=in_progress&assignee_filters=agent:" + f.agentID, f.agentProg},
	}
	for _, c := range cases {
		list := listIssueIDs(t, c.query+"&limit=100")
		grouped := groupedIssueIDs(t, c.query+"&limit=100")
		assertIDSet(t, grouped, list...)
		assertContains(t, list, c.want)
	}
}
