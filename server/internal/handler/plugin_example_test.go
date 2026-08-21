package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// examples/plugins/deploy-sentinel, driven end to end.
//
// This installs the REAL manifest from the repository, not a fixture written
// next to the assertions. That is the point: a hand-written fixture keeps
// passing after the example rots, and the example is what a plugin author
// copies. If somebody breaks the manifest, this is what says so.
//
// The plugin declares all four triggers and both transports, so one install
// covers the whole PR-4 surface: a skill resource lands in the skill table, its
// http hooks reach an agent as tools and are invoked over a real signed request,
// and its mcp hook is discovered and approved.
//
// The only thing rewritten on the way in is the two endpoint hosts — a test
// server's port is not knowable when the manifest is written. Everything else,
// including every tool description an agent reads, is the file's own text.

func exampleRepoDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	// server/internal/handler -> repo root
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "examples", "plugins", "deploy-sentinel")
	if _, err := os.Stat(filepath.Join(dir, plugincontract.ManifestFilename)); err != nil {
		t.Fatalf("the example plugin is missing: %v", err)
	}
	return dir
}

type exampleServers struct {
	sentinel *httptest.Server
	metrics  *httptest.Server
}

func hostOf(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return parsed.Hostname()
}

// stageExamplePlugin copies the example into a temp MULTICA_PLUGIN_DIR and
// repoints its two endpoints (and the net: scopes that authorise them) at the
// running test servers.
func stageExamplePlugin(t *testing.T, servers exampleServers) (string, []string) {
	t.Helper()
	source := exampleRepoDir(t)
	root := t.TempDir()
	staged := filepath.Join(root, "deploy-sentinel")

	if err := os.CopyFS(staged, os.DirFS(source)); err != nil {
		t.Fatalf("stage the example: %v", err)
	}

	manifestPath := filepath.Join(staged, plugincontract.ManifestFilename)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read the example manifest: %v", err)
	}
	sentinelHost, metricsHost := hostOf(t, servers.sentinel.URL), hostOf(t, servers.metrics.URL)
	rewritten := strings.NewReplacer(
		"https://sentinel.example.com", servers.sentinel.URL,
		"https://metrics.example.com", servers.metrics.URL,
		"net:sentinel.example.com", "net:"+sentinelHost,
		"net:metrics.example.com", "net:"+metricsHost,
	).Replace(string(raw))
	// A net: scope names a host, not a URL, and both test servers land on the
	// same loopback host with different ports. Two identical scope lines are a
	// manifest error, so collapse them — this is an artefact of the rewrite,
	// not of the example.
	rewritten = dedupeScopes(t, rewritten)
	if err := os.WriteFile(manifestPath, []byte(rewritten), 0o644); err != nil {
		t.Fatalf("write the staged manifest: %v", err)
	}

	// Read the scopes back out of the staged file rather than listing them
	// here: granting a set this test maintains by hand would let the manifest
	// add a scope that nothing ever notices.
	var parsed struct {
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal([]byte(rewritten), &parsed); err != nil {
		t.Fatalf("parse the staged manifest: %v", err)
	}
	return root, parsed.Scopes
}

// dedupeScopes rewrites the manifest's scope array in place, dropping repeats
// while preserving order.
func dedupeScopes(t *testing.T, manifest string) string {
	t.Helper()
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(manifest), &document); err != nil {
		t.Fatalf("parse the staged manifest: %v", err)
	}
	var scopes []string
	if err := json.Unmarshal(document["scopes"], &scopes); err != nil {
		t.Fatalf("parse scopes: %v", err)
	}
	seen := map[string]bool{}
	unique := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if seen[scope] {
			continue
		}
		seen[scope] = true
		unique = append(unique, scope)
	}
	encoded, err := json.Marshal(unique)
	if err != nil {
		t.Fatalf("encode scopes: %v", err)
	}
	document["scopes"] = encoded
	out, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode the staged manifest: %v", err)
	}
	return string(out)
}

