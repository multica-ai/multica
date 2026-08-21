package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channelmedia"
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
	bindings              map[string]pgtype.UUID
	nextSession           byte
	createdSessions       int
	messages              []string
	messageID             pgtype.UUID
	lastCreate            db.CreateChatMessageParams
	touched               int
	replyTargets          int
	lockedWorkspace       int    // count of LockWorkspaceForChatSessionCreate calls
	lastConfig            []byte // config of the most recent CreateChannelChatSessionBinding
	attachments           []db.CreateAttachmentParams
	linked                db.LinkAttachmentsToChatMessageParams
	mediaCleared          int
	updatedMediaContent   string
	updateMediaRows       int64
	issueMediaMarkdown    string
	issueMediaBase        pgtype.Text
	issueMediaDescription string
	reconcilerOwnedKeys   map[string]bool
	issueLookupErr        error

	markRows         int64 // MarkChannelInboundDedupProcessed result
	pendingFresh     bool
	createBindingErr error // simulate a unique violation on create
	raceWinner       pgtype.UUID
}

func newFake() *fakeSessionQueries {
	return &fakeSessionQueries{bindings: map[string]pgtype.UUID{}, markRows: 1, messageID: uid(42), updateMediaRows: 1}
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

func (f *fakeSessionQueries) LockChatSessionForAppend(_ context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	return id, nil
}

func (f *fakeSessionQueries) CreateChatMessage(_ context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error) {
	f.messages = append(f.messages, arg.Content)
	f.lastCreate = arg
	return db.ChatMessage{ID: f.messageID}, nil
}

func (f *fakeSessionQueries) ClearChatMessageChannelMediaPending(context.Context, db.ClearChatMessageChannelMediaPendingParams) error {
	f.mediaCleared++
	return nil
}

func (f *fakeSessionQueries) LockIssueForChannelMediaBind(_ context.Context, arg db.LockIssueForChannelMediaBindParams) (pgtype.UUID, error) {
	if f.issueLookupErr != nil {
		return pgtype.UUID{}, f.issueLookupErr
	}
	return arg.ID, nil
}

func (f *fakeSessionQueries) UpdateChatMessageContentForChannelMedia(_ context.Context, arg db.UpdateChatMessageContentForChannelMediaParams) (int64, error) {
	f.updatedMediaContent = arg.Content
	return f.updateMediaRows, nil
}

func (f *fakeSessionQueries) MaterializeIssueChannelMediaMarkdown(_ context.Context, arg db.MaterializeIssueChannelMediaMarkdownParams) (db.Issue, error) {
	f.issueMediaMarkdown = arg.Markdown.String
	f.issueMediaBase = arg.BaseDescription
	f.issueMediaDescription = arg.Description
	return db.Issue{ID: arg.ID, WorkspaceID: arg.WorkspaceID}, nil
}

func (f *fakeSessionQueries) CreateAttachment(_ context.Context, arg db.CreateAttachmentParams) (db.Attachment, error) {
	f.attachments = append(f.attachments, arg)
	return db.Attachment{ID: arg.ID}, nil
}

func (f *fakeSessionQueries) LinkAttachmentsToChatMessage(_ context.Context, arg db.LinkAttachmentsToChatMessageParams) ([]pgtype.UUID, error) {
	f.linked = arg
	return append([]pgtype.UUID(nil), arg.AttachmentIds...), nil
}

func (f *fakeSessionQueries) ClaimChannelMediaPendingObjectsForBind(_ context.Context, arg db.ClaimChannelMediaPendingObjectsForBindParams) ([]string, error) {
	if f.reconcilerOwnedKeys == nil {
		return append([]string(nil), arg.StorageKeys...), nil
	}
	var claimed []string
	for _, k := range arg.StorageKeys {
		if !f.reconcilerOwnedKeys[k] {
			claimed = append(claimed, k)
		}
	}
	return claimed, nil
}

func (f *fakeSessionQueries) TouchChatSession(context.Context, pgtype.UUID) error {
	f.touched++
	return nil
}

func (f *fakeSessionQueries) MarkChannelChatSessionPendingFresh(context.Context, pgtype.UUID) (bool, error) {
	f.pendingFresh = true
	return true, nil
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

func TestAppendUserMessage_BeforeWriteFenceRejectsWithoutDurableWrites(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	guardCalls := 0
	_, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1),
		Body:      "must be rerouted",
		BeforeWrite: func(_ context.Context, _ pgx.Tx) error {
			guardCalls++
			return ErrRouteChanged
		},
	})
	if !errors.Is(err, ErrRouteChanged) {
		t.Fatalf("append fence error = %v, want route changed", err)
	}
	if guardCalls != 1 || len(f.messages) != 0 || f.touched != 0 {
		t.Fatalf("rejected append guard=%d messages=%d touches=%d, want 1/0/0", guardCalls, len(f.messages), f.touched)
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
	if !f.lastCreate.MessageKind.Valid || f.lastCreate.MessageKind.String != channelCommandMessageKind {
		t.Errorf("handled command message kind = %+v", f.lastCreate.MessageKind)
	}
	// The stored message is still the full (enriched) body.
	if f.messages[0] != "> quoted context from another message\n/issue Real intent" {
		t.Errorf("stored body should be the enriched Body: %q", f.messages[0])
	}
}

