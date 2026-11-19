package inbox

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeTrimQueries models an inbox of `rows` shown rows, newest first, where
// every row strictly older than the cutoff can be archived.
type fakeTrimQueries struct {
	rows       int
	cutoffs    []int32
	archiveRun int
}

func (f *fakeTrimQueries) GetInboxActiveCutoff(_ context.Context, arg db.GetInboxActiveCutoffParams) (pgtype.Timestamptz, error) {
	f.cutoffs = append(f.cutoffs, arg.RowOffset)
	if int(arg.RowOffset) >= f.rows {
		return pgtype.Timestamptz{}, pgx.ErrNoRows
	}
	return pgtype.Timestamptz{Time: time.Unix(int64(f.rows-int(arg.RowOffset)), 0), Valid: true}, nil
}

func (f *fakeTrimQueries) ArchiveInboxOlderThan(_ context.Context, arg db.ArchiveInboxOlderThanParams) (int64, error) {
	f.archiveRun++
	n := min(f.rows-ActiveLimit, int(arg.BatchSize))
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

// An inbox within the slack is left alone, so a busy inbox is not trimmed and
// refreshed on every notification.
func TestTrimmerLeavesInboxWithinSlack(t *testing.T) {
	q := &fakeTrimQueries{rows: ActiveLimit + trimSlack}
	bus := events.New()
	got := batchArchivedEvents(bus)
	tr := NewTrimmer(q, bus)

	tr.Mark(trimRecipient)
	tr.Flush(context.Background())

	if q.rows != ActiveLimit+trimSlack || q.archiveRun != 0 || len(*got) != 0 {
		t.Fatalf("rows=%d archive runs=%d events=%d; want the inbox untouched", q.rows, q.archiveRun, len(*got))
	}
	if len(tr.pending) != 0 {
		t.Fatalf("pending = %v, want the inbox settled", tr.pending)
	}
}

func TestTrimmerArchivesBackToLimitAndAnnounces(t *testing.T) {
	q := &fakeTrimQueries{rows: ActiveLimit + trimSlack + 1}
	bus := events.New()
	got := batchArchivedEvents(bus)
	tr := NewTrimmer(q, bus)

	tr.Mark(trimRecipient)
	tr.Flush(context.Background())

	if q.rows != ActiveLimit {
		t.Fatalf("rows = %d, want %d", q.rows, ActiveLimit)
	}
	if len(q.cutoffs) != 2 || q.cutoffs[0] != ActiveLimit+trimSlack || q.cutoffs[1] != ActiveLimit-1 {
		t.Fatalf("cutoff offsets = %v, want trigger then boundary", q.cutoffs)
	}
	if len(*got) != 1 {
		t.Fatalf("batch-archived events = %d, want 1", len(*got))
	}
	payload := (*got)[0].Payload.(map[string]any)
	if payload["recipient_id"] != util.UUIDToString(trimRecipient.RecipientID) ||
		payload["count"] != int64(trimSlack+1) || payload["reason"] != "inbox_limit" ||
		(*got)[0].WorkspaceID != util.UUIDToString(trimRecipient.WorkspaceID) {
		t.Fatalf("event = %+v", (*got)[0])
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
	q := &fakeTrimQueries{rows: ActiveLimit + trimSlack + 5}
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

func TestTrimmerMarksRecipientOfNewItem(t *testing.T) {
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
}

func TestNilTrimmerMarkIsNoop(t *testing.T) {
	var tr *Trimmer
	tr.Mark(trimRecipient)
}
