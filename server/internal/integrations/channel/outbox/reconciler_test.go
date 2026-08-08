package outbox

// reconciler_test.go — the compensating scan. What matters here is the cursor
// discipline (advance only after a clean scan, release otherwise) and the
// distinction between "rescued a reply the realtime path lost" and "lost a
// harmless race with it".

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const testCursorLease = "cursor-lease-1"

type fakeReconcilerStore struct {
	claimState    db.ChannelOutboundReconcileState
	claimStateErr error

	// cursorAbsent models a deployment where no cursor row exists yet, so the
	// claim query's upsert is what creates it. The seed value the Reconciler
	// passes is then what the first scan window is built from, which is the
	// difference between a first sweep that scans nothing and one that
	// backfills every reply the pre-queue path already delivered.
	cursorAbsent    bool
	cursorCreatedAt time.Time
	seededCursorAt  time.Time

	candidates    [][]db.ListChannelOutboundReconcileCandidatesRow
	candidatesErr error
	listCalls     []db.ListChannelOutboundReconcileCandidatesParams

	task    db.AgentTaskQueue
	taskErr error

	channelIngested bool

	advanced []db.AdvanceChannelOutboundReconcileStateParams
	released []db.ReleaseChannelOutboundReconcileStateParams

	failUndeliverableCalls int
	purgedSent             int
	purgedFailed           int
	purgedAttempts         int
}

func (s *fakeReconcilerStore) ClaimChannelOutboundReconcileState(_ context.Context, arg db.ClaimChannelOutboundReconcileStateParams) (db.ChannelOutboundReconcileState, error) {
	if s.cursorAbsent {
		// INSERT ... ON CONFLICT DO NOTHING then claim: the row is born with the
		// caller's seed as cursor_at and the database's now() as created_at.
		s.cursorAbsent = false
		s.seededCursorAt = arg.CursorAt.Time
		s.cursorCreatedAt = time.Now()
		s.claimState = db.ChannelOutboundReconcileState{
			ChannelType: testChannelType,
			CursorAt:    arg.CursorAt,
			CreatedAt:   pgtype.Timestamptz{Time: s.cursorCreatedAt, Valid: true},
			LeaseToken:  pgtype.Text{String: testCursorLease, Valid: true},
		}
	}
	return s.claimState, s.claimStateErr
}

func (s *fakeReconcilerStore) ListChannelOutboundReconcileCandidates(_ context.Context, arg db.ListChannelOutboundReconcileCandidatesParams) ([]db.ListChannelOutboundReconcileCandidatesRow, error) {
	s.listCalls = append(s.listCalls, arg)
	if s.candidatesErr != nil {
		return nil, s.candidatesErr
	}
	if len(s.candidates) == 0 {
		return nil, nil
	}
	page := s.candidates[0]
	s.candidates = s.candidates[1:]
	// Mirror the query's window predicate (completed_at > start AND <= end).
	// Without it the fake hands back rows the real scan could never see, and a
	// change to how the window is computed reads as having changed nothing.
	kept := make([]db.ListChannelOutboundReconcileCandidatesRow, 0, len(page))
	for _, row := range page {
		if row.CompletedAt.Valid &&
			row.CompletedAt.Time.After(arg.WindowStart.Time) &&
			!row.CompletedAt.Time.After(arg.WindowEnd.Time) {
			kept = append(kept, row)
		}
	}
	return kept, nil
}

func (s *fakeReconcilerStore) AdvanceChannelOutboundReconcileState(_ context.Context, arg db.AdvanceChannelOutboundReconcileStateParams) (db.ChannelOutboundReconcileState, error) {
	s.advanced = append(s.advanced, arg)
	// Model the commit so a test can sweep more than once: the next claim sees
	// the cursor this sweep left behind.
	s.claimState.CursorAt = arg.CursorAt
	return s.claimState, nil
}

func (s *fakeReconcilerStore) ReleaseChannelOutboundReconcileState(_ context.Context, arg db.ReleaseChannelOutboundReconcileStateParams) error {
	s.released = append(s.released, arg)
	return nil
}

