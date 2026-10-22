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
