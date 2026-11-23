package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/collab"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func useCollab(t *testing.T, url string) {
	t.Helper()
	client, err := collab.NewClient(url, "test-collab-secret")
	if err != nil {
		t.Fatal(err)
	}
	previous := testHandler.Collab
	testHandler.Collab = client
	t.Cleanup(func() { testHandler.Collab = previous })
}

func docHistory(t *testing.T, docID string) []DocVersionResponse {
	t.Helper()
	var history struct {
		Versions []DocVersionResponse `json:"versions"`
	}
	testutil.Call(t, testHandler.ListDocVersions, withURLParam(newRequest("GET", "/api/docs/versions", nil), "id", docID)).Want(http.StatusOK).JSON(&history)
	return history.Versions
}

func TestEditDocWithoutCollabService(t *testing.T) {
	doc := createDocForTest(t)
	edit := func(edits ...collab.TextEdit) *http.Request {
		return withURLParam(newRequest("POST", "/api/docs/"+doc.ID+"/edits", map[string]any{"edits": edits, "message": "fix"}), "id", doc.ID)
	}
	var updated DocWriteResponse
	testutil.Call(t, testHandler.EditDoc, edit(collab.TextEdit{OldText: "Original", NewText: "Edited"})).Want(http.StatusOK).JSON(&updated)
	if updated.Content != "# First\n\nEdited" || updated.Revision != 2 {
		t.Fatalf("edit not applied: %+v", updated)
	}
	var failed map[string]any
	testutil.Call(t, testHandler.EditDoc, edit(collab.TextEdit{OldText: "Original", NewText: "x"})).Want(http.StatusConflict).JSON(&failed)
	if failed["code"] != "doc_edit_failed" || failed["content"] != "# First\n\nEdited" {
		t.Fatalf("stale quote must fail with the current text: %v", failed)
	}
	testutil.Call(t, testHandler.EditDoc, edit()).Want(http.StatusBadRequest)

	versions := docHistory(t, doc.ID)
	if len(versions) != 2 || versions[0].Message != "fix" || len(versions[0].Contributors) != 1 || versions[0].Contributors[0].ID != testUserID {
		t.Fatalf("edit must be its own credited version: %+v", versions)
	}
}

func TestStoreCollabDocCreditsEveryoneAndFoldsTheSession(t *testing.T) {
	doc := createDocForTest(t)
	const bob = "00000000-0000-4000-8000-0000000000b0"
	state := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	store := func(body map[string]any) *http.Request {
		body["ystate"] = state
		return withURLParam(newRequest("POST", "/internal/collab/docs/"+doc.ID+"/store", body), "id", doc.ID)
	}
	var stored struct {
		Revision int64 `json:"revision"`
	}
	testutil.Call(t, testHandler.StoreCollabDoc, store(map[string]any{
		"base_revision": 1, "content": "Together",
		"contributors": []DocContributor{{Type: "member", ID: testUserID}, {Type: "member", ID: bob}},
	})).Want(http.StatusOK).JSON(&stored)
	testutil.Call(t, testHandler.StoreCollabDoc, store(map[string]any{
		"base_revision": stored.Revision, "content": "Together again",
		"contributors": []DocContributor{{Type: "member", ID: bob}},
	})).Want(http.StatusOK).JSON(&stored)

	versions := docHistory(t, doc.ID)
	if len(versions) != 2 || len(versions[0].Contributors) != 2 {
		t.Fatalf("a co-editing session must fold into one version crediting both: %+v", versions)
	}

	// An agent's write stands alone.
	agentID := "00000000-0000-4000-8000-0000000000a1"
	testutil.Call(t, testHandler.StoreCollabDoc, store(map[string]any{
		"base_revision": stored.Revision, "content": "Together again, agent",
		"contributors": []DocContributor{{Type: "agent", ID: agentID}}, "message": "polish",
	})).Want(http.StatusOK).JSON(&stored)
	versions = docHistory(t, doc.ID)
	if len(versions) != 3 || versions[0].AuthorType != "agent" || versions[0].Message != "polish" {
		t.Fatalf("agent write must be its own version: %+v", versions)
	}

	var conflict struct {
		Revision int64  `json:"revision"`
		Content  string `json:"content"`
	}
	testutil.Call(t, testHandler.StoreCollabDoc, store(map[string]any{
		"base_revision": 1, "content": "stale", "contributors": []DocContributor{},
	})).Want(http.StatusConflict).JSON(&conflict)
	if conflict.Revision != stored.Revision || conflict.Content != "Together again, agent" {
		t.Fatalf("stale store must report the stored doc: %+v", conflict)
	}

	var loaded collabDocResponse
	testutil.Call(t, testHandler.GetCollabDoc, withURLParam(newRequest("GET", "/internal/collab/docs/"+doc.ID, nil), "id", doc.ID)).Want(http.StatusOK).JSON(&loaded)
	if loaded.YState == nil || *loaded.YState != state || loaded.YStateRevision == nil || *loaded.YStateRevision != stored.Revision {
		t.Fatalf("saved state must come back with its revision: %+v", loaded)
	}

	// A state-only store saves the Yjs state and leaves the doc alone.
	caughtUp := base64.StdEncoding.EncodeToString([]byte{4, 5, 6})
	var same struct {
		Revision int64 `json:"revision"`
	}
	testutil.Call(t, testHandler.StoreCollabDoc, withURLParam(newRequest("POST", "/internal/collab/docs/"+doc.ID+"/store", map[string]any{
		"base_revision": stored.Revision, "content": "ignored", "ystate": caughtUp,
		"contributors": []DocContributor{}, "state_only": true,
	}), "id", doc.ID)).Want(http.StatusOK).JSON(&same)
	testutil.Call(t, testHandler.GetCollabDoc, withURLParam(newRequest("GET", "/internal/collab/docs/"+doc.ID, nil), "id", doc.ID)).Want(http.StatusOK).JSON(&loaded)
	if same.Revision != stored.Revision || loaded.Content != "Together again, agent" || *loaded.YState != caughtUp || len(docHistory(t, doc.ID)) != 3 {
		t.Fatalf("state-only store must not touch the doc: revision %d, %+v", same.Revision, loaded)
	}
}

