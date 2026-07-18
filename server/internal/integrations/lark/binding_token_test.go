package lark

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// These tests cover the pure-Go halves of BindingTokenService — token
// generation entropy/encoding, deterministic hashing — without
// touching the database. DB-backed mint/redeem invariants (single use,
// expiry) are covered by the DB CHECK on channel_binding_token plus the
// ConsumeLarkBindingToken query, which require an integration test
// against a real Postgres and are added in a follow-up.

func TestRandomTokenIsUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 256; i++ {
		tok, err := randomToken(32)
		if err != nil {
			t.Fatalf("randomToken: %v", err)
		}
		if _, dup := seen[tok]; dup {
			t.Fatalf("randomToken returned a duplicate after %d iterations: %q", i, tok)
		}
		seen[tok] = struct{}{}
	}
}

func TestRandomTokenURLSafe(t *testing.T) {
	tok, err := randomToken(32)
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}
	// RawURLEncoding alphabet: A-Z a-z 0-9 - _
	for _, r := range tok {
		ok := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			t.Fatalf("token contains non-url-safe rune %q in %q", r, tok)
		}
	}
	if strings.Contains(tok, "=") {
		t.Fatalf("RawURLEncoding should drop padding, got %q", tok)
	}
}

// TestRedeemAndBindRequiresTxStarter guards the constructor-misuse
// path: if a future refactor wires up BindingTokenService without a
// TxStarter (e.g. for a legacy code path that only needed Mint),
// RedeemAndBind must fail fast with a clear error rather than panic
// on the nil dereference at s.tx.Begin. The atomicity contract
// documented above depends on that transaction existing.
func TestRedeemAndBindRequiresTxStarter(t *testing.T) {
	svc := &BindingTokenService{}
	_, err := svc.RedeemAndBind(context.Background(), "tok", pgtype.UUID{})
	if err == nil {
		t.Fatal("expected error when TxStarter is nil, got nil")
	}
	if !strings.Contains(err.Error(), "missing TxStarter") {
		t.Fatalf("expected missing-TxStarter error, got %v", err)
	}
}

// TestBindingErrorSentinelsAreDistinct guards against accidentally
// collapsing the three rejection sentinels (e.g. someone making
// ErrBindingNotWorkspaceMember an alias of ErrBindingTokenInvalid to
// "hide" the workspace-membership signal). The HTTP handler maps
// each to a distinct status code (410/409/403); if errors.Is started
// matching the wrong sentinel, the response code would silently
// regress without any other test catching it.
func TestBindingErrorSentinelsAreDistinct(t *testing.T) {
	if errors.Is(ErrBindingAlreadyAssigned, ErrBindingTokenInvalid) ||
		errors.Is(ErrBindingTokenInvalid, ErrBindingAlreadyAssigned) {
		t.Fatal("ErrBindingAlreadyAssigned and ErrBindingTokenInvalid must not alias")
	}
	if errors.Is(ErrBindingNotWorkspaceMember, ErrBindingTokenInvalid) ||
		errors.Is(ErrBindingTokenInvalid, ErrBindingNotWorkspaceMember) {
		t.Fatal("ErrBindingNotWorkspaceMember and ErrBindingTokenInvalid must not alias")
	}
	if errors.Is(ErrBindingAlreadyAssigned, ErrBindingNotWorkspaceMember) ||
		errors.Is(ErrBindingNotWorkspaceMember, ErrBindingAlreadyAssigned) {
		t.Fatal("ErrBindingAlreadyAssigned and ErrBindingNotWorkspaceMember must not alias")
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	a := hashToken("hello")
	b := hashToken("hello")
	if a != b {
		t.Fatalf("hashToken non-deterministic: %q vs %q", a, b)
	}
	if a == hashToken("hello ") {
		t.Fatalf("hashToken collided trivially with whitespace variant")
	}
	if len(a) != 64 {
		t.Fatalf("expected sha256 hex (64 chars), got %d chars", len(a))
	}
}

// TestBindingTokenMintClampsAppClockSkew protects the production binding-card
// path. Application and database clocks can differ slightly; computing the
// full 15-minute TTL from an app clock that is ahead of Postgres used to trip
// channel_binding_token_ttl_cap, so unbound Feishu users received no card.
func TestBindingTokenMintClampsAppClockSkew(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	installationID := util.MustParseUUID("5c09e000-0000-4000-8000-000000000101")
	workspaceID := util.MustParseUUID("5c09e000-0000-4000-8000-000000000102")

	clean := func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM channel_binding_token WHERE installation_id = $1`, installationID)
	}
	clean()
	t.Cleanup(clean)

	service := NewBindingTokenServiceWithClock(db.New(pool), nil, func() time.Time {
		return time.Now().Add(time.Hour)
	})
	token, err := service.Mint(ctx, workspaceID, installationID, OpenID("ou_clock_skew"))
	if err != nil {
		t.Fatalf("Mint with app clock ahead of database: %v", err)
	}

	var storedExpiresAt, createdAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT expires_at, created_at
		FROM channel_binding_token
		WHERE installation_id = $1
	`, installationID).Scan(&storedExpiresAt, &createdAt); err != nil {
		t.Fatalf("read minted token: %v", err)
	}
	if storedExpiresAt.After(createdAt.Add(BindingTokenTTL)) {
		t.Fatalf("stored TTL exceeds cap: created=%s expires=%s", createdAt, storedExpiresAt)
	}
	if storedExpiresAt.Before(createdAt.Add(BindingTokenTTL - time.Second)) {
		t.Fatalf("stored TTL was shortened unexpectedly: created=%s expires=%s", createdAt, storedExpiresAt)
	}
	if !token.ExpiresAt.Equal(storedExpiresAt) {
		t.Fatalf("returned expiry %s does not match stored expiry %s", token.ExpiresAt, storedExpiresAt)
	}
}
