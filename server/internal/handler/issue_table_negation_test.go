package handler

import (
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// "Is not" keeps exactly the rows "is" drops — including rows where the
// dimension has no value at all — and relations OR together the ways the
// requesting user relates to an issue.
func TestIssueTableRowsNegationAndRelations(t *testing.T) {
	suffix := time.Now().UnixNano()
	otherUser := dbfx.User(t, fmt.Sprintf("negate other %d", suffix), fmt.Sprintf("negate-%d@example.com", suffix))
	myAgent := dbfx.Agent(t, fmt.Sprintf("negate mine %d", suffix), "")
	otherAgent := dbfx.Agent(t, fmt.Sprintf("negate theirs %d", suffix), "", testutil.Cols{"owner_id": otherUser})
	project := dbfx.Project(t, fmt.Sprintf("negate project %d", suffix))

	issue := func(title string, cols testutil.Cols) string {
		return dbfx.Issue(t, fmt.Sprintf("negate %s %d", title, suffix), cols)
	}
	// Created by the test user unless stated otherwise.
	assignedToMe := issue("assigned to me", testutil.Cols{
		"status": "done", "assignee_type": "member", "assignee_id": testUserID, "project_id": project,
	})
	mineByAgent := issue("my agent", testutil.Cols{
		"assignee_type": "agent", "assignee_id": myAgent, "creator_id": otherUser,
	})
	theirAgent := issue("their agent", testutil.Cols{
		"assignee_type": "agent", "assignee_id": otherAgent, "creator_id": otherUser, "project_id": project,
	})
	unassignedByOther := issue("unassigned", testutil.Cols{
		"status": "in_progress", "creator_id": otherUser,
	})
	ledByMyAgent := dbfx.Squad(t, fmt.Sprintf("negate my squad %d", suffix), myAgent)
	withMe := dbfx.Squad(t, fmt.Sprintf("negate squad with me %d", suffix), otherAgent)
	dbfx.SquadMember(t, withMe, "member", testUserID)
	withMyAgent := dbfx.Squad(t, fmt.Sprintf("negate squad with my agent %d", suffix), otherAgent)
	dbfx.SquadMember(t, withMyAgent, "agent", myAgent)
	notMine := dbfx.Squad(t, fmt.Sprintf("negate their squad %d", suffix), otherAgent)
	mySquad := issue("my agent's squad", testutil.Cols{
		"assignee_type": "squad", "assignee_id": ledByMyAgent, "creator_id": otherUser,
	})
	squadWithMe := issue("squad with me", testutil.Cols{
		"assignee_type": "squad", "assignee_id": withMe, "creator_id": otherUser,
	})
	squadWithMyAgent := issue("squad with my agent", testutil.Cols{
		"assignee_type": "squad", "assignee_id": withMyAgent, "creator_id": otherUser,
	})
	theirSquad := issue("their squad", testutil.Cols{
		"assignee_type": "squad", "assignee_id": notMine, "creator_id": otherUser,
	})

	subscribe := func(issueID, userType, userID string) {
		dbfx.InsertNoID(t, "issue_subscriber", testutil.Cols{
			"issue_id": issueID, "user_type": userType, "user_id": userID, "reason": "manual",
		}, "issue_id = $1", issueID)
	}
	// Only the test user's own member rows count as "subscribed".
	subscribe(assignedToMe, "member", testUserID)
	subscribe(theirAgent, "member", testUserID)
	subscribe(unassignedByOther, "member", otherUser)
	subscribe(mineByAgent, "agent", myAgent)

	fixture := map[string]struct{}{
		assignedToMe: {}, mineByAgent: {}, theirAgent: {}, unassignedByOther: {},
		mySquad: {}, squadWithMe: {}, squadWithMyAgent: {}, theirSquad: {},
	}
	rows := func(filters issueTableFiltersRequest) []string {
		t.Helper()
		var response issueTableRowsResponse
		testutil.Call(t, testHandler.ListIssueTableRows,
			newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
				Query: issueTableQuerySpec{
					Scope:   issueTableScope{Kind: "workspace"},
					Filters: filters,
					Sort:    issueTableSortRequest{Field: "title", Direction: "asc"},
				},
				Group: issueTableGroupSpec{Kind: "none"},
				Page:  issueTablePageRequest{Limit: 100},
			}),
		).Want(http.StatusOK).JSON(&response)
		ids := make([]string, 0, len(response.Rows))
		for _, row := range response.Rows {
			if _, ok := fixture[row.Issue.ID]; ok {
				ids = append(ids, row.Issue.ID)
			}
		}
		sort.Strings(ids)
		return ids
	}
	assertRows := func(name string, filters issueTableFiltersRequest, want ...string) {
		t.Helper()
		got := rows(filters)
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: ids = %v, want %v", name, got, want)
		}
	}

	assertRows("status is not done",
		issueTableFiltersRequest{Statuses: []string{"done"}, Negate: []string{"statuses"}},
		mineByAgent, theirAgent, unassignedByOther, mySquad, squadWithMe, squadWithMyAgent, theirSquad)
	// The unassigned row has no assignee to compare: "is not" keeps it.
	assertRows("assignee is not me keeps unassigned rows",
		issueTableFiltersRequest{
			Assignees: []issueTableActorRef{{Type: "member", ID: testUserID}},
			Negate:    []string{"assignees"},
		},
		mineByAgent, theirAgent, unassignedByOther, mySquad, squadWithMe, squadWithMyAgent, theirSquad)
	assertRows("assignee is not 'no assignee'",
		issueTableFiltersRequest{IncludeNoAssignee: true, Negate: []string{"assignees"}},
		assignedToMe, mineByAgent, theirAgent, mySquad, squadWithMe, squadWithMyAgent, theirSquad)
	assertRows("project is not P keeps projectless rows",
		issueTableFiltersRequest{ProjectIDs: []string{project}, Negate: []string{"project_ids"}},
		mineByAgent, unassignedByOther, mySquad, squadWithMe, squadWithMyAgent, theirSquad)
	assertRows("negating a dimension without values changes nothing",
		issueTableFiltersRequest{Negate: []string{"label_ids", "priorities"}},
		assignedToMe, mineByAgent, theirAgent, unassignedByOther, mySquad, squadWithMe, squadWithMyAgent, theirSquad)

	assertRows("relation assigned",
		issueTableFiltersRequest{Relations: []string{"assigned"}},
		assignedToMe)
	assertRows("relation my agents matches the involved scope: my agents and my squads",
		issueTableFiltersRequest{Relations: []string{"my_agents"}},
		mineByAgent, mySquad, squadWithMe, squadWithMyAgent)
	assertRows("relations OR within the dimension",
		issueTableFiltersRequest{Relations: []string{"assigned", "my_agents"}},
		assignedToMe, mineByAgent, mySquad, squadWithMe, squadWithMyAgent)
	assertRows("relation is not my agents",
		issueTableFiltersRequest{Relations: []string{"my_agents"}, Negate: []string{"relations"}},
		assignedToMe, theirAgent, unassignedByOther, theirSquad)
	assertRows("relation subscribed matches only my member subscriptions",
		issueTableFiltersRequest{Relations: []string{"subscribed"}},
		assignedToMe, theirAgent)
	assertRows("relation is not subscribed",
		issueTableFiltersRequest{Relations: []string{"subscribed"}, Negate: []string{"relations"}},
		mineByAgent, unassignedByOther, mySquad, squadWithMe, squadWithMyAgent, theirSquad)
	subscribedNotMine := issueTableFilterNode{Match: "all", Children: []issueTableFilterNode{
		{Field: "relations", Values: rawValues("subscribed")},
		{Field: "relations", Negate: true, Values: rawValues("assigned", "my_agents")},
	}}
	assertRows("subscribed but not assigned to me or my agents",
		issueTableFiltersRequest{Advanced: &subscribedNotMine},
		theirAgent)
	assertRows("relation is not created by me",
		issueTableFiltersRequest{Relations: []string{"created"}, Negate: []string{"relations"}},
		mineByAgent, theirAgent, unassignedByOther, mySquad, squadWithMe, squadWithMyAgent, theirSquad)
	assertRows("negation is per dimension and ANDs with the rest",
		issueTableFiltersRequest{
			Relations: []string{"assigned"},
			Statuses:  []string{"done"},
			Negate:    []string{"relations"},
		},
	)

	// The id list clients filter "my agents" with holds the same squads.
	involved := map[string]bool{}
	for _, id := range involvedSquadIDs(t) {
		involved[id] = true
	}
	for squad, want := range map[string]bool{ledByMyAgent: true, withMe: true, withMyAgent: true, notMine: false} {
		if involved[squad] != want {
			t.Fatalf("involved squads include %s = %v, want %v", squad, involved[squad], want)
		}
	}
}

