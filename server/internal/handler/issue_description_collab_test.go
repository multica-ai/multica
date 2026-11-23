package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/collab"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func getIssueForTest(t *testing.T, id string) IssueResponse {
	t.Helper()
	var issue IssueResponse
	testutil.Call(t, testHandler.GetIssue, withURLParam(newRequest("GET", "/api/issues/"+id, nil), "id", id)).Want(http.StatusOK).JSON(&issue)
	return issue
}

func putIssueForTest(t *testing.T, id string, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest("PUT", "/api/issues/"+id, body), "id", id))
}

func storeIssueDescription(t *testing.T, id string, body map[string]any) *testutil.Response {
	t.Helper()
	if _, ok := body["ystate"]; !ok {
		body["ystate"] = base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	}
	return testutil.Call(t, testHandler.StoreCollabIssue, withURLParam(newRequest("POST", "/internal/collab/issues/"+id+"/store", body), "id", id))
}

// captureIssueUpdates records the issue:updated payloads published for one issue.
func captureIssueUpdates(t *testing.T, issueID string) func() []map[string]any {
	t.Helper()
	var mu sync.Mutex
	var payloads []map[string]any
	testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if resp, ok := payload["issue"].(IssueResponse); ok && resp.ID == issueID {
			mu.Lock()
			payloads = append(payloads, payload)
			mu.Unlock()
		}
	})
	return func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), payloads...)
	}
}

func TestStoreCollabIssueUsesTheDescriptionsOwnRevision(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	id := dbfx.Issue(t, "live description", testutil.Cols{"description": "One"})
	updates := captureIssueUpdates(t, id)
	start := getIssueForTest(t, id)
	base := *start.DescriptionRevision

	var stored struct {
		Revision int64    `json:"revision"`
		Mentions []string `json:"mentions"`
	}
	mention := "[@Bob](mention://member/" + testUserID + ")"
	storeIssueDescription(t, id, map[string]any{
		"base_revision": base, "content": "One " + mention, "live": true,
		"contributors":  []DocContributor{{Type: "member", ID: testUserID}},
		"seen_mentions": []string{"member:someone-else"},
	}).Want(http.StatusOK).JSON(&stored)
	if stored.Revision != base+1 || len(stored.Mentions) != 1 || stored.Mentions[0] != "member:"+testUserID {
		t.Fatalf("store answered %+v, want revision %d and the stored mention", stored, base+1)
	}

	// Other fields moving does not touch the description's revision, so the
	// live session keeps saving from it.
	putIssueForTest(t, id, map[string]any{"status": "in_progress", "priority": "high"}).Want(http.StatusOK)
	storeIssueDescription(t, id, map[string]any{
		"base_revision": stored.Revision, "content": "One two " + mention, "live": true,
		"contributors": []DocContributor{{Type: "member", ID: testUserID}},
	}).Want(http.StatusOK).JSON(&stored)
	after := getIssueForTest(t, id)
	if after.Description == nil || *after.Description != "One two "+mention || after.Status != "in_progress" || *after.DescriptionRevision != base+2 {
		t.Fatalf("issue after live saves = %+v", after)
	}

	var conflict struct {
		Revision int64  `json:"revision"`
		Content  string `json:"content"`
	}
	storeIssueDescription(t, id, map[string]any{
		"base_revision": base, "content": "stale", "contributors": []DocContributor{},
	}).Want(http.StatusConflict).JSON(&conflict)
	if conflict.Revision != base+2 || conflict.Content != "One two "+mention {
		t.Fatalf("stale store must report the stored description: %+v", conflict)
	}

	var live []map[string]any
	for _, p := range updates() {
		if _, ok := p["description_live_editors"]; ok {
			live = append(live, p)
		}
	}
	if len(live) != 2 {
		t.Fatalf("want two live issue:updated payloads, got %d", len(live))
	}
	if seen, _ := live[0]["description_seen_mentions"].([]string); len(seen) != 1 || seen[0] != "member:someone-else" {
		t.Fatalf("seen mentions must reach the listeners: %v", live[0]["description_seen_mentions"])
	}

	var loaded collabDocResponse
	testutil.Call(t, testHandler.GetCollabIssue, withURLParam(newRequest("GET", "/internal/collab/issues/"+id, nil), "id", id)).Want(http.StatusOK).JSON(&loaded)
	if loaded.Content != "One two "+mention || loaded.Revision != base+2 || loaded.YStateRevision == nil || *loaded.YStateRevision != base+2 {
		t.Fatalf("internal load = %+v", loaded)
	}
}

