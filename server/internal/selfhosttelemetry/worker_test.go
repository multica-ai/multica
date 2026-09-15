package selfhosttelemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fakeStore struct {
	session leaderSession
	leader  bool
	err     error
	calls   int
}

func (s *fakeStore) TryLeader(context.Context) (leaderSession, bool, error) {
	s.calls++
	return s.session, s.leader, s.err
}

type fakeSession struct {
	state       telemetryState
	savedBodies [][]byte
	dropped     int
	succeeded   int
	closed      int
}

func (s *fakeSession) Connection() *pgx.Conn { return nil }

func (s *fakeSession) LoadOrCreate(context.Context) (telemetryState, error) {
	state := s.state
	state.PendingBody = slices.Clone(state.PendingBody)
	return state, nil
}

func (s *fakeSession) SavePending(_ context.Context, day time.Time, body []byte, next time.Time) error {
	day = utcDay(day)
	next = next.UTC()
	s.state.PendingDay = &day
	s.state.PendingBody = slices.Clone(body)
	s.state.NextAttemptAt = &next
	s.state.AttemptCount = 0
	s.savedBodies = append(s.savedBodies, slices.Clone(body))
	return nil
}

func (s *fakeSession) DropPending(context.Context) error {
	s.dropped++
	s.state.PendingDay = nil
	s.state.PendingBody = nil
	s.state.NextAttemptAt = nil
	s.state.AttemptCount = 0
	return nil
}

func (s *fakeSession) MarkSuccessful(_ context.Context, day time.Time) error {
	s.succeeded++
	day = utcDay(day)
	s.state.LastSuccessfulDay = &day
	return s.DropPending(context.Background())
}

func (s *fakeSession) ScheduleRetry(_ context.Context, next time.Time, attempt int) error {
	next = next.UTC()
	s.state.NextAttemptAt = &next
	s.state.AttemptCount = attempt
	return nil
}

func (s *fakeSession) Close() error {
	s.closed++
	return nil
}

type fakeCollector struct {
	counts Counts
	err    error
	calls  int
}

func (c *fakeCollector) Collect(context.Context, *pgx.Conn, time.Time) (Counts, error) {
	c.calls++
	return c.counts, c.err
}

type fakeSender struct {
	dispositions []deliveryDisposition
	bodies       [][]byte
	closed       int
}

func (s *fakeSender) Send(_ context.Context, body []byte) deliveryDisposition {
	s.bodies = append(s.bodies, slices.Clone(body))
	if len(s.dispositions) == 0 {
		return deliverySuccess
	}
	result := s.dispositions[0]
	s.dispositions = s.dispositions[1:]
	return result
}

func (s *fakeSender) Close() { s.closed++ }

func testWorker(state telemetryState, collector *fakeCollector, sender *fakeSender) (*Worker, *fakeSession) {
	session := &fakeSession{state: state}
	return &Worker{
		store:         &fakeStore{session: session, leader: true},
		collector:     collector,
		sender:        sender,
		serverVersion: "v1.2.3",
		logger:        slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		now:           time.Now,
	}, session
}

func TestDefaultConfigCollectsAndSends(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("01010101-2222-3333-4444-555555555555")
	today := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	now := scheduledAt(id, today).Add(time.Minute)
	session := &fakeSession{state: telemetryState{InstanceID: id}}
	collector := &fakeCollector{}
	sender := &fakeSender{}
	worker := newWithFactories(ConfigFromDoNotTrack(""), "v1.2.3", nil, workerFactories{
		store:     func() leaderStore { return &fakeStore{session: session, leader: true} },
		collector: func() eventCollector { return collector },
		sender:    func() eventSender { return sender },
	})
	if worker == nil {
		t.Fatal("default configuration did not construct telemetry worker")
	}
	if _, err := worker.runOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if collector.calls != 1 || len(sender.bodies) != 1 {
		t.Fatalf("default collector/sender calls = %d/%d, want 1/1", collector.calls, len(sender.bodies))
	}
}

