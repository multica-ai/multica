package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/integrations/ghuser"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	testGitHubClientID     = "Iv1.handlertest"
	testGitHubClientSecret = "handler-test-secret"
	testGitHubUserToken    = "ghu_handler_1"
)

type recordedGitHubCall struct {
	Method string
	Path   string
	Query  url.Values
	Auth   string
	Body   []byte
}

// fakeGitHubAPI is one httptest server standing in for both github.com (OAuth)
// and api.github.com. Routes are keyed "METHOD /path"; unknown routes 404.
type fakeGitHubAPI struct {
	srv *httptest.Server

	mu           sync.Mutex
	routes       map[string]http.HandlerFunc
	calls        []recordedGitHubCall
	issued       int
	refreshError string
}

func newFakeGitHubAPI(t *testing.T) *fakeGitHubAPI {
	t.Helper()
	f := &fakeGitHubAPI{routes: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		f.mu.Lock()
		f.calls = append(f.calls, recordedGitHubCall{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Auth: r.Header.Get("Authorization"), Body: body})
		route := f.routes[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if route == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		route(w, r)
	}))
	t.Cleanup(f.srv.Close)

	f.on("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Form.Get("grant_type") == "refresh_token" && f.refreshError != "" {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": f.refreshError})
			return
		}
		if r.Form.Get("grant_type") != "refresh_token" && r.Form.Get("code") != "good-code" {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code"})
			return
		}
		f.issued++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":             fmt.Sprintf("ghu_handler_%d", f.issued),
			"expires_in":               28800,
			"refresh_token":            fmt.Sprintf("ghr_handler_%d", f.issued),
			"refresh_token_expires_in": 15897600,
		})
	})
	f.on("GET /user", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":777,"login":"octocat","avatar_url":"https://avatars.example/u/777"}`))
	})
	f.on("DELETE /applications/"+testGitHubClientID+"/grant", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return f
}

func (f *fakeGitHubAPI) on(route string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[route] = h
}

func (f *fakeGitHubAPI) reply(route string, status int, body string) {
	f.on(route, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func (f *fakeGitHubAPI) callsTo(route string) []recordedGitHubCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recordedGitHubCall
	for _, c := range f.calls {
		if c.Method+" "+c.Path == route {
			out = append(out, c)
		}
	}
	return out
}

// apiCalls are the calls made with a user token, i.e. everything except the
// OAuth plumbing.
func (f *fakeGitHubAPI) apiCalls() []recordedGitHubCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recordedGitHubCall
	for _, c := range f.calls {
		if c.Path == "/login/oauth/access_token" || strings.HasPrefix(c.Path, "/applications/") || c.Path == "/user" {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (f *fakeGitHubAPI) resetCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

// installGitHubUserService points testHandler.GitHubUser at the fake for the
// duration of the test.
func installGitHubUserService(t *testing.T, api *fakeGitHubAPI) *ghuser.Service {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	svc, err := ghuser.New(ghuser.Config{ClientID: testGitHubClientID, ClientSecret: testGitHubClientSecret, SecretKey: key}, testHandler.Queries, testPool)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetEndpoints(api.srv.URL, api.srv.URL)
	previous := testHandler.GitHubUser
	testHandler.GitHubUser = svc
	t.Cleanup(func() { testHandler.GitHubUser = previous })
	dbfx.Cleanup(t, `DELETE FROM github_user_connection WHERE user_id = $1`, testUserID)
	return svc
}

func connectTestGitHubUser(t *testing.T, svc *ghuser.Service) {
	t.Helper()
	if _, err := svc.Connect(context.Background(), util.MustParseUUID(testUserID), "good-code", ""); err != nil {
		t.Fatalf("connect test user: %v", err)
	}
}

type prActionsFixture struct {
	api            *fakeGitHubAPI
	svc            *ghuser.Service
	issueID        string
	prID           string
	repo           string
	installationID int64
}

func (f *prActionsFixture) pullPath() string { return "/repos/acme/" + f.repo + "/pulls/7" }
func (f *prActionsFixture) repoPath() string { return "/repos/acme/" + f.repo }

func randomInstallationID(t *testing.T) int64 {
	t.Helper()
	n, err := rand.Int(rand.Reader, big.NewInt(1<<40))
	if err != nil {
		t.Fatal(err)
	}
	return 1<<50 + n.Int64()
}

func insertTestGitHubPR(t *testing.T, installationID int64, repo string, number int, author any) string {
	t.Helper()
	now := time.Now()
	return dbfx.Insert(t, "github_pull_request", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"installation_id": installationID,
		"repo_owner":      "acme",
		"repo_name":       repo,
		"pr_number":       number,
		"title":           "Add widget",
		"state":           "open",
		"html_url":        fmt.Sprintf("https://github.com/acme/%s/pull/%d", repo, number),
		"branch":          "feature/widget",
		"author_login":    author,
		"head_sha":        "sha-head",
		"pr_created_at":   now,
		"pr_updated_at":   now,
	})
}

func linkTestPR(t *testing.T, issueID, prID string) {
	t.Helper()
	dbfx.InsertNoID(t, "issue_pull_request", testutil.Cols{"issue_id": issueID, "pull_request_id": prID},
		"issue_id = $1 AND pull_request_id = $2", issueID, prID)
}

func setupPRActions(t *testing.T) *prActionsFixture {
	t.Helper()
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	api := newFakeGitHubAPI(t)
	svc := installGitHubUserService(t, api)
	connectTestGitHubUser(t, svc)

	issueID := dbfx.Issue(t, "PR actions")
	dbfx.Cleanup(t, `DELETE FROM activity_log WHERE issue_id = $1`, issueID)
	installationID := randomInstallationID(t)
	dbfx.Insert(t, "github_installation", testutil.Cols{
		"workspace_id": testWorkspaceID, "installation_id": installationID, "account_login": "acme",
	})
	repo := "widgets-" + uuid.NewString()[:8]
	prID := insertTestGitHubPR(t, installationID, repo, 7, "alice")
	linkTestPR(t, issueID, prID)
	api.resetCalls()
	return &prActionsFixture{api: api, svc: svc, issueID: issueID, prID: prID, repo: repo, installationID: installationID}
}

func (f *prActionsFixture) request(method, action string, body any) *http.Request {
	return f.requestFor(f.prID, method, action, body)
}

func (f *prActionsFixture) requestFor(prID, method, action string, body any) *http.Request {
	path := "/api/issues/" + f.issueID + "/pull-requests/" + prID + "/" + action
	req := newRequest(method, path, body)
	return withURLParams(req, "id", f.issueID, "prId", prID)
}

func servePRAction(t *testing.T, handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("PR actions must never answer 401 (the web client logs out): %s", w.Body.String())
	}
	return w
}

type apiErrorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func expectErrorCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != status || body.Code != code {
		t.Fatalf("got %d %+v, want %d %s", w.Code, body, status, code)
	}
	return body
}

