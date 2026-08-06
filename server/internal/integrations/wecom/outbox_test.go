package wecom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeSendConn struct {
	calls int
	err   error
	code  int
}

func (f *fakeSendConn) SendRequest(context.Context, string, any) (Response, error) {
	f.calls++
	if f.err != nil {
		return Response{}, f.err
	}
	return Response{ErrCode: f.code}, nil
}

type fakeOutboxStore struct {
	rows        map[string]db.ChannelOutboundQueue
	attempts    int
	complete    int
	deferCount  int
	failCount   int
	retryCount  int
	lastFailErr string

	inst       db.ChannelInstallation
	instErr    error
	binding    db.ChannelChatSessionBinding
	bindingErr error
	session    db.ChatSession
	sessionErr error
}

func (f *fakeOutboxStore) ClaimChannelOutbound(_ context.Context, arg db.ClaimChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	for _, row := range f.rows {
		if row.Status == "queued" && util.UUIDToString(row.InstallationID) == util.UUIDToString(arg.InstallationID) {
			row.LeaseToken = pgtype.Text{String: "lease1", Valid: true}
			f.rows[util.UUIDToString(row.ID)] = row
			return row, nil
		}
	}
	return db.ChannelOutboundQueue{}, pgx.ErrNoRows
}

func (f *fakeOutboxStore) DeferClaimedChannelOutbound(context.Context, db.DeferClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	f.deferCount++
	return db.ChannelOutboundQueue{}, nil
}
func (f *fakeOutboxStore) RetryClaimedChannelOutbound(_ context.Context, arg db.RetryClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	f.retryCount++
	if arg.LastError.Valid {
		f.lastFailErr = arg.LastError.String
	}
	return db.ChannelOutboundQueue{}, nil
}
func (f *fakeOutboxStore) CompleteClaimedChannelOutbound(context.Context, db.CompleteClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	f.complete++
	return db.ChannelOutboundQueue{}, nil
}
func (f *fakeOutboxStore) FailClaimedChannelOutbound(_ context.Context, arg db.FailClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	f.failCount++
	if arg.LastError.Valid {
		f.lastFailErr = arg.LastError.String
	}
	return db.ChannelOutboundQueue{}, nil
}
func (f *fakeOutboxStore) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	if f.instErr != nil {
		return db.ChannelInstallation{}, f.instErr
	}
	if f.inst.Status != "" {
		return f.inst, nil
	}
	return db.ChannelInstallation{Status: "active"}, nil
}
func (f *fakeOutboxStore) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return db.Workspace{Slug: "acme"}, nil
}
func (f *fakeOutboxStore) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	if f.bindingErr != nil {
		return db.ChannelChatSessionBinding{}, f.bindingErr
	}
	return f.binding, nil
}
func (f *fakeOutboxStore) GetChatSession(context.Context, pgtype.UUID) (db.ChatSession, error) {
	if f.sessionErr != nil {
		return db.ChatSession{}, f.sessionErr
	}
	return f.session, nil
}

type fakeRateGate struct {
	deferAt time.Time
	ok      bool
}

func (f *fakeRateGate) Reserve(context.Context, db.ChannelOutboundQueue) (time.Time, bool, error) {
	if !f.ok {
		return f.deferAt, false, nil
	}
	return time.Time{}, true, nil
}

func TestOutboxConsumerCompletesOnSuccess(t *testing.T) {
	id := parseTestUUID(t, "11111111-1111-1111-1111-000000000001")
	inst := parseTestUUID(t, "22222222-2222-2222-2222-000000000001")
	store := &fakeOutboxStore{rows: map[string]db.ChannelOutboundQueue{
		util.UUIDToString(id): {
			ID:             id,
			InstallationID: inst,
			WorkspaceID:    inst,
			Status:         "queued",
			TargetChatID:   "user1",
			TargetChatType: 1,
			Payload:        []byte(`{"content":"hi"}`),
			PayloadVersion: 1,
		},
	}}
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := c.processOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !worked {
		t.Fatal("expected work")
	}
	if conn.calls != 1 || store.complete != 1 {
		t.Fatalf("calls=%d complete=%d", conn.calls, store.complete)
	}
}

