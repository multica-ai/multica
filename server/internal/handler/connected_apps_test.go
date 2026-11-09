package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/connectedapps"
	composio "github.com/multica-ai/multica/server/internal/integrations/composio"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// appsFakeSDK serves a small Gmail toolkit and records executions.
type appsFakeSDK struct {
	composioFakeSDK
	mu         sync.Mutex
	executed   []sdk.ExecuteToolRequest
	executeErr error
	toolError  string
}

var appsFakeGmailTools = []sdk.Tool{
	{Slug: "GMAIL_FETCH_EMAILS", Name: "Fetch emails", Description: "List recent emails", Toolkit: sdk.Toolkit{Slug: "gmail"}},
	{Slug: "GMAIL_SEND_EMAIL", Name: "Send email", Description: "Send an email", Toolkit: sdk.Toolkit{Slug: "gmail"}},
	{Slug: "GMAIL_DELETE_MESSAGE", Name: "Delete message", Description: "Delete an email", Toolkit: sdk.Toolkit{Slug: "gmail"}},
}

func (f *appsFakeSDK) GetToolkit(_ context.Context, slug string) (*sdk.Toolkit, error) {
	return &sdk.Toolkit{Slug: slug, Name: slug}, nil
}

func (f *appsFakeSDK) ListTools(_ context.Context, req sdk.ListToolsRequest) (*sdk.ListToolsResponse, error) {
	if req.ToolkitSlug != "gmail" {
		return &sdk.ListToolsResponse{}, nil
	}
	return &sdk.ListToolsResponse{Items: appsFakeGmailTools}, nil
}

func (f *appsFakeSDK) GetTool(_ context.Context, slug string) (*sdk.Tool, error) {
	for i := range appsFakeGmailTools {
		if appsFakeGmailTools[i].Slug == slug {
			return &appsFakeGmailTools[i], nil
		}
	}
	return nil, fmt.Errorf("tool %s not found", slug)
}

func (f *appsFakeSDK) ExecuteTool(_ context.Context, slug string, req sdk.ExecuteToolRequest) (*sdk.ExecuteToolResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executed = append(f.executed, req)
	if req.UserID == "" || req.ConnectedAccountID == "" {
		return nil, errors.New("User ID is required with connected account")
	}
	if f.executeErr != nil {
		return nil, f.executeErr
	}
	if f.toolError != "" {
		return &sdk.ExecuteToolResponse{Error: f.toolError}, nil
	}
	return &sdk.ExecuteToolResponse{Successful: true, Data: map[string]any{"tool": slug}}, nil
}

func (f *appsFakeSDK) CreateManagedAuthConfig(_ context.Context, slug string) (string, error) {
	return "ac_" + slug, nil
}

func (f *appsFakeSDK) executions() []sdk.ExecuteToolRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sdk.ExecuteToolRequest(nil), f.executed...)
}

type appsFixture struct {
	h       *Handler
	sdk     *appsFakeSDK
	agentID string
	issueID string
	taskID  string
	member  string
}

func newAppsFixture(t *testing.T) *appsFixture {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}
	fake := &appsFakeSDK{}
	svc, err := composio.NewService(fake, testHandler.Queries, composio.Config{
		StateSecret:     []byte("apps-test-secret"),
		CallbackBaseURL: "https://multica.ai",
		FrontendBaseURL: "https://multica.ai",
	})
	if err != nil {
		t.Fatalf("composio.NewService: %v", err)
	}
	h := *testHandler
	h.Composio = svc
	apps, err := connectedapps.New(connectedapps.Config{
		Queries:       h.Queries,
		Composio:      svc,
		TokenSecret:   connectedapps.DeriveTokenSecret([]byte("apps-test-secret")),
		PublicBaseURL: "https://api.multica.test",
	})
	if err != nil {
		t.Fatalf("connectedapps.New: %v", err)
	}
	apps.SetResumer(&h)
	h.ConnectedApps = apps
	withComposioMCPAppsFlag(t, &h, true)

	agentID := createHandlerTestAgent(t, fmt.Sprintf("apps-agent-%d", testSeq.Add(1)), nil)
	issueID := createTestIssue(t, "apps confirmation test", "todo", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET originator_user_id = $1, accountable_user_id = $1, originator_source = 'direct_human' WHERE id = $2`, testUserID, taskID); err != nil {
		t.Fatalf("set originator: %v", err)
	}

	account := fmt.Sprintf("ca_apps_test_%d", testSeq.Add(1))
	if _, err := testHandler.Queries.UpsertUserComposioConnection(context.Background(), db.UpsertUserComposioConnectionParams{
		UserID:             parseUUID(testUserID),
		ToolkitSlug:        "gmail",
		AuthConfigID:       "ac_gmail",
		ConnectedAccountID: account,
		ComposioUserID:     testUserID,
	}); err != nil {
		t.Fatalf("seed personal account: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM user_composio_connection WHERE connected_account_id = $1`, account)
		for _, table := range []string{"app_tool_call", "app_action_request", "app_action_rule", "agent_app", "workspace_app_setting", "app_tool_class_override"} {
			testPool.Exec(ctx, `DELETE FROM `+table+` WHERE workspace_id = $1`, testWorkspaceID)
		}
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type LIKE 'app\_%'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
	})

	return &appsFixture{
		h: &h, sdk: fake, agentID: agentID, issueID: issueID, taskID: taskID,
		member: createTestUserAndMember(t, "member"),
	}
}