func expectOK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"ok":true}` {
		t.Fatalf("got %d %s, want {\"ok\":true}", w.Code, w.Body.String())
	}
}

func decodeBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return m
}

func expectUserToken(t *testing.T, calls []recordedGitHubCall) {
	t.Helper()
	for _, c := range calls {
		if c.Auth != "Bearer "+testGitHubUserToken {
			t.Fatalf("%s %s sent %q; every PR action must use the caller's user token", c.Method, c.Path, c.Auth)
		}
	}
}

// graphQLRoute answers each GraphQL request with the first response whose key
// appears in the query text.
func graphQLRoute(t *testing.T, responses map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for key, resp := range responses {
			if strings.Contains(req.Query, key) {
				_, _ = w.Write([]byte(resp))
				return
			}
		}
		t.Errorf("unexpected graphql query: %s", req.Query)
		w.WriteHeader(http.StatusBadRequest)
	}
}

func graphQLVariables(t *testing.T, call recordedGitHubCall) map[string]any {
	t.Helper()
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(call.Body, &req); err != nil {
		t.Fatal(err)
	}
	return req.Variables
}

func activityActions(t *testing.T, issueID string) []map[string]any {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT action, actor_type, actor_id::text, details FROM activity_log WHERE issue_id = $1 ORDER BY created_at`, issueID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var action, actorType, actorID string
		var details []byte
		if err := rows.Scan(&action, &actorType, &actorID, &details); err != nil {
			t.Fatal(err)
		}
		out = append(out, map[string]any{"action": action, "actor_type": actorType, "actor_id": actorID, "details": decodeBody(t, details)})
	}
	return out
}

func allPRActionHandlers() map[string]struct {
	method  string
	handler http.HandlerFunc
} {
	h := testHandler
	return map[string]struct {
		method  string
		handler http.HandlerFunc
	}{
		"merge-options":       {http.MethodGet, h.GetPullRequestMergeOptions},
		"merge":               {http.MethodPost, h.MergePullRequest},
		"auto-merge":          {http.MethodPut, h.EnablePullRequestAutoMerge},
		"auto-merge/disable":  {http.MethodDelete, h.DisablePullRequestAutoMerge},
		"update-branch":       {http.MethodPost, h.UpdatePullRequestBranch},
		"rerun-failed-checks": {http.MethodPost, h.RerunFailedChecks},
		"ready-for-review":    {http.MethodPost, h.MarkPullRequestReadyForReview},
		"convert-to-draft":    {http.MethodPost, h.ConvertPullRequestToDraft},
		"approve":             {http.MethodPost, h.ApprovePullRequest},
		"reviewer-candidates": {http.MethodGet, h.ListPullRequestReviewerCandidates},
		"request-review":      {http.MethodPost, h.RequestPullRequestReview},
		"close":               {http.MethodPost, h.ClosePullRequest},
	}
}

// --- preconditions -------------------------------------------------------------

func TestGitHubPRActionsRejectAgentActorsOnEveryEndpoint(t *testing.T) {
	f := setupPRActions(t)
	for name, ep := range allPRActionHandlers() {
		t.Run(name, func(t *testing.T) {
			req := f.request(ep.method, name, map[string]any{})
			req.Header.Set("X-Actor-Source", "task_token")
			w := servePRAction(t, ep.handler, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", w.Code)
			}
		})
	}
	if calls := f.api.apiCalls(); len(calls) != 0 {
		t.Fatalf("an agent request reached GitHub: %+v", calls)
	}
}

func TestGitHubPRActionsRejectUnlinkedPROnEveryEndpoint(t *testing.T) {
	f := setupPRActions(t)
	unlinked := insertTestGitHubPR(t, f.installationID, f.repo, 8, "alice")
	for name, ep := range allPRActionHandlers() {
		t.Run(name, func(t *testing.T) {
			w := servePRAction(t, ep.handler, f.requestFor(unlinked, ep.method, name, map[string]any{}))
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
			}
		})
	}
	if calls := f.api.apiCalls(); len(calls) != 0 {
		t.Fatalf("an unlinked PR reached GitHub: %+v", calls)
	}
}

