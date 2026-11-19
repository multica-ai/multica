package inbox

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ActiveLimit is how many rows a recipient's inbox keeps in one workspace,
// newest first by when each arrived or came back (returned_at). Snoozed rows
// and rows in projects the recipient cannot see do not count. Older rows are
// archived, so every inbox read stays bounded however many notifications a
// recipient collects.
const ActiveLimit = 2000

const (
	// trimBatchSize bounds the rows one archive statement locks and writes.
	trimBatchSize = 500
	// trimMaxBatches bounds the work one recipient gets per round. A
	// recipient with more left over is trimmed further in the next round.
	trimMaxBatches = 20

	// trimInterval also coalesces a burst of notifications for one recipient
	// into one trim, and one inbox:batch-archived, per round.
	trimInterval    = 30 * time.Second
	trimRoundBudget = 25 * time.Second

	// backfillReruns are when, after start, Run gives snoozes written without
	// a return rank their rank (BackfillInboxSnoozeReturns): at once for
	// snoozes that predate the column, and again once a rolling deploy's
	// older servers, which still write them without one, are gone.
	backfillBudget = 2 * time.Minute
)

var backfillReruns = []time.Duration{0, 15 * time.Minute}

// TrimQueries is the slice of db.Queries a Trimmer uses.
type TrimQueries interface {
	InboxOverflows(ctx context.Context, arg db.InboxOverflowsParams) (bool, error)
	ArchiveInboxOverflow(ctx context.Context, arg db.ArchiveInboxOverflowParams) (int64, error)
	NormalizeInboxReturns(ctx context.Context, arg db.NormalizeInboxReturnsParams) (int64, error)
	InboxReturnsSettled(ctx context.Context, arg db.InboxReturnsSettledParams) (bool, error)
	BackfillInboxSnoozeReturns(ctx context.Context, arg db.BackfillInboxSnoozeReturnsParams) (int64, error)
	ListHiddenProjectIDsForUser(ctx context.Context, arg db.ListHiddenProjectIDsForUserParams) ([]pgtype.UUID, error)
}

// Recipient identifies one inbox: a recipient in a workspace.
type Recipient struct {
	WorkspaceID   pgtype.UUID
	RecipientType string
	RecipientID   pgtype.UUID
}

// Trimmer keeps inboxes within ActiveLimit. Inboxes that may have outgrown it
// are marked (see Subscribe, plus a list read that hit the limit) and trimmed
// in the background, so no notification write or inbox read waits on
// the archive.
type Trimmer struct {
	queries TrimQueries
	pub     Publisher

	mu      sync.Mutex
	pending map[Recipient]struct{}
}

// NewTrimmer returns a Trimmer that archives through queries and announces
// what it archived on pub.
func NewTrimmer(queries TrimQueries, pub Publisher) *Trimmer {
	return &Trimmer{queries: queries, pub: pub, pending: map[Recipient]struct{}{}}
}

// Mark queues r for the next round. Safe on a nil Trimmer.
func (t *Trimmer) Mark(r Recipient) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.pending[r] = struct{}{}
	t.mu.Unlock()
}

// Subscribe marks the inboxes that may have grown: a new row, a row restored
// from the archive, or an issue taken out of a snooze early. A row written
// into a snooze is announced as inbox:snoozed and does not count yet.
func (t *Trimmer) Subscribe(bus *events.Bus) {
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		if r, ok := recipientOfNewItem(e); ok {
			t.Mark(r)
		}
	})
	for _, eventType := range []string{protocol.EventInboxUnarchived, protocol.EventInboxUnsnoozed} {
		bus.Subscribe(eventType, func(e events.Event) {
			if r, ok := memberRecipient(e); ok {
				t.Mark(r)
			}
		})
	}
}

// memberRecipient reads the member a personal inbox event names at the top
// of its payload.
func memberRecipient(e events.Event) (Recipient, bool) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return Recipient{}, false
	}
	recipientID, _ := payload["recipient_id"].(string)
	ws, err := util.ParseUUID(e.WorkspaceID)
	if err != nil {
		return Recipient{}, false
	}
	rid, err := util.ParseUUID(recipientID)
	if err != nil {
		return Recipient{}, false
	}
	return Recipient{WorkspaceID: ws, RecipientType: "member", RecipientID: rid}, true
}

func recipientOfNewItem(e events.Event) (Recipient, bool) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return Recipient{}, false
	}
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return Recipient{}, false
	}
	workspaceID, _ := item["workspace_id"].(string)
	recipientType, _ := item["recipient_type"].(string)
	recipientID, _ := item["recipient_id"].(string)
	ws, err := util.ParseUUID(workspaceID)
	if err != nil || recipientType == "" {
		return Recipient{}, false
	}
	rid, err := util.ParseUUID(recipientID)
	if err != nil {
		return Recipient{}, false
	}
	return Recipient{WorkspaceID: ws, RecipientType: recipientType, RecipientID: rid}, true
}

// Run trims the marked inboxes every trimInterval until ctx ends.
func (t *Trimmer) Run(ctx context.Context) {
	go t.runBackfill(ctx)
	ticker := time.NewTicker(trimInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			roundCtx, cancel := context.WithTimeout(ctx, trimRoundBudget)
			t.Flush(roundCtx)
			cancel()
		}
	}
}

func (t *Trimmer) runBackfill(ctx context.Context) {
	start := time.Now()
	for _, at := range backfillReruns {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(start.Add(at))):
		}
		runCtx, cancel := context.WithTimeout(ctx, backfillBudget)
		n, err := t.Backfill(runCtx, pgtype.UUID{})
		cancel()
		if err != nil {
			slog.Warn("inbox snooze return backfill failed", "count", n, "error", err)
		} else if n > 0 {
			slog.Info("inbox snooze return backfill ranked snoozed issues", "count", n)
		}
	}
}

