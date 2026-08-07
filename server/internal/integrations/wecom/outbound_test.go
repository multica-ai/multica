package wecom

// outbound_test.go — the EventChatDone reply path and the inbox:new delivery
// path, both driven through a fake outboundQueries (the interface Outbound
// depends on) and a recording enqueue store, so no database is required.
// These are the paths that put an agent's words back in front of the WeCom
// user, and the "deliver via bot only when bound" contract the inbox
// notification rests on.
//
// The observation point is the queue row, not a WebSocket frame: Outbound no
// longer writes to WeCom at all. It enqueues, and the replica holding the bot's
// socket delivers (see outbox_sender_test.go).
//
// Original inbox-delivery design and review: seacen (PR #5833).

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/outbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const testTaskID = "55555555-5555-5555-5555-555555555555"

// fakeOutboundQueries is an in-memory stand-in for the four queries Outbound
// uses. A nil error field returns the row; a non-nil one is returned as-is
// (use pgx.ErrNoRows to exercise the "not a wecom session" / "no binding"
// branches).
type fakeOutboundQueries struct {
	sessionBinding db.ChannelChatSessionBinding
	sessionErr     error
	installation   db.ChannelInstallation
	installErr     error
	memberBinding  db.ChannelUserBinding
	memberErr      error
	workspace      db.Workspace
	workspaceErr   error
}

func (f *fakeOutboundQueries) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	return f.sessionBinding, f.sessionErr
}
func (f *fakeOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.installation, f.installErr
}
func (f *fakeOutboundQueries) FindChannelBindingForMember(context.Context, db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error) {
	return f.memberBinding, f.memberErr
}
func (f *fakeOutboundQueries) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return f.workspace, f.workspaceErr
}

// recordingEnqueueStore captures every enqueue so a test can assert on the row
// that would have been written.
type recordingEnqueueStore struct {
	rows []db.EnqueueChannelOutboundParams
	err  error
}

func (s *recordingEnqueueStore) EnqueueChannelOutbound(_ context.Context, arg db.EnqueueChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	if s.err != nil {
		return db.ChannelOutboundQueue{}, s.err
	}
	s.rows = append(s.rows, arg)
	return db.ChannelOutboundQueue{
		InstallationID: arg.InstallationID,
		WorkspaceID:    arg.WorkspaceID,
		ChannelType:    arg.ChannelType,
		SourceKind:     arg.SourceKind,
		SourceID:       arg.SourceID,
	}, nil
}

// payload decodes the queue payload of row i.
func (s *recordingEnqueueStore) payload(t *testing.T, i int) outboundPayload {
	t.Helper()
	if i >= len(s.rows) {
		t.Fatalf("no enqueued row at index %d (have %d)", i, len(s.rows))
	}
	var p outboundPayload
	if err := json.Unmarshal(s.rows[i].Payload, &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return p
}

func newOutboundWithQueue(t *testing.T, q outboundQueries) (*Outbound, pgtype.UUID, *recordingEnqueueStore) {
	t.Helper()
	store := &recordingEnqueueStore{}
	producer, err := outbox.NewProducer(channelTypeWecom, store, nil, nil)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	return NewOutbound(q, producer, slog.Default()), mustTestUUID(t), store
}

func chatDoneEvent(content string) events.Event {
	return events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload: protocol.ChatDonePayload{
			TaskID:  testTaskID,
			Content: content,
		},
	}
}

