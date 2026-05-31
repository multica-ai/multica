package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// insertTestPAT creates a PAT row for the shared test user with the given
// expiry and returns (rawToken, patID). Each call generates a fresh raw token
// so a test can hold many independent rows without colliding on token_hash.
// The row is auto-cleaned at test end.
func insertTestPAT(t *testing.T, expiresAt time.Time) (string, string) {
	t.Helper()
	raw, err := auth.GeneratePATToken()
	if err != nil {
		t.Fatalf("generate pat: %v", err)
	}
	prefix := raw
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	pat, err := testHandler.Queries.CreatePersonalAccessToken(context.Background(), db.CreatePersonalAccessTokenParams{
		UserID:      parseUUID(testUserID),
		Name:        "renew-test",
		TokenHash:   auth.HashToken(raw),
		TokenPrefix: prefix,
		ExpiresAt:   pgtype.Timestamptz{Time: expiresAt, Valid: !expiresAt.IsZero()},
	})
	if err != nil {
		t.Fatalf("create pat: %v", err)
	}
	patID := uuidToString(pat.ID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM personal_access_token WHERE id = $1`, parseUUID(patID))
	})
	return raw, patID
}

// newRenewRequest builds a POST /api/tokens/current/renew request with both
// the X-User-ID and Authorization headers set, so the handler can resolve
// the PAT row in addition to the caller's user.
func newRenewRequest(rawToken string) *http.Request {
	req := newRequest("POST", "/api/tokens/current/renew", nil)
	if rawToken != "" {
		req.Header.Set("Authorization", "Bearer "+rawToken)
	}
	return req
}

func decodeRenewResponse(t *testing.T, body *httptest.ResponseRecorder) RenewPATResponse {
	t.Helper()
	var resp RenewPATResponse
	if err := json.NewDecoder(body.Body).Decode(&resp); err != nil {
		t.Fatalf("decode renew response: %v (body: %s)", err, body.Body.String())
	}
	return resp
}

func TestRenewPAT_ExtendsWhenInsideRenewalWindow(t *testing.T) {
	// 3 days remaining — well inside the 7-day threshold.
	oldExpiry := time.Now().Add(3 * 24 * time.Hour)
	raw, patID := insertTestPAT(t, oldExpiry)

	w := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeRenewResponse(t, w)
	if !resp.Renewed {
		t.Fatalf("expected renewed=true, got false (expires_at=%s)", resp.ExpiresAt)
	}

	var actual time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT expires_at FROM personal_access_token WHERE id = $1`, parseUUID(patID),
	).Scan(&actual); err != nil {
		t.Fatalf("readback: %v", err)
	}
	// Renewed expiry should be roughly now + PATRenewExtension (90 days),
	// well past the old expiry. Use a wide window — the test only needs to
	// know the row was bumped, not the exact instant.
	if !actual.After(oldExpiry.Add(24 * time.Hour)) {
		t.Fatalf("expected new expiry to be far past old %v, got %v", oldExpiry, actual)
	}
	wantAround := time.Now().Add(PATRenewExtension)
	if actual.Before(wantAround.Add(-time.Hour)) || actual.After(wantAround.Add(time.Hour)) {
		t.Fatalf("expected new expiry near %v, got %v", wantAround, actual)
	}
}

func TestRenewPAT_NoOpWhenOutsideRenewalWindow(t *testing.T) {
	// 30 days remaining — well outside the 7-day threshold.
	oldExpiry := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	raw, patID := insertTestPAT(t, oldExpiry)

	w := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeRenewResponse(t, w)
	if resp.Renewed {
		t.Fatalf("expected renewed=false, got true (expires_at=%s)", resp.ExpiresAt)
	}

	var actual time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT expires_at FROM personal_access_token WHERE id = $1`, parseUUID(patID),
	).Scan(&actual); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if !actual.Equal(oldExpiry) {
		t.Fatalf("no-op should not change expires_at; old=%v new=%v", oldExpiry, actual)
	}
}

func TestRenewPAT_RejectsExpiredToken(t *testing.T) {
	raw, _ := insertTestPAT(t, time.Now().Add(-time.Hour))

	w := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
	// Expired tokens are filtered by GetPersonalAccessTokenByHash, so the
	// handler reports 401 — the auth middleware in production would already
	// have rejected the request, but the handler defends in depth.
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRenewPAT_RejectsRevokedToken(t *testing.T) {
	raw, patID := insertTestPAT(t, time.Now().Add(3*24*time.Hour))
	if _, err := testPool.Exec(context.Background(),
		`UPDATE personal_access_token SET revoked = TRUE WHERE id = $1`, parseUUID(patID),
	); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for revoked token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRenewPAT_RejectsNonPATAuthHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"empty", ""},
		{"missing bearer prefix", "mul_abc123"},
		{"wrong prefix", "Bearer mdt_abc123"},
		{"jwt", "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0.sig"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest("POST", "/api/tokens/current/renew", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			testHandler.RenewCurrentPersonalAccessToken(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRenewPAT_HandlesNullExpiresAt(t *testing.T) {
	// Pre-existing PATs may carry NULL expires_at; the handler returns
	// renewed=false with an empty expires_at field rather than failing.
	raw, _ := insertTestPAT(t, time.Time{})

	w := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeRenewResponse(t, w)
	if resp.Renewed {
		t.Fatalf("expected renewed=false for NULL expiry, got true")
	}
	if resp.ExpiresAt != "" {
		t.Fatalf("expected empty expires_at for NULL expiry, got %q", resp.ExpiresAt)
	}
}

func TestRenewPAT_ConcurrentRenewIsIdempotent(t *testing.T) {
	// Two callers race to extend the same PAT. The WHERE clause on
	// ExtendPersonalAccessTokenExpiry guarantees only one UPDATE actually
	// bumps the row; the loser sees pgx.ErrNoRows and reports renewed=false
	// with the already-extended expires_at. Both calls return 200.
	raw, patID := insertTestPAT(t, time.Now().Add(2*24*time.Hour))

	w1 := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w1, newRenewRequest(raw))
	if w1.Code != http.StatusOK {
		t.Fatalf("first renew: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	resp1 := decodeRenewResponse(t, w1)
	if !resp1.Renewed {
		t.Fatal("first renew should have extended the row")
	}

	w2 := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w2, newRenewRequest(raw))
	if w2.Code != http.StatusOK {
		t.Fatalf("second renew: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	resp2 := decodeRenewResponse(t, w2)
	if resp2.Renewed {
		t.Fatal("second renew should be a no-op (token now far in the future)")
	}
	if resp2.ExpiresAt != resp1.ExpiresAt {
		t.Fatalf("second renew should report same expires_at as first; got %q vs %q",
			resp2.ExpiresAt, resp1.ExpiresAt)
	}

	// And the DB only carries the single extended value.
	var actual time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT expires_at FROM personal_access_token WHERE id = $1`, parseUUID(patID),
	).Scan(&actual); err != nil {
		t.Fatalf("readback: %v", err)
	}
	wantAround := time.Now().Add(PATRenewExtension)
	if actual.Before(wantAround.Add(-time.Hour)) || actual.After(wantAround.Add(time.Hour)) {
		t.Fatalf("expected expiry near %v, got %v", wantAround, actual)
	}
}

