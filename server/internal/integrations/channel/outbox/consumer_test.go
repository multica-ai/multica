package outbox

// consumer_test.go — the settle decisions the Consumer makes for one claimed
// row. Everything runs against a fake store, so no database is required: what
// is under test is the policy (when to complete, retry, dead-letter, fence, or
// defer), not the SQL.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	testInstallationID = "11111111-1111-1111-1111-111111111111"
	testChannelType    = "wecom"
	testLease          = "lease-token-1"
)

func uuidFrom(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

// fakeConsumerStore records every settle call so a test can assert which one
// the Consumer chose.
type fakeConsumerStore struct {
	claim    []db.ChannelOutboundQueue
	claimErr error

	installation    db.ChannelInstallation
	installationErr error

	binding    db.ChannelChatSessionBinding
	bindingErr error

	session    db.ChatSession
	sessionErr error

	deferred  []db.DeferClaimedChannelOutboundParams
	retried   []db.RetryClaimedChannelOutboundParams
	completed []db.CompleteClaimedChannelOutboundParams
	failed    []db.FailClaimedChannelOutboundParams
}

func (s *fakeConsumerStore) ClaimChannelOutbound(context.Context, db.ClaimChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	if s.claimErr != nil {
		return db.ChannelOutboundQueue{}, s.claimErr
	}
	if len(s.claim) == 0 {
		return db.ChannelOutboundQueue{}, pgx.ErrNoRows
	}
	row := s.claim[0]
	s.claim = s.claim[1:]
	return row, nil
}

func (s *fakeConsumerStore) DeferClaimedChannelOutbound(_ context.Context, arg db.DeferClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	s.deferred = append(s.deferred, arg)
	return db.ChannelOutboundQueue{}, nil
}

func (s *fakeConsumerStore) RetryClaimedChannelOutbound(_ context.Context, arg db.RetryClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	s.retried = append(s.retried, arg)
	return db.ChannelOutboundQueue{}, nil
}

// The settle fakes honour ctx because pgx does: a cancelled context fails the
// UPDATE. Without that a test cannot tell a settle that survived a shutdown from
// one that never ran.
func (s *fakeConsumerStore) CompleteClaimedChannelOutbound(ctx context.Context, arg db.CompleteClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	if err := ctx.Err(); err != nil {
		return db.ChannelOutboundQueue{}, err
	}
	s.completed = append(s.completed, arg)
	return db.ChannelOutboundQueue{}, nil
}

func (s *fakeConsumerStore) FailClaimedChannelOutbound(_ context.Context, arg db.FailClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	s.failed = append(s.failed, arg)
	return db.ChannelOutboundQueue{}, nil
}

func (s *fakeConsumerStore) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return s.installation, s.installationErr
}

func (s *fakeConsumerStore) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	return s.binding, s.bindingErr
}

func (s *fakeConsumerStore) GetChatSession(context.Context, pgtype.UUID) (db.ChatSession, error) {
	return s.session, s.sessionErr
}

// fakeSender returns a scripted disposition and records what it was handed.
type fakeSender struct {
	disposition Disposition
	err         error
	sent        []db.ChannelOutboundQueue
	// onSend runs after the row is recorded and before the disposition is
	// returned, so a test can make something happen "mid-send" — a shutdown,
	// for instance.
	onSend func()
}

func (f *fakeSender) Send(_ context.Context, row db.ChannelOutboundQueue) (Disposition, error) {
	f.sent = append(f.sent, row)
	if f.onSend != nil {
		f.onSend()
	}
	return f.disposition, f.err
}

// recordingMetrics counts delivery outcomes by name. Mutex-guarded because the
// MetricsRef tests record from several goroutines at once.
type recordingMetrics struct {
	mu         sync.Mutex
	delivery   map[string]int
	enqueued   int
	raceLosses int
}

func newRecordingMetrics() *recordingMetrics {
	return &recordingMetrics{delivery: map[string]int{}}
}

func (m *recordingMetrics) RecordEnqueued(string, string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enqueued++
}

func (m *recordingMetrics) RecordDelivery(_, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delivery[outcome]++
}

func (m *recordingMetrics) RecordReconcileRaceLost(string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.raceLosses++
}