func TestProcessEvent_EnqueuesChatReplyForBoundChat(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:   db.ChannelInstallation{Status: string(InstallationActive)},
	}
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)

	if err := o.processEvent(context.Background(), chatDoneEvent("the agent reply")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("enqueued %d rows, want 1", len(store.rows))
	}
	row := store.rows[0]
	if row.TargetChatID != "CHAT_1" {
		t.Errorf("target_chat_id = %q, want CHAT_1", row.TargetChatID)
	}
	if row.TargetChatType != int16(chatTypeGroupInt) {
		t.Errorf("target_chat_type = %d, want group (%d)", row.TargetChatType, chatTypeGroupInt)
	}
	if row.ChannelType != channelTypeWecom {
		t.Errorf("channel_type = %q, want %q", row.ChannelType, channelTypeWecom)
	}
	if row.SourceKind != sourceKindChatDone {
		t.Errorf("source_kind = %q, want %q", row.SourceKind, sourceKindChatDone)
	}
	// The business key must be the task, so a redelivered event dedupes
	// against the row the first delivery wrote.
	if row.SourceID != testTaskID {
		t.Errorf("source_id = %q, want the task id %q", row.SourceID, testTaskID)
	}
	if got := store.payload(t, 0).Content; got != "the agent reply" {
		t.Errorf("payload content = %q, want the agent reply", got)
	}
}

// The task id lives in ChatDonePayload, not on the event envelope
// (service.broadcastChatDone leaves Event.TaskID empty). Reading only the
// envelope would skip every realtime enqueue and silently demote delivery to
// the deliberately-lagged reconciler.
func TestProcessEvent_TakesTaskIDFromPayloadNotEnvelope(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"},
		installation:   db.ChannelInstallation{Status: string(InstallationActive)},
	}
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)

	event := chatDoneEvent("hi")
	if event.TaskID != "" {
		t.Fatal("fixture must mirror production: envelope TaskID is empty")
	}
	if err := o.processEvent(context.Background(), event); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("enqueued %d rows, want 1 — the payload task id must be used", len(store.rows))
	}
}

func TestProcessEvent_MapPayloadTaskIDIsUsed(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"},
		installation:   db.ChannelInstallation{Status: string(InstallationActive)},
	}
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)

	// Post-serialization shape, as a Redis-relayed event arrives.
	err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload:       map[string]any{"task_id": testTaskID, "content": "mapped reply"},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("enqueued %d rows, want 1", len(store.rows))
	}
	if store.rows[0].SourceID != testTaskID {
		t.Errorf("source_id = %q, want %q", store.rows[0].SourceID, testTaskID)
	}
}

func TestProcessEvent_MissingTaskIDIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"},
		installation:   db.ChannelInstallation{Status: string(InstallationActive)},
	}
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)

	err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload:       protocol.ChatDonePayload{Content: "no task id"},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 0 {
		t.Errorf("expected no enqueue without a business key, got %d rows", len(store.rows))
	}
}

func TestProcessEvent_NonWecomSessionIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{sessionErr: pgx.ErrNoRows}
	o, _, store := newOutboundWithQueue(t, q)
	if err := o.processEvent(context.Background(), chatDoneEvent("x")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 0 {
		t.Errorf("expected no enqueue for a non-wecom session, got %d rows", len(store.rows))
	}
}

func TestProcessEvent_EmptyContentIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"}}
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	if err := o.processEvent(context.Background(), chatDoneEvent("")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 0 {
		t.Errorf("empty completion should enqueue nothing, got %d rows", len(store.rows))
	}
}

func TestProcessEvent_RevokedInstallationIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"},
		installation:   db.ChannelInstallation{Status: "revoked"},
	}
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)
	if err := o.processEvent(context.Background(), chatDoneEvent("hi")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 0 {
		t.Errorf("revoked installation should enqueue nothing, got %d rows", len(store.rows))
	}
}

