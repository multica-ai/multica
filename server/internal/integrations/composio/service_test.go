package composio

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---- fakes ---------------------------------------------------------------

type fakeSDK struct {
	createLinkResp  *sdk.CreateLinkResponse
	createLinkErr   error
	lastCreateLink  sdk.CreateLinkRequest
	revoked         []string
	revokeErr       error
	deleted         []string
	deleteErr       error
	createSessResp  *sdk.CreateSessionResponse
	createSessErr   error
	lastSessReq     sdk.CreateSessionRequest
	createSessCalls int
}

func (f *fakeSDK) CreateLink(_ context.Context, req sdk.CreateLinkRequest) (*sdk.CreateLinkResponse, error) {
	f.lastCreateLink = req
	if f.createLinkErr != nil {
		return nil, f.createLinkErr
	}
	if f.createLinkResp != nil {
		return f.createLinkResp, nil
	}
	return &sdk.CreateLinkResponse{RedirectURL: "https://composio.example/redirect", ConnectedAccountID: "ca_pending"}, nil
}

func (f *fakeSDK) RevokeConnection(_ context.Context, id string) error {
	f.revoked = append(f.revoked, id)
	return f.revokeErr
}

func (f *fakeSDK) DeleteConnectedAccount(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return f.deleteErr
}

func (f *fakeSDK) CreateSession(_ context.Context, req sdk.CreateSessionRequest) (*sdk.CreateSessionResponse, error) {
	f.createSessCalls++
	f.lastSessReq = req
	if f.createSessErr != nil {
		return nil, f.createSessErr
	}
	if f.createSessResp != nil {
		return f.createSessResp, nil
	}
	return &sdk.CreateSessionResponse{MCP: sdk.MCPDescriptor{URL: "https://mcp.example/session"}}, nil
}

func (f *fakeSDK) MCPAuthHeaders() map[string]string {
	return map[string]string{"x-api-key": "secret"}
}

// fakeStore is an in-memory implementation of Store with the same
// (user_id, connected_account_id) uniqueness as the real table.
type fakeStore struct {
	rows   []db.UserComposioConnection
	nextID byte
}

func newFakeStore() *fakeStore { return &fakeStore{nextID: 1} }

