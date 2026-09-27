package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/weixin"
)

func TestListWeixinInstallationsNotConfiguredReturnsEmpty(t *testing.T) {
	h := &Handler{}
	w := httptest.NewRecorder()

	h.ListWeixinInstallations(w, httptest.NewRequest(http.MethodGet, "/api/workspaces/x/weixin/installations", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Installations    []any `json:"installations"`
		Configured       bool  `json:"configured"`
		InstallSupported bool  `json:"install_supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Configured || resp.InstallSupported || len(resp.Installations) != 0 {
		t.Fatalf("unexpected unconfigured response: %+v", resp)
	}
}

func TestWeixinMutationHandlersRejectUnconfiguredDeployment(t *testing.T) {
	for name, run := range map[string]func(*Handler, http.ResponseWriter, *http.Request){
		"start login": (*Handler).StartWeixinLogin,
		"get login":   (*Handler).GetWeixinLogin,
		"verify code": (*Handler).SubmitWeixinVerifyCode,
		"complete":    (*Handler).CompleteWeixinLogin,
		"revoke":      (*Handler).RevokeWeixinInstallation,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			run(&Handler{}, w, httptest.NewRequest(http.MethodPost, "/api/workspaces/x/weixin", strings.NewReader("{}")))
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestWeixinLoginResponseHidesOwner(t *testing.T) {
	payload, err := json.Marshal(weixinLoginToResponse(weixin.Login{
		ID: "l1", State: "waiting", QRContent: "https://qr", WorkspaceID: "ws-sentinel",
		AgentID: "agent-sentinel", UserID: "user-sentinel",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"ws-sentinel", "agent-sentinel", "user-sentinel"} {
		if strings.Contains(string(payload), leaked) {
			t.Fatalf("login response leaked %q: %s", leaked, payload)
		}
	}
}

// fakeWeixinHost is an in-memory stand-in for apps/weixin-host's control API.
type fakeWeixinHost struct {
	mu            sync.Mutex
	logins        map[string]weixin.Login
	installations []weixin.Installation
	created       []weixin.CreateInstallationRequest
	superseded    []weixin.SupersededInstallation
}

func newFakeWeixinHost(t *testing.T) (*fakeWeixinHost, *weixin.HostClient) {
	t.Helper()
	fake := &fakeWeixinHost{logins: map[string]weixin.Login{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/installations", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		out := []weixin.Installation{}
		for _, inst := range fake.installations {
			if inst.WorkspaceID == r.URL.Query().Get("workspace_id") {
				out = append(out, inst)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"installations": out})
	})
	mux.HandleFunc("GET /v1/logins/{id}", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		login, ok := fake.logins[r.PathValue("id")]
		if !ok {
			http.Error(w, `{"error":"login not found"}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(login)
	})
	mux.HandleFunc("POST /v1/installations", func(w http.ResponseWriter, r *http.Request) {
		var req weixin.CreateInstallationRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.created = append(fake.created, req)
		inst := weixin.Installation{
			ID: "inst-new", WorkspaceID: req.WorkspaceID, AgentID: req.AgentID,
			AccountID: "bot-im-bot", InstallerUserID: req.InstallerUserID, TokenID: req.TokenID,
		}
		fake.installations = append(fake.installations, inst)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"installation": inst, "superseded": fake.superseded})
	})
	mux.HandleFunc("DELETE /v1/installations/{id}", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		kept := fake.installations[:0]
		for _, inst := range fake.installations {
			if inst.ID != r.PathValue("id") {
				kept = append(kept, inst)
			}
		}
		fake.installations = kept
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return fake, weixin.NewHostClient(server.URL, "secret")
}

