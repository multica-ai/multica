package knowledge

// Contract tests for the platform calls: logins, tenant members, chat and rerank
// models, knowledge-base settings and data sources. The response fixtures
// (user_create, model_create_chat, model_create_rerank, model_debug_rejected,
// kb_get_settings, kb_patch_settings, datasource_list) follow the response
// shapes in WeKnora 0.9.0's handlers. They were written from that source, not
// recorded from a running instance; re-record them with the live check when a
// model or a data source is first configured on a deployment.

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
)

func TestEnsureUserSendsPasswordAndReturnsLogin(t *testing.T) {
	m := newMock(t).withLogin().
		onFixture("POST /api/v1/system/admin/users/create", 200, "user_create")
	user, err := newClient(t, m).EnsureUser(context.Background(), UserSpec{
		Username: "mul-owner", Email: "owner@example.test", Password: "generated-pass-1",
	})
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if user.ID != "a1d6c2e0-3f0b-4c1e-9a55-0d7f2b9e8c11" || user.Email != "owner@example.test" {
		t.Fatalf("user = %+v", user)
	}
	req := m.requests("POST", "/api/v1/system/admin/users/create")[0]
	if !strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") {
		t.Fatalf("user creation must use the service account's login: %v", req.Header)
	}
	body := decode(t, req.Body)
	if body["password"] != "generated-pass-1" || body["email"] != "owner@example.test" {
		t.Fatalf("create body = %v", body)
	}
}

