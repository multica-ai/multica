package handler

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// knowledgeFixture enables knowledge for the shared test workspace and
// provides an agent identity and a plain member.
type knowledgeFixture struct {
	fake    *knowledge.Fake
	agentID string
	taskID  string
	member  string
}

func newKnowledgeFixture(t *testing.T) knowledgeFixture {
	t.Helper()
	fake := useKnowledge(t)
	if code, _, body := callKnowledge(t, testUserID, http.MethodPost); code != http.StatusOK {
		t.Fatalf("enable = %d %s", code, body)
	}
	agentID := createHandlerTestAgent(t, "Knowledge Writer "+t.Name(), nil)
	return knowledgeFixture{
		fake: fake, agentID: agentID, taskID: createHandlerTestTaskForAgent(t, agentID),
		member: newWorkspaceMemberUser(t, "member"),
	}
}

func (fx knowledgeFixture) asAgent(req *http.Request) *http.Request {
	return asAgent(req, fx.agentID, fx.taskID)
}

func docRequest(body map[string]any) *http.Request {
	return newRequest("POST", "/api/knowledge/documents", body)
}

func docActionRequest(method, id, action string) *http.Request {
	path := "/api/knowledge/documents/" + id
	if action != "" {
		path += "/" + action
	}
	return withURLParam(newRequest(method, path, nil), "id", id)
}

func searchRequest(q string, extra ...string) *http.Request {
	v := url.Values{"q": {q}}
	for i := 0; i+1 < len(extra); i += 2 {
		v.Set(extra[i], extra[i+1])
	}
	return newRequest("GET", "/api/knowledge/search?"+v.Encode(), nil)
}

