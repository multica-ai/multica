package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/multica-ai/multica/server/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// hookHandlerTestManifest declares the three host-driven triggers. Its endpoint
// is never reached in these tests — they cover the checks BEFORE the request
// leaves, which is where a wrong answer is a security bug rather than a broken
// integration. The wire format and signature are covered end-to-end against a
// live server in internal/service.
const hookHandlerTestManifest = `{
  "manifest_version": 1,
  "key": "com.example.hooked",
  "name": "Hooked",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read", "comments:write", "net:example.com"],
  "contributes": {
    "hooks": [{
      "key": "summarize",
      "name": "Summarize",
      "description": "Compress the discussion.",
      "triggers": ["ui", "manual", "event"],
      "events": ["issue.created"],
      "transport": { "type": "http", "url": "https://example.com/hooks/summarize" }
    }, {
      "key": "manual_only",
      "name": "Manual only",
      "description": "Only ever picked from a menu.",
      "triggers": ["manual"],
      "transport": { "type": "http", "url": "https://example.com/hooks/manual" }
    }]
  }
}`

func installHookPlugin(t *testing.T) string {
	t.Helper()
	versionID := withLocalPluginSource(t, hookHandlerTestManifest)
	body, _ := json.Marshal(map[string]any{
		"version_id":     versionID,
		"granted_scopes": []string{"issues:read", "comments:write", "net:example.com"},
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install hook plugin: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	return installed.ID
}

func invokeHookRequest(installationID, hookKey string, payload map[string]any) *http.Request {
	body, _ := json.Marshal(payload)
	request := httptest.NewRequest(http.MethodPost, "/api/plugin-bridge/v1/hooks/"+hookKey, bytes.NewReader(body))
	request.Header.Set("X-User-ID", testUserID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(pluginInstallationHeader, installationID)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("key", hookKey)
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

// `event` is dispatched by the host, off the event bus, precisely so that its
// writes carry the plugin's identity rather than a person's. A browser asking
// for one would be electing to run as somebody it is not.
func TestInvokePluginHookRefusesHostDrivenTriggers(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	installationID := installHookPlugin(t)

	for _, trigger := range []string{plugincontract.TriggerEvent, plugincontract.TriggerAgent, "", "nonsense"} {
		recorder := httptest.NewRecorder()
		testHandler.InvokePluginHook(recorder, invokeHookRequest(installationID, "summarize", map[string]any{"trigger": trigger}))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("trigger %q: status=%d body=%s, want 400", trigger, recorder.Code, recorder.Body.String())
		}
	}
}

// A hook may only be reached through a trigger its own manifest declared, even
// when the host supports that trigger in general.
func TestInvokePluginHookRefusesTriggerTheManifestDidNotDeclare(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	installationID := installHookPlugin(t)

	recorder := httptest.NewRecorder()
	// manual_only declares manual, not ui.
	testHandler.InvokePluginHook(recorder, invokeHookRequest(installationID, "manual_only", map[string]any{"trigger": "ui"}))
	if recorder.Code == http.StatusOK {
		t.Fatalf("a hook was invoked through a trigger it never declared: %s", recorder.Body.String())
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", recorder.Code, recorder.Body.String())
	}
}

func TestInvokePluginHookRefusesUnknownHook(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	installationID := installHookPlugin(t)

	recorder := httptest.NewRecorder()
	testHandler.InvokePluginHook(recorder, invokeHookRequest(installationID, "not_declared", map[string]any{"trigger": "manual"}))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", recorder.Code, recorder.Body.String())
	}
}

// The flag gates the hook endpoint like every other plugin route: fail closed,
// not merely hidden from the UI.
func TestInvokePluginHookRequiresTheFeatureFlag(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	installationID := installHookPlugin(t)

	withPluginsV1Flag(t, testHandler, false)
	recorder := httptest.NewRecorder()
	testHandler.InvokePluginHook(recorder, invokeHookRequest(installationID, "summarize", map[string]any{"trigger": "manual"}))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want 503", recorder.Code, recorder.Body.String())
	}
}

// A disabled installation is off, not hidden. A stale tab must not keep
// invoking hooks after an admin switches the plugin off.
func TestInvokePluginHookRefusesDisabledInstallation(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	installationID := installHookPlugin(t)

	disable := httptest.NewRecorder()
	testHandler.DisablePlugin(disable, pluginHandlerRequest(http.MethodPost, "/disable", nil, map[string]string{
		"id": testWorkspaceID, "installationId": installationID,
	}))
	if disable.Code != http.StatusOK {
		t.Fatalf("disable: status=%d body=%s", disable.Code, disable.Body.String())
	}

	recorder := httptest.NewRecorder()
	testHandler.InvokePluginHook(recorder, invokeHookRequest(installationID, "summarize", map[string]any{"trigger": "manual"}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", recorder.Code, recorder.Body.String())
	}
}

// The install token is returned once and stored only as a hash, so the same
// token must never come back from a second rotation, and the old one must stop
// working the moment a new one is issued.
func TestRotatePluginTokenIssuesOnceAndInvalidatesThePrevious(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	installationID := installHookPlugin(t)
	testHandler.PluginService.DeploymentKey = bytes.Repeat([]byte{9}, 32)
	t.Cleanup(func() { testHandler.PluginService.DeploymentKey = nil })

	first := rotateToken(t, installationID)
	if first.Token == "" || first.SigningSecret == "" {
		t.Fatalf("rotation returned nothing usable: %+v", first)
	}

	installation, err := testHandler.PluginService.AuthenticateInstallToken(context.Background(), first.Token)
	if err != nil {
		t.Fatalf("the freshly issued token must authenticate: %v", err)
	}
	if uuidToString(installation.ID) != installationID {
		t.Fatalf("token resolved to the wrong installation: %s", uuidToString(installation.ID))
	}

	second := rotateToken(t, installationID)
	if second.Token == first.Token {
		t.Fatal("rotation reissued the same token")
	}
	if _, err := testHandler.PluginService.AuthenticateInstallToken(context.Background(), first.Token); err == nil {
		t.Fatal("the previous token must stop working after a rotation")
	}
	// Derived from the deployment key and the installation, so it survives
	// rotation — an author does not have to reconfigure their server every
	// time an admin rotates the token.
	if second.SigningSecret != first.SigningSecret {
		t.Fatal("the signing secret must be stable across token rotations")
	}
}

func TestRevokePluginTokenStopsAuthentication(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	installationID := installHookPlugin(t)
	testHandler.PluginService.DeploymentKey = bytes.Repeat([]byte{9}, 32)
	t.Cleanup(func() { testHandler.PluginService.DeploymentKey = nil })

	issued := rotateToken(t, installationID)
	recorder := httptest.NewRecorder()
	testHandler.RevokePluginToken(recorder, pluginHandlerRequest(http.MethodDelete, "/token", nil, map[string]string{
		"id": testWorkspaceID, "installationId": installationID,
	}))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("revoke: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := testHandler.PluginService.AuthenticateInstallToken(context.Background(), issued.Token); err == nil {
		t.Fatal("a revoked token must not authenticate")
	}
}

// A garbage token must not resolve to some installation by accident — the
// lookup is by hash, so an empty or malformed value must be refused before it
// reaches the query.
func TestAuthenticateInstallTokenRefusesMalformedValues(t *testing.T) {
	for _, token := range []string{"", "mpi_", "not-a-token", "mpc_something", "Bearer mpi_x"} {
		if _, err := testHandler.PluginService.AuthenticateInstallToken(context.Background(), token); err == nil {
			t.Fatalf("token %q must not authenticate", token)
		}
	}
}

func rotateToken(t *testing.T, installationID string) pluginTokenResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	testHandler.RotatePluginToken(recorder, pluginHandlerRequest(http.MethodPost, "/token", nil, map[string]string{
		"id": testWorkspaceID, "installationId": installationID,
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("rotate: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var issued pluginTokenResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	return issued
}

// The feature flag gates the EVENT path, observed where it matters: does a
// request actually leave for the plugin's endpoint.
//
// Every other plugin surface is refused by a handler that can read the flag off
// a request. An event hook has none — the dispatcher is subscribed at startup
// and runs on a worker — so without a check inside it, turning plugins off
// would hide the UI and refuse the Action API while the outbound calls kept
// going. That is fail-open on the one path that reaches a third party.
//
// Needs a database because the dispatcher only calls anything when a real,
// enabled installation declares an event hook.
func TestEventDispatchRespectsTheFeatureFlagEndToEnd(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)

	received := make(chan struct{}, 4)
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(endpoint.Close)

	host := strings.Split(strings.TrimPrefix(endpoint.URL, "https://"), ":")[0]
	manifest := strings.NewReplacer(
		"https://example.com/hooks/summarize", endpoint.URL+"/hooks/summarize",
		"https://example.com/hooks/manual", endpoint.URL+"/hooks/manual",
		`"net:example.com"`, `"net:`+host+`"`,
	).Replace(hookHandlerTestManifest)

	versionID := withLocalPluginSource(t, manifest)
	// The dev-origin opt-in is what lets a loopback endpoint be dialled at all;
	// the granted net: scope still has to cover it, which the replace above did.
	previousOrigins := testHandler.PluginService.DevOrigins
	previousClient := testHandler.PluginService.HookClient
	previousFlags := testHandler.PluginService.FeatureFlags
	previousKey := testHandler.PluginService.DeploymentKey
	previousCallbacks := testHandler.PluginService.Callbacks
	testHandler.PluginService.DevOrigins = []string{endpoint.URL}
	testHandler.PluginService.HookClient = endpoint.Client()
	// Without a deployment key there is nothing to sign with and the call is
	// refused before it leaves — which would make this test pass for the wrong
	// reason in the flag-off half.
	testHandler.PluginService.DeploymentKey = bytes.Repeat([]byte{5}, 32)
	testHandler.PluginService.Callbacks = service.NewCallbackTokens()
	t.Cleanup(func() {
		testHandler.PluginService.DevOrigins = previousOrigins
		testHandler.PluginService.HookClient = previousClient
		testHandler.PluginService.FeatureFlags = previousFlags
		testHandler.PluginService.DeploymentKey = previousKey
		testHandler.PluginService.Callbacks = previousCallbacks
	})

	body, _ := json.Marshal(map[string]any{
		"version_id":     versionID,
		"granted_scopes": []string{"issues:read", "comments:write", "net:" + host},
	})
	install := httptest.NewRecorder()
	testHandler.InstallPlugin(install, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if install.Code != http.StatusCreated {
		t.Fatalf("install: status=%d body=%s", install.Code, install.Body.String())
	}

	dispatch := func() {
		dispatcher := service.NewPluginEventDispatcher(testHandler.PluginService)
		defer dispatcher.Close()
		dispatcher.Dispatch(plugincontract.EventIssueCreated, testWorkspaceID, map[string]any{})
		// Long enough for a worker to pick the job up and complete the call.
		time.Sleep(2 * time.Second)
	}

	// Flag on: the endpoint is called.
	testHandler.PluginService.FeatureFlags = testHandler.FeatureFlags
	dispatch()
	select {
	case <-received:
	default:
		t.Fatal("with the flag on, an installed event hook was never called")
	}

	// Flag off: nothing leaves, even though the same installation is still
	// enabled and still declares the hook.
	withPluginsV1Flag(t, testHandler, false)
	testHandler.PluginService.FeatureFlags = testHandler.FeatureFlags
	dispatch()
	select {
	case <-received:
		t.Fatal("with the flag off, an event hook still called out — the flag does not gate the outbound path")
	default:
	}
}
