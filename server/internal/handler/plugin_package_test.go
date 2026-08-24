package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// Publishing, and the guarantee it exists for.
//
// The old model froze the manifest at install and loaded the surface script from
// the author's server on every panel open, so "the administrator approved this"
// and "this is what runs" were two different statements. These tests pin the
// replacement: an installation names one immutable version, and publishing
// another one changes nothing until an administrator upgrades.

const packageTestManifest = `{
  "manifest_version": 1,
  "key": "com.example.published",
  "name": "Published Panel",
  "version": "%VERSION%",
  "author": { "name": "example" },
  "scopes": ["issues:read"],
  "contributes": {
    "surfaces": [{ "key": "hello", "type": "issue_panel", "name": "Hello", "entry": "ui/main.js" }]
  }
}`

func packageManifest(version string) string {
	return strings.Replace(packageTestManifest, "%VERSION%", version, 1)
}

func pluginBundleZip(t *testing.T, manifest, script string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range map[string]string{
		plugincontract.ManifestFilename: manifest,
		"ui/main.js":                    script,
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func uploadPluginBundle(t *testing.T, archive []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("bundle", "plugin.zip")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/plugins/packages", &body)
	request.Header.Set("X-User-ID", testUserID)
	request.Header.Set("Content-Type", form.FormDataContentType())
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", testWorkspaceID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))

	recorder := httptest.NewRecorder()
	testHandler.PublishPluginPackage(recorder, request)
	return recorder
}

func publishUploadedBundle(t *testing.T, manifest, script string) service.PluginPackageSummary {
	t.Helper()
	recorder := uploadPluginBundle(t, pluginBundleZip(t, manifest, script))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("publish: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var published service.PluginPackageSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &published); err != nil {
		t.Fatalf("decode published package: %v", err)
	}
	return published
}

func installPublishedVersion(t *testing.T, versionID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"version_id": versionID, "granted_scopes": []string{"issues:read"}})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	return installed.ID
}

func surfaceScript(t *testing.T, installationID, surfaceKey string) (*httptest.ResponseRecorder, service.PluginSurfaceScript) {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.GetPluginSurfaceScript(recorder, pluginHandlerRequest(http.MethodGet, "/script", nil, map[string]string{
		"id":             testWorkspaceID,
		"installationId": installationID,
		"surfaceKey":     surfaceKey,
	}))
	var script service.PluginSurfaceScript
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &script); err != nil {
			t.Fatalf("decode surface script: %v", err)
		}
	}
	return recorder, script
}