func TestGitHubPRActionPreconditions(t *testing.T) {
	f := setupPRActions(t)
	closePR := testHandler.ClosePullRequest

	t.Run("explicitly unlinked", func(t *testing.T) {
		dbfx.InsertNoID(t, "issue_pull_request_exclusion", testutil.Cols{
			"issue_id": f.issueID, "pull_request_id": f.prID, "workspace_id": testWorkspaceID,
		}, "issue_id = $1 AND pull_request_id = $2", f.issueID, f.prID)
		w := servePRAction(t, closePR, f.request(http.MethodPost, "close", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
	})

	t.Run("non-GitHub PR", func(t *testing.T) {
		connID := dbfx.Insert(t, "vcs_connection", testutil.Cols{
			"workspace_id": testWorkspaceID, "provider": "gitlab", "instance_url": "https://gitlab.example/" + uuid.NewString(),
			"account_login": "bot", "access_token_encrypted": "x", "webhook_secret_encrypted": "x",
		})
		now := time.Now()
		vcsPR := dbfx.Insert(t, "vcs_pull_request", testutil.Cols{
			"workspace_id": testWorkspaceID, "connection_id": connID, "provider": "gitlab",
			"repo_owner": "acme", "repo_name": "gl", "pr_number": 3, "title": "MR", "state": "open",
			"html_url": "https://gitlab.example/acme/gl/-/merge_requests/3", "pr_created_at": now, "pr_updated_at": now,
		})
		w := servePRAction(t, closePR, f.requestFor(vcsPR, http.MethodPost, "close", nil))
		expectErrorCode(t, w, http.StatusUnprocessableEntity, "pull_request_provider_unsupported")
	})

	t.Run("installation not in this workspace", func(t *testing.T) {
		orphan := insertTestGitHubPR(t, randomInstallationID(t), f.repo, 9, "alice")
		linkTestPR(t, f.issueID, orphan)
		w := servePRAction(t, closePR, f.requestFor(orphan, http.MethodPost, "close", nil))
		expectErrorCode(t, w, http.StatusUnprocessableEntity, "github_installation_missing")
	})

	t.Run("non-member", func(t *testing.T) {
		outsider := dbfx.User(t, "Outsider", "outsider-"+uuid.NewString()+"@example.test")
		req := f.request(http.MethodPost, "close", nil)
		req.Header.Set("X-User-ID", outsider)
		w := servePRAction(t, middleware.RequireWorkspaceMember(testHandler.Queries)(http.HandlerFunc(closePR)).ServeHTTP, req)
		if w.Code < 400 {
			t.Fatalf("non-member got %d", w.Code)
		}
	})

	if calls := f.api.apiCalls(); len(calls) != 0 {
		t.Fatalf("a rejected request reached GitHub: %+v", calls)
	}

	t.Run("not configured", func(t *testing.T) {
		previous := testHandler.GitHubUser
		testHandler.GitHubUser = nil
		defer func() { testHandler.GitHubUser = previous }()
		w := servePRAction(t, closePR, f.request(http.MethodPost, "close", nil))
		expectErrorCode(t, w, http.StatusServiceUnavailable, "github_user_auth_not_configured")
	})

	t.Run("reauth required after failed refresh", func(t *testing.T) {
		f.api.mu.Lock()
		f.api.refreshError = "bad_refresh_token"
		f.api.mu.Unlock()
		dbfx.Exec(t, `UPDATE github_user_connection SET access_token_expires_at = now() - interval '1 minute' WHERE user_id = $1`, testUserID)
		w := servePRAction(t, closePR, f.request(http.MethodPost, "close", nil))
		expectErrorCode(t, w, http.StatusConflict, "github_reauth_required")
		var n int
		dbfx.QueryRow(t, `SELECT count(*) FROM github_user_connection WHERE user_id = $1`, testUserID).Scan(&n)
		if n != 0 {
			t.Fatal("a failed refresh must clear the connection")
		}
	})

	t.Run("not connected", func(t *testing.T) {
		dbfx.Exec(t, `DELETE FROM github_user_connection WHERE user_id = $1`, testUserID)
		w := servePRAction(t, closePR, f.request(http.MethodPost, "close", nil))
		expectErrorCode(t, w, http.StatusConflict, "github_account_not_connected")
	})
}

// --- error mapping -------------------------------------------------------------

func TestGitHubPRActionErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		want    int
		code    string
		message string
	}{
		{"app permission missing", 403, nil, `{"message":"Resource not accessible by integration"}`, 403, "github_app_permission_missing", ""},
		{"user forbidden", 403, nil, `{"message":"Must have push access to repository"}`, 403, "github_forbidden", "Must have push access to repository"},
		{"repository hidden", 404, nil, `{"message":"Not Found"}`, 403, "github_forbidden", ""},
		{"rate limited", 429, map[string]string{"Retry-After": "42"}, `{"message":"secondary rate limit"}`, 429, "github_rate_limited", ""},
		{"primary rate limit", 403, map[string]string{"X-RateLimit-Remaining": "0"}, `{"message":"API rate limit exceeded"}`, 429, "github_rate_limited", ""},
		{"validation", 422, nil, `{"message":"Validation Failed","errors":[{"message":"state cannot be changed"}]}`, 422, "github_request_failed", "Validation Failed: state cannot be changed"},
		{"github outage", 502, nil, `<html>bad gateway</html>`, 502, "github_request_failed", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupPRActions(t)
			f.api.on("PATCH "+f.pullPath(), func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			w := servePRAction(t, testHandler.ClosePullRequest, f.request(http.MethodPost, "close", nil))
			body := expectErrorCode(t, w, tc.want, tc.code)
			if tc.message != "" && body.Error != tc.message {
				t.Fatalf("error = %q, want GitHub's message %q", body.Error, tc.message)
			}
			if tc.code == "github_rate_limited" && tc.headers["Retry-After"] != "" && w.Header().Get("Retry-After") != tc.headers["Retry-After"] {
				t.Fatalf("Retry-After = %q", w.Header().Get("Retry-After"))
			}
			if strings.Contains(w.Body.String(), "ghu_") {
				t.Fatal("response leaked the user token")
			}
			if acts := activityActions(t, f.issueID); len(acts) != 0 {
				t.Fatalf("a failed close recorded activity: %+v", acts)
			}
		})
	}
}

