package outbound

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

type fakeNotificationStore struct {
	claimed         []OutboxNotification
	reclaimed       []OutboxNotification
	claimErr        error
	claimReadyIDs   []string
	reclaimReadyIDs []string
	claimCalls      int
	reclaimCalls    int
	sent            []pgtype.UUID
	retried         []pgtype.UUID
	dead            []pgtype.UUID
}

func (f *fakeNotificationStore) ClaimDue(_ context.Context, _ int32, readyConnectionIDs []string) ([]OutboxNotification, error) {
	f.claimCalls++
	f.claimReadyIDs = append([]string(nil), readyConnectionIDs...)
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	return f.claimed, nil
}

func (f *fakeNotificationStore) ReclaimStaleProcessing(_ context.Context, _ int32, _ time.Duration, readyConnectionIDs []string) ([]OutboxNotification, error) {
	f.reclaimCalls++
	f.reclaimReadyIDs = append([]string(nil), readyConnectionIDs...)
	return f.reclaimed, nil
}

func (f *fakeNotificationStore) MarkSent(_ context.Context, ids []pgtype.UUID) error {
	f.sent = append(f.sent, ids...)
	return nil
}

func (f *fakeNotificationStore) ScheduleRetry(_ context.Context, ids []pgtype.UUID, _ string, _ time.Duration) error {
	f.retried = append(f.retried, ids...)
	return nil
}

func (f *fakeNotificationStore) MarkDead(_ context.Context, ids []pgtype.UUID, _ string) error {
	f.dead = append(f.dead, ids...)
	return nil
}

func (f *fakeNotificationStore) Cleanup(context.Context) error { return nil }

type mockRetrySender struct {
	err   error
	calls []mockRetrySendCall
}

type mockRetrySendCall struct {
	ConnectionID string
	Target       port.OutboundTarget
	Payload      RetryPayload
}

func (m *mockRetrySender) SendCard(_ context.Context, connectionID string, target port.OutboundTarget, card RetryPayload) error {
	m.calls = append(m.calls, mockRetrySendCall{ConnectionID: connectionID, Target: target, Payload: card})
	return m.err
}

func TestOutboxWorker_MergesDueNotificationsAndMarksSent(t *testing.T) {
	t.Parallel()

	userID := pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}
	store := &fakeNotificationStore{claimed: []OutboxNotification{
		{ID: pgtype.UUID{Bytes: [16]byte{0x11}, Valid: true}, Provider: "feishu", EventKind: "issue_assigned", TargetUserID: userID, TargetExternalUserID: "ou_1", Title: "A", Body: "body A", MaxAttempts: 3},
		{ID: pgtype.UUID{Bytes: [16]byte{0x12}, Valid: true}, Provider: "feishu", EventKind: "issue_assigned", TargetUserID: userID, TargetExternalUserID: "ou_1", Title: "B", Body: "body B", MaxAttempts: 3},
	}}
	sender := &mockRetrySender{}
	worker := NewOutboxWorker(store, sender)

	worker.processBatch(context.Background())

	if len(sender.calls) != 1 {
		t.Fatalf("send calls = %d, want 1", len(sender.calls))
	}
	if sender.calls[0].Payload.Title != "Multica 有 2 条新通知" {
		t.Fatalf("title = %q", sender.calls[0].Payload.Title)
	}
	if !strings.Contains(sender.calls[0].Payload.Body, "[1] A: body A") ||
		!strings.Contains(sender.calls[0].Payload.Body, "[2] B: body B") {
		t.Fatalf("merged body = %q", sender.calls[0].Payload.Body)
	}
	if len(store.sent) != 2 {
		t.Fatalf("sent ids = %d, want 2", len(store.sent))
	}
}

