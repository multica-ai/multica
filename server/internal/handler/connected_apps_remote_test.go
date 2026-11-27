package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/connectedapps"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

const remoteTestBase = "https://api.multica.test"

// remoteFakeUpstream is a remote MCP app and its authorization server.
type remoteFakeUpstream struct {
	mu            sync.Mutex
	verifiers     []string
	refreshed     []string
	calls         []string
	tokens        []string
	issued        int
	refreshErr    error
	rejectTokenOf string
	registered    int
	onRefresh     func()
}

func (f *remoteFakeUpstream) DiscoverOAuth(context.Context, string) (remotemcp.OAuthMetadata, error) {
	return remotemcp.OAuthMetadata{
		ResourceEndpoint:      "https://api.mobbin.com/mcp",
		AuthorizationEndpoint: "https://auth.mobbin.test/authorize",
		TokenEndpoint:         "https://auth.mobbin.test/token",
		RegistrationEndpoint:  "https://auth.mobbin.test/register",
		Scopes:                []string{"openid"},
	}, nil
}

func (f *remoteFakeUpstream) Register(context.Context, remotemcp.OAuthMetadata, string) (remotemcp.OAuthClientRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered++
	return remotemcp.OAuthClientRegistration{ClientID: "client-1", TokenEndpointAuthMethod: "none"}, nil
}

func (f *remoteFakeUpstream) issue() remotemcp.OAuthTokenResponse {
	f.issued++
	n := string(rune('0' + f.issued))
	return remotemcp.OAuthTokenResponse{AccessToken: "access-" + n, RefreshToken: "refresh-" + n, ExpiresIn: 3600, TokenType: "Bearer"}
}

func (f *remoteFakeUpstream) Exchange(_ context.Context, _, _, code, _, verifier string, _ remotemcp.OAuthClientRegistration) (remotemcp.OAuthTokenResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifiers = append(f.verifiers, verifier)
	return f.issue(), nil
}

func (f *remoteFakeUpstream) Refresh(_ context.Context, _, _, refreshToken string, _ remotemcp.OAuthClientRegistration) (remotemcp.OAuthTokenResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshed = append(f.refreshed, refreshToken)
	if f.onRefresh != nil {
		f.onRefresh()
	}
	if f.refreshErr != nil {
		return remotemcp.OAuthTokenResponse{}, f.refreshErr
	}
	return f.issue(), nil
}

func (f *remoteFakeUpstream) ListTools(_ context.Context, _, token string) ([]json.RawMessage, error) {
	return []json.RawMessage{
		json.RawMessage(`{"name":"search_screens","description":"Search app screens","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}}`),
		json.RawMessage(`{"name":"save_screen","description":"Save a screen to a collection","inputSchema":{"type":"object"}}`),
	}, nil
}

func (f *remoteFakeUpstream) CallTool(_ context.Context, _, token, name string, _ map[string]any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	f.tokens = append(f.tokens, token)
	if f.rejectTokenOf != "" && token == f.rejectTokenOf {
		return nil, &remotemcp.StatusError{Code: http.StatusUnauthorized}
	}
	return json.RawMessage(`{"content":[{"type":"image","data":"AAA","mimeType":"image/png"}]}`), nil
}

type remoteAppsFixture struct {
	*appsFixture
	up *remoteFakeUpstream
}