func appsReqAs(t *testing.T, userID, method, path string, body any) *http.Request {
	t.Helper()
	req := newRequestAs(userID, method, path, body)
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: util.MustParseUUID(userID), WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member %s: %v", userID, err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
}

func (f *appsFixture) putAgentApps(t *testing.T, userID string, apps []map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := appsReqAs(t, userID, http.MethodPut, "/api/agents/"+f.agentID+"/apps", map[string]any{"apps": apps})
	f.h.UpdateAgentApps(w, withURLParam(req, "id", f.agentID))
	return w
}

// gateway sends one tools/call to the MCP gateway with the run's token.
func (f *appsFixture) gateway(t *testing.T, token, tool string, args map[string]any) (int, string, bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	req := httptest.NewRequest(http.MethodPost, connectedapps.GatewayPath, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.h.AppsGateway(w, req)
	if w.Code != http.StatusOK {
		return w.Code, w.Body.String(), false
	}
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || len(resp.Result.Content) == 0 {
		t.Fatalf("decode gateway response %s: %v", w.Body.String(), err)
	}
	return w.Code, resp.Result.Content[0].Text, resp.Result.IsError
}

func (f *appsFixture) token(t *testing.T) string {
	t.Helper()
	token, err := connectedapps.SignTaskToken(connectedapps.DeriveTokenSecret([]byte("apps-test-secret")), f.taskID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

func (f *appsFixture) listRequests(t *testing.T, userID string) []AppRequestResponse {
	t.Helper()
	w := httptest.NewRecorder()
	f.h.ListAppRequests(w, appsReqAs(t, userID, http.MethodGet, "/api/app-requests?issue_id="+f.issueID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAppRequests: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Requests []AppRequestResponse `json:"requests"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.Requests
}

func TestConnectedApps_ConfirmApproveResumes(t *testing.T) {
	f := newAppsFixture(t)

	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "confirm_writes"}}); w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentApps: %d %s", w.Code, w.Body.String())
	}
	var allowlist []string
	testPool.QueryRow(context.Background(), `SELECT composio_toolkit_allowlist FROM agent WHERE id = $1`, f.agentID).Scan(&allowlist)
	if len(allowlist) != 1 || allowlist[0] != "gmail" {
		t.Fatalf("legacy allowlist = %v, want [gmail]", allowlist)
	}

	token := f.token(t)
	if code, _, _ := f.gateway(t, "", "apps_list", nil); code != http.StatusUnauthorized {
		t.Fatalf("gateway without token: %d, want 401", code)
	}
	if _, text, isErr := f.gateway(t, token, "apps_list", nil); isErr || !strings.Contains(text, `"status": "connected"`) {
		t.Fatalf("apps_list = %s (error %v)", text, isErr)
	}

	// A read runs directly.
	if _, text, isErr := f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_FETCH_EMAILS", "arguments": map[string]any{}}); isErr {
		t.Fatalf("read call failed: %s", text)
	}
	if n := len(f.sdk.executions()); n != 1 {
		t.Fatalf("executions after read = %d, want 1", n)
	}

	// A send waits for the person who triggered the run.
	args := map[string]any{"to": "a@example.com", "body": "draft"}
	_, text, isErr := f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": args, "summary": "Reply to the customer"})
	if isErr || !strings.HasPrefix(text, "submitted") {
		t.Fatalf("send call = %s (error %v), want submitted", text, isErr)
	}
	if n := len(f.sdk.executions()); n != 1 {
		t.Fatalf("send executed before approval")
	}
	// Retrying the same call reuses the pending request.
	f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": args})

	mine := f.listRequests(t, testUserID)
	if len(mine) != 1 || mine[0].Status != "pending" || mine[0].Redacted || mine[0].Summary != "Reply to the customer" {
		t.Fatalf("approver view = %+v", mine)
	}
	other := f.listRequests(t, f.member)
	if len(other) != 1 || !other[0].Redacted || other[0].Arguments != nil {
		t.Fatalf("non-approver view not redacted: %+v", other)
	}

	var inboxCount int
	testPool.QueryRow(context.Background(), `SELECT count(*) FROM inbox_item WHERE recipient_id = $1 AND type = 'app_action_requested' AND details->>'request_id' = $2`, testUserID, mine[0].ID).Scan(&inboxCount)
	if inboxCount != 1 {
		t.Fatalf("inbox items = %d, want 1", inboxCount)
	}

	approve := func(userID string, body any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := appsReqAs(t, userID, http.MethodPost, "/api/app-requests/"+mine[0].ID+"/approve", body)
		f.h.ApproveAppRequest(w, withURLParam(req, "id", mine[0].ID))
		return w
	}
	if w := approve(f.member, nil); w.Code != http.StatusForbidden {
		t.Fatalf("approve by non-approver: %d, want 403", w.Code)
	}
	edited := map[string]any{"to": "a@example.com", "body": "edited"}
	w := approve(testUserID, map[string]any{"arguments": edited})
	if w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	execs := f.sdk.executions()
	if len(execs) != 2 || execs[1].Arguments["body"] != "edited" {
		t.Fatalf("approved execution = %+v", execs)
	}
	if w := approve(testUserID, nil); w.Code != http.StatusConflict {
		t.Fatalf("second approve: %d, want 409", w.Code)
	}

	var note string
	if err := testPool.QueryRow(context.Background(), `
		SELECT handoff_note FROM agent_task_queue
		WHERE agent_id = $1 AND issue_id = $2 AND status = 'queued' AND trigger_evidence_kind = 'app_action_request'
	`, f.agentID, f.issueID).Scan(&note); err != nil {
		t.Fatalf("resume run not queued: %v", err)
	}
	if !strings.Contains(note, "approved") || !strings.Contains(note, "edited the arguments") {
		t.Fatalf("handoff note = %q", note)
	}

	// The token dies with the run.
	testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, f.taskID)
	if code, _, _ := f.gateway(t, token, "apps_list", nil); code != http.StatusUnauthorized {
		t.Fatalf("gateway after run ended: %d, want 401", code)
	}
}

func TestConnectedApps_RulesAndLevels(t *testing.T) {
	f := newAppsFixture(t)
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "read"}}); w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentApps: %d %s", w.Code, w.Body.String())
	}
	token := f.token(t)

	// A read-only agent cannot send, whatever the rules say.
	if _, text, isErr := f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": map[string]any{}}); !isErr || !strings.HasPrefix(text, "blocked") {
		t.Fatalf("send at read level = %s, want blocked", text)
	}

	// With write level, the account owner's own rule lets sends through.
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "write"}})
	w := httptest.NewRecorder()
	f.h.UpdateAppRules(w, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/rules?toolkit_slug=gmail&scope=personal",
		map[string]any{"rules": []map[string]string{{"action_class": "send", "policy": "allow"}, {"action_class": "delete", "policy": "block"}}}))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAppRules: %d %s", w.Code, w.Body.String())
	}
	if _, text, isErr := f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": map[string]any{}}); isErr {
		t.Fatalf("allowed send failed: %s", text)
	}
	if _, text, isErr := f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_DELETE_MESSAGE", "arguments": map[string]any{}}); !isErr || !strings.HasPrefix(text, "blocked") {
		t.Fatalf("blocked delete = %s", text)
	}

	// Another member's rules are their own and do not touch this account.
	w = httptest.NewRecorder()
	f.h.GetAppRules(w, appsReqAs(t, f.member, http.MethodGet, "/api/apps/rules?toolkit_slug=gmail&scope=personal", nil))
	var rules struct {
		Rules []AppRuleResponse `json:"rules"`
	}
	json.Unmarshal(w.Body.Bytes(), &rules)
	for _, r := range rules.Rules {
		if r.Custom {
			t.Fatalf("member sees another person's rule: %+v", r)
		}
	}
}

func TestConnectedApps_AdminOnlyTeamSurfaces(t *testing.T) {
	f := newAppsFixture(t)

	w := f.putAgentApps(t, f.member, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "team", "level": "read"}})
	if w.Code != http.StatusForbidden {
		t.Fatalf("member gives agent a team account: %d, want 403", w.Code)
	}

	w = httptest.NewRecorder()
	f.h.UpdateAppRules(w, appsReqAs(t, f.member, http.MethodPut, "/api/apps/rules?toolkit_slug=gmail&scope=team",
		map[string]any{"rules": []map[string]string{{"action_class": "send", "policy": "allow"}}}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("member edits team rules: %d, want 403", w.Code)
	}

	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"connect":  f.h.ConnectTeamApp,
		"settings": f.h.UpdateAppSettings,
		"impact":   f.h.GetTeamAppImpact,
	} {
		w := httptest.NewRecorder()
		call(w, appsReqAs(t, f.member, http.MethodPost, "/api/apps/team", map[string]any{"toolkit_slug": "gmail", "catalog_mode": "curated"}))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s as member: %d, want 403", name, w.Code)
		}
	}

	// Curated mode keeps unlisted apps out of agents.
	w = httptest.NewRecorder()
	f.h.UpdateAppSettings(w, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/settings", map[string]any{"catalog_mode": "curated"}))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAppSettings: %d %s", w.Code, w.Body.String())
	}
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "some_unlisted_app", "account_scope": "personal", "level": "read"}}); w.Code != http.StatusForbidden {
		t.Fatalf("unlisted app in curated mode: %d, want 403", w.Code)
	}
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "team", "level": "read"}}); w.Code != http.StatusOK {
		t.Fatalf("admin gives agent a team account: %d %s", w.Code, w.Body.String())
	}

	// A team app without a connected team account reports not_connected.
	if _, text, _ := f.gateway(t, f.token(t), "apps_list", nil); !strings.Contains(text, `"status": "not_connected"`) {
		t.Fatalf("apps_list = %s, want not_connected", text)
	}
}

func (f *appsFixture) inboxCount(t *testing.T, recipient, notifType string) int {
	t.Helper()
	var n int
	testPool.QueryRow(context.Background(), `SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND recipient_id = $2 AND type = $3`, testWorkspaceID, recipient, notifType).Scan(&n)
	return n
}

func TestConnectedApps_ResultNotifications(t *testing.T) {
	f := newAppsFixture(t)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "read"}})
	token := f.token(t)

	// A block tells the person who triggered the run once, not on every retry.
	for i := 0; i < 2; i++ {
		if _, text, isErr := f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": map[string]any{}}); !isErr || !strings.HasPrefix(text, "blocked") {
			t.Fatalf("send at read level = %s", text)
		}
	}
	if n := f.inboxCount(t, testUserID, connectedapps.NotifActionResult); n != 1 {
		t.Fatalf("block notifications = %d, want 1", n)
	}

	// An unanswered confirmation expires into a result notification.
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "confirm_writes"}})
	if _, text, _ := f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": map[string]any{"to": "x"}}); !strings.HasPrefix(text, "submitted") {
		t.Fatalf("send = %s, want submitted", text)
	}
	testPool.Exec(context.Background(), `UPDATE app_action_request SET expires_at = now() - interval '1 minute' WHERE task_id = $1`, f.taskID)
	f.h.ConnectedApps.Sweep(context.Background())
	if reqs := f.listRequests(t, testUserID); len(reqs) != 1 || reqs[0].Status != "expired" {
		t.Fatalf("requests after sweep = %+v", reqs)
	}
	if n := f.inboxCount(t, testUserID, connectedapps.NotifActionResult); n != 2 {
		t.Fatalf("result notifications after expiry = %d, want 2", n)
	}

	// Muting agent activity mutes results, never confirmations.
	testPool.Exec(context.Background(), `
		INSERT INTO notification_preference (workspace_id, user_id, preferences)
		VALUES ($1, $2, '{"agent_activity":"muted"}')
		ON CONFLICT (workspace_id, user_id) DO UPDATE SET preferences = EXCLUDED.preferences
	`, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM notification_preference WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	})
	f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": map[string]any{"to": "y"}})
	if n := f.inboxCount(t, testUserID, connectedapps.NotifActionRequested); n != 2 {
		t.Fatalf("confirmations with agent activity muted = %d, want 2", n)
	}
	testPool.Exec(context.Background(), `UPDATE app_action_request SET expires_at = now() - interval '1 minute' WHERE task_id = $1 AND status = 'pending'`, f.taskID)
	f.h.ConnectedApps.Sweep(context.Background())
	if n := f.inboxCount(t, testUserID, connectedapps.NotifActionResult); n != 2 {
		t.Fatalf("muted result delivered: %d, want 2", n)
	}
}

func TestConnectedApps_RateLimit(t *testing.T) {
	f := newAppsFixture(t)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "write"}})
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO app_tool_call (workspace_id, agent_id, account_scope, account_owner_user_id, toolkit_slug, tool_slug, action_class, outcome)
		SELECT $1, $2, 'personal', $3, 'gmail', 'GMAIL_FETCH_EMAILS', 'read', 'ok' FROM generate_series(1, $4::int)
	`, testWorkspaceID, f.agentID, testUserID, connectedapps.DefaultCallsPerMinute); err != nil {
		t.Fatalf("seed calls: %v", err)
	}
	_, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_FETCH_EMAILS", "arguments": map[string]any{}})
	if !isErr || !strings.HasPrefix(text, "rate limited") {
		t.Fatalf("call over the limit = %s (error %v)", text, isErr)
	}
	if n := len(f.sdk.executions()); n != 0 {
		t.Fatalf("limited call executed")
	}
	// Older calls fall out of the window.
	testPool.Exec(context.Background(), `UPDATE app_tool_call SET created_at = now() - interval '2 minutes' WHERE workspace_id = $1`, testWorkspaceID)
	if _, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_FETCH_EMAILS", "arguments": map[string]any{}}); isErr {
		t.Fatalf("call after the window = %s", text)
	}
}

func TestConnectedApps_ConnectRequestResolvesOnConnect(t *testing.T) {
	f := newAppsFixture(t)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "notion", "account_scope": "personal", "level": "write"}})
	_, text, isErr := f.gateway(t, f.token(t), "apps_request_connection", map[string]any{"app": "notion", "summary": "Read the spec"})
	if isErr || !strings.HasPrefix(text, "requested") {
		t.Fatalf("request connection = %s (error %v)", text, isErr)
	}
	reqs := f.listRequests(t, testUserID)
	if len(reqs) != 1 || reqs[0].Kind != "connect" {
		t.Fatalf("requests = %+v", reqs)
	}
	expires, _ := time.Parse(time.RFC3339, reqs[0].ExpiresAt)
	if time.Until(expires) < 6*24*time.Hour {
		t.Fatalf("connect request expires %s, want about 7 days out", reqs[0].ExpiresAt)
	}

	account := fmt.Sprintf("ca_notion_%d", testSeq.Add(1))
	testHandler.Queries.UpsertUserComposioConnection(context.Background(), db.UpsertUserComposioConnectionParams{
		UserID: parseUUID(testUserID), ToolkitSlug: "notion", AuthConfigID: "ac_notion",
		ConnectedAccountID: account, ComposioUserID: testUserID,
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM user_composio_connection WHERE connected_account_id = $1`, account)
	})
	f.h.ConnectedApps.ResolveConnectRequests(context.Background(), parseUUID(testUserID), "notion")

	if reqs := f.listRequests(t, testUserID); len(reqs) != 1 || reqs[0].Status != "approved" {
		t.Fatalf("connect request after connecting = %+v", reqs)
	}
	var note string
	if err := testPool.QueryRow(context.Background(), `
		SELECT handoff_note FROM agent_task_queue
		WHERE agent_id = $1 AND issue_id = $2 AND status = 'queued' AND trigger_evidence_kind = 'app_action_request'
	`, f.agentID, f.issueID).Scan(&note); err != nil || !strings.Contains(note, "now connected") {
		t.Fatalf("resume run = %q, %v", note, err)
	}
}

func TestConnectedApps_ToolClassOverride(t *testing.T) {
	f := newAppsFixture(t)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "confirm_writes"}})

	setClass := func(userID, class string) int {
		w := httptest.NewRecorder()
		f.h.UpdateAppToolClass(w, appsReqAs(t, userID, http.MethodPut, "/api/apps/tools/class",
			map[string]string{"toolkit_slug": "gmail", "tool_slug": "GMAIL_FETCH_EMAILS", "action_class": class}))
		return w.Code
	}
	if code := setClass(f.member, "send"); code != http.StatusForbidden {
		t.Fatalf("member overrides a class: %d, want 403", code)
	}
	if code := setClass(testUserID, "send"); code != http.StatusNoContent {
		t.Fatalf("admin override: %d", code)
	}

	w := httptest.NewRecorder()
	f.h.ListAppTools(w, appsReqAs(t, f.member, http.MethodGet, "/api/apps/tools?toolkit_slug=gmail", nil))
	var tools struct {
		Tools []AppToolResponse `json:"tools"`
	}
	json.Unmarshal(w.Body.Bytes(), &tools)
	found := false
	for _, tool := range tools.Tools {
		if tool.Slug == "GMAIL_FETCH_EMAILS" {
			found = true
			if tool.Class != "send" || tool.AutoClass != "read" || !tool.Overridden {
				t.Fatalf("overridden tool = %+v", tool)
			}
		}
	}
	if !found {
		t.Fatalf("tools = %s", w.Body.String())
	}

	// The corrected class drives the policy: a "read" now waits for confirmation.
	if _, text, _ := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_FETCH_EMAILS", "arguments": map[string]any{}}); !strings.HasPrefix(text, "submitted") {
		t.Fatalf("overridden read = %s, want submitted", text)
	}
	if code := setClass(testUserID, ""); code != http.StatusNoContent {
		t.Fatalf("reset override: %d", code)
	}
	if _, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_FETCH_EMAILS", "arguments": map[string]any{}}); isErr || strings.HasPrefix(text, "submitted") {
		t.Fatalf("read after reset = %s", text)
	}
}

func TestConnectedApps_CustomCatalogAndIssueCalls(t *testing.T) {
	f := newAppsFixture(t)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "write"}})
	f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_FETCH_EMAILS", "arguments": map[string]any{}})

	w := httptest.NewRecorder()
	f.h.ListIssueAppCalls(w, appsReqAs(t, f.member, http.MethodGet, "/api/app-calls?issue_id="+f.issueID, nil))
	var calls struct {
		Calls []IssueAppCallResponse `json:"calls"`
	}
	json.Unmarshal(w.Body.Bytes(), &calls)
	if w.Code != http.StatusOK || len(calls.Calls) != 1 || calls.Calls[0].ToolSlug != "GMAIL_FETCH_EMAILS" || calls.Calls[0].Outcome != "ok" {
		t.Fatalf("issue calls = %d %s", w.Code, w.Body.String())
	}

	// A custom catalog keeps everything off the list away from agents.
	w = httptest.NewRecorder()
	f.h.UpdateAppSettings(w, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/settings",
		map[string]any{"catalog_mode": "custom", "allowed_toolkits": []string{"Notion", "notion", ""}}))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"allowed_toolkits":["notion"]`) {
		t.Fatalf("custom settings: %d %s", w.Code, w.Body.String())
	}
	if _, text, _ := f.gateway(t, f.token(t), "apps_list", nil); strings.Contains(text, "gmail") {
		t.Fatalf("apps_list in custom catalog = %s", text)
	}
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "notion", "account_scope": "personal", "level": "read"}}); w.Code != http.StatusOK {
		t.Fatalf("listed app: %d %s", w.Code, w.Body.String())
	}
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "slack", "account_scope": "personal", "level": "read"}}); w.Code != http.StatusForbidden {
		t.Fatalf("unlisted app in custom catalog: %d, want 403", w.Code)
	}
}

