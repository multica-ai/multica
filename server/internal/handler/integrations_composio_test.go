package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	composio "github.com/multica-ai/multica/server/internal/integrations/composio"
	"github.com/multica-ai/multica/server/internal/util"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const composioTestUserID = "22222222-2222-2222-2222-222222222222"

// --- local fakes (handler package can only see the exported interfaces) ---

type composioFakeSDK struct {
	createLinkResp *sdk.CreateLinkResponse
	revokeErr      error
	// listAuthErr, when set, makes ListAuthConfigs fail. This propagates through
	// Service.authConfigMap → ListToolkits, exercising the handler's 502 path.
	listAuthErr error
}

func (f *composioFakeSDK) CreateLink(_ context.Context, _ sdk.CreateLinkRequest) (*sdk.CreateLinkResponse, error) {
	if f.createLinkResp != nil {
		return f.createLinkResp, nil
	}
	return &sdk.CreateLinkResponse{RedirectURL: "https://composio.example/redirect"}, nil
}

// ListConnectedAccounts echoes the requested id as an account owned by the
// handler-test user under the notion auth config, so callback ownership
// verification passes on the happy path.
func (f *composioFakeSDK) ListConnectedAccounts(_ context.Context, req sdk.ListConnectedAccountsRequest) (*sdk.ListConnectedAccountsResponse, error) {
	id := ""
	if len(req.ConnectedAccountIDs) > 0 {
		id = req.ConnectedAccountIDs[0]
	}
	return &sdk.ListConnectedAccountsResponse{Items: []sdk.ConnectedAccount{{
		ID:           id,
		UserID:       composioTestUserID,
		AuthConfigID: "ac_notion",
	}}}, nil
}
func (f *composioFakeSDK) RevokeConnection(_ context.Context, _ string) error       { return f.revokeErr }
func (f *composioFakeSDK) DeleteConnectedAccount(_ context.Context, _ string) error { return nil }

// ListAuthConfigs reports a single enabled notion auth config so BeginConnect
// resolves notion → ac_notion and the callback's auth-config check matches.
func (f *composioFakeSDK) ListAuthConfigs(_ context.Context, _ sdk.ListAuthConfigsRequest) (*sdk.ListAuthConfigsResponse, error) {
	if f.listAuthErr != nil {
		return nil, f.listAuthErr
	}
	return &sdk.ListAuthConfigsResponse{Items: []sdk.AuthConfig{{
		ID:      "ac_notion",
		Toolkit: sdk.Toolkit{Slug: "notion"},
		Status:  "ENABLED",
	}}}, nil
}

func (f *composioFakeSDK) ListToolkits(_ context.Context, _ sdk.ListToolkitsRequest) (*sdk.ListToolkitsResponse, error) {
	return &sdk.ListToolkitsResponse{Items: []sdk.Toolkit{
		{Slug: "notion", Name: "Notion"},
		{Slug: "github", Name: "GitHub"},
	}}, nil
}
func (f *composioFakeSDK) CreateSession(_ context.Context, _ sdk.CreateSessionRequest) (*sdk.CreateSessionResponse, error) {
	return &sdk.CreateSessionResponse{}, nil
}
func (f *composioFakeSDK) MCPAuthHeaders() map[string]string {
	return map[string]string{"x-api-key": "k"}
}

type composioFakeStore struct {
	rows   []db.UserComposioConnection
	nextID byte
}