func TestAppendUserMessage_OrdinaryTurnKeepsDefaultMessageKind(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	if _, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Body: "hello", CommandText: "hello", MessageID: "m1",
	}); err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if f.lastCreate.MessageKind.Valid {
		t.Fatalf("ordinary message kind must use the database default: %+v", f.lastCreate.MessageKind)
	}
}

func TestBindMediaRefs_CreatesAndLinksChatAttachments(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	body := "Use [Image] literally\n[Image]"
	res, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1),
		Sender:    uid(7),
		Body:      body,
		MessageID: "om_image",
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if res.IssueCommand != nil {
		t.Fatalf("media placeholder must not parse as /issue: %+v", res.IssueCommand)
	}
	if res.MessageID != f.messageID {
		t.Fatalf("message id = %v, want %v", res.MessageID, f.messageID)
	}
	if !f.lastCreate.ChannelIngested.Valid || !f.lastCreate.ChannelIngested.Bool {
		t.Fatalf("channel append must stamp channel_ingested, got %+v", f.lastCreate.ChannelIngested)
	}
	ref := channel.MediaRef{
		Type:              channel.MsgTypeImage,
		StorageKey:        "lark/cli/img.png",
		StorageURL:        "https://cdn.example.test/lark/cli/img.png",
		Filename:          "screenshot.png",
		MimeType:          "image/png",
		SizeBytes:         3,
		InlinePlaceholder: "[Image]",
		InlineIndex:       1,
	}
	err = s.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:   res.MessageID,
		SessionID:   uid(1),
		WorkspaceID: uid(9),
		Sender:      uid(7),
		Body:        body,
		MediaRefs:   []channel.MediaRef{ref},
	})
	if err != nil {
		t.Fatalf("BindMediaRefs: %v", err)
	}
	if len(f.attachments) != 1 {
		t.Fatalf("attachments created = %d, want 1", len(f.attachments))
	}
	att := f.attachments[0]
	if att.WorkspaceID != uid(9) || att.ChatSessionID != uid(1) || att.UploaderType != "member" || att.UploaderID != uid(7) {
		t.Fatalf("attachment ownership/session wrong: %+v", att)
	}
	if att.IssueID.Valid {
		t.Fatalf("plain chat attachment unexpectedly targeted issue %v", att.IssueID)
	}
	if att.Filename != "screenshot.png" || att.Url != "https://cdn.example.test/lark/cli/img.png" ||
		att.ContentType != "image/png" || att.SizeBytes != 3 {
		t.Fatalf("attachment metadata wrong: %+v", att)
	}
	if f.linked.ChatMessageID != res.MessageID || f.linked.ChatSessionID != uid(1) || f.linked.WorkspaceID != uid(9) {
		t.Fatalf("link params wrong: %+v", f.linked)
	}
	if len(f.linked.AttachmentIds) != 1 || f.linked.AttachmentIds[0] != att.ID {
		t.Fatalf("linked ids = %+v, want attachment id %v", f.linked.AttachmentIds, att.ID)
	}
	if want := "Use [Image] literally\n" + inlineAttachmentMarkdown(ref, att.ID); f.updatedMediaContent != want {
		t.Fatalf("updated content = %q, want %q", f.updatedMediaContent, want)
	}
}

