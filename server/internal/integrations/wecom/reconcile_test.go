package wecom

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	reconcileInstID = mustTestUUID("cccccccc-cccc-cccc-cccc-000000000010")
	reconcileSessID = mustTestUUID("bbbbbbbb-bbbb-bbbb-bbbb-000000000010")
	reconcileWsID   = mustTestUUID("dddddddd-dddd-dddd-dddd-000000000010")
)

type fakeReconcileStore struct {
	state      db.ChannelOutboundReconcileState
	candidates []db.ListWecomOutboundReconcileCandidatesRow
	advanced   bool
	enqueued   int
	enqueueErr error
	page       int
	failList   bool

	sentPurgedBefore   time.Time
	failedPurgedBefore time.Time

	// purgeRows simulates channel_outbound_queue terminal rows so purge
	// tests can assert status-scoped retention (spec §5.3.3) instead of
	// only recording the cutoff each purge call received.
	purgeRows []purgeRow
}

type purgeRow struct {
	id        string
	status    string
	updatedAt time.Time
}

func (f *fakeReconcileStore) ClaimChannelOutboundReconcileState(context.Context, db.ClaimChannelOutboundReconcileStateParams) (db.ChannelOutboundReconcileState, error) {
	if !f.state.LeaseToken.Valid {
		f.state.LeaseToken = pgtype.Text{String: "lease", Valid: true}
	}
	if !f.state.CursorAt.Valid {
		f.state.CursorAt = pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}
	}
	return f.state, nil
}

func (f *fakeReconcileStore) ListWecomOutboundReconcileCandidates(context.Context, db.ListWecomOutboundReconcileCandidatesParams) ([]db.ListWecomOutboundReconcileCandidatesRow, error) {
	if f.failList {
		return nil, pgx.ErrTxClosed
	}
	f.page++
	if f.page > 1 {
		return nil, nil
	}
	return f.candidates, nil
}

func (f *fakeReconcileStore) AdvanceChannelOutboundReconcileState(context.Context, db.AdvanceChannelOutboundReconcileStateParams) (db.ChannelOutboundReconcileState, error) {
	f.advanced = true
	return f.state, nil
}

func (f *fakeReconcileStore) ReleaseChannelOutboundReconcileState(context.Context, db.ReleaseChannelOutboundReconcileStateParams) error {
	return nil
}

func (f *fakeReconcileStore) FailUndeliverableChannelOutbound(context.Context) error { return nil }
func (f *fakeReconcileStore) PurgeChannelOutboundSendAttemptsBefore(context.Context, pgtype.Timestamptz) error {
	return nil
}
func (f *fakeReconcileStore) PurgeSentChannelOutboundQueueBefore(_ context.Context, cutoff pgtype.Timestamptz) error {
	f.sentPurgedBefore = cutoff.Time
	f.purgeRows = deleteMatchingRows(f.purgeRows, "sent", cutoff.Time)
	return nil
}
func (f *fakeReconcileStore) PurgeFailedChannelOutboundQueueBefore(_ context.Context, cutoff pgtype.Timestamptz) error {
	f.failedPurgedBefore = cutoff.Time
	f.purgeRows = deleteMatchingRows(f.purgeRows, "failed", cutoff.Time)
	return nil
}

func deleteMatchingRows(rows []purgeRow, status string, cutoff time.Time) []purgeRow {
	kept := make([]purgeRow, 0, len(rows))
	for _, row := range rows {
		if row.status == status && row.updatedAt.Before(cutoff) {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}

func (f *fakeReconcileStore) hasRow(id string) bool {
	for _, row := range f.purgeRows {
		if row.id == id {
			return true
		}
	}
	return false
}

func (f *fakeReconcileStore) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return db.AgentTaskQueue{ChatInputTaskID: pgtype.UUID{}}, nil
}
func (f *fakeReconcileStore) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return true, nil
}
func (f *fakeReconcileStore) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	return db.ChannelChatSessionBinding{
		InstallationID: reconcileInstID,
		ChatSessionID:  reconcileSessID,
		Config:         []byte(`{"target_chat_id":"u","target_chat_type":1}`),
	}, nil
}
func (f *fakeReconcileStore) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{ID: reconcileInstID, WorkspaceID: reconcileWsID, Status: "active"}, nil
}
func (f *fakeReconcileStore) GetAgent(context.Context, pgtype.UUID) (db.Agent, error) {
	return db.Agent{Name: "A"}, nil
}
func (f *fakeReconcileStore) GetChatMessageByTaskAssistant(context.Context, pgtype.UUID) (db.ChatMessage, error) {
	return db.ChatMessage{Content: "done"}, nil
}
func (f *fakeReconcileStore) EnqueueChannelOutbound(context.Context, db.EnqueueChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	if f.enqueueErr != nil {
		return db.ChannelOutboundQueue{}, f.enqueueErr
	}
	f.enqueued++
	return db.ChannelOutboundQueue{}, nil
}

func TestReconcilerAdvancesAfterFullWindow(t *testing.T) {
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000010")
	store := &fakeReconcileStore{
		candidates: []db.ListWecomOutboundReconcileCandidatesRow{{
			TaskID:        taskID,
			ChatSessionID: reconcileSessID,
			TaskStatus:    "completed",
			CompletedAt:   pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		}},
	}
	r := NewOutboundReconciler(OutboundReconcilerConfig{Queries: store, Now: time.Now})
	r.sweep(context.Background())
	if !store.advanced {
		t.Fatal("cursor not advanced")
	}
	if store.enqueued != 1 {
		t.Fatalf("enqueued=%d", store.enqueued)
	}
}