// activeRow is a claimed, leased row on an active installation with no session.
func activeRow(t *testing.T, attempts int32) db.ChannelOutboundQueue {
	t.Helper()
	return db.ChannelOutboundQueue{
		ID:             uuidFrom(t, "22222222-2222-2222-2222-222222222222"),
		InstallationID: uuidFrom(t, testInstallationID),
		WorkspaceID:    uuidFrom(t, "33333333-3333-3333-3333-333333333333"),
		ChannelType:    testChannelType,
		SourceKind:     "chat_done",
		SourceID:       "task-1",
		TargetChatID:   "CHAT_1",
		TargetChatType: 2,
		MsgType:        "markdown",
		PayloadVersion: 1,
		Payload:        []byte(`{"content":"hi"}`),
		Status:         "queued",
		Attempts:       attempts,
		LeaseToken:     pgtype.Text{String: testLease, Valid: true},
	}
}

func newTestConsumer(t *testing.T, store *fakeConsumerStore, sender Sender, rate RateGate, m Metrics) *Consumer {
	t.Helper()
	c, err := NewConsumer(ConsumerConfig{
		InstallationID: testInstallationID,
		ChannelType:    testChannelType,
		Queries:        store,
		Sender:         sender,
		Rate:           rate,
		Metrics:        m,
	})
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	return c
}

func TestConsumer_SentRowIsCompleted(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{activeRow(t, 0)},
		installation: db.ChannelInstallation{Status: "active"},
	}
	sender := &fakeSender{disposition: DispositionSent}
	m := newRecordingMetrics()
	c := newTestConsumer(t, store, sender, nil, m)

	worked, err := c.processOne(context.Background())
	if err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if !worked {
		t.Error("processOne reported no work despite claiming a row")
	}
	if len(store.completed) != 1 {
		t.Fatalf("completed %d rows, want 1", len(store.completed))
	}
	// The lease token must round-trip: the settle is what proves this worker
	// still owns the row.
	if store.completed[0].LeaseToken.String != testLease {
		t.Errorf("settle lease = %q, want %q", store.completed[0].LeaseToken.String, testLease)
	}
	if m.delivery[OutcomeSent] != 1 {
		t.Errorf("sent outcomes = %d, want 1", m.delivery[OutcomeSent])
	}
}

// A send whose outcome is unknown but must not be repeated settles as sent.
// This is the one path where "sent" does not mean "confirmed" — retrying it
// would risk delivering a one-shot credential twice.
func TestConsumer_AmbiguousSendSettlesAsSent(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{activeRow(t, 0)},
		installation: db.ChannelInstallation{Status: "active"},
	}
	sender := &fakeSender{disposition: DispositionSent, err: errors.New("write timeout")}
	c := newTestConsumer(t, store, sender, nil, nil)

	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.completed) != 1 {
		t.Errorf("completed %d rows, want 1", len(store.completed))
	}
	if len(store.retried) != 0 || len(store.failed) != 0 {
		t.Error("an ambiguous send must not be retried or dead-lettered")
	}
}

func TestConsumer_RetryableSendSchedulesBackoff(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{activeRow(t, 0)},
		installation: db.ChannelInstallation{Status: "active"},
	}
	sender := &fakeSender{disposition: DispositionRetry, err: errors.New("connection not ready")}
	m := newRecordingMetrics()
	c := newTestConsumer(t, store, sender, nil, m)

	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.retried) != 1 {
		t.Fatalf("retried %d rows, want 1", len(store.retried))
	}
	if !store.retried[0].NextAttemptAt.Valid || !store.retried[0].NextAttemptAt.Time.After(time.Now()) {
		t.Error("retry must schedule next_attempt_at in the future")
	}
	if store.retried[0].LastError.String == "" {
		t.Error("retry must record last_error for triage")
	}
	if m.delivery[OutcomeRetried] != 1 {
		t.Errorf("retried outcomes = %d, want 1", m.delivery[OutcomeRetried])
	}
}

