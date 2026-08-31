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
)

// ---------------------------------------------------------------------------
// 1. A shed belongs to the replica that could have sent it
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

// A frame goes to EVERY replica, so a full queue on one that holds no socket
// costs the user nothing — the holder still has its own copy and still sends.
// Counting a reply drop there reports the same reply as delivered and dropped
// at once, and once more for every further replica that happened to be full.
//
// relay_shed is the counter that should move: a queue overflowing is a real
// local admission event, worth seeing wherever it happens.
func TestRelayShed_OnAReplicaThatHoldsNoSocketIsNotAReplyLoss(t *testing.T) {
	t.Parallel()
	// Depth 1 and never started: the first frame fills the only slot and the
	// second has nowhere to go. No timing involved.
	router := NewRelayOutbound(&fanoutRelay{}, nil,
		RelayConfig{Shards: 1, QueueDepth: 1}, slog.Default())
	mx := newCountingMetrics()
	router.SetMetrics(mx)
	router.Attach(&ownsSocketHandler{owns: false})

	body, _ := json.Marshal(relayFrame{Kind: relayKindReply, InstallationID: "inst-1", Content: "答案"})
	router.DeliverWecomOutbound("inst-1", body, "ev-1") // fills the queue
	router.DeliverWecomOutbound("inst-1", body, "ev-2") // shed

	if got := mx.get("relay_shed:" + relayKindReply); got != 1 {
		t.Errorf("relay_shed = %d, want 1 — the local admission decision still happened", got)
	}
	if got := mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped = %d, want 0 — this replica was never going to send that reply, "+
			"so shedding it cost the user nothing and the holder still counts the delivery", got)
	}
}

// The other half: when the shedding replica IS the holder, the reply really is
// gone and the drop counter has to say so.
func TestRelayShed_OnTheHolderIsAReplyLoss(t *testing.T) {
	t.Parallel()
	router := NewRelayOutbound(&fanoutRelay{}, nil,
		RelayConfig{Shards: 1, QueueDepth: 1}, slog.Default())
	mx := newCountingMetrics()
	router.SetMetrics(mx)
	router.Attach(&ownsSocketHandler{owns: true})

	body, _ := json.Marshal(relayFrame{Kind: relayKindReply, InstallationID: "inst-1", Content: "答案"})
	router.DeliverWecomOutbound("inst-1", body, "ev-1")
	router.DeliverWecomOutbound("inst-1", body, "ev-2")

	if got := mx.get("outbound_dropped:" + string(dropRelayOverflow)); got != 1 {
		t.Errorf("outbound_dropped:%s = %d, want 1 — the replica holding the socket shed it, "+
			"so nobody sent it", dropRelayOverflow, got)
	}
}

// An inbox push never moves the reply counters on either replica: its unit is
// not an agent reply. This is the rule shed already had, kept honest alongside
// the new ownership condition.
func TestRelayShed_AnInboxPushNeverMovesTheReplyCounter(t *testing.T) {
	t.Parallel()
	router := NewRelayOutbound(&fanoutRelay{}, nil,
		RelayConfig{Shards: 1, QueueDepth: 1}, slog.Default())
	mx := newCountingMetrics()
	router.SetMetrics(mx)
	router.Attach(&ownsSocketHandler{owns: true})

	body, _ := json.Marshal(relayFrame{Kind: relayKindInbox, InstallationID: "inst-1", Content: "通知"})
	router.DeliverWecomOutbound("inst-1", body, "ev-1")
	router.DeliverWecomOutbound("inst-1", body, "ev-2")

	if got := mx.get("relay_shed:" + relayKindInbox); got != 1 {
		t.Errorf("relay_shed:%s = %d, want 1", relayKindInbox, got)
	}
	if got := mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped = %d, want 0 — an inbox push is not an agent reply", got)
	}
}

// ownsSocketNow is read from the shard reader, which runs before Attach during
// boot. It must answer false rather than panic on a nil handler.
func TestRelayOwnsSocketNow_IsFalseBeforeAttach(t *testing.T) {
	t.Parallel()
	router := NewRelayOutbound(&fanoutRelay{}, nil, RelayConfig{Shards: 1}, slog.Default())
	if router.ownsSocketNow("inst-1") {
		t.Fatal("ownsSocketNow = true with no handler attached")
	}
}

// ---------------------------------------------------------------------------
// 2. The outcome grace carries one claim round trip PER OFFER
// ---------------------------------------------------------------------------

// The re-offer chain makes a claim on every attempt, not once for the whole
// chain, and each of those can burn the full ClaimTimeout. A grace built from
// the backoffs plus a single round trip therefore expires while the chain is
// still running against a slow store — and the watcher records
// no_live_connection for a reply the very next attempt then delivers, so one
// reply moves both counters.
func TestRelayOutcomeGrace_CoversAClaimRoundTripPerOffer(t *testing.T) {
	t.Parallel()
	r := NewRelayOutbound(nil, nil, RelayConfig{}, slog.Default())

	var chain time.Duration
	for _, d := range r.retryPlan {
		chain += d
	}
	// Worst case the chain can actually take: every backoff, plus a claim that
	// times out on the first offer and on each re-offer.
	worst := chain + r.cfg.ClaimTimeout*time.Duration(len(r.retryPlan)+1)

	if r.outcomeGrace() < worst {
		t.Fatalf("outcome grace %s is shorter than the %s a fully timed-out chain can take "+
			"(%d offers × %s claim + %s of backoff) — the watch would call a reply lost while "+
			"it was still being retried, and the retry would then deliver it",
			r.outcomeGrace(), worst, len(r.retryPlan)+1, r.cfg.ClaimTimeout, chain)
	}
}

// ClaimTimeout sizes the grace but the bound belongs to the claim store, so the
// default has to be the store's own. Nothing links them at compile time; this
// is what catches the drift.
func TestRelayClaimTimeout_MatchesTheStoreItSizes(t *testing.T) {
	t.Parallel()
	if got := (RelayConfig{}).withDefaults().ClaimTimeout; got != claimTimeout {
		t.Fatalf("default ClaimTimeout = %s, but redisDedupe bounds a round trip at %s — "+
			"the outcome grace is sized from the wrong number", got, claimTimeout)
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
