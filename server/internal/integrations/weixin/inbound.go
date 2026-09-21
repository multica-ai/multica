package weixin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// InboundMetadata is the adapter-private portion of an inbound envelope. The
// rotating context token is intentionally absent from normalized Source: it is
// not cross-platform routing state. A future session binder will encrypt and
// atomically persist this Raw value with the durable inbound message.
type InboundMetadata struct {
	BotID        string `json:"ilink_bot_id"`
	ContextToken string `json:"context_token,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	Seq          int64  `json:"seq,omitempty"`
}

// DecodeInboundMetadata is the adapter-side inverse of the Raw encoding. It is
// exported for the future session and outbound components; the shared Router
// must continue treating Raw as opaque.
func DecodeInboundMetadata(msg channel.InboundMessage) (InboundMetadata, error) {
	if len(msg.Raw) == 0 {
		return InboundMetadata{}, fmt.Errorf("weixin: inbound Raw is empty")
	}
	var out InboundMetadata
	if err := json.Unmarshal(msg.Raw, &out); err != nil {
		return InboundMetadata{}, fmt.Errorf("weixin: decode inbound Raw: %w", err)
	}
	return out, nil
}

// NormalizeInbound converts one completed, user-authored iLink direct message
// into the channel engine's normalized envelope. It deliberately rejects bot
// echoes, groups, generating messages, missing identities, and media-only
// messages. Tencent's current reference channel declares direct chat only; a
// group_id must not be silently treated as supported group chat.
func NormalizeInbound(msg Message, installationBotID string) (channel.InboundMessage, bool) {
	if msg.MessageType != messageTypeUser {
		return channel.InboundMessage{}, false
	}
	if msg.MessageState != 0 && msg.MessageState != messageStateFinish {
		return channel.InboundMessage{}, false
	}
	if strings.TrimSpace(msg.GroupID) != "" {
		return channel.InboundMessage{}, false
	}
	senderID := strings.TrimSpace(msg.FromUserID)
	botID := strings.TrimSpace(msg.ToUserID)
	if botID == "" {
		botID = strings.TrimSpace(installationBotID)
	}
	if senderID == "" || botID == "" {
		return channel.InboundMessage{}, false
	}

	text := messageText(msg.Items)
	if text == "" {
		return channel.InboundMessage{}, false
	}
	messageID := stableMessageID(msg)
	if messageID == "" {
		return channel.InboundMessage{}, false
	}

	raw, err := json.Marshal(InboundMetadata{
		BotID:        botID,
		ContextToken: msg.ContextToken,
		SessionID:    msg.SessionID,
		Seq:          msg.Seq,
	})
	if err != nil {
		return channel.InboundMessage{}, false
	}

	return channel.InboundMessage{
		EventID:   messageID,
		MessageID: messageID,
		Source: channel.Source{
			ChannelType: TypeWeixin,
			ChatID:      senderID,
			ChatType:    channel.ChatTypeP2P,
			SenderID:    senderID,
		},
		Type:           channel.MsgTypeText,
		Text:           text,
		CommandText:    text,
		AddressedToBot: true,
		Raw:            raw,
	}, true
}

func messageText(items []MessageItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		switch item.Type {
		case messageItemTypeText:
			if item.Text != nil && item.Text.Text != "" {
				parts = append(parts, item.Text.Text)
			}
		case messageItemTypeVoice:
			// Server-provided voice transcription is already text. The media
			// bytes remain unsupported until the AES/SILK pipeline is hardened.
			if item.Voice != nil && item.Voice.Text != "" {
				parts = append(parts, item.Voice.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// stableMessageID prefers upstream identifiers and derives a deterministic
// fallback only when they are absent. Random ids are forbidden because a
// reconnect replay must hit channel_inbound_message_dedup.
func stableMessageID(msg Message) string {
	if id := rawMessageID(msg.MessageID); id != "" && id != "0" {
		return id
	}
	for _, item := range msg.Items {
		if id := strings.TrimSpace(item.MsgID); id != "" {
			return id
		}
	}

	h := sha256.New()
	fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s",
		msg.Seq,
		msg.ClientID,
		msg.FromUserID,
		msg.ToUserID,
		msg.CreateTimeMS,
		msg.SessionID,
		msg.ContextToken,
	)
	for _, item := range msg.Items {
		fmt.Fprintf(h, "\x00%d\x00%s", item.Type, item.MsgID)
		if item.Text != nil {
			fmt.Fprintf(h, "\x00%s", item.Text.Text)
		}
		if item.Voice != nil {
			fmt.Fprintf(h, "\x00%s", item.Voice.Text)
		}
	}
	sum := h.Sum(nil)
	return "derived-" + hex.EncodeToString(sum[:16])
}

func rawMessageID(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		if _, err := strconv.ParseInt(number.String(), 10, 64); err == nil {
			return number.String()
		}
	}
	return ""
}
