package dingtalk

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
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
	ConversationId    string              `json:"conversationId"`
	ConversationTitle string              `json:"conversationTitle"`
	ConversationType  string              `json:"conversationType"`
	AtUsers           []botCallbackAtUser `json:"atUsers"`
	ChatbotUserId     string              `json:"chatbotUserId"`
	SenderStaffId     string              `json:"senderStaffId"`
	MsgId             string              `json:"msgId"`
	Msgtype           string              `json:"msgtype"`
	IsInAtList        bool                `json:"isInAtList"`
	Text              botCallbackText     `json:"text"`
	// Content is the msgtype-discriminated payload of non-text messages
	// (picture / richText). Decoded lazily per msgtype; absent on over-quota
	// callbacks (errorCode 20001 strips text/content entirely).
	Content json.RawMessage `json:"content"`
}

type botCallbackAtUser struct {
	DingtalkId string `json:"dingtalkId"`
	StaffId    string `json:"staffId"`
}

type botCallbackText struct {
	Content string `json:"content"`
}

// pictureContent is the content shape of msgtype=picture. Real payloads carry
// both codes (developerpedia); either resolves through messageFiles/download.
type pictureContent struct {
	DownloadCode        string `json:"downloadCode"`
	PictureDownloadCode string `json:"pictureDownloadCode"`
}

// richTextContent is the content shape of msgtype=richText: an ORDERED array
// of heterogeneous items — text runs {"text":…} interleaved with picture items
// {"type":"picture","downloadCode":…} in send order. Item kinds beyond
// text/picture are undocumented today and skipped.
type richTextContent struct {
	RichText []richTextItem `json:"richText"`
}

type richTextItem struct {
	Text                string `json:"text"`
	Type                string `json:"type"`
	DownloadCode        string `json:"downloadCode"`
	PictureDownloadCode string `json:"pictureDownloadCode"`
}

// refAlt orders a picture item's two download codes into (primary, fallback),
// promoting the secondary code when the primary is missing.
func refAlt(downloadCode, pictureDownloadCode string) (ref, alt string) {
	if downloadCode != "" {
		return downloadCode, pictureDownloadCode
	}
	return pictureDownloadCode, ""
}

// dingtalkRawEvent carries the DingTalk-specific fields the cross-platform
// envelope does not. AppID is stamped by the receiving connection (it is the
// installation's routing key) and read back only inside the resolvers.
type dingtalkRawEvent struct {
	AppID             string                  `json:"app_id"`
	ConversationTitle string                  `json:"conversation_title,omitempty"`
	Media             []dingtalkMediaResource `json:"media,omitempty"`
}

type dingtalkMediaResource struct {
	Ref string `json:"ref"`
	Alt string `json:"alt,omitempty"`
	// InlineIndex is the occurrence of the adapter-generated marker in the
	// visible body, including identical user-authored text.
	InlineIndex int `json:"inline_index,omitempty"`
}

func dingtalkMediaResourceAt(ref, alt string, inlineIndex int) dingtalkMediaResource {
	return dingtalkMediaResource{Ref: ref, Alt: alt, InlineIndex: inlineIndex}
}

// conversation type discriminators DingTalk sends in conversationType.
const (
	convTypeP2P              = "1"
	convTypeGroup            = "2"
	dingtalkImagePlaceholder = "[Image]"
)

// inboundFromCallback normalizes a DingTalk bot callback. It returns ok=false
// only for events that must not reach the core at all: messages with no sender
// staff id (system / bot-authored). Text, picture and richText become
// ingestable messages; a malformed/over-quota media payload (the 20001 shape
// strips content) still reaches the core as an explicit unavailable-image
// placeholder rather than the adapter dropping it silently;
// audio/video/file/unknown kinds likewise pass through as text placeholders.
// A direct (1:1) message is always addressed to the bot; a group
// message reaches the bot only when it carries an @-mention of it, which
// DingTalk reports via isInAtList.
func inboundFromCallback(data *botCallbackData, appID string) (channel.InboundMessage, bool) {
	return inboundFromCallbackWithBotName(data, appID, "")
}

