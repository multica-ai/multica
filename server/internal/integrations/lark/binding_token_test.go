package lark

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// These tests cover the pure-Go halves of BindingTokenService — token
// generation entropy/encoding, deterministic hashing — without
// touching the database. DB-backed mint/redeem invariants (single use,
// expiry) are covered by the DB CHECK on lark_binding_token plus the
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
