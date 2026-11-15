package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueRequesterAttributionAndCreatedSurfaces(t *testing.T) {
	ctx := context.Background()
	other := dbfx.User(t, "Requester peer", fmt.Sprintf("requester-%d@example.test", time.Now().UnixNano()))
	agent := dbfx.Agent(t, "Shared requester agent", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": other, "visibility": "workspace",
	})
	foreignAgent := dbfx.Agent(t, "Different origin agent", handlerTestRuntimeID(t))
	for _, tc := range []struct {
		name, origin, source, evidence      string
		other, direct, mismatch, historical bool
		want                                bool
	}{
		{name: "direct", direct: true, want: true},
		{name: "unattributed agent"},
		{name: "quick create", origin: "quick_create", source: "direct_human", want: true},
		{name: "peer quick create", origin: "quick_create", source: "direct_human", other: true},
		{name: "fallback", origin: "quick_create", source: "owner_fallback"},
		{name: "backfill", origin: "quick_create", source: "backfill"},
		{name: "legacy source", origin: "quick_create"},
		{name: "chat", origin: "agent_create", source: "direct_human", evidence: "chat", want: true},
		{name: "peer chat", origin: "agent_create", source: "direct_human", evidence: "chat", other: true},
		{name: "issue comment", origin: "agent_create", source: "direct_human", evidence: "comment"},
		{name: "delegation", origin: "agent_create", source: "delegation", evidence: "chat"},
		{name: "autopilot", origin: "agent_create", source: "trigger_owner", evidence: "autopilot_run"},
		{name: "different agent", origin: "agent_create", source: "direct_human", evidence: "chat", mismatch: true},
		{name: "historical chat", origin: "agent_create", source: "direct_human", evidence: "chat", historical: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title := fmt.Sprintf("Requester surface %s %d", tc.name, time.Now().UnixNano())
			creatorType, creatorID := "agent", agent
			if tc.direct {
				creatorType, creatorID = "member", testUserID
			}
			var origin pgtype.UUID
			if tc.origin != "" {
				user := testUserID
				if tc.other {
					user = other
				}
				cols := testutil.Cols{
					"status": "completed", "runtime_id": handlerTestRuntimeID(t), "originator_user_id": user, "accountable_user_id": user,
					"context": fmt.Sprintf(`{"type":"quick_create","workspace_id":%q}`, testWorkspaceID),
				}
				if tc.source != "" {
					cols["originator_source"] = tc.source
				}
				if tc.evidence != "" {
					cols["trigger_evidence_kind"] = tc.evidence
				}
				runAgent := agent
				if tc.mismatch {
					runAgent = foreignAgent
				}
				origin = parseUUID(dbfx.Task(t, runAgent, cols))
			}
			var id string
			if tc.historical {
				id = dbfx.Issue(t, title, testutil.Cols{"creator_type": creatorType, "creator_id": creatorID, "origin_type": tc.origin, "origin_id": origin})
			} else {
				result, err := testHandler.IssueService.Create(ctx, service.IssueCreateParams{
					WorkspaceID: parseUUID(testWorkspaceID), Title: title, Status: "todo", Priority: "none",
					CreatorType: creatorType, CreatorID: parseUUID(creatorID), OriginID: origin,
					OriginType: pgtype.Text{String: tc.origin, Valid: tc.origin != ""},
				}, service.IssueCreateOpts{})
				if err != nil {
					t.Fatal(err)
				}
				id = uuidToString(result.Issue.ID)
				dbfx.Cleanup(t, "DELETE FROM issue WHERE id=$1", id)
			}
			issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(id))
			if err != nil {
				t.Fatal(err)
			}
			response := issueToResponse(issue, "MUL")
			if got := response.RequesterID; !tc.direct && tc.want {
				if got == nil || *got != testUserID {
					t.Fatalf("requester=%v", got)
				}
			} else if tc.other {
				if got == nil || *got != other {
					t.Fatalf("peer requester=%v", got)
				}
			} else if got != nil {
				t.Fatalf("unexpected requester=%s", *got)
			}

			for _, spec := range []issueTableQuerySpec{
				{Scope: issueTableScope{Kind: "my", Relation: "created"}},
				{Scope: issueTableScope{Kind: "my", Relation: "any"}},
				{Scope: issueTableScope{Kind: "workspace"}, Filters: issueTableFiltersRequest{Relations: []string{"created"}}},
				{Scope: issueTableScope{Kind: "workspace"}, Filters: issueTableFiltersRequest{Relations: []string{"created"}, Negate: []string{"relations"}}},
			} {
				spec.Search = title
				var out issueTableRowsResponse
				testutil.Call(t, testHandler.ListIssueTableRows, newRequest("POST", "/api/issues/table/rows", issueTableRowsRequest{
					Query: spec, Group: issueTableGroupSpec{Kind: "none"}, Page: issueTablePageRequest{Limit: 100},
				})).Want(http.StatusOK).JSON(&out)
				found := false
				for _, row := range out.Rows {
					if row.Issue.ID == id {
						found = true
					}
				}
				want := tc.want
				if len(spec.Filters.Negate) > 0 {
					want = !want
				}
				if found != want {
					t.Fatalf("scope=%+v negate=%v found=%v want=%v", spec.Scope, spec.Filters.Negate, found, want)
				}
			}
			for _, suffix := range []string{"", "&open_only=true"} {
				var out struct {
					Issues       []IssueResponse `json:"issues"`
					Acknowledged string          `json:"created_by_user_id"`
				}
				testutil.Call(t, testHandler.ListIssues, newRequest("GET", "/api/issues?created_by_user_id="+testUserID+suffix, nil)).Want(http.StatusOK).JSON(&out)
				found := false
				for _, row := range out.Issues {
					if row.ID == id {
						found = true
					}
				}
				if found != tc.want || out.Acknowledged != testUserID {
					t.Fatalf("list%s found=%v acknowledged=%q", suffix, found, out.Acknowledged)
				}
			}
			var grouped GroupedIssuesResponse
			testutil.Call(t, testHandler.ListGroupedIssues, newRequest("GET", "/api/issues/grouped?group_by=assignee&created_by_user_id="+testUserID, nil)).Want(http.StatusOK).JSON(&grouped)
			found := false
			for _, group := range grouped.Groups {
				for _, row := range group.Issues {
					if row.ID == id {
						found = true
					}
				}
			}
			if found != tc.want {
				t.Fatalf("grouped found=%v want=%v", found, tc.want)
			}

			dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": id, "status": "running"})
			for _, relation := range []string{"created", "any"} {
				var working []WorkspaceWorkingAgent
				testutil.Call(t, testHandler.ListWorkspaceWorkingAgents, newRequest("GET", "/api/working-agents?type=issue&scope=mine&relation="+relation, nil)).Want(http.StatusOK).JSON(&working)
				found := false
				for _, worker := range working {
					for _, issueID := range worker.IssueIDs {
						if issueID == id {
							found = true
						}
					}
				}
				if found != tc.want {
					t.Fatalf("working %s found=%v want=%v", relation, found, tc.want)
				}
			}

			// A creator actor filter keeps its literal meaning.
			var literal struct {
				Issues []IssueResponse `json:"issues"`
			}
			testutil.Call(t, testHandler.ListIssues, newRequest("GET", "/api/issues?creator_id="+testUserID, nil)).Want(http.StatusOK).JSON(&literal)
			for _, row := range literal.Issues {
				if row.ID == id && !tc.direct {
					t.Fatal("literal creator_id included agent issue")
				}
			}

			if tc.want && !tc.direct {
				// The snapshot survives losing its provenance row.
				dbfx.Exec(t, "DELETE FROM agent_task_queue WHERE id=$1", origin)
				stored, err := testHandler.Queries.GetIssue(ctx, parseUUID(id))
				if err != nil || uuidToString(stored.RequesterUserID) != testUserID {
					t.Fatalf("lost snapshot: %v", err)
				}
			}
		})
	}
}

