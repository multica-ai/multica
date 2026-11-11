package notifydebounce

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	testWorkspaceID = "00000000-0000-4000-8000-000000000101"
	testRecipientID = "00000000-0000-4000-8000-000000000102"
	testItemOneID   = "00000000-0000-4000-8000-000000000103"
	testItemTwoID   = "00000000-0000-4000-8000-000000000104"
	testItemThreeID = "00000000-0000-4000-8000-000000000105"
)

type fakeQueries struct {
	mu     sync.Mutex
	items  map[string]db.InboxItem
	errors map[string]error
}

func (f *fakeQueries) GetInboxItem(_ context.Context, id pgtype.UUID) (db.InboxItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := util.UUIDToString(id)
	if err := f.errors[key]; err != nil {
		return db.InboxItem{}, err
	}
	item, ok := f.items[key]
	if !ok {
		return db.InboxItem{}, pgx.ErrNoRows
	}
	return item, nil
}

func (f *fakeQueries) setRead(id string, read bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item := f.items[id]
	item.Read = read
	f.items[id] = item
}

type fakeTimer struct {
	mu      sync.Mutex
	stopped bool
	run     func()
}

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}

func (t *fakeTimer) fire() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true
	run := t.run
	t.mu.Unlock()
	run()
}

type fakeClock struct {
	mu     sync.Mutex
	timers []*fakeTimer
}

func (c *fakeClock) AfterFunc(_ time.Duration, run func()) timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &fakeTimer{run: run}
	c.timers = append(c.timers, timer)
	return timer
}

func (c *fakeClock) fireAll() {
	c.mu.Lock()
	timers := append([]*fakeTimer(nil), c.timers...)
	c.mu.Unlock()
	for _, timer := range timers {
		timer.fire()
	}
}

type recordingChannel struct {
	mu      sync.Mutex
	batches []Batch
}

func (c *recordingChannel) DeliverInboxBatch(_ context.Context, batch Batch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	batch.Items = append([]db.InboxItem(nil), batch.Items...)
	c.batches = append(c.batches, batch)
}

func (c *recordingChannel) snapshot() []Batch {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Batch(nil), c.batches...)
}

func testItem(id string) db.InboxItem {
	return db.InboxItem{
		ID:            util.MustParseUUID(id),
		WorkspaceID:   util.MustParseUUID(testWorkspaceID),
		RecipientType: "member",
		RecipientID:   util.MustParseUUID(testRecipientID),
		Title:         id,
	}
}

func testEvent(id string) events.Event {
	return events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: testWorkspaceID,
		Payload: map[string]any{"item": map[string]any{
			"id":             id,
			"workspace_id":   testWorkspaceID,
			"recipient_type": "member",
			"recipient_id":   testRecipientID,
		}},
	}
}

func newTestDebouncer(delay time.Duration, maxItems int, queries *fakeQueries, channel *recordingChannel) (*Debouncer, *fakeClock) {
	clock := &fakeClock{}
	debouncer := New(queries, Config{
		Delay:    delay,
		MaxItems: maxItems,
		Logger:   slog.Default(),
	})
	debouncer.clock = clock
	debouncer.spawn = func(run func()) { run() }
	debouncer.AddChannel(channel)
	return debouncer, clock
}

func TestDebouncerBatchesItemsForOneRecipient(t *testing.T) {
	t.Parallel()

	queries := &fakeQueries{items: map[string]db.InboxItem{
		testItemOneID: testItem(testItemOneID),
		testItemTwoID: testItem(testItemTwoID),
	}}
	channel := &recordingChannel{}
	debouncer, clock := newTestDebouncer(time.Minute, 10, queries, channel)
	bus := events.New()
	debouncer.Register(bus)

	bus.Publish(testEvent(testItemOneID))
	bus.Publish(testEvent(testItemTwoID))
	if got := channel.snapshot(); len(got) != 0 {
		t.Fatalf("delivered before timer: %#v", got)
	}

	clock.fireAll()
	batches := channel.snapshot()
	if len(batches) != 1 || len(batches[0].Items) != 2 {
		t.Fatalf("batches = %#v", batches)
	}
	if got := util.UUIDToString(batches[0].Items[0].ID); got != testItemOneID {
		t.Fatalf("first item = %s", got)
	}
	if got := util.UUIDToString(batches[0].Items[1].ID); got != testItemTwoID {
		t.Fatalf("second item = %s", got)
	}
}

