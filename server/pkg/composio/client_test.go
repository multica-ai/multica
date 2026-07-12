package composio_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/composio"
)

// newTestServer wires up a httptest.Server with the provided handler and
// returns a composio.Client pointed at it.
func newTestServer(t *testing.T, h http.HandlerFunc) (*composio.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := composio.NewClient(composio.Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv
}

func readJSON(t *testing.T, r *http.Request, out any) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("unmarshal body %q: %v", string(body), err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("write json: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Client construction
// ---------------------------------------------------------------------------

func TestNewClient_Defaults(t *testing.T) {
	c, err := composio.NewClient(composio.Options{APIKey: "k"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := c.BaseURL(); got != composio.DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", got, composio.DefaultBaseURL)
	}
}

func TestNewClient_RequiresAPIKey(t *testing.T) {
	_, err := composio.NewClient(composio.Options{})
	if err == nil {
		t.Fatal("expected error when APIKey is empty")
	}
}

func TestNewClient_TrimsTrailingSlash(t *testing.T) {
	c, err := composio.NewClient(composio.Options{APIKey: "k", BaseURL: "https://x.example.com/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := c.BaseURL(), "https://x.example.com"; got != want {
		t.Errorf("BaseURL = %q, want %q", got, want)
	}
}

// recordingTransport observes whether it actually handled a request.
type recordingTransport struct {
	mu     sync.Mutex
	calls  int
	last   *http.Request
	status int
}

func (rt *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.calls++
	rt.last = r
	rt.mu.Unlock()
	body := io.NopCloser(strings.NewReader(`{"items":[]}`))
	return &http.Response{
		StatusCode: rt.statusOr(200),
		Body:       body,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    r,
	}, nil
}
func (rt *recordingTransport) statusOr(d int) int {
	if rt.status == 0 {
		return d
	}
	return rt.status
}

// TestNewClient_HonorsInjectedHTTPClient asserts that when Options.HTTPClient
// is non-nil the SDK actually routes requests through *that* client — full
// fidelity, not just transport+timeout. GPT-Boy's PR review against #4603
// caught the partial behavior; this test locks the fix in.
func TestNewClient_HonorsInjectedHTTPClient(t *testing.T) {
	rt := &recordingTransport{}
	hc := &http.Client{Transport: rt}
	c, err := composio.NewClient(composio.Options{
		APIKey:     "k",
		BaseURL:    "https://api.example.invalid",
		HTTPClient: hc,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.ListToolkits(context.Background(), composio.ListToolkitsRequest{}); err != nil {
		t.Fatalf("ListToolkits: %v", err)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.calls != 1 {
		t.Fatalf("expected 1 call through injected transport, got %d", rt.calls)
	}
	if rt.last == nil || rt.last.URL.Host != "api.example.invalid" {
		t.Errorf("request did not flow through injected client: %+v", rt.last)
	}
	if got := rt.last.Header.Get("x-api-key"); got != "k" {
		t.Errorf("api key header lost in injected client: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Connect Link
// ---------------------------------------------------------------------------

func TestCreateLink_Success(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/connected_accounts/link" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("missing api key header, got %q", got)
		}
		var body composio.CreateLinkRequest
		readJSON(t, r, &body)
		if body.AuthConfigID != "ac_abc" || body.UserID != "u_1" {
			t.Errorf("unexpected body: %+v", body)
		}
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"link_token":           "ltok_xyz",
			"redirect_url":         "https://connect.composio.dev/ln_xyz",
			"expires_at":           "2026-12-31T00:00:00Z",
			"connected_account_id": "ca_pending",
		})
	})
	resp, err := c.CreateLink(context.Background(), composio.CreateLinkRequest{
		AuthConfigID: "ac_abc",
		UserID:       "u_1",
		CallbackURL:  "https://example.com/cb",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if resp.RedirectURL == "" || resp.LinkToken != "ltok_xyz" || resp.ConnectedAccountID != "ca_pending" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestCreateLink_ValidatesInputs(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be hit when inputs are invalid")
	})
	if _, err := c.CreateLink(context.Background(), composio.CreateLinkRequest{UserID: "u"}); err == nil {
		t.Error("expected error when AuthConfigID is empty")
	}
	if _, err := c.CreateLink(context.Background(), composio.CreateLinkRequest{AuthConfigID: "ac"}); err == nil {
		t.Error("expected error when UserID is empty")
	}
}

func TestCreateLink_APIError(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{
				"message":    "bad input",
				"code":       400,
				"slug":       "INVALID_INPUT",
				"request_id": "req_1",
			},
		})
	})
	_, err := c.CreateLink(context.Background(), composio.CreateLinkRequest{
		AuthConfigID: "ac", UserID: "u",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *composio.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.HTTPStatus != http.StatusBadRequest || apiErr.Slug != "INVALID_INPUT" || apiErr.Message != "bad input" {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
}

// ---------------------------------------------------------------------------
// Connected accounts list / revoke / delete
// ---------------------------------------------------------------------------

func TestListConnectedAccounts_QueryString(t *testing.T) {
	var seen *http.Request
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r
		writeJSON(t, w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"id": "ca_1", "user_id": "u_1", "status": "ACTIVE",
					"toolkit": map[string]any{"slug": "notion"}},
			},
			"next_cursor": "cur_2",
		})
	})
	resp, err := c.ListConnectedAccounts(context.Background(), composio.ListConnectedAccountsRequest{
		UserIDs:             []string{"u_1", "u_2"},
		ToolkitSlugs:        []string{"notion", "slack"},
		AuthConfigIDs:       []string{"ac_a"},
		ConnectedAccountIDs: []string{"ca_x"},
		Statuses:            []string{"ACTIVE"},
		OrderBy:             "updated_at",
		OrderDirection:      "desc",
		AccountType:         "PRIVATE",
		Limit:               25,
	})
	if err != nil {
		t.Fatalf("ListConnectedAccounts: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Toolkit.Slug != "notion" || resp.NextCursor != "cur_2" {
		t.Errorf("unexpected response: %+v", resp)
	}
	q := seen.URL.Query()

	// Per Composio v3.1 these are plural array params.
	if got := q["user_ids"]; len(got) != 2 || got[0] != "u_1" || got[1] != "u_2" {
		t.Errorf("user_ids = %v", got)
	}
	if got := q["toolkit_slugs"]; len(got) != 2 || got[0] != "notion" || got[1] != "slack" {
		t.Errorf("toolkit_slugs = %v", got)
	}
	if got := q["auth_config_ids"]; len(got) != 1 || got[0] != "ac_a" {
		t.Errorf("auth_config_ids = %v", got)
	}
	if got := q["connected_account_ids"]; len(got) != 1 || got[0] != "ca_x" {
		t.Errorf("connected_account_ids = %v", got)
	}
	if got := q["statuses"]; len(got) != 1 || got[0] != "ACTIVE" {
		t.Errorf("statuses = %v", got)
	}

	// Singular legacy keys must NOT appear — guard against regression.
	if q.Has("user_id") || q.Has("auth_config_id") {
		t.Errorf("legacy singular query keys leaked: %s", seen.URL.RawQuery)
	}

	if q.Get("order_by") != "updated_at" {
		t.Errorf("order_by = %q", q.Get("order_by"))
	}
	if q.Get("order_direction") != "desc" {
		t.Errorf("order_direction = %q", q.Get("order_direction"))
	}
	if q.Get("account_type") != "PRIVATE" {
		t.Errorf("account_type = %q", q.Get("account_type"))
	}
	if q.Get("limit") != "25" {
		t.Errorf("limit = %q", q.Get("limit"))
	}
}

func TestListConnectedAccounts_ParsesNestedAuthConfig(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{
					"id":      "ca_nested",
					"user_id": "u_1",
					"auth_config": map[string]any{
						"id":                  "ac_nested",
						"auth_scheme":         "OAUTH2",
						"is_composio_managed": true,
					},
					"toolkit": map[string]any{"slug": "notion"},
					"status":  "ACTIVE",
				},
				{
					"id":             "ca_top_level",
					"user_id":        "u_1",
					"auth_config_id": "ac_top_level",
					"auth_config":    map[string]any{"id": "ac_nested_ignored"},
					"toolkit":        map[string]any{"slug": "gmail"},
					"status":         "ACTIVE",
				},
			},
		})
	})

	resp, err := c.ListConnectedAccounts(context.Background(), composio.ListConnectedAccountsRequest{})
	if err != nil {
		t.Fatalf("ListConnectedAccounts: %v", err)
	}
	if got := resp.Items[0].AuthConfig.ID; got != "ac_nested" {
		t.Errorf("nested auth config id = %q, want ac_nested", got)
	}
	if got := resp.Items[0].AuthConfig.AuthScheme; got != "OAUTH2" {
		t.Errorf("nested auth scheme = %q, want OAUTH2", got)
	}
	if got := resp.Items[1].AuthConfigID; got != "ac_top_level" {
		t.Errorf("top-level auth config id = %q, want ac_top_level", got)
	}
}

