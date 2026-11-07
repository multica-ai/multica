package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueSearch_ScopeTitleDesc(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires DB")
	}

	token := fmt.Sprintf("mulscopesearch%d", time.Now().UnixNano())
	titleIssueID := dbfx.Issue(t, token+" title match")
	descIssueID := dbfx.Issue(t, "issue with description match", testutil.Cols{
		"description": "body carrying " + token,
	})
	commentIssueID := dbfx.Issue(t, "issue with comment-only match")
	dbfx.Comment(t, commentIssueID, "comment carrying "+token)

	// scope=title_desc skips comment scanning and returns only title/description matches.
	titleDescParams := url.Values{
		"q":     {token},
		"scope": {searchScopeTitleDesc},
	}
	reqTitleDesc := newRequest("GET", "/api/issues/search?"+titleDescParams.Encode(), nil)
	wTitleDesc := testutil.Call(t, testHandler.SearchIssues, reqTitleDesc).Want(http.StatusOK)

	var titleDescResp struct {
		Issues []SearchIssueResponse `json:"issues"`
	}
	wTitleDesc.JSON(&titleDescResp)

	gotTitleDesc := make(map[string]bool, len(titleDescResp.Issues))
	for _, item := range titleDescResp.Issues {
		gotTitleDesc[item.ID] = true
	}
	if !gotTitleDesc[titleIssueID] || !gotTitleDesc[descIssueID] {
		t.Errorf("expected title and description hits in scope=title_desc results, got %+v", titleDescResp.Issues)
	}
	if gotTitleDesc[commentIssueID] {
		t.Errorf("expected comment-only hit %s to be excluded when scope=title_desc, got %+v", commentIssueID, titleDescResp.Issues)
	}

	// Default scope includes comment matches.
	allParams := url.Values{
		"q": {token},
	}
	reqAll := newRequest("GET", "/api/issues/search?"+allParams.Encode(), nil)
	wAll := testutil.Call(t, testHandler.SearchIssues, reqAll).Want(http.StatusOK)

	var allResp struct {
		Issues []SearchIssueResponse `json:"issues"`
	}
	wAll.JSON(&allResp)

	gotAll := make(map[string]bool, len(allResp.Issues))
	for _, item := range allResp.Issues {
		gotAll[item.ID] = true
	}
	if !gotAll[commentIssueID] {
		t.Errorf("expected comment-only hit %s in default scope results, got %+v", commentIssueID, allResp.Issues)
	}

	// Multi-term queries under scope=title_desc must match terms split across
	// title and description, exclude issues missing a term, and ignore comments
	// that carry the missing term.
	termA := token + "alpha"
	termB := token + "beta"
	crossFieldID := dbfx.Issue(t, termA+" in title only", testutil.Cols{
		"description": "description carries " + termB,
	})
	missingSecondID := dbfx.Issue(t, termA+" without the other term", testutil.Cols{
		"description": "unrelated description body",
	})
	commentCarriesSecondID := dbfx.Issue(t, termA+" in title", testutil.Cols{
		"description": "unrelated description body",
	})
	dbfx.Comment(t, commentCarriesSecondID, "comment carries "+termB)

	multiTitleDescParams := url.Values{
		"q":     {termA + " " + termB},
		"scope": {searchScopeTitleDesc},
	}
	reqMultiTitleDesc := newRequest("GET", "/api/issues/search?"+multiTitleDescParams.Encode(), nil)
	wMultiTitleDesc := testutil.Call(t, testHandler.SearchIssues, reqMultiTitleDesc).Want(http.StatusOK)

	var multiTitleDescResp struct {
		Issues []SearchIssueResponse `json:"issues"`
	}
	wMultiTitleDesc.JSON(&multiTitleDescResp)

	gotMultiTitleDesc := make(map[string]bool, len(multiTitleDescResp.Issues))
	for _, item := range multiTitleDescResp.Issues {
		gotMultiTitleDesc[item.ID] = true
	}
	if !gotMultiTitleDesc[crossFieldID] {
		t.Errorf("expected cross-title/description multi-term hit %s under scope=title_desc, got %+v", crossFieldID, multiTitleDescResp.Issues)
	}
	if gotMultiTitleDesc[missingSecondID] || gotMultiTitleDesc[commentCarriesSecondID] {
		t.Errorf("expected missing-term (%s) and comment-complement (%s) issues to be excluded under scope=title_desc, got %+v",
			missingSecondID, commentCarriesSecondID, multiTitleDescResp.Issues)
	}

	multiAllParams := url.Values{
		"q": {termA + " " + termB},
	}
	reqMultiAll := newRequest("GET", "/api/issues/search?"+multiAllParams.Encode(), nil)
	wMultiAll := testutil.Call(t, testHandler.SearchIssues, reqMultiAll).Want(http.StatusOK)

	var multiAllResp struct {
		Issues []SearchIssueResponse `json:"issues"`
	}
	wMultiAll.JSON(&multiAllResp)

	gotMultiAll := make(map[string]bool, len(multiAllResp.Issues))
	for _, item := range multiAllResp.Issues {
		gotMultiAll[item.ID] = true
	}
	if !gotMultiAll[crossFieldID] || !gotMultiAll[commentCarriesSecondID] {
		t.Errorf("expected both %s and %s in default scope multi-term results, got %+v",
			crossFieldID, commentCarriesSecondID, multiAllResp.Issues)
	}
	if gotMultiAll[missingSecondID] {
		t.Errorf("expected missing-term issue %s to be excluded in default scope multi-term results, got %+v",
			missingSecondID, multiAllResp.Issues)
	}

	// A number-only hit under scope=title_desc falls back to match_source="description"
	// (since comments are skipped) without attaching an unrelated description snippet.
	numWS := dbfx.Workspace(t, "Number hit title_desc", fmt.Sprintf("numhit-%d", time.Now().UnixNano()), testutil.Cols{"issue_prefix": "NH"})
	dbfx.Member(t, numWS, testUserID, "owner")
	numberedID := dbfx.Issue(t, "unrelated numbered issue", testutil.Cols{
		"workspace_id": numWS,
		"number":       12,
		"description":  "a long description that never mentions the identifier",
	})
	for _, scope := range []string{"", "&scope=" + searchScopeTitleDesc} {
		reqNum := newRequest("GET", "/api/issues/search?q=NH-12&include_closed=true&limit=50"+scope, nil)
		reqNum.Header.Set("X-Workspace-ID", numWS)
		var numResp struct {
			Issues []SearchIssueResponse `json:"issues"`
		}
		testutil.Call(t, testHandler.SearchIssues, reqNum).Want(http.StatusOK).JSON(&numResp)
		if len(numResp.Issues) != 1 || numResp.Issues[0].ID != numberedID {
			t.Fatalf("scope %q search = %d issues, want only %s", scope, len(numResp.Issues), numberedID)
		}
		wantSource := "comment"
		if scope != "" {
			wantSource = "description"
		}
		if numResp.Issues[0].MatchSource != wantSource {
			t.Fatalf("scope %q match_source = %q, want %q", scope, numResp.Issues[0].MatchSource, wantSource)
		}
		if numResp.Issues[0].MatchedDescriptionSnippet != nil {
			t.Fatalf("scope %q number hit attached a description snippet: %q", scope, *numResp.Issues[0].MatchedDescriptionSnippet)
		}
	}
}
