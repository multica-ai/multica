package wechat

import (
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// wechatRawEvent is the adapter-private payload stashed in
// channel.InboundMessage.Raw. The core router never reads Raw; only this
// adapter's resolvers read it back. It carries the fields the resolvers need
// that have no cross-platform home on InboundMessage: the bot id (installation
// routing), the context_token (must be echoed back on the outbound reply — the
// core iLink quirk), the group id, and the raw msg_type (for audit).
type wechatRawEvent struct {
	// IlinkBotID is the bot identity the message was addressed to
	// (to_user_id, e.g. "xxxxxx@im.bot"). The installation resolver matches it
	// against channel_installation.config->>'app_id'.
	IlinkBotID string `json:"ilink_bot_id"`
	// ContextToken MUST be echoed back on the sendmessage reply or the reply is
	// not associated with the conversation. Persisted into the session binding
	// at inbound time and read back at outbound time.
	ContextToken string `json:"context_token,omitempty"`
	// GroupID is non-empty for group messages. Its presence determines
	// ChatTypeGroup vs ChatTypeP2P.
	GroupID string `json:"group_id,omitempty"`
	// MsgType is the raw iLink type string ("text", "image", ...), kept for
	// audit/diagnostics. It is NOT the normalized channel.MsgType.
	MsgType string `json:"msg_type,omitempty"`
}

// inboundFromIlink translates one iLink inbound message into the normalized
// channel.InboundMessage the core router consumes. It is a pure function with no
// platform I/O, so it is unit-testable in isolation.
//
// Routing/dedup keys:
//   - EventID == MessageID == the iLink msg_id (the only stable id; dedup keys
//     on (installation, MessageID) so reconnect redelivery is idempotent).
//
// Source fields:
//   - ChannelType is fixed to TypeWechat.
//   - ChatID is the conversation key: for a p2p chat it is the peer user id; for
//     a group it is the group id. One ChatID maps to one chat_session via the
//     channel_chat_session_binding.
//   - ChatType is Group when GroupID is set, else P2P.
//   - SenderID is the WeChat user id ("xxx@im.wechat"), the key the identity
//     binding is stored under. It is stable within one installation.
//
// Addressing: a p2p message is always addressed to the bot. For groups, MVP
// ingests every group message (addressed=true); a future phase can narrow this
// to @-bot detection once the iLink mention shape is confirmed (Phase 6).
func inboundFromIlink(m iLinkMessage) channel.InboundMessage {
	chatType := channel.ChatTypeP2P
	chatID := m.FromUserID
	addressed := true
	if m.GroupID != "" {
		chatType = channel.ChatTypeGroup
		chatID = m.GroupID
	}

	msgType := channel.MsgTypeText
	if m.MsgType != "" && m.MsgType != "text" {
		// Non-text types are mapped to Unknown for the MVP; the core treats them
		// as non-actionable (no media pipeline yet). The raw type is preserved
		// in Raw for diagnostics.
		msgType = channel.MsgTypeUnknown
	}

	raw, _ := json.Marshal(wechatRawEvent{
		IlinkBotID:   m.ToUserID,
		ContextToken: m.ContextToken,
		GroupID:      m.GroupID,
		MsgType:      m.MsgType,
	})

	return channel.InboundMessage{
		EventID:   m.MsgID,
		MessageID: m.MsgID,
		Source: channel.Source{
			ChannelType: TypeWechat,
			ChatID:      chatID,
			ChatType:    chatType,
			SenderID:    m.FromUserID,
		},
		Type:            msgType,
		Text:            m.Content,
		AddressedToBot:  addressed,
		Raw:             raw,
	}
}

// decodeWechatRaw reads the adapter-private payload back out of an
// InboundMessage.Raw. A decode miss yields a zero-value struct (the resolvers
// treat a missing bot id as "no installation match").
func decodeWechatRaw(msg channel.InboundMessage) wechatRawEvent {
	var raw wechatRawEvent
	_ = json.Unmarshal(msg.Raw, &raw)
	return raw
}
