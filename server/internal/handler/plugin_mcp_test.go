package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The gates in front of an mcp hook's tool list.
//
// Discovery itself reaches the plugin author's own MCP server, so these cover
// what has to hold BEFORE any outbound call happens — the checks that would
// otherwise be a comment claiming they exist. Every one of them refuses without
// the network being involved at all, which is the point: a wrong answer here is
// a request that should never have left the process.

const mcpToolboxManifest = `{
  "manifest_version": 1,
  "key": "com.example.toolbox",
  "name": "Toolbox",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read", "net:tools.example.com"],
  "contributes": {
    "hooks": [{
      "key": "toolbox",
      "name": "Toolbox",
      "description": "Tools from an external MCP server.",
      "triggers": ["agent"],
      "transport": { "type": "mcp", "url": "https://tools.example.com/mcp" }
    }]
  }
}`

// An http hook declares one endpoint the administrator already saw on the
// consent screen. Asking it for a tool list is a category error, not an empty
// list — answering `{"tools":[]}` would make the UI offer an approval panel for
// a hook that has nothing to approve.
const httpHookManifest = `{
  "manifest_version": 1,
  "key": "com.example.plainhook",
  "name": "Plain Hook",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read", "net:hooks.example.com"],
  "contributes": {
    "hooks": [{
      "key": "notify",
      "name": "Notify",
      "description": "Posts to an endpoint.",
      "triggers": ["manual"],
      "transport": { "type": "http", "url": "https://hooks.example.com/notify" }
    }]
  }
}`

func installMCPPlugin(t *testing.T, manifest string, scopes []string) string {
	t.Helper()
	withPluginsV1Flag(t, testHandler, true)
	versionID := withLocalPluginSource(t, manifest)
	body, _ := json.Marshal(map[string]any{"version_id": versionID, "granted_scopes": scopes})
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
	t.Cleanup(func() {
		cleanup := httptest.NewRecorder()
		testHandler.UninstallPlugin(cleanup, pluginHandlerRequest(http.MethodDelete, "/plugins",
			nil, map[string]string{"id": testWorkspaceID, "installationId": installed.ID}))
	})
	return installed.ID
}

func listMCPTools(t *testing.T, installationID, hookKey string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.ListPluginMCPTools(recorder, pluginHandlerRequest(http.MethodGet, "/mcp/tools", nil, map[string]string{
		"id": testWorkspaceID, "installationId": installationID, "hookKey": hookKey,
	}))
	return recorder
}

func TestMCPToolListRejectsAnHTTPTransportHook(t *testing.T) {
	installationID := installMCPPlugin(t, httpHookManifest, []string{"issues:read", "net:hooks.example.com"})

	recorder := listMCPTools(t, installationID, "notify")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("an http hook must not answer a tool list: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestMCPToolListRejectsAnUnknownHook(t *testing.T) {
	installationID := installMCPPlugin(t, mcpToolboxManifest, []string{"issues:read", "net:tools.example.com"})

	recorder := listMCPTools(t, installationID, "does-not-exist")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", recorder.Code, recorder.Body.String())
	}
}

// The `net:` scope is the consent, and a hook's own transport URL is not
// self-authorizing. A manifest can declare any endpoint it likes; what makes it
// reachable is the scope line the administrator read and approved, so a manifest
// that declares an mcp hook and no `net:` scope gets refused before any DNS
// lookup happens.
const scopelessMCPManifest = `{
  "manifest_version": 1,
  "key": "com.example.scopeless",
  "name": "Scopeless",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read"],
  "contributes": {
    "hooks": [{
      "key": "toolbox",
      "name": "Toolbox",
      "description": "Points somewhere it was never granted.",
      "triggers": ["agent"],
      "transport": { "type": "mcp", "url": "https://tools.example.com/mcp" }
    }]
  }
}`

// Refused at PUBLISH, not at discovery: manifest validation already requires
// every hook transport URL to sit inside a declared `net:` scope, and that check
// does not care which transport it is. The equivalent check inside
// DiscoverMCPHookTools is a second layer behind this one, not the gate.
//
// The gate used to sit at install, because that was the first moment anybody
// parsed the manifest. Now the artifact is parsed when the author hands it over,
// so a manifest like this never becomes something an administrator can be shown.
func TestMCPHookWithoutANetScopeCannotBePublished(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	root := t.TempDir()
	writeLocalPluginManifest(t, root, scopelessMCPManifest)
	previousDir := testHandler.PluginService.LocalDir
	testHandler.PluginService.LocalDir = root
	t.Cleanup(func() { testHandler.PluginService.LocalDir = previousDir })

	body, _ := json.Marshal(map[string]string{"name": "hello"})
	recorder := httptest.NewRecorder()
	testHandler.PublishLocalPluginPackage(recorder,
		pluginHandlerRequest(http.MethodPost, "/plugins/packages/local", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code == http.StatusCreated {
		t.Fatal("a hook pointing outside its declared net: scopes was published")
	}
	if !strings.Contains(recorder.Body.String(), "net: scope") {
		t.Fatalf("the refusal does not name the reason: %s", recorder.Body.String())
	}
}

// The daemon's credential route keys on "<installation>:<hook>". A malformed id
// must be refused rather than resolved as a bare installation id — otherwise a
// daemon holding an id for one contribution could name another by trimming it.
func TestPluginContributionIDNeedsBothHalves(t *testing.T) {
	refused := []string{
		"", "no-colon", ":toolbox", "installation:",
		"plugin:", "plugin:no-colon", "plugin::toolbox", "plugin:installation:",
		// Unprefixed. A workspace's own Remote MCP connection resolves its
		// credential from somewhere else entirely, so its id must not be
		// answerable here even when it happens to have the right shape.
		"1f0d9b7e-0000-0000-0000-000000000000:toolbox",
	}
	for _, contribution := range refused {
		if _, _, ok := splitPluginContributionID(contribution); ok {
			t.Fatalf("contribution %q was accepted; it must be refused", contribution)
		}
	}

	installationID, hookKey, ok := splitPluginContributionID("plugin:1f0d9b7e-0000-0000-0000-000000000000:toolbox")
	if !ok || installationID != "1f0d9b7e-0000-0000-0000-000000000000" || hookKey != "toolbox" {
		t.Fatalf("split = (%q, %q, %v), want the two halves", installationID, hookKey, ok)
	}
}
