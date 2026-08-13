package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil/plugintest"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
}

func TestPluginManagementUnavailableWhenFlagDisabled(t *testing.T) {
	h := &Handler{}
	request := func() *http.Request {
		return pluginHandlerRequest(http.MethodGet, "/plugins", nil, map[string]string{
			"id":             testWorkspaceID,
			"installationId": "00000000-0000-0000-0000-000000000001",
			"pluginKey":      plugintest.ReviewReadinessPluginKey,
		})
	}

	for name, handler := range map[string]http.HandlerFunc{
		"list installed": h.ListPlugins,
		"list catalog":   h.ListPluginCatalog,
		"catalog detail": h.GetPluginCatalogRelease,
		"install":        h.InstallPlugin,
		"upgrade":        h.UpgradePlugin,
		"enable":         h.EnablePlugin,
		"disable":        h.DisablePlugin,
		"rollback":       h.RollbackPlugin,
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