// executionAccount gives each scope a stored identity distinct from the run's
// originator, so deriving the identity instead of reading it fails the test.
func (f *appsFixture) executionAccount(t *testing.T, scope string) (table, id, userID, accountID string) {
	t.Helper()
	userID = fmt.Sprintf("stored_%s_%d", scope, testSeq.Add(1))
	accountID = fmt.Sprintf("ca_%s_%d", scope, testSeq.Add(1))
	if scope == "team" {
		table = "workspace_composio_connection"
		id = dbfx.Insert(t, table, testutil.Cols{
			"workspace_id": testWorkspaceID, "toolkit_slug": "gmail", "auth_config_id": "ac_gmail",
			"connected_account_id": accountID, "composio_user_id": userID, "connected_by": testUserID,
		})
	} else {
		table = "user_composio_connection"
		dbfx.QueryRow(t, `UPDATE user_composio_connection SET composio_user_id = $1, connected_account_id = $2
			WHERE user_id = $3 AND toolkit_slug = 'gmail' AND status = 'active' RETURNING id`, userID, accountID, testUserID).Scan(&id)
		dbfx.Cleanup(t, `DELETE FROM user_composio_connection WHERE id = $1`, id)
	}
	return
}

func (f *appsFixture) approve(t *testing.T, requestID string, args map[string]any) AppRequestResponse {
	t.Helper()
	var body any
	if args != nil {
		body = map[string]any{"arguments": args}
	}
	req := appsReqAs(t, testUserID, http.MethodPost, "/api/app-requests/"+requestID+"/approve", body)
	var out AppRequestResponse
	testutil.Call(t, f.h.ApproveAppRequest, withURLParam(req, "id", requestID)).Want(http.StatusOK).JSON(&out)
	return out
}

