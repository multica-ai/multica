package wecom

// dedupe_redis_test.go — the four Lua operations against a real Redis, one
// assertion per rule the dispatcher relies on. Skips without REDIS_TEST_URL.

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestRedisDedupe_TokenFencedOperations(t *testing.T) {
	rdb := wecomTestRedis(t)
	ctx := context.Background()
	d := NewRedisDedupe(rdb, testClaimBudget, slog.Default())
	const key, a, b = "wecom:outbound:claim:test", "owner-a/ev", "owner-b/ev"
	ttl := time.Minute

	// Claim: first taker wins; the same owner may come back; another may not.
	if won, err := d.Claim(ctx, key, a, ttl); err != nil || !won {
		t.Fatalf("A's first claim = %v, %v; want won", won, err)
	}
	if won, err := d.Claim(ctx, key, a, ttl); err != nil || !won {
		t.Fatalf("A re-claiming its own key = %v, %v; want won (a retry after an unknown release)", won, err)
	}
	if won, err := d.Claim(ctx, key, b, ttl); err != nil || won {
		t.Fatalf("B claiming A's key = %v, %v; want lost", won, err)
	}
	// Release is compare-and-delete: B cannot take A's claim away, A can.
	if released, err := d.Release(ctx, key, b); err != nil || released {
		t.Fatalf("B releasing A's claim = %v, %v; want not released", released, err)
	}
	if released, err := d.Release(ctx, key, a); err != nil || !released {
		t.Fatalf("A releasing its claim = %v, %v; want released", released, err)
	}
	if released, err := d.Release(ctx, key, a); err != nil || released {
		t.Fatalf("A releasing again = %v, %v; want not released, not an error (a retry after the delete landed)", released, err)
	}
	if won, err := d.Claim(ctx, key, b, ttl); err != nil || !won {
		t.Fatalf("B claiming the released key = %v, %v; want won", won, err)
	}
	// Settle: only the holder; idempotent; fenced against a later Resolve.
	if ok, err := d.Settle(ctx, key, a); err != nil || ok {
		t.Fatalf("A settling B's claim = %v, %v; want refused", ok, err)
	}
	if ok, err := d.Settle(ctx, key, b); err != nil || !ok {
		t.Fatalf("B settling its claim = %v, %v; want settled", ok, err)
	}
	if ok, err := d.Settle(ctx, key, b); err != nil || !ok {
		t.Fatalf("B settling again = %v, %v; want settled (a retry is safe)", ok, err)
	}
	if st, err := d.Resolve(ctx, key); err != nil || st != claimSettled {
		t.Fatalf("Resolve after settle = %v, %v; want claimSettled", st, err)
	}
	// Resolve on a held key fences it: the holder's later Settle is refused.
	const key2 = key + ":2"
	if won, _ := d.Claim(ctx, key2, a, ttl); !won {
		t.Fatal("A's claim on key2 was not won")
	}
	if st, err := d.Resolve(ctx, key2); err != nil || st != claimHeld {
		t.Fatalf("Resolve on a held key = %v, %v; want claimHeld", st, err)
	}
	if ok, err := d.Settle(ctx, key2, a); err != nil || ok {
		t.Fatalf("A settling after the publisher resolved it = %v, %v; want refused", ok, err)
	}
	if st, err := d.Resolve(ctx, key2); err != nil || st != claimLost {
		t.Fatalf("Resolve again = %v, %v; want claimLost", st, err)
	}
	if won, _ := d.Claim(ctx, key2, b, ttl); won {
		t.Fatal("B claimed a key already resolved as lost; a late winner would deliver a reply already counted")
	}
	if st, err := d.Resolve(ctx, key+":absent"); err != nil || st != claimAbsent {
		t.Fatalf("Resolve on no key = %v, %v; want claimAbsent", st, err)
	}
	// A claim written before this scheme — the plain "1" of the old SET NX —
	// belongs to a holder that recorded its own outcome inline. Reported as
	// settled and left alone, so a rolling upgrade cannot turn a reply that
	// was already counted into a second, contradictory record.
	const legacy = key + ":legacy"
	if err := rdb.Set(ctx, legacy, "1", ttl).Err(); err != nil {
		t.Fatalf("seed a legacy claim: %v", err)
	}
	if st, err := d.Resolve(ctx, legacy); err != nil || st != claimSettled {
		t.Fatalf("Resolve on a legacy claim = %v, %v; want claimSettled", st, err)
	}
	if v := rdb.Get(ctx, legacy).Val(); v != "1" {
		t.Fatalf("Resolve rewrote a legacy claim to %q; it belongs to a holder this process cannot speak for", v)
	}
	// TTL survives settle and resolve (KEEPTTL), so the key still expires.
	if pttl := rdb.PTTL(ctx, key).Val(); pttl <= 0 || pttl > ttl {
		t.Fatalf("settled key TTL = %v, want within %v", pttl, ttl)
	}
}
