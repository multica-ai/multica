package composio_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		UserID:       "u_1",
		ToolkitSlugs: []string{"notion", "slack"},
		Statuses:     []string{"ACTIVE"},
		Limit:        25,
	})
	if err != nil {
		t.Fatalf("ListConnectedAccounts: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Toolkit.Slug != "notion" || resp.NextCursor != "cur_2" {
		t.Errorf("unexpected response: %+v", resp)
	}
	q := seen.URL.Query()
	if q.Get("user_id") != "u_1" {
		t.Errorf("user_id = %q", q.Get("user_id"))
	}
	if got := q["toolkit_slugs"]; len(got) != 2 || got[0] != "notion" || got[1] != "slack" {
		t.Errorf("toolkit_slugs = %v", got)
	}
	if q.Get("limit") != "25" {
		t.Errorf("limit = %q", q.Get("limit"))
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
		var body composio.ExecuteToolRequest
		readJSON(t, r, &body)
		if body.UserID != "u_1" || body.Arguments["title"] != "hi" {
			t.Errorf("body = %+v", body)
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
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !resp.Successful || resp.Data["issue_number"].(float64) != 42 || resp.LogID != "log_1" {
		t.Errorf("unexpected response: %+v", resp)
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