func (s *fakeReconcilerStore) FailUndeliverableChannelOutbound(context.Context) error {
	s.failUndeliverableCalls++
	return nil
}

func (s *fakeReconcilerStore) PurgeChannelOutboundSendAttemptsBefore(context.Context, pgtype.Timestamptz) error {
	s.purgedAttempts++
	return nil
}

func (s *fakeReconcilerStore) PurgeSentChannelOutboundQueueBefore(context.Context, pgtype.Timestamptz) error {
	s.purgedSent++
	return nil
}

func (s *fakeReconcilerStore) PurgeFailedChannelOutboundQueueBefore(context.Context, pgtype.Timestamptz) error {
	s.purgedFailed++
	return nil
}

func (s *fakeReconcilerStore) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.task, s.taskErr
}

func (s *fakeReconcilerStore) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return s.channelIngested, nil
}

// fakeBuilder yields a scripted request and records the candidates it saw.
type fakeBuilder struct {
	kinds []string
	ok    bool
	err   error
	seen  []Candidate
	req   Request
}

func (b *fakeBuilder) SourceKinds() []string { return b.kinds }

func (b *fakeBuilder) Build(_ context.Context, cand Candidate) (Request, bool, error) {
	b.seen = append(b.seen, cand)
	if b.err != nil {
		return Request{}, false, b.err
	}
	return b.req, b.ok, nil
}

func candidateRow(t *testing.T, status string) db.ListChannelOutboundReconcileCandidatesRow {
	t.Helper()
	return db.ListChannelOutboundReconcileCandidatesRow{
		TaskID:         uuidFrom(t, "77777777-7777-7777-7777-777777777777"),
		ChatSessionID:  uuidFrom(t, "44444444-4444-4444-4444-444444444444"),
		TaskStatus:     status,
		CompletedAt:    pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		InstallationID: uuidFrom(t, testInstallationID),
		WorkspaceID:    uuidFrom(t, "33333333-3333-3333-3333-333333333333"),
		ChannelType:    testChannelType,
	}
}

// leasedCursor is a mature deployment's cursor: the row has existed far longer
// than any scan window, so its created_at never constrains the window.
func leasedCursor(cursorAt time.Time) db.ChannelOutboundReconcileState {
	return db.ChannelOutboundReconcileState{
		ChannelType: testChannelType,
		CursorAt:    pgtype.Timestamptz{Time: cursorAt, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: time.Now().Add(-7 * 24 * time.Hour), Valid: true},
		LeaseToken:  pgtype.Text{String: testCursorLease, Valid: true},
	}
}

func newTestReconciler(t *testing.T, store *fakeReconcilerStore, builder PayloadBuilder, producerStore ProducerStore, m Metrics) *Reconciler {
	t.Helper()
	producer, err := NewProducer(testChannelType, producerStore, nil, m)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		ChannelType: testChannelType,
		Queries:     store,
		Producer:    producer,
		Builder:     builder,
		Metrics:     m,
	})
	if err != nil {
		t.Fatalf("NewReconciler: %v", err)
	}
	return r
}

func TestReconciler_RescuesAMissedReply(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{
		claimState:      leasedCursor(time.Now().Add(-time.Hour)),
		candidates:      [][]db.ListChannelOutboundReconcileCandidatesRow{{candidateRow(t, "completed")}},
		channelIngested: true,
	}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true, req: validRequest(t)}
	producerStore := &fakeProducerStore{}
	m := newRecordingMetrics()
	r := newTestReconciler(t, store, builder, producerStore, m)

	r.sweep(context.Background())

	if len(producerStore.rows) != 1 {
		t.Fatalf("enqueued %d rows, want 1", len(producerStore.rows))
	}
	if m.enqueued != 1 {
		t.Errorf("enqueue observations = %d, want 1", m.enqueued)
	}
	if m.raceLosses != 0 {
		t.Error("a fresh insert is a rescue, not a lost race")
	}
	if len(store.advanced) != 1 {
		t.Fatalf("advanced the cursor %d times, want 1", len(store.advanced))
	}
	if store.advanced[0].LeaseToken.String != testCursorLease {
		t.Errorf("advance lease = %q, want %q", store.advanced[0].LeaseToken.String, testCursorLease)
	}
}