func TestConnectedApps_ExecutionIdentity(t *testing.T) {
	for _, scope := range []string{"personal", "team"} {
		for _, approved := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/approved=%t", scope, approved), func(t *testing.T) {
				f := newAppsFixture(t)
				table, id, userID, accountID := f.executionAccount(t, scope)
				if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": scope, "level": "confirm_writes"}}); w.Code != http.StatusOK {
					t.Fatalf("attach app: %d %s", w.Code, w.Body.String())
				}
				tool := "GMAIL_FETCH_EMAILS"
				if approved {
					tool = "GMAIL_SEND_EMAIL"
				}
				args := map[string]any{"user_id": "me", "connected_account_id": "caller_account", "body": "private draft"}
				_, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": tool, "arguments": args})
				if isErr {
					t.Fatalf("gateway call failed: %s", text)
				}
				if approved {
					if !strings.HasPrefix(text, "submitted") || len(f.sdk.executions()) != 0 {
						t.Fatalf("call did not wait for approval: %s", text)
					}
					requests := f.listRequests(t, testUserID)
					if len(requests) != 1 {
						t.Fatalf("requests = %+v", requests)
					}
					// Execution must resolve the current pair again after approval.
					userID, accountID = userID+"_reconnected", accountID+"_reconnected"
					dbfx.Exec(t, "UPDATE "+table+" SET composio_user_id = $1, connected_account_id = $2 WHERE id = $3", userID, accountID, id)
					args["body"] = "edited draft"
					if out := f.approve(t, requests[0].ID, args); out.Status != "approved" {
						t.Fatalf("approved call = %+v", out)
					}
				}
				execs := f.sdk.executions()
				if len(execs) != 1 {
					t.Fatalf("executions = %d, want 1", len(execs))
				}
				if got := execs[0]; got.UserID != userID || got.ConnectedAccountID != accountID || !reflect.DeepEqual(got.Arguments, args) {
					t.Fatalf("execution identity or tool arguments changed: %+v", got)
				}
			})
		}
	}
}