func installExamplePlugin(t *testing.T, servers exampleServers) string {
	t.Helper()
	withPluginsV1Flag(t, testHandler, true)
	root, scopes := stageExamplePlugin(t, servers)

	plugins := testHandler.PluginService
	previous := *plugins
	plugins.LocalDir = root
	plugins.Host = plugincontract.HostCapabilities()
	// The two test servers run on loopback with self-signed certs, which the
	// outbound guard refuses by design. DevOrigins is the operator opt-in a
	// plugin author uses for exactly this, and it is scoped to these origins.
	plugins.DevOrigins = []string{servers.sentinel.URL, servers.metrics.URL}
	plugins.HookClient = servers.sentinel.Client()
	// The manifest declares two secret config fields, so storing configuration
	// needs the secretbox the deployment would normally supply.
	box, err := secretbox.New(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	plugins.Secrets = box
	// Signing is what makes a hook call verifiable by the plugin's own server;
	// without a deployment key the engine refuses to call out at all.
	plugins.DeploymentKey = bytes.Repeat([]byte{3}, 32)
	plugins.Callbacks = service.NewCallbackTokens()
	plugins.CallbackBaseURL = "https://multica.test/api/v1/plugin"
	t.Cleanup(func() { *plugins = previous })

	body, _ := json.Marshal(map[string]any{
		"source_url":     service.LocalSourcePrefix + "deploy-sentinel",
		"granted_scopes": scopes,
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install deploy-sentinel: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	t.Cleanup(func() {
		cleanup := httptest.NewRecorder()
		testHandler.UninstallPlugin(cleanup, pluginHandlerRequest(http.MethodDelete, "/plugins", nil,
			map[string]string{"id": testWorkspaceID, "installationId": installed.ID}))
	})
	return installed.ID
}

func configureExamplePlugin(t *testing.T, installationID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"values": map[string]any{
		"service_prefix":          "prod-",
		"rollback_window_minutes": 60,
		"require_incident_label":  true,
		"environment":             "production",
		"sentinel_token":          "sentinel-secret",
	}})
	recorder := httptest.NewRecorder()
	testHandler.ConfigurePlugin(recorder, pluginHandlerRequest(http.MethodPut, "/config", body,
		map[string]string{"id": testWorkspaceID, "installationId": installationID}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("configure: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// quietServer is the plugin's own hook endpoint: TLS, because the http
// transport's client is supplied by the test and can trust its certificate.
func quietServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return server
}

// metricsServer is the plugin author's own MCP server: HTTPS, because the
// manifest validator requires it and a dev origin does not relax that.
//
// remotemcp.Discover builds its own HTTP client, so the only way for it to
// trust this certificate is the same one a plugin author uses locally —
// MULTICA_PLUGIN_DEV_CA, pointing at a CA bundle.
func metricsServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(serveMetricsMCP))
	t.Cleanup(server.Close)

	caPath := filepath.Join(t.TempDir(), "dev-ca.pem")
	pem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, pem, 0o600); err != nil {
		t.Fatalf("write the dev CA: %v", err)
	}
	t.Setenv(remotemcp.DevCAEnv, caPath)
	t.Setenv(remotemcp.DevOriginsEnv, server.URL)
	return server
}

// The example's SKILL.md becomes an ordinary workspace skill. No bundle
// pipeline, no digest, no new table — that was the whole claim.
func TestExamplePluginContributesItsSkill(t *testing.T) {
	servers := exampleServers{
		sentinel: quietServer(t, func(http.ResponseWriter, *http.Request) {}),
		metrics:  metricsServer(t),
	}
	installationID := installExamplePlugin(t, servers)

	names := skillNamesForInstallation(t, installationID)
	if len(names) != 1 || names[0] != "incident-response" {
		t.Fatalf("skills for the installation = %v, want [incident-response]", names)
	}

	var body string
	if err := testPool.QueryRow(context.Background(),
		`SELECT content FROM skill WHERE plugin_installation_id = $1`, installationID).Scan(&body); err != nil {
		t.Fatalf("read the skill content: %v", err)
	}
	// Asserting on a sentence the skill actually teaches, not on its length: a
	// truncated or mis-resolved entry would still have bytes in it.
	if !strings.Contains(body, "Blast radius") {
		t.Fatalf("the skill did not come from the example's SKILL.md; got %d bytes", len(body))
	}
}

// What an agent working an incident actually sees.
func TestExamplePluginHooksReachAnAgentAsTools(t *testing.T) {
	servers := exampleServers{
		sentinel: quietServer(t, func(http.ResponseWriter, *http.Request) {}),
		metrics:  metricsServer(t),
	}
	installExamplePlugin(t, servers)

	tools, err := testHandler.PluginService.AgentHookTools(context.Background(), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("agent hook tools: %v", err)
	}
	byHook := make(map[string]service.PluginHookTool, len(tools))
	for _, tool := range tools {
		byHook[tool.HookKey] = tool
	}

	for _, hookKey := range []string{"correlate_deploys", "request_rollback"} {
		if _, ok := byHook[hookKey]; !ok {
			t.Fatalf("hook %q declares the agent trigger but is not offered as a tool", hookKey)
		}
	}
	// page_oncall is event-only. An agent being able to page on-call by
	// choosing to would defeat the point of the trigger being separate.
	if _, offered := byHook["page_oncall"]; offered {
		t.Fatal("page_oncall is event-only and must never appear in an agent's tool list")
	}

	rollback := byHook["request_rollback"]
	// The description is the manifest's verbatim, because that text is how the
	// agent decides which tool to reach for.
	if !strings.Contains(rollback.Description, "does NOT roll anything back") {
		t.Fatalf("the tool description must be the manifest's own text, got: %q", rollback.Description)
	}
	if !strings.HasPrefix(rollback.Name, "deploy_sentinel_") || !strings.HasSuffix(rollback.Name, "__request_rollback") {
		t.Fatalf("tool name %q is not namespaced by plugin", rollback.Name)
	}
	if len(rollback.InputSchema) == 0 || !strings.Contains(string(rollback.InputSchema), "deploy_id") {
		t.Fatalf("the manifest's input schema did not reach the agent: %s", rollback.InputSchema)
	}
}

// The whole round trip: an agent calls the tool, Multica signs the request, the
// plugin's own server sees that signature and answers, and the answer comes
// back. Nothing is mocked except the author's business logic, which is theirs.
func TestExamplePluginAgentHookRoundTripIsSigned(t *testing.T) {
	var seenSignature, seenTimestamp, seenHook, seenTrigger string
	var seenConfig map[string]any

	sentinel := quietServer(t, func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			HookKey string         `json:"hook_key"`
			Trigger string         `json:"trigger"`
			Config  map[string]any `json:"config"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		seenSignature = r.Header.Get("X-Multica-Signature")
		seenTimestamp = r.Header.Get("X-Multica-Timestamp")
		seenHook, seenTrigger, seenConfig = payload.HookKey, payload.Trigger, payload.Config

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"deploy_count":1,"summary":"1 deploy to %vcheckout-api"}`, payload.Config["service_prefix"])
	})
	servers := exampleServers{sentinel: sentinel, metrics: metricsServer(t)}

	installationID := installExamplePlugin(t, servers)
	configureExamplePlugin(t, installationID)

	input := json.RawMessage(`{"service":"checkout-api","window_minutes":120}`)
	result, err := testHandler.PluginService.InvokeAgentHook(
		context.Background(), installationID, "correlate_deploys", parseUUID(testUserID), input)
	if err != nil {
		t.Fatalf("agent hook invocation: %v", err)
	}

	if seenHook != "correlate_deploys" || seenTrigger != "agent" {
		t.Fatalf("the plugin saw hook=%q trigger=%q", seenHook, seenTrigger)
	}
	if seenSignature == "" || seenTimestamp == "" {
		t.Fatal("the request reached the plugin unsigned")
	}
	// The plugin's configuration travels with the call — its business rules
	// depend on it, and it must not have to keep a second copy.
	if seenConfig["service_prefix"] != "prod-" {
		t.Fatalf("config did not reach the hook: %v", seenConfig)
	}
	// Structural, not behavioural: a secret-typed value is written to a
	// different table and never reaches installation.Config, so this cannot
	// fail today. It is here for the day somebody merges those two tables.
	if _, leaked := seenConfig["sentinel_token"]; leaked {
		t.Fatal("a secret config value was sent to the hook endpoint")
	}
	if result.Status != "ok" {
		t.Fatalf("hook result status = %q, want ok", result.Status)
	}
	body, _ := json.Marshal(result.Output)
	if !strings.Contains(string(body), "prod-checkout-api") {
		t.Fatalf("the plugin's answer did not come back to the agent: %s", body)
	}
}