func TestRevokeConnection_Success(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/connected_accounts/ca_42/revoke" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.RevokeConnection(context.Background(), "ca_42"); err != nil {
		t.Errorf("RevokeConnection: %v", err)
	}
}

func TestRevokeConnection_RequiresID(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be hit")
	})
	if err := c.RevokeConnection(context.Background(), ""); err == nil {
		t.Error("expected error for empty id")
	}
}

func TestDeleteConnectedAccount_IdempotentOn404(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		writeJSON(t, w, http.StatusNotFound, map[string]any{
			"error": map[string]any{"message": "not found", "status": 404, "slug": "NOT_FOUND"},
		})
	})
	if err := c.DeleteConnectedAccount(context.Background(), "ca_gone"); err != nil {
		t.Errorf("expected nil on 404, got %v", err)
	}
}

func TestDeleteConnectedAccount_PropagatesOtherErrors(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]any{
			"error": map[string]any{"message": "boom", "status": 500, "slug": "INTERNAL"},
		})
	})
	err := c.DeleteConnectedAccount(context.Background(), "ca_1")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *composio.APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusInternalServerError {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

func TestCreateSession_Success(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tool_router/session" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body composio.CreateSessionRequest
		readJSON(t, r, &body)
		if body.UserID != "u_1" {
			t.Errorf("user_id = %q", body.UserID)
		}
		if body.ManageConnections == nil || body.ManageConnections.CallbackURL != "https://cb" {
			t.Errorf("manage_connections = %+v", body.ManageConnections)
		}
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"session_id": "trs_1",
			"mcp":        map[string]any{"type": "http", "url": "https://mcp.example/trs_1"},
		})
	})
	enable := true
	resp, err := c.CreateSession(context.Background(), composio.CreateSessionRequest{
		UserID: "u_1",
		ManageConnections: &composio.ManageConnections{
			Enable:      &enable,
			CallbackURL: "https://cb",
		},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if resp.MCP.URL == "" || resp.SessionID != "trs_1" {
		t.Errorf("unexpected response: %+v", resp)
	}
	hdr := c.MCPAuthHeaders()
	if hdr["x-api-key"] != "test-key" {
		t.Errorf("MCPAuthHeaders = %v", hdr)
	}
}

func TestCreateSession_RequiresUserID(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be hit")
	})
	if _, err := c.CreateSession(context.Background(), composio.CreateSessionRequest{}); err == nil {
		t.Error("expected error for empty UserID")
	}
}