func involvedSquadIDs(t *testing.T) []string {
	t.Helper()
	var out struct {
		SquadIDs []string `json:"squad_ids"`
	}
	req := newRequest(http.MethodGet, "/api/squads/involved", nil)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
	testutil.Call(t, testHandler.ListInvolvedSquadIDs, req).Want(http.StatusOK).JSON(&out)
	return out.SquadIDs
}

func TestIssueTableRowsRejectsUnknownNegateAndRelation(t *testing.T) {
	for name, filters := range map[string]issueTableFiltersRequest{
		"negate":   {Negate: []string{"date"}},
		"relation": {Relations: []string{"involved"}},
	} {
		t.Run(name, func(t *testing.T) {
			testutil.Call(t, testHandler.ListIssueTableRows,
				newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
					Query: issueTableQuerySpec{
						Scope:   issueTableScope{Kind: "workspace"},
						Filters: filters,
						Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
					},
					Group: issueTableGroupSpec{Kind: "none"},
					Page:  issueTablePageRequest{Limit: 50},
				}),
			).Want(http.StatusBadRequest)
		})
	}
}

func TestIssueTableFingerprintCanonicalizesNegateAndRelations(t *testing.T) {
	spec := func(negate, relations []string) issueTableQuerySpec {
		return issueTableQuerySpec{
			Scope: issueTableScope{Kind: "workspace"},
			Filters: issueTableFiltersRequest{
				Statuses:  []string{"done"},
				Relations: relations,
				Negate:    negate,
			},
			Sort: issueTableSortRequest{Field: "position", Direction: "asc"},
		}
	}
	a, err := canonicalIssueTableFingerprint(testWorkspaceID, spec(
		[]string{"statuses", "relations"}, []string{"created", "assigned"}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalIssueTableFingerprint(testWorkspaceID, spec(
		[]string{"relations", "statuses", "statuses"}, []string{"assigned", "created"}))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("order and duplicates changed the fingerprint: %s != %s", a, b)
	}
	plain, err := canonicalIssueTableFingerprint(testWorkspaceID, spec(nil, []string{"assigned", "created"}))
	if err != nil {
		t.Fatal(err)
	}
	if plain == a {
		t.Fatal("negation did not change the fingerprint")
	}
}

func TestIssueTableQueryWithoutFacetDropsItsNegation(t *testing.T) {
	query := issueTableQuerySpec{Filters: issueTableFiltersRequest{
		Statuses: []string{"done"},
		LabelIDs: []string{"00000000-0000-0000-0000-000000000001"},
		Negate:   []string{"statuses", "label_ids"},
	}}
	got := issueTableQueryWithoutFacet(query, issueTableFacetSpec{Kind: "status"})
	if got.Filters.Statuses != nil || fmt.Sprint(got.Filters.Negate) != "[label_ids]" {
		t.Fatalf("status facet query = %+v", got.Filters)
	}
	if fmt.Sprint(query.Filters.Negate) != "[statuses label_ids]" {
		t.Fatalf("input negate mutated: %v", query.Filters.Negate)
	}
}