// The retry budget is bounded: the attempt that would reach MaxAttempts
// dead-letters instead of scheduling yet another try.
func TestConsumer_RetryBudgetExhaustionDeadLetters(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{activeRow(t, MaxAttempts-1)},
		installation: db.ChannelInstallation{Status: "active"},
	}
	sender := &fakeSender{disposition: DispositionRetry, err: errors.New("still broken")}
	m := newRecordingMetrics()
	c := newTestConsumer(t, store, sender, nil, m)

	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.retried) != 0 {
		t.Error("a row at the attempt ceiling must not be retried again")
	}
	if len(store.failed) != 1 {
		t.Fatalf("dead-lettered %d rows, want 1", len(store.failed))
	}
	if m.delivery[OutcomeFailed] != 1 {
		t.Errorf("failed outcomes = %d, want 1", m.delivery[OutcomeFailed])
	}
}

func TestConsumer_TerminalSendDeadLettersImmediately(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{activeRow(t, 0)},
		installation: db.ChannelInstallation{Status: "active"},
	}
	sender := &fakeSender{disposition: DispositionFailed, err: errors.New("unknown template")}
	c := newTestConsumer(t, store, sender, nil, nil)

	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.retried) != 0 {
		t.Error("a terminal disposition must not consume retries")
	}
	if len(store.failed) != 1 {
		t.Errorf("dead-lettered %d rows, want 1", len(store.failed))
	}
}

func TestConsumer_InactiveInstallationIsFenced(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{activeRow(t, 0)},
		installation: db.ChannelInstallation{Status: "revoked"},
	}
	sender := &fakeSender{disposition: DispositionSent}
	m := newRecordingMetrics()
	c := newTestConsumer(t, store, sender, nil, m)

	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Error("a revoked installation must be fenced before Send")
	}
	if m.delivery[OutcomeFenced] != 1 {
		t.Errorf("fenced outcomes = %d, want 1", m.delivery[OutcomeFenced])
	}
}

// A missing installation row is terminal, but a read that merely FAILED says
// nothing about whether the installation is still active — so it retries rather
// than permanently dropping a user-visible reply.
func TestConsumer_InstallationReadFailureRetries(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:           []db.ChannelOutboundQueue{activeRow(t, 0)},
		installationErr: errors.New("connection refused"),
	}
	c := newTestConsumer(t, store, &fakeSender{disposition: DispositionSent}, nil, nil)

	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.retried) != 1 {
		t.Errorf("retried %d rows, want 1 — a failed read must not dead-letter", len(store.retried))
	}
	if len(store.failed) != 0 {
		t.Error("a failed installation read must not be treated as a missing installation")
	}
}

func TestConsumer_MissingInstallationIsFenced(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:           []db.ChannelOutboundQueue{activeRow(t, 0)},
		installationErr: pgx.ErrNoRows,
	}
	m := newRecordingMetrics()
	c := newTestConsumer(t, store, &fakeSender{disposition: DispositionSent}, nil, m)

	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if m.delivery[OutcomeFenced] != 1 {
		t.Errorf("fenced outcomes = %d, want 1", m.delivery[OutcomeFenced])
	}
}

