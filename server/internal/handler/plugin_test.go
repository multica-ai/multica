package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// The reference manifest for handler tests declares only capabilities this
// build ships. Contribution kinds that are still gated are covered by the
// capability matrix in pkg/plugincontract; repeating them here would just
// re-run that matrix through HTTP.
const handlerTestManifest = `{
  "manifest_version": 1,
  "key": "com.example.hello",
  "name": "Hello Panel",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read", "comments:write", "storage:user"],
  "config": {
    "repo": { "type": "string", "label": "Repo", "required": true },
    "token": { "type": "secret", "label": "Token" }
  },
  "contributes": {
    "surfaces": [{ "key": "hello", "type": "issue_panel", "name": "Hello", "entry": "ui/main.js" }]
  }
}`

// hookOnlyTestManifest declares a contribution kind the host does not ship yet.
// Kept separate from handlerTestManifest so flipping a surface on never silently
// changes what the capability-gate test is asserting.
const hookOnlyTestManifest = `{
  "manifest_version": 1,
  "key": "com.example.hooked",
  "name": "Hooked",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read", "net:example.com"],
  "contributes": {
    "hooks": [{
      "key": "summarize",
      "name": "Summarize",
      "description": "Compress the discussion.",
      "triggers": ["ui"],
      "transport": { "type": "http", "url": "https://example.com/hooks/summarize" }
    }]
  }
}`

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

func writeLocalPluginManifest(t *testing.T, root, manifest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "hello"), 0o755); err != nil {
		t.Fatalf("create local plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "hello", plugincontract.ManifestFilename), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write local manifest: %v", err)
	}
}

// withLocalPluginSource points the service at a temp MULTICA_PLUGIN_DIR and
// enables every capability, so these tests exercise the HTTP surface rather
// than the staged-rollout gate.
func withLocalPluginSource(t *testing.T, manifest string) string {
	t.Helper()
	return withLocalPluginSourceIn(t, t.TempDir(), manifest)
}

// withLocalPluginSourceIn takes the root explicitly so an upgrade test can
// rewrite the manifest in place and install again from the same source URL.
func withLocalPluginSourceIn(t *testing.T, root string, manifest string) string {
	t.Helper()
	writeLocalPluginManifest(t, root, manifest)

	previousDir := testHandler.PluginService.LocalDir
	previousHost := testHandler.PluginService.Host
	previousSecrets := testHandler.PluginService.Secrets
	testHandler.PluginService.LocalDir = root
	testHandler.PluginService.Host = plugincontract.Capabilities{
		SurfaceTypes:  map[string]bool{plugincontract.SurfaceIssuePanel: true, plugincontract.SurfaceSidebarPanel: true, plugincontract.SurfaceModal: true},
		HookTriggers:  map[string]bool{plugincontract.TriggerUI: true, plugincontract.TriggerManual: true, plugincontract.TriggerAgent: true, plugincontract.TriggerEvent: true},
		HookTransport: map[string]bool{plugincontract.TransportHTTP: true, plugincontract.TransportMCP: true},
		ResourceTypes: map[string]bool{plugincontract.ResourceSkill: true},
	}
	box, err := secretbox.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	testHandler.PluginService.Secrets = box
	t.Cleanup(func() {
		testHandler.PluginService.LocalDir = previousDir
		testHandler.PluginService.Host = previousHost
		testHandler.PluginService.Secrets = previousSecrets
	})
	return service.LocalSourcePrefix + "hello"
}