// inboundFromCallbackWithBotName translates one callback using only a Bot name
// verified for this installation through DingTalk's group Bot list API. An
// empty name is deliberately fail-closed: the adapter preserves every visible
// mention rather than guessing its span from whitespace.
func inboundFromCallbackWithBotName(data *botCallbackData, appID, botName string) (channel.InboundMessage, bool) {
	if data == nil {
		return channel.InboundMessage{}, false
	}
	if data.SenderStaffId == "" {
		return channel.InboundMessage{}, false
	}

	chatType := dingtalkChatType(data.ConversationType)
	rawEvent := dingtalkRawEvent{
		AppID:             appID,
		ConversationTitle: strings.TrimSpace(data.ConversationTitle),
	}
	msg := channel.InboundMessage{
		EventID:        data.MsgId,
		MessageID:      data.MsgId,
		AddressedToBot: chatType == channel.ChatTypeP2P || data.IsInAtList,
		Source: channel.Source{
			ChannelType: TypeDingTalk,
			ChatID:      data.ConversationId,
			ChatType:    chatType,
			SenderID:    data.SenderStaffId,
		},
	}

	switch data.Msgtype {
	case "text":
		msg.Type = channel.MsgTypeText
		msg.Text = strings.TrimSpace(normalizeDingTalkBotMention(data, data.Text.Content, botName))
		msg.CommandText = msg.Text
		return withDingTalkRaw(msg, rawEvent), true

	case "picture":
		var pc pictureContent
		if len(data.Content) == 0 || json.Unmarshal(data.Content, &pc) != nil {
			// Over-quota (errorCode 20001 strips content) or malformed payload:
			// the sender is a real user who sent an image the bot cannot read.
			// Route it into the engine so it gets identity-gated feedback.
			return mediaUnreadableMsg(msg, rawEvent), true
		}
		ref, alt := refAlt(pc.DownloadCode, pc.PictureDownloadCode)
		if ref == "" {
			return mediaUnreadableMsg(msg, rawEvent), true
		}
		msg.Type = channel.MsgTypeImage
		msg.Text = dingtalkImagePlaceholder
		msg.CommandText = msg.Text
		rawEvent.Media = []dingtalkMediaResource{dingtalkMediaResourceAt(ref, alt, 0)}
		return withDingTalkRaw(msg, rawEvent), true

	case "richText":
		var rc richTextContent
		if len(data.Content) == 0 || json.Unmarshal(data.Content, &rc) != nil {
			// Over-quota / malformed richText: surface it to the engine for
			// identity-gated feedback rather than a silent adapter drop.
			return mediaUnreadableMsg(msg, rawEvent), true
		}
		normalizeDingTalkRichTextBotMention(data, rc.RichText, botName)
		var (
			text                   strings.Builder
			commandText            strings.Builder
			inlinePlaceholderCount int
		)
		for _, item := range rc.RichText {
			// A single item may in principle carry BOTH a text run and a picture
			// code; handle each independently (not a switch) so neither is
			// silently dropped. Text first, then image, matching send order.
			// Items with neither (undocumented kinds) contribute nothing.
			if item.Text != "" {
				text.WriteString(item.Text)
				commandText.WriteString(item.Text)
				inlinePlaceholderCount += strings.Count(item.Text, dingtalkImagePlaceholder)
			}
			if item.Type == "picture" || item.DownloadCode != "" || item.PictureDownloadCode != "" {
				ref, alt := refAlt(item.DownloadCode, item.PictureDownloadCode)
				if ref == "" {
					continue // a picture item with no usable code
				}
				appendImagePlaceholder(&text)
				rawEvent.Media = append(rawEvent.Media, dingtalkMediaResourceAt(ref, alt, inlinePlaceholderCount))
				inlinePlaceholderCount++
			}
		}
		if len(rawEvent.Media) == 0 {
			msg.Type = channel.MsgTypeText
		} else {
			msg.Type = channel.MsgTypeImage
		}
		msg.Text = strings.TrimSpace(text.String())
		msg.CommandText = strings.TrimSpace(commandText.String())
		normalizeDingTalkRichTextControlLayout(&msg, rc.RichText, len(rawEvent.Media) > 0)
		return withDingTalkRaw(msg, rawEvent), true

	case "audio":
		msg.Type = channel.MsgTypeAudio
		msg.Text = "[Audio message]"
	case "video":
		msg.Type = channel.MsgTypeVideo
		msg.Text = "[Video message]"
	case "file":
		msg.Type = channel.MsgTypeFile
		msg.Text = "[File]"
	default:
		msg.Type = channel.MsgTypeUnknown
		msg.Text = "[Unsupported DingTalk message]"
	}
	msg.CommandText = msg.Text
	return withDingTalkRaw(msg, rawEvent), true
}