// The candidate scan and the insert are not one transaction, so the realtime
// path can land the row in between. That is expected background noise, not a
// rescue — and must not be logged or counted as one.
func TestReconciler_LostRaceIsCountedSeparately(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{
		claimState:      leasedCursor(time.Now().Add(-time.Hour)),
		candidates:      [][]db.ListChannelOutboundReconcileCandidatesRow{{candidateRow(t, "completed")}},
		channelIngested: true,
	}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true, req: validRequest(t)}
	m := newRecordingMetrics()
	r := newTestReconciler(t, store, builder, &fakeProducerStore{err: pgx.ErrNoRows}, m)

	r.sweep(context.Background())

	if m.raceLosses != 1 {
		t.Errorf("race losses = %d, want 1", m.raceLosses)
	}
	if m.enqueued != 0 {
		t.Error("a lost race enqueued nothing")
	}
	if len(store.advanced) != 1 {
		t.Error("a lost race is not a scan failure; the cursor must still advance")
	}
}

// A channel-bound session also carries web/mobile tasks. Their replies stay in
// Multica, so the reconciler must not push them to the platform.
func TestReconciler_SkipsTasksNotIngestedFromTheChannel(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{
		claimState:      leasedCursor(time.Now().Add(-time.Hour)),
		candidates:      [][]db.ListChannelOutboundReconcileCandidatesRow{{candidateRow(t, "completed")}},
		task:            db.AgentTaskQueue{ChatInputTaskID: uuidFrom(t, "88888888-8888-8888-8888-888888888888")},
		channelIngested: false,
	}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true, req: validRequest(t)}
	producerStore := &fakeProducerStore{}
	r := newTestReconciler(t, store, builder, producerStore, nil)

	r.sweep(context.Background())

	if len(builder.seen) != 0 {
		t.Error("a non-channel task must be skipped before the builder runs")
	}
	if len(producerStore.rows) != 0 {
		t.Error("a web/mobile reply must not be delivered to the platform")
	}
}

func TestReconciler_BuilderSkipEnqueuesNothingButStillAdvances(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{
		claimState:      leasedCursor(time.Now().Add(-time.Hour)),
		candidates:      [][]db.ListChannelOutboundReconcileCandidatesRow{{candidateRow(t, "completed")}},
		channelIngested: true,
	}
	// ok=false is the ordinary "nothing to deliver" answer: empty reply,
	// revoked target, a status this channel does not notify on.
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: false}
	producerStore := &fakeProducerStore{}
	r := newTestReconciler(t, store, builder, producerStore, nil)

	r.sweep(context.Background())

	if len(producerStore.rows) != 0 {
		t.Error("a skipped candidate must enqueue nothing")
	}
	if len(store.advanced) != 1 {
		t.Error("skipped candidates are not failures; the cursor must advance")
	}
}

// A failed scan must release the lease WITHOUT advancing, or the cursor would
// step past rows the scan never looked at and those replies would be lost for
// good.
func TestReconciler_ScanFailureReleasesWithoutAdvancing(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{
		claimState:    leasedCursor(time.Now().Add(-time.Hour)),
		candidatesErr: errors.New("connection refused"),
	}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true, req: validRequest(t)}
	r := newTestReconciler(t, store, builder, &fakeProducerStore{}, nil)

	r.sweep(context.Background())

	if len(store.advanced) != 0 {
		t.Error("a failed scan must not advance the cursor past unscanned rows")
	}
	if len(store.released) != 1 {
		t.Fatalf("released the lease %d times, want 1", len(store.released))
	}
	if store.released[0].LeaseToken.String != testCursorLease {
		t.Errorf("release lease = %q, want %q", store.released[0].LeaseToken.String, testCursorLease)
	}
}

