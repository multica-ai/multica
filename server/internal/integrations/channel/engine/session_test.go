package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
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
type fakeTx struct {
	pgx.Tx
	commitErr  error
	committed  bool
	rolledBack bool
}

func (tx *fakeTx) Commit(context.Context) error {
	tx.committed = true
	return tx.commitErr
}
func (tx *fakeTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

type fakeTxStarter struct {
	tx       *fakeTx
	beginErr error
}

func (s fakeTxStarter) Begin(context.Context) (pgx.Tx, error) {
	if s.beginErr != nil {
		return nil, s.beginErr
	}
	if s.tx != nil {
		return s.tx, nil
	}
	return &fakeTx{}, nil
}

type recordingTx struct {
	pgx.Tx
	commits   int
	rollbacks int
}

func (t *recordingTx) Commit(context.Context) error {
	t.commits++
	return nil
}

func (t *recordingTx) Rollback(context.Context) error {
	t.rollbacks++
	return nil
}

type recordingTxStarter struct{ tx *recordingTx }

func (s recordingTxStarter) Begin(context.Context) (pgx.Tx, error) { return s.tx, nil }

// fakeSessionQueries is an in-memory SessionQueries for unit tests.
type fakeSessionQueries struct {
	bindings              map[string]pgtype.UUID
	nextSession           byte
	createdSessions       int
	lastSessionCreate     db.CreateChatSessionParams
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
	initializedMediaTitle string
	initializedTitle      string
	explicitlyCreated     bool
	hasPublicUserMessage  bool
	sessionTitle          string
	routeRetired          bool
	lastRetire            db.RetireChannelChatSessionBindingParams

	markRows           int64 // MarkChannelInboundDedupProcessed result
	lastMark           db.MarkChannelInboundDedupProcessedParams
	pendingFresh       bool
	contextRevision    int64
	contextInitiator   pgtype.UUID
	boundaryPending    bool
	lockContextErr     error
	advanceContextErr  error
	resolveContextErr  error
	setInitiatorErr    error
	pendingContextsErr error
	createBindingErr   error // simulate a unique violation on create
	raceWinner         pgtype.UUID
}

func newFake() *fakeSessionQueries {
	return &fakeSessionQueries{bindings: map[string]pgtype.UUID{}, markRows: 1, messageID: uid(42), updateMediaRows: 1, contextRevision: 1}
}

func bindKey(inst pgtype.UUID, chat string) string { return fmt.Sprintf("%x|%s", inst.Bytes, chat) }

func (f *fakeSessionQueries) WithTx(tx pgx.Tx) SessionQueries { return f }

func (f *fakeSessionQueries) GetChannelChatSessionBinding(_ context.Context, arg db.GetChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	if id, ok := f.bindings[bindKey(arg.InstallationID, arg.ChannelChatID)]; ok {
		return db.ChannelChatSessionBinding{ID: uid(89), ChatSessionID: id}, nil
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
	f.lastSessionCreate = arg
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

func (f *fakeSessionQueries) CreateChannelChatSessionBindingGeneration(_ context.Context, arg db.CreateChannelChatSessionBindingGenerationParams) (db.ChannelChatSessionBinding, error) {
	f.lastConfig = arg.Config
	f.bindings[bindKey(arg.InstallationID, arg.ChannelChatID)] = arg.ChatSessionID
	return db.ChannelChatSessionBinding{ID: uid(90), ChatSessionID: arg.ChatSessionID, RouteRevision: arg.RouteRevision}, nil
}

func (f *fakeSessionQueries) LockCurrentChannelChatSessionBinding(_ context.Context, arg db.LockCurrentChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	if id, ok := f.bindings[bindKey(arg.InstallationID, arg.ChannelChatID)]; ok {
		return db.ChannelChatSessionBinding{ID: uid(89), ChatSessionID: id, RouteRevision: 1}, nil
	}
	return db.ChannelChatSessionBinding{}, pgx.ErrNoRows
}

func (f *fakeSessionQueries) LockCurrentChannelChatSessionBindingBySession(_ context.Context, sessionID pgtype.UUID) (db.ChannelChatSessionBinding, error) {
	if !f.routeRetired {
		return db.ChannelChatSessionBinding{
			ID: uid(89), ChatSessionID: sessionID, ContextRevision: f.contextRevision,
			PendingFresh: f.pendingFresh, RouteRevision: 1,
		}, nil
	}
	for _, id := range f.bindings {
		if id == sessionID {
			return db.ChannelChatSessionBinding{
				ID: uid(89), ChatSessionID: id, ContextRevision: f.contextRevision,
				PendingFresh: f.pendingFresh, RouteRevision: 1,
			}, nil
		}
	}
	return db.ChannelChatSessionBinding{}, pgx.ErrNoRows
}

func (f *fakeSessionQueries) RetireChannelChatSessionBinding(_ context.Context, arg db.RetireChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	f.lastRetire = arg
	return db.ChannelChatSessionBinding{}, nil
}

func (f *fakeSessionQueries) LockChatSessionForAppend(_ context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	return id, nil
}

func (f *fakeSessionQueries) GetChatSession(_ context.Context, id pgtype.UUID) (db.ChatSession, error) {
	return db.ChatSession{
		ID:                  id,
		Title:               f.sessionTitle,
		ExplicitlyCreatedAt: pgtype.Timestamptz{Valid: f.explicitlyCreated},
	}, nil
}

func (f *fakeSessionQueries) ChatSessionHasPublicUserMessage(context.Context, pgtype.UUID) (bool, error) {
	return f.hasPublicUserMessage, nil
}

func (f *fakeSessionQueries) MarkChatSessionExplicitlyCreated(_ context.Context, id pgtype.UUID) (db.ChatSession, error) {
	f.explicitlyCreated = true
	return db.ChatSession{ID: id}, nil
}

func (f *fakeSessionQueries) InitializeChatSessionTitle(_ context.Context, arg db.InitializeChatSessionTitleParams) (db.ChatSession, error) {
	if f.sessionTitle != "" || f.hasPublicUserMessage {
		return db.ChatSession{}, pgx.ErrNoRows
	}
	f.initializedTitle = arg.Title
	f.sessionTitle = arg.Title
	return db.ChatSession{ID: arg.ID, Title: arg.Title}, nil
}

func (f *fakeSessionQueries) ReplaceImplicitChatSessionTitle(_ context.Context, arg db.ReplaceImplicitChatSessionTitleParams) (db.ChatSession, error) {
	f.initializedTitle = arg.Title
	f.sessionTitle = arg.Title
	return db.ChatSession{ID: arg.ID, Title: arg.Title}, nil
}

func (f *fakeSessionQueries) InitializeChatSessionMediaTitle(_ context.Context, arg db.InitializeChatSessionMediaTitleParams) (db.ChatSession, error) {
	f.initializedMediaTitle = arg.Title
	return db.ChatSession{ID: arg.ID, Title: arg.Title}, nil
}

func (f *fakeSessionQueries) CreateChatMessage(_ context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error) {
	f.messages = append(f.messages, arg.Content)
	f.lastCreate = arg
	if arg.Role == "user" && (!arg.MessageKind.Valid || arg.MessageKind.String != channelCommandMessageKind) {
		f.hasPublicUserMessage = true
	}
	return db.ChatMessage{ID: f.messageID}, nil
}

func (f *fakeSessionQueries) ListUnownedChannelChatContextRevisions(context.Context, pgtype.UUID) ([]PendingContext, error) {
	if f.pendingContextsErr != nil {
		return nil, f.pendingContextsErr
	}
	return []PendingContext{{Revision: f.contextRevision, InitiatorUserID: f.contextInitiator}}, nil
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

func (f *fakeSessionQueries) LockChannelChatSessionBindingForContext(context.Context, pgtype.UUID) (db.ChannelChatSessionBinding, error) {
	if f.lockContextErr != nil {
		return db.ChannelChatSessionBinding{}, f.lockContextErr
	}
	return db.ChannelChatSessionBinding{
		ContextRevision: f.contextRevision,
	}, nil
}

func (f *fakeSessionQueries) LockChannelChatContextGenerationByRevision(context.Context, db.LockChannelChatContextGenerationByRevisionParams) (db.ChannelChatContextGeneration, error) {
	if f.lockContextErr != nil {
		return db.ChannelChatContextGeneration{}, f.lockContextErr
	}
	return db.ChannelChatContextGeneration{
		Revision: f.contextRevision, PendingFresh: f.pendingFresh,
		HistoryBoundaryPending: f.boundaryPending,
	}, nil
}

func (f *fakeSessionQueries) AdvanceChannelChatContextGeneration(_ context.Context, arg db.AdvanceChannelChatContextGenerationParams) (db.AdvanceChannelChatContextGenerationRow, error) {
	if f.advanceContextErr != nil {
		return db.AdvanceChannelChatContextGenerationRow{}, f.advanceContextErr
	}
	f.contextRevision = arg.CurrentRevision + 1
	f.pendingFresh = true
	f.boundaryPending = !arg.HasMessageBody
	return db.AdvanceChannelChatContextGenerationRow{Revision: f.contextRevision, PendingFresh: true, HistoryBoundaryPending: f.boundaryPending}, nil
}

func (f *fakeSessionQueries) ResolveChannelChatContextHistoryStart(context.Context, db.ResolveChannelChatContextHistoryStartParams) error {
	if f.resolveContextErr != nil {
		return f.resolveContextErr
	}
	f.boundaryPending = false
	return nil
}

func (f *fakeSessionQueries) SetChannelChatContextInitiator(_ context.Context, arg db.SetChannelChatContextInitiatorParams) (pgtype.UUID, error) {
	if f.setInitiatorErr != nil {
		return pgtype.UUID{}, f.setInitiatorErr
	}
	f.contextInitiator = arg.InitiatorUserID
	return arg.InitiatorUserID, nil
}

func (f *fakeSessionQueries) UpdateChannelChatSessionBindingReplyTarget(context.Context, db.UpdateChannelChatSessionBindingReplyTargetParams) error {
	f.replyTargets++
	return nil
}

func (f *fakeSessionQueries) MarkChannelInboundDedupProcessed(_ context.Context, arg db.MarkChannelInboundDedupProcessedParams) (int64, error) {
	f.lastMark = arg
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
	if f.lastSessionCreate.Title != "" {
		t.Fatalf("implicit Chat title = %q, want empty until first effective message", f.lastSessionCreate.Title)
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

func TestStartSessionCreatesExplicitEmptyGeneration(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	result, err := s.StartSession(context.Background(), StartSessionInput{
		EnsureSessionInput: EnsureSessionInput{
			WorkspaceID: uid(2), AgentID: uid(3), InstallationID: uid(1), Sender: uid(7),
			BindingKey: "chatA", ChatType: channel.ChatTypeP2P,
		},
		MessageID: "command-1", ClaimToken: uid(8),
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if !result.SessionID.Valid || len(f.messages) != 0 {
		t.Fatalf("session=%v messages=%v", result.SessionID.Valid, f.messages)
	}
	if f.createdSessions != 1 || f.markRows != 1 {
		t.Fatalf("created=%d markRows=%d", f.createdSessions, f.markRows)
	}
	if !f.lastSessionCreate.ID.Valid || uuid.UUID(f.lastSessionCreate.ID.Bytes).Version() != 7 {
		t.Fatalf("started Chat id = %v, want an explicit UUIDv7", f.lastSessionCreate.ID)
	}
}

func TestStartSessionBodyCreatesOrdinaryFirstMessageAndTitle(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	result, err := s.StartSession(context.Background(), StartSessionInput{
		EnsureSessionInput: EnsureSessionInput{
			WorkspaceID: uid(2), AgentID: uid(3), InstallationID: uid(1), Sender: uid(7),
			BindingKey: "chatA", ChatType: channel.ChatTypeP2P,
		},
		Initiator: uid(8), Body: "# 发布检查", MessageID: "command-1", ClaimToken: uid(9), PersistMessage: true,
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if len(f.messages) != 1 || f.messages[0] != "# 发布检查" {
		t.Fatalf("messages=%v", f.messages)
	}
	if f.lastCreate.MessageKind.Valid {
		t.Fatalf("message kind=%q, want ordinary", f.lastCreate.MessageKind.String)
	}
	if f.lastSessionCreate.CreatorID != uid(7) {
		t.Fatalf("Chat creator=%v, want route owner %v", f.lastSessionCreate.CreatorID, uid(7))
	}
	if f.contextInitiator != uid(8) || len(result.Append.PendingContexts) != 1 || result.Append.PendingContexts[0].InitiatorUserID != uid(8) {
		t.Fatalf("context initiator=%v pending=%+v, want command sender %v", f.contextInitiator, result.Append.PendingContexts, uid(8))
	}
}

func TestStartSessionMediaBeforeTextUsesUserTextForTitle(t *testing.T) {
	f := newFake()
	result, err := newTestSession(f).StartSession(context.Background(), StartSessionInput{
		EnsureSessionInput: EnsureSessionInput{
			WorkspaceID: uid(2), AgentID: uid(3), InstallationID: uid(1), Sender: uid(7),
			BindingKey: "chatA", ChatType: channel.ChatTypeP2P,
		},
		Initiator: uid(7), Body: "[Image]\n点评一下", MediaPendingSeconds: 45, PersistMessage: true,
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if f.lastSessionCreate.Title != "点评一下" || result.Append.InitialTitle != "点评一下" {
		t.Fatalf("media-before-text title = stored %q result %q, want user text", f.lastSessionCreate.Title, result.Append.InitialTitle)
	}
}

func TestStartSessionRunsBeforeCommitInOwningTransaction(t *testing.T) {
	f := newFake()
	tx := &recordingTx{}
	s := newChatSessionWith(f, recordingTxStarter{tx: tx}, channel.Type("slack"), SessionTitles{})
	called := false
	result, err := s.StartSession(context.Background(), StartSessionInput{
		EnsureSessionInput: EnsureSessionInput{
			WorkspaceID: uid(2), AgentID: uid(3), InstallationID: uid(1), Sender: uid(7),
			BindingKey: "chatA", ChatType: channel.ChatTypeP2P,
		},
		Initiator: uid(7), Body: "ship it", PersistMessage: true,
		BeforeCommit: func(_ context.Context, gotTx pgx.Tx, session db.ChatSession) error {
			called = true
			if gotTx != tx {
				t.Fatal("BeforeCommit did not receive StartSession's transaction")
			}
			if !session.ID.Valid {
				t.Fatal("BeforeCommit did not receive the created Chat")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if !called || result.SessionID != uid(1) || tx.commits != 1 {
		t.Fatalf("called=%v session=%v commits=%d", called, result.SessionID, tx.commits)
	}
}

func TestStartSessionBeforeCommitFailurePreventsCommit(t *testing.T) {
	f := newFake()
	tx := &recordingTx{}
	s := newChatSessionWith(f, recordingTxStarter{tx: tx}, channel.Type("slack"), SessionTitles{})
	wantErr := errors.New("enqueue failed")
	_, err := s.StartSession(context.Background(), StartSessionInput{
		EnsureSessionInput: EnsureSessionInput{
			WorkspaceID: uid(2), AgentID: uid(3), InstallationID: uid(1), Sender: uid(7),
			BindingKey: "chatA", ChatType: channel.ChatTypeP2P,
		},
		Initiator: uid(7), Body: "ship it", PersistMessage: true,
		BeforeCommit: func(context.Context, pgx.Tx, db.ChatSession) error { return wantErr },
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want %v", err, wantErr)
	}
	if tx.commits != 0 || tx.rollbacks == 0 {
		t.Fatalf("commits=%d rollbacks=%d, want no commit and rollback", tx.commits, tx.rollbacks)
	}
}

func TestAppendUserMessageRunsBeforeCommitWithNativeDedup(t *testing.T) {
	f := newFake()
	tx := &recordingTx{}
	s := newChatSessionWith(f, recordingTxStarter{tx: tx}, channel.Type("slack"), SessionTitles{})
	called := false
	result, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Sender: uid(2), InstallationID: uid(3),
		Body: "start clean", CommandText: "/clear", ForceFresh: true,
		DedupMessageID: "env-clear", ClaimToken: uid(4),
		BeforeCommit: func(_ context.Context, gotTx pgx.Tx, session db.ChatSession, contextRevision int64, bindingID pgtype.UUID, routeRevision int64) error {
			called = true
			if gotTx != tx || session.ID != uid(1) {
				t.Fatalf("callback tx/session = %v/%v", gotTx == tx, session.ID)
			}
			if contextRevision != 2 || bindingID != uid(89) || routeRevision != 1 {
				t.Fatalf("callback context/binding/route = %d/%v/%d", contextRevision, bindingID, routeRevision)
			}
			if f.lastMark.MessageID != "env-clear" {
				t.Fatalf("dedup message = %q", f.lastMark.MessageID)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if !called || !result.DedupMarked || result.ContextRevision != 2 || !f.boundaryPending || f.replyTargets != 0 || tx.commits != 1 {
		t.Fatalf("called=%t marked=%t revision=%d pending=%t replies=%d commits=%d", called, result.DedupMarked, result.ContextRevision, f.boundaryPending, f.replyTargets, tx.commits)
	}
}

func TestAppendUserMessageBeforeCommitFailureRollsBackNativeTurn(t *testing.T) {
	f := newFake()
	tx := &recordingTx{}
	s := newChatSessionWith(f, recordingTxStarter{tx: tx}, channel.Type("slack"), SessionTitles{})
	wantErr := errors.New("enqueue failed")
	_, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Sender: uid(2), Body: "start clean", ForceFresh: true,
		BeforeCommit: func(context.Context, pgx.Tx, db.ChatSession, int64, pgtype.UUID, int64) error {
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want %v", err, wantErr)
	}
	if tx.commits != 0 || tx.rollbacks == 0 {
		t.Fatalf("commits=%d rollbacks=%d, want no commit and rollback", tx.commits, tx.rollbacks)
	}
}

func TestMarkPendingFreshWithDedupIsAtomic(t *testing.T) {
	f := newFake()
	tx := &recordingTx{}
	s := newChatSessionWith(f, recordingTxStarter{tx: tx}, channel.Type("slack"), SessionTitles{})
	if err := s.MarkPendingFreshWithDedup(context.Background(), uid(1), "", uid(2), "env-clear", uid(3)); err != nil {
		t.Fatalf("MarkPendingFreshWithDedup: %v", err)
	}
	if f.lastMark.MessageID != "env-clear" || f.lastMark.InstallationID != uid(2) || !f.boundaryPending || tx.commits != 1 {
		t.Fatalf("mark=%+v pending=%t commits=%d", f.lastMark, f.boundaryPending, tx.commits)
	}
}

func TestMarkPendingFreshWithDedupRejectsLostClaim(t *testing.T) {
	f := newFake()
	f.markRows = 0
	tx := &recordingTx{}
	s := newChatSessionWith(f, recordingTxStarter{tx: tx}, channel.Type("slack"), SessionTitles{})
	err := s.MarkPendingFreshWithDedup(context.Background(), uid(1), "", uid(2), "env-clear", uid(3))
	if !errors.Is(err, ErrClaimLost) {
		t.Fatalf("error=%v, want ErrClaimLost", err)
	}
	if tx.commits != 0 || tx.rollbacks == 0 {
		t.Fatalf("commits=%d rollbacks=%d, want no commit and rollback", tx.commits, tx.rollbacks)
	}
}

func TestStartSessionPendingSlackBoundaryLeavesRetiredEndOpen(t *testing.T) {
	f := newFake()
	f.bindings[bindKey(uid(1), "chatA")] = uid(7)
	s := newTestSession(f)
	_, err := s.StartSession(context.Background(), StartSessionInput{
		EnsureSessionInput:     EnsureSessionInput{WorkspaceID: uid(2), AgentID: uid(3), InstallationID: uid(1), Sender: uid(7), BindingKey: "chatA", ChatType: channel.ChatTypeP2P},
		HistoryBoundaryPending: true,
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if f.lastRetire.HistoryEndMessageID.Valid {
		t.Fatalf("retired end=%q, want open until first real inbound", f.lastRetire.HistoryEndMessageID.String)
	}
}

func TestAppendUserMessageRejectsRetiredGeneration(t *testing.T) {
	f := newFake()
	f.routeRetired = true
	s := newTestSession(f)
	_, err := s.AppendUserMessage(context.Background(), AppendInput{SessionID: uid(1), Body: "stale"})
	if !errors.Is(err, ErrRouteChanged) {
		t.Fatalf("error=%v, want ErrRouteChanged", err)
	}
	if len(f.messages) != 0 {
		t.Fatalf("retired generation received %d messages", len(f.messages))
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
	if res.InitialTitle != "hello there" {
		t.Errorf("initial title = %q, want first effective channel message", res.InitialTitle)
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

func TestAppendUserMessage_ChannelCommandDoesNotReplaceContextInitiator(t *testing.T) {
	f := newFake()
	f.contextInitiator = uid(7)
	s := newTestSession(f)
	if _, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Sender: uid(8), Body: "/issue Fix bug", MessageID: "m1",
	}); err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if f.contextInitiator != uid(7) {
		t.Fatalf("context initiator = %v, want prior agent-input sender %v", f.contextInitiator, uid(7))
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

func TestAppendUserMessageReportsOnlyImplicitFirstTurnAsNewlyVisible(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		explicitlyCreated    bool
		hadPublicUser        bool
		initialTitle         string
		wantBecameVisible    bool
		wantInitializedTitle string
	}{
		{name: "implicit first turn replaces legacy title", initialTitle: "Legacy channel title", wantBecameVisible: true, wantInitializedTitle: "hello"},
		{name: "explicit empty chat first turn", explicitlyCreated: true, wantInitializedTitle: "hello"},
		{name: "implicit chat later turn", hadPublicUser: true, initialTitle: "first title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			f.explicitlyCreated = tc.explicitlyCreated
			f.hasPublicUserMessage = tc.hadPublicUser
			f.sessionTitle = tc.initialTitle
			res, err := newTestSession(f).AppendUserMessage(context.Background(), AppendInput{
				SessionID: uid(1), Body: "hello", CommandText: "hello", MessageID: "m1",
			})
			if err != nil {
				t.Fatalf("AppendUserMessage: %v", err)
			}
			if res.BecameVisible != tc.wantBecameVisible {
				t.Fatalf("BecameVisible = %t, want %t", res.BecameVisible, tc.wantBecameVisible)
			}
			if f.initializedTitle != tc.wantInitializedTitle {
				t.Fatalf("initialized title = %q, want %q", f.initializedTitle, tc.wantInitializedTitle)
			}
		})
	}
}

func TestAppendUserMessageClearsLegacyTitleForImplicitMediaFirstTurn(t *testing.T) {
	f := newFake()
	f.sessionTitle = "Legacy channel title"
	res, err := newTestSession(f).AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Body: "[Image]", MessageID: "m1", MediaPendingSeconds: 10,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if !res.BecameVisible {
		t.Fatal("media first turn did not make the implicit Chat visible")
	}
	if f.sessionTitle != "" || res.InitialTitle != "" {
		t.Fatalf("media first turn title = stored %q result %q, want empty until attachment bind", f.sessionTitle, res.InitialTitle)
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

func TestBindMediaRefs_InitializesPlaceholderOnlyFirstMessageFromFilename(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	res, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Sender: uid(7), Body: "[Image]", MessageID: "om_image",
		MediaPendingSeconds: 45,
	})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if res.InitialTitle != "" {
		t.Fatalf("placeholder-only append initialized title %q before filename resolution", res.InitialTitle)
	}
	err = s.BindMediaRefs(context.Background(), BindMediaInput{
		MessageID: res.MessageID, SessionID: uid(1), WorkspaceID: uid(9), Sender: uid(7), Body: "[Image]",
		MediaRefs: []channel.MediaRef{{
			Type: channel.MsgTypeImage, StorageKey: "image-key", StorageURL: "https://cdn.example.test/image-key",
			Filename: "incident screenshot.png", MimeType: "image/png",
		}},
	})
	if err != nil {
		t.Fatalf("BindMediaRefs: %v", err)
	}
	if f.initializedMediaTitle != "incident screenshot.png" {
		t.Fatalf("media title = %q, want attachment filename", f.initializedMediaTitle)
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
	if f.contextRevision != 2 || !f.lastCreate.ChannelContextRevision.Valid || f.lastCreate.ChannelContextRevision.Int64 != 2 {
		t.Fatalf("fresh message context = generation %d / row %+v, want 2", f.contextRevision, f.lastCreate.ChannelContextRevision)
	}
}

func TestMarkPendingFresh_BareCommandPersistsIntent(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	if err := s.MarkPendingFresh(context.Background(), uid(1), "m1"); err != nil {
		t.Fatalf("MarkPendingFresh: %v", err)
	}
	if !f.pendingFresh {
		t.Fatal("bare fresh command did not persist pending intent")
	}
	if f.contextRevision != 2 || !f.boundaryPending {
		t.Fatalf("bare fresh context = generation %d pending=%t, want 2/true", f.contextRevision, f.boundaryPending)
	}
}

func TestAppendUserMessage_ResolvesBareFreshBoundaryOnNextMessage(t *testing.T) {
	f := newFake()
	s := newTestSession(f)
	if err := s.MarkPendingFresh(context.Background(), uid(1), "command-ts"); err != nil {
		t.Fatalf("MarkPendingFresh: %v", err)
	}
	res, err := s.AppendUserMessage(context.Background(), AppendInput{SessionID: uid(1), Body: "next", MessageID: "next-ts"})
	if err != nil {
		t.Fatalf("AppendUserMessage: %v", err)
	}
	if f.boundaryPending || res.ContextRevision != 2 || f.lastCreate.ChannelContextRevision.Int64 != 2 {
		t.Fatalf("resolved context: pending=%t result=%d row=%d", f.boundaryPending, res.ContextRevision, f.lastCreate.ChannelContextRevision.Int64)
	}
}

func TestAppendUserMessage_ContextGenerationFailuresDoNotWriteMessage(t *testing.T) {
	testErr := errors.New("context generation failed")
	tests := []struct {
		name  string
		setup func(*fakeSessionQueries)
		input AppendInput
	}{
		{name: "lock", setup: func(f *fakeSessionQueries) { f.lockContextErr = testErr }},
		{name: "advance", setup: func(f *fakeSessionQueries) { f.advanceContextErr = testErr }, input: AppendInput{ForceFresh: true}},
		{name: "resolve", setup: func(f *fakeSessionQueries) {
			f.boundaryPending = true
			f.resolveContextErr = testErr
		}, input: AppendInput{MessageID: "boundary"}},
		{name: "snapshot initiator", setup: func(f *fakeSessionQueries) { f.setInitiatorErr = testErr }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()
			tt.setup(f)
			input := tt.input
			input.SessionID = uid(1)
			input.Body = "must not persist"
			_, err := newTestSession(f).AppendUserMessage(context.Background(), input)
			if !errors.Is(err, testErr) {
				t.Fatalf("AppendUserMessage error = %v, want injected failure", err)
			}
			if len(f.messages) != 0 || f.touched != 0 || f.replyTargets != 0 {
				t.Fatalf("failed append wrote message/touch/reply = %d/%d/%d", len(f.messages), f.touched, f.replyTargets)
			}
		})
	}
}

func TestAppendUserMessage_PendingContextLookupFailureRollsBack(t *testing.T) {
	testErr := errors.New("pending context lookup failed")
	f := newFake()
	f.pendingContextsErr = testErr
	tx := &fakeTx{}
	s := newChatSessionWith(f, fakeTxStarter{tx: tx}, channel.TypeFeishu, SessionTitles{})

	_, err := s.AppendUserMessage(context.Background(), AppendInput{
		SessionID: uid(1), Body: "must roll back", MessageID: "m1",
	})
	if !errors.Is(err, testErr) {
		t.Fatalf("AppendUserMessage error = %v, want injected failure", err)
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("transaction committed/rolled back = %t/%t, want false/true", tx.committed, tx.rolledBack)
	}
	if f.touched != 0 || f.replyTargets != 0 {
		t.Fatalf("failed append touched session/reply target = %d/%d, want 0/0", f.touched, f.replyTargets)
	}
}

func TestMarkPendingFresh_ContextGenerationFailuresRollBack(t *testing.T) {
	testErr := errors.New("context generation failed")
	tests := []struct {
		name  string
		setup func(*fakeSessionQueries)
	}{
		{name: "lock", setup: func(f *fakeSessionQueries) { f.lockContextErr = testErr }},
		{name: "advance", setup: func(f *fakeSessionQueries) { f.advanceContextErr = testErr }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()
			tt.setup(f)
			tx := &fakeTx{}
			s := newChatSessionWith(f, fakeTxStarter{tx: tx}, channel.TypeFeishu, SessionTitles{})
			err := s.MarkPendingFresh(context.Background(), uid(1), "command")
			if !errors.Is(err, testErr) {
				t.Fatalf("MarkPendingFresh error = %v, want injected failure", err)
			}
			if tx.committed || !tx.rolledBack {
				t.Fatalf("transaction committed/rolledBack = %t/%t, want false/true", tx.committed, tx.rolledBack)
			}
		})
	}
}

func TestMarkPendingFresh_CommitFailureRollsBack(t *testing.T) {
	commitErr := errors.New("commit failed")
	tx := &fakeTx{commitErr: commitErr}
	s := newChatSessionWith(newFake(), fakeTxStarter{tx: tx}, channel.TypeFeishu, SessionTitles{})
	err := s.MarkPendingFresh(context.Background(), uid(1), "command")
	if !errors.Is(err, commitErr) {
		t.Fatalf("MarkPendingFresh error = %v, want commit failure", err)
	}
	if !tx.committed || !tx.rolledBack {
		t.Fatalf("transaction committed/rolledBack = %t/%t, want true/true", tx.committed, tx.rolledBack)
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
