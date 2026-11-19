package inbox

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeTrimQueries models an inbox of `rows` shown rows of which `locked` of
// the overflow are held by another write and skipped.
type fakeTrimQueries struct {
	rows       int
	locked     int
	archiveRun int
	normalized int
	unsettled  int // rows NormalizeInboxReturns still has to fix
	held       int // unsettled rows another write holds, which it skips
}

func (f *fakeTrimQueries) InboxReturnsSettled(context.Context, db.InboxReturnsSettledParams) (bool, error) {
	return f.unsettled == 0 && f.held == 0, nil
}

func (f *fakeTrimQueries) BackfillInboxSnoozeReturns(context.Context, db.BackfillInboxSnoozeReturnsParams) (int64, error) {
	return 0, nil
}

func (f *fakeTrimQueries) InboxOverflows(_ context.Context, arg db.InboxOverflowsParams) (bool, error) {
	return f.rows > int(arg.Keep), nil
}

func (f *fakeTrimQueries) ListHiddenProjectIDsForUser(context.Context, db.ListHiddenProjectIDsForUserParams) ([]pgtype.UUID, error) {
	return nil, nil
}

func (f *fakeTrimQueries) NormalizeInboxReturns(_ context.Context, arg db.NormalizeInboxReturnsParams) (int64, error) {
	f.normalized++
	n := min(f.unsettled, int(arg.BatchSize))
	f.unsettled -= n
	return int64(n), nil
}

func (f *fakeTrimQueries) ArchiveInboxOverflow(_ context.Context, arg db.ArchiveInboxOverflowParams) (int64, error) {
	f.archiveRun++
	n := min(max(f.rows-int(arg.Keep)-f.locked, 0), int(arg.BatchSize))
	f.rows -= n
	return int64(n), nil
}

var trimRecipient = Recipient{
	WorkspaceID:   util.MustParseUUID("33333333-3333-3333-3333-333333333333"),
	RecipientType: "member",
	RecipientID:   util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
}

func batchArchivedEvents(bus *events.Bus) *[]events.Event {
	var got []events.Event
	bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) { got = append(got, e) })
	return &got
}

func TestTrimmerLeavesInboxAtTheLimit(t *testing.T) {
	q := &fakeTrimQueries{rows: ActiveLimit}
	bus := events.New()
	got := batchArchivedEvents(bus)
	tr := NewTrimmer(q, bus)

	tr.Mark(trimRecipient)
	tr.Flush(context.Background())

	if q.rows != ActiveLimit || q.archiveRun != 0 || q.normalized != 0 || len(*got) != 0 {
		t.Fatalf("rows=%d archive runs=%d normalized=%d events=%d; want the inbox untouched", q.rows, q.archiveRun, q.normalized, len(*got))
	}
	if len(tr.pending) != 0 {
		t.Fatalf("pending = %v, want the inbox settled", tr.pending)
	}
}

// One row past the limit is trimmed: the list shows only ActiveLimit rows, so
// an extra row would otherwise sit out of sight in neither list.
func TestTrimmerArchivesOneRowPastTheLimitAndAnnounces(t *testing.T) {
	q := &fakeTrimQueries{rows: ActiveLimit + 1}
	bus := events.New()
	got := batchArchivedEvents(bus)
	tr := NewTrimmer(q, bus)

	tr.Mark(trimRecipient)
	tr.Flush(context.Background())

	if q.rows != ActiveLimit || len(tr.pending) != 0 || q.normalized != 1 {
		t.Fatalf("rows=%d pending=%v normalized=%d; want returns settled once, trimmed to %d and settled", q.rows, tr.pending, q.normalized, ActiveLimit)
	}
	if len(*got) != 1 {
		t.Fatalf("batch-archived events = %d, want 1", len(*got))
	}
	payload := (*got)[0].Payload.(map[string]any)
	if payload["recipient_id"] != util.UUIDToString(trimRecipient.RecipientID) ||
		payload["count"] != int64(1) || payload["reason"] != "inbox_limit" ||
		(*got)[0].WorkspaceID != util.UUIDToString(trimRecipient.WorkspaceID) {
		t.Fatalf("event = %+v", (*got)[0])
	}
}

// Rows skipped because another write held them keep the inbox queued.
func TestTrimmerRequeuesInboxWithSkippedRows(t *testing.T) {
	q := &fakeTrimQueries{rows: ActiveLimit + 10, locked: 3}
	tr := NewTrimmer(q, nil)

	tr.Mark(trimRecipient)
	tr.Flush(context.Background())
	if q.rows != ActiveLimit+3 {
		t.Fatalf("rows = %d, want the unlocked overflow archived", q.rows)
	}
	if _, ok := tr.pending[trimRecipient]; !ok {
		t.Fatal("inbox with skipped rows was not queued for the next round")
	}

	q.locked = 0
	tr.Flush(context.Background())
	if q.rows != ActiveLimit || len(tr.pending) != 0 {
		t.Fatalf("rows=%d pending=%v after the lock cleared", q.rows, tr.pending)
	}
}

