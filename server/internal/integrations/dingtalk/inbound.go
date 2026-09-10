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
	OriginalMsgId     string              `json:"originalMsgId"`
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
	Content    string                     `json:"content"`
	IsReplyMsg bool                       `json:"isReplyMsg"`
	RepliedMsg *botCallbackRepliedMessage `json:"repliedMsg"`
}

type botCallbackReplyMetadata struct {
	IsReplyMsg bool                       `json:"isReplyMsg"`
	RepliedMsg *botCallbackRepliedMessage `json:"repliedMsg"`
}

// botCallbackRepliedMessage is the snapshot DingTalk embeds under
// text.repliedMsg when a user explicitly quotes another message. DingTalk's
// public receive-message schema does not document these fields, so every field
// remains optional and the decoder must tolerate partial snapshots.
type botCallbackRepliedMessage struct {
	MsgType    string                    `json:"msgType"`
	MsgId      string                    `json:"msgId"`
	SenderId   string                    `json:"senderId"`
	SenderNick string                    `json:"senderNick"`
	Content    botCallbackRepliedContent `json:"content"`
}

func (m *botCallbackRepliedMessage) UnmarshalJSON(data []byte) error {
	type wireMessage struct {
		MsgType    json.RawMessage `json:"msgType"`
		MsgId      json.RawMessage `json:"msgId"`
		SenderId   json.RawMessage `json:"senderId"`
		SenderNick json.RawMessage `json:"senderNick"`
		Content    json.RawMessage `json:"content"`
	}
	*m = botCallbackRepliedMessage{}
	var wire wireMessage
	if json.Unmarshal(data, &wire) != nil {
		// The selected snapshot is optional. An unknown envelope must not
		// reject the sender's otherwise valid current message.
		return nil
	}
	_ = json.Unmarshal(wire.MsgType, &m.MsgType)
	_ = json.Unmarshal(wire.MsgId, &m.MsgId)
	_ = json.Unmarshal(wire.SenderId, &m.SenderId)
	_ = json.Unmarshal(wire.SenderNick, &m.SenderNick)
	// Each optional field degrades independently. Missing display or routing
	// metadata must not hide an independently readable selected body.
	_ = json.Unmarshal(wire.Content, &m.Content)
	return nil
}

type botCallbackRepliedContent struct {
	Text                string        `json:"text"`
	RichText            richTextItems `json:"richText"`
	DownloadCode        string        `json:"downloadCode"`
	PictureDownloadCode string        `json:"pictureDownloadCode"`
	FileName            string        `json:"fileName"`
	Recognition         string        `json:"recognition"`
}

func (content *botCallbackRepliedContent) UnmarshalJSON(data []byte) error {
	type wireContent struct {
		Text                json.RawMessage `json:"text"`
		RichText            json.RawMessage `json:"richText"`
		DownloadCode        string          `json:"downloadCode"`
		PictureDownloadCode string          `json:"pictureDownloadCode"`
		FileName            string          `json:"fileName"`
		Recognition         string          `json:"recognition"`
	}
	var wire wireContent
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	content.Text = ""
	_ = json.Unmarshal(wire.Text, &content.Text)
	// Decode only the documented ordered-array shape. Unsupported quote
	// variants remain unavailable; never infer a layout from a sibling summary.
	content.RichText = nil
	_ = json.Unmarshal(wire.RichText, &content.RichText)
	content.DownloadCode = wire.DownloadCode
	content.PictureDownloadCode = wire.PictureDownloadCode
	content.FileName = wire.FileName
	content.Recognition = wire.Recognition
	return nil
}

// pictureContent is the content shape of msgtype=picture. Real callbacks may
// carry either download code; both resolve through messageFiles/download.
type pictureContent struct {
	DownloadCode        string `json:"downloadCode"`
	PictureDownloadCode string `json:"pictureDownloadCode"`
}

// richTextContent is the content shape of msgtype=richText: an ORDERED array
// of heterogeneous items — text runs {"text":…} interleaved with picture items
// {"type":"picture","downloadCode":…} in send order. Item kinds beyond
// text/picture are undocumented and receive an unavailable-content marker.
type richTextContent struct {
	RichText richTextItems `json:"richText"`
}

type richTextItems []richTextItem

type richTextItem struct {
	Text                string `json:"text"`
	Type                string `json:"type"`
	DownloadCode        string `json:"downloadCode"`
	PictureDownloadCode string `json:"pictureDownloadCode"`
}

