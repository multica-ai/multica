package dingtalk

import (
	"encoding/json"
	"fmt"
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
		MsgType    string          `json:"msgType"`
		MsgId      string          `json:"msgId"`
		SenderId   string          `json:"senderId"`
		SenderNick string          `json:"senderNick"`
		Content    json.RawMessage `json:"content"`
	}
	var wire wireMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	m.MsgType = wire.MsgType
	m.MsgId = wire.MsgId
	m.SenderId = wire.SenderId
	m.SenderNick = wire.SenderNick
	m.Content = botCallbackRepliedContent{}
	// repliedMsg is undocumented and card snapshots may use an unknown content
	// shape. Keep the callback usable even when it cannot populate the typed
	// best-effort view.
	_ = json.Unmarshal(wire.Content, &m.Content)
	return nil
}

type botCallbackRepliedContent struct {
	Text string `json:"text"`
	// CardContent is not a stable scalar: observed interactiveCard quote
	// callbacks encode it as either a string or a nested object. Keep the raw
	// value so one shape mismatch cannot discard the other typed content fields.
	CardContent         json.RawMessage `json:"cardContent"`
	RichText            richTextItems   `json:"richText"`
	DownloadCode        string          `json:"downloadCode"`
	PictureDownloadCode string          `json:"pictureDownloadCode"`
	FileName            string          `json:"fileName"`
	Recognition         string          `json:"recognition"`
}

func (content *botCallbackRepliedContent) UnmarshalJSON(data []byte) error {
	type wireContent struct {
		Text                json.RawMessage `json:"text"`
		CardContent         json.RawMessage `json:"cardContent"`
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
	content.Text = dingTalkRichTextNodeText(wire.Text)
	content.CardContent = wire.CardContent
	// An undocumented richText variant must not hide an independently usable
	// text summary or download code from the same selected message.
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
// text/picture are undocumented today and skipped.
type richTextContent struct {
	RichText richTextItems `json:"richText"`
}

type richTextItems []richTextItem

func (items *richTextItems) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*items = nil
		return nil
	}
	var encoded string
	if json.Unmarshal(data, &encoded) == nil {
		return json.Unmarshal([]byte(encoded), items)
	}
	type plainItems richTextItems
	var decoded plainItems
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*items = richTextItems(decoded)
	return nil
}

type richTextItem struct {
	Text                string `json:"text"`
	Type                string `json:"type"`
	DownloadCode        string `json:"downloadCode"`
	PictureDownloadCode string `json:"pictureDownloadCode"`
}

func (item *richTextItem) UnmarshalJSON(data []byte) error {
	type wireItem struct {
		Text                json.RawMessage `json:"text"`
		Content             json.RawMessage `json:"content"`
		Data                json.RawMessage `json:"data"`
		Type                string          `json:"type"`
		MsgType             string          `json:"msgType"`
		DownloadCode        string          `json:"downloadCode"`
		PictureDownloadCode string          `json:"pictureDownloadCode"`
	}
	var wire wireItem
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	item.Type = wire.Type
	if item.Type == "" {
		item.Type = wire.MsgType
	}
	item.DownloadCode = wire.DownloadCode
	item.PictureDownloadCode = wire.PictureDownloadCode
	item.Text = dingTalkRichTextNodeText(wire.Text)
	textBearingNode := item.Type == "" || strings.EqualFold(item.Type, "text")
	if item.Text == "" && textBearingNode {
		item.Text = dingTalkRichTextNodeText(wire.Content)
	}
	if item.Text == "" && textBearingNode {
		item.Text = dingTalkRichTextNodeText(wire.Data)
	}
	return nil
}

// dingTalkRichTextNodeText decodes the text-bearing value of a RichText node.
// Current-message callbacks commonly use a scalar `text`, while reply
// snapshots can wrap the same value in a structural `text` or `content`
// object. Only those explicit text-bearing fields are traversed; unrelated
// strings in the node cannot leak into the visible message body.
func dingTalkRichTextNodeText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	return dingTalkRichTextNodeTextValue(value)
}