func TestKnowledgeDocumentFlow(t *testing.T) {
	fx := newKnowledgeFixture(t)

	// An agent drafts, even when it asks to publish.
	var draft service.KnowledgeDocumentView
	testutil.Call(t, testHandler.CreateKnowledgeDocument, fx.asAgent(docRequest(map[string]any{
		"kb": "market", "title": "Acme profile", "content": "Acme sells anvils.", "publish": true,
		"source_type": "issue", "source_id": "0b1b8d4e-6f6f-4f0e-9c0e-3a8d1f2e4a55", "idempotency_key": "issue:7:acme",
	}))).Want(http.StatusCreated).JSON(&draft)
	if draft.State != "draft" || draft.CreatedByType != "agent" || draft.CreatedByID == nil || *draft.CreatedByID != fx.agentID ||
		draft.SourceType != "issue" || draft.SourceID == nil || draft.KnowledgeBase != "market" {
		t.Fatalf("draft = %+v", draft)
	}

	// The same key again is a 200 for the same document.
	var again service.KnowledgeDocumentView
	testutil.Call(t, testHandler.CreateKnowledgeDocument, fx.asAgent(docRequest(map[string]any{
		"kb": "market", "title": "Acme profile", "content": "Acme sells anvils.", "idempotency_key": "issue:7:acme",
	}))).Want(http.StatusOK).JSON(&again)
	if again.ID != draft.ID || fx.fake.Calls(knowledge.OpCreateManual) != 1 {
		t.Fatalf("retry = %+v, %d creates", again, fx.fake.Calls(knowledge.OpCreateManual))
	}

	// Members list drafts and see nothing in search yet.
	var drafts service.KnowledgeDocumentList
	testutil.Call(t, testHandler.ListKnowledgeDocuments, asUser(newRequest("GET", "/api/knowledge/documents?kb=market&state=draft", nil), fx.member)).
		Want(http.StatusOK).JSON(&drafts)
	if drafts.State != "ready" || len(drafts.Documents) != 1 || drafts.Documents[0].ID != draft.ID {
		t.Fatalf("drafts = %+v", drafts)
	}
	var found service.KnowledgeSearchResult
	testutil.Call(t, testHandler.SearchKnowledge, asUser(searchRequest("anvils"), fx.member)).Want(http.StatusOK).JSON(&found)
	if len(found.Hits) != 0 {
		t.Fatalf("a draft was searchable: %+v", found)
	}

	// Publishing is for human owners and admins.
	testutil.Call(t, testHandler.PublishKnowledgeDocument, fx.asAgent(docActionRequest("POST", draft.ID, "publish"))).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.PublishKnowledgeDocument, asUser(docActionRequest("POST", draft.ID, "publish"), fx.member)).Want(http.StatusForbidden)
	var published service.KnowledgeDocumentView
	testutil.Call(t, testHandler.PublishKnowledgeDocument, docActionRequest("POST", draft.ID, "publish")).Want(http.StatusOK).JSON(&published)
	if published.State != "published" {
		t.Fatalf("published = %+v", published)
	}
	testutil.Call(t, testHandler.SearchKnowledge, asUser(searchRequest("anvils", "kb", "market"), fx.member)).Want(http.StatusOK).JSON(&found)
	if len(found.Hits) != 1 || found.Hits[0].Title != "Acme profile" || found.Hits[0].KnowledgeBase != "market" {
		t.Fatalf("hits = %+v", found.Hits)
	}

	// A member's own write publishes only when asked.
	var mine, kept service.KnowledgeDocumentView
	testutil.Call(t, testHandler.CreateKnowledgeDocument, asUser(docRequest(map[string]any{
		"kb": "company", "title": "Charter", "content": "We ship weekly.", "publish": true,
	}), fx.member)).Want(http.StatusCreated).JSON(&mine)
	testutil.Call(t, testHandler.CreateKnowledgeDocument, asUser(docRequest(map[string]any{
		"kb": "company", "title": "Notes", "content": "Rough notes.",
	}), fx.member)).Want(http.StatusCreated).JSON(&kept)
	if mine.State != "published" || mine.CreatedByType != "member" || kept.State != "draft" {
		t.Fatalf("member writes = %+v / %+v", mine, kept)
	}

	// Discard is for drafts; delete is admin-only and works on anything.
	testutil.Call(t, testHandler.DiscardKnowledgeDocument, asUser(docActionRequest("POST", kept.ID, "discard"), fx.member)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DiscardKnowledgeDocument, docActionRequest("POST", mine.ID, "discard")).Want(http.StatusConflict)
	testutil.Call(t, testHandler.DiscardKnowledgeDocument, docActionRequest("POST", kept.ID, "discard")).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.DeleteKnowledgeDocument, asUser(docActionRequest("DELETE", mine.ID, ""), fx.member)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DeleteKnowledgeDocument, fx.asAgent(docActionRequest("DELETE", mine.ID, ""))).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DeleteKnowledgeDocument, docActionRequest("DELETE", mine.ID, "")).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.DeleteKnowledgeDocument, docActionRequest("DELETE", mine.ID, "")).Want(http.StatusNotFound)
}

func TestKnowledgeAgentsCannotWriteDecisions(t *testing.T) {
	fx := newKnowledgeFixture(t)
	testutil.Call(t, testHandler.CreateKnowledgeDocument, fx.asAgent(docRequest(map[string]any{
		"kb": "decisions", "title": "Forged", "content": "We decided everything.",
	}))).Want(http.StatusForbidden)
}

func TestKnowledgeBasesForMembers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	fx := newKnowledgeFixture(t)
	var view service.KnowledgeBasesView
	testutil.Call(t, testHandler.ListKnowledgeBases, asUser(newRequest("GET", "/api/knowledge/bases", nil), fx.member)).Want(http.StatusOK).JSON(&view)
	if !view.Offered || view.State != "ready" || len(view.KnowledgeBases) != 6 || view.KnowledgeBases[0].Key != "company" {
		t.Fatalf("bases = %+v", view)
	}
}

