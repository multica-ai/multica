package wecom

// outbound_test.go — the EventChatDone reply path and the inbox:new delivery
// path, both driven through a fake outboundQueries (the interface Outbound
// depends on) and a recording wsConn, so no database is required. These are
// the paths that put an agent's words back in front of the WeCom user, and
// the "deliver via bot only when bound" contract the inbox notification rests
// on.
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
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

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

func newOutboundWithConn(t *testing.T, q outboundQueries) (*Outbound, pgtype.UUID, *recordingConn) {
	t.Helper()
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := &recordingConn{}
	reg.set(instID, newWSSender(conn, nil))
	return NewOutbound(q, reg, slog.Default()), instID, conn
}

func TestProcessEvent_DeliversChatReplyToBoundChat(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:   db.ChannelInstallation{Status: string(InstallationActive)},
	}
	o, instID, conn := newOutboundWithConn(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload:       protocol.ChatDonePayload{Content: "the agent reply"},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	body := conn.sendBody(t, 0)
	if body["chatid"] != "CHAT_1" {
		t.Errorf("reply chatid = %v, want CHAT_1", body["chatid"])
	}
	if body["chat_type"] != float64(chatTypeGroupInt) {
		t.Errorf("reply chat_type = %v, want group", body["chat_type"])
	}
	md, _ := body["markdown"].(map[string]any)
	if md == nil || md["content"] != "the agent reply" {
		t.Errorf("reply content = %v, want the agent reply", body["markdown"])
	}
}

func TestProcessEvent_NonWecomSessionIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{sessionErr: pgx.ErrNoRows}
	o, _, conn := newOutboundWithConn(t, q)
	if err := o.processEvent(context.Background(), events.Event{ChatSessionID: "22222222-2222-2222-2222-222222222222", Payload: protocol.ChatDonePayload{Content: "x"}}); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(conn.frames) != 0 {
		t.Errorf("expected no send for a non-wecom session, got %d frames", len(conn.frames))
	}
}

func TestProcessEvent_EmptyContentIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"}}
	o, instID, conn := newOutboundWithConn(t, q)
	q.sessionBinding.InstallationID = instID
	if err := o.processEvent(context.Background(), events.Event{ChatSessionID: "22222222-2222-2222-2222-222222222222", Payload: protocol.ChatDonePayload{Content: ""}}); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(conn.frames) != 0 {
		t.Errorf("empty completion should send nothing, got %d frames", len(conn.frames))
	}
}

func TestProcessEvent_RevokedInstallationIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"},
		installation:   db.ChannelInstallation{Status: "revoked"},
	}
	o, instID, conn := newOutboundWithConn(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	if err := o.processEvent(context.Background(), events.Event{ChatSessionID: "22222222-2222-2222-2222-222222222222", Payload: protocol.ChatDonePayload{Content: "hi"}}); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(conn.frames) != 0 {
		t.Errorf("revoked installation should send nothing, got %d frames", len(conn.frames))
	}
}

func TestTryDeliverInbox_PushesToBoundMemberPrivately(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		memberBinding: db.ChannelUserBinding{ChannelUserID: "T_USER_1"},
		workspace:     db.Workspace{Slug: "acme"},
	}
	o, instID, conn := newOutboundWithConn(t, q)
	q.memberBinding.InstallationID = instID

	item := map[string]any{
		"recipient_type": "member",
		"recipient_id":   "33333333-3333-3333-3333-333333333333",
		"workspace_id":   "44444444-4444-4444-4444-444444444444",
		"type":           "issue_assigned",
		"title":          "New issue",
	}
	if !o.tryDeliverInbox(context.Background(), item, "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444") {
		t.Fatal("tryDeliverInbox returned false; expected delivery to a bound member")
	}
	body := conn.sendBody(t, 0)
	if body["chatid"] != "T_USER_1" {
		t.Errorf("inbox push chatid = %v, want the member's bound userid", body["chatid"])
	}
	if body["chat_type"] != float64(chatTypeSingleInt) {
		t.Errorf("inbox push chat_type = %v, want single (1)", body["chat_type"])
	}
}

func TestTryDeliverInbox_NoBindingIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{memberErr: pgx.ErrNoRows}
	o, _, conn := newOutboundWithConn(t, q)
	if o.tryDeliverInbox(context.Background(), map[string]any{}, "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444") {
		t.Error("expected false when the member has no wecom binding")
	}
	if len(conn.frames) != 0 {
		t.Errorf("no binding should push nothing, got %d frames", len(conn.frames))
	}
}

func TestHandleInboxNew_IgnoresNonMemberRecipient(t *testing.T) {
	t.Parallel()
	// memberErr set so that if it somehow reached the query it would no-op;
	// the recipient_type guard should return before any query.
	q := &fakeOutboundQueries{memberErr: errors.New("must not be called")}
	o, _, conn := newOutboundWithConn(t, q)
	o.handleInboxNew(events.Event{Payload: map[string]any{
		"item": map[string]any{"recipient_type": "agent", "recipient_id": "x", "workspace_id": "y"},
	}})
	if len(conn.frames) != 0 {
		t.Errorf("agent recipient should not be pushed to, got %d frames", len(conn.frames))
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
