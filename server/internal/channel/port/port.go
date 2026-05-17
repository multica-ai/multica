package port

import (
	"context"
	"encoding/json"
)

// EventType identifies the kind of inbound event the channel layer is
// observing. Adapters normalise the platform-specific event names into this
// closed enum so the dispatcher (T9–T11) does not need to know per-platform
// vocabulary. New variants are added here, not by passing strings.
type EventType string

const (
	// EventTypeMessageReceived is emitted when a user sends a message in a
	// conversation the adapter is subscribed to. Group chats may still be
	// filtered later using BotMentioned + the workspace binding's listen_mode;
	// direct chats are typically processed without that gate.
	EventTypeMessageReceived EventType = "message_received"

	// EventTypeMessageRecalled is emitted when the upstream platform signals
	// that a previously delivered message has been recalled / deleted by the
	// sender. The dispatcher must NOT delete any Issue or Comment; it only
	// posts a recall annotation in the chat thread (PRD E6).
	EventTypeMessageRecalled EventType = "message_recalled"
)

// IntentKind is the high-level command category. Defined here (in port)
// rather than imported from the intent package to avoid a circular
// dependency: port is imported by intent-recog (T9/T10) which produces
// Intents, and by dispatch (T11) which consumes them.
type IntentKind string

const (
	IntentCreateIssue   IntentKind = "CreateIssue"
	IntentAddComment    IntentKind = "AddComment"
	IntentQueryIssue    IntentKind = "QueryIssue"
	IntentQueryProgress IntentKind = "QueryProgress"
	IntentIssueDetail   IntentKind = "IssueDetail"
	IntentIssueTimeline IntentKind = "IssueTimeline"
	IntentIssueLogs     IntentKind = "IssueLogs"
	IntentSetStatus     IntentKind = "SetStatus"
	IntentDelete        IntentKind = "Delete"
	IntentSetAssignee   IntentKind = "SetAssignee"
	IntentSetPriority   IntentKind = "SetPriority"
	IntentSetLabel      IntentKind = "SetLabel"
	IntentConfirmAction IntentKind = "ConfirmAction"
	IntentCancelAction  IntentKind = "CancelAction"
	IntentUnsupported   IntentKind = "Unsupported"
	IntentUnknown       IntentKind = "Unknown"
	IntentASKClarify    IntentKind = "ASK_CLARIFY"
)

// IntentSource identifies how an Intent was recognised.
type IntentSource string

const (
	SourceCommand IntentSource = "command"
	SourceRule    IntentSource = "rule"
	SourceChat    IntentSource = "chat"
)

// InboundIntent carries the parsed intent attached to an InboundEvent by
// the intent-recog step (T9/T10). The dispatch step (T11) reads this
// field to route the event to the correct facade handler.
type InboundIntent struct {
	Kind       IntentKind
	Confidence float64
	Params     map[string]string
	Source     IntentSource
}

// ChatType distinguishes between a one-on-one (direct) chat and a group chat.
// The dispatcher uses this to apply different policies (e.g. PRD §F7 path
// rules: "group commands require workspace membership; private chat is
// reserved for the binding flow and notification delivery").
type ChatType string

const (
	// ChatTypeGroup is a multi-user group / channel.
	ChatTypeGroup ChatType = "group"
	// ChatTypeDirect is a private 1:1 chat with the bot.
	ChatTypeDirect ChatType = "direct"
)

// InboundEvent is the canonical, platform-agnostic envelope every adapter
// emits on its Events() channel. The shape is deliberately platform-neutral —
// adapter-specific quirks (e.g. Feishu's @_user_xxx mention markers) are
// stripped during normalisation so downstream code (intent parsing,
// dispatching, idempotency) never needs to know which platform an event
// originated on.
//
// Field-level rationale (DESIGN §3.1, §4.1):
//
//   - ChannelName is the provider key emitted by the adapter, e.g. "feishu".
//     ChannelConnectionID is injected by the runtime manager when a concrete
//     configured connection receives the event. Downstream code should use
//     ConnectionID() for binding, queueing, and registry lookups.
//   - EventID is the platform's native event id, used as the de-duplication key
//     by the inbound de-dup table (T6). Adapters must NOT generate their own
//     uuid here — re-deliveries from the platform's replay buffer must collide
//     with prior receipts so the dedup table can drop them.
//   - BotMentioned is set by the adapter when the platform indicates the bot
//     was @-mentioned in a group (ignored for direct chats at the filter step).
//   - Text is the user-visible message body with mention markers stripped
//     (e.g. "@_user_xxx hi" → "hi"). Keeping the canonical form here lets the
//     intent parser (T9) match against a clean string.
//   - RawPayload preserves the platform's original JSON for incident
//     debugging. Type is json.RawMessage rather than `any` so we can log or
//     re-marshal it without a re-encoding step (and so a nil payload is
//     trivially distinguishable from an empty object).
type InboundEvent struct {
	ChannelName         string
	ChannelConnectionID string
	EventID             string
	Type                EventType
	ChatID              string
	ChatType            ChatType
	SenderID            string
	SenderName          string
	Text                string
	BotMentioned        bool
	MessageID           string
	RuntimeEventID      string `json:"-"`
	Intent              InboundIntent
	Attachments         []AttachmentInfo
	RawPayload          json.RawMessage

	// QuotedMessageID is the platform message ID the user explicitly quoted
	// (e.g. Feishu quote block). Empty if the user did not quote any message.
	QuotedMessageID string

	// QuotedText is a truncated text summary (~200 runes max) of the quoted
	// message, extracted directly from the quote block so the LLM can use it
	// without a second round-trip to fetch the original message.
	QuotedText string

	// ReplyToMessageID is the platform message ID the user is replying to
	// (e.g. Feishu "reply to message" parent_id). Empty for standalone msgs.
	ReplyToMessageID string

	// ThreadID is the platform thread / topic root message ID.
	// Empty if the message is not inside a thread.
	ThreadID string
}