func dingTalkRichTextNodeTextValue(value any) string {
	switch typed := value.(type) {
	case string:
		// A scalar text run is user prose, even when it looks like JSON. Only
		// explicitly structural wire fields (such as richText) decode JSON
		// strings; interpreting this value could turn a pasted JSON example
		// into a /new or /clear command.
		return typed
	case map[string]any:
		for _, key := range []string{"text", "content", "value"} {
			if nested, ok := typed[key]; ok {
				if text := dingTalkRichTextNodeTextValue(nested); text != "" {
					return text
				}
			}
		}
	case []any:
		var text strings.Builder
		for _, nested := range typed {
			text.WriteString(dingTalkRichTextNodeTextValue(nested))
		}
		return text.String()
	}
	return ""
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
		if len(data.Content) == 0 || json.Unmarshal(data.Content, &rc) != nil {
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
		return
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
	if visibleInstruction != "" {
		msg.Text += "\n\n" + visibleInstruction
	}
	if len(rawEvent.Media) > 0 {
		msg.Type = channel.MsgTypeImage
	}
}

// dingTalkReplyMetadata normalizes the two callback locations observed for
// quote metadata. Text callbacks put it under text; rich-text callbacks may
// instead keep it beside richText under content, notably in direct chats.
// Prefer text when both are present, and use content only to fill missing
// fields so one client variant cannot hide the quoted snapshot.
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
	appendSummary := func(value string) {
		summary := dingTalkRichTextSummary(value)
		if summary == "" {
			return
		}
		if body.Len() > 0 && !strings.HasSuffix(body.String(), "\n") {
			appendText("\n")
		}
		appendText(summary)
	}

	switch msgType {
	case "text":
		appendText(replied.Content.Text)
	case "interactiveCard":
		appendText(dingTalkCardText(replied.Content.CardContent))
	case "picture", "image":
		appendPicture(replied.Content.DownloadCode, replied.Content.PictureDownloadCode)
		appendSummary(replied.Content.Text)
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
			appendText(recognition)
		} else {
			appendText("[Audio message]")
		}
	case "video":
		appendText("[Video message]")
	default:
		appendText(replied.Content.Text)
	}

	quotedBody := strings.TrimSpace(body.String())
	if quotedBody == "" {
		quotedBody = "[empty or unsupported message]"
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

func dingTalkRichTextSummary(text string) string {
	summary := strings.TrimSpace(text)
	for summary != "" {
		stripped := strings.TrimSpace(strings.TrimPrefix(summary, dingtalkImagePlaceholder))
		if stripped == summary {
			break
		}
		summary = stripped
	}
	return summary
}

// renderDingTalkQuotedRichText reconciles the two structural views DingTalk
// can include in a reply snapshot. richText carries ordered media references
// but may omit visible text runs; text carries the visual summary and uses
// [Image] markers but has no download codes. Ordered nodes remain authoritative
// when they contain prose; otherwise the summary supplies the visible layout,
// while media references stay bound to markers in their original order.
func renderDingTalkQuotedRichText(content botCallbackRepliedContent, placeholderOffset int) (string, []dingtalkMediaResource) {
	var nodes strings.Builder
	pictures := make([]dingtalkMediaResource, 0)
	nodePictureIndexes := make([]int, 0)
	pictureAvailability := make([]bool, 0)
	hasNodeProse := false
	nodeMarkerCount := 0
	for _, item := range content.RichText {
		if item.Text != "" {
			nodes.WriteString(item.Text)
			hasNodeProse = hasNodeProse || dingTalkRichTextComparableText(item.Text) != ""
			nodeMarkerCount += strings.Count(item.Text, dingtalkImagePlaceholder)
		}
		if item.Type != "picture" && item.DownloadCode == "" && item.PictureDownloadCode == "" {
			continue
		}
		ref, alt := refAlt(item.DownloadCode, item.PictureDownloadCode)
		pictureAvailability = append(pictureAvailability, ref != "")
		if ref == "" {
			if nodes.Len() > 0 {
				nodes.WriteByte('\n')
			}
			nodes.WriteString("[Image unavailable]")
			continue
		}
		appendImagePlaceholder(&nodes)
		pictures = append(pictures, dingtalkMediaResource{Ref: ref, Alt: alt})
		nodePictureIndexes = append(nodePictureIndexes, nodeMarkerCount)
		nodeMarkerCount++
	}

	nodeLayout := strings.TrimSpace(nodes.String())
	summaryLayout := strings.TrimSpace(content.Text)
	// A generated unavailable marker is a degradation signal, not text from
	// the selected author. A separate readable summary must survive it.
	if !hasNodeProse && summaryLayout != "" && len(pictureAvailability) > len(pictures) {
		return renderDingTalkQuotedSummaryMedia(summaryLayout, pictureAvailability, pictures, placeholderOffset)
	}
	layout, usesNodePositions := selectDingTalkRichTextLayout(nodeLayout, summaryLayout)
	if layout == "" {
		layout = nodeLayout
		usesNodePositions = true
	}

	markerCount := strings.Count(layout, dingtalkImagePlaceholder)
	if markerCount < len(pictures) {
		var completed strings.Builder
		completed.WriteString(layout)
		for range len(pictures) - markerCount {
			appendImagePlaceholder(&completed)
		}
		layout = strings.TrimSpace(completed.String())
	}
	for i := range pictures {
		index := i
		if usesNodePositions && i < len(nodePictureIndexes) {
			index = nodePictureIndexes[i]
		} else if i >= markerCount {
			index = markerCount + (i - markerCount)
		}
		pictures[i].InlineIndex = placeholderOffset + index
	}
	return layout, pictures
}

// renderDingTalkQuotedSummaryMedia pairs summary image markers with the ordered
// picture slots, including unavailable slots that carry no downloadable ref.
func renderDingTalkQuotedSummaryMedia(summary string, available []bool, pictures []dingtalkMediaResource, placeholderOffset int) (string, []dingtalkMediaResource) {
	for missing := len(available) - strings.Count(summary, dingtalkImagePlaceholder); missing > 0; missing-- {
		summary += "\n" + dingtalkImagePlaceholder
	}
	parts := strings.Split(summary, dingtalkImagePlaceholder)
	var body strings.Builder
	pictureIndex, markerIndex := 0, 0
	for i, part := range parts {
		body.WriteString(part)
		if i == len(parts)-1 {
			break
		}
		if i < len(available) && !available[i] {
			body.WriteString("[Image unavailable]")
			continue
		}
		body.WriteString(dingtalkImagePlaceholder)
		if i < len(available) {
			pictures[pictureIndex].InlineIndex = placeholderOffset + markerIndex
			pictureIndex++
		}
		markerIndex++
	}
	return body.String(), pictures
}

func selectDingTalkRichTextLayout(nodes, summary string) (layout string, usesNodePositions bool) {
	// Ordered richText nodes are the lossless source when they contain visible
	// prose. The sibling summary is a fallback for the observed media-only quote
	// snapshot; it may otherwise be an opaque provider preview, so never mix the
	// two textual representations or guess which fragments overlap.
	if dingTalkRichTextComparableText(nodes) != "" {
		return nodes, true
	}
	if summary != "" {
		return summary, false
	}
	return nodes, true
}

func dingTalkRichTextComparableText(value string) string {
	withoutImages := strings.ReplaceAll(value, dingtalkImagePlaceholder, " ")
	return strings.Join(strings.Fields(withoutImages), " ")
}

// dingTalkCardText extracts only body-like fields from the undocumented
// interactiveCard quote snapshot. DingTalk may represent a robot Markdown
// message with title/text inside cardData/cardParamMap and may JSON-encode an
// intermediate object as a string. Template IDs and unrelated card metadata
// are deliberately ignored.
func dingTalkCardText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	return dingTalkCardTextValue(value)
}