func TestTryEnqueueInbox_TargetsBoundMemberPrivately(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		memberBinding: db.ChannelUserBinding{ChannelUserID: "T_USER_1"},
		workspace:     db.Workspace{Slug: "acme"},
	}
	o, instID, store := newOutboundWithQueue(t, q)
	q.memberBinding.InstallationID = instID

	const itemID = "66666666-6666-6666-6666-666666666666"
	item := map[string]any{
		"id":             itemID,
		"recipient_type": "member",
		"recipient_id":   "33333333-3333-3333-3333-333333333333",
		"workspace_id":   "44444444-4444-4444-4444-444444444444",
		"type":           "issue_assigned",
		"title":          "New issue",
	}
	if !o.tryEnqueueInbox(context.Background(), item, itemID, "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444") {
		t.Fatal("tryEnqueueInbox returned false; expected an enqueue for a bound member")
	}
	if len(store.rows) != 1 {
		t.Fatalf("enqueued %d rows, want 1", len(store.rows))
	}
	row := store.rows[0]
	if row.TargetChatID != "T_USER_1" {
		t.Errorf("target_chat_id = %q, want the member's bound userid", row.TargetChatID)
	}
	if row.TargetChatType != int16(chatTypeSingleInt) {
		t.Errorf("target_chat_type = %d, want single (%d)", row.TargetChatType, chatTypeSingleInt)
	}
	// Keyed on the inbox item so a redelivered inbox:new cannot notify twice.
	if row.SourceKind != sourceKindInboxNotify || row.SourceID != itemID {
		t.Errorf("business key = (%q, %q), want (%q, %q)", row.SourceKind, row.SourceID, sourceKindInboxNotify, itemID)
	}
	// The row must carry no chat_session_id: an inbox push is addressed to a
	// user, not a conversation, so session fencing must not apply to it.
	if row.ChatSessionID.Valid {
		t.Error("inbox push must not be fenced on a chat session")
	}
}

func TestTryEnqueueInbox_NoBindingIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{memberErr: pgx.ErrNoRows}
	o, _, store := newOutboundWithQueue(t, q)
	if o.tryEnqueueInbox(context.Background(), map[string]any{}, "66666666-6666-6666-6666-666666666666", "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444") {
		t.Error("expected false when the member has no wecom binding")
	}
	if len(store.rows) != 0 {
		t.Errorf("no binding should enqueue nothing, got %d rows", len(store.rows))
	}
}

func TestHandleInboxNew_IgnoresNonMemberRecipient(t *testing.T) {
	t.Parallel()
	// memberErr set so that if it somehow reached the query it would no-op;
	// the recipient_type guard should return before any query.
	q := &fakeOutboundQueries{memberErr: errors.New("must not be called")}
	o, _, store := newOutboundWithQueue(t, q)
	o.handleInboxNew(events.Event{Payload: map[string]any{
		"item": map[string]any{"id": "x", "recipient_type": "agent", "recipient_id": "x", "workspace_id": "y"},
	}})
	if len(store.rows) != 0 {
		t.Errorf("agent recipient should not be enqueued for, got %d rows", len(store.rows))
	}
}

func TestChatDoneContent(t *testing.T) {
	t.Parallel()
	if got := chatDoneContent(protocol.ChatDonePayload{Content: "typed"}); got != "typed" {
		t.Errorf("typed payload = %q, want typed", got)
	}
	roundTripped := map[string]any{"content": "mapped"}
	if got := chatDoneContent(roundTripped); got != "mapped" {
		t.Errorf("map payload = %q, want mapped", got)
	}
	if got := chatDoneContent(map[string]any{"other": 1}); got != "" {
		t.Errorf("payload without content = %q, want empty", got)
	}
	if got := chatDoneContent(json.RawMessage(`{}`)); got != "" {
		t.Errorf("unknown payload type = %q, want empty", got)
	}
}

func TestChatDoneTaskID(t *testing.T) {
	t.Parallel()
	if got := chatDoneTaskID(events.Event{Payload: protocol.ChatDonePayload{TaskID: "typed"}}); got != "typed" {
		t.Errorf("typed payload = %q, want typed", got)
	}
	if got := chatDoneTaskID(events.Event{Payload: map[string]any{"task_id": "mapped"}}); got != "mapped" {
		t.Errorf("map payload = %q, want mapped", got)
	}
	// Envelope fallback for a publisher that sets it there instead.
	if got := chatDoneTaskID(events.Event{TaskID: "envelope", Payload: map[string]any{}}); got != "envelope" {
		t.Errorf("envelope fallback = %q, want envelope", got)
	}
	if got := chatDoneTaskID(events.Event{Payload: json.RawMessage(`{}`)}); got != "" {
		t.Errorf("unknown payload type = %q, want empty", got)
	}
}
