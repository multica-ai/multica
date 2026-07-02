package dingtalk

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file holds the translation from a DingTalk Stream callback
// (botCallbackData) to the engine's normalized channel.InboundMessage. The
// per-installation connection (dingtalk_channel.go) threads in its OWN
// installation's AppKey so the resolver can route the event back to its
// installation — DingTalk's callback payload does not carry the robot code
// itself.

// botCallbackData is the DingTalk bot-message callback payload — the JSON carried
// in a CALLBACK frame's data field. It holds only the fields the translation
// reads; DingTalk sends more, which we ignore. Replaces the vendor SDK's
// chatbot.BotCallbackDataModel.
type botCallbackData struct {
	ConversationId   string          `json:"conversationId"`
	ConversationType string          `json:"conversationType"`
	SenderStaffId    string          `json:"senderStaffId"`
	ChatbotCorpId    string          `json:"chatbotCorpId"`
	MsgId            string          `json:"msgId"`
	Msgtype          string          `json:"msgtype"`
	IsInAtList       bool            `json:"isInAtList"`
	Text             botCallbackText `json:"text"`
}

type botCallbackText struct {
	Content string `json:"content"`
}

// dingtalkRawEvent carries the DingTalk-specific fields the cross-platform
// envelope does not. AppID is stamped by the receiving connection (it is the
// installation's routing key); the rest are read back only inside the resolvers.
type dingtalkRawEvent struct {
	AppID            string `json:"app_id"`
	ConversationType string `json:"conversation_type,omitempty"`
	CorpID           string `json:"corp_id,omitempty"`
}

// conversation type discriminators DingTalk sends in conversationType.
const (
	convTypeP2P   = "1"
	convTypeGroup = "2"
)

// inboundFromCallback normalizes a DingTalk bot callback. It returns ok=false
// for events that must not reach the core: non-text messages and messages with
// no sender staff id (system / bot-authored). A direct (1:1) message is always
// addressed to the bot; a group message reaches the bot only when it carries an
// @-mention of it, which DingTalk reports via isInAtList.
func inboundFromCallback(data *botCallbackData, appID string) (channel.InboundMessage, bool) {
	if data == nil {
		return channel.InboundMessage{}, false
	}
	if data.Msgtype != "text" {
		return channel.InboundMessage{}, false
	}
	if data.SenderStaffId == "" {
		return channel.InboundMessage{}, false
	}

	chatType := dingtalkChatType(data.ConversationType)
	addressed := chatType == channel.ChatTypeP2P || data.IsInAtList

	raw, _ := json.Marshal(dingtalkRawEvent{
		AppID:            appID,
		ConversationType: data.ConversationType,
		CorpID:           data.ChatbotCorpId,
	})

	// Normalize the platform-specific /new affordance into the core's ForceFresh
	// flag, stripping the directive so the agent sees only the user's prompt.
	// DingTalk text is never enriched, so the stripped body IS the user's own
	// text: an empty remainder means a lone "/new" (a bare reset).
	text := strings.TrimSpace(data.Text.Content)
	forceFresh := false
	bareFresh := false
	if cmd, ok := parseFreshSessionCommand(text); ok {
		forceFresh = true
		text = cmd.Body
		bareFresh = strings.TrimSpace(text) == ""
	}

	return channel.InboundMessage{
		EventID:        data.MsgId,
		MessageID:      data.MsgId,
		Type:           channel.MsgTypeText,
		Text:           text,
		AddressedToBot: addressed,
		ForceFresh:     forceFresh,
		BareFresh:      bareFresh,
		Source: channel.Source{
			ChannelType: TypeDingTalk,
			ChatID:      data.ConversationId,
			ChatType:    chatType,
			SenderID:    data.SenderStaffId,
		},
		Raw: raw,
	}, true
}

// dingtalkChatType maps DingTalk's conversationType to the normalized ChatType.
// "1" is a 1:1 direct chat; everything else (group "2") is a group, which routes
// through the engine's "must address the bot" filter.
func dingtalkChatType(conversationType string) channel.ChatType {
	if conversationType == convTypeP2P {
		return channel.ChatTypeP2P
	}
	return channel.ChatTypeGroup
}
