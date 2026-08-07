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
// their adapters — slack.TypeSlack and channel.TypeFeishu — and both sides
// agree on the wire strings below. WeCom keeps its reserved wire discriminator
// here until its adapter lands.
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
// The per-turn chat prompt names the audience once. Keeping classification in
// one function prevents group, direct, and compatibility paths from drifting.
type ChatAudience int

const (
	// ChatAudienceUnknown is deliberately the zero value: uninitialized room
	// context must never turn into a privacy claim.
	ChatAudienceUnknown ChatAudience = iota
	// ChatAudienceDirect — an explicit p2p binding, or the no-channel shape used
	// by web chat. The claim protocol cannot distinguish a deleted binding here.
	ChatAudienceDirect
	// ChatAudienceGroup — a room shared by people the run has not been shown.
	ChatAudienceGroup
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

// ChannelCarriesFiles reports whether an adapter can put a file the agent
// produced into the conversation. It is the delivery half of the two-layer
// channel policy (MUL-4899), and it is a per-adapter capability rather than a
// property of having a channel: `multica attachment upload` binds the file to
// the Multica chat reply on every surface, and whether that reaches the reader
// depends on whether the adapter goes back for it. WeCom does — it reads the
// bound attachment out of object storage and sends it into the chat behind the
// answer (integrations/wecom/outbound_media.go). Slack and Lark do not, so
// their briefs still say to describe the file in words.
//
// Every channel type this package knows is named below rather than left to the
// default, so adding a fourth adapter surfaces here as a decision to make. An
// unrecognized type — a channel the server added that this daemon build has no
// constant for — answers false, which is the safe direction: the agent is told
// to describe its file in words, and the worst case is a file that could have
// been delivered was not. False the other way would have the agent write "see
// the attached chart" into a conversation that never receives one.
//
// Web / mobile chat is not answered here. It has no channel type at all and is
// handled by its own branch, which points at the attachment card the browser
// renders rather than at an IM message.
func ChannelCarriesFiles(channelType string) bool {
	switch channelType {
	case ChannelTypeWecom:
		return true
	case ChannelTypeSlack, ChannelTypeFeishu:
		return false
	default:
		return false
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
