package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestParseQueryNumber(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"MUL-123", 123},
		{"mul-123", 123},
		{"  123  ", 123},
		{"2147483647", 2147483647},
		{"MUL-2147483647", 2147483647},
		{"2147483648", 0},
		{"MUL-2147483648", 0},
		{"35185005423742", 0},
		{"  35185005423742  ", 0},
		{"MUL-99999999999", 0},
		{"V2-99999999999", 0},
		{"9223372036854775807", 0},
		{"9223372036854775808", 0},
		{"MUL-9223372036854775808", 0},
		{"-2147483649", 0},
		{"V2-12", 12},
		{"  v2-12  ", 12},
		{"A1-7", 7},
		{"a1b2-42", 42},
		{"12-3", 0},
		{"2FAS-1", 0},
		{"V2-0", 0},
		{"V2--12", 0},
		{"V2-12x", 0},
		{"V_2-12", 0},
		{"MUL-", 0},
		{"search", 0},
		{"0", 0},
		{"", 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got, ok := parseQueryNumber(tc.query)
			if got != tc.want || ok != (tc.want > 0) {
				t.Fatalf("parseQueryNumber(%q) = (%d, %t), want (%d, %t)", tc.query, got, ok, tc.want, tc.want > 0)
			}
		})
	}
}

func TestIssueSearch_OutOfRangeNumbers(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Out of range number search", "number-search-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")

	for _, query := range []string{"35185005423742", "MUL-99999999999"} {
		t.Run(query, func(t *testing.T) {
			titleID := dbfx.Issue(t, "Notes about "+query, testutil.Cols{"workspace_id": workspaceID})
			descriptionID := dbfx.Issue(t, "Description match", testutil.Cols{
				"workspace_id": workspaceID, "description": "Reference: " + query,
			})
			commentID := dbfx.Issue(t, "Comment match", testutil.Cols{"workspace_id": workspaceID})
			dbfx.Comment(t, commentID, "Reference: "+query, testutil.Cols{"workspace_id": workspaceID})
			dbfx.Issue(t, "Closed notes about "+query, testutil.Cols{
				"workspace_id": workspaceID, "status": "cancelled",
			})
			dbfx.Issue(t, "Unrelated issue", testutil.Cols{"workspace_id": workspaceID})

			t.Run("global", func(t *testing.T) {
				req := newRequest(http.MethodGet, "/api/issues/search?q="+url.QueryEscape(query), nil)
				req.Header.Set("X-Workspace-ID", workspaceID)
				var response struct {
					Issues []SearchIssueResponse `json:"issues"`
				}
				testutil.Call(t, testHandler.SearchIssues, req).Want(http.StatusOK).JSON(&response)
				want := map[string]string{titleID: "title", descriptionID: "description", commentID: "comment"}
				if len(response.Issues) != len(want) {
					t.Fatalf("global search: got %+v, want title, description and comment matches", response.Issues)
				}
				for _, issue := range response.Issues {
					if source, ok := want[issue.ID]; !ok || issue.MatchSource != source {
						t.Errorf("global search: unexpected issue %s with match source %q", issue.ID, issue.MatchSource)
					}
					delete(want, issue.ID)
				}
				if len(want) != 0 {
					t.Errorf("global search: missing matches %v", want)
				}
			})

			t.Run("list", func(t *testing.T) {
				req := newRequest(http.MethodGet, "/api/issues?q="+url.QueryEscape(query)+"&status=todo&limit=1", nil)
				req.Header.Set("X-Workspace-ID", workspaceID)
				var response struct {
					Issues []IssueResponse `json:"issues"`
					Total  int64           `json:"total"`
				}
				testutil.Call(t, testHandler.ListIssues, req).Want(http.StatusOK).JSON(&response)
				if response.Total != 1 || len(response.Issues) != 1 || response.Issues[0].ID != titleID {
					t.Fatalf("list search: got %+v, want only title match %s", response, titleID)
				}
			})
		})
	}
}

func TestIssueSearch_DigitPrefix(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Digit prefix search", "digit-search-"+uuid.NewString(), testutil.Cols{"issue_prefix": "V2"})
	dbfx.Member(t, workspaceID, testUserID, "owner")
	targetID := dbfx.Issue(t, "Abandoned plan", testutil.Cols{
		"workspace_id": workspaceID, "number": 12, "status": "cancelled",
	})
	liveID := dbfx.Issue(t, "Notes about V2-12", testutil.Cols{"workspace_id": workspaceID})
	foreignWorkspaceID := dbfx.Workspace(t, "Other search workspace", "other-search-"+uuid.NewString())
	dbfx.Issue(t, "V2-12", testutil.Cols{"workspace_id": foreignWorkspaceID, "number": 12})

	for _, query := range []string{"V2-12", "v2-12", "12"} {
		t.Run(query, func(t *testing.T) {
			// The fallback API must retain the exact cancelled hit ahead of live
			// text matches, even when the result window has only one slot.
			for _, includeClosed := range []bool{true, false} {
				path := fmt.Sprintf("/api/issues/search?q=%s&include_closed=%t&limit=1", url.QueryEscape(query), includeClosed)
				req := newRequest(http.MethodGet, path, nil)
				req.Header.Set("X-Workspace-ID", workspaceID)
				var response struct {
					Issues []SearchIssueResponse `json:"issues"`
				}
				testutil.Call(t, testHandler.SearchIssues, req).Want(http.StatusOK).JSON(&response)
				wantID := liveID
				if includeClosed {
					wantID = targetID
				}
				if len(response.Issues) != 1 || response.Issues[0].ID != wantID {
					t.Fatalf("search include_closed=%t: got %+v, want issue %s", includeClosed, response.Issues, wantID)
				}
			}

			// List search uses the same parser while preserving active filters
			// and the total for the filtered result set.
			for _, status := range []string{"cancelled", "todo"} {
				path := fmt.Sprintf("/api/issues?q=%s&status=%s&limit=1", url.QueryEscape(query), status)
				req := newRequest(http.MethodGet, path, nil)
				req.Header.Set("X-Workspace-ID", workspaceID)
				var response struct {
					Issues []IssueResponse `json:"issues"`
					Total  int64           `json:"total"`
				}
				testutil.Call(t, testHandler.ListIssues, req).Want(http.StatusOK).JSON(&response)
				wantID := liveID
				if status == "cancelled" {
					wantID = targetID
				}
				if response.Total != 1 || len(response.Issues) != 1 || response.Issues[0].ID != wantID {
					t.Fatalf("list status=%s: got %+v, want only issue %s", status, response, wantID)
				}
			}
		})
	}
}
