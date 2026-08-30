package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channelmedia"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// This file is the SHARED, channel-agnostic chat-session service every IM
// adapter reuses (MUL-3516). It was lifted out of the Feishu-specific
// lark.chatSessionService so that adding an IM never re-implements the
// session/append/`/issue` machinery — the platform adapter contributes only a
// channel_type, its session titles, and (because enrichment is
// platform-specific) the command-parse source. The logic — find-or-create
// session + binding, append message + touch + reply-target + in-tx dedup mark,
// `/issue` parse — is identical across platforms and carries the channel_type
// discriminator through the generalized channel_* tables.

const pgSQLStateUniqueViolation = "23505"

// TxStarter abstracts transaction creation. Satisfied by *pgxpool.Pool. Kept
// local to the engine so the integration layer never back-references
// internal/service.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// SessionQueries is the narrow slice of the generated queries the ChatSession
// service needs. *db.Queries satisfies it through the dbSessionQueries adapter
// (whose WithTx returns the interface type); tests supply an in-memory fake.
type SessionQueries interface {
	WithTx(tx pgx.Tx) SessionQueries
	GetChannelChatSessionBinding(ctx context.Context, arg db.GetChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error)
	LockWorkspaceForChatSessionCreate(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
	CreateChatSession(ctx context.Context, arg db.CreateChatSessionParams) (db.ChatSession, error)
	CreateChannelChatSessionBinding(ctx context.Context, arg db.CreateChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error)
	CreateChannelChatSessionBindingGeneration(ctx context.Context, arg db.CreateChannelChatSessionBindingGenerationParams) (db.ChannelChatSessionBinding, error)
	LockCurrentChannelChatSessionBinding(ctx context.Context, arg db.LockCurrentChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error)
	LockCurrentChannelChatSessionBindingBySession(ctx context.Context, chatSessionID pgtype.UUID) (db.ChannelChatSessionBinding, error)
	RetireChannelChatSessionBinding(ctx context.Context, arg db.RetireChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error)
	LockChatSessionForAppend(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
	ChatSessionHasPublicUserMessage(ctx context.Context, id pgtype.UUID) (bool, error)
	MarkChatSessionExplicitlyCreated(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
	InitializeChatSessionTitle(ctx context.Context, arg db.InitializeChatSessionTitleParams) (db.ChatSession, error)
	ReplaceImplicitChatSessionTitle(ctx context.Context, arg db.ReplaceImplicitChatSessionTitleParams) (db.ChatSession, error)
	InitializeChatSessionMediaTitle(ctx context.Context, arg db.InitializeChatSessionMediaTitleParams) (db.ChatSession, error)
	CreateChatMessage(ctx context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error)
	ListUnownedChannelChatContextRevisions(ctx context.Context, chatSessionID pgtype.UUID) ([]PendingContext, error)
	ClearChatMessageChannelMediaPending(ctx context.Context, arg db.ClearChatMessageChannelMediaPendingParams) error
	LockIssueForChannelMediaBind(ctx context.Context, arg db.LockIssueForChannelMediaBindParams) (pgtype.UUID, error)
	UpdateChatMessageContentForChannelMedia(ctx context.Context, arg db.UpdateChatMessageContentForChannelMediaParams) (int64, error)
	MaterializeIssueChannelMediaMarkdown(ctx context.Context, arg db.MaterializeIssueChannelMediaMarkdownParams) (db.Issue, error)
	CreateAttachment(ctx context.Context, arg db.CreateAttachmentParams) (db.Attachment, error)
	LinkAttachmentsToChatMessage(ctx context.Context, arg db.LinkAttachmentsToChatMessageParams) ([]pgtype.UUID, error)
	ClaimChannelMediaPendingObjectsForBind(ctx context.Context, arg db.ClaimChannelMediaPendingObjectsForBindParams) ([]string, error)
	TouchChatSession(ctx context.Context, id pgtype.UUID) error
	LockChannelChatSessionBindingForContext(ctx context.Context, chatSessionID pgtype.UUID) (db.ChannelChatSessionBinding, error)
	LockChannelChatContextGenerationByRevision(ctx context.Context, arg db.LockChannelChatContextGenerationByRevisionParams) (db.ChannelChatContextGeneration, error)
	AdvanceChannelChatContextGeneration(ctx context.Context, arg db.AdvanceChannelChatContextGenerationParams) (db.AdvanceChannelChatContextGenerationRow, error)
	ResolveChannelChatContextHistoryStart(ctx context.Context, arg db.ResolveChannelChatContextHistoryStartParams) error
	SetChannelChatContextInitiator(ctx context.Context, arg db.SetChannelChatContextInitiatorParams) (pgtype.UUID, error)
	UpdateChannelChatSessionBindingReplyTarget(ctx context.Context, arg db.UpdateChannelChatSessionBindingReplyTargetParams) error
	MarkChannelInboundDedupProcessed(ctx context.Context, arg db.MarkChannelInboundDedupProcessedParams) (int64, error)
}

// dbSessionQueries adapts *db.Queries to SessionQueries — the only purpose is
// to give WithTx an interface return type so the transactional path stays
// behind SessionQueries.
type dbSessionQueries struct{ q *db.Queries }

func (a dbSessionQueries) WithTx(tx pgx.Tx) SessionQueries {
	return dbSessionQueries{q: a.q.WithTx(tx)}
}
func (a dbSessionQueries) GetChannelChatSessionBinding(ctx context.Context, arg db.GetChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	return a.q.GetChannelChatSessionBinding(ctx, arg)
}
func (a dbSessionQueries) LockWorkspaceForChatSessionCreate(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	return a.q.LockWorkspaceForChatSessionCreate(ctx, id)
}
func (a dbSessionQueries) CreateChatSession(ctx context.Context, arg db.CreateChatSessionParams) (db.ChatSession, error) {
	return a.q.CreateChatSession(ctx, arg)
}
func (a dbSessionQueries) CreateChannelChatSessionBinding(ctx context.Context, arg db.CreateChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	row, err := a.q.CreateChannelChatSessionBinding(ctx, arg)
	return db.ChannelChatSessionBinding{
		ID: row.ID, ChatSessionID: row.ChatSessionID, InstallationID: row.InstallationID,
		ChannelType: row.ChannelType, ChannelChatID: row.ChannelChatID, ChatType: row.ChatType,
		LastMessageID: row.LastMessageID, LastThreadID: row.LastThreadID, Config: row.Config,
		CreatedAt: row.CreatedAt, PendingFresh: row.PendingFresh, ContextRevision: row.ContextRevision,
	}, err
}
func (a dbSessionQueries) CreateChannelChatSessionBindingGeneration(ctx context.Context, arg db.CreateChannelChatSessionBindingGenerationParams) (db.ChannelChatSessionBinding, error) {
	row, err := a.q.CreateChannelChatSessionBindingGeneration(ctx, arg)
	return db.ChannelChatSessionBinding{
		ID: row.ID, ChatSessionID: row.ChatSessionID, InstallationID: row.InstallationID,
		ChannelType: row.ChannelType, ChannelChatID: row.ChannelChatID, ChatType: row.ChatType,
		LastMessageID: row.LastMessageID, LastThreadID: row.LastThreadID, Config: row.Config,
		CreatedAt: row.CreatedAt, PendingFresh: row.PendingFresh, ContextRevision: row.ContextRevision,
		RouteRevision: row.RouteRevision, RetiredAt: row.RetiredAt,
		HistoryStartMessageID: row.HistoryStartMessageID, HistoryEndMessageID: row.HistoryEndMessageID,
		HistoryBoundaryPending: row.HistoryBoundaryPending,
	}, err
}
func (a dbSessionQueries) LockCurrentChannelChatSessionBinding(ctx context.Context, arg db.LockCurrentChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	return a.q.LockCurrentChannelChatSessionBinding(ctx, arg)
}
func (a dbSessionQueries) LockCurrentChannelChatSessionBindingBySession(ctx context.Context, chatSessionID pgtype.UUID) (db.ChannelChatSessionBinding, error) {
	return a.q.LockCurrentChannelChatSessionBindingBySession(ctx, chatSessionID)
}
func (a dbSessionQueries) RetireChannelChatSessionBinding(ctx context.Context, arg db.RetireChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	return a.q.RetireChannelChatSessionBinding(ctx, arg)
}
func (a dbSessionQueries) LockChatSessionForAppend(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	return a.q.LockChatSessionForAppend(ctx, id)
}
func (a dbSessionQueries) GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error) {
	return a.q.GetChatSession(ctx, id)
}
func (a dbSessionQueries) ChatSessionHasPublicUserMessage(ctx context.Context, id pgtype.UUID) (bool, error) {
	return a.q.ChatSessionHasPublicUserMessage(ctx, id)
}
func (a dbSessionQueries) MarkChatSessionExplicitlyCreated(ctx context.Context, id pgtype.UUID) (db.ChatSession, error) {
	return a.q.MarkChatSessionExplicitlyCreated(ctx, id)
}
func (a dbSessionQueries) InitializeChatSessionTitle(ctx context.Context, arg db.InitializeChatSessionTitleParams) (db.ChatSession, error) {
	return a.q.InitializeChatSessionTitle(ctx, arg)
}
func (a dbSessionQueries) ReplaceImplicitChatSessionTitle(ctx context.Context, arg db.ReplaceImplicitChatSessionTitleParams) (db.ChatSession, error) {
	return a.q.ReplaceImplicitChatSessionTitle(ctx, arg)
}
func (a dbSessionQueries) InitializeChatSessionMediaTitle(ctx context.Context, arg db.InitializeChatSessionMediaTitleParams) (db.ChatSession, error) {
	return a.q.InitializeChatSessionMediaTitle(ctx, arg)
}
func (a dbSessionQueries) CreateChatMessage(ctx context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error) {
	return a.q.CreateChatMessage(ctx, arg)
}
func (a dbSessionQueries) ListUnownedChannelChatContextRevisions(ctx context.Context, chatSessionID pgtype.UUID) ([]PendingContext, error) {
	rows, err := a.q.ListUnownedChannelChatContextRevisions(ctx, chatSessionID)
	if err != nil {
		return nil, err
	}
	contexts := make([]PendingContext, 0, len(rows))
	for _, row := range rows {
		contexts = append(contexts, PendingContext{
			Revision:        row.ContextRevision,
			InitiatorUserID: row.InitiatorUserID,
		})
	}
	return contexts, nil
}
func (a dbSessionQueries) ClearChatMessageChannelMediaPending(ctx context.Context, arg db.ClearChatMessageChannelMediaPendingParams) error {
	return a.q.ClearChatMessageChannelMediaPending(ctx, arg)
}
func (a dbSessionQueries) LockIssueForChannelMediaBind(ctx context.Context, arg db.LockIssueForChannelMediaBindParams) (pgtype.UUID, error) {
	return a.q.LockIssueForChannelMediaBind(ctx, arg)
}
func (a dbSessionQueries) UpdateChatMessageContentForChannelMedia(ctx context.Context, arg db.UpdateChatMessageContentForChannelMediaParams) (int64, error) {
	return a.q.UpdateChatMessageContentForChannelMedia(ctx, arg)
}
func (a dbSessionQueries) MaterializeIssueChannelMediaMarkdown(ctx context.Context, arg db.MaterializeIssueChannelMediaMarkdownParams) (db.Issue, error) {
	return a.q.MaterializeIssueChannelMediaMarkdown(ctx, arg)
}
func (a dbSessionQueries) CreateAttachment(ctx context.Context, arg db.CreateAttachmentParams) (db.Attachment, error) {
	created, err := a.q.CreateAttachment(ctx, arg)
	return created.Attachment(), err
}
func (a dbSessionQueries) LinkAttachmentsToChatMessage(ctx context.Context, arg db.LinkAttachmentsToChatMessageParams) ([]pgtype.UUID, error) {
	return a.q.LinkAttachmentsToChatMessage(ctx, arg)
}
func (a dbSessionQueries) ClaimChannelMediaPendingObjectsForBind(ctx context.Context, arg db.ClaimChannelMediaPendingObjectsForBindParams) ([]string, error) {
	return a.q.ClaimChannelMediaPendingObjectsForBind(ctx, arg)
}
func (a dbSessionQueries) TouchChatSession(ctx context.Context, id pgtype.UUID) error {
	return a.q.TouchChatSession(ctx, id)
}
func (a dbSessionQueries) LockChannelChatSessionBindingForContext(ctx context.Context, chatSessionID pgtype.UUID) (db.ChannelChatSessionBinding, error) {
	return a.q.LockChannelChatSessionBindingForContext(ctx, chatSessionID)
}
func (a dbSessionQueries) LockChannelChatContextGenerationByRevision(ctx context.Context, arg db.LockChannelChatContextGenerationByRevisionParams) (db.ChannelChatContextGeneration, error) {
	return a.q.LockChannelChatContextGenerationByRevision(ctx, arg)
}
func (a dbSessionQueries) AdvanceChannelChatContextGeneration(ctx context.Context, arg db.AdvanceChannelChatContextGenerationParams) (db.AdvanceChannelChatContextGenerationRow, error) {
	return a.q.AdvanceChannelChatContextGeneration(ctx, arg)
}
func (a dbSessionQueries) ResolveChannelChatContextHistoryStart(ctx context.Context, arg db.ResolveChannelChatContextHistoryStartParams) error {
	return a.q.ResolveChannelChatContextHistoryStart(ctx, arg)
}
func (a dbSessionQueries) SetChannelChatContextInitiator(ctx context.Context, arg db.SetChannelChatContextInitiatorParams) (pgtype.UUID, error) {
	return a.q.SetChannelChatContextInitiator(ctx, arg)
}
func (a dbSessionQueries) UpdateChannelChatSessionBindingReplyTarget(ctx context.Context, arg db.UpdateChannelChatSessionBindingReplyTargetParams) error {
	return a.q.UpdateChannelChatSessionBindingReplyTarget(ctx, arg)
}
func (a dbSessionQueries) MarkChannelInboundDedupProcessed(ctx context.Context, arg db.MarkChannelInboundDedupProcessedParams) (int64, error) {
	return a.q.MarkChannelInboundDedupProcessed(ctx, arg)
}

// SessionTitles is retained in the constructor surface for adapter
// compatibility. New implicit channel Chats deliberately start with an empty
// persisted title: their first effective user message initializes the shared
// deterministic title, and clients already render the brief empty interval as
// a localized "New chat" fallback.
type SessionTitles struct {
	Group    string
	Direct   string
	Fallback string
}

func (t SessionTitles) forType(ct channel.ChatType) string {
	switch ct {
	case channel.ChatTypeGroup:
		return t.Group
	case channel.ChatTypeP2P:
		return t.Direct
	default:
		return t.Fallback
	}
}

// ChatSession is the shared chat-session service. One instance is built per
// channel_type (so the binding rows carry the right discriminator); the logic
// is otherwise platform-neutral.
type ChatSession struct {
	q           SessionQueries
	tx          TxStarter
	channelType channel.Type
	titles      SessionTitles
}

// NewChatSession builds the shared service over the generated queries. tx is
// required: AppendUserMessage runs the dedup Mark inside the chat_message
// transaction so the durable write and the Mark commit (or roll back) together.
func NewChatSession(q *db.Queries, tx TxStarter, channelType channel.Type, titles SessionTitles) *ChatSession {
	return &ChatSession{q: dbSessionQueries{q: q}, tx: tx, channelType: channelType, titles: titles}
}

// newChatSessionWith is the test seam: it accepts a SessionQueries directly so
// an in-memory fake can stand in for *db.Queries.
func newChatSessionWith(q SessionQueries, tx TxStarter, channelType channel.Type, titles SessionTitles) *ChatSession {
	return &ChatSession{q: q, tx: tx, channelType: channelType, titles: titles}
}

// EnsureSessionInput is the channel-agnostic input for EnsureSession.
//
// BindingKey is the SESSION-ISOLATION key (stored as channel_chat_id; one
// chat_session per (installation_id, BindingKey)). It is intentionally NOT the
// same thing as "the chat to reply into": the adapter composes it so that
// distinct conversations get distinct sessions — Feishu passes the chat id;
// Slack passes the channel id for a DM, and the channel id PLUS the thread root
// for a channel/thread, so two @bot threads in one Slack channel do not collapse
// into one transcript (the Hermes model: IM-independent, Slack groups isolated
// by thread root). A raw platform chat id must never be passed straight through
// as the key for a threaded platform.
//
// BindingConfig is opaque platform routing the key alone cannot carry — e.g.
// Slack's real channel_id when BindingKey is a composite — persisted on the
// binding's config for the outbound path to read back. nil means "{}".
//
// Sender is the already-resolved Multica user (the session creator: the sole
// human for p2p, the installer for group chats — the caller decides which).
type EnsureSessionInput struct {
	WorkspaceID    pgtype.UUID
	AgentID        pgtype.UUID
	InstallationID pgtype.UUID
	Sender         pgtype.UUID
	BindingKey     string
	BindingConfig  []byte
	ChatType       channel.ChatType
}

// EnsureSession returns the chat_session.id bound to (installation, BindingKey),
// creating it (with its channel_chat_session_binding) on first contact. The
// race between two concurrent first messages is resolved by the
// UNIQUE (installation_id, channel_chat_id) constraint: the loser re-reads the
// winner's row.
func (s *ChatSession) EnsureSession(ctx context.Context, in EnsureSessionInput) (pgtype.UUID, error) {
	lookup := db.GetChannelChatSessionBindingParams{InstallationID: in.InstallationID, ChannelChatID: in.BindingKey}

	existing, err := s.q.GetChannelChatSessionBinding(ctx, lookup)
	if err == nil {
		return existing.ChatSessionID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, fmt.Errorf("lookup chat session binding: %w", err)
	}

	id, err := s.createSessionAndBinding(ctx, in)
	if err == nil {
		return id, nil
	}
	if isUniqueViolation(err) {
		existing, lookupErr := s.q.GetChannelChatSessionBinding(ctx, lookup)
		if lookupErr == nil {
			return existing.ChatSessionID, nil
		}
		return pgtype.UUID{}, fmt.Errorf("race re-read after unique violation: %w", lookupErr)
	}
	return pgtype.UUID{}, err
}

func (s *ChatSession) createSessionAndBinding(ctx context.Context, in EnsureSessionInput) (pgtype.UUID, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	// FOR KEY SHARE on the workspace row before creating the session — the creator
	// half of the #5219 delete/create protocol, so a channel session cannot be
	// created into a workspace mid-delete (see LockWorkspaceForChatSessionCreate).
	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, in.WorkspaceID); err != nil {
		return pgtype.UUID{}, fmt.Errorf("lock workspace for chat session create: %w", err)
	}

	session, err := qtx.CreateChatSession(ctx, db.CreateChatSessionParams{
		ID:          dbid.NewV7(),
		WorkspaceID: in.WorkspaceID,
		AgentID:     in.AgentID,
		CreatorID:   in.Sender,
		Title:       "",
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("create chat session: %w", err)
	}
	bindingConfig := in.BindingConfig
	if len(bindingConfig) == 0 {
		bindingConfig = []byte("{}")
	}
	if _, err := qtx.CreateChannelChatSessionBinding(ctx, db.CreateChannelChatSessionBindingParams{
		ChatSessionID:  session.ID,
		InstallationID: in.InstallationID,
		ChannelType:    string(s.channelType),
		ChannelChatID:  in.BindingKey,
		ChatType:       string(in.ChatType),
		Config:         bindingConfig,
	}); err != nil {
		return pgtype.UUID{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, fmt.Errorf("commit: %w", err)
	}
	return session.ID, nil
}

// AppendInput is the channel-agnostic input for AppendUserMessage. Body is the
// full stored text (including any platform enrichment); CommandText is the
// user's OWN typed text used for `/issue` parsing (empty falls back to Body) —
// the adapter supplies it because enrichment is platform-specific. ClaimToken
// is the dedup owner-fence: when valid, the Mark runs inside this method's tx.
//
// MessageID and ThreadID are the REAL platform message id and thread id of this
// trigger — the outbound reply target recorded on the binding (last_message_id /
// last_thread_id), NOT the session BindingKey. Because each isolated session has
// its own binding row, recording the real thread here per session does not clash
// across sibling threads.
type AppendInput struct {
	SessionID           pgtype.UUID
	Sender              pgtype.UUID
	InstallationID      pgtype.UUID
	Body                string
	CommandText         string
	MessageID           string
	DedupMessageID      string
	ThreadID            string
	ClaimToken          pgtype.UUID
	MediaPendingSeconds float64
	ForceFresh          bool
	// BeforeCommit adds work that must be atomic with this message and its
	// context-generation change. Native slash commands use it to snapshot and
	// enqueue the task before the message becomes visible.
	BeforeCommit func(context.Context, pgx.Tx, db.ChatSession, int64, pgtype.UUID, int64) error
}

// StartSessionInput is the shared, transactional implementation of /new.
// The adapter supplies only its route key/config and optional platform fence.
type StartSessionInput struct {
	EnsureSessionInput
	// Initiator is the authenticated sender of the /new command. Sender in the
	// embedded EnsureSessionInput remains the owner of the newly created Chat.
	Initiator              pgtype.UUID
	Body                   string
	MessageID              string
	DedupMessageID         string
	ThreadID               string
	ClaimToken             pgtype.UUID
	MediaPendingSeconds    float64
	PersistMessage         bool
	HistoryBoundaryPending bool
	BeforeWrite            func(context.Context, pgx.Tx) error
	// BeforeCommit can add work that must be atomic with the route rotation and
	// first message. The newly created session is visible through tx, but none of
	// these writes are externally observable until StartSession commits.
	BeforeCommit func(context.Context, pgx.Tx, db.ChatSession) error
}

// StartSession atomically retires the current route generation, creates an
// explicitly visible Chat, installs the next generation, and optionally writes
// the command body as its first ordinary user message.
func (s *ChatSession) StartSession(ctx context.Context, in StartSessionInput) (StartSessionResult, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return StartSessionResult{}, fmt.Errorf("begin start chat tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, in.WorkspaceID); err != nil {
		return StartSessionResult{}, fmt.Errorf("lock workspace for start chat: %w", err)
	}

	lookup := db.GetChannelChatSessionBindingParams{InstallationID: in.InstallationID, ChannelChatID: in.BindingKey}
	current, currentErr := qtx.GetChannelChatSessionBinding(ctx, lookup)
	if currentErr != nil && !errors.Is(currentErr, pgx.ErrNoRows) {
		return StartSessionResult{}, fmt.Errorf("load current chat route: %w", currentErr)
	}
	nextRevision := int64(1)
	if currentErr == nil {
		// Preserve the global chat_session -> binding lock order used by append.
		if _, err := qtx.LockChatSessionForAppend(ctx, current.ChatSessionID); err != nil {
			return StartSessionResult{}, fmt.Errorf("lock prior chat session: %w", err)
		}
		locked, err := qtx.LockCurrentChannelChatSessionBinding(ctx, db.LockCurrentChannelChatSessionBindingParams{
			InstallationID: in.InstallationID,
			ChannelChatID:  in.BindingKey,
		})
		if err != nil || locked.ID != current.ID {
			return StartSessionResult{}, ErrRouteChanged
		}
		if in.BeforeWrite != nil {
			if err := in.BeforeWrite(ctx, tx); err != nil {
				return StartSessionResult{}, err
			}
		}
		historyEnd := locked.LastMessageID
		if in.HistoryBoundaryPending && in.MessageID == "" {
			// A native command has no public platform cursor. Keep the old end
			// open until the next real inbound atomically closes it and opens the
			// pending generation at the same message id.
			historyEnd = pgtype.Text{}
		} else if in.MessageID != "" {
			historyEnd = textOrNull(in.MessageID)
		}
		if _, err := qtx.RetireChannelChatSessionBinding(ctx, db.RetireChannelChatSessionBindingParams{
			ID: locked.ID, HistoryEndMessageID: historyEnd,
		}); err != nil {
			return StartSessionResult{}, fmt.Errorf("retire current chat route: %w", err)
		}
		nextRevision = locked.RouteRevision + 1
	} else if in.BeforeWrite != nil {
		if err := in.BeforeWrite(ctx, tx); err != nil {
			return StartSessionResult{}, err
		}
	}

	title := ""
	if in.PersistMessage {
		title = deriveFirstMessageTitle(in.Body, in.MediaPendingSeconds > 0)
	}
	session, err := qtx.CreateChatSession(ctx, db.CreateChatSessionParams{
		ID: dbid.NewV7(), WorkspaceID: in.WorkspaceID, AgentID: in.AgentID,
		CreatorID: in.Sender, Title: title,
	})
	if err != nil {
		return StartSessionResult{}, fmt.Errorf("create started chat session: %w", err)
	}
	if _, err := qtx.MarkChatSessionExplicitlyCreated(ctx, session.ID); err != nil {
		return StartSessionResult{}, fmt.Errorf("mark started chat explicit: %w", err)
	}
	config := in.BindingConfig
	if len(config) == 0 {
		config = []byte("{}")
	}
	startMessageID := textOrNull(in.MessageID)
	binding, err := qtx.CreateChannelChatSessionBindingGeneration(ctx, db.CreateChannelChatSessionBindingGenerationParams{
		ChatSessionID: session.ID, InstallationID: in.InstallationID,
		ChannelType: string(s.channelType), ChannelChatID: in.BindingKey,
		ChatType: string(in.ChatType), Config: config, RouteRevision: nextRevision,
		HistoryStartMessageID:  startMessageID,
		HistoryBoundaryPending: in.HistoryBoundaryPending && in.MessageID == "",
	})
	if err != nil {
		if isUniqueViolation(err) {
			return StartSessionResult{}, ErrRouteChanged
		}
		return StartSessionResult{}, fmt.Errorf("create next chat route: %w", err)
	}
	result := StartSessionResult{SessionID: session.ID, BindingID: binding.ID, RouteRevision: binding.RouteRevision}
	result.Append.InitialTitle = title
	result.Append.BindingID = binding.ID
	result.Append.RouteRevision = binding.RouteRevision
	result.Append.ContextRevision = 1
	if in.PersistMessage {
		if _, err := qtx.SetChannelChatContextInitiator(ctx, db.SetChannelChatContextInitiatorParams{
			ChatSessionID: session.ID, Revision: 1, InitiatorUserID: in.Initiator,
		}); err != nil {
			return StartSessionResult{}, fmt.Errorf("snapshot started chat initiator: %w", err)
		}
		msg, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
			ID: dbid.NewV7(), ChatSessionID: session.ID, Role: "user", Content: in.Body,
			ChannelMediaPendingSecs: pgtype.Float8{Float64: in.MediaPendingSeconds, Valid: in.MediaPendingSeconds > 0},
			ChannelIngested:         pgtype.Bool{Bool: true, Valid: true},
			ChannelContextRevision:  pgtype.Int8{Int64: 1, Valid: true},
		})
		if err != nil {
			return StartSessionResult{}, fmt.Errorf("create first chat message: %w", err)
		}
		if err := qtx.TouchChatSession(ctx, session.ID); err != nil {
			return StartSessionResult{}, fmt.Errorf("touch started chat: %w", err)
		}
		result.Append.MessageID = msg.ID
		result.Append.PendingContexts = []PendingContext{{Revision: 1, InitiatorUserID: in.Initiator}}
	}
	if in.MessageID != "" {
		if err := qtx.UpdateChannelChatSessionBindingReplyTarget(ctx, db.UpdateChannelChatSessionBindingReplyTargetParams{
			ReplyChatSessionID: session.ID, LastMessageID: textOrNull(in.MessageID), LastThreadID: textOrNull(in.ThreadID),
		}); err != nil {
			return StartSessionResult{}, fmt.Errorf("set started chat reply target: %w", err)
		}
	}
	dedupMessageID := in.DedupMessageID
	if dedupMessageID == "" {
		dedupMessageID = in.MessageID
	}
	if in.ClaimToken.Valid && dedupMessageID != "" {
		rows, err := qtx.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
			InstallationID: in.InstallationID, MessageID: dedupMessageID, ClaimToken: in.ClaimToken,
		})
		if err != nil {
			return StartSessionResult{}, fmt.Errorf("mark start chat dedup: %w", err)
		}
		if rows == 0 {
			return StartSessionResult{}, ErrClaimLost
		}
		result.Append.DedupMarked = true
	}
	if in.BeforeCommit != nil {
		if err := in.BeforeCommit(ctx, tx, session); err != nil {
			return StartSessionResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return StartSessionResult{}, fmt.Errorf("commit start chat: %w", err)
	}
	startOutcome := "chat_started"
	if in.PersistMessage {
		startOutcome = "chat_started_with_message"
	}
	slog.Info("channel chat route started",
		"outcome", startOutcome,
		"channel_type", string(s.channelType),
		"source_chat_session_id", utilUUID(current.ChatSessionID),
		"new_chat_session_id", utilUUID(session.ID),
		"chat_route_generation", binding.RouteRevision,
	)
	return result, nil
}

