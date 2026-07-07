package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// uid builds a deterministic, valid pgtype.UUID from a single byte so tests can
// compare ids by equality.
func uid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Bytes[0] = b
	u.Valid = true
	return u
}

// fakeTx satisfies pgx.Tx by embedding the (nil) interface; the ChatSession
// service only calls Commit/Rollback, which we override as no-ops.
type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

type fakeTxStarter struct{}

func (fakeTxStarter) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

// fakeSessionQueries is an in-memory SessionQueries for unit tests.
type fakeSessionQueries struct {
	bindings        map[string]pgtype.UUID
	nextSession     byte
	createdSessions int
	messages        []string
	touched         int
	replyTargets    int
	lastConfig      []byte // config of the most recent CreateChannelChatSessionBinding

	prevMessage      *string // GetMostRecentUserChatMessage result; nil → ErrNoRows
	markRows         int64   // MarkChannelInboundDedupProcessed result
	createBindingErr error   // simulate a unique violation on create
	raceWinner       pgtype.UUID
}

func newFake() *fakeSessionQueries {
	return &fakeSessionQueries{bindings: map[string]pgtype.UUID{}, markRows: 1}
}

func bindKey(inst pgtype.UUID, chat string) string { return fmt.Sprintf("%x|%s", inst.Bytes, chat) }

func (f *fakeSessionQueries) WithTx(tx pgx.Tx) SessionQueries { return f }

func (f *fakeSessionQueries) GetChannelChatSessionBinding(_ context.Context, arg db.GetChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	if id, ok := f.bindings[bindKey(arg.InstallationID, arg.ChannelChatID)]; ok {
		return db.ChannelChatSessionBinding{ChatSessionID: id}, nil
	}
	return db.ChannelChatSessionBinding{}, pgx.ErrNoRows
}

func (f *fakeSessionQueries) CreateChatSession(_ context.Context, _ db.CreateChatSessionParams) (db.ChatSession, error) {
	f.nextSession++
	f.createdSessions++
	return db.ChatSession{ID: uid(f.nextSession)}, nil
}

func (f *fakeSessionQueries) CreateChannelChatSessionBinding(_ context.Context, arg db.CreateChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	f.lastConfig = arg.Config
	if f.createBindingErr != nil {
		// Simulate the race winner having committed its binding first.
		f.bindings[bindKey(arg.InstallationID, arg.ChannelChatID)] = f.raceWinner
		return db.ChannelChatSessionBinding{}, f.createBindingErr
	}
	f.bindings[bindKey(arg.InstallationID, arg.ChannelChatID)] = arg.ChatSessionID
	return db.ChannelChatSessionBinding{ChatSessionID: arg.ChatSessionID}, nil
}

func (f *fakeSessionQueries) CreateChatMessage(_ context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error) {
	f.messages = append(f.messages, arg.Content)
	return db.ChatMessage{}, nil
}

func (f *fakeSessionQueries) TouchChatSession(context.Context, pgtype.UUID) error {
	f.touched++
	return nil
}

func (f *fakeSessionQueries) GetMostRecentUserChatMessage(context.Context, pgtype.UUID) (db.ChatMessage, error) {
	if f.prevMessage != nil {
		return db.ChatMessage{Content: *f.prevMessage}, nil
	}
	return db.ChatMessage{}, pgx.ErrNoRows
}

func (f *fakeSessionQueries) UpdateChannelChatSessionBindingReplyTarget(context.Context, db.UpdateChannelChatSessionBindingReplyTargetParams) error {
	f.replyTargets++
	return nil
}

func (f *fakeSessionQueries) MarkChannelInboundDedupProcessed(context.Context, db.MarkChannelInboundDedupProcessedParams) (int64, error) {
	return f.markRows, nil
}

func newTestSession(f SessionQueries) *ChatSession {
	return newChatSessionWith(f, fakeTxStarter{}, channel.TypeFeishu, SessionTitles{Group: "G", Direct: "D", Fallback: "F"})
}

func TestEnsureSession_CreateThenReuse(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	in := EnsureSessionInput{InstallationID: uid(1), BindingKey: "chatA", ChatType: channel.ChatTypeP2P, Sender: uid(7)}

	id1, err := s.EnsureSession(context.Background(), in)
	if err != nil {
		t.Fatalf("first EnsureSession: %v", err)
	}
	if f.createdSessions != 1 {
		t.Fatalf("createdSessions = %d, want 1", f.createdSessions)
	}

	id2, err := s.EnsureSession(context.Background(), in)
	if err != nil {
		t.Fatalf("second EnsureSession: %v", err)
	}
	if f.createdSessions != 1 {
		t.Errorf("second call must reuse the binding, not create: createdSessions = %d", f.createdSessions)
	}
	if id1 != id2 {
		t.Errorf("ids differ: %v vs %v", id1, id2)
	}
}

func TestEnsureSession_RaceUniqueViolation(t *testing.T) {
	f := newFake()
	f.createBindingErr = &pgconn.PgError{Code: "23505"}
	f.raceWinner = uid(99)
	s := newTestSession(f)

	id, err := s.EnsureSession(context.Background(), EnsureSessionInput{InstallationID: uid(1), BindingKey: "chatA", ChatType: channel.ChatTypeGroup})
	if err != nil {
		t.Fatalf("EnsureSession on race: %v", err)
	}
	if id != uid(99) {
		t.Errorf("lost-race re-read should return the winner's session: %v", id)
	}
}

