package lark

import (
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TaskSource returns the Feishu ids of the message a chat task answers. It
// reads only the task's immutable delivery, so a retry or a run debounced
// behind newer messages is handed the ids of its own trigger, and it makes no
// platform call. Ids the delivery did not record stay empty.
func TaskSource(delivery db.ChannelTaskDelivery) *protocol.FeishuSource {
	if delivery.ChannelType != channelTypeFeishu {
		return nil
	}
	chatID := outboundChatID(ChatSessionBinding{ChannelChatID: delivery.ChannelChatID, Config: delivery.Config})
	return protocol.SafeFeishuSource(&protocol.FeishuSource{
		ChatID:       string(chatID),
		ChatType:     delivery.ChatType,
		MessageID:    delivery.ChannelMessageID.String,
		ThreadID:     delivery.ChannelThreadID.String,
		SenderOpenID: delivery.ChannelSenderID.String,
	})
}
