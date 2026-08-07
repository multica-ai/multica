package outbox

// rategate_test.go — the admission decision. The transaction is faked, so what
// is under test is the window policy: which counts reject, that a rejection
// records nothing, and that a grant records exactly one attempt.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeTx is a pgx.Tx that records only whether it was committed or rolled back.
// Everything else panics: the gate must not reach for any other tx behavior.
type fakeTx struct {
	pgx.Tx
	committed  bool
	rolledBack bool
	commitErr  error
}

func (t *fakeTx) Commit(context.Context) error {
	if t.commitErr != nil {
		return t.commitErr
	}
	t.committed = true
	return nil
}

func (t *fakeTx) Rollback(context.Context) error {
	t.rolledBack = true
	return nil
}

type fakeTxStarter struct {
	tx       *fakeTx
	beginErr error
}

func (s *fakeTxStarter) Begin(context.Context) (pgx.Tx, error) {
	if s.beginErr != nil {
		return nil, s.beginErr
	}
	return s.tx, nil
}

// fakeRateQueries scripts one count per Count call, in order.
type fakeRateQueries struct {
	counts   []int64
	countIdx int

	lockErr   error
	countErr  error
	recordErr error

	locked   int
	recorded []db.RecordChannelOutboundSendAttemptParams
	cutoffs  []time.Time
}

func (q *fakeRateQueries) LockChannelOutboundRateWindow(context.Context, db.LockChannelOutboundRateWindowParams) error {
	q.locked++
	return q.lockErr
}

func (q *fakeRateQueries) CountChannelOutboundAttemptsSince(_ context.Context, arg db.CountChannelOutboundAttemptsSinceParams) (int64, error) {
	if q.countErr != nil {
		return 0, q.countErr
	}
	q.cutoffs = append(q.cutoffs, arg.AttemptedAt.Time)
	if q.countIdx >= len(q.counts) {
		return 0, nil
	}
	c := q.counts[q.countIdx]
	q.countIdx++
	return c, nil
}

func (q *fakeRateQueries) RecordChannelOutboundSendAttempt(_ context.Context, arg db.RecordChannelOutboundSendAttemptParams) (db.ChannelOutboundSendAttempt, error) {
	if q.recordErr != nil {
		return db.ChannelOutboundSendAttempt{}, q.recordErr
	}
	q.recorded = append(q.recorded, arg)
	return db.ChannelOutboundSendAttempt{}, nil
}

func testWindows() []Window {
	return []Window{
		{Name: "minute", Duration: time.Minute, Limit: 30},
		{Name: "hour", Duration: time.Hour, Limit: 1000},
	}
}

func newTestGate(t *testing.T, q *fakeRateQueries, starter *fakeTxStarter, now time.Time) *WindowRateGate {
	t.Helper()
	g, err := NewWindowRateGate(func(pgx.Tx) RateGateQueries { return q }, starter, testWindows()...)
	if err != nil {
		t.Fatalf("NewWindowRateGate: %v", err)
	}
	g.now = func() time.Time { return now }
	return g
}

func TestWindowRateGate_UnderLimitAdmitsAndRecords(t *testing.T) {
	t.Parallel()
	q := &fakeRateQueries{counts: []int64{5, 100}}
	tx := &fakeTx{}
	g := newTestGate(t, q, &fakeTxStarter{tx: tx}, time.Now())
	row := activeRow(t, 0)

	deferUntil, ok, err := g.Reserve(context.Background(), row)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if !ok {
		t.Fatal("expected admission under both limits")
	}
	if !deferUntil.IsZero() {
		t.Errorf("deferUntil = %v, want zero on admission", deferUntil)
	}
	// Exactly one attempt: the count is a per-send decision, so a double-record
	// would halve the effective quota.
	if len(q.recorded) != 1 {
		t.Fatalf("recorded %d attempts, want 1", len(q.recorded))
	}
	if q.recorded[0].QueueID != row.ID {
		t.Error("the recorded attempt must reference the row being sent")
	}
	// The lock, counts, and insert must be one committed transaction, or two
	// drainers both count under the limit and both send.
	if q.locked != 1 {
		t.Errorf("locks = %d, want 1", q.locked)
	}
	if !tx.committed {
		t.Error("an admission must commit the attempt row")
	}
}

func TestWindowRateGate_OverLimitDefersWithoutRecording(t *testing.T) {
	t.Parallel()
	now := time.Now()
	cases := []struct {
		name      string
		counts    []int64
		wantDefer time.Duration
	}{
		// The shortest window rejects first and defers by its own duration, so
		// a burst waits a minute rather than an hour.
		{"minute window full", []int64{30, 0}, time.Minute},
		{"hour window full", []int64{1, 1000}, time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := &fakeRateQueries{counts: tc.counts}
			tx := &fakeTx{}
			g := newTestGate(t, q, &fakeTxStarter{tx: tx}, now)

			deferUntil, ok, err := g.Reserve(context.Background(), activeRow(t, 0))
			if err != nil {
				t.Fatalf("Reserve: %v", err)
			}
			if ok {
				t.Fatal("expected rejection at the limit")
			}
			if want := now.Add(tc.wantDefer); !deferUntil.Equal(want) {
				t.Errorf("deferUntil = %v, want %v", deferUntil, want)
			}
			// A rejected send never happened, so counting it would make the
			// gate ratchet itself shut.
			if len(q.recorded) != 0 {
				t.Error("a rejected send must not be recorded as an attempt")
			}
			if tx.committed {
				t.Error("a rejection must not commit")
			}
			// Rollback is what releases the transaction-scoped advisory lock;
			// without it a rejected target stays wedged.
			if !tx.rolledBack {
				t.Error("a rejection must roll back so the advisory lock is released")
			}
		})
	}
}

