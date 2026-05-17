// Package conversation owns the channel conversation, message, entity reference,
// and turn persistence boundary.
package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ConversationTypeGroup  = "group"
	ConversationTypeDirect = "direct"
	ConversationTypeThread = "thread"

	DirectionInbound  = "inbound"
	DirectionOutbound = "outbound"
	DirectionInternal = "internal"

	MessageTypeUser         = "user"
	MessageTypeBot          = "bot"
	MessageTypeAgent        = "agent"
	MessageTypeSystem       = "system"
	MessageTypeNotification = "notification"

	SenderTypeUser    = "user"
	SenderTypeBot     = "bot"
	SenderTypeAgent   = "agent"
	SenderTypeSystem  = "system"
	SenderTypeUnknown = "unknown"

	ContentFormatPlain    = "plain"
	ContentFormatMarkdown = "markdown"
	ContentFormatCard     = "card"
	ContentFormatJSON     = "json"

	HandoffKindNone       = "none"
	HandoffKindApproval   = "approval"
	HandoffKindRetry      = "retry"
	HandoffKindContinue   = "continue"
	HandoffKindNeedInput  = "need_input"
	HandoffKindReviewPass = "review_pass"
	HandoffKindFailure    = "failure"

	EntityTypeIssue        = "issue"
	EntityTypeAgent        = "agent"
	EntityTypeAgentTask    = "agent_task"
	EntityTypeIssueComment = "issue_comment"
	EntityTypeProject      = "project"
	EntityTypePullRequest  = "pull_request"
	EntityTypeInboxItem    = "inbox_item"
	EntityTypeWorkspace    = "workspace"

	EntityRolePrimary       = "primary"
	EntityRoleMentioned     = "mentioned"
	EntityRoleHandoffTarget = "handoff_target"
	EntityRoleSource        = "source"
	EntityRoleResult        = "result"
	EntityRoleContext       = "context"

	TurnStatusProcessing   = "processing"
	TurnStatusWaitingAgent = "waiting_agent"
	TurnStatusCompleted    = "completed"
	TurnStatusFailed       = "failed"
	TurnStatusDead         = "dead"
	TurnStatusSkipped      = "skipped"
)

