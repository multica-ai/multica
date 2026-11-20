package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"


	"github.com/multica-ai/multica/server/internal/connectedapps"
	"github.com/multica-ai/multica/server/internal/entitlement"
	composio "github.com/multica-ai/multica/server/internal/integrations/composio"
	"github.com/multica-ai/multica/server/internal/testutil"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func clearTeamAccounts(t *testing.T) {
	t.Helper()
	clear := func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_composio_connection WHERE workspace_id = $1`, testWorkspaceID)
	}
	clear()
	t.Cleanup(clear)
}

func seedTeamAccount(t *testing.T, slug, status string) string {
	t.Helper()
	return dbfx.Insert(t, "workspace_composio_connection", testutil.Cols{
		"workspace_id": testWorkspaceID, "toolkit_slug": slug, "auth_config_id": "ac_" + slug,
		"connected_account_id": fmt.Sprintf("ca_team_%s_%d", slug, testSeq.Add(1)),
		"composio_user_id":     composio.TeamComposioUserID(parseUUID(testWorkspaceID)),
		"connected_by":         testUserID, "status": status,
	})
}

func (f *appsFixture) connectTeam(t *testing.T, slug string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, f.h.ConnectTeamApp, appsReqAs(t, testUserID, http.MethodPost, "/api/apps/team/connect", map[string]any{"toolkit_slug": slug}))
}

func (f *appsFixture) putTeamRules(t *testing.T, rules map[string]string) *testutil.Response {
	t.Helper()
	body := []map[string]string{}
	for class, policy := range rules {
		body = append(body, map[string]string{"action_class": class, "policy": policy})
	}
	return testutil.Call(t, f.h.UpdateAppRules, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/rules?toolkit_slug=gmail&scope=team", map[string]any{"rules": body}))
}

func (f *appsFixture) putAgentAppsAs(t *testing.T, userID string, apps []map[string]string) *testutil.Response {
	t.Helper()
	req := appsReqAs(t, userID, http.MethodPut, "/api/agents/"+f.agentID+"/apps", map[string]any{"apps": apps})
	return testutil.Call(t, f.h.UpdateAgentApps, withURLParam(req, "id", f.agentID))
}

func agentApps(t *testing.T, agentID string) map[string]string {
	t.Helper()
	rows, err := testHandler.Queries.ListAgentApps(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatalf("list agent apps: %v", err)
	}
	out := map[string]string{}
	for _, row := range rows {
		out[row.ToolkitSlug] = row.AccountScope + "/" + row.Level
	}
	return out
}

// Starting to connect a team account for an app that has none, under each
// action. An observed start is not recorded: the callback records it once
// the account is written.
func TestTeamAppAccountsConnectUnderEachAction(t *testing.T) {
	for _, tc := range featureCases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppsFixture(t)
			clearTeamAccounts(t)
			usePaidFeatureOn(t, f.h, entitlement.FeatureTeamAppAccounts, tc.decision)
			resp := f.connectTeam(t, "notion")
			if tc.locked {
				wantFeatureLockedFor(t, resp, entitlement.FeatureTeamAppAccounts)
				wantAttempts(t, entitlement.FeatureTeamAppAccounts, featureAttemptCounts{enforced: 1, lastOperation: "connect_team_account"})
				return
			}
			resp.Want(http.StatusOK)
			wantNoAttempts(t, entitlement.FeatureTeamAppAccounts)
		})
	}
}

// Reauthorizing an app's existing team account (active or expired) is always
// allowed, so a downgraded workspace can recover an expired account. After a
// disconnect the app has none, and connecting again is an addition.
func TestTeamAppAccountsEnforcedAllowsReauthorization(t *testing.T) {
	f := newAppsFixture(t)
	clearTeamAccounts(t)
	usePaidFeatureOn(t, f.h, entitlement.FeatureTeamAppAccounts, enforced())
	seedTeamAccount(t, "notion", "expired")
	f.connectTeam(t, "notion").Want(http.StatusOK)
	testPool.Exec(context.Background(), `UPDATE workspace_composio_connection SET status = 'revoked' WHERE workspace_id = $1`, testWorkspaceID)
	wantFeatureLockedFor(t, f.connectTeam(t, "notion"), entitlement.FeatureTeamAppAccounts)
	// Members still get the ordinary admin-only error first.
	wantPlainForbidden(t, testutil.Call(t, f.h.ConnectTeamApp, appsReqAs(t, f.member, http.MethodPost, "/api/apps/team/connect", map[string]any{"toolkit_slug": "notion"})))
	wantAttempts(t, entitlement.FeatureTeamAppAccounts, featureAttemptCounts{enforced: 1, lastOperation: "connect_team_account"})
}

// teamConnectSDK completes team handshakes: it remembers the callback URL
// (and so the signed state), reports every account as the workspace's team
// account under the notion auth config, and records upstream revocations.
type teamConnectSDK struct {
	appsFakeSDK
	mu          sync.Mutex
	callbackURL string
	revoked     []string
}

func (f *teamConnectSDK) CreateLink(_ context.Context, req sdk.CreateLinkRequest) (*sdk.CreateLinkResponse, error) {
	f.mu.Lock()
	f.callbackURL = req.CallbackURL
	f.mu.Unlock()
	return &sdk.CreateLinkResponse{RedirectURL: "https://composio.example/redirect"}, nil
}

func (f *teamConnectSDK) ListConnectedAccounts(_ context.Context, req sdk.ListConnectedAccountsRequest) (*sdk.ListConnectedAccountsResponse, error) {
	id := ""
	if len(req.ConnectedAccountIDs) > 0 {
		id = req.ConnectedAccountIDs[0]
	}
	return &sdk.ListConnectedAccountsResponse{Items: []sdk.ConnectedAccount{{
		ID: id, UserID: composio.TeamComposioUserID(parseUUID(testWorkspaceID)), AuthConfigID: "ac_notion",
	}}}, nil
}

func (f *teamConnectSDK) RevokeConnection(_ context.Context, id string) error {
	f.mu.Lock()
	f.revoked = append(f.revoked, id)
	f.mu.Unlock()
	return nil
}

func (f *teamConnectSDK) state(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := url.Parse(f.callbackURL)
	if err != nil {
		t.Fatalf("parse callback url: %v", err)
	}
	return u.Query().Get("state")
}

// newTeamConnectService wires a composio service with the paid feature gate
// into h, as the router does.
func newTeamConnectService(t *testing.T, h *Handler) (*composio.Service, *teamConnectSDK) {
	t.Helper()
	fake := &teamConnectSDK{}
	svc, err := composio.NewService(fake, h.Queries, composio.Config{
		StateSecret: []byte("team-connect-test-secret"), CallbackBaseURL: "https://multica.ai", FrontendBaseURL: "https://multica.ai",
	})
	if err != nil {
		t.Fatalf("composio.NewService: %v", err)
	}
	svc.SetTeamConnectGate(h.TeamAppConnectGate)
	return svc, fake
}

func teamAccountRows(t *testing.T) map[string]string {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `SELECT connected_account_id, status FROM workspace_composio_connection WHERE workspace_id = $1 AND toolkit_slug = 'notion'`, testWorkspaceID)
	if err != nil {
		t.Fatalf("list team accounts: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[id] = status
	}
	return out
}

// The callback re-checks the feature right before writing a team account: a
// connect started before a downgrade cannot add an account, the refused
// account is revoked upstream and nothing is written, a reauthorization still
// replaces the existing account, and an observed connect is recorded once,
// after the write.
func TestTeamAppAccountsCallbackRechecksBeforeWriting(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	h := *testHandler
	svc, fake := newTeamConnectService(t, &h)
	begin := func(t *testing.T) string {
		t.Helper()
		if _, err := svc.BeginTeamConnect(ctx, workspaceID, parseUUID(testUserID), "notion", ""); err != nil {
			t.Fatalf("BeginTeamConnect: %v", err)
		}
		return fake.state(t)
	}

	t.Run("refused", func(t *testing.T) {
		clearTeamAccounts(t)
		state := begin(t)
		usePaidFeatureOn(t, &h, entitlement.FeatureTeamAppAccounts, enforced())
		_, err := svc.CompleteCallback(ctx, state, "success", "ca_refused")
		if !errors.Is(err, composio.ErrTeamConnectRefused) {
			t.Fatalf("CompleteCallback err = %v, want ErrTeamConnectRefused", err)
		}
		if rows := teamAccountRows(t); len(rows) != 0 {
			t.Fatalf("refused connect wrote %v", rows)
		}
		if len(fake.revoked) == 0 || fake.revoked[len(fake.revoked)-1] != "ca_refused" {
			t.Fatalf("refused account not revoked upstream: %v", fake.revoked)
		}
		wantAttempts(t, entitlement.FeatureTeamAppAccounts, featureAttemptCounts{enforced: 1, lastOperation: "connect_team_account"})
	})

	// Over HTTP the refusal reaches the page as a plan refusal, so it can
	// show the upgrade prompt instead of asking for a retry (an older page
	// still sees the ordinary failure fields).
	t.Run("refused over HTTP", func(t *testing.T) {
		clearTeamAccounts(t)
		state := begin(t)
		usePaidFeatureOn(t, &h, entitlement.FeatureTeamAppAccounts, enforced())
		h.Composio = svc
		withComposioMCPAppsFlag(t, &h, true)
		q := url.Values{"state": {state}, "status": {"success"}, "connected_account_id": {"ca_refused_http"}}
		resp := testutil.Call(t, h.ComposioCallback, httptest.NewRequest(http.MethodGet, "/api/integrations/composio/callback?"+q.Encode(), nil)).Want(http.StatusFound)
		loc, err := url.Parse(resp.Header().Get("Location"))
		if err != nil {
			t.Fatalf("parse redirect: %v", err)
		}
		got := loc.Query()
		if got.Get("error") != "composio_connect_failed" || got.Get("reason") != "feature_locked" || got.Get("scope") != "team" {
			t.Fatalf("redirect = %s, want the failure with reason feature_locked", loc)
		}
		if rows := teamAccountRows(t); len(rows) != 0 {
			t.Fatalf("refused connect wrote %v", rows)
		}
	})

	t.Run("reauthorization", func(t *testing.T) {
		clearTeamAccounts(t)
		existing := seedTeamAccount(t, "notion", "expired")
		var existingAccount string
		dbfx.QueryRow(t, `SELECT connected_account_id FROM workspace_composio_connection WHERE id = $1`, existing).Scan(&existingAccount)
		state := begin(t)
		usePaidFeatureOn(t, &h, entitlement.FeatureTeamAppAccounts, enforced())
		if _, err := svc.CompleteCallback(ctx, state, "success", "ca_reauthorized"); err != nil {
			t.Fatalf("CompleteCallback: %v", err)
		}
		rows := teamAccountRows(t)
		if rows["ca_reauthorized"] != "active" || rows[existingAccount] != "revoked" {
			t.Fatalf("team accounts = %v, want the new one active and %s revoked", rows, existingAccount)
		}
		wantNoAttempts(t, entitlement.FeatureTeamAppAccounts)
	})

	t.Run("observed", func(t *testing.T) {
		clearTeamAccounts(t)
		state := begin(t)
		usePaidFeatureOn(t, &h, entitlement.FeatureTeamAppAccounts, observed())
		if _, err := svc.CompleteCallback(ctx, state, "success", "ca_observed"); err != nil {
			t.Fatalf("CompleteCallback: %v", err)
		}
		if rows := teamAccountRows(t); rows["ca_observed"] != "active" {
			t.Fatalf("team accounts = %v", rows)
		}
		wantAttempts(t, entitlement.FeatureTeamAppAccounts, featureAttemptCounts{observed: 1, lastOperation: "connect_team_account"})
	})

	t.Run("self-hosted", func(t *testing.T) {
		clearTeamAccounts(t)
		state := begin(t)
		usePaidFeatureOn(t, &h, entitlement.FeatureTeamAppAccounts, nil)
		if _, err := svc.CompleteCallback(ctx, state, "success", "ca_self_hosted"); err != nil {
			t.Fatalf("CompleteCallback: %v", err)
		}
		wantNoAttempts(t, entitlement.FeatureTeamAppAccounts)
	})
}

// Giving an agent a team-account app under each action.
func TestTeamAppAccountsAgentAppUnderEachAction(t *testing.T) {
	for _, tc := range featureCases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppsFixture(t)
			usePaidFeatureOn(t, f.h, entitlement.FeatureTeamAppAccounts, tc.decision)
			resp := f.putAgentAppsAs(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "team", "level": "read"}})
			if tc.locked {
				wantFeatureLockedFor(t, resp, entitlement.FeatureTeamAppAccounts)
				if apps := agentApps(t, f.agentID); len(apps) != 0 {
					t.Fatalf("rejected update wrote %v", apps)
				}
				wantAttempts(t, entitlement.FeatureTeamAppAccounts, featureAttemptCounts{enforced: 1, lastOperation: "add_agent_team_app"})
				return
			}
			resp.Want(http.StatusOK)
			want := featureAttemptCounts{}
			if tc.observed > 0 {
				want = featureAttemptCounts{observed: 1, lastOperation: "add_agent_team_app"}
			}
			wantAttempts(t, entitlement.FeatureTeamAppAccounts, want)
		})
	}
}

// After a downgrade an agent's team-account apps keep working and can be
// lowered or removed; switching a personal app to the team account and
// raising a level are refused. Personal apps are never restricted.
func TestTeamAppAccountsEnforcedAgentApps(t *testing.T) {
	f := newAppsFixture(t)
	put := func(apps ...map[string]string) *testutil.Response {
		t.Helper()
		return f.putAgentAppsAs(t, testUserID, apps)
	}
	app := func(slug, scope, level string) map[string]string {
		return map[string]string{"toolkit_slug": slug, "account_scope": scope, "level": level}
	}
	put(app("gmail", "team", "write"), app("notion", "personal", "read")).Want(http.StatusOK)

	usePaidFeatureOn(t, f.h, entitlement.FeatureTeamAppAccounts, enforced())

	put(app("gmail", "team", "write"), app("notion", "personal", "read")).Want(http.StatusOK)
	put(app("gmail", "team", "read"), app("notion", "personal", "write")).Want(http.StatusOK)
	wantFeatureLockedFor(t, put(app("gmail", "team", "write"), app("notion", "personal", "write")), entitlement.FeatureTeamAppAccounts)
	wantFeatureLockedFor(t, put(app("gmail", "team", "read"), app("notion", "team", "read")), entitlement.FeatureTeamAppAccounts)
	if apps := agentApps(t, f.agentID); apps["gmail"] != "team/read" || apps["notion"] != "personal/write" {
		t.Fatalf("agent apps = %v after refusals", apps)
	}
	put(app("notion", "personal", "write")).Want(http.StatusOK)
	if apps := agentApps(t, f.agentID); len(apps) != 1 || apps["notion"] != "personal/write" {
		t.Fatalf("agent apps = %v after removing the team app", apps)
	}
	// A member still gets the ordinary admin-only error first.
	wantPlainForbidden(t, f.putAgentAppsAs(t, f.member, []map[string]string{app("gmail", "team", "read")}))
	wantAttempts(t, entitlement.FeatureTeamAppAccounts, featureAttemptCounts{enforced: 2, lastOperation: "add_agent_team_app"})
}

// Custom team rules are part of the feature. After a downgrade, existing
// rules keep applying, a same-value write-back and restoring defaults (Allow
// all actions) are allowed, a new custom value is refused, and personal rules
// are never restricted.
func TestTeamAppAccountsEnforcedTeamRules(t *testing.T) {
	f := newAppsFixture(t)
	clearTeamAccounts(t)
	_, _, _, _ = f.executionAccount(t, "team")
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "team", "level": "write"}}); w.Code != http.StatusOK {
		t.Fatalf("attach team app: %d %s", w.Code, w.Body.String())
	}
	f.putTeamRules(t, map[string]string{"delete": "block"}).Want(http.StatusOK)

	usePaidFeatureOn(t, f.h, entitlement.FeatureTeamAppAccounts, enforced())

	// Existing configuration keeps working: the team account still runs and
	// the block rule still applies.
	token := f.token(t)
	if _, text, isErr := f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": map[string]any{}}); isErr {
		t.Fatalf("team account send under enforce: %s", text)
	}
	if _, text, isErr := f.gateway(t, token, "apps_run_tool", map[string]any{"tool": "GMAIL_DELETE_MESSAGE", "arguments": map[string]any{}}); !isErr || !strings.HasPrefix(text, "blocked") {
		t.Fatalf("existing block rule under enforce = %s", text)
	}

	f.putTeamRules(t, map[string]string{"delete": "block", "send": "allow"}).Want(http.StatusOK)
	wantFeatureLockedFor(t, f.putTeamRules(t, map[string]string{"send": "block"}), entitlement.FeatureTeamAppAccounts)
	wantFeatureLockedFor(t, f.putTeamRules(t, map[string]string{"delete": "confirm"}), entitlement.FeatureTeamAppAccounts)
	allowAll := map[string]string{}
	for _, class := range connectedapps.Classes {
		allowAll[class] = connectedapps.PolicyAllow
	}
	f.putTeamRules(t, allowAll).Want(http.StatusOK)
	testutil.Call(t, f.h.UpdateAppRules, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/rules?toolkit_slug=gmail&scope=personal",
		map[string]any{"rules": []map[string]string{{"action_class": "send", "policy": "block"}}})).Want(http.StatusOK)
	// A member still gets the ordinary admin-only error first.
	wantPlainForbidden(t, testutil.Call(t, f.h.UpdateAppRules, appsReqAs(t, f.member, http.MethodPut, "/api/apps/rules?toolkit_slug=gmail&scope=team",
		map[string]any{"rules": []map[string]string{{"action_class": "send", "policy": "block"}}})))
	wantAttempts(t, entitlement.FeatureTeamAppAccounts, featureAttemptCounts{enforced: 2, lastOperation: "change_team_rules"})
}

func TestTeamAppAccountsClassification(t *testing.T) {
	team := func(level string) db.AgentApp {
		return db.AgentApp{AccountScope: connectedapps.ScopeTeam, Level: level}
	}
	personal := db.AgentApp{AccountScope: connectedapps.ScopePersonal, Level: connectedapps.LevelWrite}
	for _, tc := range []struct {
		name  string
		prev  db.AgentApp
		had   bool
		scope string
		level string
		want  string
	}{
		{"new personal", db.AgentApp{}, false, connectedapps.ScopePersonal, connectedapps.LevelWrite, ""},
		{"new team", db.AgentApp{}, false, connectedapps.ScopeTeam, connectedapps.LevelRead, teamAppOpAgentApp},
		{"personal to team", personal, true, connectedapps.ScopeTeam, connectedapps.LevelRead, teamAppOpAgentApp},
		{"team to personal", team(connectedapps.LevelWrite), true, connectedapps.ScopePersonal, connectedapps.LevelWrite, ""},
		{"same", team(connectedapps.LevelWrite), true, connectedapps.ScopeTeam, connectedapps.LevelWrite, ""},
		{"lower", team(connectedapps.LevelWrite), true, connectedapps.ScopeTeam, connectedapps.LevelRead, ""},
		{"raise", team(connectedapps.LevelRead), true, connectedapps.ScopeTeam, connectedapps.LevelWrite, teamAppOpRaiseLevel},
		{"raise to confirm_writes", team(connectedapps.LevelRead), true, connectedapps.ScopeTeam, connectedapps.LevelConfirmWrites, teamAppOpRaiseLevel},
		{"confirm_writes to write runs the same", team(connectedapps.LevelConfirmWrites), true, connectedapps.ScopeTeam, connectedapps.LevelWrite, ""},
	} {
		if got := agentTeamAppAdds(tc.prev, tc.had, tc.scope, tc.level); got != tc.want {
			t.Errorf("%s: op = %q, want %q", tc.name, got, tc.want)
		}
	}

	rule := func(class, policy string) appRuleInput { return appRuleInput{ActionClass: class, Policy: policy} }
	for _, tc := range []struct {
		name    string
		current map[string]string
		rules   []appRuleInput
		want    bool
	}{
		{"default write-back", nil, []appRuleInput{rule("send", "allow")}, false},
		{"new custom", nil, []appRuleInput{rule("send", "block")}, true},
		{"same custom", map[string]string{"send": "block"}, []appRuleInput{rule("send", "block")}, false},
		{"restore default", map[string]string{"send": "block"}, []appRuleInput{rule("send", "allow")}, false},
		{"other custom value", map[string]string{"send": "block"}, []appRuleInput{rule("send", "confirm")}, true},
	} {
		if got := teamRulesAdd("gmail", tc.current, tc.rules); got != tc.want {
			t.Errorf("%s: adds = %v, want %v", tc.name, got, tc.want)
		}
	}
}