func TestConnectedApps_ExecutionRejectsMissingIdentity(t *testing.T) {
	for _, scope := range []string{"personal", "team"} {
		for _, approved := range []bool{false, true} {
			for _, identity := range []string{"", " \t"} {
				t.Run(fmt.Sprintf("%s/approved=%t/identity=%q", scope, approved, identity), func(t *testing.T) {
					f := newAppsFixture(t)
					table, id, _, _ := f.executionAccount(t, scope)
					if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": scope, "level": "confirm_writes"}}); w.Code != http.StatusOK {
						t.Fatalf("attach app: %d %s", w.Code, w.Body.String())
					}
					args := map[string]any{"user_id": "me"}
					if approved {
						_, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": args})
						if isErr || !strings.HasPrefix(text, "submitted") {
							t.Fatalf("submit = %s", text)
						}
					}
					dbfx.Exec(t, "UPDATE "+table+" SET composio_user_id = $1 WHERE id = $2", identity, id)
					if approved {
						requests := f.listRequests(t, testUserID)
						if len(requests) != 1 {
							t.Fatalf("requests = %+v", requests)
						}
						if out := f.approve(t, requests[0].ID, nil); out.Status != "failed" || out.Error == nil || !strings.Contains(*out.Error, "user ID is missing") {
							t.Fatalf("missing identity approval = %+v", out)
						}
					} else if _, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_FETCH_EMAILS", "arguments": args}); !isErr || !strings.Contains(text, "user ID is missing") {
						t.Fatalf("missing identity call = %s (error %v)", text, isErr)
					}
					if len(f.sdk.executions()) != 0 {
						t.Fatal("missing identity reached upstream")
					}
				})
			}
		}
	}
}

