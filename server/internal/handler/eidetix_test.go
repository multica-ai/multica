package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// insertTestProject creates a project in the test workspace and registers a
// cleanup that removes it. status defaults to 'planned' at the DB level.
func insertTestProject(t *testing.T, title string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id`,
		testWorkspaceID, title).Scan(&id); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, id) })
	return id
}

// newTestEidetixBox builds a secretbox with a throwaway in-test key. NEVER use
// a real Eidetix token in tests; "fake-token" below is not a secret.
func newTestEidetixBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

func TestEidetixTokenRoundTrip(t *testing.T) {
	box := newTestEidetixBox(t)
	const plain = "fake-token-not-a-secret"

	sealed, err := box.Seal([]byte(plain))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if string(sealed) == plain {
		t.Fatalf("sealed bytes must not equal plaintext")
	}
	opened, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(opened) != plain {
		t.Errorf("round-trip = %q, want %q", opened, plain)
	}
}

func TestSetAndShowEidetixConfig_TokenNeverReturned(t *testing.T) {
	prev := testHandler.EidetixSecrets
	testHandler.EidetixSecrets = newTestEidetixBox(t)
	t.Cleanup(func() { testHandler.EidetixSecrets = prev })

	projectID := insertTestProject(t, "Eidetix Marketing")

	// PUT set
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/projects/"+projectID+"/eidetix", map[string]any{
		"token":       "fake-token-not-a-secret",
		"graph_label": "Marketing",
	})
	req = withURLParam(req, "id", projectID)
	testHandler.SetEidetixConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set: status = %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fake-token-not-a-secret") {
		t.Fatalf("set response leaked the token: %s", w.Body.String())
	}

	// GET show
	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/projects/"+projectID+"/eidetix", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ShowEidetixConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("show: status = %d body=%s", w.Code, w.Body.String())
	}
	var show map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &show); err != nil {
		t.Fatalf("show body not JSON: %v", err)
	}
	if show["configured"] != true {
		t.Errorf("configured = %v, want true", show["configured"])
	}
	if show["enabled"] != true {
		t.Errorf("enabled = %v, want true", show["enabled"])
	}
	if show["graph_label"] != "Marketing" {
		t.Errorf("graph_label = %v, want Marketing", show["graph_label"])
	}
	if _, present := show["token"]; present {
		t.Errorf("show response must never include a token field")
	}
}

// TestEidetixConfigRejectsNonOwner proves the owner/admin write gate actually
// BLOCKS a workspace member who is neither owner nor admin: a plain 'member'
// PUT must return 403, never reaching the handler body.
func TestEidetixConfigRejectsNonOwner(t *testing.T) {
	prev := testHandler.EidetixSecrets
	testHandler.EidetixSecrets = newTestEidetixBox(t)
	t.Cleanup(func() { testHandler.EidetixSecrets = prev })

	projectID := insertTestProject(t, "Eidetix Gate")

	// Create a second user who is only a 'member' of the test workspace.
	// member.user_id REFERENCES "user"(id) ON DELETE CASCADE, so deleting the
	// user removes the member row too.
	var memberUserID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Eidetix Member", "eidetix-member@example.test").Scan(&memberUserID); err != nil {
		t.Fatalf("insert member user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberUserID) })
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, memberUserID); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequestAs(memberUserID, http.MethodPut, "/api/projects/"+projectID+"/eidetix", map[string]any{"token": "fake-token-not-a-secret"})
	req = withURLParam(req, "id", projectID)
	testHandler.SetEidetixConfig(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-owner PUT: status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestDisableThenClearEidetixConfig(t *testing.T) {
	prev := testHandler.EidetixSecrets
	testHandler.EidetixSecrets = newTestEidetixBox(t)
	t.Cleanup(func() { testHandler.EidetixSecrets = prev })

	projectID := insertTestProject(t, "Eidetix Toggle")

	// set first
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/projects/"+projectID+"/eidetix", map[string]any{"token": "fake-token-not-a-secret"})
	req = withURLParam(req, "id", projectID)
	testHandler.SetEidetixConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body.String())
	}

	// PATCH disable
	w = httptest.NewRecorder()
	req = newRequest(http.MethodPatch, "/api/projects/"+projectID+"/eidetix", map[string]any{"enabled": false})
	req = withURLParam(req, "id", projectID)
	testHandler.PatchEidetixConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/projects/"+projectID+"/eidetix", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ShowEidetixConfig(w, req)
	var show map[string]any
	json.Unmarshal(w.Body.Bytes(), &show)
	if show["enabled"] != false {
		t.Errorf("after disable, enabled = %v, want false", show["enabled"])
	}

	// DELETE clear
	w = httptest.NewRecorder()
	req = newRequest(http.MethodDelete, "/api/projects/"+projectID+"/eidetix", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ClearEidetixConfig(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/projects/"+projectID+"/eidetix", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ShowEidetixConfig(w, req)
	json.Unmarshal(w.Body.Bytes(), &show)
	if show["configured"] != false {
		t.Errorf("after clear, configured = %v, want false", show["configured"])
	}
}

func insertEnabledEidetixConfig(t *testing.T, box *secretbox.Box, projectID, token string) {
	t.Helper()
	sealed, err := box.Seal([]byte(token))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO eidetix_project_config (project_id, enabled, endpoint_url, token_encrypted, graph_label)
		VALUES ($1, true, 'https://eidetix.example/mcp/sse', $2, 'Marketing')
		ON CONFLICT (project_id) DO UPDATE SET enabled = true, token_encrypted = EXCLUDED.token_encrypted
	`, projectID, sealed); err != nil {
		t.Fatalf("insert eidetix config: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM eidetix_project_config WHERE project_id = $1`, projectID)
	})
}

