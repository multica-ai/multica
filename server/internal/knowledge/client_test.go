package knowledge

// Contract tests: the adapter is exercised against responses recorded from a
// live WeKnora v0.9.0 (testdata/*.json, secrets replaced with REDACTED
// placeholders). They pin the two directions of the contract: what the adapter
// sends (paths, auth headers, bodies) and how it reads what WeKnora answers.
// Re-record the fixtures when WeKnora is upgraded.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// recorded is one request the mock server saw.
type recorded struct {
	Method string
	Path   string // path plus query
	Header http.Header
	Body   []byte
}

// mock serves canned responses by "METHOD /path" (query ignored) and records
// every request.
type mock struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string][]reply // queue per route; the last reply repeats
	seen   []recorded
}

type reply struct {
	status int
	body   []byte
}

func newMock(t *testing.T) *mock {
	t.Helper()
	m := &mock{t: t, routes: map[string][]reply{}}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mock) on(route string, status int, body []byte) *mock {
	m.routes[route] = append(m.routes[route], reply{status, body})
	return m
}

func (m *mock) onFixture(route string, status int, name string) *mock {
	return m.on(route, status, fixture(m.t, name))
}

func (m *mock) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	m.seen = append(m.seen, recorded{r.Method, r.URL.RequestURI(), r.Header.Clone(), body})
	q := m.routes[r.Method+" "+r.URL.Path]
	var rep reply
	switch {
	case len(q) == 0:
		rep = reply{http.StatusNotFound, []byte(`{"success":false,"error":{"code":1003,"message":"no mock route"}}`)}
	case len(q) == 1:
		rep = q[0]
	default:
		rep, m.routes[r.Method+" "+r.URL.Path] = q[0], q[1:]
	}
	m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rep.status)
	_, _ = w.Write(rep.body)
}

func (m *mock) requests(method, path string) []recorded {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []recorded
	for _, r := range m.seen {
		p := r.Path
		if i := strings.IndexByte(p, '?'); i >= 0 {
			p = p[:i]
		}
		if r.Method == method && p == path {
			out = append(out, r)
		}
	}
	return out
}

func (m *mock) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.seen)
}

const (
	svcEmail    = "svc@example.test"
	svcPassword = "SvcPass12345"
	wsKey       = "sk-test-workspace-key"
	kbID        = "6230f301-de2c-4009-bd05-120bc0e6b2a0"
	draftID     = "3654005a-2cda-4b47-b767-25dddae0a291"
	fileDocID   = "eb3ae08e-d73a-454d-9b4a-b715d1b366b3"
	modelID     = "69969a78-e405-4a6b-907e-60091b70ba8c"
	endpointID  = "11a56d5f-13c7-40c0-b683-e8bc44742e34"
)

