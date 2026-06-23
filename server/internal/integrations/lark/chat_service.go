package lark

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// pgSQLStateUniqueViolation is the Postgres SQLSTATE for unique
// constraint violations. Spelled out as a literal rather than imported
// from pgerrcode to avoid pulling in another dependency for a single
// constant. See https://www.postgresql.org/docs/current/errcodes-appendix.html
const pgSQLStateUniqueViolation = "23505"

// ErrClaimLost signals that AppendUserMessage's in-tx dedup Mark
// matched zero rows — another worker re-claimed the lark_inbound_-
// message_dedup row while we were running (stale-reclaim race). The
// transaction is rolled back, no chat_message lands, and the
// Dispatcher treats this as a duplicate drop: the other worker is the
// sole writer for this Lark message_id.
var ErrClaimLost = errors.New("lark dedup claim lost to a concurrent reclaim")

// isUniqueViolation reports whether err is a Postgres unique-violation
// (SQLSTATE 23505). The lark_chat_session_binding
// UNIQUE (installation_id, lark_chat_id) constraint surfaces this
// code when two concurrent first messages on the same Lark chat race
// to create the binding row.
func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == pgSQLStateUniqueViolation
	}
	return false
}

// TxStarter abstracts transaction creation. Re-declared in this
// package (rather than depending on internal/service) so the
// integrations layer does not back-reference into service — a circular
// dependency we want to avoid as both packages grow. Satisfied by
// *pgxpool.Pool.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// chatSessionService is the concrete ChatSessionService. It enforces
// the architectural rules from doc.go:
//
//   - EnsureChatSession only creates / looks up rows; identity must
//     already be resolved by the caller (the sender argument is a
//     trusted Multica user UUID).
//
//   - AppendUserMessage runs message-write + session-touch in a single
//     transaction so a session that has received a message has its
//     `updated_at` advanced atomically. Per-Lark-message-id idempotency
//     is enforced by the Dispatcher's two-phase dedup gate
//     (ClaimLarkInboundDedup + Mark/Release) BEFORE AppendUserMessage
//     runs — see Dispatcher.Handle. AppendUserMessage trusts the
//     dispatcher's claim and does not re-check dedup itself; this is
//     what lets the dispatcher safely Release the claim on infra
//     failure (rolled-back tx → no chat_message → next replay
//     re-processes).
type chatSessionService struct {
	queries   *db.Queries
	txStarter TxStarter
}

// NewChatSessionService constructs a ChatSessionService backed by the
// supplied queries and tx starter. The tx starter is required;
// without it, AppendUserMessage cannot run dedup + insert atomically.
func NewChatSessionService(queries *db.Queries, tx TxStarter) ChatSessionService {
	return &chatSessionService{queries: queries, txStarter: tx}
}

// EnsureChatSession returns the chat_session.id bound to the given
// Lark chat. The implementation is the two-phase find-or-create
// expected by the interface contract:
//
//  1. Look up the existing lark_chat_session_binding.
//  2. If found, return its chat_session_id.
//  3. Otherwise, in one transaction: create chat_session +
//     lark_chat_session_binding. Commit.
//
// The race between two concurrent first messages on the same Lark
// chat is resolved by the UNIQUE (installation_id, lark_chat_id)
// constraint on lark_chat_session_binding: the loser of the race
// catches the unique violation, re-reads the existing row, and
// returns its chat_session_id.
func (s *chatSessionService) EnsureChatSession(ctx context.Context, p EnsureChatSessionParams) (pgtype.UUID, error) {
	// Fast path: existing binding.
	existing, err := s.queries.GetLarkChatSessionBinding(ctx, db.GetLarkChatSessionBindingParams{
		InstallationID: p.InstallationID,
		LarkChatID:     string(p.ChatID),
	})
	if err == nil {
		return existing.ChatSessionID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, fmt.Errorf("lookup chat session binding: %w", err)
	}

	// Create path: chat_session + binding atomically.
	id, err := s.createSessionAndBinding(ctx, p)
	if err == nil {
		return id, nil
	}

	// Lost the race: another goroutine created the binding between our
	// lookup and our insert. Re-read and return the winner's session.
	if isUniqueViolation(err) {
		existing, lookupErr := s.queries.GetLarkChatSessionBinding(ctx, db.GetLarkChatSessionBindingParams{
			InstallationID: p.InstallationID,
			LarkChatID:     string(p.ChatID),
		})
		if lookupErr == nil {
			return existing.ChatSessionID, nil
		}
		return pgtype.UUID{}, fmt.Errorf("race re-read after unique violation: %w", lookupErr)
	}
	return pgtype.UUID{}, err
}

