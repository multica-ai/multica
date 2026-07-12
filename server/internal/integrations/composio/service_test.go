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
	// account-ownership verification (CompleteCallback). By default
	// ListConnectedAccounts echoes the requested id with acctUserID /
	// acctAuthConfigID so success-path tests can opt in to a matching account;
	// acctMissing returns no items, listAccountsErr forces a transport error.
	acctUserID             string
	acctAuthConfigID       string
	acctNestedAuthConfigID string
	acctMissing            bool
	listAccountsErr        error
	lastListAccounts       sdk.ListConnectedAccountsRequest
	// auth-config resolution (BeginConnect / ListToolkits connectable flag).
	// authConfigs nil => a default single notion→ac_notion ENABLED config so
	// existing connect tests keep resolving; set explicitly to override.
	authConfigs    []sdk.AuthConfig
	authConfigsSet bool
	listAuthErr    error
	// toolkit catalog (ListToolkits).
	toolkits        []sdk.Toolkit
	listToolkitsErr error
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

func (f *fakeSDK) ListConnectedAccounts(_ context.Context, req sdk.ListConnectedAccountsRequest) (*sdk.ListConnectedAccountsResponse, error) {
	f.lastListAccounts = req
	if f.listAccountsErr != nil {
		return nil, f.listAccountsErr
	}
	if f.acctMissing {
		return &sdk.ListConnectedAccountsResponse{}, nil
	}
	id := ""
	if len(req.ConnectedAccountIDs) > 0 {
		id = req.ConnectedAccountIDs[0]
	}
	return &sdk.ListConnectedAccountsResponse{Items: []sdk.ConnectedAccount{{
		ID:           id,
		UserID:       f.acctUserID,
		AuthConfigID: f.acctAuthConfigID,
		AuthConfig:   sdk.AuthConfigRef{ID: f.acctNestedAuthConfigID},
	}}}, nil
}

func (f *fakeSDK) ListAuthConfigs(_ context.Context, _ sdk.ListAuthConfigsRequest) (*sdk.ListAuthConfigsResponse, error) {
	if f.listAuthErr != nil {
		return nil, f.listAuthErr
	}
	items := f.authConfigs
	if !f.authConfigsSet && items == nil {
		items = []sdk.AuthConfig{{
			ID:                "ac_notion",
			Toolkit:           sdk.Toolkit{Slug: "notion"},
			Status:            "ENABLED",
			IsComposioManaged: true,
		}}
	}
	return &sdk.ListAuthConfigsResponse{Items: items}, nil
}

func (f *fakeSDK) ListToolkits(_ context.Context, _ sdk.ListToolkitsRequest) (*sdk.ListToolkitsResponse, error) {
	if f.listToolkitsErr != nil {
		return nil, f.listToolkitsErr
	}
	return &sdk.ListToolkitsResponse{Items: f.toolkits}, nil
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
	// The resolved auth_config_id is signed into the state so the callback can
	// verify the returned account against it exactly (no fail-open re-resolve).
	if claims.AuthConfigID != "ac_notion" {
		t.Errorf("state auth config = %q, want ac_notion", claims.AuthConfigID)
	}
}

func TestBeginConnect_UnsupportedToolkit(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeSDK{}, newFakeStore())
	if _, err := svc.BeginConnect(context.Background(), mintUUID(1), "github"); !errors.Is(err, ErrToolkitNotSupported) {
		t.Fatalf("expected ErrToolkitNotSupported, got %v", err)
	}
}

// TestBeginConnect_UnsupportedWhenNoAuthConfig: with the project reporting no
// enabled auth configs at all, even notion is not connectable.
func TestBeginConnect_UnsupportedWhenNoAuthConfig(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{authConfigsSet: true, authConfigs: []sdk.AuthConfig{}}
	svc := newTestService(t, sdkFake, newFakeStore())
	if _, err := svc.BeginConnect(context.Background(), mintUUID(1), "notion"); !errors.Is(err, ErrToolkitNotSupported) {
		t.Fatalf("expected ErrToolkitNotSupported with no auth configs, got %v", err)
	}
}

// TestBeginConnect_PrefersCustomAuthConfig: when a toolkit has both a
// Composio-managed and a custom (white-label) auth config, the custom one wins.
func TestBeginConnect_PrefersCustomAuthConfig(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{authConfigsSet: true, authConfigs: []sdk.AuthConfig{
		{ID: "ac_managed", Toolkit: sdk.Toolkit{Slug: "notion"}, Status: "ENABLED", IsComposioManaged: true},
		{ID: "ac_custom", Toolkit: sdk.Toolkit{Slug: "notion"}, Status: "ENABLED", IsComposioManaged: false},
	}}
	svc := newTestService(t, sdkFake, newFakeStore())
	if _, err := svc.BeginConnect(context.Background(), mintUUID(1), "notion"); err != nil {
		t.Fatalf("BeginConnect: %v", err)
	}
	if sdkFake.lastCreateLink.AuthConfigID != "ac_custom" {
		t.Errorf("auth config = %q, want ac_custom (custom preferred over managed)", sdkFake.lastCreateLink.AuthConfigID)
	}
}

