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
	ChannelTypeSlack    = "slack"
	ChannelTypeFeishu   = "feishu"
	ChannelTypeWecom    = "wecom"
	ChannelTypeDingtalk = "dingtalk"
)

// SurfacePersistsTranscript reports whether a chat surface stores its
// conversation in Multica's chat_message table, readable back via `multica chat
// history` (handler/chat_history.go's non-Slack fallback). Web chat (empty
// discriminator), Feishu, WeCom and DingTalk all persist via the shared
// AppendUserMessage path; Slack reads the live channel instead. It is the single
// source of truth for "which surfaces are readable", shared by the
// continuity-notice router, the chat-prompt history copy, and the surface list —
// so a future non-persisting channel is never told "read it back".
func SurfacePersistsTranscript(channelType string) bool {
	switch channelType {
	case "", ChannelTypeFeishu, ChannelTypeWecom, ChannelTypeDingtalk:
		return true
	default:
		return false
	}
}

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

// ChannelCarriesFiles reports whether a file the agent produces will actually
// reach this conversation. It is the delivery half of the two-layer channel
// policy (MUL-4899).
//
// Its one caller is the per-turn chat prompt (daemon.buildChatPrompt). The
// runtime brief must not call it: the answer changes turn to turn on one
// resumed session, and the brief is the prompt-cache prefix (MUL-5377).
//
// serverSaysDelivers is the claim's chat_channel_delivers_files, and it is the
// ONLY thing consulted for a channel-backed chat. The channel type is not, and
// the temptation to answer from it is the defect this signature exists to
// prevent: whether the last hop happens takes an adapter that goes back for the
// bound attachment AND object storage for it to go back to, and the second half
// is a deployment fact no daemon can observe. `case ChannelTypeWecom: return
// true` reads as a statement about the adapter but functions as a statement
// about every deployment running it, including the ones with no storage
// configured, where it promises the agent a delivery the server cannot make.
// Mixed versions land in the same place from the other direction: a daemon new
// enough to know WeCom delivers files, talking to a server too old to perform
// the hop, infers a capability that is not there. Both cases arrive as false
// here, which is the per-turn instruction that says to describe the file in
// words.
//
// Web / mobile chat is not answered here. It has no channel type at all and is
// handled by its own branch, which points at the attachment card the browser
// renders rather than at an IM message.
func ChannelCarriesFiles(channelType string, serverSaysDelivers bool) bool {
	if channelType == "" {
		return false
	}
	return serverSaysDelivers
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
	case ChannelTypeDingtalk:
		return "DingTalk"
	default:
		return channelType
	}
}
