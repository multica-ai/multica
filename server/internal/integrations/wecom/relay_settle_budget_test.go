package wecom

// relay_settle_budget_test.go — the settle retry lives INSIDE the shutdown
// budget it was added under.
//
// The retry that resolves an unknown settle is worth having: a delivered reply
// whose claim never gets settled is a reply nothing counts. But it turned one
// store round trip into three, and a graceful shutdown promises to be done in
// DrainBudget. Both halves of that promise are checked here — the store call
// that must not take a fresh budget past its caller's deadline, and the loop
// that must not open a new attempt once the deadline has passed.
//
// Neither needs a database or a Redis: the rule under test is which context
// the work runs on.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// The claim-bookkeeping calls drop CANCELLATION on purpose — a settle for a
// frame already in the user's chat has to outlive the shutdown that
// interrupted it — but a DEADLINE is not cancellation. drainRemaining bounds
// the whole drain with one, so a round trip that helped itself to a full fresh
// budget past that point spends time the shutdown already promised away.
//
// REVERSE VERIFICATION: restore context.WithTimeout(context.WithoutCancel(ctx),
// d.budget) and the first case fails with a deadline a whole budget out.
func TestRedisDedupe_BookkeepingKeepsABoundingDeadline(t *testing.T) {
	t.Parallel()
	d := &redisDedupe{log: slog.Default(), budget: 2 * time.Second}

	// A caller's deadline shorter than the store's budget is the bound.
	bounded, cancelBounded := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelBounded()
	ctx, cancel := d.bookkeepingBudget(bounded)
	defer cancel()
	got, ok := ctx.Deadline()
	if !ok {
		t.Fatal("the bookkeeping context has no deadline, want the caller's")
	}
	if want, _ := bounded.Deadline(); !got.Equal(want) {
		t.Fatalf("deadline = %v, want the caller's %v: a drain budget has to bound the round trips inside it", got, want)
	}

	// Cancelling the caller does NOT end the call: the delivery is already in
	// the chat and its claim still has to be settled.
	live, cancelLive := context.WithCancel(context.Background())
	survives, cancelSurvives := d.bookkeepingBudget(live)
	defer cancelSurvives()
	cancelLive()
	select {
	case <-survives.Done():
		t.Fatal("the bookkeeping context died with its caller: a delivered frame's claim still has to be settled")
	default:
	}

	// With no caller deadline the store's own budget is what applies.
	own, cancelOwn := d.bookkeepingBudget(context.Background())
	defer cancelOwn()
	deadline, ok := own.Deadline()
	if !ok || time.Until(deadline) <= time.Second {
		t.Fatalf("deadline = %v (ok=%v), want the store's own budget out", deadline, ok)
	}
}

// A deadline the caller sets has to reach the WIRE, not just the context.
//
// go-redis discards context deadlines unless ContextTimeoutEnabled is set
// (baseClient.context returns context.Background() otherwise), so a store
// built on a default client bounds its commands by the socket timeout and
// nothing else. Choosing the earlier deadline in bookkeepingBudget would then
// be bookkeeping about bookkeeping: the drain still overruns its budget by
// however far ReadTimeout reaches, which in production is 3s.
//
// So this drives a REAL redis.Client over a connection that swallows the
// request and never answers, and asserts which of the two bounds wins.
//
// REVERSE VERIFICATION: it is built in — the same store on a default client is
// the second case, and it waits out the socket timeout instead.
func TestRedisDedupe_ACallersDeadlineReachesTheWire(t *testing.T) {
	t.Parallel()
	const (
		callerDeadline = 40 * time.Millisecond
		socketTimeout  = 400 * time.Millisecond
	)
	// A store on a client built the way cmd/server builds this one, and the
	// same store on go-redis's defaults.
	newStore := func(t *testing.T, honoursDeadlines bool) *redisDedupe {
		t.Helper()
		srv, cli := net.Pipe()
		go func() { _, _ = io.Copy(io.Discard, srv) }()
		t.Cleanup(func() { _ = srv.Close() })
		rdb := redis.NewClient(&redis.Options{
			Dialer:                func(context.Context, string, string) (net.Conn, error) { return cli, nil },
			ReadTimeout:           socketTimeout,
			WriteTimeout:          socketTimeout,
			ContextTimeoutEnabled: honoursDeadlines,
			// One attempt: this measures which bound applies, not how many
			// times go-redis is willing to apply it.
			MaxRetries: -1,
		})
		t.Cleanup(func() { _ = rdb.Close() })
		return &redisDedupe{rdb: rdb, log: slog.Default(), budget: 2 * time.Second}
	}
	settle := func(t *testing.T, d *redisDedupe) time.Duration {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), callerDeadline)
		defer cancel()
		start := time.Now()
		if _, err := d.Settle(ctx, "wecom:outbound:claim:test", "owner/ev"); err == nil {
			t.Fatal("Settle against a connection that never answers returned no error")
		}
		return time.Since(start)
	}

	// The production wiring: the caller's deadline is what ends the wait.
	if took := settle(t, newStore(t, true)); took >= socketTimeout {
		t.Fatalf("Settle took %v with a %v caller deadline: the deadline never reached the wire, "+
			"so a drain cannot hold the budget it promised", took, callerDeadline)
	}
	// The default client, kept as the contrast that makes the flag load-bearing.
	if took := settle(t, newStore(t, false)); took < socketTimeout {
		t.Fatalf("a default client bounded Settle at %v, so this test no longer demonstrates why "+
			"the claim store needs its own client", took)
	}
}