func (s *chatSessionService) createSessionAndBinding(ctx context.Context, p EnsureChatSessionParams) (pgtype.UUID, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)

	session, err := qtx.CreateChatSession(ctx, db.CreateChatSessionParams{
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
		CreatorID:   p.Sender,
		Title:       defaultSessionTitle(p.ChatType),
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("create chat session: %w", err)
	}

	if _, err := qtx.CreateLarkChatSessionBinding(ctx, db.CreateLarkChatSessionBindingParams{
		ChatSessionID:  session.ID,
		InstallationID: p.InstallationID,
		LarkChatID:     string(p.ChatID),
		LarkChatType:   string(p.ChatType),
	}); err != nil {
		return pgtype.UUID{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, fmt.Errorf("commit: %w", err)
	}
	return session.ID, nil
}

// AppendUserMessage writes the user message into chat_session and
// (when the body parses as `/issue …`) returns the parsed command so
// the caller can dispatch through IssueService.
//
// Idempotency is enforced as a two-step contract with the Dispatcher's
// ClaimLarkInboundDedup gate:
//
//  1. BEFORE this method is called, the Dispatcher claims an in-flight
//     row in lark_inbound_message_dedup and gets back a `claim_token`.
//     A replay whose previous attempt reached a durable outcome —
//     processed_at IS NOT NULL — is dropped at that gate and never
//     reaches AppendUserMessage at all.
//
//  2. INSIDE this method's chat_message+session transaction, when
//     ClaimToken is supplied, we run MarkLarkInboundDedupProcessed
//     gated on (message_id, claim_token, processed_at IS NULL). If
//     another worker re-claimed the dedup row in the meantime — e.g.
//     because we ran slowly past the 60-second staleness TTL — the
//     row's claim_token has rotated, our UPDATE matches zero rows,
//     and we return ErrClaimLost. The deferred Rollback then unwinds
//     the chat_message + session writes, so no second chat_message
//     lands for the same Lark message_id.
//
// Together these close the two windows that staleness-TTL alone left
// open (stale-reclaim race + Mark-window crash): the durable write
// and the dedup Mark commit (or roll back) atomically.
//
// Callers that pass an invalid (zero) ClaimToken get the legacy
// behavior — just write the message — and are responsible for
// finalizing the dedup row themselves. The dispatcher always passes
// the live token; tests use the zero value when they want to exercise
// the write path without modelling dedup.
func (s *chatSessionService) AppendUserMessage(ctx context.Context, p AppendUserMessageParams) (AppendResult, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return AppendResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)

	// Parse the command from the user's OWN typed text (CommandBody),
	// not the stored Body: the enricher prepends quoted / forwarded
	// context to Body, which would push a `/issue …` off the first line
	// and silently stop creating the issue (parseIssueCommand only
	// inspects the first non-empty line). Fall back to Body when
	// CommandBody is unset so non-enriched callers are unaffected.
	//
	// Parse BEFORE the insert so the "/issue alone → use previous user
	// message" fallback queries from the message set that does NOT yet
	// include the message currently being appended; otherwise the
	// previous-message lookup would self-reference.
	commandSource := p.CommandBody
	if commandSource == "" {
		commandSource = p.Body
	}
	cmd, _ := parseIssueCommand(commandSource)
	if cmd != nil && cmd.Title == "" {
		prev, err := qtx.GetMostRecentUserChatMessage(ctx, p.ChatSessionID)
		if err == nil {
			cmd.Title = titleFromPreviousMessage(prev.Content)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return AppendResult{}, fmt.Errorf("previous message lookup: %w", err)
		}
	}

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

	// Record this message as the chat binding's most-recent reply
	// target. The outbound patcher is event-driven and disconnected from
	// the inbound message, so it cannot otherwise know which message /
	// thread to reply into. We persist the latest trigger here (in the
	// same tx as the chat_message write) so EventChatDone can thread the
	// reply back into the originating Lark topic. last_lark_thread_id is
	// NULL for non-thread messages, which keeps the outbound on the
	// chat-level send path. Skipped when there is no Lark message_id
	// (defensive: every real inbound carries one).
	if p.LarkMessageID != "" {
		if err := qtx.UpdateLarkChatSessionBindingReplyTarget(ctx, db.UpdateLarkChatSessionBindingReplyTargetParams{
			ChatSessionID:     p.ChatSessionID,
			LastLarkMessageID: textOrNull(p.LarkMessageID),
			LastLarkThreadID:  textOrNull(p.LarkThreadID),
		}); err != nil {
			return AppendResult{}, fmt.Errorf("update reply target: %w", err)
		}
	}

	// In-tx dedup Mark, gated on the supplied claim token. The whole
	// point of doing this here — rather than after Commit — is that
	// the chat_message write and the Mark either commit atomically or
	// roll back atomically. A stale-reclaim by another worker rotated
	// claim_token, so our UPDATE matches zero rows; deferring to the
	// post-method Mark path would leave the chat_message in place
	// while another worker also wrote one.
	markedInTx := false
	if p.ClaimToken.Valid && p.LarkMessageID != "" {
		rows, err := qtx.MarkLarkInboundDedupProcessed(ctx, db.MarkLarkInboundDedupProcessedParams{
			InstallationID: p.InstallationID,
			MessageID:      p.LarkMessageID,
			ClaimToken:     p.ClaimToken,
		})
		if err != nil {
			return AppendResult{}, fmt.Errorf("mark dedup processed: %w", err)
		}
		if rows == 0 {
			// Another worker holds (or already finalized) this
			// claim. Roll back via the deferred Rollback — the
			// chat_message insert never commits.
			return AppendResult{}, ErrClaimLost
		}
		markedInTx = true
	}

	if err := tx.Commit(ctx); err != nil {
		return AppendResult{}, fmt.Errorf("commit: %w", err)
	}

	return AppendResult{IssueCommand: cmd, DedupMarked: markedInTx}, nil
}

// titleFromPreviousMessage extracts a sensible title from a prior
// chat message. The spec says the previous "user message" is the
// fallback; in practice the previous message itself might also be an
// `/issue ...` invocation (the user typed two commands in a row), in
// which case stripping the prefix yields the real intent.
func titleFromPreviousMessage(body string) string {
	if cmd, ok := parseIssueCommand(body); ok {
		return cmd.Title
	}
	// First non-empty line, trimmed. Multi-line free text "becomes"
	// the title via its first line; description fallback for the
	// previous-message path is out of scope (the user's intent was a
	// title alone).
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			return t
		}
	}
	return ""
}

// defaultSessionTitle gives a freshly created chat_session a
// reasonable display title. We do not derive from message content —
// the first message hasn't been appended yet — so we use a stable
// per-chat-type label that the front-end can localize later.
func defaultSessionTitle(t ChatType) string {
	switch t {
	case ChatTypeGroup:
		return "Lark group chat"
	case ChatTypeP2P:
		return "Lark direct message"
	default:
		return "Lark chat"
	}
}
