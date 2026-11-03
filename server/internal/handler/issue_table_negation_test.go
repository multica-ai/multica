package handler

import (
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
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

	fixture := map[string]struct{}{
		assignedToMe: {}, mineByAgent: {}, theirAgent: {}, unassignedByOther: {},
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
		mineByAgent, theirAgent, unassignedByOther)
	// The unassigned row has no assignee to compare: "is not" keeps it.
	assertRows("assignee is not me keeps unassigned rows",
		issueTableFiltersRequest{
			Assignees: []issueTableActorRef{{Type: "member", ID: testUserID}},
			Negate:    []string{"assignees"},
		},
		mineByAgent, theirAgent, unassignedByOther)
	assertRows("assignee is not 'no assignee'",
		issueTableFiltersRequest{IncludeNoAssignee: true, Negate: []string{"assignees"}},
		assignedToMe, mineByAgent, theirAgent)
	assertRows("project is not P keeps projectless rows",
		issueTableFiltersRequest{ProjectIDs: []string{project}, Negate: []string{"project_ids"}},
		mineByAgent, unassignedByOther)
	assertRows("negating a dimension without values changes nothing",
		issueTableFiltersRequest{Negate: []string{"label_ids", "priorities"}},
		assignedToMe, mineByAgent, theirAgent, unassignedByOther)

	assertRows("relation assigned",
		issueTableFiltersRequest{Relations: []string{"assigned"}},
		assignedToMe)
	assertRows("relation my agents excludes other people's agents",
		issueTableFiltersRequest{Relations: []string{"my_agents"}},
		mineByAgent)
	assertRows("relations OR within the dimension",
		issueTableFiltersRequest{Relations: []string{"assigned", "my_agents"}},
		assignedToMe, mineByAgent)
	assertRows("relation is not created by me",
		issueTableFiltersRequest{Relations: []string{"created"}, Negate: []string{"relations"}},
		mineByAgent, theirAgent, unassignedByOther)
	assertRows("negation is per dimension and ANDs with the rest",
		issueTableFiltersRequest{
			Relations: []string{"assigned"},
			Statuses:  []string{"done"},
			Negate:    []string{"relations"},
		},
	)
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
