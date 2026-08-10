package dingtalk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

func sessionUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func newTestAck(now func() time.Time) (*ackNotifier, *[]string) {
	var sent []string
	var mu sync.Mutex
	n := &ackNotifier{
		logger:      slog.Default(),
		window:      5 * time.Second,
		now:         now,
		lastAck:     map[string]time.Time{},
		outstanding: map[string]*sessionAcks{},
		sendText: func(_ context.Context, _ engine.ResolvedInstallation, _ channel.InboundMessage, text string) error {
			mu.Lock()
			defer mu.Unlock()
			sent = append(sent, text)
			return nil
		},
	}
	return n, &sent
}

// promisesFor reports how many posted promises the session currently holds.
func promisesFor(n *ackNotifier, sid pgtype.UUID) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.outstanding[util.UUIDToString(sid)]
	if s == nil {
		return 0
	}
	return len(s.promises)
}

func TestAckNotifier_CoalescesBurstThenReacksAfterWindow(t *testing.T) {
	base := time.Unix(1700000000, 0)
	cur := base
	n, sent := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(1)
	ctx := context.Background()

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	if len(*sent) != 1 {
		t.Fatalf("a burst within the window must coalesce to one ack, got %d", len(*sent))
	}
	if (*sent)[0] != ackProcessingText {
		t.Errorf("ack text = %q, want %q", (*sent)[0], ackProcessingText)
	}

	cur = base.Add(6 * time.Second)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	if len(*sent) != 2 {
		t.Fatalf("a message after the window must re-ack, got %d", len(*sent))
	}
}

func TestAckNotifier_SettleResetsDedup(t *testing.T) {
	cur := time.Unix(1700000000, 0)
	n, sent := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(2)
	ctx := context.Background()

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	n.OnSettled(ctx, sid)
	// Even within the window, a settled session acks its next turn immediately.
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	if len(*sent) != 2 {
		t.Fatalf("OnSettled must reset dedup so the next turn re-acks, got %d", len(*sent))
	}
}

func TestAckNotifier_DistinctSessionsAckIndependently(t *testing.T) {
	cur := time.Unix(1700000000, 0)
	n, sent := newTestAck(func() time.Time { return cur })
	ctx := context.Background()

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sessionUUID(3))
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sessionUUID(4))
	if len(*sent) != 2 {
		t.Fatalf("distinct sessions must each ack, got %d", len(*sent))
	}
}

// A bulk cancel is broadcast once per task row, so several cancels reach one
// conversation at once. Exactly one of them may come away with something to say,
// whatever the room was holding and however the calls interleave — the caller has
// already established that no turn is in flight, so one message closes the room
// out and a second would be a duplicate.
func TestAckNotifier_ConcurrentIdleRoomTakesSpeakOnce(t *testing.T) {
	for _, rounds := range []int{0, 1, 2, 8} {
		t.Run(fmt.Sprintf("%d promises", rounds), func(t *testing.T) {
			cur := time.Unix(1700000000, 0)
			n, _ := newTestAck(func() time.Time { return cur })
			sid := sessionUUID(5)
			ctx := context.Background()

			for i := 0; i < rounds; i++ {
				// Step past the coalesce window each time so every turn acks.
				cur = cur.Add(2 * ackCoalesceWindow)
				n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
			}

			idleAsOf := cur
			var taken int32
			var wg sync.WaitGroup
			for i := 0; i < 2*rounds+2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, ok := n.takeAckForIdleRoom(sid, idleAsOf); ok {
						atomic.AddInt32(&taken, 1)
					}
				}()
			}
			wg.Wait()

			want := int32(1)
			if rounds == 0 {
				want = 0
			}
			if taken != want {
				t.Fatalf("%d of the concurrent cancels had a notice to post, want %d: "+
					"each one past the first is a duplicate in the same conversation", taken, want)
			}
			if n.hasOutstandingAck(sid) {
				t.Error("the room reads as still owed a reply after every promise was taken")
			}
		})
	}
}

