package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestShip_ListDeployAdapters_ReturnsBuiltins verifies the registry-
// fed listing endpoint exposes every built-in adapter so the dropdown
// in the env-config dialog can populate without hardcoding the list.
func TestShip_ListDeployAdapters_ReturnsBuiltins(t *testing.T) {
	enableShipHub(t, false)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/deploy/adapters", nil)
	testHandler.ListDeployAdapters(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Adapters []map[string]any `json:"adapters"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]bool{
		"vercel":          false,
		"cloudflare":      false,
		"fly":             false,
		"render":          false,
		"github_actions":  false,
		"generic_webhook": false,
	}
	for _, a := range resp.Adapters {
		kind, _ := a["kind"].(string)
		if _, ok := want[kind]; ok {
			want[kind] = true
		}
		// webhook_url should be populated.
		if url, _ := a["webhook_url"].(string); !strings.Contains(url, "/api/integrations/deploy/"+kind+"/webhook") {
			t.Errorf("adapter %s: webhook_url unexpected: %q", kind, url)
		}
	}
	for kind, found := range want {
		if !found {
			t.Errorf("expected adapter %s in response", kind)
		}
	}
}

// TestShip_DeployAdapterPhase6Migration verifies the column added in
// migration 084 is present, so subsequent tests in this package can
// rely on it. Acts as a guard against running the suite against a
// stale schema.
func TestShip_DeployAdapterPhase6Migration(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	var exists bool
	err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'deploy_environment' AND column_name = 'adapter_kind'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("probe migration: %v", err)
	}
	if !exists {
		t.Skip("phase 6 migration (084) not yet applied; skipping deploy-adapter tests")
	}
	var tableExists bool
	err = testPool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables WHERE table_name = 'deploy_adapter_config'
		)`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("probe deploy_adapter_config: %v", err)
	}
	if !tableExists {
		t.Skip("phase 6 deploy_adapter_config table missing; skipping")
	}
}

// TestShip_ConfigureDeployAdapter_RejectsUnknownKind ensures the
// handler validates the kind against the registry before persisting —
// an unknown adapter would persist garbage and confuse the webhook
// receiver later.
func TestShip_ConfigureDeployAdapter_RejectsUnknownKind(t *testing.T) {
	enableShipHub(t, false)
	if !phase6Applied(t) {
		t.Skip("phase 6 migration not applied")
	}
	projectID := createShipProject(t, "https://github.com/multica-ai/test-repo-adapter")

	// Create an env so we have one to configure.
	envID := createDeployEnv(t, projectID, "staging", "Staging")

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/deploy_environments/"+envID+"/adapter", map[string]any{
		"adapter_kind": "nonsense",
		"config":       map[string]any{},
	})
	req = withURLParam(req, "id", envID)
	testHandler.ConfigureDeployAdapter(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for unknown adapter, got %d: %s", w.Code, w.Body.String())
	}
}

// TestShip_ConfigureDeployAdapter_PersistsAndExposesWebhookURL — the
// happy path: configure vercel, then verify GET /adapters returns
// webhook_secret_set: true (server says we have one).
func TestShip_ConfigureDeployAdapter_PersistsAndExposesWebhookURL(t *testing.T) {
	enableShipHub(t, false)
	if !phase6Applied(t) {
		t.Skip("phase 6 migration not applied")
	}
	projectID := createShipProject(t, "https://github.com/multica-ai/test-repo-vercel")
	envID := createDeployEnv(t, projectID, "production", "Production")

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/deploy_environments/"+envID+"/adapter", map[string]any{
		"adapter_kind": "vercel",
		"config": map[string]any{
			"team_id":    "team_abc",
			"project_id": "prj_xyz",
			"token":      "vc_secret",
		},
		"webhook_secret": "wh_secret_value",
	})
	req = withURLParam(req, "id", envID)
	testHandler.ConfigureDeployAdapter(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := resp["adapter_kind"].(string); got != "vercel" {
		t.Errorf("adapter_kind: want vercel, got %q", got)
	}
	if got, _ := resp["webhook_secret_set"].(bool); !got {
		t.Errorf("webhook_secret_set: want true")
	}
	// The deploy_environment.adapter_kind should now be 'vercel'.
	var stored string
	err := testPool.QueryRow(context.Background(),
		`SELECT adapter_kind FROM deploy_environment WHERE id = $1`, envID).Scan(&stored)
	if err != nil {
		t.Fatalf("select adapter_kind: %v", err)
	}
	if stored != "vercel" {
		t.Errorf("DB adapter_kind: want vercel, got %q", stored)
	}
}

// TestShip_PollDeployEnvironment_RejectsNonPollAdapter — generic_webhook
// declines polling; the handler must surface that as a 400 rather than
// returning a misleading "no current state" payload.
func TestShip_PollDeployEnvironment_RejectsNonPollAdapter(t *testing.T) {
	enableShipHub(t, false)
	if !phase6Applied(t) {
		t.Skip("phase 6 migration not applied")
	}
	projectID := createShipProject(t, "https://github.com/multica-ai/test-repo-generic")
	envID := createDeployEnv(t, projectID, "staging", "Staging")

	// Configure as generic_webhook so SupportsPoll returns false.
	cw := httptest.NewRecorder()
	cReq := newRequest("PUT", "/api/deploy_environments/"+envID+"/adapter", map[string]any{
		"adapter_kind":   "generic_webhook",
		"config":         map[string]any{"status_path": "s", "sha_path": "sha"},
		"webhook_secret": "abc",
	})
	cReq = withURLParam(cReq, "id", envID)
	testHandler.ConfigureDeployAdapter(cw, cReq)
	if cw.Code != 200 {
		t.Fatalf("setup configure: %d %s", cw.Code, cw.Body.String())
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/deploy_environments/"+envID+"/poll_now", nil)
	req = withURLParam(req, "id", envID)
	testHandler.PollDeployEnvironment(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 (poll not supported), got %d: %s", w.Code, w.Body.String())
	}
}

// phase6Applied returns true when migration 084 (deploy_adapter_config
// table) has been applied. Skip-guard for tests that exercise the new
// schema.
func phase6Applied(t *testing.T) bool {
	t.Helper()
	var ok bool
	err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables WHERE table_name = 'deploy_adapter_config'
		)`).Scan(&ok)
	if err != nil {
		t.Fatalf("probe phase 6: %v", err)
	}
	return ok
}

// createDeployEnv is a tiny test helper that inserts a deploy
// environment row directly. We bypass the handler so the test focuses
// on the adapter endpoint under test.
func createDeployEnv(t *testing.T, projectID, kind, name string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO deploy_environment (workspace_id, project_id, kind, name, target_branch)
		VALUES ($1, $2, $3, $4, 'main')
		RETURNING id
	`, testWorkspaceID, projectID, kind, name).Scan(&id)
	if err != nil {
		t.Fatalf("insert deploy env: %v", err)
	}
	return id
}
