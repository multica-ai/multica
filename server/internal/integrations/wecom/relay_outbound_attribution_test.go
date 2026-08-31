package wecom

// relay_outbound_attribution_test.go — three properties the dispatcher gets
// wrong in ways nothing else here would notice, because each one is about
// ATTRIBUTION rather than delivery: who a shed belongs to, how long a reply may
// still be in flight before it counts as lost, and which of two answers goes
// out first when the process is on its way down.
//
// None of them needs a database or a Redis: each is a decision the dispatcher
// makes on its own, so the test makes it directly.

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// 1. A shed is an admission decision, never a reply outcome
// ---------------------------------------------------------------------------

// ownsSocketHandler holds the socket, or does not, and records nothing else.
type ownsSocketHandler struct {
	mu    sync.Mutex
	owns  bool
	calls []relayFrame
}

func (h *ownsSocketHandler) ownsSocket(string) bool { return h.owns }
func (h *ownsSocketHandler) deliverRelayed(_ context.Context, f relayFrame) deliveryOutcome {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, f)
	return outcomeDone
}
func (h *ownsSocketHandler) sent() []relayFrame {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]relayFrame(nil), h.calls...)
}

// shedRouter is a router wired to shed: depth 1, never started, so the first
// frame fills the only slot and the second has nowhere to go. No timing.
func shedRouter(t *testing.T, owns bool) (*RelayOutbound, *countingMetrics) {
	t.Helper()
	r := NewRelayOutbound(&fanoutRelay{}, nil, RelayConfig{Shards: 1, QueueDepth: 1}, slog.Default())
	mx := newCountingMetrics()
	r.SetMetrics(mx)
	r.Attach(&ownsSocketHandler{owns: owns})
	return r, mx
}

func shedTwice(t *testing.T, r *RelayOutbound, kind string) {
	t.Helper()
	body, err := json.Marshal(relayFrame{Kind: kind, InstallationID: "inst-1", Content: "答案"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r.DeliverWecomOutbound("inst-1", body, "ev-1") // fills the queue
	r.DeliverWecomOutbound("inst-1", body, "ev-2") // shed
}

// No replica can decide a reply's fate from a shed, so none of them may move
// the reply counter — not even the one holding the socket.
//
// Every replica reads every frame. The one that sheds may not be the one that
// would have sent it, and during a lease handoff two replicas can hold a
// sender at once, so "do I hold the socket" is not proof of being the only one
// who could. Each replica answering for itself is what produced one reply
// counted as delivered and dropped at the same time. The publisher's
// watchOutcomes is the single owner that settles it after the fact.
func TestRelayShed_NeverMovesTheReplyCounter(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		owns bool
	}{
		{"holding the socket", true},
		{"holding no socket", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, mx := shedRouter(t, tc.owns)
			shedTwice(t, r, relayKindReply)

			if got := mx.get("relay_shed:" + relayKindReply); got != 1 {
				t.Errorf("relay_shed = %d, want 1 — the admission decision still happened", got)
			}
			if got := mx.get("outbound_dropped"); got != 0 {
				t.Errorf("outbound_dropped = %d, want 0 — a shed is not a reply outcome; "+
					"the publisher's outcome watch settles that, once", got)
			}
		})
	}
}