// The claim query already fences on the session, but claim and send are not one
// transaction: an unbind, rebind, or archive in between must stop the send
// rather than deliver into a conversation the target no longer owns.
func TestConsumer_SessionFencingBeforeSend(t *testing.T) {
	t.Parallel()
	sessionID := uuidFrom(t, "44444444-4444-4444-4444-444444444444")
	installationID := uuidFrom(t, testInstallationID)
	otherInstallation := uuidFrom(t, "99999999-9999-9999-9999-999999999999")

	cases := []struct {
		name    string
		store   *fakeConsumerStore
		wantErr bool // true = retried (check failed), false = fenced
	}{
		{
			name: "binding removed",
			store: &fakeConsumerStore{
				installation: db.ChannelInstallation{Status: "active"},
				bindingErr:   pgx.ErrNoRows,
			},
		},
		{
			name: "rebound to another installation",
			store: &fakeConsumerStore{
				installation: db.ChannelInstallation{Status: "active"},
				binding:      db.ChannelChatSessionBinding{InstallationID: otherInstallation},
			},
		},
		{
			name: "session archived",
			store: &fakeConsumerStore{
				installation: db.ChannelInstallation{Status: "active"},
				binding:      db.ChannelChatSessionBinding{InstallationID: installationID},
				session:      db.ChatSession{Status: "archived"},
			},
		},
		{
			name: "session missing",
			store: &fakeConsumerStore{
				installation: db.ChannelInstallation{Status: "active"},
				binding:      db.ChannelChatSessionBinding{InstallationID: installationID},
				sessionErr:   pgx.ErrNoRows,
			},
		},
		{
			name: "binding read failed",
			store: &fakeConsumerStore{
				installation: db.ChannelInstallation{Status: "active"},
				bindingErr:   errors.New("connection refused"),
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := activeRow(t, 0)
			row.ChatSessionID = sessionID
			tc.store.claim = []db.ChannelOutboundQueue{row}
			sender := &fakeSender{disposition: DispositionSent}
			m := newRecordingMetrics()
			c := newTestConsumer(t, tc.store, sender, nil, m)

			if _, err := c.processOne(context.Background()); err != nil {
				t.Fatalf("processOne: %v", err)
			}
			if len(sender.sent) != 0 {
				t.Fatal("an undeliverable session must be fenced before Send")
			}
			if tc.wantErr {
				if len(tc.store.retried) != 1 {
					t.Errorf("retried %d rows, want 1 — a failed check must retry, not fence", len(tc.store.retried))
				}
				return
			}
			if m.delivery[OutcomeFenced] != 1 {
				t.Errorf("fenced outcomes = %d, want 1", m.delivery[OutcomeFenced])
			}
		})
	}
}

// deferGate refuses admission until deferUntil.
type deferGate struct {
	until  time.Time
	err    error
	called int
}

func (g *deferGate) Reserve(context.Context, db.ChannelOutboundQueue) (time.Time, bool, error) {
	g.called++
	if g.err != nil {
		return time.Time{}, false, g.err
	}
	return g.until, false, nil
}

// A row held back by the gate has not failed: deferring must not spend an
// attempt, or a target that is merely busy would eventually be dead-lettered
// for a message that was never tried.
func TestConsumer_RateDeferralDoesNotSpendAnAttempt(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{activeRow(t, 0)},
		installation: db.ChannelInstallation{Status: "active"},
	}
	sender := &fakeSender{disposition: DispositionSent}
	gate := &deferGate{until: time.Now().Add(time.Minute)}
	m := newRecordingMetrics()
	c := newTestConsumer(t, store, sender, gate, m)

	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.deferred) != 1 {
		t.Fatalf("deferred %d rows, want 1", len(store.deferred))
	}
	if len(store.retried) != 0 {
		t.Error("a deferral must not bump attempts")
	}
	if len(sender.sent) != 0 {
		t.Error("a deferred row must not be sent")
	}
	if !store.deferred[0].NextAttemptAt.Time.Equal(gate.until) {
		t.Errorf("next_attempt_at = %v, want the gate's deferUntil %v", store.deferred[0].NextAttemptAt.Time, gate.until)
	}
	if m.delivery[OutcomeDeferred] != 1 {
		t.Errorf("deferred outcomes = %d, want 1", m.delivery[OutcomeDeferred])
	}
}

// A gate that cannot answer is neither a grant nor a failure: leave the row
// claimed and let the lease expire so another pass reclaims it.
func TestConsumer_RateGateErrorLeavesRowClaimed(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{activeRow(t, 0)},
		installation: db.ChannelInstallation{Status: "active"},
	}
	sender := &fakeSender{disposition: DispositionSent}
	c := newTestConsumer(t, store, sender, &deferGate{err: errors.New("lock timeout")}, nil)

	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.deferred)+len(store.retried)+len(store.failed)+len(store.completed) != 0 {
		t.Error("a gate error must not settle the row at all")
	}
	if len(sender.sent) != 0 {
		t.Error("a gate error must not admit the row")
	}
}

func TestConsumer_NilRateGateAdmitsEveryRow(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{activeRow(t, 0)},
		installation: db.ChannelInstallation{Status: "active"},
	}
	sender := &fakeSender{disposition: DispositionSent}
	c := newTestConsumer(t, store, sender, nil, nil)

	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Errorf("sent %d rows, want 1 — a nil gate must admit everything", len(sender.sent))
	}
}