// TestListToolkits_FiltersToConnectable: only toolkits with an enabled auth
// config are returned (MUL-4009); the rest are dropped from the catalog, and
// every surfaced entry is Connectable by construction.
func TestListToolkits_FiltersToConnectable(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		authConfigsSet: true,
		authConfigs: []sdk.AuthConfig{
			{ID: "ac_notion", Toolkit: sdk.Toolkit{Slug: "notion"}, Status: "ENABLED"},
			{ID: "ac_slack", Toolkit: sdk.Toolkit{Slug: "slack"}, Status: "ENABLED"},
		},
		toolkits: []sdk.Toolkit{
			{Slug: "github", Name: "GitHub", LogoURL: "https://logo/gh", Categories: []string{"dev"}},
			{Slug: "notion", Name: "Notion", LogoURL: "https://logo/notion", Categories: []string{"productivity"}},
			{Slug: "slack", Name: "Slack"},
		},
	}
	svc := newTestService(t, sdkFake, newFakeStore())
	tks, err := svc.ListToolkits(context.Background())
	if err != nil {
		t.Fatalf("ListToolkits: %v", err)
	}
	// notion and slack have enabled auth configs; github is filtered out.
	if len(tks) != 2 {
		t.Fatalf("expected 2 connectable toolkits, got %d: %+v", len(tks), tks)
	}
	bySlug := make(map[string]ToolkitView, len(tks))
	for _, tk := range tks {
		if !tk.Connectable {
			t.Errorf("surfaced toolkit %q must be connectable", tk.Slug)
		}
		if tk.Slug == "github" {
			t.Errorf("non-connectable toolkit %q should have been filtered out", tk.Slug)
		}
		bySlug[tk.Slug] = tk
	}
	notion, ok := bySlug["notion"]
	if !ok {
		t.Fatalf("expected notion in results: %+v", tks)
	}
	if notion.Name != "Notion" || notion.LogoURL != "https://logo/notion" || notion.Category != "productivity" {
		t.Errorf("notion fields not mapped: %+v", notion)
	}
	// slack carried no upstream logo, so the composio default logo URL is derived.
	if slack, ok := bySlug["slack"]; !ok || slack.LogoURL != "https://logos.composio.dev/api/slack" {
		t.Errorf("slack default logo = %+v", bySlug["slack"])
	}
}

// TestListToolkits_ResolverErrorReturnsError: an /auth_configs failure is
// surfaced as an error rather than silently degrading to an empty catalog
// (MUL-4009). With filtering in place, masking the error would render as a
// misleading "no apps configured" empty state, so the handler must be able to
// return a 502 instead.
func TestListToolkits_ResolverErrorReturnsError(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		listAuthErr: errors.New("upstream blip"),
		toolkits:    []sdk.Toolkit{{Slug: "notion", Name: "Notion"}},
	}
	svc := newTestService(t, sdkFake, newFakeStore())
	tks, err := svc.ListToolkits(context.Background())
	if err == nil {
		t.Fatalf("ListToolkits should fail on auth-config resolver error, got %+v", tks)
	}
	if tks != nil {
		t.Errorf("expected nil toolkits on error, got %+v", tks)
	}
}

func TestCompleteCallback_SuccessAndIdempotent(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(3)
	// The account Composio reports for ca_123 belongs to this user under the
	// notion auth config, so ownership verification passes.
	sdkFake := &fakeSDK{acctUserID: util.UUIDToString(userID), acctAuthConfigID: "ac_notion"}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:       util.UUIDToString(userID),
		ToolkitSlug:  "notion",
		AuthConfigID: "ac_notion",
		Exp:          time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
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

func TestCompleteCallback_AcceptsNestedAuthConfig(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(30)
	// Composio v3.1 returns connected-account auth config under auth_config.id,
	// not always as a top-level auth_config_id.
	sdkFake := &fakeSDK{acctUserID: util.UUIDToString(userID), acctNestedAuthConfigID: "ac_notion"}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:       util.UUIDToString(userID),
		ToolkitSlug:  "notion",
		AuthConfigID: "ac_notion",
		Exp:          time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})

	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_nested"); err != nil {
		t.Fatalf("CompleteCallback: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(store.rows))
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

// TestCompleteCallback_TamperedAccountRejected covers the PR 4608 blocker:
// a valid, un-expired state paired with a connected_account_id that Composio
// reports as belonging to a DIFFERENT user must be rejected, and no row written.
func TestCompleteCallback_TamperedAccountRejected(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(20)
	// Composio says ca_evil belongs to someone else, not our state's user.
	sdkFake := &fakeSDK{acctUserID: util.UUIDToString(mintUUID(99)), acctAuthConfigID: "ac_notion"}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:       util.UUIDToString(userID),
		ToolkitSlug:  "notion",
		AuthConfigID: "ac_notion",
		Exp:          time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})
	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_evil"); !errors.Is(err, ErrAccountVerification) {
		t.Fatalf("expected ErrAccountVerification for foreign account, got %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("no row should be written when ownership fails, got %d", len(store.rows))
	}
}

