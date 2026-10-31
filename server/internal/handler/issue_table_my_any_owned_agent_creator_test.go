package handler

import (
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The My Issues "All" tab (scope my, relation any) is the union of the
// assigned / created / involved legs plus issues created by an agent the user
// owns. The other relations keep their narrower meaning: Agents (`involved`)
// stays assignee-based so it remains disjoint from Assigned.
func TestIssueTableMyAnyIncludesIssuesCreatedByOwnedAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	otherID := dbfx.User(t, "my any other member", "my-any-other@multica.test")
	dbfx.Member(t, testWorkspaceID, otherID, "member")
	myAgent := dbfx.Agent(t, "my any owned agent", testRuntimeID)
	otherAgent := dbfx.Agent(t, "my any other agent", testRuntimeID, testutil.Cols{"owner_id": otherID})

	byMyAgent := dbfx.Issue(t, "my any created by my agent", testutil.Cols{
		"creator_type": "agent", "creator_id": myAgent,
	})
	byMyAgentAssignedToMe := dbfx.Issue(t, "my any created by my agent, assigned to me", testutil.Cols{
		"creator_type": "agent", "creator_id": myAgent,
		"assignee_type": "member", "assignee_id": testUserID,
	})
	byOtherAgent := dbfx.Issue(t, "my any created by another owner's agent", testutil.Cols{
		"creator_type": "agent", "creator_id": otherAgent,
	})
	fixture := map[string]struct{}{byMyAgent: {}, byMyAgentAssignedToMe: {}, byOtherAgent: {}}

	// The workspace is shared, so read back only the rows this test wrote.
	rows := func(userID, relation string) []string {
		t.Helper()
		var response issueTableRowsResponse
		req := newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope: issueTableScope{Kind: "my", Relation: relation},
				Sort:  issueTableSortRequest{Field: "title", Direction: "asc"},
			},
			Group: issueTableGroupSpec{Kind: "none"},
			Page:  issueTablePageRequest{Limit: 100},
		})
		req.Header.Set("X-User-ID", userID)
		testutil.Call(t, testHandler.ListIssueTableRows, req).Want(http.StatusOK).JSON(&response)

		ids := make([]string, 0, len(response.Rows))
		for _, row := range response.Rows {
			if _, ok := fixture[row.Issue.ID]; ok {
				ids = append(ids, row.Issue.ID)
			}
		}
		sort.Strings(ids)
		return ids
	}

	assertRows := func(name, userID, relation string, want ...string) {
		t.Helper()
		got := rows(userID, relation)
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: ids = %v, want %v", name, got, want)
		}
	}

	assertRows("my any", testUserID, "any", byMyAgent, byMyAgentAssignedToMe)
	assertRows("my any (default relation)", testUserID, "", byMyAgent, byMyAgentAssignedToMe)
	assertRows("my assigned", testUserID, "assigned", byMyAgentAssignedToMe)
	assertRows("my created ignores agent creators", testUserID, "created")
	assertRows("my involved stays assignee-based", testUserID, "involved")
	assertRows("other user's any", otherID, "any", byOtherAgent)
}
