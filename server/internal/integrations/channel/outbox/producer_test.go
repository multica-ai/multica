package outbox

// producer_test.go — the enqueue side: the business-key contract that lets the
// realtime path and the reconciler both insert without coordinating, and the
// wake nudge that keeps the common case off the poll tick.

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// pgxUUIDZero is the invalid/NULL UUID a caller would leave a required id at.
func pgxUUIDZero() pgtype.UUID { return pgtype.UUID{} }

type fakeProducerStore struct {
	rows []db.EnqueueChannelOutboundParams
	err  error
}

func (s *fakeProducerStore) EnqueueChannelOutbound(_ context.Context, arg db.EnqueueChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	if s.err != nil {
		return db.ChannelOutboundQueue{}, s.err
	}
	s.rows = append(s.rows, arg)
	return db.ChannelOutboundQueue{}, nil
}

func validRequest(t *testing.T) Request {
	t.Helper()
	return Request{
		InstallationID: uuidFrom(t, testInstallationID),
		WorkspaceID:    uuidFrom(t, "33333333-3333-3333-3333-333333333333"),
		SourceKind:     "chat_done",
		SourceID:       "task-1",
		TargetChatID:   "CHAT_1",
		TargetChatType: 2,
		MsgType:        "markdown",
		Payload:        []byte(`{"content":"hi"}`),
	}
}

func TestProducer_EnqueueStampsChannelTypeAndRecords(t *testing.T) {
	t.Parallel()
	store := &fakeProducerStore{}
	m := newRecordingMetrics()
	p, err := NewProducer(testChannelType, store, nil, m)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}

	inserted, err := p.Enqueue(context.Background(), validRequest(t), EnqueuePathRealtime)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !inserted {
		t.Error("a fresh business key must report inserted=true")
	}
	if len(store.rows) != 1 {
		t.Fatalf("enqueued %d rows, want 1", len(store.rows))
	}
	// The producer owns channel_type, not the caller: that is what keeps a
	// channel from enqueueing under another's discriminator.
	if store.rows[0].ChannelType != testChannelType {
		t.Errorf("channel_type = %q, want %q", store.rows[0].ChannelType, testChannelType)
	}
	if m.enqueued != 1 {
		t.Errorf("enqueue observations = %d, want 1", m.enqueued)
	}
}

// ON CONFLICT DO NOTHING surfaces as pgx.ErrNoRows. That is the expected
// outcome when the realtime path and the reconciler race, so it must read as
// "already handled", not as an error.
func TestProducer_BusinessKeyConflictIsNotAnError(t *testing.T) {
	t.Parallel()
	store := &fakeProducerStore{err: pgx.ErrNoRows}
	m := newRecordingMetrics()
	p, err := NewProducer(testChannelType, store, nil, m)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}

	inserted, err := p.Enqueue(context.Background(), validRequest(t), EnqueuePathReconcile)
	if err != nil {
		t.Fatalf("a business-key conflict must not be an error, got %v", err)
	}
	if inserted {
		t.Error("a conflicting key must report inserted=false")
	}
	if m.enqueued != 0 {
		t.Error("a conflict enqueued nothing, so it must not be counted as an enqueue")
	}
}

func TestProducer_RealErrorPropagates(t *testing.T) {
	t.Parallel()
	store := &fakeProducerStore{err: errors.New("connection refused")}
	p, err := NewProducer(testChannelType, store, nil, nil)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	if _, err := p.Enqueue(context.Background(), validRequest(t), EnqueuePathRealtime); err == nil {
		t.Error("expected the store error to propagate")
	}
}

func TestProducer_WakesTheInstallationOnInsertAndOnConflict(t *testing.T) {
	t.Parallel()
	// Both cases must wake: on a conflict the row that won the race may be
	// sitting unclaimed on this very replica.
	for _, tc := range []struct {
		name  string
		store *fakeProducerStore
	}{
		{"insert", &fakeProducerStore{}},
		{"conflict", &fakeProducerStore{err: pgx.ErrNoRows}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wake := NewWakeRegistry()
			ch := wake.Register(testInstallationID)
			p, err := NewProducer(testChannelType, tc.store, wake, nil)
			if err != nil {
				t.Fatalf("NewProducer: %v", err)
			}
			if _, err := p.Enqueue(context.Background(), validRequest(t), EnqueuePathRealtime); err != nil {
				t.Fatalf("Enqueue: %v", err)
			}
			select {
			case <-ch:
			default:
				t.Error("expected a wake for the enqueued installation")
			}
		})
	}
}

func TestProducer_RejectsRequestsWithoutABusinessKey(t *testing.T) {
	t.Parallel()
	p, err := NewProducer(testChannelType, &fakeProducerStore{}, nil, nil)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}

	cases := map[string]func(Request) Request{
		"no installation": func(r Request) Request { r.InstallationID = pgxUUIDZero(); return r },
		"no workspace":    func(r Request) Request { r.WorkspaceID = pgxUUIDZero(); return r },
		"no source kind":  func(r Request) Request { r.SourceKind = "  "; return r },
		"no source id":    func(r Request) Request { r.SourceID = ""; return r },
		"no target":       func(r Request) Request { r.TargetChatID = " "; return r },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := p.Enqueue(context.Background(), mutate(validRequest(t)), EnqueuePathRealtime); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestNewProducer_RejectsIncompleteConfig(t *testing.T) {
	t.Parallel()
	if _, err := NewProducer("", &fakeProducerStore{}, nil, nil); err == nil {
		t.Error("expected an error for an empty channel type")
	}
	if _, err := NewProducer(testChannelType, nil, nil, nil); err == nil {
		t.Error("expected an error for nil queries")
	}
}