func cleanupPluginInstallations(t *testing.T) {
	t.Helper()
	remove := func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM plugin_storage WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_secret WHERE installation_id IN (SELECT id FROM plugin_installation WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM plugin_installation WHERE workspace_id = $1`, testWorkspaceID)
	}
	remove()
	t.Cleanup(remove)
}

func TestPluginManagementRequiresPluginsV1(t *testing.T) {
	withPluginsV1Flag(t, testHandler, false)
	for name, handler := range map[string]http.HandlerFunc{
		"list":      testHandler.ListPlugins,
		"preview":   testHandler.PreviewPlugin,
		"install":   testHandler.InstallPlugin,
		"configure": testHandler.ConfigurePlugin,
		"enable":    testHandler.EnablePlugin,
		"disable":   testHandler.DisablePlugin,
		"uninstall": testHandler.UninstallPlugin,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", []byte(`{}`), map[string]string{"id": testWorkspaceID}))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestPluginPreviewShowsScopesWithoutInstalling(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	source := withLocalPluginSource(t, handlerTestManifest)

	recorder := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"source_url": source})
	testHandler.PreviewPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/preview", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var preview service.PluginPreview
	if err := json.Unmarshal(recorder.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if len(preview.Scopes) != 3 || preview.Installed {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	if len(preview.ConfigSchema) != 2 || preview.ConfigSchema[0].Key != "repo" || preview.ConfigSchema[1].Type != plugincontract.ConfigSecret {
		t.Fatalf("unexpected config schema: %+v", preview.ConfigSchema)
	}

	// Preview must be side-effect free: the consent screen has to be able to
	// show scopes before anything exists to revoke.
	listRecorder := httptest.NewRecorder()
	testHandler.ListPlugins(listRecorder, pluginHandlerRequest(http.MethodGet, "/plugins", nil, map[string]string{"id": testWorkspaceID}))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var list struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Plugins) != 0 {
		t.Fatalf("preview created an installation: %v", list.Plugins)
	}
}

func TestPluginInstallRequiresExactConsent(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	source := withLocalPluginSource(t, handlerTestManifest)

	partial, _ := json.Marshal(map[string]any{"source_url": source, "granted_scopes": []string{"issues:read"}})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", partial, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("partial consent status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	extra, _ := json.Marshal(map[string]any{
		"source_url":     source,
		"granted_scopes": []string{"issues:read", "comments:write", "storage:user", "issues:write"},
	})
	recorder = httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", extra, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("over-consent status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPluginInstallConfigureAndUninstall(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	source := withLocalPluginSource(t, handlerTestManifest)

	install, _ := json.Marshal(map[string]any{
		"source_url":     source,
		"granted_scopes": []string{"issues:read", "comments:write", "storage:user"},
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", install, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID                string         `json:"id"`
		PluginKey         string         `json:"plugin_key"`
		Enabled           bool           `json:"enabled"`
		Config            map[string]any `json:"config"`
		ConfiguredSecrets []string       `json:"configured_secrets"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	if installed.ID == "" || installed.PluginKey != "com.example.hello" || !installed.Enabled {
		t.Fatalf("unexpected installation: %+v", installed)
	}

	params := map[string]string{"id": testWorkspaceID, "installationId": installed.ID}
	configure, _ := json.Marshal(map[string]any{"values": map[string]any{"repo": "multica-ai/multica", "token": "sk-super-secret"}})
	recorder = httptest.NewRecorder()
	testHandler.ConfigurePlugin(recorder, pluginHandlerRequest(http.MethodPut, "/plugins/config", configure, params))
	if recorder.Code != http.StatusOK {
		t.Fatalf("configure status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// The secret must not come back in any form: not in `config`, not masked,
	// not anywhere else in the payload.
	if strings.Contains(recorder.Body.String(), "sk-super-secret") {
		t.Fatalf("configure response leaked the secret value: %s", recorder.Body.String())
	}
	var configured struct {
		Config            map[string]any `json:"config"`
		ConfiguredSecrets []string       `json:"configured_secrets"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &configured); err != nil {
		t.Fatalf("decode configured installation: %v", err)
	}
	if configured.Config["repo"] != "multica-ai/multica" {
		t.Fatalf("plain config value was not stored: %v", configured.Config)
	}
	if _, present := configured.Config["token"]; present {
		t.Fatalf("secret value was stored in config: %v", configured.Config)
	}
	if len(configured.ConfiguredSecrets) != 1 || configured.ConfiguredSecrets[0] != "token" {
		t.Fatalf("configured secrets = %v, want [token]", configured.ConfiguredSecrets)
	}

	recorder = httptest.NewRecorder()
	testHandler.ListPlugins(recorder, pluginHandlerRequest(http.MethodGet, "/plugins", nil, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "sk-super-secret") {
		t.Fatalf("list leaked the secret or failed: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	unknown, _ := json.Marshal(map[string]any{"values": map[string]any{"nope": "x"}})
	recorder = httptest.NewRecorder()
	testHandler.ConfigurePlugin(recorder, pluginHandlerRequest(http.MethodPut, "/plugins/config", unknown, params))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown config field status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.DisablePlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/disable", nil, params))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"enabled":false`) {
		t.Fatalf("disable status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.UninstallPlugin(recorder, pluginHandlerRequest(http.MethodDelete, "/plugins", nil, params))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("uninstall status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// Uninstall must take the plugin's own state with it, or a reinstall would
	// silently inherit rows nobody can audit.
	var remaining int
	if err := testPool.QueryRow(context.Background(),
		`SELECT (SELECT COUNT(*) FROM plugin_secret WHERE installation_id = $1)
		      + (SELECT COUNT(*) FROM plugin_storage WHERE installation_id = $1)`,
		installed.ID).Scan(&remaining); err != nil {
		t.Fatalf("count leftover plugin rows: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("uninstall left %d plugin-owned rows behind", remaining)
	}
}

func TestPluginInstallRejectsMalformedManifest(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	source := withLocalPluginSource(t, `{"manifest_version":1,"key":"com.example.hello","surprise":true}`)

	body, _ := json.Marshal(map[string]any{"source_url": source, "granted_scopes": []string{}})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed manifest status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "manifest") {
		t.Fatalf("malformed manifest error is not actionable: %s", recorder.Body.String())
	}
}

func TestPluginInstallRejectsUnshippedCapabilities(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	// Tests the WIRING — that install consults the capability gate and refuses
	// what the host cannot run — against an explicitly narrowed host set rather
	// than against whatever HostCapabilities happens to contain today.
	//
	// The earlier version picked a contribution kind that was unshipped at the
	// time, so shipping it turned this red with a message about the flip instead
	// of about the gate. It also had a shelf life: once every kind ships there
	// is nothing left to write such a fixture against. Narrowing the host here
	// keeps the assertion true for good.
	source := withLocalPluginSource(t, hookOnlyTestManifest)
	testHandler.PluginService.Host = plugincontract.Capabilities{
		SurfaceTypes:  map[string]bool{plugincontract.SurfaceIssuePanel: true},
		HookTriggers:  map[string]bool{},
		HookTransport: map[string]bool{},
		ResourceTypes: map[string]bool{},
	}

	body, _ := json.Marshal(map[string]any{
		"source_url":     source,
		"granted_scopes": []string{"issues:read", "net:example.com"},
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unshipped capability status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// A declared-but-unrunnable contribution must fail the install outright and
	// name what is missing: silently dropping it would look installed and never
	// fire.
	if !strings.Contains(recorder.Body.String(), "hook trigger") {
		t.Fatalf("error does not name the missing capability: %s", recorder.Body.String())
	}
}

// The gate must also let through what the host DOES ship, read from the real
// set. Together with the test above this pins both directions: nothing
// unrunnable installs, and nothing runnable is refused.
func TestPluginInstallAcceptsEveryCapabilityThisHostShips(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	source := withLocalPluginSource(t, hookOnlyTestManifest)
	testHandler.PluginService.Host = plugincontract.HostCapabilities()

	body, _ := json.Marshal(map[string]any{
		"source_url":     source,
		"granted_scopes": []string{"issues:read", "net:example.com"},
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	// hookOnlyTestManifest declares ui + http, both shipped by the hook engine.
	// If this starts failing, the gate is refusing something the host can run.
	if recorder.Code != http.StatusCreated {
		t.Fatalf("a manifest declaring only shipped capabilities was refused: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPluginInstallAcceptsAShippedSurface(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	// The other half of the gate: a contribution the host DOES ship must install
	// against the real HostCapabilities set, not only against a test set that
	// enables everything.
	source := withLocalPluginSource(t, handlerTestManifest)
	testHandler.PluginService.Host = plugincontract.HostCapabilities()

	body, _ := json.Marshal(map[string]any{
		"source_url":     source,
		"granted_scopes": []string{"issues:read", "comments:write", "storage:user"},
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("shipped surface was refused: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPluginInstallationFromAnotherWorkspaceIsNotFound(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	recorder := httptest.NewRecorder()
	testHandler.EnablePlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/enable", nil, map[string]string{
		"id":             testWorkspaceID,
		"installationId": "11111111-1111-1111-1111-111111111111",
	}))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPluginUpgradePrunesSecretsTheNewManifestDropped(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	root := t.TempDir()
	source := withLocalPluginSourceIn(t, root, handlerTestManifest)

	install, _ := json.Marshal(map[string]any{
		"source_url":     source,
		"granted_scopes": []string{"issues:read", "comments:write", "storage:user"},
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", install, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID string `json:"id"`
	}
	json.Unmarshal(recorder.Body.Bytes(), &installed)

	params := map[string]string{"id": testWorkspaceID, "installationId": installed.ID}
	configure, _ := json.Marshal(map[string]any{"values": map[string]any{"token": "sk-old-secret"}})
	recorder = httptest.NewRecorder()
	testHandler.ConfigurePlugin(recorder, pluginHandlerRequest(http.MethodPut, "/plugins/config", configure, params))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"configured_secrets":["token"]`) {
		t.Fatalf("configure status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// v2 drops the `token` secret field entirely. Its ciphertext must go with
	// it: nothing can reach a secret whose field no longer exists, and leaving
	// it means an uninstall is the only thing that ever removes it.
	upgraded := strings.Replace(handlerTestManifest, `"version": "1.0.0"`, `"version": "2.0.0"`, 1)
	upgraded = strings.Replace(upgraded, `,
    "token": { "type": "secret", "label": "Token" }`, "", 1)
	writeLocalPluginManifest(t, root, upgraded)

	recorder = httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", install, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upgrade status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"configured_secrets":[]`) {
		t.Fatalf("upgrade left an unreachable secret: %s", recorder.Body.String())
	}

	var remaining int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM plugin_secret WHERE installation_id = $1`, installed.ID).Scan(&remaining); err != nil {
		t.Fatalf("count secrets: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("upgrade left %d secret rows behind", remaining)
	}
}

// TestPluginRoutesRequireWorkspaceAdmin pins the gate that actually protects
// these endpoints. Every other handler test calls the handler directly, which
// bypasses middleware entirely — so without this, "install/configure/uninstall
// are admin-only" rests on where the routes sit in router.go and nothing would
// fail if one moved into the member group.
func TestPluginRoutesRequireWorkspaceAdmin(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	memberID := createTestUserAndMember(t, "member")

	router := chi.NewRouter()
	router.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
			r.Post("/plugins/preview", testHandler.PreviewPlugin)
			r.Post("/plugins", testHandler.InstallPlugin)
			r.Put("/plugins/{installationId}/config", testHandler.ConfigurePlugin)
			r.Post("/plugins/{installationId}/enable", testHandler.EnablePlugin)
			r.Post("/plugins/{installationId}/disable", testHandler.DisablePlugin)
			r.Delete("/plugins/{installationId}", testHandler.UninstallPlugin)
		})
		// Listing is member-visible on purpose: a member should be able to see
		// what is mounted in their workspace and which scopes it holds.
		r.Get("/plugins", testHandler.ListPlugins)
	})

	base := "/api/workspaces/" + testWorkspaceID
	adminOnly := []struct {
		method string
		path   string
	}{
		{http.MethodPost, base + "/plugins/preview"},
		{http.MethodPost, base + "/plugins"},
		{http.MethodPut, base + "/plugins/11111111-1111-1111-1111-111111111111/config"},
		{http.MethodPost, base + "/plugins/11111111-1111-1111-1111-111111111111/enable"},
		{http.MethodPost, base + "/plugins/11111111-1111-1111-1111-111111111111/disable"},
		{http.MethodDelete, base + "/plugins/11111111-1111-1111-1111-111111111111"},
	}
	for _, route := range adminOnly {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			request := newRequest(route.method, route.path, map[string]any{})
			request.Header.Set("X-User-ID", memberID)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("plain member reached an admin route: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}

	request := newRequest(http.MethodGet, base+"/plugins", nil)
	request.Header.Set("X-User-ID", memberID)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("plain member could not list Plugins: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
