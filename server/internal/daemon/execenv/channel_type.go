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
// agree on the wire strings below.
const (
	ChannelTypeSlack  = "slack"
	ChannelTypeFeishu = "feishu"
)

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
	default:
		return channelType
	}
}