func dingTalkCardTextValue(value any) string {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return ""
		}
		if (strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")) && json.Valid([]byte(text)) {
			var nested any
			decoder := json.NewDecoder(strings.NewReader(text))
			decoder.UseNumber()
			if decoder.Decode(&nested) == nil {
				if extracted := dingTalkCardTextValue(nested); extracted != "" {
					return extracted
				}
			}
		}
		return text
	case map[string]any:
		if _, isNode := typed["elementType"]; isNode {
			return dingTalkCardNodeMarkdown(typed).markdown
		}
		for _, key := range []string{"text", "content", "markdown"} {
			if candidate, ok := typed[key]; ok {
				if extracted := dingTalkCardBodyText(candidate); extracted != "" {
					return extracted
				}
			}
		}
		if leaf, ok := typed["value"]; ok {
			if text, ok := leaf.(string); ok {
				// Preserve whitespace between adjacent inline runs. The enclosing
				// node trims only the completed rendered block.
				return text
			}
			if extracted := dingTalkCardTextValue(leaf); extracted != "" {
				return extracted
			}
		}
		if children, ok := typed["children"]; ok {
			return dingTalkCardNodeSequence(children, false)
		}
		for _, key := range []string{"cardData", "cardParamMap", "data", "params"} {
			if candidate, ok := typed[key]; ok {
				if extracted := dingTalkCardTextValue(candidate); extracted != "" {
					return extracted
				}
			}
		}
	case []any:
		return dingTalkCardNodeSequence(typed, true)
	}
	return ""
}

// dingTalkCardBodyText preserves a known body string as user-visible prose.
// Only structural wrapper fields may contain another encoded card envelope.
func dingTalkCardBodyText(value any) string {
	if body, ok := value.(string); ok {
		return strings.TrimSpace(body)
	}
	return dingTalkCardTextValue(value)
}

type dingTalkCardNodeRender struct {
	markdown string
	block    bool
	breaks   bool
}