// An inbox push moves relay_shed under its own label and nothing else: its
// unit is not an agent reply.
func TestRelayShed_AnInboxPushIsLabelledSeparately(t *testing.T) {
	t.Parallel()
	r, mx := shedRouter(t, true)
	shedTwice(t, r, relayKindInbox)

	if got := mx.get("relay_shed:" + relayKindInbox); got != 1 {
		t.Errorf("relay_shed:%s = %d, want 1", relayKindInbox, got)
	}
	if got := mx.get("relay_shed:"+relayKindReply) + mx.get("outbound_dropped"); got != 0 {
		t.Errorf("reply counters moved by %d on an inbox push, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// 2. The outcome grace carries one claim round trip PER OFFER
// ---------------------------------------------------------------------------

// budgetStore is a DedupeStore that only exists to state a budget.
type budgetStore struct {
	DedupeStore
	budget time.Duration
}

func (b budgetStore) ClaimBudget() time.Duration { return b.budget }

// The re-offer chain makes a claim on every attempt, not once for the whole
// chain, and each of those can burn the full budget. A grace built from the
// backoffs plus a single round trip therefore expires while the chain is still
// running against a slow store — and the watcher records no_live_connection
// for a reply the very next attempt then delivers, so one reply moves both
// counters.
func TestRelayOutcomeGrace_CoversAClaimRoundTripPerOffer(t *testing.T) {
	t.Parallel()
	const budget = 2 * time.Second
	r := NewRelayOutbound(nil, budgetStore{budget: budget}, RelayConfig{}, slog.Default())

	var chain time.Duration
	for _, d := range r.retryPlan {
		chain += d
	}
	// Worst case the chain can actually take: every backoff, plus a claim that
	// times out on the first offer and on each re-offer.
	worst := chain + budget*time.Duration(len(r.retryPlan)+1)

	if r.outcomeGrace() < worst {
		t.Fatalf("outcome grace %s is shorter than the %s a fully timed-out chain can take "+
			"(%d offers × %s claim + %s of backoff) — the watch would call a reply lost while "+
			"it was still being retried, and the retry would then deliver it",
			r.outcomeGrace(), worst, len(r.retryPlan)+1, budget, chain)
	}
}

// The grace is sized from the budget the STORE enforces, not from a number the
// dispatcher keeps for itself — that is the whole reason ClaimBudget is on the
// interface. A store with a different bound must move the grace with it.
func TestRelayOutcomeGrace_TracksTheStoresOwnBudget(t *testing.T) {
	t.Parallel()
	small := NewRelayOutbound(nil, budgetStore{budget: 10 * time.Millisecond}, RelayConfig{}, slog.Default())
	large := NewRelayOutbound(nil, budgetStore{budget: 10 * time.Second}, RelayConfig{}, slog.Default())
	if small.outcomeGrace() >= large.outcomeGrace() {
		t.Fatalf("grace did not follow the store's budget: 10ms store gave %s, 10s store gave %s",
			small.outcomeGrace(), large.outcomeGrace())
	}
}

// The production store's default is what the dispatcher inherits when a
// deployment does not override it.
func TestRedisDedupe_DefaultsToTheDocumentedClaimBudget(t *testing.T) {
	t.Parallel()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}) // never dialled
	t.Cleanup(func() { rdb.Close() })
	if got := NewRedisDedupe(rdb, 0, slog.Default()).ClaimBudget(); got != defaultClaimBudget {
		t.Fatalf("ClaimBudget() = %s, want the documented default %s", got, defaultClaimBudget)
	}
}

// ---------------------------------------------------------------------------
// 3. Shutdown does not invert two answers
// ---------------------------------------------------------------------------

// A worker can take a frame off the queue in the same turn the cancel fires:
// the select is fair, so the queue branch can win against an already-closed
// Done. That frame is handed to the drain as `first`.
//
// It arrived AFTER whatever is parked at its installation's line, so performing
// it first inverts two answers in the user's chat — the exact reordering
// offer() exists to prevent, undone on the way out. drainRemaining is called
// directly here because the interleaving that produces it is a race by nature,
// and the decision under test is not.
func TestRelayDrain_DoesNotLetALateFrameOvertakeAParkedOne(t *testing.T) {
	t.Parallel()
	h := &ownsSocketHandler{owns: true}
	router := NewRelayOutbound(&fanoutRelay{}, nil, RelayConfig{Shards: 1}, slog.Default())
	router.Attach(h)

	const inst = "inst-1"
	// "first" in the chat is the one already parked, waiting out its backoff.
	lines := map[string]*hold{
		inst: {items: []queued{{
			frame:   relayFrame{Kind: relayKindReply, InstallationID: inst, Content: "answer-1"},
			eventID: "ev-1",
		}}},
	}
	// The one the worker had just taken off the queue when the cancel won.
	late := queued{
		frame:   relayFrame{Kind: relayKindReply, InstallationID: inst, Content: "answer-2"},
		eventID: "ev-2",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	router.drainRemaining(ctx, make(chan queued), lines, &late)

	var got []string
	for _, f := range h.sent() {
		got = append(got, f.Content)
	}
	want := []string{"answer-1", "answer-2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the chat received %v, want %v — shutdown let a later reply overtake the one "+
			"already waiting at the head of its installation's line", got, want)
	}
}

// A late frame for an installation with no line has nothing to overtake, so it
// still goes out on the drain rather than being stranded.
func TestRelayDrain_ALateFrameWithNoLineIsStillDelivered(t *testing.T) {
	t.Parallel()
	h := &ownsSocketHandler{owns: true}
	router := NewRelayOutbound(&fanoutRelay{}, nil, RelayConfig{Shards: 1}, slog.Default())
	router.Attach(h)

	late := queued{
		frame:   relayFrame{Kind: relayKindReply, InstallationID: "inst-2", Content: "answer"},
		eventID: "ev-1",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	router.drainRemaining(ctx, make(chan queued), map[string]*hold{}, &late)

	if got := h.sent(); len(got) != 1 || got[0].Content != "answer" {
		t.Fatalf("drained %d frames (%v), want the one answer", len(got), got)
	}
}
