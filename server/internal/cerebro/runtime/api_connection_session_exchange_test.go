package runtime

// Locks the per-person session-exchange semantics (FIR-2564 fase 2):
// context plumbing, cache hit/miss + expiry skew, the exchange HTTP contract
// (shared key on the exchange call, personal key on the data call), and the
// fail-closed posture when exchange is enabled but unavailable.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/cerebro/credentials"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	testWorkspaceID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testConnectionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testMemberID     = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

// fakePersonKeyStore is an in-memory connectionPersonKeyStore.
type fakePersonKeyStore struct {
	rows    map[string]cerebrodb.CerebroConnectionPersonKey
	upserts int
}

func keyFor(conn, member pgtype.UUID) string {
	return util.UUIDToString(conn) + "/" + util.UUIDToString(member)
}

func (s *fakePersonKeyStore) GetConnectionPersonKey(_ context.Context, arg cerebrodb.GetConnectionPersonKeyParams) (cerebrodb.CerebroConnectionPersonKey, error) {
	row, ok := s.rows[keyFor(arg.ConnectionID, arg.MemberID)]
	if !ok {
		return cerebrodb.CerebroConnectionPersonKey{}, context.Canceled // any error means "no cache row"
	}
	return row, nil
}

func (s *fakePersonKeyStore) UpsertConnectionPersonKey(_ context.Context, arg cerebrodb.UpsertConnectionPersonKeyParams) (cerebrodb.CerebroConnectionPersonKey, error) {
	s.upserts++
	row := cerebrodb.CerebroConnectionPersonKey{
		WorkspaceID:   arg.WorkspaceID,
		ConnectionID:  arg.ConnectionID,
		MemberID:      arg.MemberID,
		KeyCiphertext: arg.KeyCiphertext,
		ExpiresAt:     arg.ExpiresAt,
	}
	if s.rows == nil {
		s.rows = map[string]cerebrodb.CerebroConnectionPersonKey{}
	}
	s.rows[keyFor(arg.ConnectionID, arg.MemberID)] = row
	return row, nil
}

func testCipher(t *testing.T) *credentials.Cipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	c, err := credentials.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testExchanger(t *testing.T, store *fakePersonKeyStore, now time.Time) *ConnectionSessionExchanger {
	t.Helper()
	return &ConnectionSessionExchanger{
		store:  store,
		cipher: testCipher(t),
		logger: slog.Default(),
		now:    func() time.Time { return now },
	}
}

func TestConnectionTriggerMemberContext(t *testing.T) {
	ctx := context.Background()
	if got := ConnectionTriggerMember(ctx); got != "" {
		t.Fatalf("empty ctx should carry no member, got %q", got)
	}
	if got := ConnectionTriggerMember(WithConnectionTriggerMember(ctx, "  ")); got != "" {
		t.Fatalf("blank member should not be stored, got %q", got)
	}
	ctx = WithConnectionTriggerMember(ctx, testMemberID)
	if got := ConnectionTriggerMember(ctx); got != testMemberID {
		t.Fatalf("got %q want %q", got, testMemberID)
	}
}

func TestPersonalKeyExchangesAndCaches(t *testing.T) {
	now := time.Now()
	var exchangeCalls int
	var seenAuth, seenBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/exchange" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		exchangeCalls++
		seenAuth = r.Header.Get("X-API-Key")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		b, _ := json.Marshal(body)
		seenBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":        "sk_personal_key_1",
			"expires_at": now.Add(time.Hour).Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	store := &fakePersonKeyStore{}
	x := testExchanger(t, store, now)
	auth := connections.AuthConfig{
		APIKey:          "rk_shared_system_key",
		APIKeyHeader:    "x-api-key",
		SessionExchange: &connections.SessionExchangeConfig{Enabled: true},
	}

	key, err := x.PersonalKey(context.Background(), srv.Client(), testWorkspaceID, testConnectionID, srv.URL, auth, testMemberID)
	if err != nil {
		t.Fatal(err)
	}
	if key != "sk_personal_key_1" {
		t.Fatalf("got key %q", key)
	}
	if exchangeCalls != 1 || store.upserts != 1 {
		t.Fatalf("exchangeCalls=%d upserts=%d", exchangeCalls, store.upserts)
	}
	if seenAuth != "rk_shared_system_key" {
		t.Fatalf("exchange must authenticate with the shared key, saw %q", seenAuth)
	}
	if want := `"principal":"member:` + testMemberID + `"`; !jsonContains(seenBody, want) {
		t.Fatalf("body %s missing %s", seenBody, want)
	}

	// Second call within the key lifetime: served from the encrypted cache.
	key2, err := x.PersonalKey(context.Background(), srv.Client(), testWorkspaceID, testConnectionID, srv.URL, auth, testMemberID)
	if err != nil {
		t.Fatal(err)
	}
	if key2 != key {
		t.Fatalf("cache should return the same key, got %q", key2)
	}
	if exchangeCalls != 1 {
		t.Fatalf("cached call must not re-exchange, calls=%d", exchangeCalls)
	}
}