// normalizeDingTalkRichTextControlLayout strips either session-control
// directive from the visible rich-text body before the shared Router handles
// it, preserving interleaved image placeholders that Router cannot reconstruct
// from CommandText. The original command source remains available to Router;
// the adapter never applies /new route rotation or reclassifies a remainder.
func normalizeDingTalkRichTextControlLayout(msg *channel.InboundMessage, items []richTextItem, hasMedia bool) {
	control, ok := engine.ParseControlCommand(msg.CommandText)
	if !ok || (control.Body == "" && !hasMedia) {
		return
	}

	firstText := -1
	for i := range items {
		if strings.TrimSpace(items[i].Text) != "" {
			firstText = i
			break
		}
	}
	if firstText < 0 {
		return
	}
	firstControl, ok := engine.ParseControlCommand(items[firstText].Text)
	if !ok || firstControl.Kind != control.Kind {
		return
	}

	if control.Kind == engine.ControlCommandFreshSession {
		msg.ForceFresh = true
	}
	items[firstText].Text = firstControl.Body
	var visible strings.Builder
	for _, item := range items {
		visible.WriteString(item.Text)
		if item.Type == "picture" || item.DownloadCode != "" || item.PictureDownloadCode != "" {
			ref, _ := refAlt(item.DownloadCode, item.PictureDownloadCode)
			if ref != "" {
				appendImagePlaceholder(&visible)
			}
		}
	}
	msg.Text = strings.TrimSpace(visible.String())
	if control.Kind == engine.ControlCommandFreshSession && control.Body == "" {
		// A media-bearing `/clear` is a real turn, not the shared bare-command
		// sentinel. ForceFresh carries the already-consumed directive.
		msg.CommandText = msg.Text
	}
}

// normalizeDingTalkRichTextBotMention removes the bot-addressing envelope from
// whichever text run contains it. DingTalk can place that run before or after
// media and independently from the run containing a control command.
func normalizeDingTalkRichTextBotMention(data *botCallbackData, items []richTextItem, botName string) {
	runs := make([]string, len(items))
	for i := range items {
		runs[i] = items[i].Text
	}
	removeDingTalkBotMention(data, runs, botName)
	for i := range items {
		items[i].Text = runs[i]
	}
}

// normalizeDingTalkBotMention removes the bot-addressing token wherever it
// appears in a plain-text message.
func normalizeDingTalkBotMention(data *botCallbackData, text, botName string) string {
	runs := []string{text}
	removeDingTalkBotMention(data, runs, botName)
	return runs[0]
}

type dingTalkMentionSpan struct {
	run        int
	start, end int
}

func removeDingTalkBotMention(data *botCallbackData, runs []string, botName string) {
	botName = strings.TrimSpace(botName)
	if data == nil || data.ConversationType != convTypeGroup || !data.IsInAtList || botName == "" {
		return
	}
	mentions := exactDingTalkBotMentionSpans(runs, botName)
	for i := len(mentions) - 1; i >= 0; i-- {
		span := mentions[i]
		prefix := runs[span.run][:span.start]
		suffix := runs[span.run][span.end:]
		switch {
		case strings.TrimSpace(prefix) == "":
			prefix = ""
			suffix = trimLeftHorizontalSpace(suffix)
		case strings.TrimSpace(suffix) == "":
			prefix = trimRightHorizontalSpace(prefix)
			suffix = ""
		default:
			suffix = trimLeftHorizontalSpace(suffix)
		}
		runs[span.run] = prefix + suffix
	}
}

func exactDingTalkBotMentionSpans(runs []string, botName string) []dingTalkMentionSpan {
	literal := "@" + botName
	var spans []dingTalkMentionSpan
	for run, text := range runs {
		for offset := 0; offset < len(text); {
			relative := strings.Index(text[offset:], literal)
			if relative < 0 {
				break
			}
			start := offset + relative
			end := start + len(literal)
			if dingTalkMentionLeftBoundary(text[:start]) && dingTalkMentionRightBoundary(text[end:]) {
				spans = append(spans, dingTalkMentionSpan{run: run, start: start, end: end})
			}
			offset = end
		}
	}
	return spans
}

func dingTalkMentionLeftBoundary(prefix string) bool {
	if prefix == "" {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(prefix)
	return unicode.IsSpace(r)
}

func dingTalkMentionRightBoundary(suffix string) bool {
	if suffix == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(suffix)
	// DingTalk supplies no mention span. Only whitespace/end can prove that the
	// verified full name ended here; punctuation may itself extend another
	// display name (for example "Bot-DEV"), so fail closed on it.
	return unicode.IsSpace(r)
}

func trimLeftHorizontalSpace(value string) string {
	return strings.TrimLeft(value, " \t\u3000")
}

func trimRightHorizontalSpace(value string) string {
	return strings.TrimRight(value, " \t\u3000")
}

func withDingTalkRaw(msg channel.InboundMessage, rawEvent dingtalkRawEvent) channel.InboundMessage {
	msg.Raw, _ = json.Marshal(rawEvent)
	return msg
}

// mediaUnreadableMsg turns media the adapter cannot resolve into an explicit
// placeholder. With no downloadable reference, the shared media resolver stays
// out of the path and the normal channel turn carries the degradation signal.
func mediaUnreadableMsg(msg channel.InboundMessage, rawEvent dingtalkRawEvent) channel.InboundMessage {
	msg.Type = channel.MsgTypeImage
	msg.Text = "[Image unavailable]"
	msg.CommandText = msg.Text
	return withDingTalkRaw(msg, rawEvent)
}

func appendImagePlaceholder(b *strings.Builder) {
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(dingtalkImagePlaceholder + "\n")
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