func TestIssueRequesterOriginWorkspace(t *testing.T) {
	workspace := dbfx.Workspace(t, "Requester isolated workspace", fmt.Sprintf("requester-isolation-%d", time.Now().UnixNano()))
	agent := dbfx.Agent(t, "Requester isolated agent", handlerTestRuntimeID(t), testutil.Cols{"workspace_id": workspace})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "status": "completed", "originator_user_id": testUserID, "accountable_user_id": testUserID, "originator_source": "direct_human", "trigger_evidence_kind": "chat"})
	_, err := testHandler.Queries.ResolveIssueRequester(context.Background(), db.ResolveIssueRequesterParams{OriginID: parseUUID(task), CreatorID: parseUUID(agent), WorkspaceID: parseUUID(testWorkspaceID), OriginType: "agent_create"})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-workspace origin resolved: %v", err)
	}
	_, err = testHandler.Queries.ResolveIssueRequester(context.Background(), db.ResolveIssueRequesterParams{OriginID: parseUUID(testUserID), CreatorID: parseUUID(agent), WorkspaceID: parseUUID(workspace), OriginType: "agent_create"})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing origin resolved: %v", err)
	}
}

func TestIssueCreatedByUserRejectsMalformedFilter(t *testing.T) {
	for _, path := range []string{"/api/issues?created_by_user_id=bad", "/api/issues?open_only=true&created_by_user_id=bad"} {
		testutil.Call(t, testHandler.ListIssues, newRequest("GET", path, nil)).Want(http.StatusBadRequest)
	}
	testutil.Call(t, testHandler.ListGroupedIssues, newRequest("GET", "/api/issues/grouped?group_by=assignee&created_by_user_id=bad", nil)).Want(http.StatusBadRequest)
}