func TestComposeInlineMediaBody_PartialResolutionKeepsFailedPlaceholderInPlace(t *testing.T) {
	body := "[Image]\n这是啥?\n[Image]\n这又是啥?"
	got, changed := composeInlineMediaBody(body, []inlineMediaReplacement{{
		placeholder: "[Image]",
		index:       1,
		markdown:    "![](/api/attachments/second/download)",
	}})
	if !changed {
		t.Fatal("expected the successful second image to update the body")
	}
	want := "[Image]\n这是啥?\n![](/api/attachments/second/download)\n这又是啥?"
	if got != want {
		t.Fatalf("composed body = %q, want %q", got, want)
	}
}

func TestComposeInlineMediaBody_ReplacesMarkersWithoutAddingWhitespace(t *testing.T) {
	body := "前[Image]中\n[Image]后"
	got, changed := composeInlineMediaBody(body, []inlineMediaReplacement{
		{placeholder: "[Image]", index: 0, markdown: "![](/api/attachments/first/download)"},
		{placeholder: "[Image]", index: 1, markdown: "![](/api/attachments/second/download)"},
	})
	if !changed {
		t.Fatal("expected both inline image markers to be replaced")
	}
	want := "前![](/api/attachments/first/download)中\n![](/api/attachments/second/download)后"
	if got != want {
		t.Fatalf("replacement changed surrounding whitespace: got %q, want %q", got, want)
	}
}

func TestComposeIssueCommandMediaDescriptionPreservesRichTextOrder(t *testing.T) {
	body := "/issue explain below questions\nWhat is this?\n[Image]\nAnd what is this?\n[Image]"
	got, changed := composeIssueCommandMediaDescription(body, "/issue explain below questions\nWhat is this?And what is this?", []inlineMediaReplacement{
		{placeholder: "[Image]", index: 0, markdown: "![](first)\n\n<!-- first -->"},
		{placeholder: "[Image]", index: 1, markdown: "![](second)\n\n<!-- second -->"},
	}, "flattened fallback")
	if !changed {
		t.Fatal("expected issue description media to be materialized")
	}
	want := "What is this?\n![](first)\n\n<!-- first -->\nAnd what is this?\n![](second)\n\n<!-- second -->"
	if got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
}

func TestComposeIssueCommandMediaDescriptionKeepsOnlyMediaBeforeCommand(t *testing.T) {
	body := "> quoted context\n[Image]\n/issue explain\nDetails"
	got, changed := composeIssueCommandMediaDescription(body, "/issue explain\nDetails", []inlineMediaReplacement{
		{placeholder: "[Image]", index: 0, markdown: "![](first)\n\n<!-- first -->"},
	}, "Details")
	if !changed {
		t.Fatal("expected leading media to be materialized")
	}
	want := "![](first)\n\n<!-- first -->\n\nDetails"
	if got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
}

func TestComposeIssueCommandMediaDescriptionFallsBackWhenMarkerIsInsideDirective(t *testing.T) {
	got, changed := composeIssueCommandMediaDescription(
		"/issue explain [Image]\nDetails",
		"/issue explain [Image]\nDetails",
		[]inlineMediaReplacement{{placeholder: "[Image]", index: 0, markdown: "![](first)"}},
		"Details",
	)
	if changed || got != "Details" {
		t.Fatalf("compose = %q, changed=%v; want fallback", got, changed)
	}
}