func TestUpdateIssueDescriptionBaseRevisionWithoutLiveEditing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	id := dbfx.Issue(t, "base revision", testutil.Cols{"description": "Alpha"})
	start := getIssueForTest(t, id)
	base := *start.DescriptionRevision

	var updated IssueResponse
	putIssueForTest(t, id, map[string]any{"description": "Beta", "description_base_revision": base}).Want(http.StatusOK).JSON(&updated)
	if *updated.DescriptionRevision != base+1 {
		t.Fatalf("description_revision = %d, want %d", *updated.DescriptionRevision, base+1)
	}
	var conflict map[string]any
	putIssueForTest(t, id, map[string]any{"description": "Gamma", "description_base_revision": base}).Want(http.StatusConflict).JSON(&conflict)
	if conflict["code"] != "revision_conflict" || conflict["resource_type"] != "issue_description" {
		t.Fatalf("stale base without live editing must be refused: %v", conflict)
	}
	// Clients that never send a base keep today's whole replacement.
	putIssueForTest(t, id, map[string]any{"description": "Delta"}).Want(http.StatusOK)
}

func TestUpdateIssueDescriptionGoesThroughTheLiveSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withFeatureFlag(t, testHandler, featureflags.IssueDescriptionLive, true)
	id := dbfx.Issue(t, "via collab", testutil.Cols{"description": "Base"})
	start := getIssueForTest(t, id)

	var mu sync.Mutex
	var paths []string
	var got collab.WriteRequest
	conflict := false
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/internal/documents/issue:"+id+"/write" {
			_, _ = w.Write([]byte(`{"loaded":false}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		if conflict {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"merge_conflict","status":"conflict","revision":3,"content":"Theirs","conflicts":[{"current":"Theirs","incoming":"Mine"}]}`))
			return
		}
		// The service stores through StoreCollabIssue; stand in for it.
		stored := httptest.NewRecorder()
		testHandler.StoreCollabIssue(stored, withURLParam(newRequest("POST", "/internal/collab/issues/"+id+"/store", map[string]any{
			"base_revision": *start.DescriptionRevision, "content": *got.Content,
			"ystate":       base64.StdEncoding.EncodeToString([]byte{1}),
			"contributors": []DocContributor{{Type: got.Actor.Type, ID: got.Actor.ID}},
		}), "id", id))
		if stored.Code != http.StatusOK {
			t.Errorf("store: %d %s", stored.Code, stored.Body.String())
		}
		_, _ = w.Write([]byte(`{"status":"ok","revision":1,"content":"","title":"","merged":false}`))
	}))
	defer service.Close()
	useCollab(t, service.URL)

	var updated IssueResponse
	putIssueForTest(t, id, map[string]any{
		"description": "Mine", "description_base_revision": *start.DescriptionRevision, "status": "in_progress",
	}).Want(http.StatusOK).JSON(&updated)
	if got.BaseContent == nil || *got.BaseContent != "Base" || got.Actor.ID != testUserID {
		t.Fatalf("write must carry the description at its base revision: %+v", got)
	}
	if updated.Description == nil || *updated.Description != "Mine" || updated.Status != "in_progress" {
		t.Fatalf("description and status must both land: %+v", updated)
	}

	mu.Lock()
	conflict = true
	mu.Unlock()
	var refused map[string]any
	putIssueForTest(t, id, map[string]any{"description": "Other", "description_base": "Base"}).Want(http.StatusConflict).JSON(&refused)
	if refused["code"] != "issue_description_merge_conflict" || refused["content"] != "Theirs" {
		t.Fatalf("overlapping edits must be refused with both sides: %v", refused)
	}

	// A write pinned to the whole issue's revision stays a direct write.
	current := getIssueForTest(t, id)
	mu.Lock()
	writes := len(paths)
	mu.Unlock()
	putIssueForTest(t, id, map[string]any{"description": "Pinned", "expected_revision": current.Revision}).Want(http.StatusOK)
	mu.Lock()
	defer mu.Unlock()
	for _, p := range paths[writes:] {
		if p == "/internal/documents/issue:"+id+"/write" {
			t.Fatalf("expected_revision write must not go through the live session: %v", paths[writes:])
		}
	}
}