// GitHub rejecting the token itself must not surface as 401; the stored
// authorization is dropped and the user asked to reconnect.
func TestGitHubPRActionRejectedTokenRequiresReauth(t *testing.T) {
	f := setupPRActions(t)
	f.api.reply("PATCH "+f.pullPath(), http.StatusUnauthorized, `{"message":"Bad credentials"}`)
	w := servePRAction(t, testHandler.ClosePullRequest, f.request(http.MethodPost, "close", nil))
	expectErrorCode(t, w, http.StatusConflict, "github_reauth_required")
	var n int
	dbfx.QueryRow(t, `SELECT count(*) FROM github_user_connection WHERE user_id = $1`, testUserID).Scan(&n)
	if n != 0 {
		t.Fatal("a token GitHub rejected must be forgotten")
	}
}

// --- merge -----------------------------------------------------------------------

func mergeTestPull(headRepoID int) string {
	return fmt.Sprintf(`{"node_id":"PR_node","head":{"ref":"feature/widget","sha":"sha-head","repo":{"id":%d}},"base":{"repo":{"id":1}}}`, headRepoID)
}

func TestGitHubPRMerge(t *testing.T) {
	f := setupPRActions(t)
	f.api.reply("GET "+f.pullPath(), 200, mergeTestPull(1))
	f.api.reply("PUT "+f.pullPath()+"/merge", 200, `{"sha":"merged-sha","merged":true,"message":"Pull Request successfully merged"}`)
	f.api.on("DELETE "+f.repoPath()+"/git/refs/heads/feature/widget", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	w := servePRAction(t, testHandler.MergePullRequest, f.request(http.MethodPost, "merge", map[string]any{
		"method": "squash", "commit_title": "Add widget (#7)", "commit_message": "body", "delete_branch": true, "expected_head_sha": "sha-head",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if got := decodeBody(t, w.Body.Bytes()); got["merged"] != true || got["sha"] != "merged-sha" || got["branch_deleted"] != true {
		t.Fatalf("response = %v", got)
	}
	merges := f.api.callsTo("PUT " + f.pullPath() + "/merge")
	if len(merges) != 1 {
		t.Fatalf("merge calls = %d", len(merges))
	}
	if body := decodeBody(t, merges[0].Body); body["merge_method"] != "squash" || body["sha"] != "sha-head" ||
		body["commit_title"] != "Add widget (#7)" || body["commit_message"] != "body" {
		t.Fatalf("merge body = %v", body)
	}
	expectUserToken(t, f.api.apiCalls())

	acts := activityActions(t, f.issueID)
	if len(acts) != 1 || acts[0]["action"] != "pull_request_merged" || acts[0]["actor_type"] != "member" || acts[0]["actor_id"] != testUserID {
		t.Fatalf("activity = %+v", acts)
	}
	details := acts[0]["details"].(map[string]any)
	if details["pr_number"] != float64(7) || details["merge_method"] != "squash" || details["pull_request_id"] != f.prID {
		t.Fatalf("activity details = %v", details)
	}
}

func TestGitHubPRMergeBranchDeletion(t *testing.T) {
	cases := []struct {
		name        string
		headRepoID  int
		deleteReply int
		deleteBody  string
		wantDeleted bool
		wantDelete  bool
	}{
		{"fork branch is left alone", 2, 0, "", false, false},
		{"already deleted by GitHub", 1, 422, `{"message":"Reference does not exist"}`, true, true},
		{"deletion failure does not fail the merge", 1, 403, `{"message":"Protected branch"}`, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupPRActions(t)
			f.api.reply("GET "+f.pullPath(), 200, mergeTestPull(tc.headRepoID))
			f.api.reply("PUT "+f.pullPath()+"/merge", 200, `{"sha":"merged-sha","merged":true}`)
			deleteRoute := "DELETE " + f.repoPath() + "/git/refs/heads/feature/widget"
			if tc.deleteReply != 0 {
				f.api.reply(deleteRoute, tc.deleteReply, tc.deleteBody)
			}
			w := servePRAction(t, testHandler.MergePullRequest, f.request(http.MethodPost, "merge", map[string]any{
				"method": "merge", "delete_branch": true, "expected_head_sha": "sha-head",
			}))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			if got := decodeBody(t, w.Body.Bytes()); got["branch_deleted"] != tc.wantDeleted {
				t.Fatalf("response = %v", got)
			}
			if called := len(f.api.callsTo(deleteRoute)) > 0; called != tc.wantDelete {
				t.Fatalf("delete called = %v, want %v", called, tc.wantDelete)
			}
		})
	}
}

func TestGitHubPRMergeErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    int
		code    string
		message string
	}{
		{"branch protection", 405, `{"message":"Required status check \"ci\" is expected."}`, 422, "github_merge_blocked", `Required status check "ci" is expected.`},
		{"method not allowed for repo", 422, `{"message":"Squash merges are not allowed on this repository."}`, 422, "github_merge_blocked", "Squash merges are not allowed on this repository."},
		{"head moved", 409, `{"message":"Head branch was modified. Review and try the merge again."}`, 409, "github_head_changed", ""},
		{"app permission missing", 403, `{"message":"Resource not accessible by integration"}`, 403, "github_app_permission_missing", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupPRActions(t)
			f.api.reply("PUT "+f.pullPath()+"/merge", tc.status, tc.body)
			w := servePRAction(t, testHandler.MergePullRequest, f.request(http.MethodPost, "merge", map[string]any{
				"method": "squash", "expected_head_sha": "sha-head",
			}))
			body := expectErrorCode(t, w, tc.want, tc.code)
			if tc.message != "" && body.Error != tc.message {
				t.Fatalf("error = %q, want %q", body.Error, tc.message)
			}
			if acts := activityActions(t, f.issueID); len(acts) != 0 {
				t.Fatalf("a failed merge recorded activity: %+v", acts)
			}
		})
	}

	t.Run("request validation", func(t *testing.T) {
		f := setupPRActions(t)
		for _, body := range []map[string]any{
			{"method": "octopus", "expected_head_sha": "sha-head"},
			{"method": "squash"},
		} {
			w := servePRAction(t, testHandler.MergePullRequest, f.request(http.MethodPost, "merge", body))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("body %v: status = %d", body, w.Code)
			}
		}
		if calls := f.api.apiCalls(); len(calls) != 0 {
			t.Fatalf("an invalid merge reached GitHub: %+v", calls)
		}
	})
}

