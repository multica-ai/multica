package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

type hubFixture struct {
	store   *store.Store
	writer  *store.Writer
	hub     *Hub
	scopeID int64
}

func newHubFixture(t *testing.T, opts HubOptions) *hubFixture {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "conversations.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	scopeID, err := s.EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "u", WorkspaceID: "w"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateConversation(ctx, store.NewConversation{ID: "conv-1", ScopeID: scopeID, Provider: "claude", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	w := s.NewWriter(store.WriterOptions{})
	t.Cleanup(w.Close)
	return &hubFixture{store: s, writer: w, hub: NewHub(s, opts), scopeID: scopeID}
}

// commit writes n events and publishes them, as the batcher does.
func (f *hubFixture) commit(t *testing.T, n int, summary bool) store.Committed {
	t.Helper()
	b := store.Batch{ConversationID: "conv-1"}
	for range n {
		b.Events = append(b.Events, store.NewEvent{Type: "item.delta", ItemID: "it", Payload: json.RawMessage(`{"text":"x"}`), CreatedAt: time.Now()})
	}
	if summary {
		status := "running"
		b.Summary = &store.SummaryPatch{Status: &status, UpdatedAt: time.Now()}
	}
	committed, err := f.writer.Submit(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	f.hub.Publish("conv-1", committed)
	return committed
}

func collect(t *testing.T, sub *Subscription, through int64) []int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var seqs []int64
	for len(seqs) == 0 || seqs[len(seqs)-1] < through {
		events, err := sub.Next(ctx)
		if err != nil {
			t.Fatalf("Next after %v: %v", seqs, err)
		}
		for _, e := range events {
			seqs = append(seqs, e.Seq)
		}
	}
	return seqs
}

func assertContiguous(t *testing.T, seqs []int64, from, through int64) {
	t.Helper()
	if int64(len(seqs)) != through-from+1 {
		t.Fatalf("got %d events %v, want %d..%d", len(seqs), seqs, from, through)
	}
	for i, s := range seqs {
		if s != from+int64(i) {
			t.Fatalf("events out of order or with gaps: %v", seqs)
		}
	}
}

func TestSubscribeCatchesUpThenGoesLive(t *testing.T) {
	t.Parallel()

	f := newHubFixture(t, HubOptions{PageSize: 3})
	f.commit(t, 10, false)
	sub, high, err := f.hub.Subscribe(context.Background(), "conv-1", 4)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if high != 10 {
		t.Fatalf("high water = %d", high)
	}
	f.commit(t, 2, false)
	assertContiguous(t, collect(t, sub, 12), 5, 12)
	if !sub.Live() {
		t.Fatal("subscription still catching up")
	}
}

func TestCommitsDuringCatchUpArriveOnceAndInOrder(t *testing.T) {
	t.Parallel()

	f := newHubFixture(t, HubOptions{PageSize: 2})
	f.commit(t, 6, false)
	sub, _, err := f.hub.Subscribe(context.Background(), "conv-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	// Commit and publish while catch-up is still paging; the live events are
	// held until the history before them has been delivered.
	f.commit(t, 1, false)
	f.commit(t, 1, false)
	assertContiguous(t, collect(t, sub, 8), 1, 8)
}

func TestSubscribeRejectsCursorAheadOfStore(t *testing.T) {
	t.Parallel()

	f := newHubFixture(t, HubOptions{})
	f.commit(t, 2, false)
	if _, _, err := f.hub.Subscribe(context.Background(), "conv-1", 9); !errors.Is(err, ErrSnapshotRequired) {
		t.Fatalf("Subscribe = %v, want ErrSnapshotRequired", err)
	}
	if _, _, err := f.hub.Subscribe(context.Background(), "missing", 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Subscribe(missing) = %v", err)
	}
}

func TestSlowSubscriberIsDroppedWithoutAffectingOthers(t *testing.T) {
	t.Parallel()

	f := newHubFixture(t, HubOptions{MaxBufferedBytes: 2000})
	slow, _, err := f.hub.Subscribe(context.Background(), "conv-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	fast, _, err := f.hub.Subscribe(context.Background(), "conv-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()
	waitLive(t, slow)
	waitLive(t, fast)

	var through int64
	for range 40 {
		through = f.commit(t, 1, false).Events[0].Seq
		if _, err := fast.Next(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := slow.Next(context.Background()); !errors.Is(err, ErrResubscribe) {
		t.Fatalf("slow subscriber = %v, want ErrResubscribe", err)
	}

	// Resubscribing from the last applied sequence recovers every event.
	again, _, err := f.hub.Subscribe(context.Background(), "conv-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	assertContiguous(t, collect(t, again, through), 1, through)
}

func TestScopeSubscriptionCatchesUpAndReceivesSummaries(t *testing.T) {
	t.Parallel()

	f := newHubFixture(t, HubOptions{})
	ctx := context.Background()
	_, before, err := f.store.SummariesAfter(ctx, f.scopeID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	sub, high, err := f.hub.SubscribeScope(ctx, f.scopeID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if high != before {
		t.Fatalf("scope high water = %d, want %d", high, before)
	}
	committed := f.commit(t, 1, true)

	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var got []string
	for len(got) < 2 {
		rows, err := sub.Next(deadline)
		if err != nil {
			t.Fatalf("Next: %v (got %v)", err, got)
		}
		for _, r := range rows {
			got = append(got, fmt.Sprintf("%s@%d:%s", r.ID, r.SummarySeq, r.Status))
		}
	}
	want := []string{fmt.Sprintf("conv-1@%d:idle", before), fmt.Sprintf("conv-1@%d:running", committed.ScopeSeq)}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("summaries = %v, want %v", got, want)
	}
}

func TestClosedSubscriptionStopsReceiving(t *testing.T) {
	t.Parallel()

	f := newHubFixture(t, HubOptions{})
	sub, _, err := f.hub.Subscribe(context.Background(), "conv-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub)
	sub.Close()
	f.commit(t, 1, false)
	if _, err := sub.Next(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Next after Close = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		f.hub.mu.Lock()
		n := len(f.hub.convs["conv-1"])
		f.hub.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed subscription still registered")
		}
		time.Sleep(time.Millisecond)
	}
}

func waitLive(t *testing.T, sub interface{ Live() bool }) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !sub.Live() {
		if time.Now().After(deadline) {
			t.Fatal("subscription never went live")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestScopeSubscriptionRecoversALatePublish(t *testing.T) {
	t.Parallel()

	f := newHubFixture(t, HubOptions{})
	ctx := context.Background()
	sub, _, err := f.hub.SubscribeScope(ctx, f.scopeID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := sub.Next(deadline); err != nil { // conv-1's catch-up
		t.Fatal(err)
	}
	for !sub.Live() {
		time.Sleep(time.Millisecond)
	}
	// conv-2 is created (scope seq N) but announced only after conv-1's
	// next commit (N+1) was published.
	if _, err := f.store.CreateConversation(ctx, store.NewConversation{ID: "conv-2", ScopeID: f.scopeID, Provider: "codex", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	f.commit(t, 1, true)
	conv2, _ := f.store.Summary(ctx, "conv-2")
	f.hub.Publish("conv-2", store.Committed{Summary: &conv2})

	seen := map[string]bool{}
	for !(seen["conv-1"] && seen["conv-2"]) {
		rows, err := sub.Next(deadline)
		if err != nil {
			t.Fatalf("Next: %v (seen %v)", err, seen)
		}
		for _, r := range rows {
			seen[r.ID] = true
		}
	}
}