func TestUpdateIssueDescriptionMergesFromTheFirstRevision(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withFeatureFlag(t, testHandler, featureflags.IssueDescriptionLive, true)
	id := dbfx.Issue(t, "first revision", testutil.Cols{"description": "## A\n\none\n\n## B\n\ntwo"})
	start := getIssueForTest(t, id)

	var got collab.WriteRequest
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"status":"ok","revision":2,"content":"","title":"","merged":true}`))
	}))
	defer service.Close()
	useCollab(t, service.URL)

	// Someone types in one section before the agent, which read the issue as
	// created, writes another.
	storeIssueDescription(t, id, map[string]any{
		"base_revision": *start.DescriptionRevision, "content": "## A\n\none, edited\n\n## B\n\ntwo",
		"contributors": []DocContributor{{Type: "member", ID: testUserID}},
	}).Want(http.StatusOK)
	putIssueForTest(t, id, map[string]any{
		"description": "## A\n\none\n\n## B\n\ntwo, by the agent", "description_base_revision": *start.DescriptionRevision,
	}).Want(http.StatusOK)
	if got.BaseContent == nil || *got.BaseContent != "## A\n\none\n\n## B\n\ntwo" {
		t.Fatalf("a write from the first revision must merge against it: %+v", got)
	}
}

func TestIssueCollabTicketFollowsTheFlag(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	id := dbfx.Issue(t, "ticket", testutil.Cols{"description": "Hello"})
	// A service that is down: tickets are still signed, writes fall back.
	service := httptest.NewServer(http.NotFoundHandler())
	service.Close()
	useCollab(t, service.URL)
	ticket := func() DocCollabTicketResponse {
		var resp DocCollabTicketResponse
		testutil.Call(t, testHandler.CreateIssueCollabTicket, withURLParam(newRequest("POST", "/api/issues/"+id+"/collab-ticket", map[string]any{"state": true}), "id", id)).Want(http.StatusOK).JSON(&resp)
		return resp
	}
	if ticket().Enabled {
		t.Fatal("live descriptions must stay off until the flag is on")
	}
	withFeatureFlag(t, testHandler, featureflags.IssueDescriptionLive, true)
	resp := ticket()
	if !resp.Enabled || resp.Document != "issue:"+id || resp.Token == "" {
		t.Fatalf("ticket = %+v", resp)
	}
	// Edits fall back to a direct write while the service is unreachable.
	var edited IssueResponse
	testutil.Call(t, testHandler.EditIssueDescription, withURLParam(newRequest("POST", "/api/issues/"+id+"/description/edits", map[string]any{
		"edits": []collab.TextEdit{{OldText: "Hello", NewText: "Hello there"}},
	}), "id", id)).Want(http.StatusOK).JSON(&edited)
	if edited.Description == nil || *edited.Description != "Hello there" {
		t.Fatalf("edit = %+v", edited)
	}
}

func TestEditIssueDescriptionWithoutLiveEditing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	id := dbfx.Issue(t, "edits", testutil.Cols{"description": "Status: draft\n\nNotes"})
	edit := func(edits ...collab.TextEdit) *http.Request {
		return withURLParam(newRequest("POST", "/api/issues/"+id+"/description/edits", map[string]any{"edits": edits}), "id", id)
	}
	var updated IssueResponse
	testutil.Call(t, testHandler.EditIssueDescription, edit(collab.TextEdit{OldText: "draft", NewText: "ready"})).Want(http.StatusOK).JSON(&updated)
	if updated.Description == nil || *updated.Description != "Status: ready\n\nNotes" {
		t.Fatalf("edit not applied: %+v", updated)
	}
	var failed map[string]any
	testutil.Call(t, testHandler.EditIssueDescription, edit(collab.TextEdit{OldText: "draft", NewText: "x"})).Want(http.StatusConflict).JSON(&failed)
	if failed["code"] != "issue_description_edit_failed" || failed["content"] != "Status: ready\n\nNotes" {
		t.Fatalf("stale quote must fail with the current text: %v", failed)
	}
	testutil.Call(t, testHandler.EditIssueDescription, edit()).Want(http.StatusBadRequest)
}