func TestConnectedApps_ExecutionAccountIsolation(t *testing.T) {
	for _, scope := range []string{"personal", "team"} {
		for _, approved := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/approved=%t", scope, approved), func(t *testing.T) {
				f := newAppsFixture(t)
				table, id, _, _ := f.executionAccount(t, scope)
				if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": scope, "level": "confirm_writes"}}); w.Code != http.StatusOK {
					t.Fatalf("attach app: %d %s", w.Code, w.Body.String())
				}
				args := map[string]any{"user_id": "me"}
				var requestID string
				if approved {
					f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": args})
					requests := f.listRequests(t, testUserID)
					if len(requests) != 1 {
						t.Fatalf("requests = %+v", requests)
					}
					requestID = requests[0].ID
					req := appsReqAs(t, f.member, http.MethodPost, "/api/app-requests/"+requestID+"/approve", nil)
					testutil.Call(t, f.h.ApproveAppRequest, withURLParam(req, "id", requestID)).Want(http.StatusForbidden)
					otherWorkspace := dbfx.Workspace(t, "Foreign approvals", fmt.Sprintf("apps-approval-%d", testSeq.Add(1)))
					if _, err := f.h.ConnectedApps.Approve(context.Background(), parseUUID(otherWorkspace), parseUUID(testUserID), parseUUID(requestID), nil); !errors.Is(err, connectedapps.ErrRequestNotFound) {
						t.Fatalf("cross-workspace approval = %v", err)
					}
					if len(f.sdk.executions()) != 0 {
						t.Fatal("unauthorized approval reached upstream")
					}
				}
				if scope == "personal" {
					dbfx.Exec(t, "UPDATE "+table+" SET user_id = $1 WHERE id = $2", f.member, id)
				} else {
					otherWorkspace := dbfx.Workspace(t, "Foreign account", fmt.Sprintf("apps-account-%d", testSeq.Add(1)))
					dbfx.Exec(t, "UPDATE "+table+" SET workspace_id = $1 WHERE id = $2", otherWorkspace, id)
				}
				if approved {
					if out := f.approve(t, requestID, nil); out.Status != "failed" {
						t.Fatalf("foreign account approval = %+v", out)
					}
				} else if _, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_FETCH_EMAILS", "arguments": args}); !isErr {
					t.Fatalf("foreign account call succeeded: %s", text)
				}
				if len(f.sdk.executions()) != 0 {
					t.Fatal("foreign account reached upstream")
				}
			})
		}
	}
}