// Only the string text and picture fields documented by DingTalk are read.
// Unknown nested values are not recursively interpreted as prose or commands.
// A bad node degrades locally so valid neighboring text and pictures survive.
func (item *richTextItem) UnmarshalJSON(data []byte) error {
	type wireItem struct {
		Text                json.RawMessage `json:"text"`
		Type                string          `json:"type"`
		DownloadCode        string          `json:"downloadCode"`
		PictureDownloadCode string          `json:"pictureDownloadCode"`
	}
	*item = richTextItem{}
	var wire wireItem
	if err := json.Unmarshal(data, &wire); err != nil {
		item.Text = "[rich-text content unavailable]"
		return nil
	}
	if wire.Type != "" && wire.Type != "text" && wire.Type != "picture" {
		item.Text = "[rich-text content unavailable]"
		return nil
	}
	item.Type = wire.Type
	item.DownloadCode = wire.DownloadCode
	item.PictureDownloadCode = wire.PictureDownloadCode
	if len(wire.Text) > 0 && string(wire.Text) != "null" {
		if err := json.Unmarshal(wire.Text, &item.Text); err != nil {
			item.Text = "[rich-text content unavailable]"
		}
	} else if item.Type != "picture" && item.DownloadCode == "" && item.PictureDownloadCode == "" {
		item.Text = "[rich-text content unavailable]"
	}
	return nil
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
		applyDingTalkReplyContext(data, &msg, &rawEvent)
		return withDingTalkRaw(msg, rawEvent), true

	case "picture":
		var pc pictureContent
		if len(data.Content) == 0 || json.Unmarshal(data.Content, &pc) != nil {
			// Over-quota (errorCode 20001 strips content) or malformed payload:
			// the sender is a real user who sent an image the bot cannot read.
			// Route it into the engine so it gets identity-gated feedback.
			return mediaUnreadableMsg(data, msg, rawEvent), true
		}
		ref, alt := refAlt(pc.DownloadCode, pc.PictureDownloadCode)
		if ref == "" {
			return mediaUnreadableMsg(data, msg, rawEvent), true
		}
		msg.Type = channel.MsgTypeImage
		msg.Text = dingtalkImagePlaceholder
		msg.CommandText = msg.Text
		rawEvent.Media = []dingtalkMediaResource{dingtalkMediaResourceAt(ref, alt, 0)}
		applyDingTalkReplyContext(data, &msg, &rawEvent)
		return withDingTalkRaw(msg, rawEvent), true

	case "richText":
		var rc richTextContent
		if len(data.Content) == 0 || json.Unmarshal(data.Content, &rc) != nil || len(rc.RichText) == 0 {
			// Over-quota / malformed richText: surface it to the engine for
			// identity-gated feedback rather than a silent adapter drop.
			return mediaUnreadableMsg(data, msg, rawEvent), true
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
			// Unsupported nodes carry an explicit placeholder from decoding.
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
		applyDingTalkReplyContext(data, &msg, &rawEvent)
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
	applyDingTalkReplyContext(data, &msg, &rawEvent)
	return withDingTalkRaw(msg, rawEvent), true
}

func applyDingTalkReplyContext(data *botCallbackData, msg *channel.InboundMessage, rawEvent *dingtalkRawEvent) {
	if data == nil || msg == nil || rawEvent == nil {
		return
	}
	reply := dingTalkReplyMetadata(data)
	replied := reply.RepliedMsg
	if !reply.IsReplyMsg && replied == nil && data.OriginalMsgId == "" {
		return
	}

	parentID := strings.TrimSpace(data.OriginalMsgId)
	if replied != nil && strings.TrimSpace(replied.MsgId) != "" {
		parentID = strings.TrimSpace(replied.MsgId)
	}
	// Undocumented callbacks can omit both message IDs while still carrying a
	// complete selected snapshot. Keep that reply relationship independently of
	// its best-effort platform ID.
	msg.ReplyTo = &channel.ReplyCtx{MessageID: parentID}
	if replied == nil {
		if !reply.IsReplyMsg {
			return
		}
		// An explicit quote with no snapshot is still selected input; a bare
		// thread coordinate above does not imply that a quote was selected.
		replied = &botCallbackRepliedMessage{}
	}

	// Once Text is enriched, the shared Router can no longer strip a leading
	// control directive by comparing Text with CommandText. Strip it from the
	// visible instruction here while leaving CommandText as the source of truth.
	instruction := msg.Text
	visibleInstruction := instruction
	// RichText has already reconstructed its visible layout above. Only an
	// untouched current body may consume a directive here; reparsing the
	// reconstructed remainder would give one turn two control meanings.
	if instruction == msg.CommandText {
		if control, ok := engine.ParseControlCommand(instruction); ok {
			visibleInstruction = control.Body
			if control.Kind == engine.ControlCommandFreshSession {
				msg.ForceFresh = true
			}
		}
	}

	block, quotedMedia := renderDingTalkQuotedMessage(replied)
	// The quoted block is prepended to the current body, so its media must also
	// lead the resource list. Shift each current resource by every placeholder
	// occurrence introduced by that block, including user-authored literals,
	// preserving InlineIndex's occurrence-based contract.
	currentMedia := rawEvent.Media
	placeholderOffset := strings.Count(block, dingtalkImagePlaceholder)
	for i := range currentMedia {
		currentMedia[i].InlineIndex += placeholderOffset
	}
	rawEvent.Media = make([]dingtalkMediaResource, 0, len(quotedMedia)+len(currentMedia))
	rawEvent.Media = append(rawEvent.Media, quotedMedia...)
	rawEvent.Media = append(rawEvent.Media, currentMedia...)

	msg.Text = block
	msg.HasSelectedContext = block != ""
	if visibleInstruction != "" {
		msg.Text += "\n\n" + visibleInstruction
	}
	if len(rawEvent.Media) > 0 {
		msg.Type = channel.MsgTypeImage
	}
}

// dingTalkReplyMetadata reads optional quote metadata under text or content.
// Neither location is guaranteed by the public receive-message schema. This
// bounded best-effort projection prefers text and fills missing fields from
// content; it does not infer the selected body from other callback fields.
func dingTalkReplyMetadata(data *botCallbackData) botCallbackReplyMetadata {
	metadata := botCallbackReplyMetadata{
		IsReplyMsg: data.Text.IsReplyMsg,
		RepliedMsg: data.Text.RepliedMsg,
	}
	if len(data.Content) == 0 || (metadata.IsReplyMsg && metadata.RepliedMsg != nil) {
		return metadata
	}
	var contentMetadata botCallbackReplyMetadata
	if json.Unmarshal(data.Content, &contentMetadata) != nil {
		return metadata
	}
	if !metadata.IsReplyMsg {
		metadata.IsReplyMsg = contentMetadata.IsReplyMsg
	}
	if metadata.RepliedMsg == nil {
		metadata.RepliedMsg = contentMetadata.RepliedMsg
	}
	return metadata
}

func renderDingTalkQuotedMessage(replied *botCallbackRepliedMessage) (string, []dingtalkMediaResource) {
	if replied == nil {
		return "", nil
	}
	// SenderId is an opaque platform identity, not a display name. A partial
	// snapshot without SenderNick keeps its quote without an invented author.
	sender := strings.TrimSpace(replied.SenderNick)
	msgType := strings.TrimSpace(replied.MsgType)
	if msgType == "" {
		msgType = "unknown"
	}
	var body strings.Builder
	placeholderCount := 0
	media := make([]dingtalkMediaResource, 0)
	appendText := func(value string) {
		body.WriteString(value)
		placeholderCount += strings.Count(value, dingtalkImagePlaceholder)
	}
	appendPicture := func(downloadCode, pictureDownloadCode string) {
		ref, alt := refAlt(downloadCode, pictureDownloadCode)
		if ref == "" {
			appendText("[Image unavailable]")
			return
		}
		appendImagePlaceholder(&body)
		media = append(media, dingtalkMediaResourceAt(ref, alt, placeholderCount))
		placeholderCount++
	}

	switch msgType {
	case "text":
		appendText(dingTalkReadableQuotedText(replied.Content.Text))
	case "interactiveCard":
		// cardParamMap belongs to a template, not a universal body schema.
		// https://open.dingtalk.com/document/orgapp/create-and-deliver-cards
		appendText("[quoted content unavailable]")
	case "picture", "image":
		appendPicture(replied.Content.DownloadCode, replied.Content.PictureDownloadCode)
		// The snapshot's text field has no documented caption meaning.
		// Keep the image while explicitly withholding supplementary text.
		if replied.Content.Text != "" {
			appendText("\n[quoted content unavailable]")
		}
	case "richText":
		quotedBody, quotedMedia := renderDingTalkQuotedRichText(replied.Content, placeholderCount)
		appendText(quotedBody)
		media = append(media, quotedMedia...)
	case "file":
		if name := strings.TrimSpace(replied.Content.FileName); name != "" {
			appendText("[File: " + name + "]")
		} else {
			appendText("[File]")
		}
	case "audio":
		if recognition := strings.TrimSpace(replied.Content.Recognition); recognition != "" {
			appendText(dingTalkReadableQuotedText(recognition))
		} else {
			appendText("[Audio message]")
		}
	case "video":
		appendText("[Video message]")
	default:
		appendText("[quoted content unavailable]")
	}

	quotedBody := strings.TrimSpace(body.String())
	if quotedBody == "" {
		quotedBody = "[quoted content unavailable]"
	}
	block := channel.FormatQuotedMessage(sender, quotedBody)
	// The final Markdown is the media-position authority. Formatting only adds
	// an author prefix and blockquote markers, so account for any placeholders
	// introduced by that prefix before joining it to the current message.
	prefixMarkers := strings.Count(block, dingtalkImagePlaceholder) - strings.Count(quotedBody, dingtalkImagePlaceholder)
	for i := range media {
		media[i].InlineIndex += prefixMarkers
	}
	return block, media
}

// renderDingTalkQuotedRichText reuses only the ordered text/picture schema:
// https://open.dingtalk.com/document/orgapp/receive-message
// The public schema does not define repliedMsg or a relationship between a
// quote's text summary and richText. Never pair summary markers with resources.
func renderDingTalkQuotedRichText(content botCallbackRepliedContent, placeholderOffset int) (string, []dingtalkMediaResource) {
	var body strings.Builder
	var media []dingtalkMediaResource
	if len(content.RichText) == 0 {
		return "[quoted content unavailable]", nil
	}
	hasText := false
	for _, item := range content.RichText {
		hasText = hasText || strings.TrimSpace(item.Text) != ""
	}
	if !hasText && strings.TrimSpace(content.Text) != "" {
		// A media-only snapshot may have omitted prose. Signal that loss
		// without guessing where the preview belongs among its pictures.
		body.WriteString("[quoted content unavailable]\n")
	}
	markerCount := placeholderOffset
	for _, item := range content.RichText {
		text := dingTalkReadableQuotedText(item.Text)
		body.WriteString(text)
		markerCount += strings.Count(text, dingtalkImagePlaceholder)
		if item.Type != "picture" && item.DownloadCode == "" && item.PictureDownloadCode == "" {
			continue
		}
		ref, alt := refAlt(item.DownloadCode, item.PictureDownloadCode)
		if ref == "" {
			if body.Len() > 0 && !strings.HasSuffix(body.String(), "\n") {
				body.WriteByte('\n')
			}
			body.WriteString("[Image unavailable]")
			continue
		}
		appendImagePlaceholder(&body)
		media = append(media, dingtalkMediaResourceAt(ref, alt, markerCount))
		markerCount++
	}
	return strings.TrimSpace(body.String()), media
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
}

// dingTalkReadableQuotedText defines a conservative projection policy, not an
// opaque-envelope decoder. The public sample in
// https://github.com/open-dingtalk/dingtalk-stream-sdk-go/issues/22 contains ||,
// but does not establish lengths, alphabets, versions, or trailer field counts.
// Selected text containing that ambiguous separator is therefore unavailable,
// including legitimate quoted code/prose containing ||. Current input is never
// filtered. Apply this only to provider text values, not rendered quote blocks,
// so a fallback cannot discard generated image markers and their media slots.
func dingTalkReadableQuotedText(value string) string {
	if strings.Contains(value, "||") {
		return "[quoted content unavailable]"
	}
	return value
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
func mediaUnreadableMsg(data *botCallbackData, msg channel.InboundMessage, rawEvent dingtalkRawEvent) channel.InboundMessage {
	msg.Type = channel.MsgTypeImage
	msg.Text = "[Image unavailable]"
	if data.Msgtype == "richText" {
		msg.Type = channel.MsgTypeText
		msg.Text = "[rich-text content unavailable]"
	}
	msg.CommandText = msg.Text
	applyDingTalkReplyContext(data, &msg, &rawEvent)
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