func TestConsumer_EmptyQueueReportsNoWork(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{}
	c := newTestConsumer(t, store, &fakeSender{disposition: DispositionSent}, nil, nil)

	worked, err := c.processOne(context.Background())
	if err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if worked {
		t.Error("an empty queue must report no work so Run goes back to waiting")
	}
}

// A row claimed without a lease token cannot be settled — every settle query
// matches on it — so it must be reported rather than silently sent.
func TestConsumer_ClaimedRowWithoutLeaseIsRejected(t *testing.T) {
	t.Parallel()
	row := activeRow(t, 0)
	row.LeaseToken = pgtype.Text{}
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{row},
		installation: db.ChannelInstallation{Status: "active"},
	}
	sender := &fakeSender{disposition: DispositionSent}
	c := newTestConsumer(t, store, sender, nil, nil)

	worked, err := c.processOne(context.Background())
	if err == nil {
		t.Error("expected an error for a claimed row with no lease token")
	}
	if !worked {
		t.Error("the row was claimed, so processOne must report work")
	}
	if len(sender.sent) != 0 {
		t.Error("an unsettleable row must not be sent")
	}
}

func TestNewConsumer_RejectsIncompleteConfig(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{}
	sender := &fakeSender{}
	cases := []struct {
		name string
		cfg  ConsumerConfig
	}{
		{"bad installation id", ConsumerConfig{InstallationID: "nope", ChannelType: testChannelType, Queries: store, Sender: sender}},
		{"no channel type", ConsumerConfig{InstallationID: testInstallationID, Queries: store, Sender: sender}},
		{"no queries", ConsumerConfig{InstallationID: testInstallationID, ChannelType: testChannelType, Sender: sender}},
		{"no sender", ConsumerConfig{InstallationID: testInstallationID, ChannelType: testChannelType, Queries: store}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewConsumer(tc.cfg); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// Backoff is full jitter over an exponentially growing window, capped. The cap
// is what keeps a long-broken target from drifting to hour-long waits.
func TestBackoff_StaysWithinCap(t *testing.T) {
	t.Parallel()
	const cap = 5 * time.Minute
	for attempt := int32(0); attempt <= 20; attempt++ {
		for range 50 {
			d := Backoff(attempt)
			if d < 0 {
				t.Fatalf("attempt %d: negative delay %v", attempt, d)
			}
			if d > cap {
				t.Fatalf("attempt %d: delay %v exceeds the %v cap", attempt, d, cap)
			}
		}
	}
}

func TestSanitizeLastError_Truncates(t *testing.T) {
	t.Parallel()
	long := make([]byte, maxLastErrorBytes*2)
	for i := range long {
		long[i] = 'x'
	}
	got := sanitizeLastError(string(long))
	if len(got) != maxLastErrorBytes {
		t.Errorf("len = %d, want %d", len(got), maxLastErrorBytes)
	}
	if got := sanitizeLastError("  spaced  "); got != "spaced" {
		t.Errorf("sanitizeLastError = %q, want spaced", got)
	}
}

// A shutdown landing between a successful send and the settling UPDATE must not
// leave the row queued. The content is already in the user's chat; the next
// lease holder would claim the same row and deliver it a second time, and no
// operator would ever see a failure to explain the duplicate.
//
// The settle therefore runs on a context the consumer's own cancellation cannot
// reach — bounded, so a wedged database cannot hold shutdown open either.
func TestConsumer_SettlesASentRowEvenWhenTheConsumerIsCancelled(t *testing.T) {
	t.Parallel()
	store := &fakeConsumerStore{
		claim:        []db.ChannelOutboundQueue{activeRow(t, 0)},
		installation: db.ChannelInstallation{Status: "active"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	// The frame left the socket, and then the process was told to stop.
	sender := &fakeSender{disposition: DispositionSent, onSend: cancel}
	c := newTestConsumer(t, store, sender, nil, newRecordingMetrics())

	if _, err := c.processOne(ctx); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	if len(store.completed) != 1 {
		t.Fatalf("settled %d rows, want 1 — a cancelled shutdown left a delivered row queued, "+
			"so the next holder will send it again", len(store.completed))
	}
	if store.completed[0].LeaseToken.String != testLease {
		t.Errorf("settle lease = %q, want %q", store.completed[0].LeaseToken.String, testLease)
	}
}