func TestComposeIssueCommandMediaDescriptionIgnoresEnrichedIssueLine(t *testing.T) {
	body := "<quoted_message>\n/issue Old intent\n</quoted_message>\n/issue Real intent\nDetails\n[Image]"
	got, changed := composeIssueCommandMediaDescription(
		body,
		"/issue Real intent\nDetails",
		[]inlineMediaReplacement{{placeholder: "[Image]", index: 0, markdown: "![](first)"}},
		"Details\n[Image]",
	)
	if !changed || got != "Details\n![](first)" {
		t.Fatalf("compose = %q, changed=%v; want real command suffix", got, changed)
	}
}

func TestBindMediaRefs_MaterializesIssueImagesInOriginalOrder(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	body := "/issue explain below questions\nWhat is this?\n[Image]\nAnd what is this?\n[Image]"
	commandText := "/issue explain below questions\nWhat is this?And what is this?"
	base := issueDescriptionFromCommandBody(body, commandText, "")
	err := s.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:            uid(42),
		SessionID:            uid(1),
		WorkspaceID:          uid(9),
		Sender:               uid(7),
		IssueID:              uid(8),
		IssueDescriptionBase: pgtype.Text{String: base, Valid: true},
		IssueCommandText:     commandText,
		Body:                 body,
		MediaRefs: []channel.MediaRef{
			{
				Type: channel.MsgTypeImage, StorageKey: "dingtalk/first", StorageURL: "https://cdn.test/first",
				Filename: "first.png", MimeType: "image/png", InlinePlaceholder: "[Image]", InlineIndex: 0,
			},
			{
				Type: channel.MsgTypeImage, StorageKey: "dingtalk/second", StorageURL: "https://cdn.test/second",
				Filename: "second.png", MimeType: "image/png", InlinePlaceholder: "[Image]", InlineIndex: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("BindMediaRefs: %v", err)
	}
	if f.issueMediaBase != (pgtype.Text{String: base, Valid: true}) {
		t.Fatalf("issue media base = %#v, want %q", f.issueMediaBase, base)
	}
	first := channelmedia.Block(uuidString(f.attachments[0].ID), "first.png", true)
	second := channelmedia.Block(uuidString(f.attachments[1].ID), "second.png", true)
	want := "What is this?\n" + first + "\nAnd what is this?\n" + second
	if f.issueMediaDescription != want {
		t.Fatalf("issue media description = %q, want %q", f.issueMediaDescription, want)
	}
}

func TestBindMediaRefs_CreatesIssueOwnedAttachments(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	err := s.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:   uid(42),
		SessionID:   uid(1),
		WorkspaceID: uid(9),
		Sender:      uid(7),
		IssueID:     uid(8),
		Body:        "[Image]",
		MediaRefs: []channel.MediaRef{{
			Type:              channel.MsgTypeImage,
			StorageKey:        "lark/cli/issue.png",
			StorageURL:        "https://cdn.example.test/lark/cli/issue.png",
			Filename:          "issue.png",
			MimeType:          "image/png",
			SizeBytes:         3,
			InlinePlaceholder: "[Image]",
		}},
	})
	if err != nil {
		t.Fatalf("BindMediaRefs: %v", err)
	}
	if len(f.attachments) != 1 {
		t.Fatalf("attachments created = %d, want 1", len(f.attachments))
	}
	att := f.attachments[0]
	if att.IssueID != uid(8) {
		t.Fatalf("attachment issue = %v, want %v", att.IssueID, uid(8))
	}
	if att.ChatSessionID.Valid {
		t.Fatalf("issue attachment must not retain chat-session ownership: %+v", att.ChatSessionID)
	}
	if f.linked.ChatMessageID.Valid || len(f.linked.AttachmentIds) != 0 {
		t.Fatalf("issue attachment must not also bind to chat message: %+v", f.linked)
	}
	if f.updatedMediaContent != "" {
		t.Fatalf("issue-owned media must not rewrite the chat command body: %q", f.updatedMediaContent)
	}
	wantMarkdown := channelmedia.Block(uuidString(att.ID), "issue.png", true)
	if f.issueMediaMarkdown != wantMarkdown {
		t.Fatalf("issue media markdown = %q, want %q", f.issueMediaMarkdown, wantMarkdown)
	}
	if f.mediaCleared != 1 {
		t.Fatalf("media pending marker clears = %d, want 1", f.mediaCleared)
	}
}

