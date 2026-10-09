package handler

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueTableConcreteStatusCatalogOrder(t *testing.T) {
	projectID, customKey := seedStatusCategoryFixture(t)
	for _, position := range []int{-100, 100} {
		if _, err := testPool.Exec(context.Background(), `UPDATE issue_status SET position = $3 WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, customKey, position); err != nil {
			t.Fatal(err)
		}
		request := issueTableGroupsRequest{Query: statusCategoryQuery(projectID), Group: issueTableGroupSpec{Kind: "status"}, Page: issueTablePageRequest{Limit: 1}}
		var got []string
		for page := 0; page < 4; page++ {
			var response issueTableGroupsResponse
			testutil.Call(t, testHandler.ListIssueTableGroups, newRequest(http.MethodPost, "/api/issues/table/groups", request)).Want(http.StatusOK).JSON(&response)
			for _, group := range response.Groups {
				got = append(got, group.Value.Status)
			}
			if response.NextCursor == nil {
				break
			}
			request.Page.Cursor = response.NextCursor
		}
		want := []string{"todo", customKey, "in_review"}
		if position > 0 {
			want = []string{"todo", "in_review", customKey}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("position %d: groups = %v, want %v", position, got, want)
		}
	}
}

func TestIssueTableCompoundConcreteCustomStatuses(t *testing.T) {
	projectID, customKey := seedStatusCategoryFixture(t)
	query := statusCategoryQuery(projectID)
	foreignWorkspaceID := dbfx.Workspace(t, "Foreign status columns", fmt.Sprintf("foreign-status-columns-%d", time.Now().UnixNano()))
	foreignKey := customKey + "_foreign"
	if _, err := testPool.Exec(context.Background(), `
 INSERT INTO issue_status (workspace_id, key, name, description, category, color, position)
 VALUES ($1, $2, 'Foreign QA', '', 'started', '#ff0000', 1)
 `, foreignWorkspaceID, foreignKey); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "DELETE FROM issue_status WHERE workspace_id = $1", foreignWorkspaceID)
	})

	for _, primary := range []string{"project", "parent", "assignee"} {
		t.Run(primary, func(t *testing.T) {
			spec := issueTableGroupSpec{
				Kind: "compound", Primary: primary, Secondary: "status",
				SecondaryValues: []string{customKey, "in_review", "todo"},
			}
			var groups issueTableGroupsResponse
			testutil.Call(t, testHandler.ListIssueTableGroups,
				newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{
					Query: query, Group: spec, Page: issueTablePageRequest{Limit: 100},
				})).Want(http.StatusOK).JSON(&groups)
			counts := map[string]int64{}
			var customCell string
			for _, lane := range groups.Groups {
				for _, cell := range lane.SecondaryGroups {
					counts[cell.Value.Status] += cell.Count
					if cell.Value.Status == customKey && cell.Count > 0 {
						customCell = cell.Key
					}
				}
			}
			if counts[customKey] != 2 || counts["in_review"] != 1 || counts["todo"] != 1 {
				t.Fatalf("exact status counts = %v", counts)
			}
			var rows issueTableRowsResponse
			request := issueTableRowsRequest{
				Query: query, Group: spec, GroupKey: &customCell, Page: issueTablePageRequest{Limit: 1},
			}
			testutil.Call(t, testHandler.ListIssueTableRows,
				newRequest(http.MethodPost, "/api/issues/table/rows", request)).
				Want(http.StatusOK).JSON(&rows)
			if len(rows.Rows) != 1 || rows.NextCursor == nil {
				t.Fatalf("custom page must retain its continuation: %+v", rows)
			}
			if rows.Rows[0].Issue.Status != customKey {
				t.Fatalf("unexpected status: %s", rows.Rows[0].Issue.Status)
			}
			firstID := rows.Rows[0].Issue.ID
			request.Page.Cursor = rows.NextCursor
			testutil.Call(t, testHandler.ListIssueTableRows,
				newRequest(http.MethodPost, "/api/issues/table/rows", request)).
				Want(http.StatusOK).JSON(&rows)
			if len(rows.Rows) != 1 || rows.NextCursor != nil {
				t.Fatalf("custom tail must contain the second custom row only: %+v", rows)
			}
			if rows.Rows[0].Issue.Status != customKey || rows.Rows[0].Issue.ID == firstID {
				t.Fatalf("tail repeated or mixed statuses: %+v", rows.Rows)
			}
			spec.SecondaryValues = []string{foreignKey}
			testutil.Call(t, testHandler.ListIssueTableGroups,
				newRequest(http.MethodPost, "/api/issues/table/groups", issueTableGroupsRequest{
					Query: query, Group: spec, Page: issueTablePageRequest{Limit: 100},
				})).Want(http.StatusBadRequest)
		})
	}
}