// The limit is a ceiling, not a threshold: at exactly Limit-1 recorded attempts
// the next send is still allowed.
func TestWindowRateGate_BoundaryAdmitsTheLastSlot(t *testing.T) {
	t.Parallel()
	q := &fakeRateQueries{counts: []int64{29, 999}}
	g := newTestGate(t, q, &fakeTxStarter{tx: &fakeTx{}}, time.Now())

	_, ok, err := g.Reserve(context.Background(), activeRow(t, 0))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if !ok {
		t.Error("the last slot under the limit must be admitted")
	}
}

// Each window counts back over its own duration; a shared cutoff would make the
// hour limit meaningless.
func TestWindowRateGate_CountsEachWindowOverItsOwnDuration(t *testing.T) {
	t.Parallel()
	now := time.Now()
	q := &fakeRateQueries{counts: []int64{0, 0}}
	g := newTestGate(t, q, &fakeTxStarter{tx: &fakeTx{}}, now)

	if _, _, err := g.Reserve(context.Background(), activeRow(t, 0)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if len(q.cutoffs) != 2 {
		t.Fatalf("counted %d windows, want 2", len(q.cutoffs))
	}
	if want := now.Add(-time.Minute); !q.cutoffs[0].Equal(want) {
		t.Errorf("minute cutoff = %v, want %v", q.cutoffs[0], want)
	}
	if want := now.Add(-time.Hour); !q.cutoffs[1].Equal(want) {
		t.Errorf("hour cutoff = %v, want %v", q.cutoffs[1], want)
	}
}

// Every failure mode must be reported as an error, never as a silent grant: the
// consumer leaves the row claimed and reclaims it after the lease expires.
func TestWindowRateGate_FailuresNeverAdmit(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		queries *fakeRateQueries
		starter *fakeTxStarter
	}{
		"begin failed":  {&fakeRateQueries{}, &fakeTxStarter{beginErr: errors.New("pool exhausted")}},
		"lock failed":   {&fakeRateQueries{lockErr: errors.New("lock timeout")}, &fakeTxStarter{tx: &fakeTx{}}},
		"count failed":  {&fakeRateQueries{countErr: errors.New("read failed")}, &fakeTxStarter{tx: &fakeTx{}}},
		"record failed": {&fakeRateQueries{counts: []int64{0, 0}, recordErr: errors.New("write failed")}, &fakeTxStarter{tx: &fakeTx{}}},
		"commit failed": {&fakeRateQueries{counts: []int64{0, 0}}, &fakeTxStarter{tx: &fakeTx{commitErr: errors.New("commit failed")}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := newTestGate(t, tc.queries, tc.starter, time.Now())
			_, ok, err := g.Reserve(context.Background(), activeRow(t, 0))
			if err == nil {
				t.Error("expected an error")
			}
			if ok {
				t.Error("a failed gate must never admit a send")
			}
		})
	}
}

// A commit failure must not be reported as a grant: the attempt row is gone, so
// admitting would send without having counted it.
func TestWindowRateGate_CommitFailureDoesNotAdmit(t *testing.T) {
	t.Parallel()
	tx := &fakeTx{commitErr: errors.New("commit failed")}
	q := &fakeRateQueries{counts: []int64{0, 0}}
	g := newTestGate(t, q, &fakeTxStarter{tx: tx}, time.Now())

	_, ok, _ := g.Reserve(context.Background(), activeRow(t, 0))
	if ok {
		t.Error("an uncommitted attempt must not admit the send")
	}
	if tx.committed {
		t.Error("the fake reported a commit despite the scripted error")
	}
}

func TestNewWindowRateGate_RejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	bind := func(pgx.Tx) RateGateQueries { return &fakeRateQueries{} }
	starter := &fakeTxStarter{tx: &fakeTx{}}

	cases := map[string]struct {
		bind    BindTx
		tx      TxStarter
		windows []Window
	}{
		"no binder":       {nil, starter, testWindows()},
		"no tx starter":   {bind, nil, testWindows()},
		"no windows":      {bind, starter, nil},
		"zero duration":   {bind, starter, []Window{{Name: "bad", Limit: 1}}},
		"zero limit":      {bind, starter, []Window{{Name: "bad", Duration: time.Minute}}},
		"negative limit":  {bind, starter, []Window{{Name: "bad", Duration: time.Minute, Limit: -1}}},
		"one bad of many": {bind, starter, []Window{{Name: "ok", Duration: time.Minute, Limit: 1}, {Name: "bad", Duration: time.Hour}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewWindowRateGate(tc.bind, tc.tx, tc.windows...); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