// --- auto-merge, draft state -------------------------------------------------------

func nodeQueryResponse(autoMergeAllowed bool) string {
	return fmt.Sprintf(`{"data":{"repository":{"autoMergeAllowed":%t,"pullRequest":{"id":"PR_node7"}}}}`, autoMergeAllowed)
}

func TestGitHubPRAutoMerge(t *testing.T) {
	t.Run("enable", func(t *testing.T) {
		f := setupPRActions(t)
		f.api.on("POST /graphql", graphQLRoute(t, map[string]string{
			"autoMergeAllowed":           nodeQueryResponse(true),
			"enablePullRequestAutoMerge": `{"data":{"enablePullRequestAutoMerge":{"clientMutationId":null}}}`,
		}))
		w := servePRAction(t, testHandler.EnablePullRequestAutoMerge, f.request(http.MethodPut, "auto-merge", map[string]any{"method": "squash"}))
		expectOK(t, w)
		calls := f.api.callsTo("POST /graphql")
		if len(calls) != 2 {
			t.Fatalf("graphql calls = %d", len(calls))
		}
		if vars := graphQLVariables(t, calls[1]); vars["id"] != "PR_node7" || vars["method"] != "SQUASH" {
			t.Fatalf("mutation variables = %v", vars)
		}
		expectUserToken(t, calls)
	})

	t.Run("repository does not allow auto-merge", func(t *testing.T) {
		f := setupPRActions(t)
		f.api.on("POST /graphql", graphQLRoute(t, map[string]string{"autoMergeAllowed": nodeQueryResponse(false)}))
		w := servePRAction(t, testHandler.EnablePullRequestAutoMerge, f.request(http.MethodPut, "auto-merge", map[string]any{"method": "merge"}))
		expectErrorCode(t, w, http.StatusUnprocessableEntity, "github_auto_merge_not_allowed")
	})

	t.Run("GitHub refuses auto-merge", func(t *testing.T) {
		f := setupPRActions(t)
		f.api.on("POST /graphql", graphQLRoute(t, map[string]string{
			"autoMergeAllowed":           nodeQueryResponse(true),
			"enablePullRequestAutoMerge": `{"data":null,"errors":[{"type":"UNPROCESSABLE","message":"Auto merge is not allowed for this repository"}]}`,
		}))
		w := servePRAction(t, testHandler.EnablePullRequestAutoMerge, f.request(http.MethodPut, "auto-merge", map[string]any{"method": "merge"}))
		expectErrorCode(t, w, http.StatusUnprocessableEntity, "github_auto_merge_not_allowed")
	})

	t.Run("invalid method", func(t *testing.T) {
		f := setupPRActions(t)
		w := servePRAction(t, testHandler.EnablePullRequestAutoMerge, f.request(http.MethodPut, "auto-merge", map[string]any{"method": ""}))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("disable", func(t *testing.T) {
		f := setupPRActions(t)
		f.api.on("POST /graphql", graphQLRoute(t, map[string]string{
			"autoMergeAllowed":            nodeQueryResponse(true),
			"disablePullRequestAutoMerge": `{"data":{"disablePullRequestAutoMerge":{"clientMutationId":null}}}`,
		}))
		expectOK(t, servePRAction(t, testHandler.DisablePullRequestAutoMerge, f.request(http.MethodDelete, "auto-merge", nil)))
	})
}

func TestGitHubPRDraftState(t *testing.T) {
	for name, tc := range map[string]struct {
		mutation string
		handler  http.HandlerFunc
	}{
		"ready-for-review": {"markPullRequestReadyForReview", testHandler.MarkPullRequestReadyForReview},
		"convert-to-draft": {"convertPullRequestToDraft", testHandler.ConvertPullRequestToDraft},
	} {
		t.Run(name, func(t *testing.T) {
			f := setupPRActions(t)
			f.api.on("POST /graphql", graphQLRoute(t, map[string]string{
				"autoMergeAllowed": nodeQueryResponse(true),
				tc.mutation:        `{"data":{}}`,
			}))
			expectOK(t, servePRAction(t, tc.handler, f.request(http.MethodPost, name, nil)))
			calls := f.api.callsTo("POST /graphql")
			if len(calls) != 2 || graphQLVariables(t, calls[1])["id"] != "PR_node7" {
				t.Fatalf("graphql calls = %+v", calls)
			}
		})
		t.Run(name+" without write permission", func(t *testing.T) {
			f := setupPRActions(t)
			f.api.on("POST /graphql", graphQLRoute(t, map[string]string{
				"autoMergeAllowed": nodeQueryResponse(true),
				tc.mutation:        `{"data":null,"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`,
			}))
			w := servePRAction(t, tc.handler, f.request(http.MethodPost, name, nil))
			expectErrorCode(t, w, http.StatusForbidden, "github_app_permission_missing")
		})
	}
}

// --- update-branch -------------------------------------------------------------------

func TestGitHubPRUpdateBranch(t *testing.T) {
	f := setupPRActions(t)
	f.api.reply("PUT "+f.pullPath()+"/update-branch", http.StatusAccepted, `{"message":"Updating pull request branch."}`)
	expectOK(t, servePRAction(t, testHandler.UpdatePullRequestBranch, f.request(http.MethodPost, "update-branch", map[string]any{"expected_head_sha": "sha-head"})))
	calls := f.api.callsTo("PUT " + f.pullPath() + "/update-branch")
	if len(calls) != 1 || decodeBody(t, calls[0].Body)["expected_head_sha"] != "sha-head" {
		t.Fatalf("update-branch calls = %+v", calls)
	}

	f.api.reply("PUT "+f.pullPath()+"/update-branch", http.StatusUnprocessableEntity, `{"message":"expected head sha didn't match current head ref."}`)
	w := servePRAction(t, testHandler.UpdatePullRequestBranch, f.request(http.MethodPost, "update-branch", map[string]any{"expected_head_sha": "stale"}))
	expectErrorCode(t, w, http.StatusConflict, "github_head_changed")
}

// --- rerun-failed-checks ---------------------------------------------------------------

func TestGitHubPRRerunFailedChecks(t *testing.T) {
	f := setupPRActions(t)
	f.api.reply("GET "+f.pullPath(), 200, mergeTestPull(1))
	f.api.on("GET "+f.repoPath()+"/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("head_sha") != "sha-head" {
			t.Errorf("runs query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":11,"name":"CI","workflow_id":1,"event":"pull_request","status":"completed","conclusion":"failure"},
			{"id":10,"name":"CI","workflow_id":1,"event":"pull_request","status":"completed","conclusion":"failure"},
			{"id":12,"name":"Lint","workflow_id":2,"event":"pull_request","status":"completed","conclusion":"success"},
			{"id":13,"name":"Deploy","workflow_id":3,"event":"pull_request","status":"completed","conclusion":"timed_out"},
			{"id":14,"name":"E2E","workflow_id":4,"event":"pull_request","status":"in_progress","conclusion":null}
		]}`))
	})
	f.api.reply("POST "+f.repoPath()+"/actions/runs/11/rerun-failed-jobs", http.StatusCreated, `{}`)
	f.api.reply("POST "+f.repoPath()+"/actions/runs/13/rerun-failed-jobs", http.StatusForbidden, `{"message":"Unable to re-run this workflow run because it was created over a month ago"}`)
	f.api.reply("GET "+f.repoPath()+"/commits/sha-head/check-runs", 200, `{"check_runs":[
		{"name":"CI / test","status":"completed","conclusion":"failure","app":{"slug":"github-actions"}},
		{"name":"codecov/patch","status":"completed","conclusion":"failure","app":{"slug":"codecov"}},
		{"name":"sonar","status":"completed","conclusion":"success","app":{"slug":"sonarcloud"}}
	]}`)
	f.api.reply("GET "+f.repoPath()+"/commits/sha-head/status", 200, `{"statuses":[{"context":"vercel","state":"error"},{"context":"netlify","state":"success"}]}`)

	w := servePRAction(t, testHandler.RerunFailedChecks, f.request(http.MethodPost, "rerun-failed-checks", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp RerunFailedChecksResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.RerunCount != 1 || strings.Join(resp.NotRerunnable, ",") != "Deploy,codecov/patch,vercel" {
		t.Fatalf("response = %+v", resp)
	}
	if len(f.api.callsTo("POST "+f.repoPath()+"/actions/runs/10/rerun-failed-jobs")) != 0 {
		t.Fatal("an older run of a workflow must not be rerun")
	}
	expectUserToken(t, f.api.apiCalls())

	t.Run("actions permission missing", func(t *testing.T) {
		f := setupPRActions(t)
		f.api.reply("GET "+f.pullPath(), 200, mergeTestPull(1))
		f.api.reply("GET "+f.repoPath()+"/actions/runs", http.StatusForbidden, `{"message":"Resource not accessible by integration"}`)
		w := servePRAction(t, testHandler.RerunFailedChecks, f.request(http.MethodPost, "rerun-failed-checks", nil))
		expectErrorCode(t, w, http.StatusForbidden, "github_app_permission_missing")
	})
}

// --- reviews -------------------------------------------------------------------------------

func TestGitHubPRApprove(t *testing.T) {
	t.Run("approve with a comment", func(t *testing.T) {
		f := setupPRActions(t)
		f.api.reply("POST "+f.pullPath()+"/reviews", 200, `{"id":1,"state":"APPROVED"}`)
		expectOK(t, servePRAction(t, testHandler.ApprovePullRequest, f.request(http.MethodPost, "approve", map[string]any{"body": "LGTM"})))
		calls := f.api.callsTo("POST " + f.pullPath() + "/reviews")
		if len(calls) != 1 {
			t.Fatalf("review calls = %d", len(calls))
		}
		if body := decodeBody(t, calls[0].Body); body["event"] != "APPROVE" || body["body"] != "LGTM" {
			t.Fatalf("review body = %v", body)
		}
	})

	t.Run("own pull request is refused before calling GitHub", func(t *testing.T) {
		f := setupPRActions(t)
		dbfx.Exec(t, `UPDATE github_pull_request SET author_login = 'OctoCat' WHERE id = $1`, f.prID)
		w := servePRAction(t, testHandler.ApprovePullRequest, f.request(http.MethodPost, "approve", nil))
		expectErrorCode(t, w, http.StatusUnprocessableEntity, "github_cannot_approve_own_pr")
		if calls := f.api.apiCalls(); len(calls) != 0 {
			t.Fatalf("calls = %+v", calls)
		}
	})

	t.Run("own pull request rejected by GitHub", func(t *testing.T) {
		f := setupPRActions(t)
		dbfx.Exec(t, `UPDATE github_pull_request SET author_login = NULL WHERE id = $1`, f.prID)
		f.api.reply("POST "+f.pullPath()+"/reviews", 422, `{"message":"Unprocessable Entity","errors":["Can not approve your own pull request"]}`)
		w := servePRAction(t, testHandler.ApprovePullRequest, f.request(http.MethodPost, "approve", nil))
		expectErrorCode(t, w, http.StatusUnprocessableEntity, "github_cannot_approve_own_pr")
	})
}

func TestGitHubPRReviewerCandidates(t *testing.T) {
	f := setupPRActions(t)
	f.api.reply("GET "+f.repoPath()+"/assignees", 200, `[
		{"login":"alice","avatar_url":"https://a/alice"},
		{"login":"jimbo","avatar_url":"https://a/jimbo"},
		{"login":"Bob","avatar_url":"https://a/bob"},
		{"login":"bobby","avatar_url":"https://a/bobby"},
		{"login":"carol","avatar_url":"https://a/carol"}
	]`)
	w := servePRAction(t, testHandler.ListPullRequestReviewerCandidates, f.request(http.MethodGet, "reviewer-candidates?q=bo", nil))
	var resp GitHubReviewerCandidatesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	var logins []string
	for _, u := range resp.Users {
		logins = append(logins, u.Login)
	}
	if w.Code != http.StatusOK || strings.Join(logins, ",") != "Bob,bobby,jimbo" || resp.Users[0].AvatarURL != "https://a/bob" {
		t.Fatalf("status=%d users=%+v", w.Code, resp.Users)
	}

	w = servePRAction(t, testHandler.ListPullRequestReviewerCandidates, f.request(http.MethodGet, "reviewer-candidates", nil))
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	for _, u := range resp.Users {
		if u.Login == "alice" {
			t.Fatal("the PR author can't be requested as a reviewer and must be left out")
		}
	}
	if len(resp.Users) != 4 {
		t.Fatalf("users = %+v", resp.Users)
	}
}

func TestGitHubPRRequestReview(t *testing.T) {
	f := setupPRActions(t)
	f.api.reply("POST "+f.pullPath()+"/requested_reviewers", http.StatusCreated, `{"number":7}`)
	expectOK(t, servePRAction(t, testHandler.RequestPullRequestReview, f.request(http.MethodPost, "request-review", map[string]any{"reviewers": []string{"bob", "Bob", "carol"}})))
	calls := f.api.callsTo("POST " + f.pullPath() + "/requested_reviewers")
	if len(calls) != 1 || string(calls[0].Body) != `{"reviewers":["bob","carol"]}` {
		t.Fatalf("calls = %+v", calls)
	}

	for _, reviewers := range [][]string{nil, {"bad login"}, {"../x"}} {
		w := servePRAction(t, testHandler.RequestPullRequestReview, f.request(http.MethodPost, "request-review", map[string]any{"reviewers": reviewers}))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("reviewers %v: status = %d", reviewers, w.Code)
		}
	}

	f.api.reply("POST "+f.pullPath()+"/requested_reviewers", 422, `{"message":"Reviews may only be requested from collaborators. One or more of the users or teams you specified is not a collaborator of the acme/widgets repository."}`)
	w := servePRAction(t, testHandler.RequestPullRequestReview, f.request(http.MethodPost, "request-review", map[string]any{"reviewers": []string{"stranger"}}))
	body := expectErrorCode(t, w, http.StatusUnprocessableEntity, "github_request_failed")
	if !strings.HasPrefix(body.Error, "Reviews may only be requested from collaborators") {
		t.Fatalf("error = %q", body.Error)
	}
}

// --- close -------------------------------------------------------------------------------

func TestGitHubPRClose(t *testing.T) {
	f := setupPRActions(t)
	f.api.reply("PATCH "+f.pullPath(), 200, `{"state":"closed"}`)
	expectOK(t, servePRAction(t, testHandler.ClosePullRequest, f.request(http.MethodPost, "close", nil)))
	calls := f.api.callsTo("PATCH " + f.pullPath())
	if len(calls) != 1 || decodeBody(t, calls[0].Body)["state"] != "closed" {
		t.Fatalf("calls = %+v", calls)
	}
	expectUserToken(t, calls)
	acts := activityActions(t, f.issueID)
	if len(acts) != 1 || acts[0]["action"] != "pull_request_closed" || acts[0]["actor_id"] != testUserID {
		t.Fatalf("activity = %+v", acts)
	}
	if details := acts[0]["details"].(map[string]any); details["pr_number"] != float64(7) {
		t.Fatalf("details = %v", details)
	}
}

// --- merge-options ---------------------------------------------------------------------------

func TestGitHubPRMergeOptions(t *testing.T) {
	f := setupPRActions(t)
	f.api.on("POST /graphql", graphQLRoute(t, map[string]string{"viewerMergeHeadlineText": `{"data":{
		"viewer":{"login":"octocat"},
		"repository":{
			"squashMergeAllowed":true,"mergeCommitAllowed":true,"rebaseMergeAllowed":false,
			"autoMergeAllowed":true,"deleteBranchOnMerge":false,"viewerDefaultMergeMethod":"SQUASH",
			"pullRequest":{
				"id":"PR_node7","headRefOid":"abc123","isCrossRepository":false,
				"viewerDidAuthor":true,"viewerCanUpdateBranch":true,"viewerCanEnableAutoMerge":true,
				"reviewDecision":"REVIEW_REQUIRED","mergeStateStatus":"BLOCKED","mergeable":"MERGEABLE",
				"autoMergeRequest":{"mergeMethod":"SQUASH","enabledBy":{"login":"octocat"}},
				"squashTitle":"Add widget (#7)","squashBody":"* commit one",
				"mergeTitle":"Merge pull request #7 from acme/feature/widget","mergeBody":"Add widget"
			}
		}
	}}`}))
	w := servePRAction(t, testHandler.GetPullRequestMergeOptions, f.request(http.MethodGet, "merge-options", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	want := `{"viewer":{"login":"octocat","did_author":true,"can_update_branch":true,"can_enable_auto_merge":true},` +
		`"allowed_methods":["squash","merge"],"default_method":"squash",` +
		`"commit_messages":{"merge":{"title":"Merge pull request #7 from acme/feature/widget","body":"Add widget"},"squash":{"title":"Add widget (#7)","body":"* commit one"}},` +
		`"delete_branch_on_merge":false,"head_is_fork":false,"auto_merge_allowed":true,` +
		`"auto_merge":{"merge_method":"squash","enabled_by":"octocat"},"review_decision":"review_required",` +
		`"merge_state_status":"blocked","mergeable":"mergeable","head_sha":"abc123"}`
	if got := strings.TrimSpace(w.Body.String()); got != want {
		t.Fatalf("merge options =\n%s\nwant\n%s", got, want)
	}
	calls := f.api.callsTo("POST /graphql")
	if len(calls) != 1 {
		t.Fatalf("merge-options must be one GraphQL query, got %d", len(calls))
	}
	if vars := graphQLVariables(t, calls[0]); vars["owner"] != "acme" || vars["name"] != f.repo || vars["number"] != float64(7) {
		t.Fatalf("variables = %v", vars)
	}
	expectUserToken(t, calls)
}

// --- snapshot fields on the PR list ----------------------------------------------------------

func TestPullRequestResponseExposesReviewDecisionAndAutoMerge(t *testing.T) {
	f := setupPRActions(t)
	dbfx.Exec(t, `UPDATE github_pull_request
		SET snapshot_head_sha = head_sha, snapshot_fetched_at = now(),
		    review_decision = 'APPROVED', auto_merge_method = 'squash', auto_merge_enabled_by = 'octocat'
		WHERE id = $1`, f.prID)
	rows, err := testHandler.Queries.ListPullRequestsByIssue(context.Background(), util.MustParseUUID(f.issueID))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	raw, _ := json.Marshal(issuePullRequestRowToResponse(rows[0], true))
	got := decodeBody(t, raw)
	autoMerge, _ := got["auto_merge"].(map[string]any)
	if got["review_decision"] != "approved" || autoMerge["merge_method"] != "squash" || autoMerge["enabled_by"] != "octocat" {
		t.Fatalf("response = %s", raw)
	}

	dbfx.Exec(t, `UPDATE github_pull_request SET review_decision = NULL, auto_merge_method = NULL, auto_merge_enabled_by = NULL WHERE id = $1`, f.prID)
	rows, _ = testHandler.Queries.ListPullRequestsByIssue(context.Background(), util.MustParseUUID(f.issueID))
	raw, _ = json.Marshal(issuePullRequestRowToResponse(rows[0], true))
	cleared := decodeBody(t, raw)
	_, hasDecision := cleared["review_decision"]
	_, hasAutoMerge := cleared["auto_merge"]
	if !hasDecision || !hasAutoMerge || cleared["review_decision"] != nil || cleared["auto_merge"] != nil {
		t.Fatalf("absent fields must be present as null: %s", raw)
	}
}