func utilUUID(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}

// BindMediaInput links already-uploaded media to either an /issue target or a
// durable chat message in a short database-only transaction. A valid
// IssueDescriptionBase permits inline replacement only while the issue still
// has its exact creation-time description; otherwise issue media appends as a
// concurrency-safe fallback. Remote downloads/uploads happen before this call
// and outside the connector ACK path.
type BindMediaInput struct {
	MessageID            pgtype.UUID
	SessionID            pgtype.UUID
	WorkspaceID          pgtype.UUID
	Sender               pgtype.UUID
	IssueID              pgtype.UUID
	IssueDescriptionBase pgtype.Text
	IssueCommandText     string
	Body                 string
	MediaRefs            []channel.MediaRef
}

// channelCommandMessageKind marks a durable control-plane turn handled
// synchronously by Router. Public Chat projections omit it, and the task batch
// seal does too so the agent cannot execute the command again on a later turn.
const channelCommandMessageKind = "channel_command"

// AppendUserMessage writes the user message into the chat_session (touching it
// and recording the reply target), runs the in-tx dedup Mark when a claim token
// is supplied, and returns the durable message id plus the parsed `/issue`
// command when present. Returns ErrClaimLost when a concurrent reclaim rotated
// the dedup token mid-flight, in which case the whole transaction rolls back
// (no chat_message lands).
func (s *ChatSession) AppendUserMessage(ctx context.Context, in AppendInput) (AppendResult, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return AppendResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	commandSource := in.CommandText
	if commandSource == "" {
		commandSource = in.Body
	}
	cmd, _ := ParseIssueCommand(commandSource)
	// Context paths acquire chat_session before binding and generation. This
	// also keeps the later TouchChatSession update from introducing the reverse
	// binding -> chat_session edge against task enqueue.
	if _, err := qtx.LockChatSessionForAppend(ctx, in.SessionID); err != nil {
		return AppendResult{}, fmt.Errorf("lock chat session for append: %w", err)
	}
	binding, err := qtx.LockCurrentChannelChatSessionBindingBySession(ctx, in.SessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AppendResult{}, ErrRouteChanged
		}
		return AppendResult{}, fmt.Errorf("lock channel chat binding: %w", err)
	}
	// The binding lock serializes ordinary appends in route order. Determine
	// first-turn visibility and initialize its fallback title only after that
	// fence, so two concurrent inbound messages cannot both announce creation or
	// let the second message win the title race.
	currentSession, err := qtx.GetChatSession(ctx, in.SessionID)
	if err != nil {
		return AppendResult{}, fmt.Errorf("reload chat session for append: %w", err)
	}
	hadPublicUserMessage, err := qtx.ChatSessionHasPublicUserMessage(ctx, in.SessionID)
	if err != nil {
		return AppendResult{}, fmt.Errorf("check public chat history: %w", err)
	}
	becameVisible := cmd == nil && !hadPublicUserMessage && !currentSession.ExplicitlyCreatedAt.Valid
	initializedTitle := ""
	if cmd == nil {
		title := deriveFirstMessageTitle(in.Body, in.MediaPendingSeconds > 0)
		if becameVisible {
			if _, err := qtx.ReplaceImplicitChatSessionTitle(ctx, db.ReplaceImplicitChatSessionTitleParams{ID: in.SessionID, Title: title}); err != nil {
				return AppendResult{}, fmt.Errorf("replace implicit chat title: %w", err)
			}
			initializedTitle = title
		} else if title != "" {
			if _, err := qtx.InitializeChatSessionTitle(ctx, db.InitializeChatSessionTitleParams{ID: in.SessionID, Title: title}); err == nil {
				initializedTitle = title
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return AppendResult{}, fmt.Errorf("initialize chat title: %w", err)
			}
		}
	}
	contextState, err := qtx.LockChannelChatContextGenerationByRevision(ctx, db.LockChannelChatContextGenerationByRevisionParams{
		ChatSessionID: in.SessionID, Revision: binding.ContextRevision,
	})
	if err != nil {
		return AppendResult{}, fmt.Errorf("lock channel chat context: %w", err)
	}
	contextRevision := binding.ContextRevision
	if in.ForceFresh {
		advanced, err := qtx.AdvanceChannelChatContextGeneration(ctx, db.AdvanceChannelChatContextGenerationParams{
			ChatSessionID: in.SessionID, CurrentRevision: contextRevision,
			HistoryBoundaryMessageID: textOrNull(in.MessageID), HasMessageBody: in.MessageID != "",
		})
		if err != nil {
			return AppendResult{}, fmt.Errorf("advance channel chat context: %w", err)
		}
		contextRevision = advanced.Revision
	} else if contextState.HistoryBoundaryPending && in.MessageID != "" {
		if err := qtx.ResolveChannelChatContextHistoryStart(ctx, db.ResolveChannelChatContextHistoryStartParams{
			HistoryStartMessageID: textOrNull(in.MessageID), ChatSessionID: in.SessionID, Revision: contextRevision,
		}); err != nil {
			return AppendResult{}, fmt.Errorf("resolve channel chat history boundary: %w", err)
		}
	}
	// A channel command is excluded from agent input, so it must not replace
	// the principal used to recover earlier unowned agent-visible messages.
	if contextRevision > 0 && cmd == nil {
		if _, err := qtx.SetChannelChatContextInitiator(ctx, db.SetChannelChatContextInitiatorParams{
			ChatSessionID: in.SessionID, Revision: contextRevision, InitiatorUserID: in.Sender,
		}); err != nil {
			return AppendResult{}, fmt.Errorf("snapshot channel context initiator: %w", err)
		}
	}
	// channel_ingested is the immutable provenance the cancel path gates on:
	// it must be stamped in the same transaction as the message so no later
	// binding deletion (archive, installation rebind) can strip it.
	msg, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ID:                      dbid.NewV7(),
		ChatSessionID:           in.SessionID,
		Role:                    "user",
		Content:                 in.Body,
		MessageKind:             textOrNullIf(cmd != nil, channelCommandMessageKind),
		ChannelMediaPendingSecs: pgtype.Float8{Float64: in.MediaPendingSeconds, Valid: in.MediaPendingSeconds > 0},
		ChannelIngested:         pgtype.Bool{Bool: true, Valid: true},
		ChannelContextRevision:  pgtype.Int8{Int64: contextRevision, Valid: contextRevision > 0},
	})
	if err != nil {
		return AppendResult{}, fmt.Errorf("create chat message: %w", err)
	}
	pendingContexts, err := qtx.ListUnownedChannelChatContextRevisions(ctx, in.SessionID)
	if err != nil {
		return AppendResult{}, fmt.Errorf("list pending channel contexts: %w", err)
	}
	if err := qtx.TouchChatSession(ctx, in.SessionID); err != nil {
		return AppendResult{}, fmt.Errorf("touch chat session: %w", err)
	}

	// Record the latest trigger so the decoupled outbound patcher can thread
	// its reply back into the originating topic.
	if in.MessageID != "" {
		if err := qtx.UpdateChannelChatSessionBindingReplyTarget(ctx, db.UpdateChannelChatSessionBindingReplyTargetParams{
			ReplyChatSessionID: in.SessionID,
			LastMessageID:      textOrNull(in.MessageID),
			LastThreadID:       textOrNull(in.ThreadID),
		}); err != nil {
			return AppendResult{}, fmt.Errorf("update reply target: %w", err)
		}
	}

	markedInTx := false
	dedupMessageID := in.DedupMessageID
	if dedupMessageID == "" {
		dedupMessageID = in.MessageID
	}
	if in.ClaimToken.Valid && dedupMessageID != "" {
		rows, err := qtx.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
			InstallationID: in.InstallationID,
			MessageID:      dedupMessageID,
			ClaimToken:     in.ClaimToken,
		})
		if err != nil {
			return AppendResult{}, fmt.Errorf("mark dedup processed: %w", err)
		}
		if rows == 0 {
			// Another worker re-claimed the dedup row; roll back via the
			// deferred Rollback so no second chat_message lands.
			return AppendResult{}, ErrClaimLost
		}
		markedInTx = true
	}
	if in.BeforeCommit != nil {
		if err := in.BeforeCommit(ctx, tx, currentSession, contextRevision, binding.ID, binding.RouteRevision); err != nil {
			return AppendResult{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return AppendResult{}, fmt.Errorf("commit: %w", err)
	}
	return AppendResult{
		MessageID:       msg.ID,
		IssueCommand:    cmd,
		DedupMarked:     markedInTx,
		ContextRevision: contextRevision,
		PendingContexts: pendingContexts,
		InitialTitle:    initializedTitle,
		BecameVisible:   becameVisible,
		BindingID:       binding.ID,
		RouteRevision:   binding.RouteRevision,
	}, nil
}

// MarkPendingFresh persists a bare `/clear` command. Non-bare `/clear` messages
// mark the same flag inside AppendUserMessage's transaction instead.
func (s *ChatSession) MarkPendingFresh(ctx context.Context, sessionID pgtype.UUID, messageID string) error {
	return s.MarkPendingFreshWithDedup(ctx, sessionID, messageID, pgtype.UUID{}, "", pgtype.UUID{})
}

// MarkPendingFreshWithDedup atomically advances the context generation and
// finalizes an optional transport dedup claim. A native slash command has no
// public platform cursor, so messageID may be empty while dedupMessageID holds
// the durable Socket Mode envelope id.
func (s *ChatSession) MarkPendingFreshWithDedup(
	ctx context.Context,
	sessionID pgtype.UUID,
	messageID string,
	installationID pgtype.UUID,
	dedupMessageID string,
	claimToken pgtype.UUID,
) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin fresh context tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if _, err := qtx.LockChatSessionForAppend(ctx, sessionID); err != nil {
		return fmt.Errorf("lock chat session for fresh context: %w", err)
	}
	binding, err := qtx.LockCurrentChannelChatSessionBindingBySession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRouteChanged
		}
		return fmt.Errorf("lock channel chat binding: %w", err)
	}
	_, err = qtx.LockChannelChatContextGenerationByRevision(ctx, db.LockChannelChatContextGenerationByRevisionParams{
		ChatSessionID: sessionID, Revision: binding.ContextRevision,
	})
	if err != nil {
		return fmt.Errorf("lock channel chat context: %w", err)
	}
	if _, err := qtx.AdvanceChannelChatContextGeneration(ctx, db.AdvanceChannelChatContextGenerationParams{
		ChatSessionID: sessionID, CurrentRevision: binding.ContextRevision,
		HistoryBoundaryMessageID: textOrNull(messageID), HasMessageBody: false,
	}); err != nil {
		return fmt.Errorf("advance channel chat context: %w", err)
	}
	if claimToken.Valid && dedupMessageID != "" {
		rows, err := qtx.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
			InstallationID: installationID,
			MessageID:      dedupMessageID,
			ClaimToken:     claimToken,
		})
		if err != nil {
			return fmt.Errorf("mark fresh context dedup: %w", err)
		}
		if rows == 0 {
			return ErrClaimLost
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit fresh context: %w", err)
	}
	return nil
}

