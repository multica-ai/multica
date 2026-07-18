package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
	CreateChatMessage(ctx context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error)
	TouchChatSession(ctx context.Context, id pgtype.UUID) error
	GetMostRecentUserChatMessage(ctx context.Context, chatSessionID pgtype.UUID) (db.ChatMessage, error)
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
	return a.q.CreateChannelChatSessionBinding(ctx, arg)
}
func (a dbSessionQueries) CreateChatMessage(ctx context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error) {
	return a.q.CreateChatMessage(ctx, arg)
}
func (a dbSessionQueries) TouchChatSession(ctx context.Context, id pgtype.UUID) error {
	return a.q.TouchChatSession(ctx, id)
}
func (a dbSessionQueries) GetMostRecentUserChatMessage(ctx context.Context, chatSessionID pgtype.UUID) (db.ChatMessage, error) {
	return a.q.GetMostRecentUserChatMessage(ctx, chatSessionID)
}
func (a dbSessionQueries) UpdateChannelChatSessionBindingReplyTarget(ctx context.Context, arg db.UpdateChannelChatSessionBindingReplyTargetParams) error {
	return a.q.UpdateChannelChatSessionBindingReplyTarget(ctx, arg)
}
func (a dbSessionQueries) MarkChannelInboundDedupProcessed(ctx context.Context, arg db.MarkChannelInboundDedupProcessedParams) (int64, error) {
	return a.q.MarkChannelInboundDedupProcessed(ctx, arg)
}

// SessionTitles are the per-platform display titles a freshly created
// chat_session gets (the first message has not been appended yet, so the title
// cannot be derived from content). The adapter supplies its own wording.
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
		WorkspaceID: in.WorkspaceID,
		AgentID:     in.AgentID,
		CreatorID:   in.Sender,
		Title:       s.titles.forType(in.ChatType),
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
	SessionID      pgtype.UUID
	Sender         pgtype.UUID
	InstallationID pgtype.UUID
	Body           string
	CommandText    string
	MessageID      string
	ThreadID       string
	ClaimToken     pgtype.UUID
}

// AppendUserMessage writes the user message into the chat_session (touching it
// and recording the reply target), runs the in-tx dedup Mark when a claim token
// is supplied, and returns the parsed `/issue` command when present. Returns
// ErrClaimLost when a concurrent reclaim rotated the dedup token mid-flight, in
// which case the whole transaction rolls back (no chat_message lands).
func (s *ChatSession) AppendUserMessage(ctx context.Context, in AppendInput) (AppendResult, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return AppendResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	// Parse before the insert so the bare-`/issue` previous-message fallback
	// queries the message set that does NOT yet include this message.
	commandSource := in.CommandText
	if commandSource == "" {
		commandSource = in.Body
	}
	cmd, _ := ParseIssueCommand(commandSource)
	if cmd != nil && cmd.Title == "" {
		prev, err := qtx.GetMostRecentUserChatMessage(ctx, in.SessionID)
		if err == nil {
			cmd.Title = titleFromPreviousMessage(prev.Content)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return AppendResult{}, fmt.Errorf("previous message lookup: %w", err)
		}
	}

	if _, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ChatSessionID: in.SessionID,
		Role:          "user",
		Content:       in.Body,
	}); err != nil {
		return AppendResult{}, fmt.Errorf("create chat message: %w", err)
	}
	if err := qtx.TouchChatSession(ctx, in.SessionID); err != nil {
		return AppendResult{}, fmt.Errorf("touch chat session: %w", err)
	}

	// Record the latest trigger so the decoupled outbound patcher can thread
	// its reply back into the originating topic.
	if in.MessageID != "" {
		if err := qtx.UpdateChannelChatSessionBindingReplyTarget(ctx, db.UpdateChannelChatSessionBindingReplyTargetParams{
			ChatSessionID: in.SessionID,
			LastMessageID: textOrNull(in.MessageID),
			LastThreadID:  textOrNull(in.ThreadID),
		}); err != nil {
			return AppendResult{}, fmt.Errorf("update reply target: %w", err)
		}
	}

	markedInTx := false
	if in.ClaimToken.Valid && in.MessageID != "" {
		rows, err := qtx.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
			InstallationID: in.InstallationID,
			MessageID:      in.MessageID,
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

	if err := tx.Commit(ctx); err != nil {
		return AppendResult{}, fmt.Errorf("commit: %w", err)
	}
	return AppendResult{IssueCommand: cmd, DedupMarked: markedInTx}, nil
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