func TestDebouncerDropsItemsReadArchivedOrDeletedBeforeFlush(t *testing.T) {
	t.Parallel()

	archived := testItem(testItemTwoID)
	archived.Archived = true
	queries := &fakeQueries{
		items: map[string]db.InboxItem{
			testItemOneID: testItem(testItemOneID),
			testItemTwoID: archived,
		},
		errors: map[string]error{
			testItemThreeID: pgx.ErrNoRows,
		},
	}
	channel := &recordingChannel{}
	debouncer, clock := newTestDebouncer(time.Minute, 10, queries, channel)
	bus := events.New()
	debouncer.Register(bus)

	bus.Publish(testEvent(testItemOneID))
	bus.Publish(testEvent(testItemTwoID))
	bus.Publish(testEvent(testItemThreeID))
	queries.setRead(testItemOneID, true)
	clock.fireAll()

	if got := channel.snapshot(); len(got) != 0 {
		t.Fatalf("delivered stale items: %#v", got)
	}
}

func TestDebouncerCapsEachRecipientAndDropsOldest(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	queries := &fakeQueries{items: map[string]db.InboxItem{
		testItemOneID:   testItem(testItemOneID),
		testItemTwoID:   testItem(testItemTwoID),
		testItemThreeID: testItem(testItemThreeID),
	}}
	channel := &recordingChannel{}
	clock := &fakeClock{}
	debouncer := New(queries, Config{
		Delay:    time.Minute,
		MaxItems: 2,
		Logger:   logger,
	})
	debouncer.clock = clock
	debouncer.spawn = func(run func()) { run() }
	debouncer.AddChannel(channel)
	bus := events.New()
	debouncer.Register(bus)

	bus.Publish(testEvent(testItemOneID))
	bus.Publish(testEvent(testItemTwoID))
	bus.Publish(testEvent(testItemThreeID))
	clock.fireAll()

	batches := channel.snapshot()
	if len(batches) != 1 || len(batches[0].Items) != 2 {
		t.Fatalf("batches = %#v", batches)
	}
	if got := util.UUIDToString(batches[0].Items[0].ID); got != testItemTwoID {
		t.Fatalf("oldest retained item = %s", got)
	}
	if !strings.Contains(logs.String(), "dropping oldest item") ||
		!strings.Contains(logs.String(), testItemOneID) {
		t.Fatalf("drop was not logged: %s", logs.String())
	}
}

func TestDebouncerZeroDelayPassesThroughImmediately(t *testing.T) {
	t.Parallel()

	queries := &fakeQueries{items: map[string]db.InboxItem{
		testItemOneID: testItem(testItemOneID),
	}}
	channel := &recordingChannel{}
	debouncer, clock := newTestDebouncer(0, 10, queries, channel)
	bus := events.New()
	debouncer.Register(bus)

	bus.Publish(testEvent(testItemOneID))

	batches := channel.snapshot()
	if len(batches) != 1 || len(batches[0].Items) != 1 {
		t.Fatalf("batches = %#v", batches)
	}
	if len(clock.timers) != 0 {
		t.Fatalf("zero-delay delivery created timers")
	}
}

func TestDebouncerCloseFlushesPendingItems(t *testing.T) {
	t.Parallel()

	queries := &fakeQueries{items: map[string]db.InboxItem{
		testItemOneID: testItem(testItemOneID),
	}}
	channel := &recordingChannel{}
	debouncer, clock := newTestDebouncer(time.Hour, 10, queries, channel)
	bus := events.New()
	debouncer.Register(bus)
	bus.Publish(testEvent(testItemOneID))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !debouncer.Close(ctx) {
		t.Fatal("Close timed out")
	}
	batches := channel.snapshot()
	if len(batches) != 1 || len(batches[0].Items) != 1 {
		t.Fatalf("batches = %#v", batches)
	}
	clock.fireAll()
	if len(channel.snapshot()) != 1 {
		t.Fatal("stopped timer delivered the batch twice")
	}
}

func TestDebouncerLogsTransientReadErrors(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	queries := &fakeQueries{
		items: map[string]db.InboxItem{},
		errors: map[string]error{
			testItemOneID: errors.New("database details"),
		},
	}
	channel := &recordingChannel{}
	debouncer := New(queries, Config{Logger: logger})
	debouncer.spawn = func(run func()) { run() }
	debouncer.AddChannel(channel)
	bus := events.New()
	debouncer.Register(bus)

	bus.Publish(testEvent(testItemOneID))

	if !strings.Contains(logs.String(), "database details") {
		t.Fatalf("log omitted query error details: %s", logs.String())
	}
}
