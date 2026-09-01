package wecom

// resolvers_test.go — the platform-specific resolver mappings and outbound
// wiring that need no database: the session-binder param mapping (driven with
// a fake engineSessionBinder), BindMedia's no-op, NewResolverSet's wiring,
// wecomMsgFromRaw / textOrNull, the Reply outcome branches that post to a
// live sender, and Outbound.Register / handleEvent over a real bus.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeSessionBinder records the engine.*Input it was handed so the wecom
// param mapping can be asserted without a database.
type fakeSessionBinder struct {
	ensureIn engine.EnsureSessionInput
	startIn  engine.StartSessionInput
	appendIn engine.AppendInput
	bindIn   engine.BindMediaInput
	binds    int
	sessID   pgtype.UUID
}

func (f *fakeSessionBinder) BindMediaRefs(_ context.Context, in engine.BindMediaInput) error {
	f.bindIn = in
	f.binds++
	return nil
}

func (f *fakeSessionBinder) EnsureSession(_ context.Context, in engine.EnsureSessionInput) (pgtype.UUID, error) {
	f.ensureIn = in
	return f.sessID, nil
}
func (f *fakeSessionBinder) StartSession(_ context.Context, in engine.StartSessionInput) (engine.StartSessionResult, error) {
	f.startIn = in
	return engine.StartSessionResult{SessionID: f.sessID}, nil
}
func (f *fakeSessionBinder) MarkPendingFresh(context.Context, pgtype.UUID, string) error { return nil }
func (f *fakeSessionBinder) AppendUserMessage(_ context.Context, in engine.AppendInput) (engine.AppendResult, error) {
	f.appendIn = in
	return engine.AppendResult{}, nil
}

func TestSessionBinder_EnsureSessionMapsGroupKey(t *testing.T) {
	t.Parallel()
	fb := &fakeSessionBinder{sessID: mustTestUUID(t)}
	b := &sessionBinder{session: fb}
	inst := engine.ResolvedInstallation{ID: mustTestUUID(t), WorkspaceID: mustTestUUID(t), AgentID: mustTestUUID(t)}
	_, err := b.EnsureSession(context.Background(), engine.EnsureSessionParams{
		Installation: inst,
		Sender:       mustTestUUID(t),
		Message:      channel.InboundMessage{Source: channel.Source{ChatID: "GROUP_1", ChatType: channel.ChatTypeGroup}},
	})
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	// wecom keys the session on the chat id (no thread concept).
	if fb.ensureIn.BindingKey != "GROUP_1" {
		t.Errorf("BindingKey = %q, want the group chat id GROUP_1", fb.ensureIn.BindingKey)
	}
	if fb.ensureIn.ChatType != channel.ChatTypeGroup {
		t.Errorf("ChatType = %v, want group", fb.ensureIn.ChatType)
	}
}