func TestUpdateDocGoesThroughCollabWithItsMergeBase(t *testing.T) {
	doc := createDocForTest(t)
	// Revision 2 is someone else's edit; the writer still holds revision 1.
	testutil.Call(t, testHandler.UpdateDoc, withURLParam(newRequest("PATCH", "/api/docs/"+doc.ID, map[string]any{"base_revision": 1, "content": "# First\n\nTheirs"}), "id", doc.ID)).Want(http.StatusOK)

	var got collab.WriteRequest
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/documents/doc:"+doc.ID+"/flush" {
			_, _ = w.Write([]byte(`{"loaded":false}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"merge_conflict","status":"conflict","revision":2,"content":"# First\n\nTheirs","conflicts":[{"current":"Theirs","incoming":"Mine"}]}`))
	}))
	defer service.Close()
	useCollab(t, service.URL)

	var conflict map[string]any
	testutil.Call(t, testHandler.UpdateDoc, withURLParam(newRequest("PATCH", "/api/docs/"+doc.ID, map[string]any{"base_revision": 1, "content": "# First\n\nMine"}), "id", doc.ID)).Want(http.StatusConflict).JSON(&conflict)
	if got.BaseContent == nil || *got.BaseContent != doc.Content || got.BaseRevision != 1 || got.Actor.ID != testUserID {
		t.Fatalf("write must carry the content at its base revision: %+v", got)
	}
	if conflict["code"] != "doc_merge_conflict" {
		t.Fatalf("unexpected response: %v", conflict)
	}

	// A base older than the kept revisions cannot be merged.
	testutil.Call(t, testHandler.UpdateDoc, withURLParam(newRequest("PATCH", "/api/docs/"+doc.ID, map[string]any{"base_revision": 9, "content": "x"}), "id", doc.ID)).Want(http.StatusConflict)
}

func TestCollabTicketCarriesTheStateSavedWithTheCurrentRevision(t *testing.T) {
	doc := createDocForTest(t)
	service := httptest.NewServer(http.NotFoundHandler())
	url := service.URL
	service.Close()
	useCollab(t, url)

	ticket := func(body any) DocCollabTicketResponse {
		t.Helper()
		var resp DocCollabTicketResponse
		testutil.Call(t, testHandler.CreateDocCollabTicket, withURLParam(newRequest("POST", "/api/docs/"+doc.ID+"/collab-ticket", body), "id", doc.ID)).Want(http.StatusOK).JSON(&resp)
		if !resp.Enabled || resp.Token == "" || resp.Document != "doc:"+doc.ID {
			t.Fatalf("ticket must be issued: %+v", resp)
		}
		return resp
	}
	if got := ticket(map[string]any{"state": true}); got.State != "" {
		t.Fatalf("a doc never opened live has no state: %+v", got)
	}

	state := base64.StdEncoding.EncodeToString([]byte{7, 8, 9})
	testutil.Call(t, testHandler.StoreCollabDoc, withURLParam(newRequest("POST", "/internal/collab/docs/"+doc.ID+"/store", map[string]any{
		"base_revision": 1, "content": doc.Content, "ystate": state, "contributors": []DocContributor{}, "state_only": true,
	}), "id", doc.ID)).Want(http.StatusOK)
	if got := ticket(map[string]any{"state": true}); got.State != state {
		t.Fatalf("state saved with the current revision must come back: %+v", got)
	}
	if got := ticket(nil); got.State != "" {
		t.Fatalf("state is sent only when asked for: %+v", got)
	}

	// Written without the live session: the saved state is behind the doc.
	testutil.Call(t, testHandler.UpdateDoc, withURLParam(newRequest("PATCH", "/api/docs/"+doc.ID, map[string]any{"base_revision": 1, "content": "Direct"}), "id", doc.ID)).Want(http.StatusOK)
	if got := ticket(map[string]any{"state": true}); got.State != "" {
		t.Fatalf("a state behind the doc must not be sent: %+v", got)
	}
}

func TestDocWritesFallBackWhenCollabIsDown(t *testing.T) {
	doc := createDocForTest(t)
	service := httptest.NewServer(http.NotFoundHandler())
	url := service.URL
	service.Close()
	useCollab(t, url)

	var updated DocWriteResponse
	testutil.Call(t, testHandler.UpdateDoc, withURLParam(newRequest("PATCH", "/api/docs/"+doc.ID, map[string]any{"base_revision": 1, "content": "Direct"}), "id", doc.ID)).Want(http.StatusOK).JSON(&updated)
	if updated.Content != "Direct" {
		t.Fatalf("write must land directly: %+v", updated)
	}
	testutil.Call(t, testHandler.EditDoc, withURLParam(newRequest("POST", "/api/docs/"+doc.ID+"/edits", map[string]any{"edits": []collab.TextEdit{{OldText: "Direct", NewText: "Edited"}}}), "id", doc.ID)).Want(http.StatusOK).JSON(&updated)
	if updated.Content != "Edited" {
		t.Fatalf("edit must land directly: %+v", updated)
	}
	testutil.Call(t, testHandler.GetDoc, withURLParam(newRequest("GET", "/api/docs/"+doc.ID, nil), "id", doc.ID)).Want(http.StatusOK)
}
