package main

// CEREBRO-PATCH(server-integration-test-cerebro): cerebro modification of upstream file

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

var (
	testServer      *httptest.Server
	testPool        *pgxpool.Pool
	testToken       string
	testUserID      string
	testWorkspaceID string
)

// jwtSecret is resolved at runtime via auth.JWTSecret() so it respects
// the JWT_SECRET env var (set in .env) and stays in sync with the server.

const (
	integrationTestEmail         = "integration-test@multica.ai"
	integrationTestName          = "Integration Tester"
	integrationTestWorkspaceSlug = "integration-tests"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping integration tests: could not connect to database: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping integration tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}

	testPool = pool
	testUserID, testWorkspaceID, err = setupIntegrationTestFixture(ctx, pool)
	if err != nil {
		fmt.Printf("Failed to set up integration test fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	hub := realtime.NewHub()
	go hub.Run()

	bus := events.New()
	registerListeners(bus, hub)
	// CEREBRO-PATCH(server-integration-test-cerebro): pushSvc=nil for tests
	router := NewRouter(pool, hub, bus, analytics.NoopClient{}, nil, nil)
	testServer = httptest.NewServer(router)

	// Generate a JWT token directly for the test user
	testToken, err = generateTestJWT(testUserID, integrationTestEmail, integrationTestName)
	if err != nil {
		fmt.Printf("Failed to generate test JWT: %v\n", err)
		testServer.Close()
		pool.Close()
		os.Exit(1)
	}

	code := m.Run()

	if err := cleanupIntegrationTestFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean up integration test fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	testServer.Close()
	pool.Close()
	os.Exit(code)
}

func setupIntegrationTestFixture(ctx context.Context, pool *pgxpool.Pool) (string, string, error) {
	if err := cleanupIntegrationTestFixture(ctx, pool); err != nil {
		return "", "", err
	}

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, integrationTestName, integrationTestEmail).Scan(&userID); err != nil {
		return "", "", err
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "Integration Tests", integrationTestWorkspaceSlug, "Temporary workspace for router integration tests").Scan(&workspaceID); err != nil {
		return "", "", err
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		return "", "", err
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
		RETURNING id
	`, workspaceID, "Integration Test Runtime", "integration_test_runtime", "Integration test runtime").Scan(&runtimeID); err != nil {
		return "", "", err
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
	`, workspaceID, "Integration Test Agent", runtimeID, userID); err != nil {
		return "", "", err
	}

	return userID, workspaceID, nil
}

func cleanupIntegrationTestFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, integrationTestWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, integrationTestEmail); err != nil {
		return err
	}
	return nil
}

// Helper to make authenticated requests
func authRequest(t *testing.T, method, path string, body any) *http.Response {
	return authRequestWithWorkspace(t, method, path, testWorkspaceID, body)
}

func authRequestWithWorkspace(t *testing.T, method, path, workspaceID string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, testServer.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-ID", workspaceID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func readJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func generateTestJWT(userID, email, name string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"name":  name,
		"exp":   time.Now().Add(72 * time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})
	return token.SignedString(auth.JWTSecret())
}

// ---- Health ----

func TestHealth(t *testing.T) {
	resp, err := http.Get(testServer.URL + "/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Fatalf("expected status ok, got %s", result["status"])
	}
}