func dingTalkCardNodeMarkdown(node map[string]any) dingTalkCardNodeRender {
	elementType, _ := node["elementType"].(string)
	kind := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(elementType))
	switch kind {
	case "paragraphspace", "paragraphbreak", "linebreak", "hardbreak", "newline", "br":
		return dingTalkCardNodeRender{breaks: true}
	case "link", "hyperlink", "a":
		return dingTalkCardLinkMarkdown(node)
	case "unorderedlist", "bulletlist", "ul":
		return dingTalkCardListMarkdown(node["children"], false)
	case "orderedlist", "numberedlist", "ol":
		return dingTalkCardListMarkdown(node["children"], true)
	case "listitem", "bulletitem", "li":
		return dingTalkCardNodeRender{
			markdown: dingTalkCardNodeSequence(node["children"], false),
			block:    true,
		}
	}

	if children, ok := node["children"]; ok {
		block := kind == "paragraph" || kind == "heading" || kind == "blockquote" || kind == "quote" || kind == "div"
		return dingTalkCardNodeRender{
			markdown: dingTalkCardNodeSequence(children, false),
			block:    block,
		}
	}
	if leaf, ok := node["value"]; ok {
		if text, ok := leaf.(string); ok {
			return dingTalkCardNodeRender{markdown: text}
		}
		return dingTalkCardNodeRender{markdown: dingTalkCardTextValue(leaf)}
	}
	for _, key := range []string{"text", "content", "markdown"} {
		if candidate, ok := node[key]; ok {
			return dingTalkCardNodeRender{markdown: dingTalkCardBodyText(candidate)}
		}
	}
	return dingTalkCardNodeRender{}
}

func dingTalkCardNodeSequence(value any, siblingBlocks bool) string {
	nodes, ok := value.([]any)
	if !ok {
		return strings.TrimSpace(dingTalkCardTextValue(value))
	}
	var rendered strings.Builder
	pendingBreak := false
	for _, value := range nodes {
		var fragment dingTalkCardNodeRender
		if node, ok := value.(map[string]any); ok {
			fragment = dingTalkCardNodeMarkdown(node)
		} else {
			fragment.markdown = dingTalkCardTextValue(value)
		}
		if fragment.breaks {
			if rendered.Len() > 0 {
				pendingBreak = true
			}
			continue
		}
		text := fragment.markdown
		if strings.TrimSpace(text) == "" {
			// A separate inline text run can carry the space between words or
			// links. Empty blocks and whitespace after a break stay invisible.
			if !siblingBlocks && !fragment.block && !pendingBreak {
				rendered.WriteString(text)
			}
			continue
		}
		if rendered.Len() > 0 && (pendingBreak || fragment.block || siblingBlocks) {
			rendered.WriteString("\n\n")
		}
		rendered.WriteString(text)
		pendingBreak = false
	}
	return strings.TrimSpace(rendered.String())
}

func dingTalkCardListMarkdown(value any, ordered bool) dingTalkCardNodeRender {
	items, ok := value.([]any)
	if !ok {
		return dingTalkCardNodeRender{}
	}
	lines := make([]string, 0, len(items))
	for i, value := range items {
		item := ""
		if node, ok := value.(map[string]any); ok {
			item = dingTalkCardNodeMarkdown(node).markdown
		} else {
			item = dingTalkCardTextValue(value)
		}
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix := "- "
		if ordered {
			prefix = fmt.Sprintf("%d. ", i+1)
		}
		lines = append(lines, prefix+strings.ReplaceAll(item, "\n", "\n  "))
	}
	return dingTalkCardNodeRender{markdown: strings.Join(lines, "\n"), block: true}
}

func dingTalkCardLinkMarkdown(node map[string]any) dingTalkCardNodeRender {
	label := dingTalkCardNodeSequence(node["children"], false)
	if label == "" {
		label = firstDingTalkCardString(node, "label", "text", "title", "name")
	}
	href := firstDingTalkCardString(node, "href", "url", "targetUrl", "targetURL", "link")
	if nested, ok := node["value"].(map[string]any); ok {
		if label == "" {
			label = firstDingTalkCardString(nested, "label", "text", "title", "name")
		}
		if href == "" {
			href = firstDingTalkCardString(nested, "href", "url", "targetUrl", "targetURL", "link")
		}
	} else if value, ok := node["value"].(string); ok {
		if href == "" {
			href = value
		} else if label == "" {
			label = value
		}
	}
	if href == "" {
		return dingTalkCardNodeRender{markdown: label}
	}
	if label == "" {
		label = href
	}
	label = strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(label)
	href = strings.ReplaceAll(href, ")", `%29`)
	return dingTalkCardNodeRender{markdown: "[" + label + "](" + href + ")"}
}

func firstDingTalkCardString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
func mediaUnreadableMsg(data *botCallbackData, msg channel.InboundMessage, rawEvent dingtalkRawEvent) channel.InboundMessage {
	msg.Type = channel.MsgTypeImage
	msg.Text = "[Image unavailable]"
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
