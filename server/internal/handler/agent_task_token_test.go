package handler

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/tasktoken"
	"github.com/multica-ai/multica/server/internal/testutil"
)

const taskTokenTestCatalog = `[
  {"id":"erp","label":"ERP","description":"erp.example.com","env":"BOT_TOKEN_ERP","claims":{"sub":"{{identity.email_local}}"}},
  {"id":"app","label":"APP","env":"BOT_TOKEN_APP","claims":{"sub":"{{identity.email_local}}"}}
]`

// withTaskTokenCatalog installs a catalog-backed issuer for the duration of a
// test and restores whatever was there before.
func withTaskTokenCatalog(t *testing.T, catalog string) {
	t.Helper()
	prev := testHandler.TaskTokenIssuer
	t.Cleanup(func() { testHandler.TaskTokenIssuer = prev })

	if catalog == "" {
		testHandler.TaskTokenIssuer = nil
		return
	}
	iss, err := tasktoken.NewIssuer(catalog, testTaskTokenKeyPEM(t), "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}
	testHandler.TaskTokenIssuer = iss
}

// testTaskTokenKeyPEM generates a throwaway P-256 key in PKCS#8 PEM form.
func testTaskTokenKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// taskTokenRequest builds a request as the given user, carrying the chi URL
// param the handler reads.
func taskTokenRequest(userID, agentID, method string, body any) *http.Request {
	return withURLParam(
		newRequestAs(userID, method, "/api/agents/"+agentID+"/task-tokens", body),
		"id", agentID)
}

// taskTokenAgentActorRequest presents agent-actor credentials. resolveActor
// reports "agent" only when X-Agent-ID and a valid X-Task-ID belonging to that
// agent are both present, so a real task row is required — a member-supplied
// header alone cannot reach this state.
func taskTokenAgentActorRequest(t *testing.T, agentID, method string, body any) *http.Request {
	t.Helper()
	// agent_task_queue_active_requires_runtime: a queued row needs a runtime.
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": handlerTestRuntimeID(t)})
	req := taskTokenRequest(testUserID, agentID, method, body)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func TestGetAgentTaskTokensListsCatalogAndEnabled(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	agentID := dbfx.Agent(t, "task-token-get", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id":             testUserID,
		"task_token_templates": `["erp"]`,
	})

	var out AgentTaskTokensResponse
	testutil.Call(t, testHandler.GetAgentTaskTokens,
		taskTokenRequest(testUserID, agentID, http.MethodGet, nil)).
		Want(http.StatusOK).JSON(&out)

	if len(out.Available) != 2 {
		t.Fatalf("Available = %v, want 2 entries", out.Available)
	}
	if out.Available[0].ID != "erp" || out.Available[0].Env != "BOT_TOKEN_ERP" {
		t.Errorf("Available[0] = %+v, want id=erp env=BOT_TOKEN_ERP", out.Available[0])
	}
	if len(out.Enabled) != 1 || out.Enabled[0] != "erp" {
		t.Errorf("Enabled = %v, want [erp]", out.Enabled)
	}
}

func TestGetAgentTaskTokensEmptyWhenUnconfigured(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, "")
	agentID := dbfx.Agent(t, "task-token-off", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": testUserID,
	})

	var out AgentTaskTokensResponse
	testutil.Call(t, testHandler.GetAgentTaskTokens,
		taskTokenRequest(testUserID, agentID, http.MethodGet, nil)).
		Want(http.StatusOK).JSON(&out)

	if len(out.Available) != 0 {
		t.Errorf("Available = %v, want empty when the feature is unconfigured", out.Available)
	}
}

func TestUpdateAgentTaskTokensPersists(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	agentID := dbfx.Agent(t, "task-token-put", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": testUserID,
	})

	var out AgentTaskTokensResponse
	testutil.Call(t, testHandler.UpdateAgentTaskTokens,
		taskTokenRequest(testUserID, agentID, http.MethodPut, map[string]any{"enabled": []string{"app", "erp"}})).
		Want(http.StatusOK).JSON(&out)

	if len(out.Enabled) != 2 {
		t.Fatalf("Enabled = %v, want 2 entries", out.Enabled)
	}

	// Re-read to prove it persisted rather than echoing the request.
	var reread AgentTaskTokensResponse
	testutil.Call(t, testHandler.GetAgentTaskTokens,
		taskTokenRequest(testUserID, agentID, http.MethodGet, nil)).
		Want(http.StatusOK).JSON(&reread)
	if len(reread.Enabled) != 2 {
		t.Errorf("after re-read Enabled = %v, want 2 entries", reread.Enabled)
	}
}

func TestUpdateAgentTaskTokensRejectsIdOutsideCatalog(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	agentID := dbfx.Agent(t, "task-token-unknown", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": testUserID,
	})

	// This is the boundary that keeps "which scopes may be signed" server-side.
	testutil.Call(t, testHandler.UpdateAgentTaskTokens,
		taskTokenRequest(testUserID, agentID, http.MethodPut, map[string]any{"enabled": []string{"erp", "not-in-catalog"}})).
		Want(http.StatusBadRequest)
}

func TestUpdateAgentTaskTokensClearsWithEmptyList(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	agentID := dbfx.Agent(t, "task-token-clear", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id":             testUserID,
		"task_token_templates": `["erp"]`,
	})

	var out AgentTaskTokensResponse
	testutil.Call(t, testHandler.UpdateAgentTaskTokens,
		taskTokenRequest(testUserID, agentID, http.MethodPut, map[string]any{"enabled": []string{}})).
		Want(http.StatusOK).JSON(&out)

	if len(out.Enabled) != 0 {
		t.Errorf("Enabled = %v, want empty", out.Enabled)
	}
}

func TestAgentTaskTokensDeniesAgentActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	agentID := dbfx.Agent(t, "task-token-actor", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": testUserID,
	})

	// Same rule as agent env (MUL-5438): an agent token must never reach the
	// endpoint that decides which identities its host may be issued.
	testutil.Call(t, testHandler.GetAgentTaskTokens,
		taskTokenAgentActorRequest(t, agentID, http.MethodGet, nil)).
		Want(http.StatusForbidden)
	testutil.Call(t, testHandler.UpdateAgentTaskTokens,
		taskTokenAgentActorRequest(t, agentID, http.MethodPut, map[string]any{"enabled": []string{"erp"}})).
		Want(http.StatusForbidden)
}

func TestAgentTaskTokensDeniesPlainMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, taskTokenTestCatalog)
	agentID := dbfx.Agent(t, "task-token-plain", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": testUserID,
	})

	otherUserID := createPermissionTestMember(t, "task-token-plain@multica.test")
	testutil.Call(t, testHandler.UpdateAgentTaskTokens,
		taskTokenRequest(otherUserID, agentID, http.MethodPut, map[string]any{"enabled": []string{"erp"}})).
		Want(http.StatusForbidden)
}
