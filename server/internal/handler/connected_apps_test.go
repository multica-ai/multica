package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/connectedapps"
	composio "github.com/multica-ai/multica/server/internal/integrations/composio"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// appsFakeSDK serves a small Gmail toolkit and records executions.
type appsFakeSDK struct {
	composioFakeSDK
	mu       sync.Mutex
	executed []sdk.ExecuteToolRequest
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
		for _, table := range []string{"app_tool_call", "app_action_request", "app_action_rule", "agent_app", "workspace_app_setting"} {
			testPool.Exec(ctx, `DELETE FROM `+table+` WHERE workspace_id = $1`, testWorkspaceID)
		}
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