// BindMediaRefs creates attachment rows owned by IssueID when present, otherwise
// links them to the existing durable chat message. It also clears the message's
// media-pending marker. A failure rolls back the attachment rows, then clears
// the marker separately so the placeholder can be promoted immediately for
// graceful degradation.
// BindMediaRefs preserves the established error-only API used by adapter test
// doubles and direct callers.
func (s *ChatSession) BindMediaRefs(ctx context.Context, in BindMediaInput) error {
	_, err := s.BindMediaRefsWithResult(ctx, in)
	return err
}

// BindMediaRefsWithResult additionally reports first-media title initialization
// to production adapters so the Router can publish and refine that title.
func (s *ChatSession) BindMediaRefsWithResult(ctx context.Context, in BindMediaInput) (BindMediaResult, error) {
	var result BindMediaResult
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin media tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if len(in.MediaRefs) > 0 {
		if err := s.bindMediaRefs(ctx, qtx, in, &result); err != nil {
			_ = tx.Rollback(ctx)
			if clearErr := s.clearMediaPending(ctx, s.q, in); clearErr != nil {
				return BindMediaResult{}, errors.Join(err, clearErr)
			}
			return BindMediaResult{}, err
		}
	}
	if err := s.clearMediaPending(ctx, qtx, in); err != nil {
		return BindMediaResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		// An ambiguous commit needs no adjudication: the intent-ledger rows
		// were deleted in this same transaction, so commit landed ⇔ intents
		// gone, atomically. Either way the reconciler settles the objects.
		return BindMediaResult{}, fmt.Errorf("commit media: %w", err)
	}
	return result, nil
}