// An author with no server of their own publishes a panel plugin by upload, an
// administrator installs it, and the panel's code comes back from us. That is
// the acceptance criterion for the whole change.
func TestUploadedBundleInstallsAndServesItsOwnCode(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withHostCapabilities(t)

	published := publishUploadedBundle(t, packageManifest("1.0.0"), "console.log('v1');\n")
	if len(published.Versions) != 1 || published.Versions[0].Version != "1.0.0" {
		t.Fatalf("unexpected published versions: %+v", published.Versions)
	}
	if published.Versions[0].Digest == "" || published.Versions[0].SizeBytes == 0 {
		t.Fatalf("a published version must report what was stored: %+v", published.Versions[0])
	}

	installationID := installPublishedVersion(t, published.Versions[0].ID)
	recorder, script := surfaceScript(t, installationID, "hello")
	if recorder.Code != http.StatusOK {
		t.Fatalf("surface script: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(script.Code, "console.log('v1')") {
		t.Fatalf("the served code is not what was uploaded: %q", script.Code)
	}
	if script.Version != "1.0.0" {
		t.Fatalf("script version = %q, want the installed version", script.Version)
	}
}

// The invariant the RFC called non-negotiable: publishing again must not change
// what an installed workspace runs.
func TestPublishingANewVersionDoesNotChangeAnInstalledWorkspace(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withHostCapabilities(t)

	first := publishUploadedBundle(t, packageManifest("1.0.0"), "console.log('v1');\n")
	installationID := installPublishedVersion(t, first.Versions[0].ID)

	second := publishUploadedBundle(t, packageManifest("2.0.0"), "console.log('v2');\n")
	if len(second.Versions) != 2 {
		t.Fatalf("the second publish should add a version, not replace one: %+v", second.Versions)
	}

	_, script := surfaceScript(t, installationID, "hello")
	if strings.Contains(script.Code, "v2") {
		t.Fatalf("a publish changed the code an installed workspace runs: %q", script.Code)
	}
	if script.Version != "1.0.0" {
		t.Fatalf("installed version = %q, want 1.0.0 until an administrator upgrades", script.Version)
	}

	// The settings page has to be able to say which one is running, or "upgrade
	// available" is not something an administrator can act on.
	installedFlags := map[string]bool{}
	for _, version := range second.Versions {
		installedFlags[version.Version] = version.Installed
	}
	if !installedFlags["1.0.0"] || installedFlags["2.0.0"] {
		t.Fatalf("installed markers are wrong: %+v", second.Versions)
	}

	// And upgrading is what moves it.
	upgradeVersionID := ""
	for _, version := range second.Versions {
		if version.Version == "2.0.0" {
			upgradeVersionID = version.ID
		}
	}
	if got := installPublishedVersion(t, upgradeVersionID); got != installationID {
		t.Fatalf("upgrade created a second installation: %s != %s", got, installationID)
	}
	if _, upgraded := surfaceScript(t, installationID, "hello"); !strings.Contains(upgraded.Code, "v2") {
		t.Fatalf("after upgrading, the code is still %q", upgraded.Code)
	}
}

// Republishing a version is refused rather than accepted as an overwrite. The
// unique index is what makes immutability a rule instead of a convention.
func TestPublishRefusesToOverwriteAnExistingVersion(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withHostCapabilities(t)

	publishUploadedBundle(t, packageManifest("1.0.0"), "console.log('v1');\n")
	recorder := uploadPluginBundle(t, pluginBundleZip(t, packageManifest("1.0.0"), "console.log('tampered');\n"))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("republish status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "immutable") {
		t.Fatalf("the conflict does not explain itself: %s", recorder.Body.String())
	}
}

func TestDeletePublishedPackageRefusesWhileInstalled(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withHostCapabilities(t)

	published := publishUploadedBundle(t, packageManifest("1.0.0"), "console.log('v1');\n")
	installationID := installPublishedVersion(t, published.Versions[0].ID)

	params := map[string]string{"id": testWorkspaceID, "packageId": published.ID}
	recorder := httptest.NewRecorder()
	testHandler.DeletePluginPackage(recorder, pluginHandlerRequest(http.MethodDelete, "/plugins/packages", nil, params))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("delete while installed status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	uninstall := httptest.NewRecorder()
	testHandler.UninstallPlugin(uninstall, pluginHandlerRequest(http.MethodDelete, "/plugins", nil, map[string]string{
		"id": testWorkspaceID, "installationId": installationID,
	}))
	if uninstall.Code != http.StatusNoContent {
		t.Fatalf("uninstall status=%d body=%s", uninstall.Code, uninstall.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.DeletePluginPackage(recorder, pluginHandlerRequest(http.MethodDelete, "/plugins/packages", nil, params))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete after uninstall status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// The stored bundle goes with it; a package row with orphaned files behind
	// it is storage nothing can reach.
	var remaining int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM plugin_package_file WHERE version_id IN (
			SELECT id FROM plugin_package_version WHERE workspace_id = $1)`, testWorkspaceID).Scan(&remaining); err != nil {
		t.Fatalf("count package files: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("deleting the package left %d files behind", remaining)
	}
}

func TestSurfaceScriptRefusesWhatWasNeverInstalled(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withHostCapabilities(t)

	published := publishUploadedBundle(t, packageManifest("1.0.0"), "console.log('v1');\n")
	installationID := installPublishedVersion(t, published.Versions[0].ID)

	// A surface key the installed manifest does not declare. The manifest
	// consulted is the installation's own snapshot, so this cannot be widened by
	// publishing a version that declares more.
	if recorder, _ := surfaceScript(t, installationID, "nope"); recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown surface key status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	disable := httptest.NewRecorder()
	testHandler.DisablePlugin(disable, pluginHandlerRequest(http.MethodPost, "/plugins/disable", nil, map[string]string{
		"id": testWorkspaceID, "installationId": installationID,
	}))
	if disable.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disable.Code, disable.Body.String())
	}
	// Disabling has to stop the code from being served, or "disabled" is a UI
	// state rather than a decision.
	if recorder, _ := surfaceScript(t, installationID, "hello"); recorder.Code != http.StatusForbidden {
		t.Fatalf("disabled installation served its code: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// withHostCapabilities enables every contribution kind for the duration of a
// test, so these exercise publishing rather than the staged-rollout gate.
func withHostCapabilities(t *testing.T) {
	t.Helper()
	previous := testHandler.PluginService.Host
	testHandler.PluginService.Host = plugincontract.Capabilities{
		SurfaceTypes:  map[string]bool{plugincontract.SurfaceIssuePanel: true, plugincontract.SurfaceSidebarPanel: true, plugincontract.SurfaceModal: true},
		HookTriggers:  map[string]bool{plugincontract.TriggerUI: true, plugincontract.TriggerManual: true, plugincontract.TriggerAgent: true, plugincontract.TriggerEvent: true},
		HookTransport: map[string]bool{plugincontract.TransportHTTP: true, plugincontract.TransportMCP: true},
		ResourceTypes: map[string]bool{plugincontract.ResourceSkill: true},
	}
	t.Cleanup(func() { testHandler.PluginService.Host = previous })
}
