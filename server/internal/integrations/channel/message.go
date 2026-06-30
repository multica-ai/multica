package channel

import "encoding/json"

// ChatType discriminates a 1:1 direct conversation with the bot from a
// multi-party group chat. Product behavior differs: direct chats ingest
// every message; group chats only ingest messages explicitly addressed
// to the bot (@-mention or reply to a bot message). The wire values match
// the existing lark_chat_session_binding.lark_chat_type constraint so the
// generalized channel_* table backfills 1:1.
type ChatType string

const (
	// ChatTypeP2P is a direct (peer-to-peer) conversation with the bot.
	ChatTypeP2P ChatType = "p2p"
	// ChatTypeGroup is a multi-party group conversation.
	ChatTypeGroup ChatType = "group"
)

// MsgType is the normalized, cross-platform message kind. Adapters map
// their platform's native type onto this small closed set; the platform's
// raw type string (Lark "post" / "merge_forward" / "interactive", …) is
// NOT represented here — it stays in InboundMessage.Raw and is read only
// by the adapter. The core only ever needs to know "text vs media, and
// which media".
type MsgType string

const (
	// MsgTypeText is a plain or rich text message. The human-readable
	// content is flattened into InboundMessage.Text by the adapter.
	MsgTypeText MsgType = "text"
	// MsgTypeImage is an image attachment.
	MsgTypeImage MsgType = "image"
	// MsgTypeFile is a generic file attachment.
	MsgTypeFile MsgType = "file"
	// MsgTypeAudio is a voice / audio attachment.
	MsgTypeAudio MsgType = "audio"
	// MsgTypeVideo is a video attachment.
	MsgTypeVideo MsgType = "video"
	// MsgTypeUnknown is the fallback for a platform type the adapter does
	// not map. The core treats it as a non-text, non-actionable message.
	MsgTypeUnknown MsgType = "unknown"
)

// Source carries the cross-platform routing identity of an inbound
// message — every field here is true on every platform. Platform-specific
// routing keys (a Lark app_id, a Slack team id) are resolved to an
// installation by the adapter and do NOT appear on Source.
type Source struct {
	// ChannelType is the platform the message arrived on; it equals the
	// owning Channel's Type.
	ChannelType Type

	// ChatID is the platform conversation identifier. One ChatID maps to
	// one Multica chat_session via the channel_chat_session_binding.
	ChatID string

	// ChatType discriminates direct from group conversations.
	ChatType ChatType

	// SenderID is the platform-native, per-installation user identifier
	// (Lark open_id, Slack user id, …). It is stable WITHIN one
	// installation and is the key the identity binding is stored under.
	// It is NOT comparable across installations.
	SenderID string

	// SenderStableID is the platform's cross-installation stable identity
	// for the sender when one exists (Lark union_id, …), otherwise empty.
	// Captured opportunistically for future cross-installation identity
	// merging; the core treats an empty value as "not available".
	SenderStableID string

	// ThreadID is the platform thread / topic the message belongs to,
	// when threading applies and the message is inside a thread. Empty
	// means a top-level conversation message. The core persists it so a
	// decoupled outbound reply can be threaded back into the same topic.
	ThreadID string
}

// MediaRef references a media attachment that the adapter has ALREADY
// persisted to object storage before the message reaches the core. The
// core never holds raw bytes — only this reference — so the envelope
// stays small and platform-neutral.
type MediaRef struct {
	// Type is the normalized media kind (image / file / audio / video).
	Type MsgType
	// StorageKey locates the persisted object in Multica object storage.
	StorageKey string
	// Filename is the original display name, when the platform supplies
	// one.
	Filename string
	// MimeType is the content type, when known.
	MimeType string
	// SizeBytes is the object size in bytes, or 0 when unknown.
	SizeBytes int64
}

// ReplyCtx describes the message an inbound message quotes / replies to.
// It is nil when the inbound message is not a reply.
type ReplyCtx struct {
	// MessageID is the immediate parent message's platform id (the
	// message being quoted).
	MessageID string
	// RootID is the thread/root anchor the platform reports, when any.
	RootID string
}

// InboundMessage is the single normalized shape the core consumes. Every
// adapter translates its platform's raw payload into this struct; the
// core's router, dedup, identity check, and persistence read ONLY these
// fields. Per the boundary rule (MUL-3515 §2) the struct holds only
// cross-platform-true fields; everything platform-specific lives in Raw.
type InboundMessage struct {
	// EventID is the platform's delivery/event identifier and MessageID
	// is the platform's message identifier. Together they back the
	// idempotency layer: a platform may redeliver the same event on
	// reconnect, and dedup keys on (installation, MessageID).
	EventID   string
	MessageID string

	// Source is the routing identity (chat, sender, thread).
	Source Source

	// Type is the normalized message kind.
	Type MsgType

	// Text is the human-readable content, flattened by the adapter. For
	// non-text messages it may be empty or a short placeholder; the media
	// itself is in MediaRefs.
	Text string

	// MediaRefs are the attachments, already persisted to object storage.
	MediaRefs []MediaRef

	// ReplyTo is the quoted/replied-to context, or nil.
	ReplyTo *ReplyCtx

	// AddressedToBot is the adapter's normalized verdict on whether a
	// GROUP message is an interaction with the bot (@-mention or reply to
	// a bot message). It is meaningless for direct (p2p) chats and the
	// core ignores it there. It is a normalized boolean, not platform
	// data — the platform-specific signals it was derived from (mention
	// arrays, parent ids) stay in Raw.
	AddressedToBot bool

	// ForceFresh asks the core to start a fresh agent session for this
	// message instead of resuming the prior one (the platform's "/fresh"
	// affordance). The adapter normalizes its platform-specific trigger
	// into this boolean; the core only reads the flag.
	ForceFresh bool

	// Raw is the untouched platform payload. Adapters stash platform-
	// specific fields here (Lark raw msg_type / parent_id / root_id /
	// mention arrays, …) and read them back only inside the adapter. The
	// core never reads Raw — that is the whole point of the boundary.
	Raw json.RawMessage
}

// OutboundMessage is the minimal outbound reply the core can ask any
// Channel to deliver: a text body into a chat, optionally threaded or
// quoting a specific message. Rich cards, media uploads, and outbound
// webhooks are deliberately NOT modeled here (MUL-3515 decision §6) — an
// adapter that supports richer output exposes it on its own type, not on
// this cross-platform envelope.
type OutboundMessage struct {
	// ChatID is the destination conversation (the platform chat id).
	ChatID string
	// Text is the message body.
	Text string
	// ThreadID, when set, threads the reply into the given platform
	// thread / topic. Empty sends at the chat level.
	ThreadID string
	// ReplyTo, when set, quote-replies to the given platform message id.
	ReplyTo string
}

// SendResult is the outcome of Channel.Send.
type SendResult struct {
	// MessageID is the platform's identifier for the delivered message.
	MessageID string
}