func TestBindMediaRefs_UsesGeneratedFilenameInIssueMarkdown(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	err := s.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:   uid(42),
		SessionID:   uid(1),
		WorkspaceID: uid(9),
		Sender:      uid(7),
		IssueID:     uid(8),
		MediaRefs: []channel.MediaRef{{
			Type:       channel.MsgTypeFile,
			StorageKey: "dingtalk/file",
			StorageURL: "https://cdn.example.test/dingtalk/file",
			MimeType:   "application/pdf",
		}},
	})
	if err != nil {
		t.Fatalf("BindMediaRefs: %v", err)
	}
	att := f.attachments[0]
	wantFilename := defaultMediaFilename(channel.MsgTypeFile, uuidString(att.ID), "application/pdf")
	if att.Filename != wantFilename {
		t.Fatalf("attachment filename = %q, want %q", att.Filename, wantFilename)
	}
	wantMarkdown := channelmedia.Block(uuidString(att.ID), wantFilename, false)
	if f.issueMediaMarkdown != wantMarkdown {
		t.Fatalf("issue media markdown = %q, want %q", f.issueMediaMarkdown, wantMarkdown)
	}
}

func TestBindMediaRefs_MissingIssueRollsBackAndClearsPendingMarker(t *testing.T) {
	f := newFake()
	f.issueLookupErr = pgx.ErrNoRows
	s := newTestSession(f)
	err := s.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID:   uid(42),
		SessionID:   uid(1),
		WorkspaceID: uid(9),
		Sender:      uid(7),
		IssueID:     uid(8),
		MediaRefs: []channel.MediaRef{{
			StorageKey: "lark/cli/deleted-issue.png",
			StorageURL: "https://cdn.example.test/lark/cli/deleted-issue.png",
		}},
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("BindMediaRefs error = %v, want missing issue", err)
	}
	if len(f.attachments) != 0 || len(f.linked.AttachmentIds) != 0 {
		t.Fatalf("missing issue created or linked attachments: created=%d linked=%d", len(f.attachments), len(f.linked.AttachmentIds))
	}
	if f.mediaCleared != 1 {
		t.Fatalf("media pending marker clears = %d, want 1", f.mediaCleared)
	}
}

func TestAppendUserMessage_BareIssueKeepsTitleEmpty(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	res, err := s.AppendUserMessage(context.Background(), AppendInput{SessionID: uid(1), Body: "/issue", MessageID: "m2"})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if res.IssueCommand == nil || res.IssueCommand.Title != "" {
		t.Errorf("bare /issue must remain titleless for the Router usage result: %+v", res.IssueCommand)
	}
}

func TestAppendUserMessage_FreshMessagePersistsPendingIntent(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	if _, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Body: "start over", MessageID: "m2", ForceFresh: true,
	}); err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if !f.pendingFresh {
		t.Fatal("fresh message did not persist pending intent in the append transaction")
	}
}

func TestMarkPendingFresh_BareCommandPersistsIntent(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	if err := s.MarkPendingFresh(context.Background(), uid(1)); err != nil {
		t.Fatalf("MarkPendingFresh: %v", err)
	}
	if !f.pendingFresh {
		t.Fatal("bare fresh command did not persist pending intent")
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