func TestWorkerGeneratesPersistsAndSendsOneDailySnapshot(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	today := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	now := scheduledAt(id, today).Add(time.Minute)
	collector := &fakeCollector{counts: Counts{Workspaces: 3, TasksStarted: 12}}
	sender := &fakeSender{dispositions: []deliveryDisposition{deliverySuccess}}
	worker, session := testWorker(telemetryState{InstanceID: id}, collector, sender)

	next, err := worker.runOnce(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if collector.calls != 1 || len(sender.bodies) != 1 {
		t.Fatalf("collector/sender calls = %d/%d, want 1/1", collector.calls, len(sender.bodies))
	}
	if len(session.savedBodies) != 1 || session.succeeded != 1 || session.closed != 1 {
		t.Fatalf("saved/succeeded/closed = %d/%d/%d, want 1/1/1", len(session.savedBodies), session.succeeded, session.closed)
	}
	if !bytes.Equal(session.savedBodies[0], sender.bodies[0]) {
		t.Fatal("sent body differs from the once-persisted body")
	}
	if want := scheduledAt(id, today.AddDate(0, 0, 1)); !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}

	// A second replica/process round sees the durable success and does nothing.
	if _, err := worker.runOnce(context.Background(), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if collector.calls != 1 || len(sender.bodies) != 1 {
		t.Fatalf("same-day success collected/sent again: %d/%d", collector.calls, len(sender.bodies))
	}
}

func TestWorkerWaitsForDeterministicDailyJitter(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	today := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	wake := scheduledAt(id, today)
	collector := &fakeCollector{}
	sender := &fakeSender{}
	worker, _ := testWorker(telemetryState{InstanceID: id}, collector, sender)

	next, err := worker.runOnce(context.Background(), today)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Equal(wake) {
		t.Fatalf("next = %s, want deterministic wake %s", next, wake)
	}
	if collector.calls != 0 || len(sender.bodies) != 0 {
		t.Fatal("worker collected or sent before its jittered daily slot")
	}
	if offset := wake.Sub(today); offset < 0 || offset >= 6*time.Hour {
		t.Fatalf("daily jitter = %s, want [0, 6h)", offset)
	}
}

func TestWorkerRetriesWithTheExactPersistedBodyAcrossRestart(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("22222222-2222-3333-4444-555555555555")
	today := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	now := scheduledAt(id, today).Add(time.Minute)
	collector := &fakeCollector{counts: Counts{TasksCompleted: 7}}
	sender := &fakeSender{dispositions: []deliveryDisposition{deliveryRetry, deliverySuccess}}
	worker, session := testWorker(telemetryState{InstanceID: id}, collector, sender)

	next, err := worker.runOnce(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if session.state.AttemptCount != 1 || session.state.NextAttemptAt == nil {
		t.Fatalf("retry state not persisted: %#v", session.state)
	}
	if delay := next.Sub(now); delay < time.Minute || delay > 75*time.Second {
		t.Fatalf("first retry delay = %s, want [1m, 1m15s]", delay)
	}

	// A newly constructed worker represents a process restart. It receives only
	// the persisted state and must not collect/marshal a replacement body.
	restartedSession := &fakeSession{state: session.state}
	restarted := &Worker{
		store: &fakeStore{session: restartedSession, leader: true}, collector: collector,
		sender: sender, serverVersion: "v9.9.9", logger: worker.logger, now: time.Now,
	}
	if _, err := restarted.runOnce(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if collector.calls != 1 {
		t.Fatalf("collector calls = %d, retry regenerated the body", collector.calls)
	}
	if len(sender.bodies) != 2 || !bytes.Equal(sender.bodies[0], sender.bodies[1]) {
		t.Fatal("response-loss retry did not reuse the byte-identical persisted body")
	}
}

func TestWorkerDropsPreviousDayPendingInsteadOfBuildingAnOfflineQueue(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("33333333-2222-3333-4444-555555555555")
	today := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	now := scheduledAt(id, today).Add(time.Minute)
	nextAttempt := now.Add(time.Hour)
	collector := &fakeCollector{counts: Counts{TasksCancelled: 2}}
	sender := &fakeSender{}
	worker, session := testWorker(telemetryState{
		InstanceID: id, PendingDay: &yesterday, PendingBody: []byte(`{"old":true}`),
		NextAttemptAt: &nextAttempt, AttemptCount: 4,
	}, collector, sender)

	if _, err := worker.runOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if session.dropped < 1 || collector.calls != 1 || len(sender.bodies) != 1 {
		t.Fatalf("dropped/collected/sent = %d/%d/%d", session.dropped, collector.calls, len(sender.bodies))
	}
	if bytes.Contains(sender.bodies[0], []byte(`"old"`)) {
		t.Fatal("old pending body was sent after a new UTC day began")
	}
}

func TestWorkerProtocolErrorStopsUntilNextUTCDay(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("44444444-2222-3333-4444-555555555555")
	today := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	now := scheduledAt(id, today).Add(time.Minute)
	collector := &fakeCollector{}
	sender := &fakeSender{dispositions: []deliveryDisposition{deliveryStopForDay, deliverySuccess}}
	worker, session := testWorker(telemetryState{InstanceID: id}, collector, sender)

	next, err := worker.runOnce(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if want := scheduledAt(id, today.AddDate(0, 0, 1)); !next.Equal(want) {
		t.Fatalf("protocol retry = %s, want next day %s", next, want)
	}
	if _, err := worker.runOnce(context.Background(), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(sender.bodies) != 1 {
		t.Fatal("protocol error retried again on the same UTC day")
	}
	if _, err := worker.runOnce(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if len(sender.bodies) != 2 || collector.calls != 2 || session.dropped < 1 {
		t.Fatalf("next day sent/collected/dropped = %d/%d/%d", len(sender.bodies), collector.calls, session.dropped)
	}
}

func TestWorkerSkipsDeliveryWhenCollectionFails(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("55555555-2222-3333-4444-555555555555")
	today := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	now := scheduledAt(id, today).Add(time.Minute)
	collector := &fakeCollector{err: errors.New("statement timeout")}
	sender := &fakeSender{}
	worker, session := testWorker(telemetryState{InstanceID: id}, collector, sender)

	next, err := worker.runOnce(context.Background(), now)
	if err == nil {
		t.Fatal("collection failure was hidden")
	}
	if !next.Equal(now.Add(time.Hour)) || len(sender.bodies) != 0 || len(session.savedBodies) != 0 {
		t.Fatalf("failure next/sent/saved = %s/%d/%d", next, len(sender.bodies), len(session.savedBodies))
	}
}

func TestWorkerDropsInvalidStoredPendingWithoutDelivery(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("56565656-2222-3333-4444-555555555555")
	today := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	now := scheduledAt(id, today).Add(time.Minute)
	nextAttempt := now
	collector := &fakeCollector{}
	sender := &fakeSender{}
	worker, session := testWorker(telemetryState{
		InstanceID: id, PendingDay: &today, PendingBody: make([]byte, maxRequestSize+1),
		NextAttemptAt: &nextAttempt,
	}, collector, sender)

	if _, err := worker.runOnce(context.Background(), now); err == nil {
		t.Fatal("oversized stored pending event was accepted")
	}
	if session.dropped != 1 || collector.calls != 0 || len(sender.bodies) != 0 {
		t.Fatalf("dropped/collected/sent = %d/%d/%d, want 1/0/0", session.dropped, collector.calls, len(sender.bodies))
	}
}

func TestWorkerNonLeaderDoesNoCollectionOrNetwork(t *testing.T) {
	t.Parallel()
	collector := &fakeCollector{}
	sender := &fakeSender{}
	store := &fakeStore{leader: false}
	worker := &Worker{store: store, collector: collector, sender: sender, logger: slog.Default(), now: time.Now}
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	if _, err := worker.runOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if collector.calls != 0 || len(sender.bodies) != 0 {
		t.Fatalf("non-leader collector/sender calls = %d/%d", collector.calls, len(sender.bodies))
	}
}

func TestRetryBackoffIsDeterministicBoundedAndExponential(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("66666666-2222-3333-4444-555555555555")
	day := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	previous := time.Duration(0)
	for attempt := 1; attempt <= 20; attempt++ {
		got := retryBackoff(id, day, attempt)
		if got < time.Minute || got > 6*time.Hour {
			t.Fatalf("attempt %d backoff = %s", attempt, got)
		}
		if got != retryBackoff(id, day, attempt) {
			t.Fatalf("attempt %d backoff is not deterministic", attempt)
		}
		if attempt < 10 && got < previous {
			t.Fatalf("backoff decreased: attempt %d = %s after %s", attempt, got, previous)
		}
		previous = got
	}
	if got := retryBackoff(id, day, 20); got != 6*time.Hour {
		t.Fatalf("capped backoff = %s, want 6h", got)
	}
}

func TestWorkerFailureLogIsRateLimitedAndDataFree(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	worker := &Worker{logger: slog.New(slog.NewTextHandler(&output, nil))}
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	worker.logFailure(now)
	worker.logFailure(now.Add(30 * time.Minute))
	worker.logFailure(now.Add(time.Hour))

	logText := output.String()
	if got := bytes.Count(output.Bytes(), []byte("self-host telemetry attempt failed")); got != 2 {
		t.Fatalf("failure log count = %d, want 2: %s", got, logText)
	}
	for _, forbidden := range []string{"instance_id", "request", "response", "error", "body"} {
		if bytes.Contains(output.Bytes(), []byte(forbidden)) {
			t.Fatalf("failure log contains prohibited %q data: %s", forbidden, logText)
		}
	}
}

type blockingSender struct {
	started chan struct{}
	done    chan struct{}
	once    sync.Once
}

func (s *blockingSender) Send(ctx context.Context, _ []byte) deliveryDisposition {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	close(s.done)
	return deliveryRetry
}

func (*blockingSender) Close() {}

func TestWorkerCancellationCancelsInFlightDelivery(t *testing.T) {
	id := uuid.MustParse("77777777-2222-3333-4444-555555555555")
	today := utcDay(time.Now())
	now := scheduledAt(id, today).Add(time.Minute)
	sender := &blockingSender{started: make(chan struct{}), done: make(chan struct{})}
	worker, _ := testWorker(telemetryState{InstanceID: id}, &fakeCollector{}, &fakeSender{})
	worker.sender = sender
	worker.now = func() time.Time { return now }
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		worker.Run(ctx)
	}()

	select {
	case <-sender.started:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	cancel()
	select {
	case <-sender.done:
	case <-time.After(time.Second):
		t.Fatal("delivery context was not cancelled")
	}
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop promptly after cancellation")
	}
}