func TestKnowledgeOfflineIsAStateNeverA500(t *testing.T) {
	fx := newKnowledgeFixture(t)
	fx.fake.SetUnavailable(true)

	res := testutil.Call(t, testHandler.CreateKnowledgeDocument, fx.asAgent(docRequest(map[string]any{
		"kb": "market", "title": "t", "content": "c",
	}))).Want(http.StatusServiceUnavailable)
	if body := res.Body.String(); !strings.Contains(body, `"state":"offline"`) {
		t.Fatalf("write body = %s", body)
	}
	var found service.KnowledgeSearchResult
	testutil.Call(t, testHandler.SearchKnowledge, searchRequest("anything")).Want(http.StatusOK).JSON(&found)
	if found.State != "offline" || found.Hits == nil || len(found.Hits) != 0 {
		t.Fatalf("offline search = %+v", found)
	}
	var list service.KnowledgeDocumentList
	testutil.Call(t, testHandler.ListKnowledgeDocuments, newRequest("GET", "/api/knowledge/documents", nil)).Want(http.StatusOK).JSON(&list)
	if list.State != "offline" {
		t.Fatalf("offline list = %+v", list)
	}
	var view service.KnowledgeBasesView
	testutil.Call(t, testHandler.ListKnowledgeBases, newRequest("GET", "/api/knowledge/bases", nil)).Want(http.StatusOK).JSON(&view)
	if view.State != "offline" || len(view.KnowledgeBases) != 6 {
		t.Fatalf("offline bases = %+v", view)
	}
}

func TestKnowledgeBeforeItIsEnabled(t *testing.T) {
	useKnowledge(t)
	res := testutil.Call(t, testHandler.CreateKnowledgeDocument, docRequest(map[string]any{"kb": "market", "title": "t", "content": "c"})).Want(http.StatusConflict)
	if body := res.Body.String(); !strings.Contains(body, `"state":"disabled"`) {
		t.Fatalf("body = %s", body)
	}
	var found service.KnowledgeSearchResult
	testutil.Call(t, testHandler.SearchKnowledge, searchRequest("x")).Want(http.StatusOK).JSON(&found)
	if found.State != "disabled" || len(found.Hits) != 0 {
		t.Fatalf("search = %+v", found)
	}
}

func TestKnowledgeNotOfferedNeverPanics(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	previous := testHandler.Knowledge
	testHandler.Knowledge = nil
	t.Cleanup(func() { testHandler.Knowledge = previous })
	testutil.Call(t, testHandler.ListKnowledgeBases, newRequest("GET", "/api/knowledge/bases", nil)).Want(http.StatusOK)
	testutil.Call(t, testHandler.SearchKnowledge, searchRequest("x")).Want(http.StatusOK)
	testutil.Call(t, testHandler.ListKnowledgeDocuments, newRequest("GET", "/api/knowledge/documents", nil)).Want(http.StatusOK)
	testutil.Call(t, testHandler.CreateKnowledgeDocument, docRequest(map[string]any{"kb": "market", "title": "t", "content": "c"})).Want(http.StatusConflict)
	testutil.Call(t, testHandler.PublishKnowledgeDocument, docActionRequest("POST", "0b1b8d4e-6f6f-4f0e-9c0e-3a8d1f2e4a55", "publish")).Want(http.StatusConflict)
}

func TestKnowledgeDocumentValidation(t *testing.T) {
	fx := newKnowledgeFixture(t)
	for name, body := range map[string]map[string]any{
		"both":       {"kb": "market", "title": "t", "content": "c", "url": "https://example.test"},
		"no title":   {"kb": "market", "content": "c"},
		"bad url":    {"kb": "market", "title": "t", "url": "ftp://example.test"},
		"bad source": {"kb": "market", "title": "t", "content": "c", "source_id": "nope"},
	} {
		testutil.Call(t, testHandler.CreateKnowledgeDocument, fx.asAgent(docRequest(body))).Want(http.StatusBadRequest)
		_ = name
	}
	testutil.Call(t, testHandler.CreateKnowledgeDocument, fx.asAgent(docRequest(map[string]any{"kb": "nope", "title": "t", "content": "c"}))).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.SearchKnowledge, newRequest("GET", "/api/knowledge/search", nil)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.ListKnowledgeDocuments, newRequest("GET", "/api/knowledge/documents?state=weird", nil)).Want(http.StatusBadRequest)
}
