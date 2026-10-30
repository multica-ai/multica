package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func vcsHandlerRequest(method, path string, body any, connectionID string) *http.Request {
	req := newRequest(method, path, body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testWorkspaceID)
	if connectionID != "" {
		rctx.URLParams.Add("connectionId", connectionID)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestListVCSConnectionsHonorsDeploymentSwitch(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo-list.test")
	t.Cleanup(func() { cleanupVCS(context.Background(), "") })

	fetch := func() struct {
		Connections []VCSConnectionResponse `json:"connections"`
		Available   bool                    `json:"available"`
		Configured  bool                    `json:"configured"`
	} {
		t.Helper()
		req := vcsHandlerRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/vcs/connections", nil, "")
		w := httptest.NewRecorder()
		testHandler.ListVCSConnections(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListVCSConnections: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Connections []VCSConnectionResponse `json:"connections"`
			Available   bool                    `json:"available"`
			Configured  bool                    `json:"configured"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode ListVCSConnections: %v", err)
		}
		return resp
	}

	testHandler.cfg.VCSIntegrationEnabled = false
	disabled := fetch()
	if disabled.Available || disabled.Configured || len(disabled.Connections) != 0 {
		t.Fatalf("disabled response must hide stored connections and availability, got %+v", disabled)
	}

	testHandler.cfg.VCSIntegrationEnabled = true
	enabled := fetch()
	if !enabled.Available || !enabled.Configured {
		t.Fatalf("enabled response must expose availability and configuration, got %+v", enabled)
	}
	if len(enabled.Connections) != 1 || enabled.Connections[0].ID != connID {
		t.Fatalf("enabled response must include seeded connection %s, got %+v", connID, enabled.Connections)
	}
}

func TestConnectVCSHonorsDeploymentSwitch(t *testing.T) {
	var validationCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		validationCalls.Add(1)
		if r.URL.Path != "/api/v1/user" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"vcs-test-user"}`))
	}))
	defer provider.Close()

	withVCSBox(t)
	t.Cleanup(func() { cleanupVCS(context.Background(), "") })
	body := map[string]any{
		"provider":     "forgejo",
		"instance_url": provider.URL,
		"access_token": "test-token",
	}
	connect := func() *httptest.ResponseRecorder {
		t.Helper()
		req := vcsHandlerRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/vcs/connections", body, "")
		w := httptest.NewRecorder()
		testHandler.ConnectVCS(w, req)
		return w
	}
	countConnections := func() int {
		t.Helper()
		var count int
		if err := testPool.QueryRow(context.Background(),
			`SELECT count(*) FROM vcs_connection WHERE workspace_id = $1 AND instance_url = $2`,
			testWorkspaceID, provider.URL,
		).Scan(&count); err != nil {
			t.Fatalf("count VCS connections: %v", err)
		}
		return count
	}

	testHandler.cfg.VCSIntegrationEnabled = false
	if w := connect(); w.Code != http.StatusNotFound {
		t.Fatalf("disabled ConnectVCS: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if got := validationCalls.Load(); got != 0 {
		t.Fatalf("disabled ConnectVCS must not call provider, got %d requests", got)
	}
	if got := countConnections(); got != 0 {
		t.Fatalf("disabled ConnectVCS must not write a connection, got %d rows", got)
	}

	testHandler.cfg.VCSIntegrationEnabled = true
	if w := connect(); w.Code != http.StatusOK {
		t.Fatalf("enabled ConnectVCS: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := validationCalls.Load(); got != 1 {
		t.Fatalf("enabled ConnectVCS: expected one provider validation, got %d", got)
	}
	if got := countConnections(); got != 1 {
		t.Fatalf("enabled ConnectVCS: expected one stored connection, got %d", got)
	}
}

func TestRotateVCSConnectionWebhookHonorsDeploymentSwitch(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo-rotate.test")
	t.Cleanup(func() { cleanupVCS(context.Background(), "") })
	connUUID := parseUUID(connID)

	loadSecret := func() string {
		t.Helper()
		conn, err := testHandler.Queries.GetVCSConnectionByID(context.Background(), connUUID)
		if err != nil {
			t.Fatalf("GetVCSConnectionByID: %v", err)
		}
		return conn.WebhookSecretEncrypted
	}
	rotate := func() *httptest.ResponseRecorder {
		t.Helper()
		req := vcsHandlerRequest(
			http.MethodPost,
			"/api/workspaces/"+testWorkspaceID+"/vcs/connections/"+connID+"/rotate-webhook",
			nil,
			connID,
		)
		w := httptest.NewRecorder()
		testHandler.RotateVCSConnectionWebhook(w, req)
		return w
	}

	originalSecret := loadSecret()
	testHandler.cfg.VCSIntegrationEnabled = false
	if w := rotate(); w.Code != http.StatusNotFound {
		t.Fatalf("disabled RotateVCSConnectionWebhook: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if got := loadSecret(); got != originalSecret {
		t.Fatal("disabled RotateVCSConnectionWebhook must not modify the stored secret")
	}

	testHandler.cfg.VCSIntegrationEnabled = true
	if w := rotate(); w.Code != http.StatusOK {
		t.Fatalf("enabled RotateVCSConnectionWebhook: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := loadSecret(); got == originalSecret {
		t.Fatal("enabled RotateVCSConnectionWebhook must replace the stored secret")
	}
}

// TestConnectVCSReportsUntrustedCertificate covers a Gitea behind a private or
// self-signed CA: the connect must fail as a certificate problem, not as an
// unreachable instance, and must not store a connection.
func TestConnectVCSReportsUntrustedCertificate(t *testing.T) {
	var reached atomic.Int32
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"vcs-test-user"}`))
	}))
	defer provider.Close()

	withVCSBox(t)
	t.Cleanup(func() { cleanupVCS(context.Background(), "") })
	req := vcsHandlerRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/vcs/connections", map[string]any{
		"provider":     "gitea",
		"instance_url": provider.URL,
		"access_token": "test-token",
	}, "")
	var resp struct {
		Error string `json:"error"`
	}
	testutil.Call(t, testHandler.ConnectVCS, req).Want(http.StatusBadGateway).JSON(&resp)
	if !strings.Contains(resp.Error, "certificate authority") {
		t.Fatalf("expected an untrusted-certificate message, got %q", resp.Error)
	}
	if got := reached.Load(); got != 0 {
		t.Fatalf("the TLS handshake must fail before any request is served, got %d", got)
	}
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM vcs_connection WHERE workspace_id = $1 AND instance_url = $2`,
		testWorkspaceID, provider.URL,
	).Scan(&count); err != nil {
		t.Fatalf("count VCS connections: %v", err)
	}
	if count != 0 {
		t.Fatalf("a failed connect must not store a connection, got %d rows", count)
	}
}

// TestVCSValidationFailureMessage drives real handshakes so each case matches
// the error shape crypto/tls actually returns, wrapped the way providers wrap
// it.
func TestVCSValidationFailureMessage(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	// srv.Client() trusts the test certificate, which covers 127.0.0.1 and
	// example.com and is valid until 2084.
	trusting := func(adjust func(*tls.Config)) *http.Client {
		transport := srv.Client().Transport.(*http.Transport).Clone()
		adjust(transport.TLSClientConfig)
		return &http.Client{Transport: transport}
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	cases := []struct {
		name   string
		client *http.Client
		url    string
		want   string
	}{
		{"untrusted CA", &http.Client{}, srv.URL, "certificate authority this server does not trust"},
		{"hostname mismatch", trusting(func(c *tls.Config) { c.ServerName = "gitea.internal.test" }), srv.URL, "does not match"},
		{"expired", trusting(func(c *tls.Config) {
			c.Time = func() time.Time { return time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC) }
		}), srv.URL, "failed verification"},
		{"unreachable", &http.Client{}, closedURL, "could not reach the provider instance"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := tc.client.Get(tc.url)
			if err == nil {
				resp.Body.Close()
				t.Fatal("expected the request to fail")
			}
			got := vcsValidationFailureMessage(fmt.Errorf("gitea: request: %w", err))
			if !strings.Contains(got, tc.want) {
				t.Fatalf("error %v: got message %q, want it to contain %q", err, got, tc.want)
			}
		})
	}
}