func (s *fakeStore) UpsertUserComposioConnection(_ context.Context, arg db.UpsertUserComposioConnectionParams) (db.UserComposioConnection, error) {
	for i := range s.rows {
		if uuidEqual(s.rows[i].UserID, arg.UserID) && s.rows[i].ConnectedAccountID == arg.ConnectedAccountID {
			s.rows[i].ToolkitSlug = arg.ToolkitSlug
			s.rows[i].AuthConfigID = arg.AuthConfigID
			s.rows[i].ComposioUserID = arg.ComposioUserID
			s.rows[i].Status = "active"
			s.rows[i].UpdatedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			return s.rows[i], nil
		}
	}
	row := db.UserComposioConnection{
		ID:                 mintUUID(s.nextID),
		UserID:             arg.UserID,
		ToolkitSlug:        arg.ToolkitSlug,
		AuthConfigID:       arg.AuthConfigID,
		ConnectedAccountID: arg.ConnectedAccountID,
		ComposioUserID:     arg.ComposioUserID,
		Status:             "active",
		ConnectedAt:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	s.nextID++
	s.rows = append(s.rows, row)
	return row, nil
}

func (s *fakeStore) ListActiveUserComposioConnections(_ context.Context, userID pgtype.UUID) ([]db.UserComposioConnection, error) {
	out := []db.UserComposioConnection{}
	for _, r := range s.rows {
		if uuidEqual(r.UserID, userID) && r.Status == "active" {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *fakeStore) GetUserComposioConnection(_ context.Context, arg db.GetUserComposioConnectionParams) (db.UserComposioConnection, error) {
	for _, r := range s.rows {
		if uuidEqual(r.ID, arg.ID) && uuidEqual(r.UserID, arg.UserID) {
			return r, nil
		}
	}
	return db.UserComposioConnection{}, pgx.ErrNoRows
}

func (s *fakeStore) MarkUserComposioConnectionRevoked(_ context.Context, arg db.MarkUserComposioConnectionRevokedParams) error {
	for i := range s.rows {
		if uuidEqual(s.rows[i].ID, arg.ID) && uuidEqual(s.rows[i].UserID, arg.UserID) {
			s.rows[i].Status = "revoked"
		}
	}
	return nil
}

func uuidEqual(a, b pgtype.UUID) bool { return a.Valid && b.Valid && a.Bytes == b.Bytes }

func mintUUID(n byte) pgtype.UUID {
	var b [16]byte
	b[15] = n
	return pgtype.UUID{Bytes: b, Valid: true}
}

func newTestService(t *testing.T, client SDK, store Store) *Service {
	t.Helper()
	svc, err := NewService(client, store, Config{
		StateSecret:     testSecret,
		CallbackBaseURL: "https://app.multica.ai",
		FrontendBaseURL: "https://app.multica.ai",
		AuthConfigs:     map[string]string{"notion": "ac_notion"},
		Now:             func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

// ---- tests ---------------------------------------------------------------

func TestNewService_Validation(t *testing.T) {
	t.Parallel()
	if _, err := NewService(nil, newFakeStore(), Config{StateSecret: testSecret, CallbackBaseURL: "x"}); err == nil {
		t.Error("expected error for nil client")
	}
	if _, err := NewService(&fakeSDK{}, nil, Config{StateSecret: testSecret, CallbackBaseURL: "x"}); err == nil {
		t.Error("expected error for nil store")
	}
	if _, err := NewService(&fakeSDK{}, newFakeStore(), Config{CallbackBaseURL: "x"}); err == nil {
		t.Error("expected error for empty secret")
	}
	if _, err := NewService(&fakeSDK{}, newFakeStore(), Config{StateSecret: testSecret}); err == nil {
		t.Error("expected error for empty callback base")
	}
}

func TestBeginConnect_MappingAndState(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{}
	svc := newTestService(t, sdkFake, newFakeStore())
	userID := mintUUID(7)

	redirect, err := svc.BeginConnect(context.Background(), userID, "Notion")
	if err != nil {
		t.Fatalf("BeginConnect: %v", err)
	}
	if redirect != "https://composio.example/redirect" {
		t.Errorf("redirect = %q", redirect)
	}
	// toolkit → auth_config mapping
	if sdkFake.lastCreateLink.AuthConfigID != "ac_notion" {
		t.Errorf("auth config = %q", sdkFake.lastCreateLink.AuthConfigID)
	}
	// composio_user_id == multica user id
	if sdkFake.lastCreateLink.UserID != util.UUIDToString(userID) {
		t.Errorf("composio user id = %q, want %q", sdkFake.lastCreateLink.UserID, util.UUIDToString(userID))
	}
	// callback URL carries the signed state and points at our callback path
	cb := sdkFake.lastCreateLink.CallbackURL
	if !strings.HasPrefix(cb, "https://app.multica.ai"+callbackPath+"?state=") {
		t.Fatalf("callback url = %q", cb)
	}
	u, _ := url.Parse(cb)
	state := u.Query().Get("state")
	claims, err := verifyState(testSecret, state, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("state did not verify: %v", err)
	}
	if claims.ToolkitSlug != "notion" || claims.UserID != util.UUIDToString(userID) {
		t.Errorf("claims = %+v", claims)
	}
}

func TestBeginConnect_UnsupportedToolkit(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeSDK{}, newFakeStore())
	if _, err := svc.BeginConnect(context.Background(), mintUUID(1), "github"); !errors.Is(err, ErrToolkitNotSupported) {
		t.Fatalf("expected ErrToolkitNotSupported, got %v", err)
	}
}

func TestCompleteCallback_SuccessAndIdempotent(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := newTestService(t, &fakeSDK{}, store)
	userID := mintUUID(3)
	state, _ := signState(testSecret, stateClaims{
		UserID:      util.UUIDToString(userID),
		ToolkitSlug: "notion",
		Exp:         time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})

	slug, err := svc.CompleteCallback(context.Background(), state, "success", "ca_123")
	if err != nil {
		t.Fatalf("CompleteCallback: %v", err)
	}
	if slug != "notion" {
		t.Errorf("slug = %q", slug)
	}
	// Duplicate callback (same connected account) must not create a 2nd row.
	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_123"); err != nil {
		t.Fatalf("second CompleteCallback: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("expected 1 row after duplicate callback, got %d", len(store.rows))
	}
	row := store.rows[0]
	if row.ComposioUserID != util.UUIDToString(userID) {
		t.Errorf("composio_user_id invariant broken: %q", row.ComposioUserID)
	}
	if row.AuthConfigID != "ac_notion" || row.ToolkitSlug != "notion" || row.Status != "active" {
		t.Errorf("row = %+v", row)
	}
}

func TestCompleteCallback_NonSuccessNoRow(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := newTestService(t, &fakeSDK{}, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:      util.UUIDToString(mintUUID(4)),
		ToolkitSlug: "notion",
		Exp:         time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})
	slug, err := svc.CompleteCallback(context.Background(), state, "failed", "ca_x")
	if !errors.Is(err, ErrConnectNotSuccessful) {
		t.Fatalf("expected ErrConnectNotSuccessful, got %v", err)
	}
	if slug != "notion" {
		t.Errorf("slug = %q (should still be returned for redirect)", slug)
	}
	if len(store.rows) != 0 {
		t.Fatalf("expected no row written on non-success, got %d", len(store.rows))
	}
}

func TestCompleteCallback_BadState(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeSDK{}, newFakeStore())
	if _, err := svc.CompleteCallback(context.Background(), "garbage", "success", "ca_1"); err == nil {
		t.Fatal("expected error for malformed state")
	}
}

func TestListConnections(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := newTestService(t, &fakeSDK{}, store)
	userID := mintUUID(5)
	seedActive(store, userID, "notion", "ca_a")

	conns, err := svc.ListConnections(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(conns) != 1 || conns[0].ToolkitSlug != "notion" || conns[0].Status != "active" {
		t.Fatalf("conns = %+v", conns)
	}
}

func TestDisconnect_OwnerRevokeIdempotentAndFilter(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sdkFake := &fakeSDK{}
	svc := newTestService(t, sdkFake, store)
	userID := mintUUID(6)
	row := seedActive(store, userID, "notion", "ca_z")

	if err := svc.Disconnect(context.Background(), userID, row.ID); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if len(sdkFake.revoked) != 1 || sdkFake.revoked[0] != "ca_z" {
		t.Errorf("revoked = %v", sdkFake.revoked)
	}
	// Local row should now be filtered out of the active list.
	conns, _ := svc.ListConnections(context.Background(), userID)
	if len(conns) != 0 {
		t.Errorf("expected 0 active after disconnect, got %d", len(conns))
	}
	// Second disconnect is idempotent (row still owned, marks revoked again).
	if err := svc.Disconnect(context.Background(), userID, row.ID); err != nil {
		t.Fatalf("idempotent Disconnect: %v", err)
	}
}

func TestDisconnect_UpstreamNotFoundIsIdempotent(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sdkFake := &fakeSDK{revokeErr: &sdk.APIError{HTTPStatus: http.StatusNotFound}}
	svc := newTestService(t, sdkFake, store)
	userID := mintUUID(8)
	row := seedActive(store, userID, "notion", "ca_404")

	if err := svc.Disconnect(context.Background(), userID, row.ID); err != nil {
		t.Fatalf("Disconnect should treat upstream 404 as success, got %v", err)
	}
}

func TestDisconnect_NotOwner(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := newTestService(t, &fakeSDK{}, store)
	owner := mintUUID(9)
	row := seedActive(store, owner, "notion", "ca_o")
	attacker := mintUUID(10)
	if err := svc.Disconnect(context.Background(), attacker, row.ID); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("expected ErrConnectionNotFound for non-owner, got %v", err)
	}
}

func TestCreateMCPSession_NoOpWhenEmpty(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{}
	svc := newTestService(t, sdkFake, newFakeStore())
	sess, err := svc.CreateMCPSession(context.Background(), mintUUID(11))
	if err != nil {
		t.Fatalf("CreateMCPSession: %v", err)
	}
	if sess != nil {
		t.Fatalf("expected nil session when no connections, got %+v", sess)
	}
	if sdkFake.createSessCalls != 0 {
		t.Errorf("CreateSession should not be called when there are no connections")
	}
}

func TestCreateMCPSession_PinsConnectedAccounts(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sdkFake := &fakeSDK{}
	svc := newTestService(t, sdkFake, store)
	userID := mintUUID(12)
	seedActive(store, userID, "notion", "ca_pin")

	sess, err := svc.CreateMCPSession(context.Background(), userID)
	if err != nil {
		t.Fatalf("CreateMCPSession: %v", err)
	}
	if sess == nil || sess.URL != "https://mcp.example/session" {
		t.Fatalf("session = %+v", sess)
	}
	if sess.Headers["x-api-key"] != "secret" {
		t.Errorf("headers = %+v", sess.Headers)
	}
	if sdkFake.lastSessReq.UserID != util.UUIDToString(userID) {
		t.Errorf("session user id = %q", sdkFake.lastSessReq.UserID)
	}
	if got := sdkFake.lastSessReq.ConnectedAccounts["notion"]; got != "ca_pin" {
		t.Errorf("connected_accounts pin = %v, want ca_pin", got)
	}
}

func TestCallbackRedirect(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeSDK{}, newFakeStore())
	if got := svc.CallbackRedirect("notion", true); got != "https://app.multica.ai/settings/integrations?connected=notion" {
		t.Errorf("success redirect = %q", got)
	}
	if got := svc.CallbackRedirect("notion", false); got != "https://app.multica.ai/settings/integrations?error=composio_connect_failed" {
		t.Errorf("failure redirect = %q", got)
	}
}

// seedActive inserts an active connection through the store and returns the row.
func seedActive(store *fakeStore, userID pgtype.UUID, slug, caID string) db.UserComposioConnection {
	row, _ := store.UpsertUserComposioConnection(context.Background(), db.UpsertUserComposioConnectionParams{
		UserID:             userID,
		ToolkitSlug:        slug,
		AuthConfigID:       "ac_notion",
		ConnectedAccountID: caID,
		ComposioUserID:     util.UUIDToString(userID),
	})
	return row
}