func TestPersonalKeyReExchangesNearExpiry(t *testing.T) {
	now := time.Now()
	var exchangeCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchangeCalls++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			// Expires INSIDE the skew window — must not be served from cache.
			"key":        "sk_short_lived",
			"expires_at": now.Add(30 * time.Second).Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	store := &fakePersonKeyStore{}
	x := testExchanger(t, store, now)
	auth := connections.AuthConfig{APIKey: "rk_shared", SessionExchange: &connections.SessionExchangeConfig{Enabled: true}}

	for i := 0; i < 2; i++ {
		if _, err := x.PersonalKey(context.Background(), srv.Client(), testWorkspaceID, testConnectionID, srv.URL, auth, testMemberID); err != nil {
			t.Fatal(err)
		}
	}
	if exchangeCalls != 2 {
		t.Fatalf("a key expiring inside the skew must be re-exchanged, calls=%d", exchangeCalls)
	}
}

func TestPersonalKeyFailsClosedOnExchangeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"This API key is not allowed to exchange for that principal"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	x := testExchanger(t, &fakePersonKeyStore{}, time.Now())
	auth := connections.AuthConfig{APIKey: "rk_shared", SessionExchange: &connections.SessionExchangeConfig{Enabled: true}}
	if _, err := x.PersonalKey(context.Background(), srv.Client(), testWorkspaceID, testConnectionID, srv.URL, auth, testMemberID); err == nil {
		t.Fatal("a denied exchange must fail the call, got nil error")
	}
}

func TestAPIConnectionToolCallUsesPersonalKey(t *testing.T) {
	now := time.Now()
	var dataAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions/exchange", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":        "sk_personal",
			"expires_at": now.Add(time.Hour).Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		dataAuth = r.Header.Get("X-API-Key")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	auth := connections.AuthConfig{
		APIKey:          "rk_shared_system_key",
		SessionExchange: &connections.SessionExchangeConfig{Enabled: true},
	}
	tool := &APIConnectionTool{
		toolName:    "conn__get_data",
		connName:    "conn",
		baseURL:     srv.URL,
		method:      http.MethodGet,
		path:        "/data",
		auth:        auth,
		connID:      testConnectionID,
		workspaceID: testWorkspaceID,
		exchanger:   testExchanger(t, &fakePersonKeyStore{}, now),
		client:      srv.Client(),
	}

	// With a triggering human: the data call runs on the personal key.
	ctx := WithConnectionTriggerMember(context.Background(), testMemberID)
	if _, err := tool.Call(ctx, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if dataAuth != "sk_personal" {
		t.Fatalf("data call must use the personal key, saw %q", dataAuth)
	}

	// Without a triggering human (system run): the shared key is kept.
	if _, err := tool.Call(context.Background(), map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if dataAuth != "rk_shared_system_key" {
		t.Fatalf("system run must use the shared key, saw %q", dataAuth)
	}
}

func TestAPIConnectionToolCallFailsClosedWithoutExchanger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	tool := &APIConnectionTool{
		toolName: "conn__get_data",
		connName: "conn",
		baseURL:  srv.URL,
		method:   http.MethodGet,
		path:     "/data",
		auth: connections.AuthConfig{
			APIKey:          "rk_shared",
			SessionExchange: &connections.SessionExchangeConfig{Enabled: true},
		},
		client: srv.Client(),
	}
	ctx := WithConnectionTriggerMember(context.Background(), testMemberID)
	if _, err := tool.Call(ctx, map[string]any{}); err == nil {
		t.Fatal("exchange enabled without an exchanger must fail closed")
	}
}

func TestPersonalKeyAuthSlots(t *testing.T) {
	apiKeyAuth := connections.AuthConfig{APIKey: "rk_x", APIKeyHeader: "x-api-key", CFAccessID: "id", CFAccessSecret: "sec"}
	got := personalKeyAuth(apiKeyAuth, "sk_y")
	if got.APIKey != "sk_y" || got.BearerToken != "" {
		t.Fatalf("api-key slot: %+v", got)
	}
	if got.CFAccessID != "id" || got.CFAccessSecret != "sec" {
		t.Fatalf("cf access must be kept: %+v", got)
	}

	bearerAuth := connections.AuthConfig{BearerToken: "rk_x"}
	got = personalKeyAuth(bearerAuth, "sk_y")
	if got.BearerToken != "sk_y" || got.APIKey != "" {
		t.Fatalf("bearer slot: %+v", got)
	}
}

func jsonContains(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 && stringsContains(haystack, needle)
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