// Backfill gives every snoozed issue written without a return rank its rank,
// in batches that skip rows another write holds. workspaceID narrows it (for
// tests); the zero value covers every workspace.
func (t *Trimmer) Backfill(ctx context.Context, workspaceID pgtype.UUID) (int64, error) {
	var total int64
	for {
		n, err := t.queries.BackfillInboxSnoozeReturns(ctx, db.BackfillInboxSnoozeReturnsParams{
			WorkspaceID: workspaceID, BatchSize: trimBatchSize,
		})
		total += n
		if err != nil || n < trimBatchSize {
			return total, err
		}
	}
}

// Flush trims every inbox marked so far. An inbox that could not be finished
// (an error, the round's deadline, or more rows than one round archives) is
// marked again for the next round.
func (t *Trimmer) Flush(ctx context.Context) {
	t.mu.Lock()
	batch := t.pending
	t.pending = make(map[Recipient]struct{}, len(batch))
	t.mu.Unlock()

	for r := range batch {
		if ctx.Err() != nil {
			t.Mark(r)
			continue
		}
		archived, done, err := t.trim(ctx, r)
		if err != nil {
			slog.Warn("inbox trim failed",
				"workspace_id", util.UUIDToString(r.WorkspaceID),
				"recipient_type", r.RecipientType,
				"recipient_id", util.UUIDToString(r.RecipientID),
				"error", err)
		}
		if !done {
			t.Mark(r)
		}
		if archived > 0 {
			slog.Info("inbox trim archived rows beyond the inbox limit",
				"workspace_id", util.UUIDToString(r.WorkspaceID),
				"recipient_type", r.RecipientType,
				"recipient_id", util.UUIDToString(r.RecipientID),
				"count", archived)
			t.announce(r, archived)
		}
	}
}

// trim archives r's shown rows beyond its newest ActiveLimit. done is false
// while rows beyond the limit remain: rows another write held locked, or more
// than one round archives.
func (t *Trimmer) trim(ctx context.Context, r Recipient) (archived int64, done bool, err error) {
	hidden, err := t.hiddenProjects(ctx, r)
	if err != nil {
		return 0, false, err
	}
	overflows := func() (bool, error) {
		return t.queries.InboxOverflows(ctx, db.InboxOverflowsParams{
			WorkspaceID: r.WorkspaceID, RecipientType: r.RecipientType, RecipientID: r.RecipientID,
			HiddenProjectIds: hidden, Keep: ActiveLimit,
		})
	}
	over, err := overflows()
	if err != nil || !over {
		return 0, err == nil, err
	}
	// Settle which row ranks each issue by its return before ranking decides
	// what to archive (see NormalizeInboxReturns). Until every rank is
	// settled, rows another write holds included, archive nothing: an
	// unsettled duplicate rank would keep a row that is not the issue's and
	// push a real one out. The inbox stays queued for the next round.
	settled, err := t.normalize(ctx, r)
	if err != nil || !settled {
		return 0, false, err
	}
	for range trimMaxBatches {
		n, err := t.queries.ArchiveInboxOverflow(ctx, db.ArchiveInboxOverflowParams{
			WorkspaceID: r.WorkspaceID, RecipientType: r.RecipientType, RecipientID: r.RecipientID,
			HiddenProjectIds: hidden, Keep: ActiveLimit, BatchSize: trimBatchSize,
		})
		archived += n
		if err != nil {
			return archived, false, err
		}
		if n < trimBatchSize {
			break
		}
	}
	over, err = overflows()
	if err != nil {
		return archived, false, err
	}
	return archived, !over, nil
}

// normalize runs NormalizeInboxReturns until it returns a partial batch, at
// most trimMaxBatches times, then reports whether anything is left: a
// partial batch can also mean rows it skipped because another write held them.
func (t *Trimmer) normalize(ctx context.Context, r Recipient) (settled bool, err error) {
	for range trimMaxBatches {
		n, err := t.queries.NormalizeInboxReturns(ctx, db.NormalizeInboxReturnsParams{
			WorkspaceID: r.WorkspaceID, RecipientType: r.RecipientType, RecipientID: r.RecipientID,
			BatchSize: trimBatchSize,
		})
		if err != nil {
			return false, err
		}
		if n < trimBatchSize {
			break
		}
	}
	return t.queries.InboxReturnsSettled(ctx, db.InboxReturnsSettledParams{
		WorkspaceID: r.WorkspaceID, RecipientType: r.RecipientType, RecipientID: r.RecipientID,
	})
}

// hiddenProjects returns the private projects a member recipient cannot see,
// so the limit counts the rows their own inbox list shows (ListInboxItems
// filters the same projects). Nil when there are none, or for an agent.
func (t *Trimmer) hiddenProjects(ctx context.Context, r Recipient) ([]pgtype.UUID, error) {
	if r.RecipientType != "member" {
		return nil, nil
	}
	ids, err := t.queries.ListHiddenProjectIDsForUser(ctx, db.ListHiddenProjectIDsForUserParams{
		WorkspaceID: r.WorkspaceID, UserID: r.RecipientID,
	})
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return ids, nil
}

// announce tells a member's clients to drop the archived rows, as the bulk
// archive actions do. Only members have a personal feed.
func (t *Trimmer) announce(r Recipient, count int64) {
	if r.RecipientType != "member" {
		return
	}
	publish(t.pub, events.Event{
		Type:        protocol.EventInboxBatchArchived,
		WorkspaceID: util.UUIDToString(r.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"recipient_id": util.UUIDToString(r.RecipientID),
			"count":        count,
			"reason":       "inbox_limit",
		},
	})
}