func newClient(t *testing.T, m *mock, mut ...func(*Config)) *Client {
	t.Helper()
	cfg := Config{
		BaseURL: m.srv.URL, PublicURL: "https://knowledge.example.com/",
		Email: svcEmail, Password: svcPassword,
		Timeout: 2 * time.Second, UploadTimeout: 2 * time.Second,
		ModelReadyTimeout: 2 * time.Second, ModelPollInterval: time.Millisecond,
	}
	for _, f := range mut {
		f(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func (m *mock) withLogin() *mock {
	return m.onFixture("POST /api/v1/auth/login", 200, "login_ok")
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode %q: %v", b, err)
	}
	return v
}

func TestNewValidatesURLs(t *testing.T) {
	for _, bad := range []string{"", "knowledge-app:8080", "ftp://x", "http://"} {
		if _, err := New(Config{BaseURL: bad}); err == nil {
			t.Errorf("New(%q) succeeded", bad)
		}
	}
	if _, err := New(Config{BaseURL: "http://a", PublicURL: "nope"}); err == nil {
		t.Error("invalid public url accepted")
	}
}

func TestMCPURLUsesPublicBase(t *testing.T) {
	c, _ := New(Config{BaseURL: "http://knowledge-app:8080", PublicURL: "https://knowledge.example.com/"})
	if got, want := c.MCPURL("abc"), "https://knowledge.example.com/mcp/abc"; got != want {
		t.Fatalf("MCPURL = %q, want %q", got, want)
	}
	c, _ = New(Config{BaseURL: "http://knowledge-app:8080/"})
	if got, want := c.MCPURL("abc"), "http://knowledge-app:8080/mcp/abc"; got != want {
		t.Fatalf("MCPURL without public url = %q, want %q", got, want)
	}
}

func TestPing(t *testing.T) {
	m := newMock(t).onFixture("GET /health", 200, "health")
	if err := newClient(t, m).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if got := m.requests("GET", "/health"); len(got) != 1 || got[0].Header.Get("Authorization") != "" {
		t.Fatalf("health must be unauthenticated: %+v", got)
	}
}

func TestEnsureTenantReusesOwnedTenant(t *testing.T) {
	m := newMock(t).withLogin().onFixture("GET /api/v1/auth/me", 200, "me")
	tenant, err := newClient(t, m).EnsureTenant(context.Background(), "mcl-fixture-ws", "d")
	if err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	if tenant.ID != "10002" || tenant.Name != "mcl-fixture-ws" {
		t.Fatalf("tenant = %+v", tenant)
	}
	if n := len(m.requests("POST", "/api/v1/tenants")); n != 0 {
		t.Fatalf("created %d tenants for an existing name", n)
	}
	login := decode(t, m.requests("POST", "/api/v1/auth/login")[0].Body)
	if login["email"] != svcEmail || login["password"] != svcPassword {
		t.Fatalf("login body = %v", login)
	}
	if got := m.requests("GET", "/api/v1/auth/me")[0].Header.Get("Authorization"); got != "Bearer jwt.access.REDACTED" {
		t.Fatalf("me authorization = %q", got)
	}
}

func TestEnsureTenantCreatesWhenMissing(t *testing.T) {
	m := newMock(t).withLogin().
		onFixture("GET /api/v1/auth/me", 200, "me").
		onFixture("POST /api/v1/tenants", 201, "tenant_create")
	tenant, err := newClient(t, m).EnsureTenant(context.Background(), "mcl-fixture-ws2", "Acme")
	if err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	if tenant.ID != "10004" {
		t.Fatalf("numeric tenant id was not stringified: %+v", tenant)
	}
	body := decode(t, m.requests("POST", "/api/v1/tenants")[0].Body)
	if body["name"] != "mcl-fixture-ws2" || body["description"] != "Acme" {
		t.Fatalf("create body = %v", body)
	}
}

func TestEnsureTenantQuotaIsTyped(t *testing.T) {
	m := newMock(t).withLogin().
		onFixture("GET /api/v1/auth/me", 200, "me").
		on("POST /api/v1/tenants", 429, []byte(`{"success":false,"error":{"code":1008,"message":"workspace limit reached"}}`))
	_, err := newClient(t, m).EnsureTenant(context.Background(), "new", "")
	if !errors.Is(err, ErrQuota) {
		t.Fatalf("err = %v, want ErrQuota", err)
	}
}

func TestCreateWorkspaceKeyReturnsToken(t *testing.T) {
	m := newMock(t).withLogin().onFixture("POST /api/v1/tenants/10002/api-keys", 201, "apikey_create")
	key, err := newClient(t, m).CreateWorkspaceKey(context.Background(), "10002", "multica")
	if err != nil {
		t.Fatalf("CreateWorkspaceKey: %v", err)
	}
	// api_key in the response is the encrypted form; token is the usable one.
	if key != "sk-REDACTED-FIXTURE" {
		t.Fatalf("key = %q, want the token field", key)
	}
	req := m.requests("POST", "/api/v1/tenants/10002/api-keys")[0]
	if req.Header.Get("X-Tenant-ID") != "10002" || req.Header.Get("Authorization") == "" {
		t.Fatalf("key creation must use the owner's login and name the tenant: %v", req.Header)
	}
	body := decode(t, req.Body)
	if body["full_access"] != true || body["name"] != "multica" {
		t.Fatalf("body = %v", body)
	}
}

func TestJWTExpiryRelogsInOnce(t *testing.T) {
	m := newMock(t).withLogin().
		on("GET /api/v1/auth/me", 401, []byte(`{"success":false,"error":{"code":1001,"message":"token expired"}}`)).
		onFixture("GET /api/v1/auth/me", 200, "me")
	if _, err := newClient(t, m).EnsureTenant(context.Background(), "mcl-fixture-ws", ""); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	if n := len(m.requests("POST", "/api/v1/auth/login")); n != 2 {
		t.Fatalf("logins = %d, want 2 (initial and after the 401)", n)
	}
}

func TestPersistentUnauthorizedIsErrAuth(t *testing.T) {
	m := newMock(t).withLogin().on("GET /api/v1/auth/me", 401, []byte(`{"error":"Unauthorized"}`))
	_, err := newClient(t, m).EnsureTenant(context.Background(), "x", "")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if n := len(m.requests("GET", "/api/v1/auth/me")); n != 2 {
		t.Fatalf("retried %d times, want exactly one retry", n)
	}
}

func TestLoginRejectedIsErrAuth(t *testing.T) {
	m := newMock(t).onFixture("POST /api/v1/auth/login", 401, "login_bad")
	_, err := newClient(t, m).EnsureTenant(context.Background(), "x", "")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if strings.Contains(err.Error(), svcPassword) {
		t.Fatal("error leaks the password")
	}
}

func TestMissingServiceAccountIsErrAuthWithoutRequest(t *testing.T) {
	m := newMock(t)
	c := newClient(t, m, func(c *Config) { c.Email, c.Password = "", "" })
	if _, err := c.EnsureTenant(context.Background(), "x", ""); !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if m.count() != 0 {
		t.Fatal("contacted the server without credentials")
	}
}

func TestEnsureEmbeddingModelCreatesAndWaits(t *testing.T) {
	m := newMock(t).
		on("GET /api/v1/models", 200, []byte(`{"success":true,"data":[]}`)).
		onFixture("POST /api/v1/models", 201, "model_create").
		on("GET /api/v1/models/"+modelID, 200, []byte(`{"success":true,"data":{"id":"`+modelID+`","name":"bge-m3","type":"Embedding","status":"downloading"}}`)).
		onFixture("GET /api/v1/models/"+modelID, 200, "model_get")
	model, err := newClient(t, m).Workspace(wsKey).EnsureEmbeddingModel(context.Background(), EmbeddingModelSpec{
		Name: "bge-m3", Provider: "ollama", BaseURL: "http://ollama.test:11434", Dimension: 1024,
	})
	if err != nil {
		t.Fatalf("EnsureEmbeddingModel: %v", err)
	}
	if model.ID != modelID || model.Status != "active" {
		t.Fatalf("model = %+v", model)
	}
	req := m.requests("POST", "/api/v1/models")[0]
	if req.Header.Get("X-API-Key") != wsKey || req.Header.Get("Authorization") != "" {
		t.Fatalf("workspace calls authenticate with X-API-Key only: %v", req.Header)
	}
	body := decode(t, req.Body)
	params := body["parameters"].(map[string]any)
	if body["type"] != "Embedding" || body["source"] != "local" || body["name"] != "bge-m3" ||
		params["provider"] != "ollama" || params["base_url"] != "http://ollama.test:11434" ||
		params["embedding_parameters"].(map[string]any)["dimension"] != float64(1024) {
		t.Fatalf("create body = %v", body)
	}
	if _, hasKey := params["api_key"]; hasKey {
		t.Fatal("empty api key was sent")
	}
}

func TestEnsureEmbeddingModelRemoteProviderCarriesKey(t *testing.T) {
	m := newMock(t).
		on("GET /api/v1/models", 200, []byte(`{"success":true,"data":[]}`)).
		on("POST /api/v1/models", 201, []byte(`{"success":true,"data":{"id":"m1","name":"text-embed","type":"Embedding","status":"active"}}`))
	_, err := newClient(t, m).Workspace(wsKey).EnsureEmbeddingModel(context.Background(), EmbeddingModelSpec{
		Name: "text-embed", Provider: "generic", BaseURL: "https://embed.example.com/v1", APIKey: "k", Dimension: 768,
	})
	if err != nil {
		t.Fatalf("EnsureEmbeddingModel: %v", err)
	}
	body := decode(t, m.requests("POST", "/api/v1/models")[0].Body)
	params := body["parameters"].(map[string]any)
	if body["source"] != "remote" || params["api_key"] != "k" || params["provider"] != "generic" {
		t.Fatalf("create body = %v", body)
	}
}

func TestEnsureEmbeddingModelIsIdempotent(t *testing.T) {
	m := newMock(t).onFixture("GET /api/v1/models", 200, "models_list")
	model, err := newClient(t, m).Workspace(wsKey).EnsureEmbeddingModel(context.Background(), EmbeddingModelSpec{
		Name: "bge-m3", Provider: "ollama", BaseURL: "http://ollama.test:11434", Dimension: 1024,
	})
	if err != nil {
		t.Fatalf("EnsureEmbeddingModel: %v", err)
	}
	if model.ID != modelID {
		t.Fatalf("model = %+v", model)
	}
	if n := len(m.requests("POST", "/api/v1/models")); n != 0 {
		t.Fatalf("created %d models although one exists", n)
	}
}

func TestEnsureEmbeddingModelTimesOutWhileDownloading(t *testing.T) {
	stuck := []byte(`{"success":true,"data":{"id":"` + modelID + `","name":"bge-m3","type":"Embedding","status":"downloading"}}`)
	m := newMock(t).
		on("GET /api/v1/models", 200, []byte(`{"success":true,"data":[]}`)).
		on("POST /api/v1/models", 201, stuck).
		on("GET /api/v1/models/"+modelID, 200, stuck)
	c := newClient(t, m, func(c *Config) { c.ModelReadyTimeout = 20 * time.Millisecond })
	_, err := c.Workspace(wsKey).EnsureEmbeddingModel(context.Background(), EmbeddingModelSpec{Name: "bge-m3", Provider: "ollama", Dimension: 1024})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestKnowledgeBaseRoundTrip(t *testing.T) {
	m := newMock(t).
		onFixture("POST /api/v1/knowledge-bases", 201, "kb_create").
		onFixture("GET /api/v1/knowledge-bases", 200, "kb_list").
		on("DELETE /api/v1/knowledge-bases/"+kbID, 200, []byte(`{"success":true}`))
	ws := newClient(t, m).Workspace(wsKey)
	ctx := context.Background()

	kb, err := ws.CreateKnowledgeBase(ctx, KnowledgeBaseSpec{Name: "Company", Description: "Charter", EmbeddingModelID: modelID})
	if err != nil {
		t.Fatalf("CreateKnowledgeBase: %v", err)
	}
	if kb.ID != kbID || kb.Name != "Company" || kb.EmbeddingModelID != modelID {
		t.Fatalf("kb = %+v", kb)
	}
	body := decode(t, m.requests("POST", "/api/v1/knowledge-bases")[0].Body)
	if body["embedding_model_id"] != modelID || body["type"] != "document" || body["name"] != "Company" {
		t.Fatalf("create body = %v", body)
	}

	list, err := ws.ListKnowledgeBases(ctx)
	if err != nil || len(list) != 1 || list[0].ID != kbID || list[0].DocumentCount != 3 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := ws.DeleteKnowledgeBase(ctx, kbID); err != nil {
		t.Fatalf("DeleteKnowledgeBase: %v", err)
	}
}

func TestManualDraftIsNotIndexedAndPublishes(t *testing.T) {
	m := newMock(t).
		onFixture("POST /api/v1/knowledge-bases/"+kbID+"/knowledge/manual", 200, "manual_draft").
		onFixture("GET /api/v1/knowledge/"+draftID, 200, "manual_draft").
		onFixture("PUT /api/v1/knowledge/manual/"+draftID, 200, "manual_publish")
	ws := newClient(t, m).Workspace(wsKey)
	ctx := context.Background()

	doc, err := ws.CreateManual(ctx, kbID, ManualDocument{Title: "Brand voice", Content: "# Brand voice\n\nWe write plainly."})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if !doc.IsDraft() || doc.IsIndexed() || doc.ID != draftID || doc.Type != "manual" || doc.Title != "Brand voice" {
		t.Fatalf("doc = %+v", doc)
	}
	body := decode(t, m.requests("POST", "/api/v1/knowledge-bases/"+kbID+"/knowledge/manual")[0].Body)
	if body["status"] != "draft" {
		t.Fatalf("default write must be a draft, got status %v", body["status"])
	}

	published, err := ws.Publish(ctx, draftID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if published.ParseStatus != ParsePending || published.IsDraft() {
		t.Fatalf("published = %+v", published)
	}
	put := decode(t, m.requests("PUT", "/api/v1/knowledge/manual/"+draftID)[0].Body)
	// WeKnora replaces the whole body and refuses empty content, so the stored
	// Markdown must travel back with the new status.
	if put["status"] != "publish" || put["title"] != "Brand voice" ||
		!strings.Contains(put["content"].(string), "Our refund window is 14 days") {
		t.Fatalf("publish body = %v", put)
	}
}

func TestCreateManualPublishDirect(t *testing.T) {
	m := newMock(t).onFixture("POST /api/v1/knowledge-bases/"+kbID+"/knowledge/manual", 200, "manual_publish")
	_, err := newClient(t, m).Workspace(wsKey).CreateManual(context.Background(), kbID, ManualDocument{Title: "t", Content: "c", Publish: true})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	body := decode(t, m.requests("POST", "/api/v1/knowledge-bases/"+kbID+"/knowledge/manual")[0].Body)
	if body["status"] != "publish" {
		t.Fatalf("status = %v", body["status"])
	}
}

func TestCreateManualRejectsEmptyContentLocally(t *testing.T) {
	m := newMock(t)
	_, err := newClient(t, m).Workspace(wsKey).CreateManual(context.Background(), kbID, ManualDocument{Title: "t", Content: "  \n"})
	if !errors.Is(err, ErrInvalid) || m.count() != 0 {
		t.Fatalf("err = %v, requests = %d", err, m.count())
	}
}

func TestPublishIsIdempotentAndManualOnly(t *testing.T) {
	m := newMock(t).
		onFixture("GET /api/v1/knowledge/"+draftID, 200, "knowledge_get_published").
		onFixture("GET /api/v1/knowledge/"+fileDocID, 200, "file_upload")
	ws := newClient(t, m).Workspace(wsKey)
	ctx := context.Background()

	if _, err := ws.Publish(ctx, draftID); err != nil {
		t.Fatalf("Publish of a published document: %v", err)
	}
	if n := len(m.requests("PUT", "/api/v1/knowledge/manual/"+draftID)); n != 0 {
		t.Fatalf("re-published an already published document (%d PUTs)", n)
	}
	if _, err := ws.Publish(ctx, fileDocID); !errors.Is(err, ErrNotManual) {
		t.Fatalf("err = %v, want ErrNotManual", err)
	}
}

func TestFileUploadCarriesFolderAndMapsDuplicate(t *testing.T) {
	m := newMock(t).
		onFixture("POST /api/v1/knowledge-bases/"+kbID+"/knowledge/file", 200, "file_upload").
		onFixture("POST /api/v1/knowledge-bases/"+kbID+"/knowledge/file", 409, "file_duplicate")
	ws := newClient(t, m).Workspace(wsKey)
	ctx := context.Background()

	doc, err := ws.CreateFile(ctx, kbID, FileDocument{Name: "runbook.md", FolderPath: "/ops/", Content: strings.NewReader("# Runbook")})
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if doc.ID != fileDocID || doc.FolderPath != "ops" || doc.Type != "file" || doc.FileName != "runbook.md" {
		t.Fatalf("doc = %+v", doc)
	}
	req := m.requests("POST", "/api/v1/knowledge-bases/"+kbID+"/knowledge/file")[0]
	if !strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
		t.Fatalf("content type = %q", req.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(req.Body), `name="fileName"`) || !strings.Contains(string(req.Body), "ops/runbook.md") ||
		!strings.Contains(string(req.Body), `name="file"; filename="runbook.md"`) || !strings.Contains(string(req.Body), "# Runbook") {
		t.Fatalf("multipart body = %q", req.Body)
	}

	_, err = ws.CreateFile(ctx, kbID, FileDocument{Name: "runbook.md", FolderPath: "ops", Content: strings.NewReader("# Runbook")})
	var dup *DuplicateError
	if !errors.Is(err, ErrDuplicate) || !errors.As(err, &dup) || dup.Existing.ID != fileDocID {
		t.Fatalf("err = %v, want a DuplicateError for %s", err, fileDocID)
	}
}

func TestURLImportAndDuplicate(t *testing.T) {
	m := newMock(t).
		onFixture("POST /api/v1/knowledge-bases/"+kbID+"/knowledge/url", 201, "url_create").
		onFixture("POST /api/v1/knowledge-bases/"+kbID+"/knowledge/url", 409, "url_duplicate")
	ws := newClient(t, m).Workspace(wsKey)
	ctx := context.Background()

	doc, err := ws.CreateURL(ctx, kbID, URLDocument{URL: "https://example.com/"})
	if err != nil {
		t.Fatalf("CreateURL: %v", err)
	}
	if doc.Type != "url" || doc.Source != "https://example.com/" || doc.ParseStatus != ParsePending {
		t.Fatalf("doc = %+v", doc)
	}
	body := decode(t, m.requests("POST", "/api/v1/knowledge-bases/"+kbID+"/knowledge/url")[0].Body)
	if body["url"] != "https://example.com/" {
		t.Fatalf("body = %v", body)
	}
	_, err = ws.CreateURL(ctx, kbID, URLDocument{URL: "https://example.com/"})
	var dup *DuplicateError
	if !errors.Is(err, ErrDuplicate) || !errors.As(err, &dup) || dup.Existing.ID == "" || dup.Code != "duplicate_url" {
		t.Fatalf("err = %v", err)
	}
}

func TestListDocumentsPagesAndFilters(t *testing.T) {
	m := newMock(t).onFixture("GET /api/v1/knowledge-bases/"+kbID+"/knowledge", 200, "knowledge_list")
	page, err := newClient(t, m).Workspace(wsKey).ListDocuments(context.Background(), kbID, ListOptions{Page: 2, PageSize: 5, Keyword: "run book"})
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if page.Total != 3 || len(page.Items) != 3 || page.Items[1].Title != "runbook.md" || !page.Items[1].IsIndexed() {
		t.Fatalf("page = %+v", page)
	}
	if got := m.requests("GET", "/api/v1/knowledge-bases/"+kbID+"/knowledge")[0].Path; got != "/api/v1/knowledge-bases/"+kbID+"/knowledge?keyword=run+book&page=2&page_size=5" {
		t.Fatalf("query = %s", got)
	}
}

func TestReadManualAndParsedFile(t *testing.T) {
	m := newMock(t).
		onFixture("GET /api/v1/knowledge/"+draftID, 200, "knowledge_get_published").
		onFixture("GET /api/v1/knowledge/"+fileDocID, 200, "file_upload").
		onFixture("GET /api/v1/chunks/"+fileDocID, 200, "chunks_list")
	ws := newClient(t, m).Workspace(wsKey)
	ctx := context.Background()

	manual, err := ws.Read(ctx, draftID)
	if err != nil || !strings.Contains(manual.Content, "Our refund window is 14 days") || manual.Title != "Brand voice" {
		t.Fatalf("manual = %+v, %v", manual, err)
	}
	if n := len(m.requests("GET", "/api/v1/chunks/"+draftID)); n != 0 {
		t.Fatal("read chunks for a manual document")
	}
	file, err := ws.Read(ctx, fileDocID)
	if err != nil || !strings.HasPrefix(file.Content, "# Runbook") {
		t.Fatalf("file = %+v, %v", file, err)
	}
}

func TestSearchParsesHitsAndFiltersByScore(t *testing.T) {
	m := newMock(t).
		onFixture("POST /api/v1/knowledge-search", 200, "search_hit").
		onFixture("POST /api/v1/knowledge-search", 200, "search_hit").
		onFixture("POST /api/v1/knowledge-search", 200, "search_empty")
	ws := newClient(t, m).Workspace(wsKey)
	ctx := context.Background()

	hits, err := ws.Search(ctx, SearchRequest{Query: "refund window", KnowledgeBaseIDs: []string{kbID}, Limit: 5})
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits = %+v, %v", hits, err)
	}
	h := hits[0]
	if h.DocumentID != draftID || h.KnowledgeBaseID != kbID || h.Title != "Brand voice" || h.FileName != "Brand voice.md" ||
		h.Score < 0.71 || h.Score > 0.72 || !strings.Contains(h.Content, "14 days") || h.ChunkID == "" {
		t.Fatalf("hit = %+v", h)
	}
	body := decode(t, m.requests("POST", "/api/v1/knowledge-search")[0].Body)
	if body["query"] != "refund window" || body["match_count"] != float64(5) || body["knowledge_base_ids"].([]any)[0] != kbID {
		t.Fatalf("search body = %v", body)
	}

	hits, err = ws.Search(ctx, SearchRequest{Query: "refund window", MinScore: 0.8})
	if err != nil || len(hits) != 0 {
		t.Fatalf("a hit below MinScore survived: %+v, %v", hits, err)
	}
	hits, err = ws.Search(ctx, SearchRequest{Query: "nothing"})
	if err != nil || len(hits) != 0 {
		t.Fatalf("empty = %+v, %v", hits, err)
	}
	if _, err := ws.Search(ctx, SearchRequest{Query: " "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank query err = %v", err)
	}
}

func TestMCPEndpointLifecycle(t *testing.T) {
	m := newMock(t).
		onFixture("POST /api/v1/mcp-endpoints", 201, "mcp_create").
		onFixture("GET /api/v1/mcp-endpoints", 200, "mcp_list").
		onFixture("POST /api/v1/mcp-endpoints/"+endpointID+"/rotate-token", 200, "mcp_rotate").
		onFixture("PUT /api/v1/mcp-endpoints/"+endpointID, 200, "mcp_update")
	ws := newClient(t, m).Workspace(wsKey)
	ctx := context.Background()

	created, err := ws.CreateMCPEndpoint(ctx, MCPEndpointSpec{
		Name: "company-read", KnowledgeBaseIDs: []string{kbID}, Tools: ReadOnlyMCPTools,
	})
	if err != nil {
		t.Fatalf("CreateMCPEndpoint: %v", err)
	}
	if created.ID != endpointID || created.Path != "/mcp/"+endpointID || created.Token != "mcp_REDACTED-FIXTURE" || !created.Enabled {
		t.Fatalf("created = %+v", created)
	}
	body := decode(t, m.requests("POST", "/api/v1/mcp-endpoints")[0].Body)
	tools := body["tools"].([]any)
	if len(tools) != 8 || body["name"] != "company-read" {
		t.Fatalf("body = %v", body)
	}
	for _, tool := range tools {
		if s := tool.(string); s == "ask" || strings.HasPrefix(s, "add_") || strings.HasPrefix(s, "update_") || strings.HasPrefix(s, "delete_") {
			t.Fatalf("read-only endpoint exposes %s", s)
		}
	}

	list, err := ws.ListMCPEndpoints(ctx)
	if err != nil || len(list) != 1 || list[0].Token != "" || list[0].Name != "company-read" {
		t.Fatalf("list = %+v, %v (a listing must not carry a token)", list, err)
	}
	rotated, err := ws.RotateMCPEndpointToken(ctx, endpointID)
	if err != nil || rotated.Token == "" {
		t.Fatalf("rotated = %+v, %v", rotated, err)
	}
	updated, err := ws.SetMCPEndpointKnowledgeBases(ctx, endpointID, []string{kbID})
	if err != nil || updated.Token != "" || len(updated.KnowledgeBaseIDs) != 1 {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	put := decode(t, m.requests("PUT", "/api/v1/mcp-endpoints/"+endpointID)[0].Body)
	if len(put) != 1 {
		t.Fatalf("a partial update must only carry the changed field, got %v", put)
	}
}

func TestMCPEndpointGuardsAgainstExposingEverything(t *testing.T) {
	m := newMock(t)
	ws := newClient(t, m).Workspace(wsKey)
	ctx := context.Background()
	// An empty knowledge base list means "every base in the workspace" to
	// WeKnora, which would expose restricted bases.
	if _, err := ws.CreateMCPEndpoint(ctx, MCPEndpointSpec{Name: "x", Tools: ReadOnlyMCPTools}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("create with no bases: %v", err)
	}
	if _, err := ws.SetMCPEndpointKnowledgeBases(ctx, endpointID, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("set no bases: %v", err)
	}
	if m.count() != 0 {
		t.Fatal("guard let a request through")
	}
}

func TestDeleteDocumentAndTenant(t *testing.T) {
	m := newMock(t).withLogin().
		onFixture("DELETE /api/v1/knowledge/"+draftID, 200, "knowledge_delete").
		onFixture("DELETE /api/v1/tenants/10004", 200, "tenant_delete")
	c := newClient(t, m)
	if err := c.Workspace(wsKey).Delete(context.Background(), draftID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := c.DeleteTenant(context.Background(), "10004"); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    []byte
		want    error
		message string
		code    string
	}{
		{"not found", 404, fixture(t, "err_notfound"), ErrNotFound, "Knowledge not found", "1003"},
		{"validation", 400, fixture(t, "err_validation"), ErrInvalid, "name cannot be empty", "1000"},
		{"string error body", 401, fixture(t, "err_apikey"), ErrAuth, "Unauthorized: invalid API key", ""},
		{"forbidden", 403, []byte(`{"error":{"code":1002,"message":"scope does not allow one or more knowledge bases"}}`), ErrForbidden, "scope does not allow one or more knowledge bases", "1002"},
		{"top-level message", 400, []byte(`{"success":false,"message":"bad"}`), ErrInvalid, "bad", ""},
		{"gateway", 502, []byte(`<html>bad gateway</html>`), ErrUnavailable, "", ""},
		{"unavailable", 503, nil, ErrUnavailable, "", ""},
		{"gateway timeout", 504, nil, ErrUnavailable, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMock(t).on("GET /api/v1/knowledge-bases", tc.status, tc.body)
			_, err := newClient(t, m).Workspace(wsKey).ListKnowledgeBases(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			var api *APIError
			if !errors.As(err, &api) || api.Status != tc.status || api.Message != tc.message || api.Code != tc.code {
				t.Fatalf("api error = %+v", api)
			}
			for _, other := range []error{ErrUnavailable, ErrAuth, ErrForbidden, ErrNotFound, ErrInvalid, ErrQuota, ErrDuplicate} {
				if other != tc.want && errors.Is(err, other) {
					t.Fatalf("error also matches %v", other)
				}
			}
		})
	}
}

func TestPlainServerErrorIsNotUnavailable(t *testing.T) {
	m := newMock(t).on("GET /api/v1/knowledge-bases", 500, []byte(`{"error":{"code":1009,"message":"boom"}}`))
	_, err := newClient(t, m).Workspace(wsKey).ListKnowledgeBases(context.Background())
	var api *APIError
	if !errors.As(err, &api) || api.Status != 500 || errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnreachableSidecarIsErrUnavailable(t *testing.T) {
	m := newMock(t)
	c := newClient(t, m)
	m.srv.Close()
	for name, call := range map[string]func() error{
		"ping":      func() error { return c.Ping(context.Background()) },
		"workspace": func() error { _, err := c.Workspace(wsKey).ListKnowledgeBases(context.Background()); return err },
		"tenant":    func() error { _, err := c.EnsureTenant(context.Background(), "x", ""); return err },
	} {
		err := call()
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: err = %v, want ErrUnavailable", name, err)
		}
		if err != nil && strings.Contains(err.Error(), m.srv.URL) {
			t.Errorf("%s: error leaks the sidecar address: %v", name, err)
		}
	}
}

func TestSlowSidecarIsErrUnavailable(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(release)
	c, err := New(Config{BaseURL: slow.URL, Timeout: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = c.Ping(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %s despite a 30ms timeout", time.Since(start))
	}
}

func TestCallerCancellationIsNotUnavailable(t *testing.T) {
	m := newMock(t).onFixture("GET /health", 200, "health")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := newClient(t, m).Ping(ctx)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want context.Canceled and not ErrUnavailable", err)
	}
}

func TestMalformedSuccessBodyIsAnError(t *testing.T) {
	m := newMock(t).on("GET /api/v1/knowledge-bases", 200, []byte(`not json`))
	if _, err := newClient(t, m).Workspace(wsKey).ListKnowledgeBases(context.Background()); err == nil {
		t.Fatal("garbage 200 body accepted")
	}
	m = newMock(t).withLogin().on("POST /api/v1/tenants/1/api-keys", 201, []byte(`{"data":{"api_key":"enc:v1:abc"}}`))
	if _, err := newClient(t, m).CreateWorkspaceKey(context.Background(), "1", "k"); err == nil {
		t.Fatal("a response without a token must not yield the encrypted api_key")
	}
}

func TestUnknownFieldsAndMissingFieldsAreTolerated(t *testing.T) {
	m := newMock(t).on("GET /api/v1/knowledge-bases", 200, []byte(`{"success":true,"data":[{"id":"a","name":"n","brand_new_field":{"x":1}},{"id":"b"}],"extra":1}`))
	list, err := newClient(t, m).Workspace(wsKey).ListKnowledgeBases(context.Background())
	if err != nil || len(list) != 2 || list[0].Name != "n" || list[1].Name != "" {
		t.Fatalf("list = %+v, %v", list, err)
	}
}
