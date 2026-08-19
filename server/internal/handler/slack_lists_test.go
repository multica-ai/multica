package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/slack"
)

type fakeSlackLists struct {
	schema      slack.ListsSchema
	schemaErr   error
	item        slack.ListsItem
	writeErr    error
	lastListID  string
	lastItemID  string
	lastFields  []slack.ListsField
	schemaCalls int
	createCalls int
}

func (f *fakeSlackLists) Schema(_ context.Context, _, _, _ pgtype.UUID, listID string) (slack.ListsSchema, error) {
	f.schemaCalls++
	f.lastListID = listID
	return f.schema, f.schemaErr
}
func (f *fakeSlackLists) Create(_ context.Context, _, _, _ pgtype.UUID, listID string, fields []slack.ListsField) (slack.ListsItem, error) {
	f.createCalls++
	f.lastListID = listID
	f.lastFields = fields
	return f.item, f.writeErr
}
func (f *fakeSlackLists) Update(_ context.Context, _, _, _ pgtype.UUID, listID, itemID string, fields []slack.ListsField) (slack.ListsItem, error) {
	f.lastListID = listID
	f.lastItemID = itemID
	f.lastFields = fields
	return f.item, f.writeErr
}

func withSlackLists(t *testing.T, api SlackListsAPI) {
	t.Helper()
	orig := testHandler.SlackLists
	testHandler.SlackLists = api
	t.Cleanup(func() { testHandler.SlackLists = orig })
}

func slackListsReq(method, target, taskID, listID, itemID string, body any) *http.Request {
	r := newRequest(method, target, body)
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Task-ID", taskID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("listId", listID)
	if itemID != "" {
		rctx.URLParams.Add("itemId", itemID)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestParseSlackListsFieldsObjectAndSkipEmpty(t *testing.T) {
	fields, err := parseSlackListsFields([]byte(`{"灵感标题":"lamp","下一步验证":"","优先级":"中"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestGetSlackListsSchemaAndCreate(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID := newChatHistoryTask(t, true)
	fake := &fakeSlackLists{
		schema: slack.ListsSchema{ListID: "F0BR8PBUAQH", Title: "Daykee 灵感池", Columns: []slack.ListsColumn{{ID: "ColT", Name: "Title"}}},
		item:   slack.ListsItem{ID: "Rec1", ListID: "F0BR8PBUAQH", Title: "lamp"},
	}
	withSlackLists(t, fake)

	w := httptest.NewRecorder()
	testHandler.GetSlackListsSchema(w, slackListsReq(http.MethodGet, "/api/slack/lists/F0BR8PBUAQH/schema", taskID, "F0BR8PBUAQH", "", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("schema status = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "xoxb-") {
		t.Fatal("schema response leaked a token")
	}
	if fake.lastListID != "F0BR8PBUAQH" {
		t.Fatalf("schema list = %q", fake.lastListID)
	}

	w = httptest.NewRecorder()
	testHandler.CreateSlackListsItem(w, slackListsReq(http.MethodPost, "/api/slack/lists/F0BR8PBUAQH/items", taskID, "F0BR8PBUAQH", "", map[string]any{"fields": map[string]any{"灵感标题": "lamp"}}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", w.Code, w.Body.String())
	}
	var item slack.ListsItem
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.ID != "Rec1" || item.Title != "lamp" {
		t.Fatalf("item = %+v", item)
	}
	if strings.Contains(w.Body.String(), "xoxb-") {
		t.Fatal("create response leaked a token")
	}
}

func TestSlackListsRejectsForgedTaskIDAndMapsErrors(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID := newChatHistoryTask(t, true)
	fake := &fakeSlackLists{schemaErr: slack.ErrListsListNotAllowed}
	withSlackLists(t, fake)

	req := newRequest(http.MethodGet, "/api/slack/lists/F0BPZZZZZZZ/schema", nil)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.GetSlackListsSchema(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("forged task status = %d", w.Code)
	}
	if fake.schemaCalls != 0 {
		t.Fatal("service must not run without task_token")
	}

	w = httptest.NewRecorder()
	testHandler.GetSlackListsSchema(w, slackListsReq(http.MethodGet, "/api/slack/lists/F0BPZZZZZZZ/schema", taskID, "F0BPZZZZZZZ", "", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("allowlist status = %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "F0BPZZZZZZZ") {
		t.Fatalf("allowlist error = %s", w.Body.String())
	}

	fake.schemaErr = slack.ErrListsNotConfigured
	w = httptest.NewRecorder()
	testHandler.GetSlackListsSchema(w, slackListsReq(http.MethodGet, "/api/slack/lists/F0BR8PBUAQH/schema", taskID, "F0BR8PBUAQH", "", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no install status = %d: %s", w.Code, w.Body.String())
	}

	fake.writeErr = slack.ErrListsWriteNotAuthorized
	w = httptest.NewRecorder()
	testHandler.CreateSlackListsItem(w, slackListsReq(http.MethodPost, "/api/slack/lists/F0BR8PBUAQH/items", taskID, "F0BR8PBUAQH", "", map[string]any{"fields": map[string]any{"Title": "x"}}))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "/idea") {
		t.Fatalf("ordinary chat = %d %s", w.Code, w.Body.String())
	}

	fake.writeErr = errors.New("upstream rejected xoxb-should-never-leak")
	w = httptest.NewRecorder()
	testHandler.CreateSlackListsItem(w, slackListsReq(http.MethodPost, "/api/slack/lists/F0BR8PBUAQH/items", taskID, "F0BR8PBUAQH", "", map[string]any{"fields": map[string]any{"Title": "x"}}))
	if strings.Contains(w.Body.String(), "xoxb-") {
		t.Fatalf("token leaked in handler error: %s", w.Body.String())
	}
}

func TestMapSlackListsErrorAPICode(t *testing.T) {
	mapped := mapSlackListsError(slackAPIErrorStub{code: "uneditable_column"}, "F1")
	if mapped.status != http.StatusBadGateway || !strings.Contains(mapped.msg, "uneditable_column") {
		t.Fatalf("mapped = %+v", mapped)
	}
}

type slackAPIErrorStub struct{ code string }

func (s slackAPIErrorStub) Error() string    { return "slack: " + s.code }
func (s slackAPIErrorStub) APIError() string { return s.code }