// A promise whose run stops reporting endings is the state the queue cannot see
// its way out of on its own: it is indistinguishable from a promise whose run is
// still working, and it sits at the head of the room's queue. Reconciling
// against the database is what clears it — every promise old enough to have had
// a task row goes with the one being withdrawn, so the room does not stay a
// promise heavy for the rest of the day.
func TestAckNotifier_IdleRoomTakeClearsPromisesNothingWillAnswer(t *testing.T) {
	base := time.Unix(1700000000, 0)
	cur := base
	n, _ := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(9)
	ctx := context.Background()

	// Three rounds acked, each past the coalesce window, none of them ever
	// answered: the agent went away without reporting a single ending.
	for i := 0; i < 3; i++ {
		cur = cur.Add(2 * ackCoalesceWindow)
		n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	}

	if _, ok := n.takeAckForIdleRoom(sid, cur); !ok {
		t.Fatal("the room is holding three unanswered promises and had nothing to withdraw")
	}
	if got := promisesFor(n, sid); got != 0 {
		t.Fatalf("the room still holds %d promises nothing will ever answer; every later "+
			"cancel there reads as one reply short and stays silent", got)
	}
	if n.hasOutstandingAck(sid) {
		t.Error("hasOutstandingAck still reports a reply owed, so every cancel in this " +
			"session keeps paying for queries that find nothing")
	}
}

// The other side of that rule: a turn ingested while the cancel was reading the
// database has no task row for that read to have seen, so "no turn in flight"
// says nothing about it. Closing it out would write off a reply that is on its
// way, and leave that turn's own cancellation with nothing to withdraw.
func TestAckNotifier_IdleRoomTakeKeepsAPromiseTheReadCouldNotHaveSeen(t *testing.T) {
	base := time.Unix(1700000000, 0)
	cur := base
	n, _ := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(10)
	ctx := context.Background()

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)

	// The cancel reads the database here, and the user's next turn acks while
	// that read is in flight.
	idleAsOf := cur
	cur = base.Add(2 * ackCoalesceWindow)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)

	if _, ok := n.takeAckForIdleRoom(sid, idleAsOf); !ok {
		t.Fatal("setup: the room holds two promises and had nothing to withdraw")
	}
	if got := promisesFor(n, sid); got != 1 {
		t.Fatalf("the room holds %d promises, want 1: the turn ingested after the read "+
			"cannot be read as dead by it", got)
	}

	// And that later turn is not this cancel's to withdraw either, so a second
	// cancelled row from the same bulk action still says nothing.
	if _, ok := n.takeAckForIdleRoom(sid, idleAsOf); ok {
		t.Error("a second cancelled row withdrew a promise made after it read the database")
	}
}

// The engine posts the ack on a detached goroutine, so a run can end while its
// own "On it" is still being sent. The ending has to be charged against that
// send: recorded blind, the promise lands with its round already over and stands
// at the head of the room's queue, one entry ahead of every promise after it.
func TestAckNotifier_EndingDuringTheAckSendLeavesNoPromiseBehind(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ending  func(n *ackNotifier, sid pgtype.UUID)
		sendErr error
	}{
		{"reply delivered", func(n *ackNotifier, sid pgtype.UUID) { n.releaseOutstandingAck(sid) }, nil},
		{"turn settled with no run", func(n *ackNotifier, sid pgtype.UUID) { n.OnSettled(context.Background(), sid) }, nil},
		{"cancelled", func(n *ackNotifier, sid pgtype.UUID) {
			n.takeAckForIdleRoom(sid, time.Unix(1700000000, 0))
		}, nil},
		{"send failed too", func(n *ackNotifier, sid pgtype.UUID) { n.releaseOutstandingAck(sid) }, errors.New("robot api down")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cur := time.Unix(1700000000, 0)
			n, _ := newTestAck(func() time.Time { return cur })
			sid := sessionUUID(11)
			ctx := context.Background()

			released := make(chan struct{})
			entered := make(chan struct{})
			n.sendText = func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string) error {
				close(entered)
				<-released
				return tc.sendErr
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
			}()

			<-entered
			if !n.hasOutstandingAck(sid) {
				t.Error("a cancel arriving now walks away without charging the ack in flight")
			}
			tc.ending(n, sid)
			close(released)
			<-done

			if got := promisesFor(n, sid); got != 0 {
				t.Fatalf("the round ended while its ack was in the air and left %d promise(s) "+
					"behind; the next cancel in that room takes one of them and says nothing", got)
			}
			if n.hasOutstandingAck(sid) {
				t.Error("the room reads as owed a reply for a round that is already over")
			}
			n.mu.Lock()
			_, present := n.outstanding[util.UUIDToString(sid)]
			n.mu.Unlock()
			if present {
				t.Error("a session owing nothing kept its map entry")
			}
		})
	}
}

