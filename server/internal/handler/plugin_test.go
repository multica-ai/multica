package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil/plugintest"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
)

type failPluginDetailQueryDB struct {
	db.DBTX
	queryFragment       string
	queryRowFragment    string
	failQueryRowAtMatch int
	queryRowMatches     int
}

func (database *failPluginDetailQueryDB) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if database.queryFragment != "" && strings.Contains(query, database.queryFragment) {
		return nil, errors.New("forced Plugin detail query failure")
	}
	return database.DBTX.Query(ctx, query, args...)
}

type pluginDetailErrorRow struct{}

func (pluginDetailErrorRow) Scan(...any) error {
	return errors.New("forced Plugin detail query failure")
}

func (database *failPluginDetailQueryDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if database.queryRowFragment != "" && strings.Contains(query, database.queryRowFragment) {
		database.queryRowMatches++
		if database.queryRowMatches == database.failQueryRowAtMatch {
			return pluginDetailErrorRow{}
		}
	}
	return database.DBTX.QueryRow(ctx, query, args...)
}

func pluginHandlerRequest(method, path string, body []byte, params map[string]string) *http.Request {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("X-User-ID", testUserID)
	request.Header.Set("Content-Type", "application/json")
	routeContext := chi.NewRouteContext()
	for key, value := range params {
		routeContext.URLParams.Add(key, value)
	}
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func TestRemoteMCPConfigResponseReady(t *testing.T) {
	ready := pluginRemoteMCPConfigResponse{
		ConfigRevision: 1, CredentialState: "configured", Reviewed: true,
		ApprovedTools: []pluginruntime.RemoteMCPTool{{Name: "search"}}, SchemaDigest: "sha256:test",
	}
	if !remoteMCPConfigResponseReady(ready) {
		t.Fatal("complete Remote MCP configuration was not ready")
	}
	for name, edit := range map[string]func(*pluginRemoteMCPConfigResponse){
		"missing config":     func(config *pluginRemoteMCPConfigResponse) { config.ConfigRevision = 0 },
		"missing credential": func(config *pluginRemoteMCPConfigResponse) { config.CredentialState = "missing" },
		"unreviewed":         func(config *pluginRemoteMCPConfigResponse) { config.Reviewed = false },
		"no approved tools":  func(config *pluginRemoteMCPConfigResponse) { config.ApprovedTools = nil },
		"missing digest":     func(config *pluginRemoteMCPConfigResponse) { config.SchemaDigest = "" },
	} {
		t.Run(name, func(t *testing.T) {
			config := ready
			edit(&config)
			if remoteMCPConfigResponseReady(config) {
				t.Fatal("incomplete Remote MCP configuration was ready")
			}
		})
	}
}

func TestPluginHTTPLifecycleForInstalledReferenceRelease(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanup := func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM plugin_health WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_capability_snapshot WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_workspace_capability_state WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_binding WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_grant WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE workspace_id = $1 AND action = 'plugin_uninstalled'`, testWorkspaceID)
	}
	cleanup()
	t.Cleanup(cleanup)

	recorder := httptest.NewRecorder()
	testHandler.ListPluginCatalog(recorder, pluginHandlerRequest(http.MethodGet, "/plugins/catalog", nil, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"signature_verified":true`)) || !bytes.Contains(recorder.Body.Bytes(), []byte(plugintest.ReviewReadinessPluginKey)) {
		t.Fatalf("catalog status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var catalogPayload struct {
		Diagnostics json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &catalogPayload); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if !bytes.Equal(catalogPayload.Diagnostics, []byte(`[]`)) {
		t.Fatalf("empty catalog diagnostics must be an array, got %s", catalogPayload.Diagnostics)
	}

	recorder = httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(
		http.MethodPost,
		"/plugins/install",
		[]byte(`{"plugin_key":"ai.multica.software-delivery","version":"1.0.0"}`),
		map[string]string{"id": testWorkspaceID},
	))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed pluginInstallationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	if installed.Enabled || installed.LifecycleStatus != "installed" {
		t.Fatalf("install must remain disabled: %+v", installed)
	}

	params := map[string]string{"id": testWorkspaceID, "installationId": installed.ID}
	recorder = httptest.NewRecorder()
	testHandler.EnablePlugin(recorder, pluginHandlerRequest(
		http.MethodPost,
		"/plugins/enable",
		[]byte(`{"scope_type":"workspace","scope_id":"00000000-0000-0000-0000-000000000001"}`),
		params,
	))
	if recorder.Code != http.StatusNotFound || bytes.Contains(recorder.Body.Bytes(), []byte("no rows")) {
		t.Fatalf("cross-workspace binding status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.EnablePlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/enable", nil, params))
	if recorder.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.ListPlugins(recorder, pluginHandlerRequest(http.MethodGet, "/plugins", nil, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(plugintest.ReviewReadinessPluginKey)) {
		t.Fatalf("list status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	for _, test := range []struct {
		name                string
		queryFragment       string
		queryRowFragment    string
		failQueryRowAtMatch int
	}{
		{name: "contributions", queryFragment: "FROM plugin_contribution"},
		{name: "bindings", queryFragment: "FROM plugin_binding"},
		{name: "available releases", queryFragment: "FROM plugin_release release"},
		{name: "active release", queryRowFragment: "FROM plugin_release\nWHERE id = $1", failQueryRowAtMatch: 2},
	} {
		t.Run("list fails closed when "+test.name+" cannot be read", func(t *testing.T) {
			failingHandler := *testHandler
			failingHandler.Queries = db.New(&failPluginDetailQueryDB{
				DBTX:                testPool,
				queryFragment:       test.queryFragment,
				queryRowFragment:    test.queryRowFragment,
				failQueryRowAtMatch: test.failQueryRowAtMatch,
			})
			recorder := httptest.NewRecorder()
			failingHandler.ListPlugins(recorder, pluginHandlerRequest(http.MethodGet, "/plugins", nil, map[string]string{"id": testWorkspaceID}))
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}

	recorder = httptest.NewRecorder()
	testHandler.UpgradePlugin(recorder, pluginHandlerRequest(
		http.MethodPost,
		"/plugins/upgrade",
		[]byte(`{"plugin_key":"ai.multica.software-delivery","version":"1.1.0"}`),
		params,
	))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"desired_version":"1.1.0"`)) {
		t.Fatalf("upgrade status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.UpgradePlugin(recorder, pluginHandlerRequest(
		http.MethodPost,
		"/plugins/upgrade",
		[]byte(`{"plugin_key":"ai.multica.software-delivery","version":"1.0.0"}`),
		params,
	))
	if recorder.Code != http.StatusConflict || !bytes.Contains(recorder.Body.Bytes(), []byte("not newer")) {
		t.Fatalf("downgrade through upgrade status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.DisablePlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/disable", nil, params))
	if recorder.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.RollbackPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/rollback", []byte(`{"version":"1.0.0"}`), params))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"desired_version":"1.0.0"`)) {
		t.Fatalf("rollback status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.RollbackPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/rollback", []byte(`{"version":"9.9.9"}`), params))
	if recorder.Code != http.StatusNotFound || bytes.Contains(recorder.Body.Bytes(), []byte("no rows")) {
		t.Fatalf("safe missing rollback status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.UninstallPlugin(recorder, pluginHandlerRequest(http.MethodDelete, "/plugins/"+installed.ID, nil, params))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("official uninstall status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var uninstallAuditCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM activity_log
		WHERE workspace_id = $1 AND action = 'plugin_uninstalled'
	`, testWorkspaceID).Scan(&uninstallAuditCount); err != nil || uninstallAuditCount != 1 {
		t.Fatalf("official uninstall audit count=%d err=%v", uninstallAuditCount, err)
	}
}

func TestPluginManagementUnavailableWhenFlagDisabled(t *testing.T) {
	h := &Handler{}
	request := func() *http.Request {
		return pluginHandlerRequest(http.MethodGet, "/plugins", nil, map[string]string{
			"id":             testWorkspaceID,
			"installationId": "00000000-0000-0000-0000-000000000001",
			"pluginKey":      plugintest.ReviewReadinessPluginKey,
			"pluginRef":      plugintest.ReviewReadinessPluginKey,
		})
	}

	for name, handler := range map[string]http.HandlerFunc{
		"list installed":        h.ListPlugins,
		"list catalog":          h.ListPluginCatalog,
		"catalog detail":        h.GetPluginCatalogRelease,
		"install":               h.InstallPlugin,
		"upgrade":               h.UpgradePlugin,
		"enable":                h.EnablePlugin,
		"disable":               h.DisablePlugin,
		"rollback":              h.RollbackPlugin,
		"private list":          h.ListPrivatePlugins,
		"private status":        h.GetPrivatePluginStatus,
		"private install":       h.InstallPrivatePlugin,
		"uninstall":             h.UninstallPlugin,
		"remote oauth start":    h.StartPluginRemoteMCPOAuth,
		"remote oauth callback": h.CompletePluginRemoteMCPOAuth,
		"remote configure":      h.ConfigurePluginRemoteMCP,
		"remote test":           h.TestPluginRemoteMCP,
		"remote review":         h.ReviewPluginRemoteMCPTools,
		"remote revoke":         h.RevokePluginRemoteMCPCredential,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler(recorder, request())
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if bytes.Contains(recorder.Body.Bytes(), []byte(plugintest.ReviewReadinessPluginKey)) {
				t.Fatalf("disabled response leaked catalog data: %s", recorder.Body.String())
			}
		})
	}
}

func TestPrivatePluginManagementRequiresPluginsV1(t *testing.T) {
	h := &Handler{}
	withPluginsV1Flag(t, h, false)
	for name, handler := range map[string]http.HandlerFunc{
		"list":    h.ListPrivatePlugins,
		"status":  h.GetPrivatePluginStatus,
		"install": h.InstallPrivatePlugin,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler(recorder, pluginHandlerRequest(http.MethodGet, "/plugins/private", nil, map[string]string{"id": testWorkspaceID}))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestPrivatePluginHTTPUploadListAndUninstall(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanup := func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM plugin_health WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_capability_snapshot WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_workspace_capability_state WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_binding WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_grant WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_contribution WHERE release_id IN (SELECT id FROM plugin_release WHERE plugin_id IN (SELECT id FROM plugin_identity WHERE owner_workspace_id = $1))`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_artifact_file WHERE release_id IN (SELECT id FROM plugin_release WHERE plugin_id IN (SELECT id FROM plugin_identity WHERE owner_workspace_id = $1))`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_release WHERE plugin_id IN (SELECT id FROM plugin_identity WHERE owner_workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_identity WHERE owner_workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE workspace_id = $1 AND action LIKE 'plugin_private_%'`, testWorkspaceID)
	}
	cleanup()
	t.Cleanup(cleanup)

	archive, _, err := plugincontract.PackDirectory(filepath.Join("..", "..", "..", "examples", "plugins", "incident-triage"))
	if err != nil {
		t.Fatalf("pack example private Plugin: %v", err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("artifact", "incident-triage.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := pluginHandlerRequest(http.MethodPost, "/plugins/private/install", body.Bytes(), map[string]string{"id": testWorkspaceID})
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	testHandler.InstallPrivatePlugin(recorder, request)
	if recorder.Code != http.StatusCreated || !bytes.Contains(recorder.Body.Bytes(), []byte(`"trust_tier":"private_dev"`)) || !bytes.Contains(recorder.Body.Bytes(), []byte(`"signature_verified":false`)) {
		t.Fatalf("private install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed pluginInstallationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatal(err)
	}

	recorder = httptest.NewRecorder()
	testHandler.ListPrivatePlugins(recorder, pluginHandlerRequest(http.MethodGet, "/plugins/private", nil, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte("dev.multica.incident-triage")) {
		t.Fatalf("private list status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	testHandler.GetPrivatePluginStatus(recorder, pluginHandlerRequest(http.MethodGet, "/plugins/private/dev.multica.incident-triage", nil, map[string]string{
		"id": testWorkspaceID, "pluginRef": "dev.multica.incident-triage",
	}))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"desired_version":"0.1.0"`)) {
		t.Fatalf("private status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	params := map[string]string{"id": testWorkspaceID, "installationId": installed.ID}
	recorder = httptest.NewRecorder()
	testHandler.EnablePlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/enable", nil, params))
	if recorder.Code != http.StatusOK {
		t.Fatalf("private enable status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	withPluginsV1Flag(t, testHandler, false)
	recorder = httptest.NewRecorder()
	testHandler.ListPlugins(recorder, pluginHandlerRequest(http.MethodGet, "/plugins", nil, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("Plugin list with plugins_v1 off status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	testHandler.EnablePlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/enable", nil, params))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("Plugin enable with plugins_v1 off status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	testHandler.DisablePlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/disable", nil, params))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("Plugin disable with plugins_v1 off status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	testHandler.UninstallPlugin(recorder, pluginHandlerRequest(http.MethodDelete, "/plugins/"+installed.ID, nil, map[string]string{
		"id": testWorkspaceID, "installationId": installed.ID,
	}))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("Plugin uninstall with plugins_v1 off status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	withPluginsV1Flag(t, testHandler, true)
	recorder = httptest.NewRecorder()
	testHandler.UninstallPlugin(recorder, pluginHandlerRequest(http.MethodDelete, "/plugins/"+installed.ID, nil, map[string]string{
		"id": testWorkspaceID, "installationId": installed.ID,
	}))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("private uninstall status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	testHandler.GetPrivatePluginStatus(recorder, pluginHandlerRequest(http.MethodGet, "/plugins/private/dev.multica.incident-triage", nil, map[string]string{
		"id": testWorkspaceID, "pluginRef": "dev.multica.incident-triage",
	}))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("uninstalled private status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestResolveTaskRemoteMCPCredentialRejectsNonDaemonAuth(t *testing.T) {
	h := &Handler{}
	recorder := httptest.NewRecorder()
	request := pluginHandlerRequest(http.MethodGet, "/api/daemon/tasks/task-id/remote-mcp/contribution-id/credential", nil, map[string]string{
		"taskId":         "task-id",
		"contributionId": "contribution-id",
	})

	h.ResolveTaskRemoteMCPCredential(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestResolveTaskRemoteMCPCredentialRejectsDifferentDaemonInWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Remote MCP daemon isolation")
	request := newDaemonTokenRequest(http.MethodGet,
		"/api/daemon/tasks/"+fixture.taskID+"/remote-mcp/contribution-id/credential",
		nil, testWorkspaceID, "different-daemon")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("taskId", fixture.taskID)
	routeCtx.URLParams.Add("contributionId", "contribution-id")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeCtx))

	recorder := httptest.NewRecorder()
	testHandler.ResolveTaskRemoteMCPCredential(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
