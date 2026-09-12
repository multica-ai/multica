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
	"log/slog"
	"sync"
	"testing"
	"time"

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

// slowSettleStore is a claim store whose Settle takes a fixed round trip
// regardless of the context it is handed.
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
// flight when the deadline passes is allowed to finish, but the chain stops.
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