func TestEnsureUserRejectsEmptyFieldsLocally(t *testing.T) {
	m := newMock(t).withLogin()
	_, err := newClient(t, m).EnsureUser(context.Background(), UserSpec{Email: "owner@example.test"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if n := len(m.requests("POST", "/api/v1/system/admin/users/create")); n != 0 {
		t.Fatalf("a user with no username reached WeKnora %d times", n)
	}
}

func TestEnsureUserWithoutUserInResponseIsAnError(t *testing.T) {
	m := newMock(t).withLogin().
		on("POST /api/v1/system/admin/users/create", 200, []byte(`{"success":true}`))
	if _, err := newClient(t, m).EnsureUser(context.Background(), UserSpec{Username: "mul-owner", Email: "owner@example.test"}); err == nil {
		t.Fatal("a response with no user was accepted")
	}
}

func TestEnsureUserNotSystemAdminIsErrForbidden(t *testing.T) {
	m := newMock(t).withLogin().
		on("POST /api/v1/system/admin/users/create", 403, []byte(`{"success":false,"error":{"message":"system administrator required"}}`))
	_, err := newClient(t, m).EnsureUser(context.Background(), UserSpec{Username: "mul-owner", Email: "owner@example.test"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestAddTenantMemberSendsRoleInTenant(t *testing.T) {
	m := newMock(t).withLogin().
		on("POST /api/v1/tenants/10002/members", 201, []byte(`{"success":true}`))
	if err := newClient(t, m).AddTenantMember(context.Background(), "10002", "owner@example.test", "admin"); err != nil {
		t.Fatalf("AddTenantMember: %v", err)
	}
	req := m.requests("POST", "/api/v1/tenants/10002/members")[0]
	if req.Header.Get("X-Tenant-ID") != "10002" {
		t.Fatalf("X-Tenant-ID = %q", req.Header.Get("X-Tenant-ID"))
	}
	body := decode(t, req.Body)
	if body["email"] != "owner@example.test" || body["role"] != "admin" {
		t.Fatalf("body = %v", body)
	}
}

func TestAddTenantMemberAlreadyPresentIsNotAnError(t *testing.T) {
	m := newMock(t).withLogin().
		on("POST /api/v1/tenants/10002/members", 409, []byte(`{"success":false,"error":{"message":"already a member"}}`))
	if err := newClient(t, m).AddTenantMember(context.Background(), "10002", "owner@example.test", "admin"); err != nil {
		t.Fatalf("a repeated AddTenantMember failed: %v", err)
	}
}

func TestEnsureModelFindsByTypeAndNameBeforeCreating(t *testing.T) {
	list := []byte(`{"success":true,"data":[
	  {"id":"` + modelID + `","name":"bge-m3","type":"Embedding","status":"active"},
	  {"id":"0e4f7a52-6c1d-4a8e-8a5b-2f1d9c7b3e10","name":"summary-chat","type":"KnowledgeQA","status":"active"}]}`)
	m := newMock(t).on("GET /api/v1/models", 200, list).
		onFixture("POST /api/v1/models", 201, "model_create_rerank")
	ws := newClient(t, m).Workspace(wsKey)

	chat, err := ws.EnsureModel(context.Background(), ModelSpec{
		Name: "summary-chat", Type: ModelTypeKnowledgeQA, Provider: "generic", BaseURL: "https://llm.example.test/v1", APIKey: "sk-llm",
	})
	if err != nil || chat.ID != "0e4f7a52-6c1d-4a8e-8a5b-2f1d9c7b3e10" {
		t.Fatalf("existing chat model = %+v, %v", chat, err)
	}
	if n := len(m.requests("POST", "/api/v1/models")); n != 0 {
		t.Fatalf("an existing model was created again (%d creates)", n)
	}

	// The same name with another type is a different model.
	rerank, err := ws.EnsureModel(context.Background(), ModelSpec{
		Name: "rerank-model", Type: ModelTypeRerank, Provider: "generic", BaseURL: "https://llm.example.test/v1", APIKey: "sk-llm",
	})
	if err != nil || rerank.ID != "5c2b9e8d-1f3a-4d6e-9b7c-8a0e2d4f6b21" {
		t.Fatalf("created rerank model = %+v, %v", rerank, err)
	}
	body := decode(t, m.requests("POST", "/api/v1/models")[0].Body)
	params := body["parameters"].(map[string]any)
	if body["type"] != "Rerank" || body["source"] != "remote" || params["api_key"] != "sk-llm" || params["provider"] != "generic" {
		t.Fatalf("create body = %v", body)
	}
}

// WeKnora sends no "stream" key on a non-streaming call, and the 9router gateway
// streams when the key is absent. The model's extra_body must carry stream:false
// so that the summary and Wiki calls get a JSON body back.
func TestEnsureModelSendsExtraBodyInCompatSpec(t *testing.T) {
	m := newMock(t).on("GET /api/v1/models", 200, []byte(`{"success":true,"data":[]}`)).
		onFixture("POST /api/v1/models", 201, "model_create_chat")
	_, err := newClient(t, m).Workspace(wsKey).EnsureModel(context.Background(), ModelSpec{
		Name: "summary-chat", Type: ModelTypeKnowledgeQA, Provider: "generic", BaseURL: "http://172.17.0.1:20128/v1",
		APIKey: "sk-llm", ExtraBody: map[string]any{"stream": false},
	})
	if err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	params := decode(t, m.requests("POST", "/api/v1/models")[0].Body)["parameters"].(map[string]any)
	spec, _ := params["spec"].(map[string]any)
	compat, _ := spec["compat"].(map[string]any)
	extra, _ := compat["extra_body"].(map[string]any)
	if extra["stream"] != false {
		t.Fatalf("create body parameters = %v, want spec.compat.extra_body.stream false", params)
	}
}

func TestEnsureModelRequiresEndpoint(t *testing.T) {
	m := newMock(t)
	_, err := newClient(t, m).Workspace(wsKey).EnsureModel(context.Background(), ModelSpec{Name: "x", Type: ModelTypeKnowledgeQA})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

// multipartFields reads a multipart body into its text fields.
func multipartFields(t *testing.T, raw []byte, contentType string) map[string]string {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("content type %q: %v", contentType, err)
	}
	r := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	out := map[string]string{}
	for {
		p, err := r.NextPart()
		if err != nil {
			break
		}
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(p)
		out[p.FormName()] = b.String()
	}
	return out
}

func TestTestModelSendsProbeAndNamesTheFailure(t *testing.T) {
	m := newMock(t).withLogin().
		onFixture("POST /api/v1/models/"+modelID+"/debug", 400, "model_debug_rejected")
	ws := newClient(t, m).Workspace(wsKey)
	err := ws.TestModel(context.Background(), modelID, ModelProbe{Input: "Reply with OK."})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Message, "upstream returned 401") {
		t.Fatalf("the WeKnora message did not reach the caller: %v", err)
	}
	req := m.requests("POST", "/api/v1/models/"+modelID+"/debug")[0]
	fields := multipartFields(t, req.Body, req.Header.Get("Content-Type"))
	if fields["input"] != "Reply with OK." || fields["documents"] != "" {
		t.Fatalf("probe fields = %v", fields)
	}
}

func TestTestModelRerankSendsDocuments(t *testing.T) {
	m := newMock(t).withLogin().
		on("POST /api/v1/models/"+modelID+"/debug", 200, []byte(`{"success":true,"data":{"results":[]}}`))
	err := newClient(t, m).Workspace(wsKey).TestModel(context.Background(), modelID, ModelProbe{
		Input: "company holidays", Documents: []string{"holiday list", "expense policy"},
	})
	if err != nil {
		t.Fatalf("TestModel: %v", err)
	}
	req := m.requests("POST", "/api/v1/models/"+modelID+"/debug")[0]
	fields := multipartFields(t, req.Body, req.Header.Get("Content-Type"))
	if fields["documents"] != `["holiday list","expense policy"]` {
		t.Fatalf("documents = %q", fields["documents"])
	}
}

func TestUpdateKnowledgeBaseTurnsWikiOnKeepingOtherFlags(t *testing.T) {
	m := newMock(t).
		onFixture("GET /api/v1/knowledge-bases/"+kbID, 200, "kb_get_settings").
		onFixture("PATCH /api/v1/knowledge-bases/"+kbID, 200, "kb_patch_settings")
	on := true
	kb, err := newClient(t, m).Workspace(wsKey).UpdateKnowledgeBase(context.Background(), kbID, KnowledgeBasePatch{
		SummaryModelID: strPtr("0e4f7a52-6c1d-4a8e-8a5b-2f1d9c7b3e10"), WikiEnabled: &on,
	})
	if err != nil {
		t.Fatalf("UpdateKnowledgeBase: %v", err)
	}
	if !kb.WikiEnabled || kb.SummaryModelID != "0e4f7a52-6c1d-4a8e-8a5b-2f1d9c7b3e10" {
		t.Fatalf("kb = %+v", kb)
	}
	body := decode(t, m.requests("PATCH", "/api/v1/knowledge-bases/"+kbID)[0].Body)
	strategy := body["indexing_strategy"].(map[string]any)
	if strategy["vector_enabled"] != true || strategy["keyword_enabled"] != true || strategy["wiki_enabled"] != true {
		t.Fatalf("strategy sent = %v; the flags Multica does not own must come back unchanged", strategy)
	}
	wiki := body["wiki_config"].(map[string]any)
	if wiki["extraction_granularity"] != "focused" {
		t.Fatalf("wiki_config = %v, want the focused setting", wiki)
	}
}

func TestUpdateKnowledgeBaseTurningWikiOffSendsNoWikiConfig(t *testing.T) {
	m := newMock(t).
		onFixture("GET /api/v1/knowledge-bases/"+kbID, 200, "kb_get_settings").
		onFixture("PATCH /api/v1/knowledge-bases/"+kbID, 200, "kb_get_settings")
	off := false
	if _, err := newClient(t, m).Workspace(wsKey).UpdateKnowledgeBase(context.Background(), kbID, KnowledgeBasePatch{WikiEnabled: &off}); err != nil {
		t.Fatalf("UpdateKnowledgeBase: %v", err)
	}
	body := decode(t, m.requests("PATCH", "/api/v1/knowledge-bases/"+kbID)[0].Body)
	if _, sent := body["wiki_config"]; sent {
		t.Fatalf("wiki_config sent when turning wiki off: %v", body)
	}
	if _, sent := body["summary_model_id"]; sent {
		t.Fatalf("summary_model_id sent when the patch did not name it: %v", body)
	}
}

func TestListDataSourcesReadsBareListWithFailures(t *testing.T) {
	m := newMock(t).onFixture("GET /api/v1/datasource", 200, "datasource_list")
	list, err := newClient(t, m).Workspace(wsKey).ListDataSources(context.Background(), kbID)
	if err != nil {
		t.Fatalf("ListDataSources: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("sources = %+v", list)
	}
	notion := list[0]
	if notion.Type != "notion" || notion.Status != "error" || notion.ErrorMessage == "" || notion.LastSyncAt == nil || notion.TotalItemsSynced != 42 {
		t.Fatalf("failed source = %+v", notion)
	}
	rss := list[1]
	if rss.LastSyncAt != nil || rss.ErrorMessage != "" {
		t.Fatalf("never-synced source = %+v", rss)
	}
	req := m.requests("GET", "/api/v1/datasource")[0]
	if req.Path != "/api/v1/datasource?kb_id="+kbID {
		t.Fatalf("path = %q", req.Path)
	}
}

func TestListDataSourcesMalformedBodyIsAnError(t *testing.T) {
	m := newMock(t).on("GET /api/v1/datasource", 200, []byte(`{"data":"not a list"}`))
	if _, err := newClient(t, m).Workspace(wsKey).ListDataSources(context.Background(), kbID); err == nil {
		t.Fatal("a malformed list was accepted")
	}
}

func TestListDataSourcesNeverSyncedTimestampMustParse(t *testing.T) {
	m := newMock(t).on("GET /api/v1/datasource", 200, []byte(`[{"id":"x","last_sync_at":"yesterday"}]`))
	if _, err := newClient(t, m).Workspace(wsKey).ListDataSources(context.Background(), kbID); err == nil {
		t.Fatal("an unparseable sync time was accepted")
	}
}

func strPtr(s string) *string { return &s }