func newRemoteAppsFixture(t *testing.T, opts ...func(*connectedapps.Config)) *remoteAppsFixture {
	t.Helper()
	f := newAppsFixture(t)
	box, err := secretbox.New(bytes.Repeat([]byte{7}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	up := &remoteFakeUpstream{}
	cfg := connectedapps.Config{
		Queries:        f.h.Queries,
		Composio:       f.h.Composio,
		TokenSecret:    connectedapps.DeriveTokenSecret([]byte("apps-test-secret")),
		PublicBaseURL:  remoteTestBase,
		SecretBox:      box,
		DB:             testPool,
		RemoteUpstream: up,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	apps, err := connectedapps.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	apps.SetResumer(f.h)
	apps.SetTeamConnectGate(f.h.TeamAppConnectGate)
	f.h.ConnectedApps = apps
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM remote_app_connection WHERE owner_id = ANY($1::uuid[])`, []string{testUserID, testWorkspaceID, f.member})
		testPool.Exec(ctx, `DELETE FROM remote_app_oauth_client WHERE redirect_uri = $1`, remoteTestBase+connectedapps.RemoteCallbackPath)
	})
	return &remoteAppsFixture{appsFixture: f, up: up}
}

// connect runs the personal connect handshake for userID and returns the
// callback's redirect.
func (f *remoteAppsFixture) connect(t *testing.T, userID string) string {
	t.Helper()
	w := httptest.NewRecorder()
	f.h.ConnectPersonalApp(w, appsReqAs(t, userID, http.MethodPost, "/api/apps/personal/connect", map[string]string{"toolkit_slug": "mobbin"}))
	if w.Code != http.StatusOK {
		t.Fatalf("connect: %d %s", w.Code, w.Body.String())
	}
	var init ComposioConnectInitResponse
	json.Unmarshal(w.Body.Bytes(), &init)
	authorize, err := url.Parse(init.RedirectURL)
	if err != nil || authorize.Host != "auth.mobbin.test" {
		t.Fatalf("redirect_url = %q", init.RedirectURL)
	}
	q := authorize.Query()
	if q.Get("client_id") != "client-1" || q.Get("resource") != "https://api.mobbin.com/mcp" ||
		q.Get("redirect_uri") != remoteTestBase+connectedapps.RemoteCallbackPath || q.Get("scope") != "openid" {
		t.Fatalf("authorize query = %v", q)
	}
	code := f.callback(t, "state="+url.QueryEscape(q.Get("state"))+"&code=auth-code")
	f.up.mu.Lock()
	verifier := f.up.verifiers[len(f.up.verifiers)-1]
	f.up.mu.Unlock()
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != q.Get("code_challenge") {
		t.Fatal("the code exchange did not use the verifier behind the PKCE challenge")
	}
	return code
}

func (f *remoteAppsFixture) callback(t *testing.T, rawQuery string) string {
	t.Helper()
	w := httptest.NewRecorder()
	f.h.AppsOAuthCallback(w, httptest.NewRequest(http.MethodGet, connectedapps.RemoteCallbackPath+"?"+rawQuery, nil))
	if w.Code != http.StatusFound {
		t.Fatalf("callback: %d %s", w.Code, w.Body.String())
	}
	return w.Header().Get("Location")
}

// remote sends one JSON-RPC request to the app's gateway server.
func (f *remoteAppsFixture) remote(t *testing.T, token, method string, params any) (int, json.RawMessage) {
	t.Helper()
	return f.remoteCtx(t, context.Background(), token, method, params)
}

func (f *remoteAppsFixture) remoteCtx(t *testing.T, ctx context.Context, token, method string, params any) (int, json.RawMessage) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, connectedapps.GatewayPath+"/mobbin", bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.h.AppsRemoteGateway(w, withURLParam(req, "app", "mobbin"))
	if w.Code != http.StatusOK {
		return w.Code, nil
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return w.Code, resp.Result
}

func (f *remoteAppsFixture) toolNames(t *testing.T, token string) []string {
	t.Helper()
	_, raw := f.remote(t, token, "tools/list", map[string]any{})
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	json.Unmarshal(raw, &result)
	names := []string{}
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func (f *remoteAppsFixture) personalStatus(t *testing.T) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(), `
		SELECT status FROM remote_app_connection WHERE app_slug = 'mobbin' AND scope = 'personal' AND owner_id = $1
	`, testUserID).Scan(&status); err != nil {
		return ""
	}
	return status
}

func TestRemoteApps_ConnectMountAndRun(t *testing.T) {
	f := newRemoteAppsFixture(t)

	w := httptest.NewRecorder()
	f.h.ListAppCatalog(w, appsReqAs(t, testUserID, http.MethodGet, "/api/apps/catalog", nil))
	var catalog struct {
		Apps []AppCatalogEntry `json:"apps"`
	}
	json.Unmarshal(w.Body.Bytes(), &catalog)
	if len(catalog.Apps) == 0 || catalog.Apps[0].Slug != "mobbin" || !catalog.Apps[0].Curated || catalog.Apps[0].RecommendedScope != "personal" {
		t.Fatalf("catalog = %+v", catalog.Apps)
	}

	if loc := f.connect(t, testUserID); !strings.Contains(loc, "tab=apps") || !strings.Contains(loc, "connected=mobbin") {
		t.Fatalf("callback redirect = %q", loc)
	}
	var sealed []byte
	testPool.QueryRow(context.Background(), `SELECT access_token FROM remote_app_connection WHERE owner_id = $1 AND app_slug = 'mobbin'`, testUserID).Scan(&sealed)
	if len(sealed) == 0 || bytes.Contains(sealed, []byte("access-")) {
		t.Fatal("the access token must be stored sealed")
	}

	w = httptest.NewRecorder()
	f.h.GetAppsOverview(w, appsReqAs(t, testUserID, http.MethodGet, "/api/apps", nil))
	var overview AppsOverviewResponse
	json.Unmarshal(w.Body.Bytes(), &overview)
	found := false
	for _, c := range overview.Personal {
		found = found || (c.ToolkitSlug == "mobbin" && c.Status == "active")
	}
	if !found {
		t.Fatalf("overview personal = %+v", overview.Personal)
	}

	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "mobbin", "account_scope": "personal", "level": "read"}}); w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentApps: %d %s", w.Code, w.Body.String())
	}
	agent, _ := f.h.Queries.GetAgent(context.Background(), parseUUID(f.agentID))
	overlay, err := f.h.ConnectedApps.BuildTaskOverlay(context.Background(), parseUUID(testUserID), agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlay.ConnectedApps) != 1 || overlay.ConnectedApps[0].Provider != connectedapps.RemoteProvider ||
		overlay.ConnectedApps[0].ServerName != "mobbin" || overlay.ConnectedApps[0].AccountStatus != "connected" {
		t.Fatalf("connected apps = %+v", overlay.ConnectedApps)
	}
	authorized, err := f.h.ConnectedApps.AuthorizeOverlay(overlay.MCPOverlay, parseUUID(f.taskID))
	if err != nil {
		t.Fatal(err)
	}
	var servers struct {
		MCPServers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	json.Unmarshal(authorized, &servers)
	if servers.MCPServers["mobbin"].URL != remoteTestBase+"/api/apps/mcp/mobbin" ||
		!strings.HasPrefix(servers.MCPServers["mobbin"].Headers["Authorization"], "Bearer ") ||
		servers.MCPServers[connectedapps.GatewayServerName].Headers["Authorization"] == "" {
		t.Fatalf("authorized overlay = %s", authorized)
	}

	token := f.token(t)
	if code, _ := f.remote(t, "", "tools/list", map[string]any{}); code != http.StatusUnauthorized {
		t.Fatalf("remote gateway without token: %d, want 401", code)
	}
	// A read-level agent sees only the read tool.
	if names := f.toolNames(t, token); len(names) != 1 || names[0] != "search_screens" {
		t.Fatalf("read-level tools = %v", names)
	}
	_, result := f.remote(t, token, "tools/call", map[string]any{"name": "search_screens", "arguments": map[string]any{"query": "onboarding"}})
	if !strings.Contains(string(result), `"type":"image"`) {
		t.Fatalf("result must pass through unchanged: %s", result)
	}
	_, blocked := f.remote(t, token, "tools/call", map[string]any{"name": "save_screen", "arguments": map[string]any{}})
	if !strings.Contains(string(blocked), "blocked") {
		t.Fatalf("write at read level = %s", blocked)
	}
	var outcomes []string
	rows, _ := testPool.Query(context.Background(), `SELECT tool_slug || ':' || outcome FROM app_tool_call WHERE task_id = $1 ORDER BY created_at`, f.taskID)
	for rows.Next() {
		var s string
		rows.Scan(&s)
		outcomes = append(outcomes, s)
	}
	rows.Close()
	if strings.Join(outcomes, ",") != "search_screens:ok,save_screen:blocked" {
		t.Fatalf("recorded calls = %v", outcomes)
	}

	if _, text, _ := f.gateway(t, token, "apps_list", nil); !strings.Contains(text, `"server": "mobbin"`) {
		t.Fatalf("apps_list = %s", text)
	}
}

func TestRemoteApps_RefreshExpiryAndReconnect(t *testing.T) {
	f := newRemoteAppsFixture(t)
	f.connect(t, testUserID)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "mobbin", "account_scope": "personal", "level": "write"}})
	token := f.token(t)
	call := func() string {
		_, result := f.remote(t, token, "tools/call", map[string]any{"name": "search_screens", "arguments": map[string]any{}})
		return string(result)
	}

	// Near expiry: refreshed before the call, with the single-use refresh
	// token replaced.
	testPool.Exec(context.Background(), `UPDATE remote_app_connection SET expires_at = now() + interval '30 seconds' WHERE owner_id = $1`, testUserID)
	if res := call(); !strings.Contains(res, "image") {
		t.Fatalf("call after refresh = %s", res)
	}
	if len(f.up.refreshed) != 1 || f.up.refreshed[0] != "refresh-1" || f.up.tokens[len(f.up.tokens)-1] != "access-2" {
		t.Fatalf("refreshed = %v tokens = %v", f.up.refreshed, f.up.tokens)
	}

	// The app rejects a token it should accept: one refresh, then a retry.
	f.up.rejectTokenOf = "access-2"
	if res := call(); !strings.Contains(res, "image") {
		t.Fatalf("call after 401 = %s", res)
	}
	if len(f.up.refreshed) != 2 || f.up.refreshed[1] != "refresh-2" || f.up.tokens[len(f.up.tokens)-1] != "access-3" {
		t.Fatalf("refreshed = %v tokens = %v", f.up.refreshed, f.up.tokens)
	}

	// The refresh token is refused: the account expires and the agent is
	// told to ask for a new connection.
	f.up.refreshErr = &remotemcp.OAuthError{Status: http.StatusBadRequest, Code: "invalid_grant"}
	testPool.Exec(context.Background(), `UPDATE remote_app_connection SET expires_at = now() - interval '1 minute' WHERE owner_id = $1`, testUserID)
	if res := call(); !strings.Contains(res, "apps_request_connection") {
		t.Fatalf("call with refused refresh = %s", res)
	}
	if status := f.personalStatus(t); status != "expired" {
		t.Fatalf("status = %q, want expired", status)
	}
	var leftover []byte
	testPool.QueryRow(context.Background(), `SELECT refresh_token FROM remote_app_connection WHERE owner_id = $1 AND app_slug = 'mobbin'`, testUserID).Scan(&leftover)
	if leftover != nil {
		t.Fatal("an expired account must not keep its refresh token")
	}
	if names := f.toolNames(t, token); len(names) != 0 {
		t.Fatalf("tools of an expired account = %v", names)
	}

	_, text, isErr := f.gateway(t, token, "apps_request_connection", map[string]any{"app": "mobbin", "summary": "Find onboarding patterns"})
	if isErr || !strings.HasPrefix(text, "requested") {
		t.Fatalf("request connection = %s (error %v)", text, isErr)
	}

	// Reconnecting resolves the request and resumes the agent.
	f.up.refreshErr = nil
	f.connect(t, testUserID)
	if status := f.personalStatus(t); status != "active" {
		t.Fatalf("status after reconnect = %q", status)
	}
	if reqs := f.listRequests(t, testUserID); len(reqs) != 1 || reqs[0].Status != "approved" {
		t.Fatalf("connect request after reconnecting = %+v", reqs)
	}
	var note string
	if err := testPool.QueryRow(context.Background(), `
		SELECT handoff_note FROM agent_task_queue
		WHERE agent_id = $1 AND issue_id = $2 AND status = 'queued' AND trigger_evidence_kind = 'app_action_request'
	`, f.agentID, f.issueID).Scan(&note); err != nil || !strings.Contains(note, "now connected") {
		t.Fatalf("resume run = %q, %v", note, err)
	}
}

func TestRemoteApps_RefreshSurvivesCallerAndDeadClient(t *testing.T) {
	f := newRemoteAppsFixture(t)
	f.connect(t, testUserID)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "mobbin", "account_scope": "personal", "level": "write"}})
	token := f.token(t)
	expireSoon := func() {
		testPool.Exec(context.Background(), `UPDATE remote_app_connection SET expires_at = now() - interval '1 minute' WHERE owner_id = $1`, testUserID)
	}
	search := map[string]any{"name": "search_screens", "arguments": map[string]any{}}

	// A Composio-style guess on multica_apps points at the app's own server.
	for _, tool := range []string{"apps_get_tool", "apps_run_tool"} {
		if _, text, _ := f.gateway(t, token, tool, map[string]any{"tool": "MOBBIN_SEARCH_SCREENS"}); !strings.Contains(text, `MCP server "mobbin"`) {
			t.Fatalf("%s = %s", tool, text)
		}
	}

	// The caller goes away after the app has rotated the single-use refresh
	// token: the new pair is still saved, so the next call needs no refresh.
	expireSoon()
	ctx, cancel := context.WithCancel(context.Background())
	f.up.onRefresh = cancel
	f.remoteCtx(t, ctx, token, "tools/call", search)
	f.up.onRefresh = nil
	if _, res := f.remote(t, token, "tools/call", search); !strings.Contains(string(res), "image") {
		t.Fatalf("call after a cancelled refresh = %s", res)
	}
	if len(f.up.refreshed) != 1 || f.up.tokens[len(f.up.tokens)-1] != "access-2" {
		t.Fatalf("refreshed = %v tokens = %v; the rotated tokens were lost", f.up.refreshed, f.up.tokens)
	}

	// The authorization server forgot the client: the account expires and the
	// client is dropped, so reconnecting registers a new one.
	f.up.refreshErr = &remotemcp.OAuthError{Status: http.StatusUnauthorized, Code: "invalid_client"}
	expireSoon()
	if _, res := f.remote(t, token, "tools/call", search); !strings.Contains(string(res), "apps_request_connection") {
		t.Fatalf("call with a dead client = %s", res)
	}
	var clients int
	testPool.QueryRow(context.Background(), `SELECT count(*) FROM remote_app_oauth_client WHERE redirect_uri = $1`, remoteTestBase+connectedapps.RemoteCallbackPath).Scan(&clients)
	if status := f.personalStatus(t); status != "expired" || clients != 0 {
		t.Fatalf("status = %q, clients = %d; want expired and none", status, clients)
	}
	f.up.refreshErr = nil
	f.connect(t, testUserID)
	if f.up.registered != 2 || f.personalStatus(t) != "active" {
		t.Fatalf("registrations = %d, status = %q", f.up.registered, f.personalStatus(t))
	}
}

func TestRemoteApps_AccountIsolation(t *testing.T) {
	f := newRemoteAppsFixture(t)
	f.connect(t, testUserID)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "mobbin", "account_scope": "personal", "level": "write"}})

	// A run another member triggered never acts as this member's account.
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET originator_user_id = $1, accountable_user_id = $1 WHERE id = $2`, f.member, f.taskID); err != nil {
		t.Fatal(err)
	}
	token := f.token(t)
	_, result := f.remote(t, token, "tools/call", map[string]any{"name": "search_screens", "arguments": map[string]any{}})
	if !strings.Contains(string(result), "not connected") || len(f.up.calls) != 0 {
		t.Fatalf("other member's run = %s, upstream calls %v", result, f.up.calls)
	}

	var id string
	testPool.QueryRow(context.Background(), `SELECT id FROM remote_app_connection WHERE owner_id = $1 AND app_slug = 'mobbin'`, testUserID).Scan(&id)
	w := httptest.NewRecorder()
	f.h.DeleteComposioConnection(w, withURLParam(appsReqAs(t, f.member, http.MethodDelete, "/api/integrations/composio/connections/"+id, nil), "id", id))
	if w.Code != http.StatusNotFound || f.personalStatus(t) != "active" {
		t.Fatalf("disconnect by another member: %d, status %s", w.Code, f.personalStatus(t))
	}
	w = httptest.NewRecorder()
	f.h.ConnectTeamApp(w, appsReqAs(t, f.member, http.MethodPost, "/api/apps/team/connect", map[string]string{"toolkit_slug": "mobbin"}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("team connect by a member: %d, want 403", w.Code)
	}

	// A tampered state writes nothing and fails generically.
	before := f.up.issued
	if loc := f.callback(t, "state=forged&code=x"); !strings.Contains(loc, "error=composio_connect_failed") || f.up.issued != before {
		t.Fatalf("forged callback = %q", loc)
	}

	w = httptest.NewRecorder()
	f.h.DeleteComposioConnection(w, withURLParam(appsReqAs(t, testUserID, http.MethodDelete, "/api/integrations/composio/connections/"+id, nil), "id", id))
	if w.Code != http.StatusNoContent || f.personalStatus(t) != "revoked" {
		t.Fatalf("own disconnect: %d, status %s", w.Code, f.personalStatus(t))
	}
	var access []byte
	testPool.QueryRow(context.Background(), `SELECT access_token FROM remote_app_connection WHERE id = $1`, id).Scan(&access)
	if access != nil {
		t.Fatal("a revoked account must not keep its access token")
	}
}

// Without the apps secret key, remote apps are off and nothing changes for
// Composio apps.
func TestRemoteApps_OffWithoutKey(t *testing.T) {
	f := newAppsFixture(t)
	if _, ok := f.h.ConnectedApps.RemoteApp("mobbin"); ok {
		t.Fatal("remote apps must be off without a secret box")
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/apps/mcp/mobbin", strings.NewReader(`{}`))
	f.h.AppsRemoteGateway(w, withURLParam(req, "app", "mobbin"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("remote gateway without remote apps: %d, want 404", w.Code)
	}
}

// connectTeam runs the team connect handshake as an admin.
func (f *remoteAppsFixture) connectTeam(t *testing.T) {
	t.Helper()
	w := httptest.NewRecorder()
	f.h.ConnectTeamApp(w, appsReqAs(t, testUserID, http.MethodPost, "/api/apps/team/connect", map[string]string{"toolkit_slug": "mobbin"}))
	if w.Code != http.StatusOK {
		t.Fatalf("team connect: %d %s", w.Code, w.Body.String())
	}
	var init ComposioConnectInitResponse
	json.Unmarshal(w.Body.Bytes(), &init)
	authorize, err := url.Parse(init.RedirectURL)
	if err != nil {
		t.Fatalf("redirect_url = %q", init.RedirectURL)
	}
	f.callback(t, "state="+url.QueryEscape(authorize.Query().Get("state"))+"&code=auth-code")
}

// A connected remote team account is the workspace's account for the app:
// reconnecting it is a reauthorization a plan without team accounts allows,
// and members cannot ask an admin for it again.
func TestRemoteApps_TeamAccountIsTheWorkspaces(t *testing.T) {
	f := newRemoteAppsFixture(t)
	f.connectTeam(t)

	usePaidFeatureOn(t, f.h, entitlement.FeatureTeamAppAccounts, enforced())
	w := httptest.NewRecorder()
	f.h.ConnectTeamApp(w, appsReqAs(t, testUserID, http.MethodPost, "/api/apps/team/connect", map[string]string{"toolkit_slug": "mobbin"}))
	if w.Code != http.StatusOK {
		t.Fatalf("reconnecting the team account under enforcement: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	f.h.RequestTeamApp(w, appsReqAs(t, f.member, http.MethodPost, "/api/apps/team/requests", map[string]string{"toolkit_slug": "mobbin"}))
	if w.Code != http.StatusConflict {
		t.Fatalf("asking for a connected team account: %d %s, want 409", w.Code, w.Body.String())
	}
}

// Disconnecting a personal remote account first lists the agents that lose it.
func TestRemoteApps_PersonalDisconnectImpact(t *testing.T) {
	f := newRemoteAppsFixture(t)
	f.connect(t, testUserID)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "mobbin", "account_scope": "personal", "level": "write"}})
	var id string
	testPool.QueryRow(context.Background(), `SELECT id FROM remote_app_connection WHERE owner_id = $1 AND app_slug = 'mobbin'`, testUserID).Scan(&id)
	impact := func(userID string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.h.GetComposioConnectionImpact(w, withURLParam(appsReqAs(t, userID, http.MethodGet, "/api/integrations/composio/connections/"+id+"/impact", nil), "id", id))
		return w
	}

	w := impact(testUserID)
	var resp struct {
		Agents []ComposioImpactAgent `json:"agents"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusOK || len(resp.Agents) != 1 || resp.Agents[0].ID != f.agentID || resp.Agents[0].WorkspaceID != testWorkspaceID {
		t.Fatalf("impact: %d %s", w.Code, w.Body.String())
	}
	if w := impact(f.member); w.Code != http.StatusNotFound {
		t.Fatalf("another member's impact check: %d, want 404", w.Code)
	}
}
