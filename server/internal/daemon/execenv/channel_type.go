package execenv

// Chat channel discriminators as they arrive on the task payload. The server
// stamps `chat_channel_type` from the channel_chat_session_binding row
// (handler/daemon.go); an empty value means a web/mobile chat session with no
// IM channel behind it.
//
// These are plain string constants on purpose: the daemon compares a value the
// server already serialized to JSON, and must not pull the server-side
// integration packages (integrations/slack, integrations/lark) into its own
// build just to read one discriminator. The canonical definitions live with
// their adapters — slack.TypeSlack, channel.TypeFeishu, wecom.TypeWecom — and
// both sides agree on the wire strings below.
const (
	ChannelTypeSlack  = "slack"
	ChannelTypeFeishu = "feishu"
	ChannelTypeWecom  = "wecom"
)

// Room-shape discriminators, mirroring channel_chat_session_binding.chat_type
// (channel.ChatTypeP2P / channel.ChatTypeGroup). Every adapter persists this
// column, so the shape of a conversation is known off one read whatever the
// platform. Empty means the server did not report one — a web chat, which has
// no binding row, or a server predating the field.
const (
	ChatTypeP2P   = "p2p"
	ChatTypeGroup = "group"
)

// ChatAudience is what a run is allowed to say about who can read its replies.
// Three states, because "unknown" is not "private": a web chat carries no
// binding row at all and is 1:1 by construction, but an IM channel whose shape
// the server did not report could be a room of any size, and the one thing the
// copy must not then do is promise a privacy the conversation may not have.
//
// Both the brief's chat-mode line and the per-turn chat prompt open by naming
// the audience, and they must not contradict each other or `## Conversation
// Channel`. Routing all three through this keeps them from drifting.
type ChatAudience int

const (
	// ChatAudienceDirect — one reader, provably: an explicit p2p binding, or a
	// web chat that has no channel behind it.
	ChatAudienceDirect ChatAudience = iota
	// ChatAudienceGroup — a room shared by people the run has not been shown.
	ChatAudienceGroup
	// ChatAudienceUnknown — an IM channel whose shape did not arrive: a daemon
	// newer than the server it claims from, or a binding deleted between
	// enqueue and claim. Assert nothing about the audience.
	ChatAudienceUnknown
)

// AudienceOf classifies a claim's (chat_channel_type, chat_type) pair.
func AudienceOf(channelType, chatType string) ChatAudience {
	switch {
	case chatType == ChatTypeGroup:
		return ChatAudienceGroup
	case chatType == ChatTypeP2P:
		return ChatAudienceDirect
	case channelType == "":
		return ChatAudienceDirect
	default:
		return ChatAudienceUnknown
	}
}

// ChannelDisplayName renders a chat_channel_type for prompt / brief copy.
// Unknown types fall through to the raw discriminator rather than a generic
// placeholder, so a channel added server-side without a mapping here still
// names itself in the prompt instead of silently reading as "unknown".
func ChannelDisplayName(channelType string) string {
	switch channelType {
	case ChannelTypeSlack:
		return "Slack"
	case ChannelTypeFeishu:
		return "Feishu/Lark"
	case ChannelTypeWecom:
		return "WeCom"
	default:
		return channelType
	}
}