// A builder error aborts the scan the same way: the window is retried rather
// than skipped.
func TestReconciler_BuilderErrorReleasesWithoutAdvancing(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{
		claimState:      leasedCursor(time.Now().Add(-time.Hour)),
		candidates:      [][]db.ListChannelOutboundReconcileCandidatesRow{{candidateRow(t, "completed")}},
		channelIngested: true,
	}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, err: errors.New("binding read failed")}
	r := newTestReconciler(t, store, builder, &fakeProducerStore{}, nil)

	r.sweep(context.Background())

	if len(store.advanced) != 0 {
		t.Error("a builder error must not advance the cursor")
	}
	if len(store.released) != 1 {
		t.Errorf("released %d times, want 1", len(store.released))
	}
}

// Another replica holding the cursor lease surfaces as pgx.ErrNoRows. That is
// the lease working, not a failure.
func TestReconciler_CursorHeldElsewhereIsANoop(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{claimStateErr: pgx.ErrNoRows}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true, req: validRequest(t)}
	producerStore := &fakeProducerStore{}
	r := newTestReconciler(t, store, builder, producerStore, nil)

	r.sweep(context.Background())

	if len(store.listCalls) != 0 {
		t.Error("a replica without the lease must not scan")
	}
	if len(producerStore.rows) != 0 || len(store.advanced) != 0 {
		t.Error("a replica without the lease must not enqueue or advance")
	}
}

// The stranded-row sweep runs regardless of the cursor lease: rows behind a
// revoked installation are never claimable, so nothing else would move them to
// a terminal state and the retention purge would never reach them.
func TestReconciler_UndeliverableSweepRunsWithoutTheLease(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{claimStateErr: pgx.ErrNoRows}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true}
	r := newTestReconciler(t, store, builder, &fakeProducerStore{}, nil)

	r.sweep(context.Background())

	if store.failUndeliverableCalls != 1 {
		t.Errorf("undeliverable sweeps = %d, want 1", store.failUndeliverableCalls)
	}
}

// With enqueues gated off, the cursor must still advance — otherwise
// re-enabling the integration would flush a backlog of stale replies at users.
func TestReconciler_DisabledEnqueueStillAdvancesTheCursor(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{
		claimState:      leasedCursor(time.Now().Add(-time.Hour)),
		candidates:      [][]db.ListChannelOutboundReconcileCandidatesRow{{candidateRow(t, "completed")}},
		channelIngested: true,
	}
	producer, err := NewProducer(testChannelType, &fakeProducerStore{}, nil, nil)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		ChannelType: testChannelType,
		Queries:     store,
		Producer:    producer,
		Builder:     &fakeBuilder{kinds: []string{"chat_done"}, ok: true, req: validRequest(t)},
		EnqueueOK:   func() bool { return false },
	})
	if err != nil {
		t.Fatalf("NewReconciler: %v", err)
	}

	r.sweep(context.Background())

	if len(store.listCalls) != 0 {
		t.Error("a disabled reconciler must not scan")
	}
	if len(store.advanced) != 1 {
		t.Errorf("advanced %d times, want 1 — the window must not accumulate", len(store.advanced))
	}
}

// The scan window trails the cursor by an overlap so a task that committed just
// before the previous advance is not lost at the seam, and stops short of now()
// so it does not race the realtime path on every fresh task.
func TestReconciler_ScanWindowOverlapsAndLags(t *testing.T) {
	t.Parallel()
	cursorAt := time.Now().Add(-30 * time.Minute)
	store := &fakeReconcilerStore{
		claimState: leasedCursor(cursorAt),
		candidates: [][]db.ListChannelOutboundReconcileCandidatesRow{nil},
	}
	builder := &fakeBuilder{kinds: []string{"chat_done", "task_failed"}, ok: true}
	r := newTestReconciler(t, store, builder, &fakeProducerStore{}, nil)

	r.sweep(context.Background())

	if len(store.listCalls) != 1 {
		t.Fatalf("scan calls = %d, want 1", len(store.listCalls))
	}
	call := store.listCalls[0]
	wantStart := cursorAt.Add(-reconcileOverlapWindow)
	if !call.WindowStart.Time.Equal(wantStart) {
		t.Errorf("window start = %v, want the cursor minus the overlap (%v)", call.WindowStart.Time, wantStart)
	}
	if !call.WindowEnd.Time.Before(time.Now()) {
		t.Error("window end must trail now() so the realtime path gets first crack")
	}
	// Only this channel's kinds are scanned, or the reconciler would resend
	// replies another kind already delivered.
	if len(call.SourceKinds) != 2 {
		t.Errorf("source kinds = %v, want the builder's two", call.SourceKinds)
	}
	if call.ChannelType != testChannelType {
		t.Errorf("channel_type = %q, want %q", call.ChannelType, testChannelType)
	}
}