// TestCompleteCallback_WrongAuthConfigRejected is the cross-toolkit proof: the
// account belongs to the right user but was created under a DIFFERENT toolkit's
// auth config (e.g. the user pasting their slack account id into a notion
// callback). The state-signed auth_config_id must not match, so it is rejected.
func TestCompleteCallback_WrongAuthConfigRejected(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(21)
	// Account is owned by the user but lives under ac_other (another toolkit).
	sdkFake := &fakeSDK{acctUserID: util.UUIDToString(userID), acctAuthConfigID: "ac_other"}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:       util.UUIDToString(userID),
		ToolkitSlug:  "notion",
		AuthConfigID: "ac_notion",
		Exp:          time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})
	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_x"); !errors.Is(err, ErrAccountVerification) {
		t.Fatalf("expected ErrAccountVerification for wrong auth config, got %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("no row should be written, got %d", len(store.rows))
	}
}

// TestCompleteCallback_MissingAuthConfigFailsClosed is the regression for the
// re-review blocker: a state with no signed auth_config_id (the old fail-open
// path) plus an account owned by the user must STILL be rejected — the empty
// expected auth config now fails closed instead of skipping the check.
func TestCompleteCallback_MissingAuthConfigFailsClosed(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(25)
	// Account genuinely belongs to the user — only the missing auth-config
	// binding should trip the rejection.
	sdkFake := &fakeSDK{acctUserID: util.UUIDToString(userID), acctAuthConfigID: "ac_notion"}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:      util.UUIDToString(userID),
		ToolkitSlug: "notion",
		// AuthConfigID deliberately omitted (empty) — must fail closed.
		Exp: time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})
	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_owned"); !errors.Is(err, ErrAccountVerification) {
		t.Fatalf("expected ErrAccountVerification when state carries no auth config, got %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("no row should be written, got %d", len(store.rows))
	}
}

// TestCompleteCallback_UnknownAccountRejected ensures an account id Composio
// does not know about fails closed rather than being mirrored verbatim.
func TestCompleteCallback_UnknownAccountRejected(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	userID := mintUUID(22)
	sdkFake := &fakeSDK{acctMissing: true}
	svc := newTestService(t, sdkFake, store)
	state, _ := signState(testSecret, stateClaims{
		UserID:      util.UUIDToString(userID),
		ToolkitSlug: "notion",
		Exp:         time.Unix(1_700_000_000, 0).Add(time.Minute).Unix(),
	})
	if _, err := svc.CompleteCallback(context.Background(), state, "success", "ca_ghost"); !errors.Is(err, ErrAccountVerification) {
		t.Fatalf("expected ErrAccountVerification for unknown account, got %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("no row should be written, got %d", len(store.rows))
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

// TestDisconnect_RevokedRowNoOp covers the PR 4608 blocker: once a row is
// locally revoked, a second DELETE must be a pure no-op and must NOT call
// upstream again — otherwise a non-404 upstream error on the repeat would be
// surfaced as a 502 and break idempotency.
func TestDisconnect_RevokedRowNoOp(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sdkFake := &fakeSDK{}
	svc := newTestService(t, sdkFake, store)
	userID := mintUUID(30)
	row := seedActive(store, userID, "notion", "ca_noop")

	// First disconnect revokes upstream and marks the row revoked.
	if err := svc.Disconnect(context.Background(), userID, row.ID); err != nil {
		t.Fatalf("first Disconnect: %v", err)
	}
	if len(sdkFake.revoked) != 1 {
		t.Fatalf("expected 1 upstream revoke, got %d", len(sdkFake.revoked))
	}

	// Now make the upstream fail with a NON-404 error. A correct no-op must not
	// touch upstream, so this error must never surface.
	sdkFake.revokeErr = &sdk.APIError{HTTPStatus: http.StatusInternalServerError}
	sdkFake.deleteErr = &sdk.APIError{HTTPStatus: http.StatusInternalServerError}
	if err := svc.Disconnect(context.Background(), userID, row.ID); err != nil {
		t.Fatalf("second Disconnect on already-revoked row should be a no-op, got %v", err)
	}
	if len(sdkFake.revoked) != 1 {
		t.Errorf("second disconnect must not call upstream revoke again, revoked=%v", sdkFake.revoked)
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
	assertPinnedAccount(t, sdkFake.lastSessReq, "notion", "ca_pin")
}

func TestCallbackRedirect(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeSDK{}, newFakeStore())
	if got := svc.CallbackRedirect("notion", true); got != "https://app.multica.ai/settings?tab=integrations&connected=notion" {
		t.Errorf("success redirect = %q", got)
	}
	if got := svc.CallbackRedirect("notion", false); got != "https://app.multica.ai/settings?tab=integrations&error=composio_connect_failed" {
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