func (s *composioFakeStore) UpsertUserComposioConnection(_ context.Context, arg db.UpsertUserComposioConnectionParams) (db.UserComposioConnection, error) {
	for i := range s.rows {
		if s.rows[i].UserID.Bytes == arg.UserID.Bytes && s.rows[i].ConnectedAccountID == arg.ConnectedAccountID {
			s.rows[i].Status = "active"
			return s.rows[i], nil
		}
	}
	s.nextID++
	var b [16]byte
	b[15] = s.nextID
	row := db.UserComposioConnection{
		ID:                 pgtype.UUID{Bytes: b, Valid: true},
		UserID:             arg.UserID,
		ToolkitSlug:        arg.ToolkitSlug,
		AuthConfigID:       arg.AuthConfigID,
		ConnectedAccountID: arg.ConnectedAccountID,
		ComposioUserID:     arg.ComposioUserID,
		Status:             "active",
		ConnectedAt:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	s.rows = append(s.rows, row)
	return row, nil
}
func (s *composioFakeStore) ListActiveUserComposioConnections(_ context.Context, userID pgtype.UUID) ([]db.UserComposioConnection, error) {
	out := []db.UserComposioConnection{}
	for _, r := range s.rows {
		if r.UserID.Bytes == userID.Bytes && r.Status == "active" {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *composioFakeStore) GetUserComposioConnection(_ context.Context, arg db.GetUserComposioConnectionParams) (db.UserComposioConnection, error) {
	for _, r := range s.rows {
		if r.ID.Bytes == arg.ID.Bytes && r.UserID.Bytes == arg.UserID.Bytes {
			return r, nil
		}
	}
	return db.UserComposioConnection{}, pgx.ErrNoRows
}
func (s *composioFakeStore) MarkUserComposioConnectionRevoked(_ context.Context, arg db.MarkUserComposioConnectionRevokedParams) error {
	for i := range s.rows {
		if s.rows[i].ID.Bytes == arg.ID.Bytes && s.rows[i].UserID.Bytes == arg.UserID.Bytes {
			s.rows[i].Status = "revoked"
		}
	}
	return nil
}

func newComposioTestHandler(t *testing.T, sdkFake composio.SDK, store composio.Store) *Handler {
	t.Helper()
	svc, err := composio.NewService(sdkFake, store, composio.Config{
		StateSecret:     []byte("handler-test-secret"),
		CallbackBaseURL: "https://multica.ai",
		FrontendBaseURL: "https://multica.ai",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	h := &Handler{Composio: svc}
	withComposioMCPAppsFlag(t, h, true)
	return h
}

func composioReq(method, target, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r.Header.Set("X-User-ID", composioTestUserID)
	return r
}

// --- tests ---

func TestComposio_ServiceUnavailableWhenNil(t *testing.T) {
	h := &Handler{}
	for _, hf := range []http.HandlerFunc{
		h.ComposioConnectInit, h.ComposioCallback, h.ListComposioConnections, h.DeleteComposioConnection,
	} {
		w := httptest.NewRecorder()
		hf(w, composioReq(http.MethodGet, "/", ""))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 when Composio nil, got %d", w.Code)
		}
	}
}

func TestComposio_ServiceUnavailableWhenFlagDisabled(t *testing.T) {
	h := newComposioTestHandler(t, &composioFakeSDK{}, &composioFakeStore{})
	withComposioMCPAppsFlag(t, h, false)

	w := httptest.NewRecorder()
	h.ListComposioToolkits(w, composioReq(http.MethodGet, "/toolkits", ""))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when feature flag disabled, got %d", w.Code)
	}
}

func TestComposio_ConnectInit(t *testing.T) {
	h := newComposioTestHandler(t, &composioFakeSDK{}, &composioFakeStore{})

	// success
	w := httptest.NewRecorder()
	h.ComposioConnectInit(w, composioReq(http.MethodPost, "/", `{"toolkit_slug":"notion"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp ComposioConnectInitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RedirectURL == "" {
		t.Error("expected redirect_url")
	}

	// unsupported toolkit → 400
	w = httptest.NewRecorder()
	h.ComposioConnectInit(w, composioReq(http.MethodPost, "/", `{"toolkit_slug":"github"}`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("unsupported toolkit: expected 400, got %d", w.Code)
	}

	// missing slug → 400
	w = httptest.NewRecorder()
	h.ComposioConnectInit(w, composioReq(http.MethodPost, "/", `{}`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing slug: expected 400, got %d", w.Code)
	}
}

func TestComposio_ListToolkits(t *testing.T) {
	h := newComposioTestHandler(t, &composioFakeSDK{}, &composioFakeStore{})

	w := httptest.NewRecorder()
	h.ListComposioToolkits(w, composioReq(http.MethodGet, "/toolkits", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var toolkits []ComposioToolkitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &toolkits); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Only notion has an enabled auth config; github is filtered out server-side
	// (MUL-4009), so a single connectable toolkit comes back.
	if len(toolkits) != 1 {
		t.Fatalf("expected 1 toolkit, got %d (%s)", len(toolkits), w.Body.String())
	}
	if toolkits[0].Slug != "notion" || !toolkits[0].Connectable {
		t.Errorf("toolkit = %+v, want connectable notion", toolkits[0])
	}
	for _, tk := range toolkits {
		if tk.Slug == "github" {
			t.Error("github has no auth config and must be filtered out")
		}
		if tk.Logo == "" {
			t.Errorf("toolkit %q missing logo", tk.Slug)
		}
	}
}

// TestComposio_ListToolkits_ResolverErrorIs502 pins the key behavior of this
// PR (MUL-4009): when the service can't resolve which toolkits are connectable
// (auth-config lookup fails), ListComposioToolkits must return 502 rather than
// silently degrading to an empty catalog. A regression back to a soft empty
// list would render as a misleading "no apps configured" state.
func TestComposio_ListToolkits_ResolverErrorIs502(t *testing.T) {
	h := newComposioTestHandler(t, &composioFakeSDK{listAuthErr: errors.New("auth_configs upstream blip")}, &composioFakeStore{})

	w := httptest.NewRecorder()
	h.ListComposioToolkits(w, composioReq(http.MethodGet, "/toolkits", ""))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on resolver error, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestComposio_CallbackRedirects(t *testing.T) {
	store := &composioFakeStore{}

	// Mint a valid signed state by driving BeginConnect through a capturing
	// SDK, then replay it through the real callback handler.
	capturing := &composioCapturingSDK{}
	h2 := newComposioTestHandler(t, capturing, store)
	bw := httptest.NewRecorder()
	h2.ComposioConnectInit(bw, composioReq(http.MethodPost, "/", `{"toolkit_slug":"notion"}`))
	state := capturing.stateFromCallback()
	if state == "" {
		t.Fatal("could not capture signed state")
	}

	w := httptest.NewRecorder()
	h2.ComposioCallback(w, composioReq(http.MethodGet, "/callback?state="+state+"&status=success&connected_account_id=ca_1", ""))
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "connected=notion") {
		t.Errorf("success location = %q", loc)
	}

	// failure path: bad state → error redirect
	w = httptest.NewRecorder()
	h2.ComposioCallback(w, composioReq(http.MethodGet, "/callback?state=bad&status=success&connected_account_id=ca_1", ""))
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 on bad state, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "error=composio_connect_failed") {
		t.Errorf("failure location = %q", loc)
	}
}

func TestComposio_ListAndDelete(t *testing.T) {
	store := &composioFakeStore{}
	userUUID, _ := util.ParseUUID(composioTestUserID)
	row, _ := store.UpsertUserComposioConnection(context.Background(), db.UpsertUserComposioConnectionParams{
		UserID:             userUUID,
		ToolkitSlug:        "notion",
		AuthConfigID:       "ac_notion",
		ConnectedAccountID: "ca_list",
		ComposioUserID:     composioTestUserID,
	})
	h := newComposioTestHandler(t, &composioFakeSDK{}, store)

	// list
	w := httptest.NewRecorder()
	h.ListComposioConnections(w, composioReq(http.MethodGet, "/connections", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	var conns []ComposioConnectionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &conns); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(conns) != 1 || conns[0].ToolkitSlug != "notion" {
		t.Fatalf("conns = %+v", conns)
	}

	// delete (owner) → 204, routed through chi so {id} resolves
	r := chi.NewRouter()
	r.Delete("/api/integrations/composio/connections/{id}", h.DeleteComposioConnection)
	delReq := composioReq(http.MethodDelete, "/api/integrations/composio/connections/"+util.UUIDToString(row.ID), "")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, delReq)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d (%s)", w.Code, w.Body.String())
	}

	// delete unknown id → 404
	missing := "33333333-3333-3333-3333-333333333333"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, composioReq(http.MethodDelete, "/api/integrations/composio/connections/"+missing, ""))
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete missing: expected 404, got %d", w.Code)
	}
}

// composioCapturingSDK records the callback URL so a test can replay the signed
// state through the real callback handler.
type composioCapturingSDK struct {
	composioFakeSDK
	lastCallbackURL string
}

func (f *composioCapturingSDK) CreateLink(_ context.Context, req sdk.CreateLinkRequest) (*sdk.CreateLinkResponse, error) {
	f.lastCallbackURL = req.CallbackURL
	return &sdk.CreateLinkResponse{RedirectURL: "https://composio.example/redirect"}, nil
}

func (f *composioCapturingSDK) stateFromCallback() string {
	idx := strings.Index(f.lastCallbackURL, "state=")
	if idx < 0 {
		return ""
	}
	return f.lastCallbackURL[idx+len("state="):]
}
