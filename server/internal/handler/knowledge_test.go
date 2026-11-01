package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// useKnowledge installs a knowledge service backed by a fake on the shared test
// handler and removes everything it provisions afterwards. The tables involved
// have no foreign keys, so nothing else would.
func useKnowledge(t *testing.T) *knowledge.Fake {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 7)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatal(err)
	}
	fake := knowledge.NewFake()
	previous := testHandler.Knowledge
	testHandler.Knowledge = &service.KnowledgeService{
		Queries: testHandler.Queries, DB: testPool, Client: fake, Box: box,
		Embedding: knowledge.EmbeddingModelSpec{Name: "bge-m3", Provider: "ollama", BaseURL: "http://ollama.test", Dimension: 1024},
		UIURL:     "http://ui.test",
	}
	t.Cleanup(func() {
		testHandler.Knowledge = previous
		ctx := context.Background()
		for _, stmt := range []string{
			`DELETE FROM agent_mcp_server WHERE server_id IN (SELECT id FROM workspace_mcp_server WHERE workspace_id = $1 AND name = 'company-read')`,
			`DELETE FROM workspace_mcp_server WHERE workspace_id = $1 AND name = 'company-read'`,
			`DELETE FROM knowledge_link WHERE workspace_id = $1`,
			`DELETE FROM knowledge_base WHERE workspace_id = $1`,
			`DELETE FROM workspace_knowledge WHERE workspace_id = $1`,
		} {
			if _, err := testPool.Exec(ctx, stmt, testWorkspaceID); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	return fake
}

func callKnowledge(t *testing.T, userID, method string) (int, service.KnowledgeStatus, string) {
	t.Helper()
	w := httptest.NewRecorder()
	path := "/api/knowledge"
	req := newRequestAs(userID, method, path, nil)
	if method == http.MethodPost {
		req = newRequestAs(userID, method, path+"/enable", nil)
		testHandler.EnableKnowledge(w, req)
	} else {
		testHandler.GetKnowledge(w, req)
	}
	var status service.KnowledgeStatus
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return w.Code, status, w.Body.String()
}

func TestKnowledgeStatusWhenNotOffered(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	previous := testHandler.Knowledge
	testHandler.Knowledge = nil
	t.Cleanup(func() { testHandler.Knowledge = previous })

	code, status, body := callKnowledge(t, testUserID, http.MethodGet)
	if code != http.StatusOK || status.Offered || status.Reason != "not_configured" || status.Enabled || status.State != "disabled" {
		t.Fatalf("GET = %d %s", code, body)
	}
	if !strings.Contains(body, `"knowledge_bases":[]`) {
		t.Fatalf("knowledge_bases must be an empty array, got %s", body)
	}
	if code, _, body := callKnowledge(t, testUserID, http.MethodPost); code != http.StatusNotFound {
		t.Fatalf("POST enable = %d %s, want 404", code, body)
	}
}

func TestKnowledgeEnableAndStatus(t *testing.T) {
	fake := useKnowledge(t)

	code, status, body := callKnowledge(t, testUserID, http.MethodGet)
	if code != http.StatusOK || !status.Offered || status.Enabled || status.State != "disabled" {
		t.Fatalf("GET before enable = %d %s", code, body)
	}

	code, status, body = callKnowledge(t, testUserID, http.MethodPost)
	if code != http.StatusOK || !status.Enabled || status.State != "ready" || len(status.KnowledgeBases) != 5 {
		t.Fatalf("POST enable = %d %s", code, body)
	}
	// The JSON contract: snake_case, no secrets.
	for _, field := range []string{`"offered":true`, `"enabled":true`, `"state":"ready"`, `"knowledge_bases":[{"id":`, `"key":"company"`, `"visibility":"workspace"`, `"ui_url":"http://ui.test"`} {
		if !strings.Contains(body, field) {
			t.Errorf("response lacks %s: %s", field, body)
		}
	}
	for _, secret := range []string{"sk-fake", "mcp_token", "ciphertext", "api_key"} {
		if strings.Contains(body, secret) {
			t.Errorf("response leaks %q: %s", secret, body)
		}
	}

	// Enabling again is a no-op, and the library entry exists exactly once.
	if code, _, body := callKnowledge(t, testUserID, http.MethodPost); code != http.StatusOK {
		t.Fatalf("second enable = %d %s", code, body)
	}
	if n := fake.Calls(knowledge.OpCreateMCPEndpoint); n != 1 {
		t.Fatalf("endpoint creates = %d", n)
	}
	var entries int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM workspace_mcp_server WHERE workspace_id = $1 AND name = 'company-read'`, testWorkspaceID).Scan(&entries); err != nil || entries != 1 {
		t.Fatalf("library entries = %d, %v", entries, err)
	}

	// The sidecar going away is a state, not an error.
	fake.SetUnavailable(true)
	code, status, body = callKnowledge(t, testUserID, http.MethodGet)
	if code != http.StatusOK || status.State != "offline" || !status.Enabled {
		t.Fatalf("offline GET = %d %s", code, body)
	}
}

func TestKnowledgeEnableReportsOfflineSidecar(t *testing.T) {
	fake := useKnowledge(t)
	fake.SetUnavailable(true)
	code, _, body := callKnowledge(t, testUserID, http.MethodPost)
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "offline") {
		t.Fatalf("POST enable while down = %d %s, want 503", code, body)
	}
	fake.SetUnavailable(false)
	if code, _, body := callKnowledge(t, testUserID, http.MethodPost); code != http.StatusOK {
		t.Fatalf("retry = %d %s", code, body)
	}
}

func TestKnowledgeIsOwnerAndAdminOnly(t *testing.T) {
	fake := useKnowledge(t)
	member := createTestUserAndMember(t, "member")
	admin := createTestUserAndMember(t, "admin")
	stranger := createTestUserAndMember(t, "")

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		for name, user := range map[string]string{"member": member, "non-member": stranger} {
			if code, _, body := callKnowledge(t, user, method); code != http.StatusForbidden && code != http.StatusNotFound {
				t.Errorf("%s %s = %d %s, want 403 or 404", name, method, code, body)
			}
		}
	}
	if fake.Calls(knowledge.OpEnsureTenant) != 0 {
		t.Fatal("a non-admin triggered provisioning")
	}
	if code, _, body := callKnowledge(t, admin, http.MethodPost); code != http.StatusOK {
		t.Fatalf("admin enable = %d %s", code, body)
	}
	if code, _, body := callKnowledge(t, admin, http.MethodGet); code != http.StatusOK {
		t.Fatalf("admin status = %d %s", code, body)
	}
}

func TestCreateAgentReceivesCompanyReadWhenKnowledgeIsReady(t *testing.T) {
	useKnowledge(t)
	runtimeID := handlerTestRuntimeID(t)

	create := func(name string) string {
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
			"name": name, "runtime_id": runtimeID, "visibility": "private",
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s = %d %s", name, w.Code, w.Body.String())
		}
		var resp AgentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_mcp_server WHERE agent_id = $1`, resp.ID)
			testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID)
		})
		return resp.ID
	}
	bound := func(agentID string) bool {
		var n int
		if err := testPool.QueryRow(context.Background(),
			`SELECT count(*) FROM agent_mcp_server ams JOIN workspace_mcp_server s ON s.id = ams.server_id WHERE ams.agent_id = $1 AND s.name = 'company-read'`, agentID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n > 0
	}

	before := create("knowledge-agent-before")
	if bound(before) {
		t.Fatal("an agent got company-read before knowledge was enabled")
	}
	if code, _, body := callKnowledge(t, testUserID, http.MethodPost); code != http.StatusOK {
		t.Fatalf("enable = %d %s", code, body)
	}
	if !bound(before) {
		t.Fatal("Enable did not assign company-read to an existing agent")
	}
	after := create("knowledge-agent-after")
	if !bound(after) {
		t.Fatal("an agent created after enabling did not receive company-read")
	}
}
