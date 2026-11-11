package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestUpdateVCSConnectionToken(t *testing.T) {
	for _, kind := range []string{"gitlab", "gitea", "forgejo"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			withVCSBox(t)
			var validationCalls int
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				validationCalls++
				wantPath, header, wantToken := "/api/v1/user", "Authorization", "token replacement-token"
				if kind == "gitlab" {
					wantPath, header, wantToken = "/api/v4/user", "PRIVATE-TOKEN", "replacement-token"
				}
				if r.URL.Path != wantPath || r.Header.Get(header) != wantToken {
					t.Errorf("unexpected validation request: %s, credential matches=%v", r.URL.Path, r.Header.Get(header) == wantToken)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"login":"service-bot","username":"service-bot"}`))
			}))
			defer provider.Close()
			tokenEnc, err := testHandler.sealVCSSecret("old-token")
			if err != nil {
				t.Fatal(err)
			}
			secretEnc, err := testHandler.sealVCSSecret(vcsTestSecret)
			if err != nil {
				t.Fatal(err)
			}
			id := dbfx.Insert(t, "vcs_connection", testutil.Cols{
				"workspace_id": testWorkspaceID, "provider": kind, "instance_url": provider.URL,
				"account_login": "old-account", "access_token_encrypted": tokenEnc,
				"webhook_secret_encrypted": secretEnc, "connected_by_id": testUserID,
			})
			before, err := testHandler.Queries.GetVCSConnectionByID(ctx, parseUUID(id))
			if err != nil {
				t.Fatal(err)
			}
			issueID := dbfx.Issue(t, "Keep linked pull request")
			prID := dbfx.Insert(t, "vcs_pull_request", testutil.Cols{
				"workspace_id": testWorkspaceID, "connection_id": id, "provider": kind,
				"repo_owner": "acme", "repo_name": "app", "pr_number": 1, "title": "Keep me",
				"state": "open", "html_url": provider.URL + "/acme/app/pulls/1",
				"pr_created_at": time.Now(), "pr_updated_at": time.Now(),
			})
			dbfx.InsertNoID(t, "issue_vcs_pull_request",
				testutil.Cols{"issue_id": issueID, "pull_request_id": prID},
				"issue_id = $1 AND pull_request_id = $2", issueID, prID)

			req := vcsHandlerRequest(http.MethodPost, "/token", map[string]any{
				"access_token": " replacement-token ",
				"instance_url": "https://ignored.example.test",
				"provider":     "ignored",
			}, id)
			var resp map[string]any
			testutil.Call(t, testHandler.UpdateVCSConnectionToken, req).Want(http.StatusOK).JSON(&resp)
			if resp["id"] != id || resp["account_login"] != "service-bot" || resp["instance_url"] != provider.URL || resp["provider"] != kind {
				t.Fatalf("unexpected connection response: %+v", resp)
			}
			for _, key := range []string{"access_token", "access_token_encrypted", "webhook_secret", "webhook_secret_encrypted"} {
				if _, present := resp[key]; present {
					t.Fatalf("response exposes %s", key)
				}
			}
			after, err := testHandler.Queries.GetVCSConnectionByID(ctx, parseUUID(id))
			if err != nil {
				t.Fatal(err)
			}
			token, err := testHandler.openVCSSecret(after.AccessTokenEncrypted)
			if err != nil || token != "replacement-token" || after.AccessTokenEncrypted == token {
				t.Fatal("replacement token must be stored encrypted and decrypt correctly")
			}
			if after.WebhookSecretEncrypted != before.WebhookSecretEncrypted || after.ConnectedByID != before.ConnectedByID || after.CreatedAt != before.CreatedAt {
				t.Fatal("token update changed webhook secret or connection identity")
			}
			linked, err := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issueID))
			if err != nil || len(linked) != 1 || uuidToString(linked[0].ID) != prID {
				t.Fatalf("linked pull request lost: %v, %+v", err, linked)
			}
			if validationCalls != 1 {
				t.Fatalf("expected one provider validation, got %d", validationCalls)
			}
			_, err = testHandler.Queries.UpdateVCSConnectionToken(ctx, db.UpdateVCSConnectionTokenParams{
				ID: parseUUID(id), WorkspaceID: parseUUID(uuid.NewString()),
				AccessTokenEncrypted: "wrong-workspace", AccountLogin: "wrong-workspace",
			})
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("cross-workspace write should return no rows, got %v", err)
			}
		})
	}
}

func TestUpdateVCSConnectionTokenFailuresPreserveConnection(t *testing.T) {
	tests := []struct {
		name              string
		token             string
		providerStatus    int
		foreignWorkspace  bool
		missingConnection bool
		disabled          bool
		unconfigured      bool
		wantStatus        int
		wantCalls         int
		wantError         string
	}{
		{name: "rejected token", token: "rejected", providerStatus: http.StatusUnauthorized, wantStatus: http.StatusBadRequest, wantCalls: 1, wantError: "rejected"},
		{name: "insufficient permissions", token: "rejected", providerStatus: http.StatusForbidden, wantStatus: http.StatusBadRequest, wantCalls: 1, wantError: "rejected"},
		{name: "provider failure", token: "valid", providerStatus: http.StatusInternalServerError, wantStatus: http.StatusBadGateway, wantCalls: 1, wantError: "could not reach"},
		{name: "empty token", token: "  ", wantStatus: http.StatusBadRequest, wantError: "access_token is required"},
		{name: "other workspace", token: "valid", foreignWorkspace: true, wantStatus: http.StatusNotFound},
		{name: "missing connection", token: "valid", missingConnection: true, wantStatus: http.StatusNotFound},
		{name: "disabled", token: "valid", disabled: true, wantStatus: http.StatusNotFound},
		{name: "unconfigured", token: "valid", unconfigured: true, wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withVCSBox(t)
			var calls int
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tt.providerStatus)
			}))
			defer provider.Close()
			tokenEnc, err := testHandler.sealVCSSecret("old-token")
			if err != nil {
				t.Fatal(err)
			}
			secretEnc, err := testHandler.sealVCSSecret(vcsTestSecret)
			if err != nil {
				t.Fatal(err)
			}
			id := dbfx.Insert(t, "vcs_connection", testutil.Cols{
				"workspace_id": testWorkspaceID, "provider": "gitlab", "instance_url": provider.URL,
				"account_login": "old-account", "access_token_encrypted": tokenEnc, "webhook_secret_encrypted": secretEnc,
			})
			reqID := id
			if tt.missingConnection {
				reqID = uuid.NewString()
			}
			req := vcsHandlerRequest(http.MethodPost, "/token", map[string]any{"access_token": tt.token}, reqID)
			if tt.foreignWorkspace {
				chi.RouteContext(req.Context()).URLParams.Add("id", uuid.NewString())
			}
			if tt.disabled {
				testHandler.cfg.VCSIntegrationEnabled = false
			}
			if tt.unconfigured {
				testHandler.VCSSecretBox = nil
			}
			var resp map[string]any
			testutil.Call(t, testHandler.UpdateVCSConnectionToken, req).Want(tt.wantStatus).JSON(&resp)
			if tt.wantError != "" && !strings.Contains(fmt.Sprint(resp["error"]), tt.wantError) {
				t.Fatalf("unexpected error: %+v", resp)
			}
			if calls != tt.wantCalls {
				t.Fatalf("provider calls = %d, want %d", calls, tt.wantCalls)
			}
			after, err := testHandler.Queries.GetVCSConnectionByID(context.Background(), parseUUID(id))
			if err != nil {
				t.Fatal(err)
			}
			if after.AccessTokenEncrypted != tokenEnc || after.WebhookSecretEncrypted != secretEnc || after.AccountLogin != "old-account" {
				t.Fatal("failed update modified the connection")
			}
		})
	}
}

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
