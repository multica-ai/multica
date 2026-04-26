package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// linkBody returns the request-body shape CreateIssueLink expects.
func linkBody(targetID, linkType string) map[string]string {
	return map[string]string{
		"target_issue_id": targetID,
		"link_type":       linkType,
	}
}

// TestListIssuesIncludesLinks verifies L-PR#2: ListIssues bulk-enriches
// every issue with its links via the ListLinksForIssues query.
func TestListIssuesIncludesLinks(t *testing.T) {
	if testPool == nil {
		t.Skip("database not reachable")
	}

	wsA, userA := seedLinkWorkspace(t, "ENA")
	srcA := seedLinkIssue(t, wsA, userA, 1, "Source A", "todo")
	tgtA := seedLinkIssue(t, wsA, userA, 2, "Target A", "todo")
	plain := seedLinkIssue(t, wsA, userA, 3, "Plain (no links)", "todo")
	_ = plain

	// Create a blocks link via the handler so the mirror row + activity all wire up.
	w := httptest.NewRecorder()
	req := linkReq("POST",
		fmt.Sprintf("/api/issues/%s/links", srcA),
		userA, wsA,
		linkBody(tgtA, "blocks"),
		"id", srcA,
	)
	testHandler.CreateIssueLink(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create link: %d %s", w.Code, w.Body.String())
	}

	// Now list issues for the workspace and verify both source and target
	// carry the right Links.
	w = httptest.NewRecorder()
	req = linkReq("GET",
		fmt.Sprintf("/api/issues?workspace_id=%s", wsA),
		userA, wsA, nil,
	)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}

	var listResp struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	byID := make(map[string]IssueResponse, len(listResp.Issues))
	for _, is := range listResp.Issues {
		byID[is.ID] = is
	}

	src, ok := byID[srcA]
	if !ok {
		t.Fatal("source issue not in list response")
	}
	tgt, ok := byID[tgtA]
	if !ok {
		t.Fatal("target issue not in list response")
	}
	plainResp, ok := byID[plain]
	if !ok {
		t.Fatal("plain issue not in list response")
	}

	if len(src.Links) != 1 {
		t.Fatalf("source: expected 1 link, got %d (%+v)", len(src.Links), src.Links)
	}
	if src.Links[0].Direction != "outgoing" || src.Links[0].LinkType != "blocks" {
		t.Fatalf("source link wrong shape: %+v", src.Links[0])
	}
	if src.Links[0].TargetIssueID != tgtA {
		t.Fatalf("source link target mismatch: got %q want %q", src.Links[0].TargetIssueID, tgtA)
	}

	if len(tgt.Links) != 1 {
		t.Fatalf("target: expected 1 link, got %d", len(tgt.Links))
	}
	if tgt.Links[0].Direction != "incoming" {
		t.Fatalf("target should see incoming, got %q", tgt.Links[0].Direction)
	}

	if len(plainResp.Links) != 0 {
		t.Fatalf("plain issue should have no links, got %+v", plainResp.Links)
	}
}

// TestGetIssueIncludesLinks verifies that the single-issue GET enriches
// with links.
func TestGetIssueIncludesLinks(t *testing.T) {
	if testPool == nil {
		t.Skip("database not reachable")
	}

	ws, user := seedLinkWorkspace(t, "ENB")
	src := seedLinkIssue(t, ws, user, 1, "Src", "todo")
	tgt := seedLinkIssue(t, ws, user, 2, "Tgt", "todo")

	// Use the handler so we get the same shape end users will see.
	w := httptest.NewRecorder()
	req := linkReq("POST",
		fmt.Sprintf("/api/issues/%s/links", src),
		user, ws, linkBody(tgt, "depends_on"), "id", src,
	)
	testHandler.CreateIssueLink(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = linkReq("GET",
		fmt.Sprintf("/api/issues/%s?workspace_id=%s", src, ws),
		user, ws, nil, "id", src,
	)
	testHandler.GetIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	var resp IssueResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(resp.Links))
	}
	if resp.Links[0].LinkType != "depends_on" {
		t.Fatalf("expected depends_on, got %q", resp.Links[0].LinkType)
	}
	if resp.Links[0].TargetIssueID != tgt {
		t.Fatalf("target id mismatch")
	}
}

// TestListIssuesReturnsEmptyLinksArray verifies that issues with no links
// emit `"links":[]` (not null) so the frontend can iterate without nil checks.
func TestListIssuesReturnsEmptyLinksArray(t *testing.T) {
	if testPool == nil {
		t.Skip("database not reachable")
	}

	ws, user := seedLinkWorkspace(t, "ENC")
	_ = seedLinkIssue(t, ws, user, 1, "Lonely", "todo")

	w := httptest.NewRecorder()
	req := linkReq("GET",
		fmt.Sprintf("/api/issues?workspace_id=%s", ws),
		user, ws, nil,
	)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), `"links":[]`) {
		t.Fatalf(`expected response to include "links":[], got: %s`, w.Body.String())
	}
}