func TestOutboxConsumerDefersRateWindow(t *testing.T) {
	id := parseTestUUID(t, "11111111-1111-1111-1111-000000000002")
	inst := parseTestUUID(t, "22222222-2222-2222-2222-000000000002")
	store := &fakeOutboxStore{rows: map[string]db.ChannelOutboundQueue{
		util.UUIDToString(id): {
			ID:             id,
			InstallationID: inst,
			WorkspaceID:    inst,
			Status:         "queued",
			TargetChatID:   "user1",
			TargetChatType: 1,
			Payload:        []byte(`{"content":"hi"}`),
		},
	}}
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: false, deferAt: time.Now().Add(time.Minute)},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.processOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if conn.calls != 0 || store.deferCount != 1 {
		t.Fatalf("calls=%d defer=%d", conn.calls, store.deferCount)
	}
}

type fakeBindingMinter struct{}

func (fakeBindingMinter) Mint(context.Context, pgtype.UUID, pgtype.UUID, string) (BindingToken, error) {
	return BindingToken{Raw: "tok123"}, nil
}

func TestOutboxBindingPromptAmbiguousCompletes(t *testing.T) {
	id := parseTestUUID(t, "11111111-1111-1111-1111-000000000003")
	inst := parseTestUUID(t, "22222222-2222-2222-2222-000000000003")
	store := &fakeOutboxStore{rows: map[string]db.ChannelOutboundQueue{
		util.UUIDToString(id): {
			ID:             id,
			InstallationID: inst,
			WorkspaceID:    inst,
			Status:         "queued",
			TargetChatID:   "user1",
			TargetChatType: 1,
			Payload:        []byte(`{"template":"binding_prompt"}`),
		},
	}}
	conn := &fakeSendConn{err: context.DeadlineExceeded}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
		AppURL:         "https://app.example.com",
		Binding:        fakeBindingMinter{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.processOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if store.complete != 1 {
		t.Fatalf("expected complete on ambiguous binding prompt, complete=%d", store.complete)
	}
}

// TestOutboxConsumerSendsWhenSessionBindingStillActive is the control case
// for Important Finding 3: a row with a non-null chat_session_id whose
// binding still points at this installation and whose session is still
// active must proceed to send normally.
func TestOutboxConsumerSendsWhenSessionBindingStillActive(t *testing.T) {
	id := parseTestUUID(t, "11111111-1111-1111-1111-000000000004")
	inst := parseTestUUID(t, "22222222-2222-2222-2222-000000000004")
	sess := parseTestUUID(t, "33333333-3333-3333-3333-000000000004")
	store := &fakeOutboxStore{
		rows: map[string]db.ChannelOutboundQueue{
			util.UUIDToString(id): {
				ID:             id,
				InstallationID: inst,
				WorkspaceID:    inst,
				ChatSessionID:  sess,
				Status:         "queued",
				TargetChatID:   "user1",
				TargetChatType: 1,
				Payload:        []byte(`{"content":"hi"}`),
				PayloadVersion: 1,
			},
		},
		binding: db.ChannelChatSessionBinding{InstallationID: inst, ChatSessionID: sess},
		session: db.ChatSession{ID: sess, Status: "active"},
	}
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := c.processOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !worked || conn.calls != 1 || store.complete != 1 || store.failCount != 0 {
		t.Fatalf("worked=%v calls=%d complete=%d failCount=%d", worked, conn.calls, store.complete, store.failCount)
	}
}

// TestOutboxConsumerFencesWhenSessionArchivedSinceEnqueue covers the
// Important Finding 3 review comment: the row was queued while the session
// was active, but by delivery time the session has been archived. Delivery
// must fence via FailClaimedChannelOutbound instead of sending.
func TestOutboxConsumerFencesWhenSessionArchivedSinceEnqueue(t *testing.T) {
	id := parseTestUUID(t, "11111111-1111-1111-1111-000000000005")
	inst := parseTestUUID(t, "22222222-2222-2222-2222-000000000005")
	sess := parseTestUUID(t, "33333333-3333-3333-3333-000000000005")
	store := &fakeOutboxStore{
		rows: map[string]db.ChannelOutboundQueue{
			util.UUIDToString(id): {
				ID:             id,
				InstallationID: inst,
				WorkspaceID:    inst,
				ChatSessionID:  sess,
				Status:         "queued",
				TargetChatID:   "user1",
				TargetChatType: 1,
				Payload:        []byte(`{"content":"hi"}`),
				PayloadVersion: 1,
			},
		},
		binding: db.ChannelChatSessionBinding{InstallationID: inst, ChatSessionID: sess},
		session: db.ChatSession{ID: sess, Status: "archived"},
	}
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := c.processOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !worked || conn.calls != 0 || store.complete != 0 || store.failCount != 1 {
		t.Fatalf("worked=%v calls=%d complete=%d failCount=%d", worked, conn.calls, store.complete, store.failCount)
	}
}

// TestOutboxConsumerFencesWhenBindingMovedToDifferentInstallation covers a
// rebind: the chat session's channel_chat_session_binding now points at a
// different installation than the one that claimed this queue row.
func TestOutboxConsumerFencesWhenBindingMovedToDifferentInstallation(t *testing.T) {
	id := parseTestUUID(t, "11111111-1111-1111-1111-000000000006")
	inst := parseTestUUID(t, "22222222-2222-2222-2222-000000000006")
	otherInst := parseTestUUID(t, "22222222-2222-2222-2222-000000000099")
	sess := parseTestUUID(t, "33333333-3333-3333-3333-000000000006")
	store := &fakeOutboxStore{
		rows: map[string]db.ChannelOutboundQueue{
			util.UUIDToString(id): {
				ID:             id,
				InstallationID: inst,
				WorkspaceID:    inst,
				ChatSessionID:  sess,
				Status:         "queued",
				TargetChatID:   "user1",
				TargetChatType: 1,
				Payload:        []byte(`{"content":"hi"}`),
				PayloadVersion: 1,
			},
		},
		binding: db.ChannelChatSessionBinding{InstallationID: otherInst, ChatSessionID: sess},
		session: db.ChatSession{ID: sess, Status: "active"},
	}
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := c.processOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !worked || conn.calls != 0 || store.complete != 0 || store.failCount != 1 {
		t.Fatalf("worked=%v calls=%d complete=%d failCount=%d", worked, conn.calls, store.complete, store.failCount)
	}
}

// TestOutboxConsumerFencesWhenBindingMissing covers the binding having been
// deleted entirely (unbind) since the row was enqueued.
func TestOutboxConsumerFencesWhenBindingMissing(t *testing.T) {
	id := parseTestUUID(t, "11111111-1111-1111-1111-000000000007")
	inst := parseTestUUID(t, "22222222-2222-2222-2222-000000000007")
	sess := parseTestUUID(t, "33333333-3333-3333-3333-000000000007")
	store := &fakeOutboxStore{
		rows: map[string]db.ChannelOutboundQueue{
			util.UUIDToString(id): {
				ID:             id,
				InstallationID: inst,
				WorkspaceID:    inst,
				ChatSessionID:  sess,
				Status:         "queued",
				TargetChatID:   "user1",
				TargetChatType: 1,
				Payload:        []byte(`{"content":"hi"}`),
				PayloadVersion: 1,
			},
		},
		bindingErr: pgx.ErrNoRows,
		session:    db.ChatSession{ID: sess, Status: "active"},
	}
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := c.processOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !worked || conn.calls != 0 || store.complete != 0 || store.failCount != 1 {
		t.Fatalf("worked=%v calls=%d complete=%d failCount=%d", worked, conn.calls, store.complete, store.failCount)
	}
}

// queuedRowFixture builds a single-row queued store for the delivery-path
// tests below.
func queuedRowFixture(t *testing.T, suffix string, sess pgtype.UUID) (pgtype.UUID, *fakeOutboxStore) {
	t.Helper()
	id := parseTestUUID(t, "11111111-1111-1111-1111-0000000000"+suffix)
	inst := parseTestUUID(t, "22222222-2222-2222-2222-0000000000"+suffix)
	return inst, &fakeOutboxStore{rows: map[string]db.ChannelOutboundQueue{
		util.UUIDToString(id): {
			ID:             id,
			InstallationID: inst,
			WorkspaceID:    inst,
			ChatSessionID:  sess,
			Status:         "queued",
			TargetChatID:   "user1",
			TargetChatType: 1,
			Payload:        []byte(`{"content":"hi"}`),
			PayloadVersion: 1,
		},
	}}
}

// TestOutboxConsumerRetriesTransientInstallationLoadError: a failed read of the
// installation row (pool timeout, connection reset) says nothing about whether
// the installation is still active, so the reply must be retried rather than
// terminal-failed. Terminal-failing here permanently drops a user-visible reply
// and records a misleading "installation inactive".
func TestOutboxConsumerRetriesTransientInstallationLoadError(t *testing.T) {
	inst, store := queuedRowFixture(t, "10", pgtype.UUID{})
	store.instErr = errors.New("read tcp: connection reset by peer")
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.retryCount != 1 || store.failCount != 0 || conn.calls != 0 {
		t.Fatalf("retry=%d fail=%d calls=%d", store.retryCount, store.failCount, conn.calls)
	}
}

// TestOutboxConsumerFailsWhenInstallationMissing is the control case: the
// installation row is genuinely gone, so no retry can ever succeed.
func TestOutboxConsumerFailsWhenInstallationMissing(t *testing.T) {
	inst, store := queuedRowFixture(t, "11", pgtype.UUID{})
	store.instErr = pgx.ErrNoRows
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.failCount != 1 || store.retryCount != 0 || conn.calls != 0 {
		t.Fatalf("fail=%d retry=%d calls=%d", store.failCount, store.retryCount, conn.calls)
	}
}

// TestOutboxConsumerFailsWhenInstallationRevoked keeps the revoke path terminal.
func TestOutboxConsumerFailsWhenInstallationRevoked(t *testing.T) {
	inst, store := queuedRowFixture(t, "12", pgtype.UUID{})
	store.inst = db.ChannelInstallation{Status: "revoked"}
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.failCount != 1 || store.retryCount != 0 || conn.calls != 0 {
		t.Fatalf("fail=%d retry=%d calls=%d", store.failCount, store.retryCount, conn.calls)
	}
	if store.lastFailErr != "installation inactive" {
		t.Fatalf("last_error=%q", store.lastFailErr)
	}
}

// TestOutboxConsumerRetriesTransientSessionCheckError: same conflation in the
// pre-send session fence. A failed binding/session read must not fence the send
// permanently.
func TestOutboxConsumerRetriesTransientSessionCheckError(t *testing.T) {
	sess := parseTestUUID(t, "33333333-3333-3333-3333-000000000013")
	inst, store := queuedRowFixture(t, "13", sess)
	store.bindingErr = errors.New("read tcp: connection reset by peer")
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.retryCount != 1 || store.failCount != 0 || conn.calls != 0 {
		t.Fatalf("retry=%d fail=%d calls=%d", store.retryCount, store.failCount, conn.calls)
	}
}

// TestOutboxConsumerRetriesTransientSessionLoadError covers the chat_session
// read specifically (binding resolved, session read failed).
func TestOutboxConsumerRetriesTransientSessionLoadError(t *testing.T) {
	sess := parseTestUUID(t, "33333333-3333-3333-3333-000000000014")
	inst, store := queuedRowFixture(t, "14", sess)
	store.binding = db.ChannelChatSessionBinding{InstallationID: inst, ChatSessionID: sess}
	store.sessionErr = errors.New("context deadline exceeded")
	conn := &fakeSendConn{}
	c, err := NewOutboxConsumer(OutboxConsumerConfig{
		InstallationID: util.UUIDToString(inst),
		Queries:        store,
		Rate:           &fakeRateGate{ok: true},
		Conn:           conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.processOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.retryCount != 1 || store.failCount != 0 || conn.calls != 0 {
		t.Fatalf("retry=%d fail=%d calls=%d", store.retryCount, store.failCount, conn.calls)
	}
}

// TestOutboxConsumerReportsDeliveryOutcomes pins the outbox side of the
// observability gap: every terminal and non-terminal transition has to be
// reported so "replies are being dropped" is visible without reading rows out
// of channel_outbound_queue by hand. A fence (target no longer deliverable) is
// deliberately a different outcome from a send failure.
func TestOutboxConsumerReportsDeliveryOutcomes(t *testing.T) {
	sess := parseTestUUID(t, "33333333-3333-3333-3333-000000000020")

	cases := []struct {
		name    string
		suffix  string
		session pgtype.UUID
		arrange func(store *fakeOutboxStore, conn *fakeSendConn)
		rate    *fakeRateGate
		want    string
	}{
		{
			name:   "sent",
			suffix: "20",
			rate:   &fakeRateGate{ok: true},
			want:   deliveryOutcomeSent,
		},
		{
			name:   "deferred by rate window",
			suffix: "21",
			rate:   &fakeRateGate{ok: false, deferAt: time.Now().Add(time.Minute)},
			want:   deliveryOutcomeDeferred,
		},
		{
			name:   "retried on a transient send error",
			suffix: "22",
			arrange: func(_ *fakeOutboxStore, conn *fakeSendConn) {
				conn.err = context.DeadlineExceeded
			},
			rate: &fakeRateGate{ok: true},
			want: deliveryOutcomeRetried,
		},
		{
			name:   "failed on a non-retryable errcode",
			suffix: "23",
			arrange: func(_ *fakeOutboxStore, conn *fakeSendConn) {
				conn.code = 40001
			},
			rate: &fakeRateGate{ok: true},
			want: deliveryOutcomeFailed,
		},
		{
			name:    "fenced when the installation was revoked",
			suffix:  "24",
			session: sess,
			arrange: func(store *fakeOutboxStore, _ *fakeSendConn) {
				store.inst = db.ChannelInstallation{Status: "revoked"}
			},
			rate: &fakeRateGate{ok: true},
			want: deliveryOutcomeFenced,
		},
		{
			name:    "fenced when the session was archived after enqueue",
			suffix:  "25",
			session: sess,
			arrange: func(store *fakeOutboxStore, _ *fakeSendConn) {
				store.session = db.ChatSession{ID: sess, Status: "archived"}
			},
			rate: &fakeRateGate{ok: true},
			want: deliveryOutcomeFenced,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst, store := queuedRowFixture(t, tc.suffix, tc.session)
			store.binding = db.ChannelChatSessionBinding{InstallationID: inst, ChatSessionID: tc.session}
			store.session = db.ChatSession{ID: tc.session, Status: "active"}
			conn := &fakeSendConn{}
			if tc.arrange != nil {
				tc.arrange(store, conn)
			}
			m := newRecordingMetrics()
			c, err := NewOutboxConsumer(OutboxConsumerConfig{
				InstallationID: util.UUIDToString(inst),
				Queries:        store,
				Rate:           tc.rate,
				Conn:           conn,
				Metrics:        m,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.processOne(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := m.delivered(tc.want); got != 1 {
				t.Fatalf("delivery outcome %q recorded %d times, want 1 (all: %v)", tc.want, got, m.deliveries)
			}
			total := 0
			for _, n := range m.deliveries {
				total += n
			}
			if total != 1 {
				t.Fatalf("expected exactly one outcome per attempt, got %v", m.deliveries)
			}
		})
	}
}

func TestClassifySendRetryable(t *testing.T) {
	if !classifySendRetryable(context.DeadlineExceeded, 0) {
		t.Fatal("deadline should retry")
	}
	if classifySendRetryable(errors.New("bad request"), 40001) {
		t.Fatal("unknown errcode should fail closed")
	}
}