type eidetixClaimAgent struct {
	McpConfig json.RawMessage `json:"mcp_config"`
	Skills    []struct {
		Name string `json:"name"`
	} `json:"skills"`
}

func claimAgentForEidetixTest(t *testing.T, runtimeID string) (*eidetixClaimAgent, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "eidetix-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("claim: status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Task struct {
			Agent *eidetixClaimAgent `json:"agent"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	return resp.Task.Agent, w.Body.String()
}

func eidetixAgentHasServer(t *testing.T, agent *eidetixClaimAgent) bool {
	t.Helper()
	if agent == nil || len(agent.McpConfig) == 0 {
		return false
	}
	var mc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(agent.McpConfig, &mc); err != nil {
		t.Fatalf("mcp_config not JSON: %s", agent.McpConfig)
	}
	_, ok := mc.McpServers["eidetix"]
	return ok
}

func eidetixAgentHasSkill(agent *eidetixClaimAgent) bool {
	if agent == nil {
		return false
	}
	for _, s := range agent.Skills {
		if s.Name == "multica-eidetix" {
			return true
		}
	}
	return false
}

func TestClaim_EnabledEidetix_MergesServerAndSkill(t *testing.T) {
	ctx := context.Background()
	box := newTestEidetixBox(t)
	prev := testHandler.EidetixSecrets
	testHandler.EidetixSecrets = box
	t.Cleanup(func() { testHandler.EidetixSecrets = prev })

	projectID := insertTestProject(t, "Eidetix Claim Enabled")
	runtimeID := createClaimReclaimRuntime(t, ctx, "eidetix-rt-on")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "eidetix-on")
	_ = agentID
	if _, err := testPool.Exec(ctx, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID); err != nil {
		t.Fatalf("attach issue to project: %v", err)
	}
	insertEnabledEidetixConfig(t, box, projectID, "fake-token-not-a-secret")
	createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	agent, body := claimAgentForEidetixTest(t, runtimeID)
	if agent == nil {
		t.Fatalf("claim returned no agent: %s", body)
	}
	if !eidetixAgentHasServer(t, agent) {
		t.Errorf("eidetix server not merged into claim mcp_config: %s", agent.McpConfig)
	}
	if !eidetixAgentHasSkill(agent) {
		t.Errorf("multica-eidetix skill not appended; body=%s", body)
	}
}

func TestClaim_DisabledEidetix_NoMergeNoSkill(t *testing.T) {
	ctx := context.Background()
	box := newTestEidetixBox(t)
	prev := testHandler.EidetixSecrets
	testHandler.EidetixSecrets = box
	t.Cleanup(func() { testHandler.EidetixSecrets = prev })

	projectID := insertTestProject(t, "Eidetix Claim Disabled")
	runtimeID := createClaimReclaimRuntime(t, ctx, "eidetix-rt-off")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "eidetix-off")
	_ = agentID
	testPool.Exec(ctx, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID)
	insertEnabledEidetixConfig(t, box, projectID, "fake-token-not-a-secret")
	testPool.Exec(ctx, `UPDATE eidetix_project_config SET enabled = false WHERE project_id = $1`, projectID)
	createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	agent, _ := claimAgentForEidetixTest(t, runtimeID)
	if eidetixAgentHasServer(t, agent) {
		t.Errorf("disabled config still merged eidetix server")
	}
	if eidetixAgentHasSkill(agent) {
		t.Errorf("disabled config still appended the loop skill")
	}
}

func TestClaim_DecryptFailure_FailsOpen(t *testing.T) {
	ctx := context.Background()
	box := newTestEidetixBox(t)
	prev := testHandler.EidetixSecrets
	testHandler.EidetixSecrets = box
	t.Cleanup(func() { testHandler.EidetixSecrets = prev })

	projectID := insertTestProject(t, "Eidetix Claim Garbage")
	runtimeID := createClaimReclaimRuntime(t, ctx, "eidetix-rt-garbage")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "eidetix-garbage")
	_ = agentID
	testPool.Exec(ctx, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID)
	// Insert a row whose token_encrypted is NOT a valid sealed box — Open must fail.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO eidetix_project_config (project_id, enabled, endpoint_url, token_encrypted, graph_label)
		VALUES ($1, true, 'https://eidetix.example/mcp/sse', $2, 'Garbage')
	`, projectID, []byte("not-a-valid-sealed-token")); err != nil {
		t.Fatalf("insert garbage config: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM eidetix_project_config WHERE project_id = $1`, projectID) })
	createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	// The claim must still SUCCEED (200) and simply omit eidetix — fail open.
	agent, body := claimAgentForEidetixTest(t, runtimeID)
	if agent == nil {
		t.Fatalf("decrypt failure must not break the claim; got no agent: %s", body)
	}
	if eidetixAgentHasServer(t, agent) {
		t.Errorf("garbage token must not yield an eidetix server")
	}
	if eidetixAgentHasSkill(agent) {
		t.Errorf("garbage token must not append the loop skill")
	}
}

// TestSetEidetixConfig_TokenRotationPreservesLabelAndEndpoint pins the
// sticky-config behavior: re-running `set` with only a token (the rotation
// case) must NOT wipe a previously-configured graph_label or custom endpoint.
func TestSetEidetixConfig_TokenRotationPreservesLabelAndEndpoint(t *testing.T) {
	prev := testHandler.EidetixSecrets
	testHandler.EidetixSecrets = newTestEidetixBox(t)
	t.Cleanup(func() { testHandler.EidetixSecrets = prev })

	projectID := insertTestProject(t, "Eidetix Rotate")

	// Initial set with a custom endpoint + label.
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/projects/"+projectID+"/eidetix", map[string]any{
		"token":        "fake-token-1",
		"endpoint_url": "https://custom.example/mcp/sse",
		"graph_label":  "Marketing",
	})
	req = withURLParam(req, "id", projectID)
	testHandler.SetEidetixConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("initial set: %d %s", w.Code, w.Body.String())
	}

	// Rotate the token only — no endpoint, no label supplied.
	w = httptest.NewRecorder()
	req = newRequest(http.MethodPut, "/api/projects/"+projectID+"/eidetix", map[string]any{
		"token": "fake-token-2",
	})
	req = withURLParam(req, "id", projectID)
	testHandler.SetEidetixConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate set: %d %s", w.Code, w.Body.String())
	}
	var show map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &show); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	if show["graph_label"] != "Marketing" {
		t.Errorf("after token rotation, graph_label = %v, want Marketing (must be sticky)", show["graph_label"])
	}
	if show["endpoint_url"] != "https://custom.example/mcp/sse" {
		t.Errorf("after token rotation, endpoint_url = %v, want the custom endpoint (must be sticky)", show["endpoint_url"])
	}
}
