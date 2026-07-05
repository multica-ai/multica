package dingtalk

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file holds the translation from a DingTalk bot-message callback
// (the /v1.0/im/bot/messages/get payload, SDK chatbot.BotCallbackDataModel)
// to the engine's normalized channel.InboundMessage.

// botCallbackData is the bot-message callback payload. Field set matches
// the official SDK's BotCallbackDataModel; unknown fields are ignored.
type botCallbackData struct {
	ConversationID string `json:"conversationId"`
	AtUsers        []struct {
		DingtalkID string `json:"dingtalkId"`
		StaffID    string `json:"staffId"`
	} `json:"atUsers"`
	ChatbotUserID             string `json:"chatbotUserId"`
	MsgID                     string `json:"msgId"`
	SenderNick                string `json:"senderNick"`
	SenderStaffID             string `json:"senderStaffId"`
	SessionWebhook            string `json:"sessionWebhook"`
	SessionWebhookExpiredTime int64  `json:"sessionWebhookExpiredTime"`
	CreateAt                  int64  `json:"createAt"`
	SenderCorpID              string `json:"senderCorpId"`
	ConversationType          string `json:"conversationType"` // "1" = DM, "2" = group
	SenderID                  string `json:"senderId"`
	ConversationTitle         string `json:"conversationTitle"`
	Msgtype                   string `json:"msgtype"`
	Text                      struct {
		Content string `json:"content"`
	} `json:"text"`
	// Content carries the non-plain-text payloads. Only richText is
	// flattened today (see flattenRichText); other media remain a
	// follow-up.
	Content richTextContent `json:"content"`
}

// richTextContent is the content envelope of a richText callback.
type richTextContent struct {
	RichText []richTextNode `json:"richText"`
}

// richTextNode is one node of the richText list: a text run, or a media
// node discriminated by `type` (text runs carry no type).
type richTextNode struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

// msgtypeRichText is the callback msgtype for formatted messages. A
// quote-reply, a copy-paste that keeps formatting, or an image+text mix
// all arrive as richText — with text.content EMPTY and the real content
// in content.richText.
const msgtypeRichText = "richText"

// flattenRichText renders a richText callback's node list to plain text.
// Text runs are concatenated verbatim (DingTalk encodes line breaks
// inside the runs); picture nodes degrade to the bracketed placeholder
// the lark flattener uses, so the agent sees something was attached
// without us downloading the binary.
func flattenRichText(data botCallbackData) string {
	var b strings.Builder
	for _, node := range data.Content.RichText {
		switch {
		case node.Text != "":
			b.WriteString(node.Text)
		case node.Type == "picture":
			b.WriteString("[Image]")
		}
	}
	return strings.TrimSpace(b.String())
}

// dingtalkRawEvent carries the DingTalk-specific fields the cross-platform
// envelope does not — read back only inside the dingtalk resolvers/replier
// (the core never reads Raw).
type dingtalkRawEvent struct {
	// ClientID is the installation routing key: the app the connection
	// belongs to. Stamped by the channel (which knows its own identity)
	// rather than read from the payload, mirroring how each Slack
	// connection only ever delivers its own app's events.
	ClientID string `json:"client_id"`
	// SessionWebhook is the per-message reply webhook (valid ~90 min);
	// the OutboundReplier posts verdict replies through it with no
	// access token or API permission needed.
	SessionWebhook            string `json:"session_webhook,omitempty"`
	SessionWebhookExpiredTime int64  `json:"session_webhook_expired_time,omitempty"`
	SenderStaffID             string `json:"sender_staff_id,omitempty"`
	SenderNick                string `json:"sender_nick,omitempty"`
	ConversationTitle         string `json:"conversation_title,omitempty"`
	Msgtype                   string `json:"msgtype,omitempty"`
	// CreateAt is the callback's epoch-millisecond send time; the typing
	// indicator uses it to skip stale redeliveries after a reconnect.
	CreateAt int64 `json:"create_at,omitempty"`
}

// inboundFromBotCallback normalizes one bot-message callback. ok=false
// drops payloads that must not reach the core (no message id — nothing
// to dedup on).
func inboundFromBotCallback(data botCallbackData, clientID string) (channel.InboundMessage, bool) {
	if data.MsgID == "" {
		return channel.InboundMessage{}, false
	}
	chatType := channel.ChatTypeGroup
	if data.ConversationType == "1" {
		chatType = channel.ChatTypeP2P
	}
	// SenderStaffID is the org-scoped stable user id; SenderID (the
	// encrypted dingtalkId) is the fallback for senders outside the
	// app's org, where staffId is absent.
	senderID := data.SenderStaffID
	if senderID == "" {
		senderID = data.SenderID
	}
	raw, _ := json.Marshal(dingtalkRawEvent{
		ClientID:                  clientID,
		SessionWebhook:            data.SessionWebhook,
		SessionWebhookExpiredTime: data.SessionWebhookExpiredTime,
		SenderStaffID:             data.SenderStaffID,
		SenderNick:                data.SenderNick,
		ConversationTitle:         data.ConversationTitle,
		Msgtype:                   data.Msgtype,
		CreateAt:                  data.CreateAt,
	})
	msgType := channel.MsgTypeText
	text := strings.TrimSpace(data.Text.Content)
	switch {
	case data.Msgtype == "text" || data.Msgtype == "":
		// Plain text — already extracted above.
	case data.Msgtype == msgtypeRichText:
		// Formatted messages carry their content in content.richText and
		// leave text.content empty; flatten so they don't ingest as empty
		// messages (which read to the agent as "your message is blank").
		text = flattenRichText(data)
		if text == "" {
			msgType = channel.MsgTypeUnknown
		}
	default:
		// picture / audio / video / file — media ingestion is a follow-up;
		// the core treats unknown as non-actionable but the message still
		// lands in the session for context.
		msgType = channel.MsgTypeUnknown
	}
	// /new on the first non-empty line forces a fresh agent session for
	// this dispatch (mirrors the Lark enricher): the directive is stripped
	// and the remainder is the prompt.
	forceFresh := false
	if stripped, ok := parseFreshSessionCommand(text); ok {
		text = stripped
		forceFresh = true
	}
	return channel.InboundMessage{
		EventID:    data.MsgID,
		MessageID:  data.MsgID,
		Type:       msgType,
		Text:       text,
		ForceFresh: forceFresh,
		// DingTalk only delivers group messages that @-mention the robot,
		// so every callback is, by construction, addressed to the bot.
		AddressedToBot: true,
		Source: channel.Source{
			ChannelType: TypeDingtalk,
			ChatID:      data.ConversationID,
			ChatType:    chatType,
			SenderID:    senderID,
		},
		Raw: raw,
	}, true
}

func decodeDingTalkRaw(msg channel.InboundMessage) (dingtalkRawEvent, error) {
	var raw dingtalkRawEvent
	if len(msg.Raw) == 0 {
		return dingtalkRawEvent{}, errEmptyRaw
	}
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		return dingtalkRawEvent{}, err
	}
	return raw, nil
}
