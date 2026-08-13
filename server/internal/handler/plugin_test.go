package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil/plugintest"
	"github.com/multica-ai/multica/server/internal/util"
)

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

	release, err := plugintest.ReviewReadinessRelease()
	if err != nil {
		t.Fatalf("reference release: %v", err)
	}
	installed, err := testHandler.PluginService.InstallPluginRelease(
		context.Background(),
		util.MustParseUUID(testWorkspaceID),
		util.MustParseUUID(testUserID),
		service.PluginReleasePublication{Release: release, PublisherType: "official", TrustTier: "official"},
	)
	if err != nil {
		t.Fatalf("install reference release: %v", err)
	}

	params := map[string]string{"id": testWorkspaceID, "installationId": uuidToString(installed.ID)}
	recorder := httptest.NewRecorder()
	testHandler.EnablePlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/enable", nil, params))
	if recorder.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.ListPlugins(recorder, pluginHandlerRequest(http.MethodGet, "/plugins", nil, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(plugintest.ReviewReadinessPluginKey)) {
		t.Fatalf("list status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.DisablePlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/disable", nil, params))
	if recorder.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.RollbackPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/rollback", []byte(`{"version":"1.0.0"}`), params))
	if recorder.Code != http.StatusOK {
		t.Fatalf("rollback status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
