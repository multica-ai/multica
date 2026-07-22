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
	lockedWorkspace int                  // count of LockWorkspaceForChatSessionCreate calls
	supersedes      int
	sessionRows     map[pgtype.UUID]bool // every session that has a binding row; supersede must NOT remove it
	lastTitle       string               // title of the most recent CreateChatSession
	lastConfig      []byte               // config of the most recent CreateChannelChatSessionBinding

	prevMessage      *string // GetMostRecentUserChatMessage result; nil → ErrNoRows
	prevLookups      int     // GetMostRecentUserChatMessage call count
	markRows         int64   // MarkChannelInboundDedupProcessed result
	marks            int     // MarkChannelInboundDedupProcessed call count
	createBindingErr error   // simulate a unique violation on create
	raceWinner       pgtype.UUID

	attachments   []db.CreateAttachmentParams // recorded CreateAttachment calls
	linkCalls     []db.LinkAttachmentsToChatMessageParams
	attachmentErr error // CreateAttachment failure injection
}

func newFake() *fakeSessionQueries {
	return &fakeSessionQueries{bindings: map[string]pgtype.UUID{}, sessionRows: map[pgtype.UUID]bool{}, markRows: 1}
}

func bindKey(inst pgtype.UUID, chat string) string { return fmt.Sprintf("%x|%s", inst.Bytes, chat) }

func (f *fakeSessionQueries) WithTx(tx pgx.Tx) SessionQueries { return f }

func (f *fakeSessionQueries) GetChannelChatSessionBinding(_ context.Context, arg db.GetChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	if id, ok := f.bindings[bindKey(arg.InstallationID, arg.ChannelChatID)]; ok {
		return db.ChannelChatSessionBinding{ChatSessionID: id}, nil
	}
	return db.ChannelChatSessionBinding{}, pgx.ErrNoRows
}

func (f *fakeSessionQueries) LockWorkspaceForChatSessionCreate(_ context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	f.lockedWorkspace++
	return id, nil
}

func (f *fakeSessionQueries) CreateChatSession(_ context.Context, arg db.CreateChatSessionParams) (db.ChatSession, error) {
	f.nextSession++
	f.createdSessions++
	f.lastTitle = arg.Title
	return db.ChatSession{ID: uid(f.nextSession)}, nil
}

// SupersedeChannelChatSessionBinding marks the chat's ACTIVE binding inactive:
// it drops the "current session for this chat" mapping so a fresh insert can
// take its place, but must NOT forget the superseded session's row (that
// retention is the whole point of the /new fix — its in-flight reply stays
// reverse-resolvable).
func (f *fakeSessionQueries) SupersedeChannelChatSessionBinding(_ context.Context, arg db.SupersedeChannelChatSessionBindingParams) error {
	f.supersedes++
	delete(f.bindings, bindKey(arg.InstallationID, arg.ChannelChatID))
	return nil
}

func (f *fakeSessionQueries) CreateChannelChatSessionBinding(_ context.Context, arg db.CreateChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	f.lastConfig = arg.Config
	if f.createBindingErr != nil {
		// Simulate the race winner having committed its binding first.
		f.bindings[bindKey(arg.InstallationID, arg.ChannelChatID)] = f.raceWinner
		f.sessionRows[f.raceWinner] = true
		return db.ChannelChatSessionBinding{}, f.createBindingErr
	}
	f.bindings[bindKey(arg.InstallationID, arg.ChannelChatID)] = arg.ChatSessionID
	f.sessionRows[arg.ChatSessionID] = true
	return db.ChannelChatSessionBinding{ChatSessionID: arg.ChatSessionID}, nil
}

func (f *fakeSessionQueries) CreateChatMessage(_ context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error) {
	f.messages = append(f.messages, arg.Content)
	return db.ChatMessage{ID: uid(0x77)}, nil
}

func (f *fakeSessionQueries) CreateAttachment(_ context.Context, arg db.CreateAttachmentParams) (db.Attachment, error) {
	if f.attachmentErr != nil {
		return db.Attachment{}, f.attachmentErr
	}
	f.attachments = append(f.attachments, arg)
	return db.Attachment{ID: arg.ID}, nil
}