func TestReadinessEndpoints(t *testing.T) {
	for _, path := range []string{"/readyz", "/healthz"} {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(testServer.URL + path)
			if err != nil {
				t.Fatalf("readiness check failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}

			var result struct {
				Status string            `json:"status"`
				Checks map[string]string `json:"checks"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if result.Status != "ok" {
				t.Fatalf("expected status ok, got %s", result.Status)
			}
			if result.Checks["db"] != "ok" {
				t.Fatalf("expected db check ok, got %s", result.Checks["db"])
			}
			if result.Checks["migrations"] != "ok" {
				t.Fatalf("expected migrations check ok, got %s", result.Checks["migrations"])
			}
		})
	}
}

func TestConfigRouteIsPublic(t *testing.T) {
	resp, err := http.Get(testServer.URL + "/api/config")
	if err != nil {
		t.Fatalf("config request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result struct {
		CdnDomain string `json:"cdn_domain"`
	}
	readJSON(t, resp, &result)
}

// ---- Auth ----

func TestSendCodeAndVerify(t *testing.T) {
	const email = "integration-sendcode@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		var userID string
		err := testPool.QueryRow(ctx, `SELECT id FROM "user" WHERE email = $1`, email).Scan(&userID)
		if err == nil {
			rows, queryErr := testPool.Query(ctx, `
				SELECT w.id FROM workspace w JOIN member m ON m.workspace_id = w.id WHERE m.user_id = $1
			`, userID)
			if queryErr == nil {
				defer rows.Close()
				for rows.Next() {
					var wsID string
					if rows.Scan(&wsID) == nil {
						testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)
					}
				}
			}
		}
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	// Step 1: Send code
	body, _ := json.Marshal(map[string]string{"email": email})
	resp, err := http.Post(testServer.URL+"/auth/send-code", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("send-code failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("send-code: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Read code from DB
	var code string
	err = testPool.QueryRow(ctx, `SELECT code FROM verification_code WHERE email = $1 ORDER BY created_at DESC LIMIT 1`, email).Scan(&code)
	if err != nil {
		t.Fatalf("failed to read code from DB: %v", err)
	}

	// Step 2: Verify code
	body, _ = json.Marshal(map[string]string{"email": email, "code": code})
	resp, err = http.Post(testServer.URL+"/auth/verify-code", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("verify-code failed: %v", err)
	}
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("verify-code: expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	var loginResp struct {
		Token string `json:"token"`
		User  struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	readJSON(t, resp, &loginResp)

	if loginResp.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if loginResp.User.Email != email {
		t.Fatalf("expected email '%s', got '%s'", email, loginResp.User.Email)
	}

	// Verify the token works with /api/me
	req, _ := http.NewRequest("GET", testServer.URL+"/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Token)
	meResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("getMe failed: %v", err)
	}
	if meResp.StatusCode != 200 {
		t.Fatalf("getMe: expected 200, got %d", meResp.StatusCode)
	}
	meResp.Body.Close()
}

func TestVerifyCodeNewUserHasNoWorkspace(t *testing.T) {
	const email = "new-integration-verify@multica.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)

	// Send code
	body, _ := json.Marshal(map[string]string{"email": email})
	resp, err := http.Post(testServer.URL+"/auth/send-code", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("send-code failed: %v", err)
	}
	resp.Body.Close()

	// Read code from DB
	var code string
	err = testPool.QueryRow(ctx, `SELECT code FROM verification_code WHERE email = $1 ORDER BY created_at DESC LIMIT 1`, email).Scan(&code)
	if err != nil {
		t.Fatalf("failed to read code from DB: %v", err)
	}

	// Verify code
	body, _ = json.Marshal(map[string]string{"email": email, "code": code})
	resp, err = http.Post(testServer.URL+"/auth/verify-code", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("verify-code failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify-code: expected 200, got %d", resp.StatusCode)
	}

	var loginResp struct {
		Token string `json:"token"`
	}
	readJSON(t, resp, &loginResp)

	// New users should have no workspaces (/workspaces/new creates one)
	req, _ := http.NewRequest("GET", testServer.URL+"/api/workspaces", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Token)
	workspacesResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("listWorkspaces failed: %v", err)
	}
	defer workspacesResp.Body.Close()

	if workspacesResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", workspacesResp.StatusCode)
	}

	var workspaces []struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	readJSON(t, workspacesResp, &workspaces)

	if len(workspaces) != 0 {
		t.Fatalf("expected 0 workspaces for new user, got %d", len(workspaces))
	}
}

func TestProtectedRoutesRequireAuth(t *testing.T) {
	paths := []string{"/api/me", "/api/issues", "/api/agents", "/api/inbox", "/api/workspaces"}

	for _, path := range paths {
		resp, err := http.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("request to %s failed: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("%s: expected 401, got %d", path, resp.StatusCode)
		}
	}
}

func TestInvalidJWT(t *testing.T) {
	cases := []struct {
		name  string
		token string
	}{
		{"garbage token", "not-a-jwt"},
		{"empty token", ""},
		{"wrong secret", func() string {
			claims := jwt.MapClaims{"sub": "test", "exp": time.Now().Add(time.Hour).Unix()}
			t, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("wrong"))
			return t
		}()},
		{"expired token", func() string {
			claims := jwt.MapClaims{"sub": "test", "exp": time.Now().Add(-time.Hour).Unix()}
			t, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(auth.JWTSecret())
			return t
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", testServer.URL+"/api/me", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != 401 {
				t.Fatalf("expected 401, got %d", resp.StatusCode)
			}
		})
	}
}

// ---- Issues CRUD through full router ----

func TestIssuesCRUDThroughRouter(t *testing.T) {
	// Create
	resp := authRequest(t, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Integration test issue",
		"status":   "todo",
		"priority": "high",
	})
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CreateIssue: expected 201, got %d: %s", resp.StatusCode, body)
	}

	var created map[string]any
	readJSON(t, resp, &created)
	issueID := created["id"].(string)
	if created["title"] != "Integration test issue" {
		t.Fatalf("expected title 'Integration test issue', got '%s'", created["title"])
	}

	// Get
	resp = authRequest(t, "GET", "/api/issues/"+issueID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("GetIssue: expected 200, got %d", resp.StatusCode)
	}
	var fetched map[string]any
	readJSON(t, resp, &fetched)
	if fetched["id"] != issueID {
		t.Fatalf("expected id %s, got %s", issueID, fetched["id"])
	}

	// Update status only — should preserve title
	resp = authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"status": "in_progress",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("UpdateIssue: expected 200, got %d", resp.StatusCode)
	}
	var updated map[string]any
	readJSON(t, resp, &updated)
	if updated["status"] != "in_progress" {
		t.Fatalf("expected status 'in_progress', got '%s'", updated["status"])
	}
	if updated["title"] != "Integration test issue" {
		t.Fatalf("title should be preserved, got '%s'", updated["title"])
	}

	// Update title only — should preserve status
	resp = authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"title": "Renamed integration issue",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("UpdateIssue title: expected 200, got %d", resp.StatusCode)
	}
	var updated2 map[string]any
	readJSON(t, resp, &updated2)
	if updated2["title"] != "Renamed integration issue" {
		t.Fatalf("expected title 'Renamed integration issue', got '%s'", updated2["title"])
	}
	if updated2["status"] != "in_progress" {
		t.Fatalf("status should be preserved, got '%s'", updated2["status"])
	}

	// List
	resp = authRequest(t, "GET", "/api/issues?workspace_id="+testWorkspaceID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("ListIssues: expected 200, got %d", resp.StatusCode)
	}
	var listResp map[string]any
	readJSON(t, resp, &listResp)
	total := listResp["total"].(float64)
	if total < 1 {
		t.Fatal("expected at least 1 issue")
	}

	// Delete
	resp = authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("DeleteIssue: expected 204, got %d", resp.StatusCode)
	}

	// Verify deleted
	resp = authRequest(t, "GET", "/api/issues/"+issueID, nil)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("GetIssue after delete: expected 404, got %d", resp.StatusCode)
	}
}

// TestSearchIssuesNotShadowedByIDRoute is a regression guard for JEH-378.
// JEH-324 added a flat GET /api/issues/{id} registration in the task-
// allowlist group, which made chi greedily route /api/issues/search to
// GetIssue with id="search" instead of SearchIssues — returning 404
// "issue not found" and breaking the cmd+k search palette. The fix
// registers /search and /child-progress as flat siblings of /{id} in
// the same routing tree. This test exercises the full router so any
// future re-shadowing fails here, not silently in production.
func TestSearchIssuesNotShadowedByIDRoute(t *testing.T) {
	// Create an issue so the search has something to find — and so a
	// 200 with zero results would still be a regression (the request
	// would never reach SearchIssues at all).
	createResp := authRequest(t, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Searchable router shadow regression",
		"status": "todo",
	})
	if createResp.StatusCode != 201 {
		body, _ := io.ReadAll(createResp.Body)
		createResp.Body.Close()
		t.Fatalf("setup CreateIssue: expected 201, got %d: %s", createResp.StatusCode, body)
	}
	var created map[string]any
	readJSON(t, createResp, &created)
	issueID := created["id"].(string)
	t.Cleanup(func() {
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	resp := authRequest(t, "GET", "/api/issues/search?q=Searchable+router+shadow", nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("SearchIssues: got 404 %q — /api/issues/search is being shadowed by GET /api/issues/{id}", body)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("SearchIssues: expected 200, got %d: %s", resp.StatusCode, body)
	}

	// SearchIssues returns an object with "issues" + "total"; GetIssue
	// would have returned a single issue object with "id" at the top
	// level. The shape disambiguates which handler ran.
	var searchResp map[string]any
	readJSON(t, resp, &searchResp)
	if _, ok := searchResp["issues"]; !ok {
		t.Fatalf("SearchIssues: response missing 'issues' field — looks like GetIssue handled the request: %v", searchResp)
	}
	if _, ok := searchResp["total"]; !ok {
		t.Fatalf("SearchIssues: response missing 'total' field — looks like GetIssue handled the request: %v", searchResp)
	}
}

// TestChildIssueProgressNotShadowedByIDRoute mirrors the search
// regression for the second static GET sibling.
func TestChildIssueProgressNotShadowedByIDRoute(t *testing.T) {
	resp := authRequest(t, "GET", "/api/issues/child-progress", nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("ChildIssueProgress: got 404 %q — /api/issues/child-progress is being shadowed by GET /api/issues/{id}", body)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("ChildIssueProgress: expected 200, got %d: %s", resp.StatusCode, body)
	}
}

// ---- Comments through full router ----

func TestCommentsThroughRouter(t *testing.T) {
	// Create issue
	resp := authRequest(t, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Comment integration test",
	})
	var issue map[string]any
	readJSON(t, resp, &issue)
	issueID := issue["id"].(string)

	// Create comment
	resp = authRequest(t, "POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "Integration test comment",
		"type":    "comment",
	})
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CreateComment: expected 201, got %d: %s", resp.StatusCode, body)
	}
	var comment map[string]any
	readJSON(t, resp, &comment)
	if comment["content"] != "Integration test comment" {
		t.Fatalf("expected content 'Integration test comment', got '%s'", comment["content"])
	}

	// Create second comment
	resp = authRequest(t, "POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "Second comment",
		"type":    "comment",
	})
	resp.Body.Close()

	// List comments
	resp = authRequest(t, "GET", "/api/issues/"+issueID+"/comments", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("ListComments: expected 200, got %d", resp.StatusCode)
	}
	var comments []map[string]any
	readJSON(t, resp, &comments)
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}

	// Cleanup
	resp = authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
	resp.Body.Close()
}

// ---- Agents through full router ----

func TestAgentsThroughRouter(t *testing.T) {
	// List
	resp := authRequest(t, "GET", "/api/agents?workspace_id="+testWorkspaceID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("ListAgents: expected 200, got %d", resp.StatusCode)
	}
	var agents []map[string]any
	readJSON(t, resp, &agents)
	if len(agents) < 1 {
		t.Fatal("expected at least 1 agent")
	}

	// Get
	agentID := agents[0]["id"].(string)
	resp = authRequest(t, "GET", "/api/agents/"+agentID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("GetAgent: expected 200, got %d", resp.StatusCode)
	}
	var agent map[string]any
	readJSON(t, resp, &agent)
	if agent["id"] != agentID {
		t.Fatalf("expected agent id %s, got %s", agentID, agent["id"])
	}

	// Update status
	resp = authRequest(t, "PUT", "/api/agents/"+agentID, map[string]any{
		"status": "idle",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("UpdateAgent: expected 200, got %d", resp.StatusCode)
	}
	var updated map[string]any
	readJSON(t, resp, &updated)
	if updated["status"] != "idle" {
		t.Fatalf("expected status 'idle', got '%s'", updated["status"])
	}
	// Name should be preserved
	if updated["name"] != agents[0]["name"] {
		t.Fatalf("name should be preserved, got '%s'", updated["name"])
	}
}

// ---- Workspaces through full router ----

func TestWorkspacesThroughRouter(t *testing.T) {
	// List
	resp := authRequest(t, "GET", "/api/workspaces", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("ListWorkspaces: expected 200, got %d", resp.StatusCode)
	}
	var workspaces []map[string]any
	readJSON(t, resp, &workspaces)
	if len(workspaces) < 1 {
		t.Fatal("expected at least 1 workspace")
	}

	// Get
	wsID := workspaces[0]["id"].(string)
	resp = authRequest(t, "GET", "/api/workspaces/"+wsID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("GetWorkspace: expected 200, got %d", resp.StatusCode)
	}
	var ws map[string]any
	readJSON(t, resp, &ws)
	if ws["id"] != wsID {
		t.Fatalf("expected workspace id %s, got %s", wsID, ws["id"])
	}

	// Update
	resp = authRequest(t, "PUT", "/api/workspaces/"+wsID, map[string]any{
		"description": "Integration test update",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("UpdateWorkspace: expected 200, got %d", resp.StatusCode)
	}
	var updated map[string]any
	readJSON(t, resp, &updated)
	if updated["description"] != "Integration test update" {
		t.Fatalf("expected description 'Integration test update', got '%v'", updated["description"])
	}
	// Name should be preserved
	if updated["name"] != ws["name"] {
		t.Fatalf("name should be preserved")
	}

	// Members
	resp = authRequest(t, "GET", "/api/workspaces/"+wsID+"/members", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("ListMembers: expected 200, got %d", resp.StatusCode)
	}
	var members []map[string]any
	readJSON(t, resp, &members)
	if len(members) < 1 {
		t.Fatal("expected at least 1 member")
	}
	// Verify member has user info
	if members[0]["email"] == nil || members[0]["email"] == "" {
		t.Fatal("member should have email field")
	}
	if members[0]["role"] == nil || members[0]["role"] == "" {
		t.Fatal("member should have role field")
	}
}

// CEREBRO-PATCH(cerebro-groups-router-tests): JEH-721 groups endpoint coverage.
func TestCerebroGroupsThroughRouter(t *testing.T) {
	ctx := context.Background()

	memberEmail := fmt.Sprintf("group-member-%d@multica.ai", time.Now().UnixNano())
	var memberUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Group Member", memberEmail).Scan(&memberUserID); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberUserID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, memberUserID); err != nil {
		t.Fatalf("create workspace member: %v", err)
	}

	resp := authRequest(t, "POST", "/api/workspaces/"+testWorkspaceID+"/groups", map[string]any{
		"name":        "Router Group",
		"description": "Created by integration test",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CreateGroup: expected 201, got %d", resp.StatusCode)
	}
	var created map[string]any
	readJSON(t, resp, &created)
	groupID := created["id"].(string)
	if created["name"] != "Router Group" {
		t.Fatalf("CreateGroup name = %v", created["name"])
	}
	if created["created_by"] != testUserID {
		t.Fatalf("CreateGroup created_by = %v, want %s", created["created_by"], testUserID)
	}

	resp = authRequest(t, "GET", "/api/workspaces/"+testWorkspaceID+"/groups", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListGroups: expected 200, got %d", resp.StatusCode)
	}
	var groups []map[string]any
	readJSON(t, resp, &groups)
	found := false
	for _, group := range groups {
		if group["id"] == groupID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListGroups did not include created group %s", groupID)
	}

	resp = authRequest(t, "GET", "/api/groups/"+groupID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetGroup: expected 200, got %d", resp.StatusCode)
	}
	var fetched map[string]any
	readJSON(t, resp, &fetched)
	if fetched["id"] != groupID {
		t.Fatalf("GetGroup id = %v, want %s", fetched["id"], groupID)
	}

	resp = authRequest(t, "PATCH", "/api/groups/"+groupID, map[string]any{
		"name": "Updated Router Group",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("UpdateGroup: expected 200, got %d", resp.StatusCode)
	}
	var updated map[string]any
	readJSON(t, resp, &updated)
	if updated["name"] != "Updated Router Group" {
		t.Fatalf("UpdateGroup name = %v", updated["name"])
	}

	resp = authRequest(t, "POST", "/api/groups/"+groupID+"/members", map[string]any{
		"user_id": memberUserID,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("AddGroupMember: expected 201, got %d", resp.StatusCode)
	}
	var added map[string]any
	readJSON(t, resp, &added)
	if added["user_id"] != memberUserID {
		t.Fatalf("AddGroupMember user_id = %v, want %s", added["user_id"], memberUserID)
	}

	resp = authRequest(t, "GET", "/api/groups/"+groupID+"/members", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListGroupMembers: expected 200, got %d", resp.StatusCode)
	}
	var members []map[string]any
	readJSON(t, resp, &members)
	found = false
	for _, member := range members {
		if member["user_id"] == memberUserID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListGroupMembers did not include %s", memberUserID)
	}

	resp = authRequest(t, "DELETE", "/api/groups/"+groupID+"/members/"+memberUserID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("RemoveGroupMember: expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = authRequest(t, "GET", "/api/groups/"+groupID+"/members", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListGroupMembers after remove: expected 200, got %d", resp.StatusCode)
	}
	readJSON(t, resp, &members)
	for _, member := range members {
		if member["user_id"] == memberUserID {
			t.Fatalf("ListGroupMembers still includes removed user %s", memberUserID)
		}
	}

	resp = authRequest(t, "DELETE", "/api/groups/"+groupID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DeleteGroup: expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = authRequest(t, "GET", "/api/groups/"+groupID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GetGroup after delete: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCerebroGroupsVisibilityAndValidation(t *testing.T) {
	ctx := context.Background()

	resp := authRequest(t, "POST", "/api/workspaces/"+testWorkspaceID+"/groups", map[string]any{
		"name": " ",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("CreateGroup empty name: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = authRequest(t, "POST", "/api/workspaces/"+testWorkspaceID+"/groups", map[string]any{
		"name": "Validation Group",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CreateGroup validation fixture: expected 201, got %d", resp.StatusCode)
	}
	var group map[string]any
	readJSON(t, resp, &group)
	groupID := group["id"].(string)

	nonMemberEmail := fmt.Sprintf("group-non-member-%d@multica.ai", time.Now().UnixNano())
	var nonMemberUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Group Non Member", nonMemberEmail).Scan(&nonMemberUserID); err != nil {
		t.Fatalf("create non-member user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, nonMemberUserID)
	})

	resp = authRequest(t, "POST", "/api/groups/"+groupID+"/members", map[string]any{
		"user_id": nonMemberUserID,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("AddGroupMember non-workspace user: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	noAccessSlug := fmt.Sprintf("group-no-access-%d", time.Now().UnixNano())
	var noAccessWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "Group No Access", noAccessSlug, "No membership for integration user").Scan(&noAccessWorkspaceID); err != nil {
		t.Fatalf("create no-access workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, noAccessWorkspaceID)
	})

	resp = authRequest(t, "GET", "/api/workspaces/"+noAccessWorkspaceID+"/groups", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("ListGroups non-member workspace: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	otherSlug := fmt.Sprintf("group-other-%d", time.Now().UnixNano())
	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "Group Other Workspace", otherSlug, "Cross-workspace visibility fixture").Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, otherWorkspaceID, testUserID); err != nil {
		t.Fatalf("create other workspace membership: %v", err)
	}

	resp = authRequestWithWorkspace(t, "POST", "/api/workspaces/"+otherWorkspaceID+"/groups", otherWorkspaceID, map[string]any{
		"name": "Other Workspace Group",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CreateGroup other workspace: expected 201, got %d", resp.StatusCode)
	}
	var otherGroup map[string]any
	readJSON(t, resp, &otherGroup)
	otherGroupID := otherGroup["id"].(string)

	resp = authRequest(t, "GET", "/api/groups/"+otherGroupID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GetGroup cross-workspace: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// CEREBRO-PATCH(cerebro-groups-description-coercion-tests): JEH-1055 — creating
// a group without a description (or with an explicit null) used to surface as
// 500 "groups request failed" because the server passed NULL through to a
// column that was eventually constrained NOT NULL. The service now coerces a
// missing description to an empty string on create. These tests pin the
// behaviour so the path can't regress and the description response field stays
// a string.
func TestCerebroGroupsDescriptionOmittedOrNull(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"omitted":      {"name": "JEH-1055 Omitted"},
		"explicit_nil": {"name": "JEH-1055 Null", "description": nil},
	} {
		t.Run(name, func(t *testing.T) {
			resp := authRequest(t, "POST", "/api/workspaces/"+testWorkspaceID+"/groups", body)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("CreateGroup %s: expected 201, got %d", name, resp.StatusCode)
			}
			var created map[string]any
			readJSON(t, resp, &created)
			if got := created["description"]; got != "" {
				t.Fatalf("CreateGroup %s description = %v (%T), want \"\"", name, got, got)
			}
			groupID := created["id"].(string)
			t.Cleanup(func() {
				_, _ = testPool.Exec(context.Background(), `DELETE FROM cerebro_group WHERE id = $1`, groupID)
			})

			// Update path: PATCH with an explicit empty string clears the
			// description via COALESCE (empty string is non-NULL in SQL).
			resp = authRequest(t, "PATCH", "/api/groups/"+groupID, map[string]any{
				"description": "set",
			})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("UpdateGroup %s set description: expected 200, got %d", name, resp.StatusCode)
			}
			var updated map[string]any
			readJSON(t, resp, &updated)
			if updated["description"] != "set" {
				t.Fatalf("UpdateGroup %s description = %v, want \"set\"", name, updated["description"])
			}

			resp = authRequest(t, "PATCH", "/api/groups/"+groupID, map[string]any{
				"description": "",
			})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("UpdateGroup %s clear description: expected 200, got %d", name, resp.StatusCode)
			}
			readJSON(t, resp, &updated)
			if updated["description"] != "" {
				t.Fatalf("UpdateGroup %s cleared description = %v, want \"\"", name, updated["description"])
			}
		})
	}
}

// TestDeleteWorkspaceRequiresOwner is a defense-in-depth regression test for the
// permission gate on DELETE /api/workspaces/{id}. It creates a separate workspace
// in which the integration test user is only an "admin" (not "owner") and asserts
// that DELETE returns 403, leaving the workspace intact. This guards against both
// router misconfiguration (missing middleware) and handler regressions.
func TestDeleteWorkspaceRequiresOwner(t *testing.T) {
	ctx := context.Background()

	const slug = "integration-tests-delete-403"
	// Best-effort cleanup from any prior run.
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "Integration Tests Delete 403", slug, "DeleteWorkspace permission test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'admin')
	`, wsID, testUserID); err != nil {
		t.Fatalf("create admin member: %v", err)
	}

	req, err := http.NewRequest("DELETE", testServer.URL+"/api/workspaces/"+wsID, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-ID", wsID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-owner DELETE, got %d", resp.StatusCode)
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if !exists {
		t.Fatal("workspace was deleted despite non-owner request")
	}
}

// ---- Inbox through full router ----

func TestInboxThroughRouter(t *testing.T) {
	resp := authRequest(t, "GET", "/api/inbox", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("ListInbox: expected 200, got %d", resp.StatusCode)
	}
	var items []map[string]any
	readJSON(t, resp, &items)
	// Inbox may be empty, just verify it returns valid JSON array
	if items == nil {
		t.Fatal("expected non-nil inbox items array")
	}
}

// ---- 404 for non-existent resources ----

func TestNonExistentResources(t *testing.T) {
	fakeUUID := "00000000-0000-0000-0000-000000000000"

	cases := []struct {
		name string
		path string
	}{
		{"issue", "/api/issues/" + fakeUUID},
		{"agent", "/api/agents/" + fakeUUID},
		{"workspace", "/api/workspaces/" + fakeUUID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := authRequest(t, "GET", tc.path, nil)
			resp.Body.Close()
			if resp.StatusCode != 404 {
				t.Fatalf("expected 404, got %d", resp.StatusCode)
			}
		})
	}
}

// ---- Invalid request bodies ----

func TestInvalidRequestBodies(t *testing.T) {
	resp := authRequest(t, "POST", "/api/issues?workspace_id="+testWorkspaceID, nil)
	defer resp.Body.Close()
	// Sending nil body should fail with 400
	if resp.StatusCode != 400 {
		// Some handlers may return 500 for nil body, that's acceptable too
		if resp.StatusCode != 500 {
			t.Fatalf("expected 400 or 500, got %d", resp.StatusCode)
		}
	}
}

// ---- WebSocket integration through full router ----

func TestWebSocketIntegration(t *testing.T) {
	// Connect WebSocket client (no token in URL — first-message auth)
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/ws?workspace_id=" + testWorkspaceID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket connection failed: %v", err)
	}
	defer conn.Close()

	// First-message auth
	authMsg, _ := json.Marshal(map[string]any{
		"type":    "auth",
		"payload": map[string]string{"token": testToken},
	})
	if err := conn.WriteMessage(websocket.TextMessage, authMsg); err != nil {
		t.Fatalf("failed to send auth message: %v", err)
	}

	// Read auth_ack
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, ack, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read auth_ack: %v", err)
	}
	if !strings.Contains(string(ack), "auth_ack") {
		t.Fatalf("expected auth_ack, got %s", ack)
	}
	conn.SetReadDeadline(time.Time{})

	// Allow Hub goroutine to process the register and add client to room
	time.Sleep(100 * time.Millisecond)

	// Create an issue — this should trigger a WebSocket broadcast
	resp := authRequest(t, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "WebSocket test issue",
		"status": "todo",
	})
	var issue map[string]any
	readJSON(t, resp, &issue)
	issueID := issue["id"].(string)

	// Read the WebSocket message
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("WebSocket read error: %v", err)
	}

	// Verify the message contains the issue event
	var wsMsg map[string]any
	if err := json.Unmarshal(msg, &wsMsg); err != nil {
		t.Fatalf("failed to parse WebSocket message: %v", err)
	}
	if wsMsg["type"] != "issue:created" {
		t.Fatalf("expected type 'issue:created', got '%s'", wsMsg["type"])
	}

	// Update the issue — should trigger another broadcast
	resp = authRequest(t, "PUT", "/api/issues/"+issueID, map[string]any{
		"status": "in_progress",
	})
	resp.Body.Close()

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("WebSocket read error on update: %v", err)
	}
	var updateMsg map[string]any
	json.Unmarshal(msg, &updateMsg)
	if updateMsg["type"] != "issue:updated" {
		t.Fatalf("expected type 'issue:updated', got '%s'", updateMsg["type"])
	}

	// Delete the issue — should trigger another broadcast
	resp = authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
	resp.Body.Close()

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("WebSocket read error on delete: %v", err)
	}
	var deleteMsg map[string]any
	json.Unmarshal(msg, &deleteMsg)
	if deleteMsg["type"] != "issue:deleted" {
		t.Fatalf("expected type 'issue:deleted', got '%s'", deleteMsg["type"])
	}
}