func (s *ChatSession) clearMediaPending(ctx context.Context, q SessionQueries, in BindMediaInput) error {
	if err := q.ClearChatMessageChannelMediaPending(ctx, db.ClearChatMessageChannelMediaPendingParams{
		ID:            in.MessageID,
		ChatSessionID: in.SessionID,
	}); err != nil {
		return fmt.Errorf("clear chat message media pending: %w", err)
	}
	return nil
}

func (s *ChatSession) bindMediaRefs(ctx context.Context, qtx SessionQueries, in BindMediaInput, result *BindMediaResult) error {
	if !in.WorkspaceID.Valid {
		return errors.New("bind media refs: workspace_id is required")
	}
	if !in.MessageID.Valid {
		return errors.New("bind media refs: message_id is required")
	}
	keys := make([]string, 0, len(in.MediaRefs))
	for _, ref := range in.MediaRefs {
		if ref.StorageURL == "" {
			return errors.New("bind media refs: storage_url is required")
		}
		if ref.StorageKey == "" {
			return errors.New("bind media refs: storage_key is required")
		}
		keys = append(keys, ref.StorageKey)
	}
	if in.IssueID.Valid {
		if _, err := qtx.LockIssueForChannelMediaBind(ctx, db.LockIssueForChannelMediaBindParams{
			ID:          in.IssueID,
			WorkspaceID: in.WorkspaceID,
		}); err != nil {
			return fmt.Errorf("validate issue media target: %w", err)
		}
	}
	// Claim the intent-ledger rows inside this same transaction: commit
	// landed <=> intents gone, atomically, so an ambiguous COMMIT never needs
	// adjudication. A key the reconciler already moved to 'deleting' is not
	// returned and its ref must NOT attach — the object is being deleted and
	// the placeholder stays.
	claimedKeys, err := qtx.ClaimChannelMediaPendingObjectsForBind(ctx, db.ClaimChannelMediaPendingObjectsForBindParams{
		StorageKeys: keys,
		WorkspaceID: in.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("claim media intents: %w", err)
	}
	claimed := make(map[string]bool, len(claimedKeys))
	for _, k := range claimedKeys {
		claimed[k] = true
	}
	type createdMedia struct {
		id       pgtype.UUID
		ref      channel.MediaRef
		filename string
	}
	created := make([]createdMedia, 0, len(in.MediaRefs))
	ids := make([]pgtype.UUID, 0, len(in.MediaRefs))
	for _, ref := range in.MediaRefs {
		if !claimed[ref.StorageKey] {
			slog.Warn("channel media: intent claimed by reconciler; skipping attach",
				"storage_key", ref.StorageKey)
			continue
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("create attachment id: %w", err)
		}
		contentType := ref.MimeType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		filename := ref.Filename
		if filename == "" {
			filename = defaultMediaFilename(ref.Type, id.String(), contentType)
		}
		chatSessionID := in.SessionID
		if in.IssueID.Valid {
			chatSessionID = pgtype.UUID{}
		}
		att, err := qtx.CreateAttachment(ctx, db.CreateAttachmentParams{
			ID:            pgtype.UUID{Bytes: id, Valid: true},
			WorkspaceID:   in.WorkspaceID,
			IssueID:       in.IssueID,
			ChatSessionID: chatSessionID,
			UploaderType:  "member",
			UploaderID:    in.Sender,
			Filename:      filename,
			Url:           ref.StorageURL,
			ContentType:   contentType,
			SizeBytes:     ref.SizeBytes,
		})
		if err != nil {
			return fmt.Errorf("create channel attachment: %w", err)
		}
		ids = append(ids, att.ID)
		created = append(created, createdMedia{id: att.ID, ref: ref, filename: filename})
	}
	if len(ids) == 0 {
		return nil
	}
	if !in.IssueID.Valid {
		for _, media := range created {
			source := media.ref.Filename
			if strings.TrimSpace(source) == "" {
				source = mediaTypeTitle(media.ref.Type)
			}
			title := DeriveChatTitle(source)
			if title == "" {
				continue
			}
			if _, err := qtx.InitializeChatSessionMediaTitle(ctx, db.InitializeChatSessionMediaTitleParams{
				ID: in.SessionID, MessageID: in.MessageID, Title: title,
			}); err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("initialize media chat title: %w", err)
				}
			} else {
				result.InitialTitle = title
				result.TitleSource = source
			}
			break
		}
	}
	if in.IssueID.Valid {
		issueMarkdown := make([]string, 0, len(created))
		replacements := make([]inlineMediaReplacement, 0, len(created))
		for _, media := range created {
			block := channelmedia.Block(
				uuid.UUID(media.id.Bytes).String(),
				media.filename,
				media.ref.Type == channel.MsgTypeImage,
			)
			issueMarkdown = append(issueMarkdown, block)
			if media.ref.InlinePlaceholder != "" {
				replacements = append(replacements, inlineMediaReplacement{
					placeholder: media.ref.InlinePlaceholder,
					index:       media.ref.InlineIndex,
					markdown:    block,
				})
			}
		}

		base := pgtype.Text{}
		description := pgtype.Text{}
		if in.IssueDescriptionBase.Valid {
			if composed, changed := composeIssueCommandMediaDescription(
				in.Body,
				in.IssueCommandText,
				replacements,
				in.IssueDescriptionBase.String,
			); changed {
				base = in.IssueDescriptionBase
				description = pgtype.Text{String: composed, Valid: true}
			}
		}
		if _, err := qtx.MaterializeIssueChannelMediaMarkdown(ctx, db.MaterializeIssueChannelMediaMarkdownParams{
			ID:              in.IssueID,
			WorkspaceID:     in.WorkspaceID,
			BaseDescription: base,
			Description:     description.String,
			Markdown:        pgtype.Text{String: strings.Join(issueMarkdown, "\n\n"), Valid: true},
		}); err != nil {
			return fmt.Errorf("materialize issue channel media markdown: %w", err)
		}
		return nil
	}
	linkedIDs, err := qtx.LinkAttachmentsToChatMessage(ctx, db.LinkAttachmentsToChatMessageParams{
		ChatMessageID: in.MessageID,
		ChatSessionID: in.SessionID,
		WorkspaceID:   in.WorkspaceID,
		UploaderType:  "member",
		UploaderID:    in.Sender,
		AttachmentIds: ids,
	})
	if err != nil {
		return fmt.Errorf("link chat attachments: %w", err)
	}

	linked := make(map[pgtype.UUID]bool, len(linkedIDs))
	for _, id := range linkedIDs {
		linked[id] = true
	}
	replacements := make([]inlineMediaReplacement, 0, len(created))
	for _, media := range created {
		if !linked[media.id] || media.ref.InlinePlaceholder == "" {
			continue
		}
		replacements = append(replacements, inlineMediaReplacement{
			placeholder: media.ref.InlinePlaceholder,
			index:       media.ref.InlineIndex,
			markdown:    inlineAttachmentMarkdown(media.ref, media.id),
		})
	}
	if body, changed := composeInlineMediaBody(in.Body, replacements); changed {
		rows, err := qtx.UpdateChatMessageContentForChannelMedia(ctx, db.UpdateChatMessageContentForChannelMediaParams{
			ID:            in.MessageID,
			ChatSessionID: in.SessionID,
			Content:       body,
		})
		if err != nil {
			return fmt.Errorf("update chat message inline media: %w", err)
		}
		if rows != 1 {
			return fmt.Errorf("update chat message inline media: updated %d rows", rows)
		}
	}
	return nil
}

