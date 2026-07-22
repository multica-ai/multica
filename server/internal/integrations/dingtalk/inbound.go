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
	MsgId            string          `json:"msgId"`
	Msgtype          string          `json:"msgtype"`
	IsInAtList       bool            `json:"isInAtList"`
	Text             botCallbackText `json:"text"`
	// Content is the msgtype-discriminated payload of non-text messages
	// (picture / richText). Decoded lazily per msgtype; absent on over-quota
	// callbacks (errorCode 20001 strips text/content entirely).
	Content json.RawMessage `json:"content"`
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
	AppID string `json:"app_id"`
}

// conversation type discriminators DingTalk sends in conversationType.
const (
	convTypeP2P   = "1"
	convTypeGroup = "2"
)

// inboundFromCallback normalizes a DingTalk bot callback. It returns ok=false
// only for events that must not reach the core at all: messages with no sender
// staff id (system / bot-authored). Text, picture and richText become
// ingestable messages; a malformed/over-quota media payload (the 20001 shape
// strips content) still reaches the core as a MediaUnreadable image so the
// engine can refuse it with feedback AFTER its identity gate rather than the
// adapter dropping it silently; audio/video/file/unknown kinds pass through
// with their normalized Type so the engine can refuse them with a capability
// notice. A direct (1:1) message is always addressed to the bot; a group
// message reaches the bot only when it carries an @-mention of it, which
// DingTalk reports via isInAtList.
func inboundFromCallback(data *botCallbackData, appID string) (channel.InboundMessage, bool) {
	if data == nil {
		return channel.InboundMessage{}, false
	}
	if data.SenderStaffId == "" {
		return channel.InboundMessage{}, false
	}

	chatType := dingtalkChatType(data.ConversationType)
	raw, _ := json.Marshal(dingtalkRawEvent{AppID: appID})
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
		Raw: raw,
	}

	switch data.Msgtype {
	case "text":
		msg.Type = channel.MsgTypeText
		msg.Text, msg.ForceFresh, msg.BareFresh = normalizeText(data.Text.Content)
		return msg, true

	case "picture":
		var pc pictureContent
		if len(data.Content) == 0 || json.Unmarshal(data.Content, &pc) != nil {
			// Over-quota (errorCode 20001 strips content) or malformed payload:
			// the sender is a real user who sent an image the bot cannot read.
			// Route it into the engine so it gets identity-gated feedback.
			return mediaUnreadableMsg(msg), true
		}
		ref, alt := refAlt(pc.DownloadCode, pc.PictureDownloadCode)
		if ref == "" {
			return mediaUnreadableMsg(msg), true
		}
		msg.Type = channel.MsgTypeImage
		msg.Segments = []channel.Segment{{MediaIdx: 0}}
		msg.PendingMedia = []channel.PendingMedia{{Kind: channel.MsgTypeImage, Ref: ref, Alt: alt}}
		return msg, true

	case "richText":
		var rc richTextContent
		if len(data.Content) == 0 || json.Unmarshal(data.Content, &rc) != nil {
			// Over-quota / malformed richText: surface it to the engine for
			// identity-gated feedback rather than a silent adapter drop.
			return mediaUnreadableMsg(msg), true
		}
		var (
			segments []channel.Segment
			media    []channel.PendingMedia
			text     strings.Builder
		)
		for _, item := range rc.RichText {
			// A single item may in principle carry BOTH a text run and a picture
			// code; handle each independently (not a switch) so neither is
			// silently dropped. Text first, then image, matching send order.
			// Items with neither (undocumented kinds) contribute nothing.
			if item.Text != "" {
				segments = append(segments, channel.Segment{Text: item.Text, MediaIdx: -1})
				text.WriteString(item.Text)
			}
			if item.Type == "picture" || item.DownloadCode != "" || item.PictureDownloadCode != "" {
				ref, alt := refAlt(item.DownloadCode, item.PictureDownloadCode)
				if ref == "" {
					continue // a picture item with no usable code
				}
				segments = append(segments, channel.Segment{MediaIdx: len(media)})
				media = append(media, channel.PendingMedia{Kind: channel.MsgTypeImage, Ref: ref, Alt: alt})
			}
		}
		if len(media) == 0 {
			// All-text richText is just a text message; degrade to the plain
			// path so /new normalization and downstream behavior match.
			msg.Type = channel.MsgTypeText
			msg.Text, msg.ForceFresh, msg.BareFresh = normalizeText(text.String())
			return msg, true
		}
		// A media-carrying turn honors /new like plain text does. The directive
		// is stripped from the segment AND the re-flattened Text so the two stay
		// in sync (ComposeBody stores from Segments; command parsing reads Text).
		// BareFresh stays false: the images are the turn's content, so the fresh
		// session still appends and runs.
		msg.Type = channel.MsgTypeImage
		segments, msg.ForceFresh = normalizeSegmentsFresh(segments)
		msg.Text = flattenSegments(segments)
		msg.Segments = segments
		msg.PendingMedia = media
		return msg, true

	case "audio":
		msg.Type = channel.MsgTypeAudio
		return msg, true
	case "video":
		msg.Type = channel.MsgTypeVideo
		return msg, true
	case "file":
		msg.Type = channel.MsgTypeFile
		return msg, true
	default:
		msg.Type = channel.MsgTypeUnknown
		return msg, true
	}
}

// normalizeSegmentsFresh applies /new normalization to a media-carrying
// segment list. Mirroring the plain-text first-line rule, only the first
// non-blank text run can open with the directive (image segments contribute
// nothing to the flattened Text, so they don't block it). The matched run
// keeps its remainder, or is dropped entirely when /new was all it held.
func normalizeSegmentsFresh(segments []channel.Segment) ([]channel.Segment, bool) {
	for i, seg := range segments {
		if strings.TrimSpace(seg.Text) == "" {
			continue // media segment or blank run ahead of the first line
		}
		cmd, ok := parseFreshSessionCommand(seg.Text)
		if !ok {
			return segments, false
		}
		if cmd.Body == "" {
			return append(segments[:i], segments[i+1:]...), true
		}
		segments[i].Text = cmd.Body
		return segments, true
	}
	return segments, false
}

// mediaUnreadableMsg marks a base message as carrying media the adapter cannot
// download — an over-quota image whose payload DingTalk stripped, or a
// malformed/codeless media callback. Typed as an image so it never trips the
// unsupported-kind gate; the engine refuses it as media_fetch_failed after the
// identity check so the sender gets feedback instead of a silent adapter drop.
func mediaUnreadableMsg(msg channel.InboundMessage) channel.InboundMessage {
	msg.Type = channel.MsgTypeImage
	msg.MediaUnreadable = true
	return msg
}

// flattenSegments rebuilds the concatenated-text view of a segment list.
func flattenSegments(segments []channel.Segment) string {
	var b strings.Builder
	for _, seg := range segments {
		b.WriteString(seg.Text)
	}
	return strings.TrimSpace(b.String())
}

// normalizeText trims the user's text and folds the platform /new affordance
// into the core's ForceFresh flag, stripping the directive so the agent sees
// only the user's prompt. DingTalk text is never enriched, so the stripped
// body IS the user's own text: an empty remainder means a lone "/new".
func normalizeText(content string) (text string, forceFresh, bareFresh bool) {
	text = strings.TrimSpace(content)
	if cmd, ok := parseFreshSessionCommand(text); ok {
		forceFresh = true
		text = cmd.Body
		bareFresh = strings.TrimSpace(text) == ""
	}
	return text, forceFresh, bareFresh
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
