package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// mintSetupTokenViaHandler drives MintSetupToken for the shared test user +
// workspace and returns the raw mst_ token. The row is reaped at test end.
func mintSetupTokenViaHandler(t *testing.T) string {
	t.Helper()
	req := newRequest("POST", "/api/setup-tokens", map[string]any{"workspace_id": testWorkspaceID})
	w := httptest.NewRecorder()
	testHandler.MintSetupToken(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("mint setup token: status %d (body: %s)", w.Code, w.Body.String())
	}
	var resp MintSetupTokenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode mint response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM setup_token WHERE token_hash = $1`, auth.HashToken(resp.Token))
	})
	return resp.Token
}

// exchangeRequest builds a public (no auth header) exchange request — the mst_
// token in the body is the only credential the endpoint reads.
func exchangeRequest(token, name string) *http.Request {
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]any{"token": token, "name": name})
	req := httptest.NewRequest("POST", "/api/setup-tokens/exchange", &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestMintSetupToken_ReturnsRedeemablePrefixedToken(t *testing.T) {
	raw := mintSetupTokenViaHandler(t)
	if len(raw) < 4 || raw[:4] != auth.SetupTokenPrefix {
		t.Fatalf("expected mst_ prefix, got %q", raw)
	}
	// Row exists, unredeemed.
	var usedAt pgtype.Timestamptz
	if err := testPool.QueryRow(context.Background(),
		`SELECT used_at FROM setup_token WHERE token_hash = $1`, auth.HashToken(raw),
	).Scan(&usedAt); err != nil {
		t.Fatalf("expected a persisted setup_token row: %v", err)
	}
	if usedAt.Valid {
		t.Fatalf("freshly minted token should be unredeemed")
	}
}

func TestMintSetupToken_RejectsNonMemberWorkspace(t *testing.T) {
	// A random workspace UUID the test user is not a member of.
	req := newRequest("POST", "/api/setup-tokens", map[string]any{
		"workspace_id": "00000000-0000-0000-0000-0000000000ff",
	})
	w := httptest.NewRecorder()
	testHandler.MintSetupToken(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member workspace, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestExchangeSetupToken_ReturnsWorkingPAT(t *testing.T) {
	raw := mintSetupTokenViaHandler(t)

	w := httptest.NewRecorder()
	testHandler.ExchangeSetupToken(w, exchangeRequest(raw, "CLI (test-host)"))
	if w.Code != http.StatusOK {
		t.Fatalf("exchange: status %d (body: %s)", w.Code, w.Body.String())
	}
	var resp ExchangeSetupTokenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode exchange response: %v", err)
	}
	if len(resp.Token) < 4 || resp.Token[:4] != "mul_" {
		t.Fatalf("expected a mul_ PAT, got %q", resp.Token)
	}
	if resp.Email == "" {
		t.Fatalf("expected the owning user's email in the response")
	}

	// The minted PAT resolves to the same user and carries the passed name.
	pat, err := testHandler.Queries.GetPersonalAccessTokenByHash(context.Background(), auth.HashToken(resp.Token))
	if err != nil {
		t.Fatalf("minted PAT not found: %v", err)
	}
	if uuidToString(pat.UserID) != testUserID {
		t.Fatalf("PAT belongs to %s, want %s", uuidToString(pat.UserID), testUserID)
	}
	if pat.Name != "CLI (test-host)" {
		t.Fatalf("PAT name = %q, want the hostname label", pat.Name)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM personal_access_token WHERE id = $1`, pat.ID)
	})

	// The setup token is now marked used.
	var usedAt pgtype.Timestamptz
	testPool.QueryRow(context.Background(),
		`SELECT used_at FROM setup_token WHERE token_hash = $1`, auth.HashToken(raw),
	).Scan(&usedAt)
	if !usedAt.Valid {
		t.Fatalf("redeemed token should have used_at set")
	}
}

func TestExchangeSetupToken_IsSingleUse(t *testing.T) {
	raw := mintSetupTokenViaHandler(t)

	// First redeem succeeds.
	w1 := httptest.NewRecorder()
	testHandler.ExchangeSetupToken(w1, exchangeRequest(raw, ""))
	if w1.Code != http.StatusOK {
		t.Fatalf("first exchange should succeed, got %d", w1.Code)
	}
	var resp ExchangeSetupTokenResponse
	json.NewDecoder(w1.Body).Decode(&resp)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM personal_access_token WHERE token_hash = $1`, auth.HashToken(resp.Token))
	})

	// Second redeem of the same token is rejected — single use.
	w2 := httptest.NewRecorder()
	testHandler.ExchangeSetupToken(w2, exchangeRequest(raw, ""))
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("second exchange should be 401, got %d (body: %s)", w2.Code, w2.Body.String())
	}
}

func TestExchangeSetupToken_RejectsExpired(t *testing.T) {
	raw, err := auth.GenerateSetupToken()
	if err != nil {
		t.Fatalf("generate setup token: %v", err)
	}
	// Insert a row that already lapsed.
	_, err = testHandler.Queries.CreateSetupToken(context.Background(), db.CreateSetupTokenParams{
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(testWorkspaceID),
		TokenHash:   auth.HashToken(raw),
		TokenPrefix: raw[:12],
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
	})
	if err != nil {
		t.Fatalf("insert expired setup token: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM setup_token WHERE token_hash = $1`, auth.HashToken(raw))
	})

	w := httptest.NewRecorder()
	testHandler.ExchangeSetupToken(w, exchangeRequest(raw, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token should be 401, got %d", w.Code)
	}
}

func TestExchangeSetupToken_RejectsUnknownAndMalformed(t *testing.T) {
	// Well-formed prefix but never minted → 401.
	unknown, _ := auth.GenerateSetupToken()
	w := httptest.NewRecorder()
	testHandler.ExchangeSetupToken(w, exchangeRequest(unknown, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token should be 401, got %d", w.Code)
	}

	// Wrong prefix → 400, never reaching the DB.
	w = httptest.NewRecorder()
	testHandler.ExchangeSetupToken(w, exchangeRequest("mul_deadbeef", ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("wrong-prefix token should be 400, got %d", w.Code)
	}
}