// TestRouterBlastRadius_NoStaticShadowedByParam locks down the full
// blast radius of the JEH-324 chi-router regression. JEH-378 fixed the
// two known shadowing victims (/api/issues/search and
// /api/issues/child-progress); this test exercises every other static
// GET that lives next to a sibling {param} route in the same chi
// subtree, asserting the request reaches a real handler rather than
// being greedily captured by the {param} sibling.
//
// The contract is intentionally loose: any non-404 response means chi
// dispatched to the static handler (the handler may then 200, 400, or
// 403 depending on validation). A 404 means chi sent the request to a
// {param} handler that responded "not found" instead — that's the
// regression we're guarding against.
func TestRouterBlastRadius_NoStaticShadowedByParam(t *testing.T) {
	// Create one issue used by every /api/issues/{id}/<sub> case so the
	// {id} segment is a real UUID (eliminates "handler returned 404
	// because issue doesn't exist" as a confounding signal).
	createResp := authRequest(t, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "blast radius",
		"status": "todo",
	})
	if createResp.StatusCode != 201 {
		body, _ := io.ReadAll(createResp.Body)
		createResp.Body.Close()
		t.Fatalf("setup CreateIssue: expected 201, got %d: %s", createResp.StatusCode, body)
	}
	var created map[string]any
	readJSON(t, createResp, &created)
	issueID := created["id"].(string)
	t.Cleanup(func() {
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	cases := []struct {
		name string
		path string
	}{
		// --- Top-level static GETs sharing /api/<resource> with a {id} sibling ---
		{"issues list", "/api/issues?workspace_id=" + testWorkspaceID},
		{"agents list", "/api/agents?workspace_id=" + testWorkspaceID},
		{"projects list", "/api/projects"},
		{"projects search", "/api/projects/search?q=foo"},
		{"projects by-repo", "/api/projects/by-repo?repo_url=https://example.com/foo"},
		{"work-sessions active", "/api/work-sessions/active"},
		{"usage daily", "/api/usage/daily"},
		{"usage summary", "/api/usage/summary"},
		{"runtimes list", "/api/runtimes"},
		{"skills list", "/api/skills"},
		{"autopilots list", "/api/autopilots"},
		{"pins list", "/api/pins"},
		{"artifacts list", "/api/artifacts"},
		{"artifact-folders list", "/api/artifact-folders"},
		{"inbox unread-count", "/api/inbox/unread-count"},
		{"inbox active-issue-tasks", "/api/inbox/active-issue-tasks"},
		{"inbox notifications", "/api/inbox/notifications"},
		{"inbox notifications unread-count", "/api/inbox/notifications/unread-count"},
		{"chat pending-tasks", "/api/chat/pending-tasks"},
		{"assignee-frequency", "/api/assignee-frequency"},
		{"invitations list", "/api/invitations"},

		// --- /api/issues/{id}/<sub> static siblings of GET /api/issues/{id} ---
		{"issue timeline", "/api/issues/" + issueID + "/timeline"},
		{"issue subscribers", "/api/issues/" + issueID + "/subscribers"},
		{"issue active-task", "/api/issues/" + issueID + "/active-task"},
		{"issue task-runs", "/api/issues/" + issueID + "/task-runs"},
		{"issue usage", "/api/issues/" + issueID + "/usage"},
		{"issue attachments", "/api/issues/" + issueID + "/attachments"},
		{"issue artifacts", "/api/issues/" + issueID + "/artifacts"},
		{"issue children", "/api/issues/" + issueID + "/children"},
		{"issue work-sessions", "/api/issues/" + issueID + "/work-sessions"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := authRequest(t, "GET", tc.path, nil)
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("GET %s: got 404 %q — route is being shadowed by a {param} sibling", tc.path, body)
			}
		})
	}
}
