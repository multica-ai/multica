package octo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// pgSQLStateUniqueViolation is the Postgres SQLSTATE for unique-constraint
// violations. The octo_chat_session_binding UNIQUE (installation_id,
// octo_channel_id) constraint surfaces this when two concurrent first messages
// on the same channel race to create the binding.
const pgSQLStateUniqueViolation = "23505"

// ErrClaimLost signals that AppendUserMessage's in-tx dedup Mark matched zero
// rows — another worker re-claimed the octo_inbound_dedup row mid-flight
// (stale-reclaim race). The transaction is rolled back, no chat_message lands,
// and the Dispatcher treats this as a duplicate drop.
var ErrClaimLost = errors.New("octo dedup claim lost to a concurrent reclaim")

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == pgSQLStateUniqueViolation
	}
	return false
}

// TxStarter abstracts transaction creation. Re-declared here rather than
// depending on internal/service so the integrations layer does not
// back-reference into service. Satisfied by *pgxpool.Pool.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// ChatSessionService finds-or-creates chat sessions for Octo channels and
// appends user messages with in-transaction dedup finalization.
type ChatSessionService struct {
	queries   *db.Queries
	txStarter TxStarter
}

// NewChatSessionService constructs a ChatSessionService. The tx starter is
// required; without it AppendUserMessage cannot run dedup + insert atomically.
func NewChatSessionService(queries *db.Queries, tx TxStarter) *ChatSessionService {
	return &ChatSessionService{queries: queries, txStarter: tx}
}

// EnsureChatSessionParams identifies the channel to find-or-create a session for.
type EnsureChatSessionParams struct {
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	AgentID        pgtype.UUID
	ChannelID      ChannelID
	ChannelType    ChannelType
	// Creator is the trusted Multica user UUID that owns the session row.
	Creator pgtype.UUID
}

// EnsureChatSession returns the chat_session bound to the given Octo channel,
// creating the session + binding on first contact. The race between two
// concurrent first messages is resolved by the UNIQUE (installation_id,
// octo_channel_id) constraint: the loser re-reads the winner's row.
//
// The full chat_session row is returned (not just the id) so the caller can
// enqueue an agent run without a second GetChatSession round-trip: the create
// path hands back the row CreateChatSession already produced, and the
// existing-session / race-loser paths reload it once via GetChatSession.
func (s *ChatSessionService) EnsureChatSession(ctx context.Context, p EnsureChatSessionParams) (db.ChatSession, error) {
	existing, err := s.queries.GetOctoChatSessionBinding(ctx, db.GetOctoChatSessionBindingParams{
		InstallationID: p.InstallationID,
		OctoChannelID:  string(p.ChannelID),
	})
	if err == nil {
		return s.queries.GetChatSession(ctx, existing.ChatSessionID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.ChatSession{}, fmt.Errorf("lookup chat session binding: %w", err)
	}

	session, err := s.createSessionAndBinding(ctx, p)
	if err == nil {
		return session, nil
	}
	if isUniqueViolation(err) {
		existing, lookupErr := s.queries.GetOctoChatSessionBinding(ctx, db.GetOctoChatSessionBindingParams{
			InstallationID: p.InstallationID,
			OctoChannelID:  string(p.ChannelID),
		})
		if lookupErr == nil {
			return s.queries.GetChatSession(ctx, existing.ChatSessionID)
		}
		return db.ChatSession{}, fmt.Errorf("race re-read after unique violation: %w", lookupErr)
	}
	return db.ChatSession{}, err
}

func (s *ChatSessionService) createSessionAndBinding(ctx context.Context, p EnsureChatSessionParams) (db.ChatSession, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return db.ChatSession{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)

	session, err := qtx.CreateChatSession(ctx, db.CreateChatSessionParams{
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
		CreatorID:   p.Creator,
		Title:       defaultSessionTitle(p.ChannelType),
	})
	if err != nil {
		return db.ChatSession{}, fmt.Errorf("create chat session: %w", err)
	}
	if _, err := qtx.CreateOctoChatSessionBinding(ctx, db.CreateOctoChatSessionBindingParams{
		ChatSessionID:   session.ID,
		InstallationID:  p.InstallationID,
		OctoChannelID:   string(p.ChannelID),
		OctoChannelType: int16(p.ChannelType),
	}); err != nil {
		return db.ChatSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChatSession{}, fmt.Errorf("commit: %w", err)
	}
	return session, nil
}

// AppendUserMessageParams carries the message to store plus the dedup claim to
// finalize in the same transaction.
type AppendUserMessageParams struct {
	ChatSessionID  pgtype.UUID
	Body           string
	InstallationID pgtype.UUID
	MessageID      string
	// ClaimToken is the dedup owner token from ClaimOctoInboundDedup. When valid
	// (and MessageID is set) the message insert and the dedup Mark commit
	// atomically; a token mismatch (stale reclaim) yields ErrClaimLost.
	ClaimToken pgtype.UUID
}

// AppendResult reports whether the dedup row was Marked inside the message
// transaction. When false (caller passed no claim token), the caller is
// responsible for finalizing the dedup row.
type AppendResult struct {
	DedupMarked bool
}

// AppendUserMessage writes the user message into chat_session and, when a claim
// token is supplied, marks the dedup row processed in the same transaction so
// the durable write and the dedup Mark commit (or roll back) atomically.
func (s *ChatSessionService) AppendUserMessage(ctx context.Context, p AppendUserMessageParams) (AppendResult, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return AppendResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)

	if _, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ChatSessionID: p.ChatSessionID,
		Role:          "user",
		Content:       p.Body,
	}); err != nil {
		return AppendResult{}, fmt.Errorf("create chat message: %w", err)
	}
	if err := qtx.TouchChatSession(ctx, p.ChatSessionID); err != nil {
		return AppendResult{}, fmt.Errorf("touch chat session: %w", err)
	}

	markedInTx := false
	if p.ClaimToken.Valid && p.MessageID != "" {
		rows, err := qtx.MarkOctoInboundDedupProcessed(ctx, db.MarkOctoInboundDedupProcessedParams{
			InstallationID: p.InstallationID,
			MessageID:      p.MessageID,
			ClaimToken:     p.ClaimToken,
		})
		if err != nil {
			return AppendResult{}, fmt.Errorf("mark dedup processed: %w", err)
		}
		if rows == 0 {
			// Another worker re-claimed this dedup row; the deferred Rollback
			// unwinds the chat_message insert so no duplicate lands.
			return AppendResult{}, ErrClaimLost
		}
		markedInTx = true
	}

	if err := tx.Commit(ctx); err != nil {
		return AppendResult{}, fmt.Errorf("commit: %w", err)
	}
	return AppendResult{DedupMarked: markedInTx}, nil
}

// defaultSessionTitle gives a freshly created chat_session a stable display
// title; the first message has not been appended yet so we use a per-channel
// label rather than deriving from content.
func defaultSessionTitle(t ChannelType) string {
	switch t {
	case ChannelGroup:
		return "Octo group chat"
	case ChannelDM:
		return "Octo direct message"
	default:
		return "Octo chat"
	}
}