// Conversation is the persisted external chat container.
//
// Parameters are represented as strings at this boundary because channel code
// already normalizes provider ids and UUIDs as strings before persistence.
type Conversation struct {
	ID               string
	Provider         string
	ConnectionID     string
	ConversationKey  string
	ChatID           string
	ChatType         string
	ConversationType string
	ExternalThreadID string
	WorkspaceID      string
	Title            string
	SenderExternalID string
	Status           string
	LastMessageAt    time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Message is the unified channel message record.
//
// It is intentionally provider-neutral; platform-specific payloads belong in
// Body or Metadata, while business relationships belong in EntityRef.
type Message struct {
	ID                       string
	Provider                 string
	ConnectionID             string
	ConversationID           string
	WorkspaceID              string
	ChatID                   string
	ChatType                 string
	ThreadID                 string
	PlatformMessageID        string
	EventID                  string
	InboundEventID           string
	OutboundNotificationID   string
	Direction                string
	MessageType              string
	SenderType               string
	SenderExternalID         string
	SenderUserID             string
	SenderAgentID            string
	RepresentedAgentID       string
	Text                     string
	Body                     json.RawMessage
	ContentFormat            string
	ReplyToPlatformMessageID string
	QuotedPlatformMessageID  string
	ReplyToMessageID         string
	QuotedMessageID          string
	HandoffKind              string
	SuggestedActions         json.RawMessage
	Metadata                 json.RawMessage
	OccurredAt               time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// EntityRef links one channel message to a business entity such as an issue,
// agent, issue comment, or agent task.
type EntityRef struct {
	ID          string
	MessageID   string
	WorkspaceID string
	EntityType  string
	EntityID    string
	EntityKey   string
	Display     string
	Role        string
	Metadata    json.RawMessage
	CreatedAt   time.Time
}

// Turn records the processing lifecycle from a user message to a system or
// agent response.
type Turn struct {
	ID                string
	Provider          string
	ConnectionID      string
	ConversationID    string
	WorkspaceID       string
	InboundEventID    string
	InboundMessageID  string
	OutboundMessageID string
	SenderExternalID  string
	IntentKind        string
	IntentSource      string
	IntentPayload     json.RawMessage
	AuthzStatus       string
	Status            string
	WaitKind          string
	WaitTaskID        string
	ResultPayload     json.RawMessage
	LastError         string
	StartedAt         time.Time
	CompletedAt       time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Store defines the persistence operations needed by the channel runtime.
//
// It deliberately exposes message and turn facts only; intent interpretation
// and action dispatch stay in the inbound runtime and dispatcher.
type Store interface {
	EnsureConversation(ctx context.Context, item Conversation) (Conversation, error)
	CreateMessage(ctx context.Context, item Message) (Message, error)
	UpdateMessageForInboundEvent(ctx context.Context, inboundEventID string, item Message) error
	AddEntityRefs(ctx context.Context, messageID string, refs []EntityRef) error
	FindMessageByPlatformID(ctx context.Context, connectionID, platformMessageID string) (Message, bool, error)
	FindMessageByInboundEventID(ctx context.Context, inboundEventID string) (Message, bool, error)
	ListEntityRefsByMessageID(ctx context.Context, messageID string) ([]EntityRef, error)
	ListRecentContextEntityRefs(ctx context.Context, connectionID, conversationID, senderExternalID, threadID string, since time.Time, limit int) ([]EntityRef, error)
	ListRecentHandoffMessages(ctx context.Context, connectionID, conversationID, threadID string, since time.Time, limit int) ([]Message, error)
	CreateTurn(ctx context.Context, item Turn) (Turn, error)
	UpsertTurn(ctx context.Context, item Turn) (Turn, error)
	CompleteTurn(ctx context.Context, id string, outboundMessageID string, status string, resultPayload json.RawMessage, lastError string) error
	CompleteTurnForInboundEvent(ctx context.Context, inboundEventID string, outboundMessageID string, status string, resultPayload json.RawMessage, lastError string) error
}

type dbHandle interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// DBStore is the PostgreSQL implementation of Store.
type DBStore struct {
	db dbHandle
}

const messageSelectColumns = `
id::text, provider, connection_id, conversation_id::text, COALESCE(workspace_id::text, ''),
chat_id, chat_type, thread_id, platform_message_id, event_id,
COALESCE(inbound_event_id::text, ''), COALESCE(outbound_notification_id::text, ''),
direction, message_type, sender_type, sender_external_id,
COALESCE(sender_user_id::text, ''), COALESCE(sender_agent_id::text, ''),
COALESCE(represented_agent_id::text, ''), text, body, content_format,
reply_to_platform_message_id, quoted_platform_message_id,
COALESCE(reply_to_message_id::text, ''), COALESCE(quoted_message_id::text, ''),
handoff_kind, suggested_actions, metadata, occurred_at, created_at, updated_at
`

type scanner interface {
	Scan(dest ...any) error
}

// NewDBStore creates a new PostgreSQL-backed channel conversation store.
func NewDBStore(pool *pgxpool.Pool) *DBStore {
	return &DBStore{db: pool}
}

// NewTxStore creates a conversation store scoped to an existing transaction.
func NewTxStore(tx pgx.Tx) *DBStore {
	return &DBStore{db: tx}
}

// EnsureConversation creates or updates an external channel conversation.
func (s *DBStore) EnsureConversation(ctx context.Context, item Conversation) (Conversation, error) {
	if s == nil || s.db == nil {
		return Conversation{}, errors.New("conversation store is not configured")
	}
	if err := validateConversation(item); err != nil {
		return Conversation{}, err
	}
	conversationType := strings.TrimSpace(item.ConversationType)
	if conversationType == "" {
		conversationType = item.ChatType
	}
	status := strings.TrimSpace(item.Status)
	if status == "" {
		status = "active"
	}
	err := s.db.QueryRow(ctx, `
INSERT INTO channel_conversation (
    provider,
    connection_id,
    conversation_key,
    chat_id,
    chat_type,
    conversation_type,
    external_thread_id,
    workspace_id,
    title,
    sender_external_id,
    status,
    last_message_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, nullif($8, '')::uuid, $9, $10, $11, $12)
ON CONFLICT (connection_id, conversation_key) DO UPDATE SET
    provider = EXCLUDED.provider,
    chat_id = EXCLUDED.chat_id,
    chat_type = EXCLUDED.chat_type,
    conversation_type = EXCLUDED.conversation_type,
    external_thread_id = EXCLUDED.external_thread_id,
    workspace_id = COALESCE(EXCLUDED.workspace_id, channel_conversation.workspace_id),
    title = CASE WHEN EXCLUDED.title <> '' THEN EXCLUDED.title ELSE channel_conversation.title END,
    sender_external_id = EXCLUDED.sender_external_id,
    status = EXCLUDED.status,
    last_message_at = COALESCE(EXCLUDED.last_message_at, channel_conversation.last_message_at),
    updated_at = now()
RETURNING id::text, provider, connection_id, conversation_key, chat_id, chat_type,
          conversation_type, external_thread_id, COALESCE(workspace_id::text, ''),
          title, sender_external_id, status, COALESCE(last_message_at, 'epoch'::timestamptz),
          created_at, updated_at
`, item.Provider, item.ConnectionID, item.ConversationKey, item.ChatID, item.ChatType, conversationType,
		item.ExternalThreadID, item.WorkspaceID, item.Title, item.SenderExternalID, status, nullableTime(item.LastMessageAt),
	).Scan(
		&item.ID,
		&item.Provider,
		&item.ConnectionID,
		&item.ConversationKey,
		&item.ChatID,
		&item.ChatType,
		&item.ConversationType,
		&item.ExternalThreadID,
		&item.WorkspaceID,
		&item.Title,
		&item.SenderExternalID,
		&item.Status,
		&item.LastMessageAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

// CreateMessage persists a unified channel message.
func (s *DBStore) CreateMessage(ctx context.Context, item Message) (Message, error) {
	if s == nil || s.db == nil {
		return Message{}, errors.New("conversation store is not configured")
	}
	if err := validateMessage(item); err != nil {
		return Message{}, err
	}
	body := jsonObjectOrDefault(item.Body)
	suggestedActions := jsonArrayOrDefault(item.SuggestedActions)
	metadata := jsonObjectOrDefault(item.Metadata)
	contentFormat := defaultString(item.ContentFormat, ContentFormatPlain)
	handoffKind := defaultString(item.HandoffKind, HandoffKindNone)
	err := s.db.QueryRow(ctx, `
INSERT INTO channel_message (
    provider,
    connection_id,
    conversation_id,
    workspace_id,
    chat_id,
    chat_type,
    thread_id,
    platform_message_id,
    event_id,
    inbound_event_id,
    outbound_notification_id,
    direction,
    message_type,
    sender_type,
    sender_external_id,
    sender_user_id,
    sender_agent_id,
    represented_agent_id,
    text,
    body,
    content_format,
    reply_to_platform_message_id,
    quoted_platform_message_id,
    reply_to_message_id,
    quoted_message_id,
    handoff_kind,
    suggested_actions,
    metadata,
    occurred_at
)
VALUES (
    $1, $2, $3::uuid, nullif($4, '')::uuid, $5, $6, $7, $8, $9,
    nullif($10, '')::uuid, nullif($11, '')::uuid, $12, $13, $14, $15,
    nullif($16, '')::uuid, nullif($17, '')::uuid, nullif($18, '')::uuid,
    $19, $20::jsonb, $21, $22, $23, nullif($24, '')::uuid, nullif($25, '')::uuid,
    $26, $27::jsonb, $28::jsonb, COALESCE($29::timestamptz, now())
)
RETURNING id::text, provider, connection_id, conversation_id::text, COALESCE(workspace_id::text, ''),
          chat_id, chat_type, thread_id, platform_message_id, event_id,
          COALESCE(inbound_event_id::text, ''), COALESCE(outbound_notification_id::text, ''),
          direction, message_type, sender_type, sender_external_id,
          COALESCE(sender_user_id::text, ''), COALESCE(sender_agent_id::text, ''),
          COALESCE(represented_agent_id::text, ''), text, body, content_format,
          reply_to_platform_message_id, quoted_platform_message_id,
          COALESCE(reply_to_message_id::text, ''), COALESCE(quoted_message_id::text, ''),
          handoff_kind, suggested_actions, metadata, occurred_at, created_at, updated_at
`, item.Provider, item.ConnectionID, item.ConversationID, item.WorkspaceID, item.ChatID, item.ChatType,
		item.ThreadID, item.PlatformMessageID, item.EventID, item.InboundEventID, item.OutboundNotificationID,
		item.Direction, item.MessageType, item.SenderType, item.SenderExternalID, item.SenderUserID,
		item.SenderAgentID, item.RepresentedAgentID, item.Text, body, contentFormat,
		item.ReplyToPlatformMessageID, item.QuotedPlatformMessageID, item.ReplyToMessageID,
		item.QuotedMessageID, handoffKind, suggestedActions, metadata, nullableTime(item.OccurredAt),
	).Scan(
		&item.ID,
		&item.Provider,
		&item.ConnectionID,
		&item.ConversationID,
		&item.WorkspaceID,
		&item.ChatID,
		&item.ChatType,
		&item.ThreadID,
		&item.PlatformMessageID,
		&item.EventID,
		&item.InboundEventID,
		&item.OutboundNotificationID,
		&item.Direction,
		&item.MessageType,
		&item.SenderType,
		&item.SenderExternalID,
		&item.SenderUserID,
		&item.SenderAgentID,
		&item.RepresentedAgentID,
		&item.Text,
		&item.Body,
		&item.ContentFormat,
		&item.ReplyToPlatformMessageID,
		&item.QuotedPlatformMessageID,
		&item.ReplyToMessageID,
		&item.QuotedMessageID,
		&item.HandoffKind,
		&item.SuggestedActions,
		&item.Metadata,
		&item.OccurredAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

// UpdateMessageForInboundEvent refreshes the message facts derived from an
// inbound event after pipeline normalization and workspace binding.
func (s *DBStore) UpdateMessageForInboundEvent(ctx context.Context, inboundEventID string, item Message) error {
	if s == nil || s.db == nil {
		return errors.New("conversation store is not configured")
	}
	inboundEventID = strings.TrimSpace(inboundEventID)
	if inboundEventID == "" {
		return errors.New("conversation message: missing inbound event id")
	}
	body := jsonObjectOrDefault(item.Body)
	metadata := jsonObjectOrDefault(item.Metadata)
	_, err := s.db.Exec(ctx, `
UPDATE channel_message
SET workspace_id = COALESCE(nullif($2, '')::uuid, workspace_id),
    text = $3,
    body = $4::jsonb,
    thread_id = $5,
    platform_message_id = $6,
    reply_to_platform_message_id = $7,
    quoted_platform_message_id = $8,
    metadata = $9::jsonb,
    updated_at = now()
WHERE inbound_event_id = $1::uuid
`, inboundEventID, item.WorkspaceID, item.Text, body, item.ThreadID,
		item.PlatformMessageID, item.ReplyToPlatformMessageID, item.QuotedPlatformMessageID, metadata)
	if err != nil {
		return err
	}
	if strings.TrimSpace(item.WorkspaceID) == "" {
		return nil
	}
	_, err = s.db.Exec(ctx, `
UPDATE channel_conversation c
SET workspace_id = COALESCE(c.workspace_id, nullif($2, '')::uuid),
    updated_at = now()
FROM channel_message m
WHERE m.inbound_event_id = $1::uuid
  AND m.conversation_id = c.id
`, inboundEventID, item.WorkspaceID)
	return err
}

// AddEntityRefs attaches business entity references to a message.
func (s *DBStore) AddEntityRefs(ctx context.Context, messageID string, refs []EntityRef) error {
	if s == nil || s.db == nil {
		return errors.New("conversation store is not configured")
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || len(refs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, ref := range refs {
		if strings.TrimSpace(ref.EntityType) == "" {
			continue
		}
		metadata := jsonObjectOrDefault(ref.Metadata)
		role := defaultString(ref.Role, EntityRoleMentioned)
		batch.Queue(`
INSERT INTO channel_message_entity_ref (
    message_id,
    workspace_id,
    entity_type,
    entity_id,
    entity_key,
    display,
    role,
    metadata
)
VALUES ($1::uuid, nullif($2, '')::uuid, $3, nullif($4, '')::uuid, $5, $6, $7, $8::jsonb)
ON CONFLICT DO NOTHING
`, messageID, ref.WorkspaceID, ref.EntityType, ref.EntityID, ref.EntityKey, ref.Display, role, metadata)
	}
	br := s.db.SendBatch(ctx, batch)
	defer br.Close()
	for range batch.Len() {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// FindMessageByPlatformID looks up a message by provider-assigned message id.
func (s *DBStore) FindMessageByPlatformID(ctx context.Context, connectionID, platformMessageID string) (Message, bool, error) {
	if s == nil || s.db == nil {
		return Message{}, false, errors.New("conversation store is not configured")
	}
	if strings.TrimSpace(connectionID) == "" || strings.TrimSpace(platformMessageID) == "" {
		return Message{}, false, nil
	}
	var item Message
	err := scanMessage(s.db.QueryRow(ctx, `
SELECT `+messageSelectColumns+`
FROM channel_message
WHERE connection_id = $1
  AND platform_message_id = $2
`, connectionID, platformMessageID), &item)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, err
	}
	return item, true, nil
}

// FindMessageByInboundEventID looks up the message fact created for an
// inbound event row.
func (s *DBStore) FindMessageByInboundEventID(ctx context.Context, inboundEventID string) (Message, bool, error) {
	if s == nil || s.db == nil {
		return Message{}, false, errors.New("conversation store is not configured")
	}
	inboundEventID = strings.TrimSpace(inboundEventID)
	if inboundEventID == "" {
		return Message{}, false, nil
	}
	var item Message
	err := scanMessage(s.db.QueryRow(ctx, `
SELECT `+messageSelectColumns+`
FROM channel_message
WHERE inbound_event_id = $1::uuid
`, inboundEventID), &item)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, err
	}
	return item, true, nil
}

// ListEntityRefsByMessageID returns business references attached to a message.
func (s *DBStore) ListEntityRefsByMessageID(ctx context.Context, messageID string) ([]EntityRef, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("conversation store is not configured")
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `
SELECT id::text, message_id::text, COALESCE(workspace_id::text, ''), entity_type,
       COALESCE(entity_id::text, ''), entity_key, display, role, metadata, created_at
FROM channel_message_entity_ref
WHERE message_id = $1::uuid
ORDER BY created_at ASC
`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntityRef
	for rows.Next() {
		var ref EntityRef
		if err := rows.Scan(
			&ref.ID,
			&ref.MessageID,
			&ref.WorkspaceID,
			&ref.EntityType,
			&ref.EntityID,
			&ref.EntityKey,
			&ref.Display,
			&ref.Role,
			&ref.Metadata,
			&ref.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// ListRecentContextEntityRefs returns recent entity references that belong to
// this user's conversation turn history.
//
// Parameters:
//   - connectionID: configured channel connection id.
//   - conversationID: channel_conversation id to search within.
//   - senderExternalID: platform user id whose turn history is considered.
//   - threadID: optional platform thread id; empty allows conversation-wide lookup.
//   - since: lower bound for message occurrence time; zero uses a default lookback.
//   - limit: maximum number of deduplicated entity refs to return.
//
// Returns:
//   - recent entity refs ordered by most recent message first.
//   - an error when persistence lookup fails.
func (s *DBStore) ListRecentContextEntityRefs(ctx context.Context, connectionID, conversationID, senderExternalID, threadID string, since time.Time, limit int) ([]EntityRef, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("conversation store is not configured")
	}
	if strings.TrimSpace(connectionID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(senderExternalID) == "" {
		return nil, nil
	}
	if since.IsZero() {
		since = time.Now().Add(-30 * time.Minute)
	}
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.Query(ctx, `
SELECT r.id::text, r.message_id::text, COALESCE(r.workspace_id::text, ''), r.entity_type,
       COALESCE(r.entity_id::text, ''), r.entity_key, r.display, r.role, r.metadata, r.created_at
FROM channel_message_entity_ref r
JOIN channel_message m ON m.id = r.message_id
LEFT JOIN channel_turn t
  ON t.outbound_message_id = m.id
 AND t.connection_id = m.connection_id
 AND t.conversation_id = m.conversation_id
 AND t.sender_external_id = $3
WHERE m.connection_id = $1
  AND m.conversation_id = $2::uuid
  AND m.occurred_at >= $4
  AND ($5 = '' OR m.thread_id = $5 OR m.thread_id = '')
  AND (
      (m.direction = 'inbound' AND m.sender_external_id = $3)
      OR (m.direction = 'outbound' AND t.id IS NOT NULL)
  )
ORDER BY m.occurred_at DESC, r.created_at DESC
LIMIT $6
`, connectionID, conversationID, senderExternalID, since, strings.TrimSpace(threadID), limit*4)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := make([]EntityRef, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		var ref EntityRef
		if err := rows.Scan(
			&ref.ID,
			&ref.MessageID,
			&ref.WorkspaceID,
			&ref.EntityType,
			&ref.EntityID,
			&ref.EntityKey,
			&ref.Display,
			&ref.Role,
			&ref.Metadata,
			&ref.CreatedAt,
		); err != nil {
			return nil, err
		}
		key := entityRefDedupeKey(ref)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
		if len(refs) >= limit {
			break
		}
	}
	return refs, rows.Err()
}

// ListRecentHandoffMessages returns recent messages that are waiting for a
// user response in one conversation.
func (s *DBStore) ListRecentHandoffMessages(ctx context.Context, connectionID, conversationID, threadID string, since time.Time, limit int) ([]Message, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("conversation store is not configured")
	}
	if strings.TrimSpace(connectionID) == "" || strings.TrimSpace(conversationID) == "" {
		return nil, nil
	}
	if since.IsZero() {
		since = time.Now().Add(-30 * time.Minute)
	}
	if limit <= 0 {
		limit = 2
	}
	rows, err := s.db.Query(ctx, `
SELECT `+messageSelectColumns+`
FROM channel_message
WHERE connection_id = $1
  AND conversation_id = $2::uuid
  AND handoff_kind <> 'none'
  AND occurred_at >= $3
  AND ($4 = '' OR thread_id = $4 OR thread_id = '')
ORDER BY occurred_at DESC
LIMIT $5
`, connectionID, conversationID, since, strings.TrimSpace(threadID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Message, 0, limit)
	for rows.Next() {
		var item Message
		if err := scanMessage(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// CreateTurn starts a channel processing turn.
func (s *DBStore) CreateTurn(ctx context.Context, item Turn) (Turn, error) {
	if s == nil || s.db == nil {
		return Turn{}, errors.New("conversation store is not configured")
	}
	if strings.TrimSpace(item.Provider) == "" || strings.TrimSpace(item.ConnectionID) == "" || strings.TrimSpace(item.ConversationID) == "" {
		return Turn{}, errors.New("conversation turn: missing required fields")
	}
	intentPayload := jsonObjectOrDefault(item.IntentPayload)
	resultPayload := jsonObjectOrDefault(item.ResultPayload)
	status := defaultString(item.Status, TurnStatusProcessing)
	err := s.db.QueryRow(ctx, `
INSERT INTO channel_turn (
    provider,
    connection_id,
    conversation_id,
    workspace_id,
    inbound_event_id,
    inbound_message_id,
    outbound_message_id,
    sender_external_id,
    intent_kind,
    intent_source,
    intent_payload,
    authz_status,
    status,
    wait_kind,
    wait_task_id,
    result_payload,
    last_error,
    started_at,
    completed_at
)
VALUES (
    $1, $2, $3::uuid, nullif($4, '')::uuid, nullif($5, '')::uuid,
    nullif($6, '')::uuid, nullif($7, '')::uuid, $8, $9, $10, $11::jsonb,
    $12, $13, nullif($14, ''), nullif($15, '')::uuid, $16::jsonb, nullif($17, ''),
    COALESCE($18::timestamptz, now()), $19
)
RETURNING id::text, provider, connection_id, conversation_id::text, COALESCE(workspace_id::text, ''),
          COALESCE(inbound_event_id::text, ''), COALESCE(inbound_message_id::text, ''),
          COALESCE(outbound_message_id::text, ''), sender_external_id, intent_kind,
          intent_source, intent_payload, authz_status, status, COALESCE(wait_kind, ''),
          COALESCE(wait_task_id::text, ''), result_payload, COALESCE(last_error, ''),
          started_at, COALESCE(completed_at, 'epoch'::timestamptz), created_at, updated_at
`, item.Provider, item.ConnectionID, item.ConversationID, item.WorkspaceID, item.InboundEventID,
		item.InboundMessageID, item.OutboundMessageID, item.SenderExternalID, item.IntentKind,
		item.IntentSource, intentPayload, item.AuthzStatus, status, item.WaitKind, item.WaitTaskID,
		resultPayload, item.LastError, nullableTime(item.StartedAt), nullableTime(item.CompletedAt),
	).Scan(
		&item.ID,
		&item.Provider,
		&item.ConnectionID,
		&item.ConversationID,
		&item.WorkspaceID,
		&item.InboundEventID,
		&item.InboundMessageID,
		&item.OutboundMessageID,
		&item.SenderExternalID,
		&item.IntentKind,
		&item.IntentSource,
		&item.IntentPayload,
		&item.AuthzStatus,
		&item.Status,
		&item.WaitKind,
		&item.WaitTaskID,
		&item.ResultPayload,
		&item.LastError,
		&item.StartedAt,
		&item.CompletedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

// UpsertTurn creates or refreshes a channel turn for the same inbound event.
func (s *DBStore) UpsertTurn(ctx context.Context, item Turn) (Turn, error) {
	if s == nil || s.db == nil {
		return Turn{}, errors.New("conversation store is not configured")
	}
	if strings.TrimSpace(item.Provider) == "" || strings.TrimSpace(item.ConnectionID) == "" || strings.TrimSpace(item.ConversationID) == "" {
		return Turn{}, errors.New("conversation turn: missing required fields")
	}
	intentPayload := jsonObjectOrDefault(item.IntentPayload)
	resultPayload := jsonObjectOrDefault(item.ResultPayload)
	status := defaultString(item.Status, TurnStatusProcessing)
	err := scanTurn(s.db.QueryRow(ctx, `
INSERT INTO channel_turn (
    provider,
    connection_id,
    conversation_id,
    workspace_id,
    inbound_event_id,
    inbound_message_id,
    outbound_message_id,
    sender_external_id,
    intent_kind,
    intent_source,
    intent_payload,
    authz_status,
    status,
    wait_kind,
    wait_task_id,
    result_payload,
    last_error,
    started_at,
    completed_at
)
VALUES (
    $1, $2, $3::uuid, nullif($4, '')::uuid, nullif($5, '')::uuid,
    nullif($6, '')::uuid, nullif($7, '')::uuid, $8, $9, $10, $11::jsonb,
    $12, $13, nullif($14, ''), nullif($15, '')::uuid, $16::jsonb, nullif($17, ''),
    COALESCE($18::timestamptz, now()), $19
)
ON CONFLICT (inbound_event_id) WHERE inbound_event_id IS NOT NULL DO UPDATE SET
    provider = EXCLUDED.provider,
    connection_id = EXCLUDED.connection_id,
    conversation_id = EXCLUDED.conversation_id,
    workspace_id = COALESCE(EXCLUDED.workspace_id, channel_turn.workspace_id),
    inbound_message_id = COALESCE(EXCLUDED.inbound_message_id, channel_turn.inbound_message_id),
    outbound_message_id = COALESCE(EXCLUDED.outbound_message_id, channel_turn.outbound_message_id),
    sender_external_id = EXCLUDED.sender_external_id,
    intent_kind = EXCLUDED.intent_kind,
    intent_source = EXCLUDED.intent_source,
    intent_payload = EXCLUDED.intent_payload,
    authz_status = EXCLUDED.authz_status,
    status = EXCLUDED.status,
    wait_kind = EXCLUDED.wait_kind,
    wait_task_id = EXCLUDED.wait_task_id,
    result_payload = EXCLUDED.result_payload,
    last_error = EXCLUDED.last_error,
    completed_at = EXCLUDED.completed_at,
    updated_at = now()
RETURNING id::text, provider, connection_id, conversation_id::text, COALESCE(workspace_id::text, ''),
          COALESCE(inbound_event_id::text, ''), COALESCE(inbound_message_id::text, ''),
          COALESCE(outbound_message_id::text, ''), sender_external_id, intent_kind,
          intent_source, intent_payload, authz_status, status, COALESCE(wait_kind, ''),
          COALESCE(wait_task_id::text, ''), result_payload, COALESCE(last_error, ''),
          started_at, COALESCE(completed_at, 'epoch'::timestamptz), created_at, updated_at
`, item.Provider, item.ConnectionID, item.ConversationID, item.WorkspaceID, item.InboundEventID,
		item.InboundMessageID, item.OutboundMessageID, item.SenderExternalID, item.IntentKind,
		item.IntentSource, intentPayload, item.AuthzStatus, status, item.WaitKind, item.WaitTaskID,
		resultPayload, item.LastError, nullableTime(item.StartedAt), nullableTime(item.CompletedAt),
	), &item)
	return item, err
}

// CompleteTurn marks a turn as terminal and optionally links an outbound
// message.
func (s *DBStore) CompleteTurn(ctx context.Context, id string, outboundMessageID string, status string, resultPayload json.RawMessage, lastError string) error {
	if s == nil || s.db == nil {
		return errors.New("conversation store is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("conversation turn: missing id")
	}
	if strings.TrimSpace(status) == "" {
		status = TurnStatusCompleted
	}
	payload := jsonObjectOrDefault(resultPayload)
	_, err := s.db.Exec(ctx, `
UPDATE channel_turn
SET outbound_message_id = COALESCE(nullif($2, '')::uuid, outbound_message_id),
    status = $3,
    result_payload = $4::jsonb,
    last_error = nullif($5, ''),
    completed_at = now(),
    updated_at = now()
WHERE id = $1::uuid
`, id, outboundMessageID, status, payload, lastError)
	return err
}

// CompleteTurnForInboundEvent marks the turn associated with an inbound event
// as terminal after the response message has been persisted.
func (s *DBStore) CompleteTurnForInboundEvent(ctx context.Context, inboundEventID string, outboundMessageID string, status string, resultPayload json.RawMessage, lastError string) error {
	if s == nil || s.db == nil {
		return errors.New("conversation store is not configured")
	}
	inboundEventID = strings.TrimSpace(inboundEventID)
	if inboundEventID == "" {
		return nil
	}
	if strings.TrimSpace(status) == "" {
		status = TurnStatusCompleted
	}
	payload := jsonObjectOrDefault(resultPayload)
	_, err := s.db.Exec(ctx, `
UPDATE channel_turn
SET outbound_message_id = COALESCE(nullif($2, '')::uuid, outbound_message_id),
    status = $3,
    result_payload = $4::jsonb,
    last_error = nullif($5, ''),
    completed_at = now(),
    updated_at = now()
WHERE inbound_event_id = $1::uuid
`, inboundEventID, outboundMessageID, status, payload, lastError)
	return err
}

func validateConversation(item Conversation) error {
	if strings.TrimSpace(item.Provider) == "" || strings.TrimSpace(item.ConnectionID) == "" ||
		strings.TrimSpace(item.ConversationKey) == "" || strings.TrimSpace(item.ChatID) == "" ||
		strings.TrimSpace(item.ChatType) == "" {
		return errors.New("conversation: missing required fields")
	}
	return nil
}

func scanMessage(row scanner, item *Message) error {
	return row.Scan(
		&item.ID,
		&item.Provider,
		&item.ConnectionID,
		&item.ConversationID,
		&item.WorkspaceID,
		&item.ChatID,
		&item.ChatType,
		&item.ThreadID,
		&item.PlatformMessageID,
		&item.EventID,
		&item.InboundEventID,
		&item.OutboundNotificationID,
		&item.Direction,
		&item.MessageType,
		&item.SenderType,
		&item.SenderExternalID,
		&item.SenderUserID,
		&item.SenderAgentID,
		&item.RepresentedAgentID,
		&item.Text,
		&item.Body,
		&item.ContentFormat,
		&item.ReplyToPlatformMessageID,
		&item.QuotedPlatformMessageID,
		&item.ReplyToMessageID,
		&item.QuotedMessageID,
		&item.HandoffKind,
		&item.SuggestedActions,
		&item.Metadata,
		&item.OccurredAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
}

func scanTurn(row scanner, item *Turn) error {
	return row.Scan(
		&item.ID,
		&item.Provider,
		&item.ConnectionID,
		&item.ConversationID,
		&item.WorkspaceID,
		&item.InboundEventID,
		&item.InboundMessageID,
		&item.OutboundMessageID,
		&item.SenderExternalID,
		&item.IntentKind,
		&item.IntentSource,
		&item.IntentPayload,
		&item.AuthzStatus,
		&item.Status,
		&item.WaitKind,
		&item.WaitTaskID,
		&item.ResultPayload,
		&item.LastError,
		&item.StartedAt,
		&item.CompletedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
}

func validateMessage(item Message) error {
	if strings.TrimSpace(item.Provider) == "" || strings.TrimSpace(item.ConnectionID) == "" ||
		strings.TrimSpace(item.ConversationID) == "" || strings.TrimSpace(item.ChatID) == "" ||
		strings.TrimSpace(item.ChatType) == "" || strings.TrimSpace(item.Direction) == "" ||
		strings.TrimSpace(item.MessageType) == "" || strings.TrimSpace(item.SenderType) == "" {
		return errors.New("conversation message: missing required fields")
	}
	return nil
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func defaultString(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func entityRefDedupeKey(ref EntityRef) string {
	entityType := strings.TrimSpace(ref.EntityType)
	if entityType == "" {
		return ""
	}
	if key := strings.ToUpper(strings.TrimSpace(ref.EntityKey)); key != "" {
		return entityType + ":key:" + key
	}
	if id := strings.TrimSpace(ref.EntityID); id != "" {
		return entityType + ":id:" + id
	}
	return ""
}

func jsonObjectOrDefault(raw json.RawMessage) []byte {
	if len(raw) == 0 || !json.Valid(raw) {
		return []byte(`{}`)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return []byte(`{}`)
	}
	if _, ok := v.(map[string]any); !ok {
		return []byte(`{}`)
	}
	return raw
}

func jsonArrayOrDefault(raw json.RawMessage) []byte {
	if len(raw) == 0 || !json.Valid(raw) {
		return []byte(`[]`)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return []byte(`[]`)
	}
	if _, ok := v.([]any); !ok {
		return []byte(`[]`)
	}
	return raw
}

var _ Store = (*DBStore)(nil)