// slowSettleStore is a claim store whose Settle takes a fixed round trip
// REGARDLESS of the context it is handed, and never answers.
//
// That is deliberately the shape the production store had BEFORE it inherited
// its caller's deadline, because it is what makes the caller's own bound
// observable: against a store that already honours the deadline there is
// nothing left for the retry loop to get wrong.
type slowSettleStore struct {
	roundTrip time.Duration

	mu       sync.Mutex
	attempts int
}

func (s *slowSettleStore) Claim(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}

func (s *slowSettleStore) Release(context.Context, string, string) (bool, error) { return true, nil }

func (s *slowSettleStore) Settle(context.Context, string, string) (bool, error) {
	s.mu.Lock()
	s.attempts++
	s.mu.Unlock()
	time.Sleep(s.roundTrip)
	return false, errors.New("dedupe: the store never answered")
}

func (s *slowSettleStore) Resolve(context.Context, string) (claimState, error) {
	return claimHeld, nil
}

func (s *slowSettleStore) ClaimBudget() time.Duration { return s.roundTrip }

func (s *slowSettleStore) settleAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

// A drain that has spent its budget opens no further store attempt. The one in
// flight when the deadline passes is allowed to finish — it is a round trip
// already paid for, and abandoning it would lose the settle it may be about to
// land — but the chain stops there.
//
// One round trip alone outlives the whole budget here, so the count is exact
// rather than a race: one attempt, not the full chain.
//
// REVERSE VERIFICATION: drop the settleBudgetSpent break from settleClaim and
// this fails with three attempts and a drain roughly three round trips long.
func TestRelayDrain_OpensNoNewSettleAttemptPastItsBudget(t *testing.T) {
	t.Parallel()
	const (
		drainBudget = 50 * time.Millisecond
		roundTrip   = 100 * time.Millisecond
	)
	store := &slowSettleStore{roundTrip: roundTrip}
	h := &ownsSocketHandler{owns: true}
	router := NewRelayOutbound(&fanoutRelay{}, store, RelayConfig{
		Shards:       1,
		DrainBudget:  drainBudget,
		RetryBackoff: 10 * time.Millisecond,
	}, testLogger())
	router.Attach(h)

	late := queued{
		frame:   relayFrame{Kind: relayKindReply, InstallationID: "inst-1", Content: "answer"},
		eventID: "ev-1",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	router.drainRemaining(ctx, make(chan queued), map[string]*hold{}, &late)
	elapsed := time.Since(start)

	if got := store.settleAttempts(); got != 1 {
		t.Fatalf("the drain made %d settle attempts, want 1: the first already ran past a %v budget, "+
			"so no further attempt may be opened", got, drainBudget)
	}
	if elapsed >= drainBudget+2*roundTrip {
		t.Fatalf("the drain took %v: a %v budget must not be extended by a whole retry chain", elapsed, drainBudget)
	}
	if got := len(h.sent()); got != 1 {
		t.Fatalf("%d frames reached the chat, want 1: the delivery itself is not what is bounded here", got)
	}
}