func (f *fakeSessionQueries) LinkAttachmentsToChatMessage(_ context.Context, arg db.LinkAttachmentsToChatMessageParams) ([]pgtype.UUID, error) {
	f.linkCalls = append(f.linkCalls, arg)
	return arg.AttachmentIds, nil
}

func (f *fakeSessionQueries) TouchChatSession(context.Context, pgtype.UUID) error {
	f.touched++
	return nil
}

func (f *fakeSessionQueries) GetMostRecentUserChatMessage(context.Context, pgtype.UUID) (db.ChatMessage, error) {
	f.prevLookups++
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
	f.marks++
	return f.markRows, nil
}

func newTestSession(f SessionQueries) *ChatSession {
	return newChatSessionWith(f, fakeTxStarter{}, channel.TypeFeishu, SessionTitles{Group: "G", Direct: "D", Fallback: "F"})
}

func TestEnsureSession_FreshRotatesToNewSession(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	base := EnsureSessionInput{InstallationID: uid(1), BindingKey: "chatA", ChatType: channel.ChatTypeP2P, Sender: uid(7)}

	id1, err := s.EnsureSession(context.Background(), base)
	if err != nil {
		t.Fatalf("first contact: %v", err)
	}
	// A Fresh call on the SAME binding must mint a new session and repoint the
	// binding to it, not reuse the old one.
	fresh := base
	fresh.Fresh = true
	id2, err := s.EnsureSession(context.Background(), fresh)
	if err != nil {
		t.Fatalf("fresh: %v", err)
	}
	if id1 == id2 {
		t.Fatal("/new must rotate to a brand-new chat_session")
	}
	if f.supersedes != 1 {
		t.Fatalf("expected exactly one supersede, got %d", f.supersedes)
	}
	// The fix: /new supersedes (does NOT move or delete) the old binding, so the
	// old session's row survives and its still-in-flight reply stays
	// reverse-resolvable to this chat.
	if !f.sessionRows[id1] {
		t.Fatal("/new must retain the superseded session's binding row, not orphan it")
	}
	if !f.sessionRows[id2] {
		t.Fatal("the fresh session must have its own binding row")
	}
	// A subsequent normal message resumes the rotated-in session, not the old one.
	id3, err := s.EnsureSession(context.Background(), base)
	if err != nil {
		t.Fatalf("reuse after fresh: %v", err)
	}
	if id3 != id2 {
		t.Fatalf("normal message after /new must reuse the fresh session, got %v want %v", id3, id2)
	}
}

func TestEnsureSession_TitleSeedAndFallback(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	in := EnsureSessionInput{InstallationID: uid(1), BindingKey: "chatA", ChatType: channel.ChatTypeP2P, Sender: uid(7), Title: "let's talk weather"}
	if _, err := s.EnsureSession(context.Background(), in); err != nil {
		t.Fatalf("seeded title: %v", err)
	}
	if f.lastTitle != "let's talk weather" {
		t.Fatalf("session title should use the seed, got %q", f.lastTitle)
	}

	f2 := newFake()
	s2 := newTestSession(f2)
	blank := EnsureSessionInput{InstallationID: uid(2), BindingKey: "chatB", ChatType: channel.ChatTypeP2P, Sender: uid(7)}
	if _, err := s2.EnsureSession(context.Background(), blank); err != nil {
		t.Fatalf("blank title: %v", err)
	}
	if f2.lastTitle != "D" { // SessionTitles.Direct fallback
		t.Fatalf("empty seed should fall back to platform default, got %q", f2.lastTitle)
	}
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

// Direct-create channels (Feishu/Slack) keep the bare-/issue fallback: a lone
// "/issue" adopts the previous user message's first line as its title, which
// createIssue needs.
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
	if f.prevLookups != 1 {
		t.Errorf("the fallback must query the previous message once, got %d", f.prevLookups)
	}
}

// Quick-create channels (DingTalk) opt out via SkipPreviousFallback: a bare
// "/issue" keeps an empty title AND never queries the previous message, so the
// caller can ask the user what to file instead of silently adopting the prior
// turn (which could be an unrelated image). Regression guard for the reported
// bug where a bare "/issue" after an image message filed that image as an issue.
func TestAppendUserMessage_SkipPreviousFallback(t *testing.T) {
	f := newFake()
	prev := "Make the export button work"
	f.prevMessage = &prev
	s := newTestSession(f)
	res, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Body: "/issue", MessageID: "m2", SkipPreviousFallback: true,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if res.IssueCommand == nil || res.IssueCommand.Title != "" {
		t.Errorf("SkipPreviousFallback must leave the title empty: %+v", res.IssueCommand)
	}
	if f.prevLookups != 0 {
		t.Errorf("SkipPreviousFallback must not query the previous message, got %d lookups", f.prevLookups)
	}
}