// Upgrading a running deployment starts with an empty channel_outbound_queue
// (migration 265 creates it) and no cursor row. "Already delivered" is only ever
// tested as "has a queue row", and the pre-queue path wrote straight to the
// socket, so every terminal task in the seed window looks like a reply the
// realtime path missed. Nothing that finished before the queue existed can be
// missing from it, so a fresh cursor must not scan behind its own creation —
// otherwise every user who talked to the bot that day gets the whole day
// re-posted, once and unrecallably.
func TestReconciler_FreshCursorDoesNotBackfillAlreadyDeliveredReplies(t *testing.T) {
	t.Parallel()
	// A reply the old path really did deliver: terminal two hours ago, and no
	// queue row anywhere, because that path never wrote one.
	delivered := candidateRow(t, "completed")
	delivered.CompletedAt = pgtype.Timestamptz{Time: time.Now().Add(-2 * time.Hour), Valid: true}
	store := &fakeReconcilerStore{
		cursorAbsent:    true,
		candidates:      [][]db.ListChannelOutboundReconcileCandidatesRow{{delivered}},
		channelIngested: true,
	}
	producerStore := &fakeProducerStore{}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true, req: validRequest(t)}
	r := newTestReconciler(t, store, builder, producerStore, nil)

	r.sweep(context.Background())

	if len(store.listCalls) != 1 {
		t.Fatalf("scan calls = %d, want 1", len(store.listCalls))
	}
	if got := store.listCalls[0].WindowStart.Time; got.Before(store.cursorCreatedAt) {
		t.Errorf("window start = %v reaches behind the cursor's creation (%v): a fresh cursor re-delivers every reply the pre-queue path already sent",
			got, store.cursorCreatedAt)
	}
	if got := len(producerStore.rows); got != 0 {
		t.Errorf("enqueued %d rows on a fresh cursor, want 0 — those replies were already delivered", got)
	}
	// cursor_at means "everything up to here has been considered". Seeding it a
	// day in the past asserts a scan that never ran, which misreads as progress
	// to anything else looking at the row.
	if store.seededCursorAt.Before(store.cursorCreatedAt.Add(-time.Minute)) {
		t.Errorf("cursor seeded at %v, a day before the row was created (%v): cursor_at would claim scan progress that never happened",
			store.seededCursorAt, store.cursorCreatedAt)
	}
}

// The floor has to be durable, not a one-off seed adjustment: the cursor
// advances to windowEnd on the first sweep, so the second sweep's
// cursor-minus-overlap reaches back before the queue existed all over again.
func TestReconciler_CursorCreationFloorsEveryWindowNotJustTheFirst(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{
		cursorAbsent:    true,
		candidates:      [][]db.ListChannelOutboundReconcileCandidatesRow{nil, nil},
		channelIngested: true,
	}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true, req: validRequest(t)}
	r := newTestReconciler(t, store, builder, &fakeProducerStore{}, nil)

	r.sweep(context.Background()) // creates and advances the cursor
	r.sweep(context.Background()) // cursor_at is now windowEnd, i.e. behind created_at

	if len(store.listCalls) != 2 {
		t.Fatalf("scan calls = %d, want 2", len(store.listCalls))
	}
	for i, call := range store.listCalls {
		if call.WindowStart.Time.Before(store.cursorCreatedAt) {
			t.Errorf("sweep %d: window start = %v reaches behind the cursor's creation (%v)",
				i+1, call.WindowStart.Time, store.cursorCreatedAt)
		}
	}
}