func weixinRequest(userID, method, path string, body any, params map[string]string) *http.Request {
	req := newRequestAs(userID, method, path, body)
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func tokenRevoked(t *testing.T, tokenID string) bool {
	t.Helper()
	var revoked bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT revoked FROM personal_access_token WHERE id = $1`, tokenID).Scan(&revoked); err != nil {
		t.Fatalf("read token %s: %v", tokenID, err)
	}
	return revoked
}

func TestCompleteWeixinLoginMintsRelayTokenAndRevokesSuperseded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	fake, client := newFakeWeixinHost(t)
	h := *testHandler
	h.WeixinHost = client
	agentID := dbfx.Agent(t, "weixin-relay-agent", handlerTestRuntimeID(t))

	oldPAT, _, err := h.mintPersonalAccessToken(context.Background(), testUserID, "old relay", pgtype.Timestamptz{})
	if err != nil {
		t.Fatal(err)
	}
	fake.superseded = []weixin.SupersededInstallation{{
		ID: "inst-old", TokenID: uuidToString(oldPAT.ID), InstallerUserID: testUserID,
	}}
	fake.logins["login-1"] = weixin.Login{
		ID: "login-1", State: "connected", AccountID: "bot-im-bot",
		WorkspaceID: testWorkspaceID, AgentID: agentID, UserID: testUserID,
	}

	w := httptest.NewRecorder()
	h.CompleteWeixinLogin(w, weixinRequest(testUserID, http.MethodPost, "/", nil,
		map[string]string{"id": testWorkspaceID, "loginId": "login-1"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	if len(fake.created) != 1 {
		t.Fatalf("expected one installation request, got %d", len(fake.created))
	}
	created := fake.created[0]
	if created.InstallerUserID != testUserID || created.AgentID != agentID || created.Token == "" {
		t.Fatalf("unexpected installation request: %+v", created)
	}
	if tokenRevoked(t, created.TokenID) {
		t.Fatal("new relay token must stay active")
	}
	if !tokenRevoked(t, uuidToString(oldPAT.ID)) {
		t.Fatal("superseded relay token must be revoked")
	}
	if strings.Contains(w.Body.String(), created.Token) {
		t.Fatal("response leaked the relay token")
	}
}

func TestWeixinLoginIsPrivateToItsStarter(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	fake, client := newFakeWeixinHost(t)
	h := *testHandler
	h.WeixinHost = client
	_, _, memberID := privateAgentTestFixture(t)
	fake.logins["login-1"] = weixin.Login{
		ID: "login-1", State: "connected", WorkspaceID: testWorkspaceID, UserID: testUserID,
	}

	for name, run := range map[string]func(http.ResponseWriter, *http.Request){
		"get":      h.GetWeixinLogin,
		"complete": h.CompleteWeixinLogin,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			run(w, weixinRequest(memberID, http.MethodPost, "/", nil,
				map[string]string{"id": testWorkspaceID, "loginId": "login-1"}))
			if w.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for another member's login, got %d", w.Code)
			}
		})
	}
	if len(fake.created) != 0 {
		t.Fatal("another member must not complete the login")
	}
}

func TestRevokeWeixinInstallationChecksInstallerOrAdmin(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	fake, client := newFakeWeixinHost(t)
	h := *testHandler
	h.WeixinHost = client
	_, _, memberID := privateAgentTestFixture(t)

	pat, _, err := h.mintPersonalAccessToken(context.Background(), testUserID, "relay", pgtype.Timestamptz{})
	if err != nil {
		t.Fatal(err)
	}
	fake.installations = []weixin.Installation{{
		ID: "inst-1", WorkspaceID: testWorkspaceID, InstallerUserID: testUserID,
		TokenID: uuidToString(pat.ID),
	}}
	params := map[string]string{"id": testWorkspaceID, "installationId": "inst-1"}

	w := httptest.NewRecorder()
	h.RevokeWeixinInstallation(w, weixinRequest(memberID, http.MethodDelete, "/", nil, params))
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member revoking another's installation: expected 403, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	h.RevokeWeixinInstallation(w, weixinRequest(testUserID, http.MethodDelete, "/", nil, params))
	if w.Code != http.StatusNoContent {
		t.Fatalf("installer revoking: expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	if len(fake.installations) != 0 {
		t.Fatal("installation was not deleted on the host")
	}
	if !tokenRevoked(t, uuidToString(pat.ID)) {
		t.Fatal("relay token must be revoked on disconnect")
	}
}

func TestWeixinHostClientMapsErrors(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/v1/logins/missing":
			http.Error(w, `{"error":"login not found"}`, http.StatusNotFound)
		default:
			http.Error(w, `{"error":"login is not connected"}`, http.StatusConflict)
		}
	}))
	defer server.Close()
	client := weixin.NewHostClient(server.URL+"/", "s3cret")

	if _, err := client.GetLogin(context.Background(), "missing"); !errors.Is(err, weixin.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if gotAuth != "Bearer s3cret" {
		t.Fatalf("expected shared-secret bearer, got %q", gotAuth)
	}
	_, err := client.CreateInstallation(context.Background(), weixin.CreateInstallationRequest{})
	if !errors.Is(err, weixin.ErrConflict) || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected ErrConflict with host message, got %v", err)
	}
}