// One round archives at most trimMaxBatches batches per inbox; the rest waits
// for the next round instead of holding the round up.
func TestTrimmerCarriesLargeBacklogToNextRound(t *testing.T) {
	q := &fakeTrimQueries{rows: ActiveLimit + trimMaxBatches*trimBatchSize + 10}
	tr := NewTrimmer(q, nil)

	tr.Mark(trimRecipient)
	tr.Flush(context.Background())
	if q.rows != ActiveLimit+10 || q.archiveRun != trimMaxBatches {
		t.Fatalf("after one round rows=%d runs=%d", q.rows, q.archiveRun)
	}
	if _, ok := tr.pending[trimRecipient]; !ok {
		t.Fatal("unfinished inbox was not marked for the next round")
	}
}

func TestTrimmerDoesNotAnnounceForAgents(t *testing.T) {
	q := &fakeTrimQueries{rows: ActiveLimit + 5}
	bus := events.New()
	got := batchArchivedEvents(bus)
	tr := NewTrimmer(q, bus)

	agent := trimRecipient
	agent.RecipientType = "agent"
	tr.Mark(agent)
	tr.Flush(context.Background())

	if q.rows != ActiveLimit || len(*got) != 0 {
		t.Fatalf("rows=%d events=%d; want trimmed silently", q.rows, len(*got))
	}
}

func TestTrimmerMarksInboxesThatMayHaveGrown(t *testing.T) {
	bus := events.New()
	tr := NewTrimmer(&fakeTrimQueries{}, bus)
	tr.Subscribe(bus)

	item := db.InboxItem{
		ID:            util.MustParseUUID("44444444-4444-4444-4444-444444444444"),
		WorkspaceID:   trimRecipient.WorkspaceID,
		RecipientType: trimRecipient.RecipientType,
		RecipientID:   trimRecipient.RecipientID,
		Type:          "new_comment",
	}
	bus.Publish(newEvent(item, nil))
	bus.Publish(events.Event{Type: protocol.EventInboxNew, Payload: map[string]any{"item": "malformed"}})
	if len(tr.pending) != 1 {
		t.Fatalf("pending = %v, want only the new item's recipient", tr.pending)
	}
	if _, ok := tr.pending[trimRecipient]; !ok {
		t.Fatalf("pending = %v, want %v", tr.pending, trimRecipient)
	}

	for _, eventType := range []string{protocol.EventInboxUnarchived, protocol.EventInboxUnsnoozed} {
		tr.pending = map[Recipient]struct{}{}
		bus.Publish(events.Event{
			Type:        eventType,
			WorkspaceID: util.UUIDToString(trimRecipient.WorkspaceID),
			Payload:     map[string]any{"recipient_id": util.UUIDToString(trimRecipient.RecipientID)},
		})
		if _, ok := tr.pending[trimRecipient]; !ok || len(tr.pending) != 1 {
			t.Fatalf("%s: pending = %v, want %v", eventType, tr.pending, trimRecipient)
		}
	}
}

func TestNilTrimmerMarkIsNoop(t *testing.T) {
	var tr *Trimmer
	tr.Mark(trimRecipient)
}

// Return ranks left unsettled after a round's normalize budget keep the inbox
// queued, so the next round finishes them.
func TestTrimmerRequeuesInboxWithUnsettledReturns(t *testing.T) {
	q := &fakeTrimQueries{rows: ActiveLimit + 1, unsettled: trimMaxBatches*trimBatchSize + 1}
	tr := NewTrimmer(q, nil)

	tr.Mark(trimRecipient)
	tr.Flush(context.Background())
	if _, ok := tr.pending[trimRecipient]; !ok || q.normalized != trimMaxBatches {
		t.Fatalf("pending=%v normalize calls=%d; want queued after %d calls", tr.pending, q.normalized, trimMaxBatches)
	}

	// Ranks only matter while the inbox is over the limit.
	q.rows = ActiveLimit + 1
	tr.Flush(context.Background())
	if len(tr.pending) != 0 || q.unsettled != 0 {
		t.Fatalf("pending=%v unsettled=%d after the next round", tr.pending, q.unsettled)
	}
}

// Unsettled ranks normalize skipped because another write held them block
// this round's archive: a duplicate rank would keep the wrong row.
func TestTrimmerArchivesNothingWhileRanksAreHeld(t *testing.T) {
	q := &fakeTrimQueries{rows: ActiveLimit + 2, held: 1}
	tr := NewTrimmer(q, nil)

	tr.Mark(trimRecipient)
	tr.Flush(context.Background())
	if q.archiveRun != 0 || q.rows != ActiveLimit+2 {
		t.Fatalf("archive runs=%d rows=%d; want nothing archived while a rank is held", q.archiveRun, q.rows)
	}
	if _, ok := tr.pending[trimRecipient]; !ok {
		t.Fatal("inbox with a held rank was not queued for the next round")
	}

	q.held = 0
	tr.Flush(context.Background())
	if q.rows != ActiveLimit || len(tr.pending) != 0 {
		t.Fatalf("rows=%d pending=%v once the lock cleared", q.rows, tr.pending)
	}
}