func TestOutboxWorker_MergesReplyableNotificationsInGroupPolicy(t *testing.T) {
	t.Parallel()

	userID := pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}
	store := &fakeNotificationStore{claimed: []OutboxNotification{
		{ID: pgtype.UUID{Bytes: [16]byte{0x13}, Valid: true}, Provider: "feishu", EventKind: "mentioned", TargetUserID: userID, TargetExternalUserID: "ou_1", Title: "A", Body: "body A", Replyable: true, MaxAttempts: 3},
		{ID: pgtype.UUID{Bytes: [16]byte{0x14}, Valid: true}, Provider: "feishu", EventKind: "mentioned", TargetUserID: userID, TargetExternalUserID: "ou_1", Title: "B", Body: "body B", Replyable: true, MaxAttempts: 3},
	}}
	sender := &mockRetrySender{}
	worker := NewOutboxWorker(store, sender)

	worker.processBatch(context.Background())

	if len(sender.calls) != 1 {
		t.Fatalf("send calls = %d, want 1", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].Payload.Body, "[1] A: body A") ||
		!strings.Contains(sender.calls[0].Payload.Body, "[2] B: body B") {
		t.Fatalf("body = %q, want merged replyable notifications", sender.calls[0].Payload.Body)
	}
	if len(store.sent) != 2 {
		t.Fatalf("sent ids = %d, want 2", len(store.sent))
	}
}

func TestOutboxWorker_GroupTargetSendsChatAndMention(t *testing.T) {
	t.Parallel()

	userID := pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}
	store := &fakeNotificationStore{claimed: []OutboxNotification{
		{
			ID:                    pgtype.UUID{Bytes: [16]byte{0x13}, Valid: true},
			Provider:              "feishu",
			EventKind:             "mentioned",
			TargetUserID:          userID,
			TargetType:            "chat",
			TargetChatID:          "oc_group",
			MentionExternalUserID: "ou_1",
			Title:                 "A",
			Body:                  "body A",
			MaxAttempts:           3,
		},
	}}
	sender := &mockRetrySender{}
	worker := NewOutboxWorker(store, sender)

	worker.processBatch(context.Background())

	if len(sender.calls) != 1 {
		t.Fatalf("send calls = %d, want 1", len(sender.calls))
	}
	if sender.calls[0].Target.Type != "chat" || sender.calls[0].Target.ID != "oc_group" {
		t.Fatalf("target = %#v, want chat oc_group", sender.calls[0].Target)
	}
	if len(sender.calls[0].Payload.Mentions) != 1 || sender.calls[0].Payload.Mentions[0].ID != "ou_1" {
		t.Fatalf("mentions = %#v, want ou_1", sender.calls[0].Payload.Mentions)
	}
}

func TestOutboxWorker_RetryableFailureSchedulesRetry(t *testing.T) {
	t.Parallel()

	store := &fakeNotificationStore{claimed: []OutboxNotification{
		{ID: pgtype.UUID{Bytes: [16]byte{0x21}, Valid: true}, Provider: "feishu", EventKind: "issue_assigned", TargetUserID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}, TargetExternalUserID: "ou_1", Title: "A", MaxAttempts: 3},
	}}
	sender := &mockRetrySender{err: WrapRetryable(errors.New("temporary"))}
	worker := NewOutboxWorker(store, sender)

	worker.processBatch(context.Background())

	if len(store.retried) != 1 {
		t.Fatalf("retried ids = %d, want 1", len(store.retried))
	}
	if len(store.dead) != 0 {
		t.Fatalf("dead ids = %d, want 0", len(store.dead))
	}
}

func TestOutboxWorker_NonRetryableFailureMarksDead(t *testing.T) {
	t.Parallel()

	store := &fakeNotificationStore{claimed: []OutboxNotification{
		{ID: pgtype.UUID{Bytes: [16]byte{0x31}, Valid: true}, Provider: "feishu", EventKind: "issue_assigned", TargetUserID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}, TargetExternalUserID: "ou_1", Title: "A", MaxAttempts: 3},
	}}
	sender := &mockRetrySender{err: errors.New("bad request")}
	worker := NewOutboxWorker(store, sender)

	worker.processBatch(context.Background())

	if len(store.dead) != 1 {
		t.Fatalf("dead ids = %d, want 1", len(store.dead))
	}
	if len(store.retried) != 0 {
		t.Fatalf("retried ids = %d, want 0", len(store.retried))
	}
}