// TestEnsureSession_ThreadRootIsolation is the regression guard for Elon's
// must-fix: two @bot threads in the SAME Slack channel must NOT collapse into
// one chat_session. The Slack resolver composes BindingKey = channel + thread
// root, so distinct thread roots map to distinct sessions while a follow-up in
// the same thread reuses its session.
func TestEnsureSession_ThreadRootIsolation(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	mk := func(key string) pgtype.UUID {
		id, err := s.EnsureSession(context.Background(), EnsureSessionInput{
			InstallationID: uid(1), BindingKey: key, ChatType: channel.ChatTypeGroup,
		})
		if err != nil {
			t.Fatalf("EnsureSession(%q): %v", key, err)
		}
		return id
	}

	thread1 := mk("C123:1111.0001")
	thread2 := mk("C123:2222.0002") // same channel, different thread root
	if thread1 == thread2 {
		t.Fatal("distinct thread roots in one channel must get distinct sessions")
	}
	if f.createdSessions != 2 {
		t.Fatalf("createdSessions = %d, want 2", f.createdSessions)
	}

	again := mk("C123:1111.0001") // a follow-up in thread 1
	if again != thread1 {
		t.Error("same thread root must reuse its session")
	}
	if f.createdSessions != 2 {
		t.Errorf("a thread follow-up must not create a new session: createdSessions = %d", f.createdSessions)
	}
}

func TestEnsureSession_StoresBindingConfig(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	if _, err := s.EnsureSession(context.Background(), EnsureSessionInput{
		InstallationID: uid(1), BindingKey: "C123:1111.0001", ChatType: channel.ChatTypeGroup,
		BindingConfig: []byte(`{"channel_id":"C123"}`),
	}); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if string(f.lastConfig) != `{"channel_id":"C123"}` {
		t.Errorf("opaque outbound routing must be persisted on the binding: %q", f.lastConfig)
	}

	// Empty BindingConfig defaults to the "{}" object (the column is NOT NULL).
	f2 := newFake()
	if _, err := newTestSession(f2).EnsureSession(context.Background(), EnsureSessionInput{
		InstallationID: uid(1), BindingKey: "chatA", ChatType: channel.ChatTypeP2P,
	}); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if string(f2.lastConfig) != "{}" {
		t.Errorf("empty BindingConfig should default to {}: %q", f2.lastConfig)
	}
}

func TestAppendUserMessage_PlainText(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	res, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Sender: uid(7), Body: "hello there", MessageID: "m1",
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if res.IssueCommand != nil {
		t.Errorf("plain text should not parse as /issue: %+v", res.IssueCommand)
	}
	if len(f.messages) != 1 || f.messages[0] != "hello there" {
		t.Errorf("messages = %v", f.messages)
	}
	if f.touched != 1 || f.replyTargets != 1 {
		t.Errorf("touched=%d replyTargets=%d, want 1/1", f.touched, f.replyTargets)
	}
}

func TestAppendUserMessage_NoReplyTargetWithoutMessageID(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	if _, err := s.AppendUserMessage(context.Background(), AppendInput{SessionID: uid(1), Body: "hi"}); err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if f.replyTargets != 0 {
		t.Errorf("no MessageID → no reply-target update, got %d", f.replyTargets)
	}
}

func TestAppendUserMessage_IssueCommand(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	res, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Body: "/issue Fix bug\nsteps to repro", MessageID: "m1",
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if res.IssueCommand == nil || res.IssueCommand.Title != "Fix bug" || res.IssueCommand.Description != "steps to repro" {
		t.Errorf("IssueCommand = %+v", res.IssueCommand)
	}
}

func TestAppendUserMessage_CommandTextOverridesEnrichedBody(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	// Body is enriched (quoted context prepended) so /issue is NOT on the first
	// line; CommandText carries the user's own text and must win.
	res, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID:   uid(1),
		Body:        "> quoted context from another message\n/issue Real intent",
		CommandText: "/issue Real intent",
		MessageID:   "m1",
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if res.IssueCommand == nil || res.IssueCommand.Title != "Real intent" {
		t.Errorf("CommandText should drive /issue parsing: %+v", res.IssueCommand)
	}
	// The stored message is still the full (enriched) body.
	if f.messages[0] != "> quoted context from another message\n/issue Real intent" {
		t.Errorf("stored body should be the enriched Body: %q", f.messages[0])
	}
}

func TestAppendUserMessage_BareIssueUsesPreviousMessage(t *testing.T) {
	f := newFake()
	prev := "Make the export button work"
	f.prevMessage = &prev
	s := newTestSession(f)
	res, err := s.AppendUserMessage(context.Background(), AppendInput{SessionID: uid(1), Body: "/issue", MessageID: "m2"})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if res.IssueCommand == nil || res.IssueCommand.Title != "Make the export button work" {
		t.Errorf("bare /issue should fall back to previous message title: %+v", res.IssueCommand)
	}
}

func TestAppendUserMessage_DedupMark(t *testing.T) {
	f := newFake()
	f.markRows = 1
	s := newTestSession(f)
	res, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Body: "hi", MessageID: "m1", InstallationID: uid(1), ClaimToken: uid(5),
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if !res.DedupMarked {
		t.Error("a successful in-tx Mark should set DedupMarked")
	}
}

func TestAppendUserMessage_ClaimLost(t *testing.T) {
	f := newFake()
	f.markRows = 0 // a concurrent reclaim rotated the token
	s := newTestSession(f)
	_, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Body: "hi", MessageID: "m1", InstallationID: uid(1), ClaimToken: uid(5),
	})
	if err != ErrClaimLost {
		t.Errorf("zero Mark rows must return ErrClaimLost, got %v", err)
	}
}