func stagedFixture() []StagedMedia {
	return []StagedMedia{
		{ID: uid(0xA1), StorageKey: "workspaces/w/x.png", URL: "https://cdn.example/x.png", Filename: "image-1.png", ContentType: "image/png", SizeBytes: 10},
		{ID: uid(0xA2), StorageKey: "workspaces/w/y.jpg", URL: "https://cdn.example/y.jpg", Filename: "image-2.jpg", ContentType: "image/jpeg", SizeBytes: 20},
	}
}

func TestAppendUserMessage_StagedAttachments_ChatBound(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	res, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Sender: uid(7), WorkspaceID: uid(9),
		Body: "look [image: image-1.png]", MessageID: "m1",
		Staged: stagedFixture(), MediaChatBind: true,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if len(f.messages) != 1 || f.messages[0] != "look [image: image-1.png]" {
		t.Errorf("messages = %v", f.messages)
	}
	if len(f.attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(f.attachments))
	}
	first := f.attachments[0]
	if first.ID != uid(0xA1) || first.WorkspaceID != uid(9) || first.UploaderType != "member" ||
		first.UploaderID != uid(7) || first.Filename != "image-1.png" ||
		first.Url != "https://cdn.example/x.png" || first.ContentType != "image/png" || first.SizeBytes != 10 {
		t.Errorf("attachment[0] = %+v", first)
	}
	if first.ChatSessionID != uid(1) || f.attachments[1].ChatSessionID != uid(1) {
		t.Error("chat-bound attachments must carry the chat_session_id")
	}
	if len(f.linkCalls) != 1 {
		t.Fatalf("linkCalls = %d, want 1", len(f.linkCalls))
	}
	link := f.linkCalls[0]
	if link.ChatMessageID != uid(0x77) || link.ChatSessionID != uid(1) || link.WorkspaceID != uid(9) ||
		link.UploaderType != "member" || link.UploaderID != uid(7) || len(link.AttachmentIds) != 2 {
		t.Errorf("link = %+v", link)
	}
	if len(res.AttachmentIDs) != 2 || res.AttachmentIDs[0] != uid(0xA1) || res.AttachmentIDs[1] != uid(0xA2) {
		t.Errorf("res.AttachmentIDs = %v", res.AttachmentIDs)
	}
}

func TestAppendUserMessage_StagedAttachments_IssueDestined(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	res, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Sender: uid(7), WorkspaceID: uid(9),
		Body: "/issue broken [image: image-1.png]", MessageID: "m1",
		Staged: stagedFixture(), MediaChatBind: false,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if len(f.attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(f.attachments))
	}
	for i, a := range f.attachments {
		if a.ChatSessionID.Valid {
			t.Errorf("attachment[%d] must not be chat-bound: %+v", i, a)
		}
	}
	if len(f.linkCalls) != 0 {
		t.Errorf("linkCalls = %d, want 0", len(f.linkCalls))
	}
	if len(res.AttachmentIDs) != 2 {
		t.Errorf("res.AttachmentIDs = %v", res.AttachmentIDs)
	}
}

func TestAppendUserMessage_AttachmentFailureRollsBack(t *testing.T) {
	f := newFake()
	f.attachmentErr = fmt.Errorf("insert failed")
	s := newTestSession(f)
	_, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Sender: uid(7), WorkspaceID: uid(9),
		Body: "x", MessageID: "m1", ClaimToken: uid(3),
		Staged: stagedFixture(), MediaChatBind: true,
	})
	if err == nil {
		t.Fatal("expected an error from the attachment insert")
	}
	if len(f.linkCalls) != 0 {
		t.Errorf("linkCalls = %d, want 0", len(f.linkCalls))
	}
	if f.marks != 0 {
		t.Errorf("dedup mark ran despite the failed tx (marks=%d)", f.marks)
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