func TestOutboxWorker_ReclaimStaleProcessing(t *testing.T) {
	t.Parallel()

	userID := pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}
	reclaimed := []OutboxNotification{
		{ID: pgtype.UUID{Bytes: [16]byte{0x41}, Valid: true}, Provider: "feishu", EventKind: "issue_assigned", TargetUserID: userID, TargetExternalUserID: "ou_1", Title: "R", MaxAttempts: 3},
	}
	store := &fakeNotificationStore{reclaimed: reclaimed}
	sender := &mockRetrySender{}
	worker := NewOutboxWorker(store, sender)

	worker.processBatch(context.Background())

	if len(sender.calls) != 1 {
		t.Fatalf("send calls = %d, want 1", len(sender.calls))
	}
	if len(store.sent) != 1 {
		t.Fatalf("sent ids = %d, want 1", len(store.sent))
	}
}

func TestOutboxWorker_ProcessesReclaimedRowsEvenWhenClaimFails(t *testing.T) {
	t.Parallel()

	userID := pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}
	reclaimed := []OutboxNotification{
		{ID: pgtype.UUID{Bytes: [16]byte{0x61}, Valid: true}, Provider: "feishu", EventKind: "issue_assigned", TargetUserID: userID, TargetExternalUserID: "ou_1", Title: "R", MaxAttempts: 3},
	}
	store := &fakeNotificationStore{reclaimed: reclaimed, claimErr: errors.New("claim fails")}
	sender := &mockRetrySender{}
	worker := NewOutboxWorker(store, sender)

	worker.processBatch(context.Background())

	if len(sender.calls) != 1 {
		t.Fatalf("send calls = %d, want 1 (reclaimed rows should still be processed)", len(sender.calls))
	}
	if len(store.sent) != 1 {
		t.Fatalf("sent ids = %d, want 1", len(store.sent))
	}
}

func TestOutboxWorker_MixedAttemptsGroup_SplitsRetryAndDead(t *testing.T) {
	t.Parallel()

	userID := pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}
	store := &fakeNotificationStore{claimed: []OutboxNotification{
		{ID: pgtype.UUID{Bytes: [16]byte{0x51}, Valid: true}, Provider: "feishu", EventKind: "issue_assigned", TargetUserID: userID, TargetExternalUserID: "ou_1", Title: "Old", Attempts: 3, MaxAttempts: 3},
		{ID: pgtype.UUID{Bytes: [16]byte{0x52}, Valid: true}, Provider: "feishu", EventKind: "issue_assigned", TargetUserID: userID, TargetExternalUserID: "ou_1", Title: "New", Attempts: 0, MaxAttempts: 3},
	}}
	sender := &mockRetrySender{err: WrapRetryable(errors.New("temporary"))}
	worker := NewOutboxWorker(store, sender)

	worker.processBatch(context.Background())

	if len(store.dead) != 1 || store.dead[0].Bytes != [16]byte{0x51} {
		t.Fatalf("dead ids = %v, want [0x51]", store.dead)
	}
	if len(store.retried) != 1 || store.retried[0].Bytes != [16]byte{0x52} {
		t.Fatalf("retried ids = %v, want [0x52]", store.retried)
	}
}

func TestOutboxWorker_ReadyConnectionsFilterClaims(t *testing.T) {
	t.Parallel()

	store := &fakeNotificationStore{}
	worker := NewOutboxWorker(store, &mockRetrySender{})
	worker.SetReadyConnectionsFunc(func() []string { return []string{"conn-a", "conn-a", " "} })

	worker.processBatch(context.Background())

	if got, want := strings.Join(store.claimReadyIDs, ","), "conn-a"; got != want {
		t.Fatalf("claim ready ids = %q, want %q", got, want)
	}
	if got, want := strings.Join(store.reclaimReadyIDs, ","), "conn-a"; got != want {
		t.Fatalf("reclaim ready ids = %q, want %q", got, want)
	}
}

func TestOutboxWorker_NoReadyConnectionsDoesNotClaim(t *testing.T) {
	t.Parallel()

	store := &fakeNotificationStore{}
	worker := NewOutboxWorker(store, &mockRetrySender{})
	worker.SetReadyConnectionsFunc(func() []string { return nil })

	worker.processBatch(context.Background())

	if store.claimCalls != 0 || store.reclaimCalls != 0 {
		t.Fatalf("claims = %d/%d, want 0/0", store.claimCalls, store.reclaimCalls)
	}
}