// TestRenewPAT_ParallelRenewExtendsExactlyOnce locks in the SQL-level
// idempotency that the MUL-2744 review flagged: when N callers race to
// renew the same in-window PAT, the WHERE clause must ensure only one
// UPDATE actually bumps the row. The previous condition (`expires_at < $2`)
// silently let every caller win — each computed a slightly larger
// `$2 = now + 90d`, so the second writer's $2 always exceeded the first
// writer's row value and the UPDATE re-matched. Pinning the CAS to the
// renewal threshold instead (`expires_at <= $3`) means after the first
// writer pushes expires_at to now + 90d, all subsequent writers see a
// row already past the threshold and the UPDATE matches zero rows.
//
// We verify the database side by counting how many times the row's
// expires_at column was actually moved across N parallel calls.
func TestRenewPAT_ParallelRenewExtendsExactlyOnce(t *testing.T) {
	const concurrency = 8

	// Token has 2 days remaining — comfortably inside the 7-day window so
	// every caller passes the handler's threshold pre-check and all of
	// them get a chance to fight at the SQL layer.
	oldExpiry := time.Now().Add(2 * 24 * time.Hour)
	raw, patID := insertTestPAT(t, oldExpiry)

	type result struct {
		code      int
		expiresAt string
		renewed   bool
	}
	results := make([]result, concurrency)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
			var resp RenewPATResponse
			_ = json.NewDecoder(w.Body).Decode(&resp)
			results[i] = result{code: w.Code, expiresAt: resp.ExpiresAt, renewed: resp.Renewed}
		}(i)
	}
	close(start)
	wg.Wait()

	var winners int
	var winnerExpiry string
	for _, r := range results {
		if r.code != http.StatusOK {
			t.Fatalf("concurrent renew should never return non-200; got %d (renewed=%v expires_at=%q)", r.code, r.renewed, r.expiresAt)
		}
		if r.renewed {
			winners++
			winnerExpiry = r.expiresAt
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one caller to flip renewed=true; got %d winners across %d calls", winners, concurrency)
	}

	// All losing callers report the same already-extended expires_at, and
	// the DB carries that same value. If the old (buggy) condition were
	// still in place, several callers would have re-bumped the row to
	// strictly-larger now+90d values and the final expiry would not match
	// the first winner's response.
	var finalExpiry time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT expires_at FROM personal_access_token WHERE id = $1`, parseUUID(patID),
	).Scan(&finalExpiry); err != nil {
		t.Fatalf("readback: %v", err)
	}
	finalAsString := timestampToString(pgtype.Timestamptz{Time: finalExpiry, Valid: true})
	if winnerExpiry != "" && finalAsString != winnerExpiry {
		t.Fatalf("DB expires_at must match the winner's response (no double-bump); db=%q winner=%q", finalAsString, winnerExpiry)
	}
}

func TestRenewPAT_RejectsTokenBelongingToDifferentUser(t *testing.T) {
	// Mint a PAT for a different user, then send a request that pairs that
	// PAT's Authorization header with our shared test user's X-User-ID
	// (simulating a forged identity past the middleware). The handler MUST
	// refuse to renew on the wrong user's behalf.
	ctx := context.Background()
	var otherUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Other User", "other-renew@multica.ai").Scan(&otherUserID); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, parseUUID(otherUserID))
	})

	raw, err := auth.GeneratePATToken()
	if err != nil {
		t.Fatalf("generate pat: %v", err)
	}
	prefix := raw
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	pat, err := testHandler.Queries.CreatePersonalAccessToken(ctx, db.CreatePersonalAccessTokenParams{
		UserID:      parseUUID(otherUserID),
		Name:        "other-renew",
		TokenHash:   auth.HashToken(raw),
		TokenPrefix: prefix,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(3 * 24 * time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatalf("create other pat: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM personal_access_token WHERE id = $1`, pat.ID)
	})

	w := httptest.NewRecorder()
	// newRequest sets X-User-ID = testUserID, but the bearer is otherUser's PAT.
	testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on user mismatch, got %d: %s", w.Code, w.Body.String())
	}
}