// Two ingests in flight and one ending between them: the charge answers one ack
// and the other still records its promise. Getting this wrong in either
// direction is a promise too many or a promise too few.
func TestAckNotifier_OneEndingChargesOneOfTwoAcksInFlight(t *testing.T) {
	cur := time.Unix(1700000000, 0)
	n, _ := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(12)

	n.beginSend(util.UUIDToString(sid))
	n.beginSend(util.UUIDToString(sid))
	n.releaseOutstandingAck(sid)

	n.finishSend(util.UUIDToString(sid), ackPromise{}, true)
	if got := promisesFor(n, sid); got != 0 {
		t.Fatalf("the first ack to land held %d promises; the ending that arrived while "+
			"both were in flight had to answer one of them", got)
	}
	n.finishSend(util.UUIDToString(sid), ackPromise{}, true)
	if got := promisesFor(n, sid); got != 1 {
		t.Fatalf("the second ack recorded %d promises, want 1: only one ending arrived", got)
	}
}

// A charge left by an ending must not outlive every ack it could answer. The
// send that fails posted nothing, so it hands the charge on — and when it was
// the last one in flight, drops it, or the session's next turn would record no
// promise at all.
func TestAckNotifier_AFailedAckDoesNotLeaveTheChargeBehind(t *testing.T) {
	cur := time.Unix(1700000000, 0)
	n, _ := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(13)
	ctx := context.Background()
	key := util.UUIDToString(sid)

	n.beginSend(key)
	n.releaseOutstandingAck(sid)
	n.finishSend(key, ackPromise{}, false)

	if n.hasOutstandingAck(sid) {
		t.Fatal("an ack that never posted still reads as a promise the room is holding")
	}
	cur = cur.Add(2 * ackCoalesceWindow)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	if got := promisesFor(n, sid); got != 1 {
		t.Fatalf("the session's next turn recorded %d promises, want 1: the charge from a "+
			"round that never acked was still standing", got)
	}
}

// More endings can arrive than there are acks in flight to answer, and one of
// those acks can then fail to post. What is left over must be dropped, not
// carried: a charge that outlives every ack it could answer is spent by the
// session's next turn, which posts an "👀 On it" and records no promise for it.
func TestAckNotifier_ChargesNeverOutliveTheAcksTheyCouldAnswer(t *testing.T) {
	cur := time.Unix(1700000000, 0)
	n, _ := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(14)
	key := util.UUIDToString(sid)

	n.beginSend(key)
	n.beginSend(key)
	n.releaseOutstandingAck(sid)
	n.releaseOutstandingAck(sid)
	n.finishSend(key, ackPromise{}, false) // one of the two acks never posted

	n.beginSend(key) // the session's next turn is acking now
	if !n.hasOutstandingAck(sid) {
		t.Fatal("the room reads as owing nothing while an ack for it is in the air; a " +
			"cancel arriving now walks away and that ack stands unanswered")
	}
	n.finishSend(key, ackPromise{}, true)
	n.finishSend(key, ackPromise{}, true)
	if got := promisesFor(n, sid); got != 1 {
		t.Fatalf("two acks landed against one ending left to answer them and %d promises "+
			"were recorded, want 1", got)
	}
}

// The sweep bounds the map for runs that never report an ending. It has to bound
// it per promise: a session acked yesterday and again a minute ago is still owed
// the reply it was promised a minute ago, and dropping the whole session would
// leave that round's cancellation with nothing to withdraw.
func TestAckNotifier_SweepDropsStalePromisesAndKeepsFreshOnes(t *testing.T) {
	base := time.Unix(1700000000, 0)
	cur := base
	n, _ := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(6)
	ctx := context.Background()

	// Two promises the session never got an ending for, half a day apart.
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	cur = base.Add(ackPromiseMaxAge / 2)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)

	// A third turn a day after the first: recording it sweeps. The first promise
	// is past the age, the second is not.
	cur = base.Add(ackPromiseMaxAge + time.Hour)
	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)

	if got := promisesFor(n, sid); got != 2 {
		t.Fatalf("the session holds %d promises, want 2: the day-old one swept, the "+
			"half-day-old one and the new one kept", got)
	}
}

// A session that is owed nothing must not keep an entry, or the map grows with
// every conversation the process has ever acked in rather than with the ones
// currently waiting.
func TestAckNotifier_SessionOwedNothingLeavesNoEntry(t *testing.T) {
	cur := time.Unix(1700000000, 0)
	n, _ := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(7)
	ctx := context.Background()

	n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	n.releaseOutstandingAck(sid)

	n.mu.Lock()
	_, present := n.outstanding[util.UUIDToString(sid)]
	n.mu.Unlock()
	if present {
		t.Fatal("a session with no promise left still holds a map entry")
	}
	if n.hasOutstandingAck(sid) {
		t.Error("hasOutstandingAck reports a reply owed after the last promise was discharged")
	}
}
