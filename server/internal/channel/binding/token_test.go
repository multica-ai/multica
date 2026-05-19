package binding_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/binding"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeTokenStore is a minimal in-memory binding.TokenStore. It captures the
// last CreateChannelBindToken arguments so tests can assert on the exact
// hash bytes, provider, and TTL the production code persists. The fake
// deliberately does NOT decode plaintext from anywhere — if the issuer
// ever leaks plaintext through this seam, the test will see an extra
// (unexpected) field and we'll catch it.
type fakeTokenStore struct {
	createCalls []db.CreateChannelBindTokenParams
	createErr   error

	consumeFunc func(ctx context.Context, hash []byte) (db.ChannelBindToken, error)
	getFunc     func(ctx context.Context, hash []byte) (db.ChannelBindToken, error)

	now func() time.Time
}

func (f *fakeTokenStore) CreateChannelBindToken(ctx context.Context, arg db.CreateChannelBindTokenParams) (db.ChannelBindToken, error) {
	f.createCalls = append(f.createCalls, arg)
	if f.createErr != nil {
		return db.ChannelBindToken{}, f.createErr
	}
	return db.ChannelBindToken{
		TokenHash:      arg.TokenHash,
		Provider:       arg.Provider,
		ExternalUserID: arg.ExternalUserID,
		ExpiresAt:      arg.ExpiresAt,
		CreatedAt:      pgtype.Timestamptz{Time: f.now(), Valid: true},
	}, nil
}

func (f *fakeTokenStore) ConsumeChannelBindToken(ctx context.Context, tokenHash []byte) (db.ChannelBindToken, error) {
	if f.consumeFunc != nil {
		return f.consumeFunc(ctx, tokenHash)
	}
	return db.ChannelBindToken{}, errors.New("fakeTokenStore: ConsumeChannelBindToken not configured")
}

func (f *fakeTokenStore) GetChannelBindToken(ctx context.Context, tokenHash []byte) (db.ChannelBindToken, error) {
	if f.getFunc != nil {
		return f.getFunc(ctx, tokenHash)
	}
	return db.ChannelBindToken{}, errors.New("fakeTokenStore: GetChannelBindToken not configured")
}

