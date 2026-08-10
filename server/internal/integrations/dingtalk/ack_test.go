package dingtalk

import (
	"context"
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
	n := &ackNotifier{
		logger:      slog.Default(),
		window:      5 * time.Second,
		now:         now,
		lastAck:     map[string]time.Time{},
		outstanding: map[string][]ackPromise{},
		sendText: func(_ context.Context, _ engine.ResolvedInstallation, _ channel.InboundMessage, text string) error {
			sent = append(sent, text)
			return nil
		},
	}
	return n, &sent
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

// Whether a cancellation notice is posted turns on the room being empty after
// the promise is taken, so the take and the count have to be one step. Read
// apart, two cancels arriving together could both find the room empty and both
// post — the duplicate the take exists to prevent.
func TestAckNotifier_ConcurrentTakesLeaveExactlyOneRoomEmpty(t *testing.T) {
	cur := time.Unix(1700000000, 0)
	n, _ := newTestAck(func() time.Time { return cur })
	sid := sessionUUID(5)
	ctx := context.Background()

	const rounds = 8
	for i := 0; i < rounds; i++ {
		// Step past the coalesce window each time so every turn acks.
		cur = cur.Add(2 * ackCoalesceWindow)
		n.OnIngested(ctx, engine.ResolvedInstallation{}, channel.InboundMessage{}, sid)
	}

	var taken, emptied int32
	var wg sync.WaitGroup
	for i := 0; i < rounds*2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, remaining, ok := n.takeOutstandingAck(sid); ok {
				atomic.AddInt32(&taken, 1)
				if remaining == 0 {
					atomic.AddInt32(&emptied, 1)
				}
			}
		}()
	}
	wg.Wait()

	if taken != rounds {
		t.Errorf("%d promises were made and %d were taken", rounds, taken)
	}
	if emptied != 1 {
		t.Fatalf("%d cancels would each have posted a notice into one conversation; "+
			"exactly one may find the room empty", emptied)
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

	n.mu.Lock()
	got := len(n.outstanding[util.UUIDToString(sid)])
	n.mu.Unlock()
	if got != 2 {
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
