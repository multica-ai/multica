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
	ClearChatMessageChannelMediaPending(ctx context.Context, arg db.ClearChatMessageChannelMediaPendingParams) error
	LockIssueForChannelMediaBind(ctx context.Context, arg db.LockIssueForChannelMediaBindParams) (pgtype.UUID, error)
	UpdateChatMessageContentForChannelMedia(ctx context.Context, arg db.UpdateChatMessageContentForChannelMediaParams) (int64, error)
	MaterializeIssueChannelMediaMarkdown(ctx context.Context, arg db.MaterializeIssueChannelMediaMarkdownParams) (db.Issue, error)
	CreateAttachment(ctx context.Context, arg db.CreateAttachmentParams) (db.Attachment, error)
	LinkAttachmentsToChatMessage(ctx context.Context, arg db.LinkAttachmentsToChatMessageParams) ([]pgtype.UUID, error)
	ClaimChannelMediaPendingObjectsForBind(ctx context.Context, arg db.ClaimChannelMediaPendingObjectsForBindParams) ([]string, error)
	TouchChatSession(ctx context.Context, id pgtype.UUID) error
	MarkChannelChatSessionPendingFresh(ctx context.Context, chatSessionID pgtype.UUID) (bool, error)
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
	return a.q.CreateAttachment(ctx, arg)
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
func (a dbSessionQueries) MarkChannelChatSessionPendingFresh(ctx context.Context, chatSessionID pgtype.UUID) (bool, error) {
	return a.q.MarkChannelChatSessionPendingFresh(ctx, chatSessionID)
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
	SessionID           pgtype.UUID
	Sender              pgtype.UUID
	InstallationID      pgtype.UUID
	Body                string
	CommandText         string
	MessageID           string
	ThreadID            string
	ClaimToken          pgtype.UUID
	MediaPendingSeconds float64
	ForceFresh          bool
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

	// channel_ingested is the immutable provenance the cancel path gates on:
	// it must be stamped in the same transaction as the message so no later
	// binding deletion (archive, installation rebind) can strip it.
	msg, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ChatSessionID:           in.SessionID,
		Role:                    "user",
		Content:                 in.Body,
		MessageKind:             textOrNullIf(cmd != nil, channelCommandMessageKind),
		ChannelMediaPendingSecs: pgtype.Float8{Float64: in.MediaPendingSeconds, Valid: in.MediaPendingSeconds > 0},
		ChannelIngested:         pgtype.Bool{Bool: true, Valid: true},
	})
	if err != nil {
		return AppendResult{}, fmt.Errorf("create chat message: %w", err)
	}
	if err := qtx.TouchChatSession(ctx, in.SessionID); err != nil {
		return AppendResult{}, fmt.Errorf("touch chat session: %w", err)
	}
	if in.ForceFresh {
		if _, err := qtx.MarkChannelChatSessionPendingFresh(ctx, in.SessionID); err != nil {
			return AppendResult{}, fmt.Errorf("mark pending fresh: %w", err)
		}
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
	return AppendResult{MessageID: msg.ID, IssueCommand: cmd, DedupMarked: markedInTx}, nil
}

// MarkPendingFresh persists a bare `/new` command. Non-bare `/new` messages
// mark the same flag inside AppendUserMessage's transaction instead.
func (s *ChatSession) MarkPendingFresh(ctx context.Context, sessionID pgtype.UUID) error {
	if _, err := s.q.MarkChannelChatSessionPendingFresh(ctx, sessionID); err != nil {
		return fmt.Errorf("mark pending fresh: %w", err)
	}
	return nil
}

// BindMediaRefs creates attachment rows owned by IssueID when present, otherwise
// links them to the existing durable chat message. It also clears the message's
// media-pending marker. A failure rolls back the attachment rows, then clears
// the marker separately so the placeholder can be promoted immediately for
// graceful degradation.
func (s *ChatSession) BindMediaRefs(ctx context.Context, in BindMediaInput) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin media tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if len(in.MediaRefs) > 0 {
		if err := s.bindMediaRefs(ctx, qtx, in); err != nil {
			_ = tx.Rollback(ctx)
			if clearErr := s.clearMediaPending(ctx, s.q, in); clearErr != nil {
				return errors.Join(err, clearErr)
			}
			return err
		}
	}
	if err := s.clearMediaPending(ctx, qtx, in); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		// An ambiguous commit needs no adjudication: the intent-ledger rows
		// were deleted in this same transaction, so commit landed ⇔ intents
		// gone, atomically. Either way the reconciler settles the objects.
		return fmt.Errorf("commit media: %w", err)
	}
	return nil
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

func (s *ChatSession) bindMediaRefs(ctx context.Context, qtx SessionQueries, in BindMediaInput) error {
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