func TestConnectedApps_ExecutionFailureLogs(t *testing.T) {
	for _, approved := range []bool{false, true} {
		for _, category := range []string{"request_failed", "tool_unsuccessful"} {
			t.Run(fmt.Sprintf("approved=%t/%s", approved, category), func(t *testing.T) {
				f := newAppsFixture(t)
				_, _, userID, accountID := f.executionAccount(t, "personal")
				if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "confirm_writes"}}); w.Code != http.StatusOK {
					t.Fatalf("attach app: %d %s", w.Code, w.Body.String())
				}
				const private = "private_mail_or_token"
				if category == "request_failed" {
					f.sdk.executeErr = errors.New(private)
				} else {
					f.sdk.toolError = private
				}
				var logs bytes.Buffer
				prev := slog.Default()
				slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
				t.Cleanup(func() { slog.SetDefault(prev) })
				tool := "GMAIL_FETCH_EMAILS"
				if approved {
					tool = "GMAIL_SEND_EMAIL"
				}
				code, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": tool, "arguments": map[string]any{"body": private}})
				requestID := ""
				if approved {
					requests := f.listRequests(t, testUserID)
					if len(requests) != 1 {
						t.Fatalf("requests = %+v", requests)
					}
					requestID = requests[0].ID
					if out := f.approve(t, requestID, nil); out.Status != "failed" {
						t.Fatalf("upstream failure approval = %+v", out)
					}
				} else if code != http.StatusOK || !isErr || !strings.Contains(text, private) {
					t.Fatalf("MCP error changed: HTTP %d, %s (error %v)", code, text, isErr)
				}
				var event map[string]any
				for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
					var entry map[string]any
					if json.Unmarshal([]byte(line), &entry) == nil && entry["msg"] == "connectedapps: tool execution failed" {
						event = entry
					}
				}
				if event["workspace_id"] != testWorkspaceID || event["task_id"] != f.taskID || event["request_id"] != requestID || event["toolkit"] != "gmail" || event["tool"] != tool || event["account_scope"] != "personal" || event["error_class"] != category {
					t.Fatalf("failure log = %v", event)
				}
				for _, sensitive := range []string{private, userID, accountID} {
					if strings.Contains(logs.String(), sensitive) {
						t.Fatalf("sensitive value appeared in logs: %q", sensitive)
					}
				}
				if n := dbfx.Count(t, `SELECT count(*) FROM app_tool_call WHERE task_id = $1 AND outcome = 'error'`, f.taskID); n != 1 {
					t.Fatalf("recorded failures = %d, want 1", n)
				}
			})
		}
	}
}