// TC-bind-1 · 未绑定 → 推一次性绑定链接（AC3.1）
//
// Asserts (per TestCase §3 TC-bind-1):
//   - Plaintext is at least 32 bytes after base64url decode (i.e. ≥ 32
//     bytes of randomness, not 32 bytes of base64 string).
//   - The persisted token_hash is exactly SHA-256(plaintext bytes) and
//     is 32 bytes long.
//   - expires_at - now() lies within [9'30", 10'30"] (PRD AC3.4
//     tolerance).
//   - The plaintext is never present in the parameters that reach the
//     database (i.e. CreateChannelBindTokenParams has no plaintext
//     field; we double-check by structural inspection).
func TestTokenIssuer_Issue_PersistsHashOnly(t *testing.T) {
	t.Parallel()

	frozenNow := time.Date(2026, 5, 3, 21, 0, 0, 0, time.UTC)
	store := &fakeTokenStore{now: func() time.Time { return frozenNow }}
	issuer := binding.NewTokenIssuer(store)

	ctx := context.Background()
	res, err := issuer.Issue(ctx, "feishu", "ou_test_001")
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	// Plaintext entropy: at least 32 bytes after base64url decoding.
	rawPlaintext, err := base64.RawURLEncoding.DecodeString(res.Plaintext)
	if err != nil {
		t.Fatalf("plaintext is not valid base64url: %v (plaintext=%q)", err, res.Plaintext)
	}
	if len(rawPlaintext) < 32 {
		t.Fatalf("plaintext entropy too low: got %d bytes, want >= 32", len(rawPlaintext))
	}

	// Exactly one CreateChannelBindToken call recorded.
	if got := len(store.createCalls); got != 1 {
		t.Fatalf("expected exactly 1 CreateChannelBindToken call, got %d", got)
	}
	call := store.createCalls[0]

	// Hash binding: token_hash MUST equal SHA-256(plaintext string).
	// The plaintext delivered to the user is the base64url-encoded string;
	// that's what they submit back, so the hash is of the string, not the
	// raw entropy bytes.
	wantHash := sha256.Sum256([]byte(res.Plaintext))
	if len(call.TokenHash) != 32 {
		t.Fatalf("token_hash length: got %d, want 32 (SHA-256 raw bytes, NOT hex)", len(call.TokenHash))
	}
	if string(call.TokenHash) != string(wantHash[:]) {
		t.Fatalf("token_hash != SHA-256(plaintext)\n got:  %x\n want: %x", call.TokenHash, wantHash[:])
	}

	// Plaintext NEVER reaches the DB. CreateChannelBindTokenParams has
	// only TokenHash/Provider/ExternalUserID/ExpiresAt. We assert by
	// scanning the params: there must be no field whose value contains
	// the plaintext (defence-in-depth — sqlc-generated struct shape is
	// already proven by compilation).
	if call.Provider == res.Plaintext || call.ExternalUserID == res.Plaintext {
		t.Fatalf("plaintext leaked into a non-hash field of CreateChannelBindTokenParams")
	}

	// Provider + external_user_id are forwarded verbatim.
	if call.Provider != "feishu" {
		t.Fatalf("provider: got %q, want %q", call.Provider, "feishu")
	}
	if call.ExternalUserID != "ou_test_001" {
		t.Fatalf("external_user_id: got %q, want %q", call.ExternalUserID, "ou_test_001")
	}

	// TTL: expires_at - now() ∈ [9'30", 10'30"]. We don't have access
	// to the issuer's internal clock; assert against time.Now() with
	// a generous lower bound so the test isn't flaky on slow CI.
	if !call.ExpiresAt.Valid {
		t.Fatalf("expires_at must be Valid (NOT NULL in schema)")
	}
	delta := time.Until(call.ExpiresAt.Time)
	if delta < 9*time.Minute+30*time.Second || delta > 10*time.Minute+30*time.Second {
		t.Fatalf("TTL out of [9'30\", 10'30\"]: got %s", delta)
	}

	// And the same TTL is reflected in the IssueResult so callers can
	// surface the deadline to the user.
	if !res.ExpiresAt.Equal(call.ExpiresAt.Time) {
		t.Fatalf("IssueResult.ExpiresAt mismatch with persisted expires_at: got %v vs %v",
			res.ExpiresAt, call.ExpiresAt.Time)
	}
}

// TC-bind-1 follow-up · two consecutive Issue calls produce distinct
// plaintexts and distinct hashes (i.e. crypto/rand is wired, not a
// deterministic stub).
func TestTokenIssuer_Issue_GeneratesUniqueTokens(t *testing.T) {
	t.Parallel()

	frozenNow := time.Date(2026, 5, 3, 21, 0, 0, 0, time.UTC)
	store := &fakeTokenStore{now: func() time.Time { return frozenNow }}
	issuer := binding.NewTokenIssuer(store)

	ctx := context.Background()
	a, err := issuer.Issue(ctx, "feishu", "ou_user_a")
	if err != nil {
		t.Fatalf("Issue #1: %v", err)
	}
	b, err := issuer.Issue(ctx, "feishu", "ou_user_b")
	if err != nil {
		t.Fatalf("Issue #2: %v", err)
	}

	if a.Plaintext == b.Plaintext {
		t.Fatalf("two consecutive Issue calls returned identical plaintext (entropy broken)")
	}
	if len(store.createCalls) != 2 {
		t.Fatalf("expected 2 CreateChannelBindToken calls, got %d", len(store.createCalls))
	}
	if string(store.createCalls[0].TokenHash) == string(store.createCalls[1].TokenHash) {
		t.Fatalf("two consecutive Issue calls produced identical token_hash")
	}
}