// reconcileInitialLookback is the ceiling on how far back any scan reaches. A
// cursor stalled for a week (a replica down, or the integration disabled) must
// not resurrect a week of replies whose sent-tombstones the purge already
// removed — sentRetention is only 24h, so those rows no longer suppress a
// re-enqueue.
func TestReconciler_StalledCursorIsClampedToTheLookbackCeiling(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{
		claimState: leasedCursor(time.Now().Add(-7 * 24 * time.Hour)),
		candidates: [][]db.ListChannelOutboundReconcileCandidatesRow{nil},
	}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true}
	r := newTestReconciler(t, store, builder, &fakeProducerStore{}, nil)

	r.sweep(context.Background())

	if len(store.listCalls) != 1 {
		t.Fatalf("scan calls = %d, want 1", len(store.listCalls))
	}
	floor := time.Now().Add(-reconcileInitialLookback)
	if got := store.listCalls[0].WindowStart.Time; got.Before(floor.Add(-time.Minute)) {
		t.Errorf("window start = %v, want no earlier than the %v lookback ceiling (%v)",
			got, reconcileInitialLookback, floor)
	}
}

// A full page means there may be more; paging must continue from the last row's
// (completed_at, task_id) rather than re-reading the same page forever.
func TestReconciler_PagesUntilShortPage(t *testing.T) {
	t.Parallel()
	full := make([]db.ListChannelOutboundReconcileCandidatesRow, reconcilePageSize)
	for i := range full {
		full[i] = candidateRow(t, "completed")
	}
	store := &fakeReconcilerStore{
		claimState:      leasedCursor(time.Now().Add(-time.Hour)),
		candidates:      [][]db.ListChannelOutboundReconcileCandidatesRow{full, {candidateRow(t, "completed")}},
		channelIngested: true,
	}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: false}
	r := newTestReconciler(t, store, builder, &fakeProducerStore{}, nil)

	r.sweep(context.Background())

	if len(store.listCalls) != 2 {
		t.Fatalf("scan calls = %d, want 2 (full page then short page)", len(store.listCalls))
	}
	if !store.listCalls[1].AfterCompletedAt.Valid || !store.listCalls[1].AfterTaskID.Valid {
		t.Error("the second page must be keyed off the last row of the first")
	}
}

func TestReconciler_PurgeSweepsBothRetentionWindows(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true}
	r := newTestReconciler(t, store, builder, &fakeProducerStore{}, nil)

	if err := r.purge(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	// All three retention windows, including the rate gate's attempt ledger —
	// the ledger is a sliding-window count, so unbounded growth is pure waste.
	if store.purgedSent != 1 || store.purgedFailed != 1 || store.purgedAttempts != 1 {
		t.Errorf("purges = sent:%d failed:%d attempts:%d, want 1 each",
			store.purgedSent, store.purgedFailed, store.purgedAttempts)
	}
}

func TestNewReconciler_RejectsIncompleteConfig(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{}
	producer, err := NewProducer(testChannelType, &fakeProducerStore{}, nil, nil)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	builder := &fakeBuilder{kinds: []string{"chat_done"}}

	cases := []struct {
		name string
		cfg  ReconcilerConfig
	}{
		{"no channel type", ReconcilerConfig{Queries: store, Producer: producer, Builder: builder}},
		{"no queries", ReconcilerConfig{ChannelType: testChannelType, Producer: producer, Builder: builder}},
		{"no producer", ReconcilerConfig{ChannelType: testChannelType, Queries: store, Builder: builder}},
		{"no builder", ReconcilerConfig{ChannelType: testChannelType, Queries: store, Producer: producer}},
		// A builder with no source kinds would scan for rows matching nothing,
		// so every terminal task would look un-enqueued and be resent.
		{"builder declares no source kinds", ReconcilerConfig{
			ChannelType: testChannelType, Queries: store, Producer: producer, Builder: &fakeBuilder{},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewReconciler(tc.cfg); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestReconciler_WaitWithTimeoutReturnsAfterRun(t *testing.T) {
	t.Parallel()
	store := &fakeReconcilerStore{claimStateErr: pgx.ErrNoRows}
	builder := &fakeBuilder{kinds: []string{"chat_done"}, ok: true}
	r := newTestReconciler(t, store, builder, &fakeProducerStore{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	cancel()
	if !r.WaitWithTimeout(5 * time.Second) {
		t.Error("Run did not return after ctx cancellation")
	}
}