func TestSessionBinder_StartSessionMapsWeComRouteAndFirstTurn(t *testing.T) {
	t.Parallel()
	fb := &fakeSessionBinder{sessID: mustTestUUID(t)}
	b := &sessionBinder{session: fb}
	inst := engine.ResolvedInstallation{ID: mustTestUUID(t), WorkspaceID: mustTestUUID(t), AgentID: mustTestUUID(t)}
	sender := mustTestUUID(t)
	creator := mustTestUUID(t)
	claim := mustTestUUID(t)
	result, err := b.StartSession(context.Background(), engine.StartSessionParams{
		Installation: inst,
		Creator:      creator,
		Sender:       sender,
		ClaimToken:   claim,
		Message: channel.InboundMessage{
			MessageID: "m1", Text: "first turn",
			Source: channel.Source{ChatID: "GROUP_1", ChatType: channel.ChatTypeGroup},
		},
		MediaPendingSeconds: 45,
		PersistMessage:      true,
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if result.SessionID != fb.sessID {
		t.Fatal("StartSession lost shared-session result")
	}
	got := fb.startIn
	if got.BindingKey != "GROUP_1" || got.Body != "first turn" || got.MessageID != "m1" || got.ClaimToken != claim || got.MediaPendingSeconds != 45 || !got.PersistMessage {
		t.Fatalf("start mapping wrong: %+v", got)
	}
	if got.Sender != creator || got.Initiator != sender {
		t.Fatalf("creator/initiator mapping wrong: %+v", got)
	}
}

func TestSessionBinder_AppendUsesTextAsCommand(t *testing.T) {
	t.Parallel()
	fb := &fakeSessionBinder{}
	b := &sessionBinder{session: fb}
	_, err := b.AppendMessage(context.Background(), engine.AppendParams{
		SessionID: mustTestUUID(t),
		Message:   channel.InboundMessage{Text: "/issue do a thing", MessageID: "m9", ForceFresh: true},
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if fb.appendIn.Body != "/issue do a thing" || fb.appendIn.CommandText != "/issue do a thing" || !fb.appendIn.ForceFresh {
		t.Errorf("append body/command/forceFresh = %q/%q/%t, want text fallback preserved with fresh intent", fb.appendIn.Body, fb.appendIn.CommandText, fb.appendIn.ForceFresh)
	}
}

// The resolver downloads, decrypts and stores an attachment; this is the step
// that attaches it to the message. Returning nil without binding reads to the
// Router as success, so every attachment would be fetched and then silently
// discarded — the agent sees only "[Image]" and nothing is logged.
func TestSessionBinder_BindMediaReachesTheSessionStore(t *testing.T) {
	t.Parallel()
	fb := &fakeSessionBinder{}
	b := &sessionBinder{session: fb}
	refs := []channel.MediaRef{{StorageKey: "k", Filename: "photo.jpg"}}
	issue := pgtype.UUID{Bytes: [16]byte{7}, Valid: true}

	if _, err := b.BindMedia(context.Background(), engine.BindMediaParams{
		MediaRefs: refs,
		IssueID:   issue,
	}); err != nil {
		t.Fatalf("BindMedia: %v", err)
	}
	if fb.binds != 1 {
		t.Fatalf("BindMediaRefs called %d times, want 1 — the refs never reached the session store", fb.binds)
	}
	if len(fb.bindIn.MediaRefs) != 1 {
		t.Errorf("MediaRefs = %d, want 1", len(fb.bindIn.MediaRefs))
	}
	// IssueID is what makes an /issue turn's attachment belong to the issue
	// rather than to a chat message nobody opens again.
	if fb.bindIn.IssueID != issue {
		t.Error("IssueID was dropped; an /issue attachment would land on the chat message instead")
	}
}

// The media budget is how long the chat task waits before running. Dropped, the
// run fires at once and the agent is handed the placeholder mid-download.
func TestSessionBinder_AppendCarriesTheMediaBudget(t *testing.T) {
	t.Parallel()
	fb := &fakeSessionBinder{}
	b := &sessionBinder{session: fb}
	if _, err := b.AppendMessage(context.Background(), engine.AppendParams{
		MediaPendingSeconds: 45,
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if fb.appendIn.MediaPendingSeconds != 45 {
		t.Errorf("MediaPendingSeconds = %v, want 45 — the media budget never reached the append", fb.appendIn.MediaPendingSeconds)
	}
}

func TestNewResolverSet_WiresAllResolvers(t *testing.T) {
	t.Parallel()
	set := NewResolverSet(&Store{}, &fakeSessionBinder{}, nil, nil)
	if set.Installation == nil || set.Identity == nil || set.Dedup == nil || set.Session == nil || set.Audit == nil {
		t.Error("NewResolverSet left a required resolver nil")
	}
	if set.OriginType != originWecomChat {
		t.Errorf("OriginType = %q, want %q", set.OriginType, originWecomChat)
	}
	// A deployment with no object store passes nil and must degrade to
	// placeholder text, which the Router only does when Media is nil.
	if set.Media != nil {
		t.Error("nil media argument produced a non-nil Media resolver")
	}
	// And a configured one must actually reach the Router — this is the
	// whole wiring, and a resolver built at boot and dropped here would look
	// exactly like media ingestion never having been written.
	media := NewMediaResolver(&fakeMediaStorage{}, newFakeMediaLedger(nil), nil, testLogger())
	withMedia := NewResolverSet(&Store{}, &fakeSessionBinder{}, nil, media)
	if withMedia.Media == nil {
		t.Fatal("a media resolver was passed and dropped")
	}
	if !withMedia.Media.HasMedia(mediaMessage(t, "image", map[string]any{
		"image": map[string]any{"url": "https://cos.invalid/a", "aeskey": testAESKey},
	})) {
		t.Error("the wired resolver does not recognize a media message")
	}
}

func TestWecomMsgFromRaw(t *testing.T) {
	t.Parallel()
	raw, _ := json.Marshal(InboundMessage{BotID: "bot-1", MsgType: "text", ChatID: "c1"})
	wm, err := wecomMsgFromRaw(channel.InboundMessage{Raw: raw})
	if err != nil {
		t.Fatalf("wecomMsgFromRaw: %v", err)
	}
	if wm.BotID != "bot-1" || wm.MsgType != "text" {
		t.Errorf("decoded = %+v, want bot-1/text", wm)
	}
	if _, err := wecomMsgFromRaw(channel.InboundMessage{}); err == nil {
		t.Error("empty Raw should error")
	}
	if _, err := wecomMsgFromRaw(channel.InboundMessage{Raw: []byte("{not json")}); err == nil {
		t.Error("bad JSON should error")
	}
}

func TestTextOrNull(t *testing.T) {
	t.Parallel()
	if got := textOrNull(""); got.Valid {
		t.Error(`textOrNull("") should be NULL (Valid=false)`)
	}
	got := textOrNull("x")
	if !got.Valid || got.String != "x" {
		t.Errorf("textOrNull(x) = %+v, want valid x", got)
	}
}

func TestReply_PostsIssueCreatedConfirmation(t *testing.T) {
	t.Parallel()
	r, inst, conn := newReplierWithConn(t)
	r.Reply(context.Background(), inst, channel.InboundMessage{Source: channel.Source{ChatID: "C1", ChatType: channel.ChatTypeP2P}}, engine.Result{
		Outcome:         engine.OutcomeIngested,
		IssueID:         mustTestUUID(t),
		IssueIdentifier: "MUL-9",
		IssueTitle:      "Ship it",
	})
	if len(conn.frames) != 1 {
		t.Fatalf("expected one confirmation frame, got %d", len(conn.frames))
	}
	body := conn.sendBody(t, 0)
	md, _ := body["markdown"].(map[string]any)
	if md == nil || md["content"] == nil {
		t.Fatalf("no markdown content in confirmation: %v", body)
	}
	if content, _ := md["content"].(string); content == "" || !strings.Contains(content, "MUL-9") {
		t.Errorf("confirmation = %q, want it to name MUL-9", md["content"])
	}
}

func TestReply_PlainChatStaysSilent(t *testing.T) {
	t.Parallel()
	r, inst, conn := newReplierWithConn(t)
	// Ingested but NOT an issue-created (no IssueID) → the agent's own reply
	// arrives via EventChatDone, so Reply must not post anything here.
	r.Reply(context.Background(), inst, channel.InboundMessage{Source: channel.Source{ChatID: "C1"}}, engine.Result{Outcome: engine.OutcomeIngested})
	if len(conn.frames) != 0 {
		t.Errorf("a plain chat message should not post a confirmation, got %d frames", len(conn.frames))
	}
}

func TestReply_OfflineAndArchivedPostNotices(t *testing.T) {
	t.Parallel()
	for _, oc := range []engine.Outcome{engine.OutcomeAgentOffline, engine.OutcomeAgentArchived} {
		r, inst, conn := newReplierWithConn(t)
		r.Reply(context.Background(), inst, channel.InboundMessage{Source: channel.Source{ChatID: "C1", ChatType: channel.ChatTypeP2P}}, engine.Result{Outcome: oc})
		if len(conn.frames) != 1 {
			t.Errorf("outcome %s: expected one notice frame, got %d", oc, len(conn.frames))
		}
	}
}

func TestOutbound_RegisterAndHandleEventNoopOnNonWecom(t *testing.T) {
	t.Parallel()
	// Register must subscribe without panicking, and a chat-done for a session
	// that isn't a wecom binding must be a silent no-op (handleEvent swallows
	// the processEvent result). Uses pgx.ErrNoRows via the fake.
	q := &fakeOutboundQueries{sessionErr: pgx.ErrNoRows}
	o := NewOutbound(q, newSendersRegistry(), slog.Default())
	bus := events.New()
	o.Register(bus)
	// Publishing must not panic; the handler runs synchronously on the bus.
	bus.Publish(events.Event{Type: protocol.EventChatDone, ChatSessionID: "55555555-5555-5555-5555-555555555555", Payload: protocol.ChatDonePayload{Content: "x"}})
}