type inlineMediaReplacement struct {
	placeholder string
	index       int
	markdown    string
}

type inlineMediaEdit struct {
	start int
	end   int
	text  string
}

func composeInlineMediaBody(body string, replacements []inlineMediaReplacement) (string, bool) {
	edits := make([]inlineMediaEdit, 0, len(replacements))
	for _, replacement := range replacements {
		if replacement.placeholder == "" || replacement.index < 0 || replacement.markdown == "" {
			continue
		}
		start := nthSubstringIndex(body, replacement.placeholder, replacement.index)
		if start < 0 {
			continue
		}
		edits = append(edits, inlineMediaEdit{
			start: start,
			end:   start + len(replacement.placeholder),
			text:  replacement.markdown,
		})
	}
	if len(edits) == 0 {
		return body, false
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out strings.Builder
	last := 0
	for _, edit := range edits {
		if edit.start < last {
			continue
		}
		out.WriteString(body[last:edit.start])
		out.WriteString(edit.text)
		last = edit.end
	}
	out.WriteString(body[last:])
	return out.String(), true
}

// composeIssueCommandMediaDescription materializes media in the same positions
// as the normalized inbound body, then removes the /issue directive line. Only
// resolved media before the command is retained from the prefix; adapter-added
// quoted context remains excluded from the issue description contract.
func composeIssueCommandMediaDescription(body, commandText string, replacements []inlineMediaReplacement, fallback string) (string, bool) {
	commandStart, _, ok := issueCommandLineBounds(body, commandText)
	if !ok {
		return fallback, false
	}

	type positionedMarkdown struct {
		start    int
		markdown string
	}
	prefix := make([]positionedMarkdown, 0, len(replacements))
	for _, replacement := range replacements {
		start := nthSubstringIndex(body, replacement.placeholder, replacement.index)
		if start >= 0 && start < commandStart {
			prefix = append(prefix, positionedMarkdown{start: start, markdown: replacement.markdown})
		}
	}
	sort.Slice(prefix, func(i, j int) bool { return prefix[i].start < prefix[j].start })

	composed, changed := composeInlineMediaBody(body, replacements)
	if !changed {
		return fallback, false
	}
	_, commandEnd, ok := issueCommandLineBounds(composed, commandText)
	if !ok {
		return fallback, false
	}

	parts := make([]string, 0, len(prefix)+1)
	for _, item := range prefix {
		if markdown := strings.TrimSpace(item.markdown); markdown != "" {
			parts = append(parts, markdown)
		}
	}
	if suffix := strings.TrimSpace(composed[commandEnd:]); suffix != "" {
		parts = append(parts, suffix)
	}
	description := strings.Join(parts, "\n\n")
	for _, replacement := range replacements {
		if replacement.markdown != "" && strings.Contains(description, replacement.markdown) {
			return description, true
		}
	}
	// A malformed adapter layout placed every matched marker inside the command
	// line that is removed above. Fall back to append so attachments never become
	// invisible merely to preserve an unusable inline layout.
	return fallback, false
}

func nthSubstringIndex(body, marker string, target int) int {
	offset := 0
	for index := 0; ; index++ {
		found := strings.Index(body[offset:], marker)
		if found < 0 {
			return -1
		}
		found += offset
		if index == target {
			return found
		}
		offset = found + len(marker)
	}
}

func inlineAttachmentMarkdown(ref channel.MediaRef, id pgtype.UUID) string {
	downloadPath := "/api/attachments/" + uuid.UUID(id.Bytes).String() + "/download"
	if ref.Type == channel.MsgTypeImage {
		return "![](" + downloadPath + ")"
	}
	label := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]").Replace(ref.Filename)
	if label == "" {
		label = "attachment"
	}
	return "[" + label + "](" + downloadPath + ")"
}

func defaultMediaFilename(kind channel.MsgType, id, contentType string) string {
	prefix := "attachment"
	switch kind {
	case channel.MsgTypeImage:
		prefix = "image"
	case channel.MsgTypeVideo:
		prefix = "video"
	case channel.MsgTypeAudio:
		prefix = "audio"
	case channel.MsgTypeFile:
		prefix = "file"
	}
	ext := ""
	switch contentType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	case "video/mp4":
		ext = ".mp4"
	}
	return prefix + "-" + id + ext
}

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == pgSQLStateUniqueViolation
	}
	return false
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func textOrNullIf(valid bool, s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: valid}
}