func TestReconcilerDBFailureDoesNotAdvance(t *testing.T) {
	store := &fakeReconcileStore{failList: true}
	r := NewOutboundReconciler(OutboundReconcilerConfig{Queries: store})
	r.sweep(context.Background())
	if store.advanced {
		t.Fatal("cursor advanced on failure")
	}
}

func TestReconcilerMaintenanceSkipsEnqueueStillAdvances(t *testing.T) {
	store := &fakeReconcileStore{}
	r := NewOutboundReconciler(OutboundReconcilerConfig{
		Queries:   store,
		EnqueueOK: func() bool { return false },
	})
	r.sweep(context.Background())
	if !store.advanced {
		t.Fatal("expected cursor advance in maintenance mode")
	}
	if store.enqueued != 0 {
		t.Fatal("should not enqueue in maintenance mode")
	}
}

func TestReconcilerWaitWithTimeout(t *testing.T) {
	r := NewOutboundReconciler(OutboundReconcilerConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()
	if !r.WaitWithTimeout(time.Second) {
		t.Fatal("worker did not exit")
	}
}

// TestReconcilerPurgeKeepsFailedRowsPastSentRetention exercises Critical
// Finding 2 from the Task 10 review: sent rows retain 24h while failed rows
// retain 7d. A failed row older than 24h but younger than 7d must survive
// the sent-status purge pass and only disappear once it crosses 7d.
func TestReconcilerPurgeKeepsFailedRowsPastSentRetention(t *testing.T) {
	now := time.Now()
	store := &fakeReconcileStore{
		purgeRows: []purgeRow{
			{id: "sent-old", status: "sent", updatedAt: now.Add(-48 * time.Hour)},
			{id: "sent-recent", status: "sent", updatedAt: now.Add(-time.Hour)},
			{id: "failed-old", status: "failed", updatedAt: now.Add(-48 * time.Hour)},
			{id: "failed-ancient", status: "failed", updatedAt: now.Add(-8 * 24 * time.Hour)},
		},
	}
	r := NewOutboundReconciler(OutboundReconcilerConfig{Queries: store, Now: func() time.Time { return now }})

	if err := r.purge(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}

	if store.hasRow("sent-old") {
		t.Fatal("sent row older than 24h retention should have been purged")
	}
	if !store.hasRow("sent-recent") {
		t.Fatal("sent row within 24h retention should survive")
	}
	if !store.hasRow("failed-old") {
		t.Fatal("failed row older than 24h but younger than 7d must survive the sent purge")
	}
	if store.hasRow("failed-ancient") {
		t.Fatal("failed row older than 7d retention should have been purged")
	}
}

func mustTestUUID(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return id
}

// TestReconcilerRescueIsReportedOnItsOwnPath is the alerting contract: a
// reconciler enqueue means the realtime path missed the event, so it must be
// attributable separately from a fast-path enqueue. Without this signal a dead
// fast path looks identical to a healthy one, just 30+ seconds slower.
func TestReconcilerRescueIsReportedOnItsOwnPath(t *testing.T) {
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000011")
	store := &fakeReconcileStore{
		candidates: []db.ListWecomOutboundReconcileCandidatesRow{{
			TaskID:        taskID,
			ChatSessionID: reconcileSessID,
			TaskStatus:    "completed",
			CompletedAt:   pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		}},
	}
	m := newRecordingMetrics()
	r := NewOutboundReconciler(OutboundReconcilerConfig{Queries: store, Metrics: m, Now: time.Now})
	r.sweep(context.Background())

	if got := m.enqueues(enqueuePathReconcile, sourceKindChatDone); got != 1 {
		t.Fatalf("reconcile-path enqueues=%d, want 1", got)
	}
	if got := m.enqueues(enqueuePathFast, sourceKindChatDone); got != 0 {
		t.Fatalf("fast-path enqueues=%d, want 0", got)
	}
	if got := m.raceLosses(); got != 0 {
		t.Fatalf("race losses=%d, want 0", got)
	}
}

// TestReconcilerRaceLossIsNotCountedAsRescue keeps the alerting signal quiet in
// the healthy case: the fast path already enqueued this reply, so the
// business-key conflict is expected noise, not a missed event.
func TestReconcilerRaceLossIsNotCountedAsRescue(t *testing.T) {
	taskID := parseTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-000000000012")
	store := &fakeReconcileStore{
		candidates: []db.ListWecomOutboundReconcileCandidatesRow{{
			TaskID:        taskID,
			ChatSessionID: reconcileSessID,
			TaskStatus:    "completed",
			CompletedAt:   pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		}},
		enqueueErr: pgx.ErrNoRows,
	}
	m := newRecordingMetrics()
	r := NewOutboundReconciler(OutboundReconcilerConfig{Queries: store, Metrics: m, Now: time.Now})
	r.sweep(context.Background())

	if !store.advanced {
		t.Fatal("a business-key conflict is not an error; the cursor must still advance")
	}
	if got := m.enqueues(enqueuePathReconcile, sourceKindChatDone); got != 0 {
		t.Fatalf("reconcile-path enqueues=%d, want 0 for a lost race", got)
	}
	if got := m.raceLosses(); got != 1 {
		t.Fatalf("race losses=%d, want 1", got)
	}
}
