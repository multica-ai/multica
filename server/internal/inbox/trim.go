package inbox

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

// ActiveLimit is how many rows a recipient's inbox keeps in one workspace,
// newest first. Snoozed rows do not count. Older rows are archived, so every
// inbox read stays bounded however many notifications a recipient collects.
const ActiveLimit = 2000

const (
	// trimSlack lets an inbox grow this far past ActiveLimit before it is
	// trimmed back, so a busy recipient is trimmed (and their clients
	// refreshed) once per trimSlack notifications rather than on every one.
	trimSlack = 100
	// trimBatchSize bounds the rows one archive statement locks and writes.
	trimBatchSize = 500
	// trimMaxBatches bounds the work one recipient gets per round. A
	// recipient with more left over is trimmed further in the next round.
	trimMaxBatches = 20

	trimInterval    = 30 * time.Second
	trimRoundBudget = 25 * time.Second
)

// TrimQueries is the slice of db.Queries a Trimmer uses.
type TrimQueries interface {
	GetInboxActiveCutoff(ctx context.Context, arg db.GetInboxActiveCutoffParams) (pgtype.Timestamptz, error)
	ArchiveInboxOlderThan(ctx context.Context, arg db.ArchiveInboxOlderThanParams) (int64, error)
}

// Recipient identifies one inbox: a recipient in a workspace.
type Recipient struct {
	WorkspaceID   pgtype.UUID
	RecipientType string
	RecipientID   pgtype.UUID
}

// Trimmer keeps inboxes within ActiveLimit. Inboxes that may have outgrown it
// are marked (a new notification, or a list read that hit the limit) and
// trimmed in the background, so no notification write or inbox read waits on
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

// Subscribe marks the recipient of every new inbox row. A row written into a
// snooze is announced as inbox:snoozed and does not count, so it is skipped.
func (t *Trimmer) Subscribe(bus *events.Bus) {
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		if r, ok := recipientOfNewItem(e); ok {
			t.Mark(r)
		}
	})
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

// trim archives r's shown rows beyond its newest ActiveLimit once the inbox
// holds more than ActiveLimit+trimSlack. Rows sharing the boundary row's
// created_at stay, so an inbox may keep slightly more than ActiveLimit. done
// is false when rows may remain for a later round.
func (t *Trimmer) trim(ctx context.Context, r Recipient) (archived int64, done bool, err error) {
	if _, err := t.queries.GetInboxActiveCutoff(ctx, db.GetInboxActiveCutoffParams{
		WorkspaceID: r.WorkspaceID, RecipientType: r.RecipientType, RecipientID: r.RecipientID,
		RowOffset: ActiveLimit + trimSlack,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, true, nil
		}
		return 0, false, err
	}
	cutoff, err := t.queries.GetInboxActiveCutoff(ctx, db.GetInboxActiveCutoffParams{
		WorkspaceID: r.WorkspaceID, RecipientType: r.RecipientType, RecipientID: r.RecipientID,
		RowOffset: ActiveLimit - 1,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, true, nil
		}
		return 0, false, err
	}
	for range trimMaxBatches {
		n, err := t.queries.ArchiveInboxOlderThan(ctx, db.ArchiveInboxOlderThanParams{
			WorkspaceID: r.WorkspaceID, RecipientType: r.RecipientType, RecipientID: r.RecipientID,
			Cutoff: cutoff, BatchSize: trimBatchSize,
		})
		archived += n
		if err != nil {
			return archived, false, err
		}
		if n < trimBatchSize {
			return archived, true, nil
		}
	}
	return archived, false, nil
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
