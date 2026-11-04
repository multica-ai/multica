package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueSearch_ProjectFilter(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires DB")
	}

	token := fmt.Sprintf("mulprojsearch%d", time.Now().UnixNano())
	targetProjectID := dbfx.Project(t, "Target search project "+token)
	otherProjectID := dbfx.Project(t, "Other search project "+token)

	targetTitleIssueID := dbfx.Issue(t, token+" target title hit", testutil.Cols{
		"project_id": targetProjectID,
	})
	targetCommentIssueID := dbfx.Issue(t, "issue with comment hit in target project", testutil.Cols{
		"project_id": targetProjectID,
	})
	dbfx.Comment(t, targetCommentIssueID, token+" target comment hit")

	otherTitleIssueID := dbfx.Issue(t, token+" other project title hit", testutil.Cols{
		"project_id": otherProjectID,
	})
	otherCommentIssueID := dbfx.Issue(t, "issue with comment hit in other project", testutil.Cols{
		"project_id": otherProjectID,
	})
	dbfx.Comment(t, otherCommentIssueID, token+" other project comment hit")

	noProjectIssueID := dbfx.Issue(t, token+" unassigned project hit")

	params := url.Values{
		"q":          {token},
		"project_id": {targetProjectID},
	}
	req := newRequest("GET", "/api/issues/search?"+params.Encode(), nil)
	w := testutil.Call(t, testHandler.SearchIssues, req).Want(http.StatusOK)

	var resp struct {
		Issues []SearchIssueResponse `json:"issues"`
	}
	w.JSON(&resp)

	gotIDs := make(map[string]bool, len(resp.Issues))
	for _, item := range resp.Issues {
		gotIDs[item.ID] = true
	}
	if !gotIDs[targetTitleIssueID] {
		t.Errorf("expected target project title-matched issue %s in results, got %+v", targetTitleIssueID, resp.Issues)
	}
	if !gotIDs[targetCommentIssueID] {
		t.Errorf("expected target project comment-matched issue %s in results, got %+v", targetCommentIssueID, resp.Issues)
	}
	if gotIDs[otherTitleIssueID] || gotIDs[otherCommentIssueID] || gotIDs[noProjectIssueID] {
		t.Errorf("expected non-target project issues to be excluded, got %+v", resp.Issues)
	}
}

func TestIssueSearch_ProjectFilterRejectsMalformedUUID(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest("GET", "/api/issues/search?q=test&project_id=not-a-uuid", nil)
	req.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000002")
	req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000001")
	w := testutil.Call(t, h.SearchIssues, req).Want(http.StatusBadRequest)
	var resp struct {
		Error string `json:"error"`
	}
	w.JSON(&resp)
	if resp.Error != "invalid project_id" {
		t.Fatalf("error = %q, want %q", resp.Error, "invalid project_id")
	}
}