// Discovery lists what the metrics server offers; approval is what makes any of
// it callable. That gap is the difference between this transport and http.
func TestExamplePluginMCPToolsNeedApproval(t *testing.T) {
	servers := exampleServers{
		sentinel: quietServer(t, func(http.ResponseWriter, *http.Request) {}),
		metrics:  metricsServer(t),
	}
	installationID := installExamplePlugin(t, servers)

	recorder := httptest.NewRecorder()
	testHandler.ListPluginMCPTools(recorder, pluginHandlerRequest(http.MethodGet, "/mcp/tools", nil, map[string]string{
		"id": testWorkspaceID, "installationId": installationID, "hookKey": "metrics",
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("discover: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var discovered struct {
		Tools []struct {
			Name     string `json:"name"`
			Approved bool   `json:"approved"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &discovered); err != nil {
		t.Fatalf("decode tools: %v", err)
	}
	if len(discovered.Tools) != 3 {
		t.Fatalf("discovered %d tools, want the metrics server's 3", len(discovered.Tools))
	}
	for _, tool := range discovered.Tools {
		if tool.Approved {
			t.Fatalf("tool %q reported as approved before anybody approved it", tool.Name)
		}
	}

	// Nothing approved yet, so an agent gets no connection at all.
	connections, err := testHandler.PluginService.AgentMCPConnections(context.Background(), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("agent mcp connections: %v", err)
	}
	if len(connections) != 0 {
		t.Fatalf("an unapproved mcp hook produced %d connections", len(connections))
	}

	approve, _ := json.Marshal(map[string]any{"tools": []string{"query_timeseries", "list_alerts"}})
	recorder = httptest.NewRecorder()
	testHandler.ApprovePluginMCPTools(recorder, pluginHandlerRequest(http.MethodPut, "/mcp/tools", approve, map[string]string{
		"id": testWorkspaceID, "installationId": installationID, "hookKey": "metrics",
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("approve: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	connections, err = testHandler.PluginService.AgentMCPConnections(context.Background(), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("agent mcp connections after approval: %v", err)
	}
	if len(connections) != 1 {
		t.Fatalf("got %d connections after approval, want 1", len(connections))
	}
	pinned := map[string]bool{}
	for _, tool := range connections[0].ApprovedTools {
		pinned[tool.Name] = true
		if tool.SchemaDigest == "" {
			t.Fatalf("approved tool %q was pinned without a schema digest", tool.Name)
		}
	}
	// The third tool was discovered and deliberately not ticked. If it rode
	// along, the approval step would be decorative.
	if len(pinned) != 2 || !pinned["query_timeseries"] || !pinned["list_alerts"] {
		t.Fatalf("pinned tools = %v, want exactly the two that were approved", pinned)
	}
}

// serveMetricsMCP mirrors examples/plugins/deploy-sentinel/server/metrics-mcp.mjs.
// The .mjs file is what a person runs by hand; this is what CI runs, so the two
// must offer the same tools.
func serveMetricsMCP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.NewDecoder(r.Body).Decode(&request)
	w.Header().Set("Content-Type", "application/json")

	send := func(result any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
	switch request.Method {
	case "initialize":
		send(map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "deploy-sentinel-metrics", "version": "1.0.0"},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		send(map[string]any{"tools": []map[string]any{
			{"name": "query_timeseries", "description": "Query a metric over a time window.",
				"inputSchema": map[string]any{"type": "object", "required": []string{"metric", "window_minutes"}}},
			{"name": "list_alerts", "description": "Alerts that fired in a window.",
				"inputSchema": map[string]any{"type": "object"}},
			{"name": "service_dependencies", "description": "Which services this one calls.",
				"inputSchema": map[string]any{"type": "object", "required": []string{"service"}}},
		}})
	default:
		send(map[string]any{})
	}
}
