package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
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
