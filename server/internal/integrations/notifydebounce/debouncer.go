package notifydebounce

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type inboxQueries interface {
	GetInboxItem(context.Context, pgtype.UUID) (db.InboxItem, error)
}

// Batch is one recipient's still-unread, still-active inbox items.
type Batch struct {
	WorkspaceID   string
	RecipientType string
	RecipientID   string
	Items         []db.InboxItem
}

// Channel receives one batch instead of subscribing to EventInboxNew itself.
type Channel interface {
	DeliverInboxBatch(context.Context, Batch)
}

type timer interface {
	Stop() bool
}

type clock interface {
	AfterFunc(time.Duration, func()) timer
}

type realClock struct{}

func (realClock) AfterFunc(delay time.Duration, run func()) timer {
	return time.AfterFunc(delay, run)
}

type batchKey struct {
	workspaceID   string
	recipientType string
	recipientID   string
}

type itemRef struct {
	id            string
	workspaceID   string
	recipientType string
	recipientID   string
}

type pendingBatch struct {
	timer timer
	items []itemRef
}

// Debouncer buffers inbox events in memory and re-checks their database rows
// immediately before channel delivery.
type Debouncer struct {
	queries  inboxQueries
	delay    time.Duration
	maxItems int
	timeout  time.Duration
	logger   *slog.Logger
	clock    clock
	spawn    func(func())

	mu       sync.Mutex
	closed   bool
	pending  map[batchKey]*pendingBatch
	channels []Channel
	inFlight sync.WaitGroup
}

// New creates a process-local delivery buffer.
func New(queries inboxQueries, config Config) *Debouncer {
	if config.MaxItems <= 0 {
		config.MaxItems = defaultMaxItemsPerRecipient
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultDeliveryTimeout
	}
	return &Debouncer{
		queries:  queries,
		delay:    config.Delay,
		maxItems: config.MaxItems,
		timeout:  config.Timeout,
		logger:   config.Logger,
		clock:    realClock{},
		spawn:    func(run func()) { go run() },
		pending:  make(map[batchKey]*pendingBatch),
	}
}

// AddChannel registers an out-of-app channel at process startup.
func (d *Debouncer) AddChannel(channel Channel) {
	if channel == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.channels = append(d.channels, channel)
}

// Register subscribes the shared buffer to new inbox items.
func (d *Debouncer) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventInboxNew, d.handleEvent)
}

func (d *Debouncer) handleEvent(event events.Event) {
	ref, ok := referenceFromEvent(event)
	if !ok {
		return
	}
	d.enqueue(ref)
}

func (d *Debouncer) enqueue(ref itemRef) {
	if d.delay <= 0 {
		d.launch(func() {
			d.processWithTimeout([]itemRef{ref})
		})
		return
	}

	key := batchKey{
		workspaceID:   ref.workspaceID,
		recipientType: ref.recipientType,
		recipientID:   ref.recipientID,
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	batch := d.pending[key]
	if batch == nil {
		batch = &pendingBatch{}
		d.pending[key] = batch
		batch.timer = d.clock.AfterFunc(d.delay, func() {
			d.fire(key, batch)
		})
	}
	for _, existing := range batch.items {
		if existing.id == ref.id {
			return
		}
	}
	if len(batch.items) >= d.maxItems {
		dropped := batch.items[0]
		batch.items = batch.items[1:]
		d.logger.Warn("notification delivery buffer full; dropping oldest item",
			"workspace_id", key.workspaceID,
			"recipient_id", key.recipientID,
			"dropped_item_id", dropped.id,
			"max_items", d.maxItems)
	}
	batch.items = append(batch.items, ref)
}

func (d *Debouncer) fire(key batchKey, batch *pendingBatch) {
	d.mu.Lock()
	if d.closed || d.pending[key] != batch {
		d.mu.Unlock()
		return
	}
	delete(d.pending, key)
	items := append([]itemRef(nil), batch.items...)
	d.inFlight.Add(1)
	spawn := d.spawn
	d.mu.Unlock()

	spawn(func() {
		defer d.inFlight.Done()
		d.processWithTimeout(items)
	})
}

func (d *Debouncer) launch(run func()) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.inFlight.Add(1)
	spawn := d.spawn
	d.mu.Unlock()
	spawn(func() {
		defer d.inFlight.Done()
		run()
	})
}

func (d *Debouncer) processWithTimeout(refs []itemRef) {
	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()
	d.process(ctx, refs)
}

func (d *Debouncer) process(ctx context.Context, refs []itemRef) {
	if len(refs) == 0 {
		return
	}
	items := make([]db.InboxItem, 0, len(refs))
	for _, ref := range refs {
		if ctx.Err() != nil {
			return
		}
		itemID, err := util.ParseUUID(ref.id)
		if err != nil || !itemID.Valid {
			continue
		}
		item, err := d.queries.GetInboxItem(ctx, itemID)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				d.logger.WarnContext(ctx, "notification delivery re-check failed",
					"workspace_id", ref.workspaceID,
					"recipient_id", ref.recipientID,
					"item_id", ref.id,
					"error", err)
			}
			continue
		}
		if item.Read ||
			item.Archived ||
			util.UUIDToString(item.WorkspaceID) != ref.workspaceID ||
			item.RecipientType != ref.recipientType ||
			util.UUIDToString(item.RecipientID) != ref.recipientID {
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return
	}

	d.mu.Lock()
	channels := append([]Channel(nil), d.channels...)
	d.mu.Unlock()
	batch := Batch{
		WorkspaceID:   refs[0].workspaceID,
		RecipientType: refs[0].recipientType,
		RecipientID:   refs[0].recipientID,
		Items:         items,
	}
	for _, channel := range channels {
		channel.DeliverInboxBatch(ctx, batch)
	}
}

// Close stops accepting events, flushes pending batches, and waits for active
// deliveries until ctx expires. It returns false when the deadline wins.
func (d *Debouncer) Close(ctx context.Context) bool {
	d.mu.Lock()
	d.closed = true
	pending := make([][]itemRef, 0, len(d.pending))
	for key, batch := range d.pending {
		batch.timer.Stop()
		pending = append(pending, append([]itemRef(nil), batch.items...))
		delete(d.pending, key)
	}
	d.mu.Unlock()

	for _, items := range pending {
		if ctx.Err() != nil {
			return false
		}
		d.process(ctx, items)
	}

	done := make(chan struct{})
	go func() {
		d.inFlight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

func referenceFromEvent(event events.Event) (itemRef, bool) {
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		return itemRef{}, false
	}
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return itemRef{}, false
	}
	ref := itemRef{
		id:            stringValue(item["id"]),
		workspaceID:   stringValue(item["workspace_id"]),
		recipientType: stringValue(item["recipient_type"]),
		recipientID:   stringValue(item["recipient_id"]),
	}
	if ref.workspaceID == "" {
		ref.workspaceID = event.WorkspaceID
	}
	if ref.id == "" || ref.workspaceID == "" || ref.recipientType == "" || ref.recipientID == "" {
		return itemRef{}, false
	}
	return ref, true
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case *string:
		if typed != nil {
			return *typed
		}
	}
	return ""
}