func (e InboundEvent) ConnectionID() string {
	if e.ChannelConnectionID != "" {
		return e.ChannelConnectionID
	}
	return e.ChannelName
}

// AttachmentInfo carries metadata about a non-text attachment (image, file,
// etc.) that arrived with an inbound message. The adapter layer populates it
// during normalisation; downstream pipeline steps use it to download, upload,
// and persist the attachment.
type AttachmentInfo struct {
	FileKey   string // platform-side file identifier (e.g. Feishu file_key)
	FileName  string // original filename
	FileType  string // "image" | "file"
	MessageID string // platform message_id (required by Feishu download API)
}

// FileDownloader abstracts platform-specific attachment download operations.
// Adapters may expose one per configured connection; channel runtime code
// resolves it through ChannelGateway by connection id.
type FileDownloader interface {
	DownloadImage(ctx context.Context, messageID, fileKey string) ([]byte, error)
	DownloadFile(ctx context.Context, messageID, fileKey string) ([]byte, string, error)
}

// ChatInfo holds metadata about a chat room / channel / thread.
type ChatInfo struct {
	ID   string
	Name string
	Type ChatType
}

// UserInfo holds metadata about a user on the external channel.
type UserInfo struct {
	ID   string
	Name string
}

type OutboundTargetType string

const (
	OutboundTargetChat OutboundTargetType = "chat"
	OutboundTargetUser OutboundTargetType = "user"
)

type OutboundTarget struct {
	Type OutboundTargetType
	ID   string
}

func TargetChat(id string) OutboundTarget {
	return OutboundTarget{Type: OutboundTargetChat, ID: id}
}

func TargetUser(id string) OutboundTarget {
	return OutboundTarget{Type: OutboundTargetUser, ID: id}
}

// OutboundMessage is a plain text message to be sent to the external channel.
// Text is the canonical field name (mirroring InboundEvent.Text) so call sites
// reading and writing share vocabulary.
type OutboundMessage struct {
	Target OutboundTarget
	Text   string
}

// OutboundRichMessage is a platform-neutral rich message to be sent to the
// external channel. The channel layer owns the intent ("title + markdown body"),
// while each adapter owns the platform-specific rendering (Feishu interactive
// card, Slack Block Kit, WeCom markdown/template card, etc.).
type OutboundRichMessage struct {
	Target   OutboundTarget
	Title    string
	Body     string
	Actions  []OutboundAction
	Mentions []OutboundMention
}

type OutboundAction struct {
	Label string
	URL   string
}

type OutboundMention struct {
	Type  OutboundTargetType
	ID    string
	Label string
}

func MentionUser(id, label string) OutboundMention {
	return OutboundMention{Type: OutboundTargetUser, ID: id, Label: label}
}

// OutboundCardMessage is kept as the current public name used throughout the
// outbound pipeline. Its contract is now platform-neutral; Body must be human
// markdown/plain text, not pre-rendered provider JSON.
type OutboundCardMessage = OutboundRichMessage

// SendResult carries the outcome of a Send or SendCard call.
//
// PlatformMessageID is the id assigned by the external platform, persisted by
// the outbound logger (T8) so reactions and edits can be correlated back to
// the originating Multica event.
//
// Retryable signals to the outbound queue whether a transient failure should
// be re-enqueued (true ⇒ network/5xx-class) or surfaced as a permanent error
// (false ⇒ client-side / 4xx). The convention removes the need for callers to
// inspect the underlying error type.
type SendResult struct {
	PlatformMessageID string
	Retryable         bool
}

// Channel is the abstraction over an external messaging platform connection.
// Provider adapters (Feishu, Slack, Discord, …) implement this interface; the
// runtime manager may wrap them so Name() becomes the configured connection id
// used by the registry.
type Channel interface {
	// Name returns the registry identifier for this channel instance.
	Name() string

	// Connect establishes the connection to the external platform. For
	// long-lived transports (WebSocket), Connect kicks off the read loop and
	// returns once the initial handshake completes.
	Connect(ctx context.Context) error

	// Disconnect tears down the connection. After Disconnect returns, the
	// channel returned by Events() must be closed so downstream consumers
	// terminate cleanly.
	Disconnect(ctx context.Context) error

	// Send delivers a plain text message.
	Send(ctx context.Context, msg OutboundMessage) (SendResult, error)

	// SendCard delivers a structured / rich message. Adapters render the
	// platform-neutral message into their native format internally.
	SendCard(ctx context.Context, msg OutboundCardMessage) (SendResult, error)

	// Events returns a receive-only channel of inbound events. The channel
	// must be closed after Disconnect returns.
	Events() <-chan InboundEvent

	// GetChatInfo fetches metadata for a chat room.
	GetChatInfo(ctx context.Context, chatID string) (ChatInfo, error)

	// GetUserInfo fetches metadata for a user.
	GetUserInfo(ctx context.Context, userID string) (UserInfo, error)
}

// ChannelGateway is the provider-neutral access point the channel runtime uses
// after an adapter connection has been registered. Pipeline steps depend on
// this interface rather than directly reaching into an adapter registry.
type ChannelGateway interface {
	SendText(ctx context.Context, connectionID string, msg OutboundMessage) (SendResult, error)
	SendRich(ctx context.Context, connectionID string, msg OutboundRichMessage) (SendResult, error)
	GetChatInfo(ctx context.Context, connectionID, chatID string) (ChatInfo, error)
	GetUserInfo(ctx context.Context, connectionID, userID string) (UserInfo, error)
	FileDownloader(connectionID string) (FileDownloader, bool)
}