// ---------------------------------------------------------------------------
// Toolkits / Tools
// ---------------------------------------------------------------------------

func TestListToolkits_Success(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/toolkits" || r.URL.Query().Get("category") != "productivity" {
			t.Errorf("unexpected request: %s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"items": []map[string]any{
				{"slug": "notion", "name": "Notion"},
				{"slug": "slack", "name": "Slack"},
			},
		})
	})
	resp, err := c.ListToolkits(context.Background(), composio.ListToolkitsRequest{Category: "productivity"})
	if err != nil {
		t.Fatalf("ListToolkits: %v", err)
	}
	if len(resp.Items) != 2 || resp.Items[0].Slug != "notion" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestGetToolkit_Success(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/toolkits/notion" {
			t.Errorf("path = %s", r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"slug": "notion", "name": "Notion"})
	})
	tk, err := c.GetToolkit(context.Background(), "notion")
	if err != nil {
		t.Fatalf("GetToolkit: %v", err)
	}
	if tk.Slug != "notion" {
		t.Errorf("slug = %q", tk.Slug)
	}
}

func TestGetToolkit_RequiresSlug(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be hit")
	})
	if _, err := c.GetToolkit(context.Background(), ""); err == nil {
		t.Error("expected error for empty slug")
	}
}

func TestExecuteTool_Success(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/execute/GITHUB_CREATE_ISSUE" {
			t.Errorf("path = %s", r.URL.Path)
		}
		// Decode into a raw map first so we can assert wire keys directly.
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var wire map[string]any
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatalf("unmarshal wire: %v", err)
		}
		if wire["user_id"] != "u_1" {
			t.Errorf("user_id = %v", wire["user_id"])
		}
		// The spec field is `version`, not `toolkit_versions`.
		if wire["version"] != "latest" {
			t.Errorf("version = %v (want %q)", wire["version"], "latest")
		}
		if _, leaked := wire["toolkit_versions"]; leaked {
			t.Errorf("legacy toolkit_versions field leaked to wire: %s", raw)
		}
		args, _ := wire["arguments"].(map[string]any)
		if args["title"] != "hi" {
			t.Errorf("arguments.title = %v", args["title"])
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"successful": true,
			"data":       map[string]any{"issue_number": float64(42)},
			"log_id":     "log_1",
		})
	})
	resp, err := c.ExecuteTool(context.Background(), "GITHUB_CREATE_ISSUE", composio.ExecuteToolRequest{
		UserID:    "u_1",
		Arguments: map[string]any{"title": "hi"},
		Version:   "latest",
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !resp.Successful || resp.Data["issue_number"].(float64) != 42 || resp.LogID != "log_1" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

// TestExecuteToolRequest_VersionSerialization locks in the json tag for the
// Version field — GPT-Boy's review against PR #4603 caught that the field
// used to serialize as `toolkit_versions`, which is not a v3.1 wire key.
func TestExecuteToolRequest_VersionSerialization(t *testing.T) {
	req := composio.ExecuteToolRequest{
		UserID:  "u_1",
		Version: "20251027_00",
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, `"version":"20251027_00"`) {
		t.Errorf("version not serialized as `version`: %s", got)
	}
	if strings.Contains(got, "toolkit_versions") {
		t.Errorf("legacy toolkit_versions key leaked: %s", got)
	}

	// Zero-value Version must omit the field entirely (omitempty).
	bEmpty, _ := json.Marshal(composio.ExecuteToolRequest{UserID: "u_1"})
	if strings.Contains(string(bEmpty), "version") {
		t.Errorf("empty Version should omit, got: %s", bEmpty)
	}
}

func TestExecuteTool_ValidatesInputs(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be hit")
	})
	if _, err := c.ExecuteTool(context.Background(), "", composio.ExecuteToolRequest{UserID: "u"}); err == nil {
		t.Error("expected error for empty tool slug")
	}
	if _, err := c.ExecuteTool(context.Background(), "X", composio.ExecuteToolRequest{}); err == nil {
		t.Error("expected error when neither UserID nor ConnectedAccountID is set")
	}
}

// ---------------------------------------------------------------------------
// Error parsing
// ---------------------------------------------------------------------------

func TestAPIError_FallbackOnNonJSONBody(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>upstream down</html>"))
	})
	_, err := c.ListToolkits(context.Background(), composio.ListToolkitsRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *composio.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.HTTPStatus != http.StatusBadGateway {
		t.Errorf("status = %d", apiErr.HTTPStatus)
	}
	if !strings.Contains(string(apiErr.RawBody), "upstream down") {
		t.Errorf("raw body lost: %q", apiErr.RawBody)
	}
}

func TestAPIError_HelperPredicates(t *testing.T) {
	e := &composio.APIError{HTTPStatus: http.StatusNotFound}
	if !e.IsNotFound() {
		t.Error("IsNotFound() = false")
	}
	e2 := &composio.APIError{HTTPStatus: http.StatusUnauthorized}
	if !e2.IsUnauthorized() {
		t.Error("IsUnauthorized() = false")
	}
	e3 := &composio.APIError{HTTPStatus: http.StatusTooManyRequests}
	if !e3.IsRateLimited() {
		t.Error("IsRateLimited() = false")
	}
}