// TC-bind-4b · Consume rejects already-consumed tokens (unit slice).
//
// The integration variant (real Postgres, RowsAffected==0 path) lives in
// cmd/server/channel_binding_integration_test.go; this fast unit test
// asserts the in-Go error mapping when ConsumeChannelBindToken returns
// pgx.ErrNoRows and the follow-up SELECT shows consumed_at is set.
func TestTokenConsumer_Consume_AlreadyConsumed_ReturnsTypedError(t *testing.T) {
	t.Parallel()

	store := &fakeTokenStore{
		consumeFunc: func(ctx context.Context, hash []byte) (db.ChannelBindToken, error) {
			// sqlc's :one with the WHERE consumed_at IS NULL AND
			// expires_at > now() guard returns pgx.ErrNoRows when no
			// row matches — which collapses both "already consumed"
			// and "expired" into the same wire-level signal.
			return db.ChannelBindToken{}, pgx.ErrNoRows
		},
		getFunc: func(ctx context.Context, hash []byte) (db.ChannelBindToken, error) {
			// Row exists, not expired, but already consumed.
			return db.ChannelBindToken{
				TokenHash:      hash,
				Provider:       "feishu",
				ExternalUserID: "ou_test_001",
				ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
				ConsumedAt:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
			}, nil
		},
	}
	consumer := binding.NewTokenConsumer(store)

	plaintext := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	_, err := consumer.Consume(context.Background(), plaintext)
	if err == nil {
		t.Fatalf("expected error for consumed token, got nil")
	}
	if !errors.Is(err, binding.ErrTokenAlreadyConsumed) {
		t.Fatalf("error should wrap ErrTokenAlreadyConsumed; got %v", err)
	}
}

// TC-bind-4a unit · Consume rejects expired tokens (unit slice).
func TestTokenConsumer_Consume_Expired_ReturnsTypedError(t *testing.T) {
	t.Parallel()

	store := &fakeTokenStore{
		consumeFunc: func(ctx context.Context, hash []byte) (db.ChannelBindToken, error) {
			return db.ChannelBindToken{}, pgx.ErrNoRows
		},
		getFunc: func(ctx context.Context, hash []byte) (db.ChannelBindToken, error) {
			return db.ChannelBindToken{
				TokenHash:      hash,
				Provider:       "feishu",
				ExternalUserID: "ou_test_001",
				ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Second), Valid: true},
				ConsumedAt:     pgtype.Timestamptz{Valid: false},
			}, nil
		},
	}
	consumer := binding.NewTokenConsumer(store)

	plaintext := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	_, err := consumer.Consume(context.Background(), plaintext)
	if !errors.Is(err, binding.ErrTokenExpired) {
		t.Fatalf("error should wrap ErrTokenExpired; got %v", err)
	}
}

// TC-bind-4 boundary · Consume rejects malformed plaintext without
// touching the store. Defends against a caller that forwards user input
// directly (e.g. typo in the bind URL) — must not turn into a DB error.
func TestTokenConsumer_Consume_MalformedPlaintext_RejectedLocally(t *testing.T) {
	t.Parallel()

	storeCalled := false
	store := &fakeTokenStore{
		consumeFunc: func(ctx context.Context, hash []byte) (db.ChannelBindToken, error) {
			storeCalled = true
			return db.ChannelBindToken{}, nil
		},
	}
	consumer := binding.NewTokenConsumer(store)

	cases := []struct {
		name      string
		plaintext string
	}{
		{"empty", ""},
		{"non_base64url", "this is not base64url!!!"},
		{"too_short", base64.RawURLEncoding.EncodeToString(make([]byte, 4))},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := consumer.Consume(context.Background(), tc.plaintext)
			if !errors.Is(err, binding.ErrTokenInvalid) {
				t.Fatalf("plaintext=%q: want ErrTokenInvalid, got %v", tc.plaintext, err)
			}
		})
	}
	if storeCalled {
		t.Fatalf("malformed plaintext must not reach the store (was called)")
	}
}
