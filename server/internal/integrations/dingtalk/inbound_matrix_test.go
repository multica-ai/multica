package dingtalk

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type dingTalkMatrixMessageKind string

const (
	matrixText     dingTalkMatrixMessageKind = "text"
	matrixPicture  dingTalkMatrixMessageKind = "picture"
	matrixRichText dingTalkMatrixMessageKind = "richText"
)

var dingTalkMatrixMessageKinds = []dingTalkMatrixMessageKind{
	matrixText,
	matrixPicture,
	matrixRichText,
}

type dingTalkMatrixMetadataLocation string

const (
	matrixMetadataInText    dingTalkMatrixMetadataLocation = "text"
	matrixMetadataInContent dingTalkMatrixMetadataLocation = "content"
)

var dingTalkMatrixMetadataLocations = []dingTalkMatrixMetadataLocation{
	matrixMetadataInText,
	matrixMetadataInContent,
}

type dingTalkMatrixCurrent struct {
	wire        map[string]any
	body        string
	commandText string
	mediaRefs   []string
}

type dingTalkMatrixQuote struct {
	wire      map[string]any
	messageID string
	sender    string
	body      string
	mediaRefs []string
}

func dingTalkMatrixCurrentMessage(t *testing.T, conversationType string, kind dingTalkMatrixMessageKind) dingTalkMatrixCurrent {
	t.Helper()
	wire := map[string]any{
		"msgId":            "current-message",
		"msgtype":          string(kind),
		"conversationId":   "matrix-conversation",
		"conversationType": conversationType,
		"senderStaffId":    "matrix-sender",
		"senderNick":       "Matrix Alice",
		"isInAtList":       conversationType == convTypeGroup,
	}
	switch kind {
	case matrixText:
		wire["text"] = map[string]any{"content": "Current plain text"}
		return dingTalkMatrixCurrent{
			wire: wire, body: "Current plain text", commandText: "Current plain text",
		}
	case matrixPicture:
		wire["content"] = map[string]any{"downloadCode": "current-picture"}
		return dingTalkMatrixCurrent{
			wire: wire, body: dingtalkImagePlaceholder, commandText: dingtalkImagePlaceholder,
			mediaRefs: []string{"current-picture"},
		}
	case matrixRichText:
		wire["content"] = map[string]any{"richText": []any{
			map[string]any{"text": "Current rich text"},
			map[string]any{"type": "picture", "downloadCode": "current-rich-picture"},
		}}
		return dingTalkMatrixCurrent{
			wire: wire, body: "Current rich text\n" + dingtalkImagePlaceholder,
			commandText: "Current rich text", mediaRefs: []string{"current-rich-picture"},
		}
	default:
		t.Fatalf("unsupported current message kind %q", kind)
		return dingTalkMatrixCurrent{}
	}
}

func dingTalkMatrixHumanQuote(t *testing.T, kind dingTalkMatrixMessageKind) dingTalkMatrixQuote {
	t.Helper()
	wire := map[string]any{
		"msgType":    string(kind),
		"msgId":      "quoted-human-message",
		"senderId":   "human-user-id",
		"senderNick": "Alice",
	}
	switch kind {
	case matrixText:
		wire["content"] = map[string]any{"text": "Quoted plain text"}
		return dingTalkMatrixQuote{
			wire: wire, messageID: "quoted-human-message", sender: "Alice", body: "Quoted plain text",
		}
	case matrixPicture:
		wire["content"] = map[string]any{"downloadCode": "quoted-picture"}
		return dingTalkMatrixQuote{
			wire: wire, messageID: "quoted-human-message", sender: "Alice", body: dingtalkImagePlaceholder,
			mediaRefs: []string{"quoted-picture"},
		}
	case matrixRichText:
		// Reply snapshots use msgType/content, unlike current RichText callbacks,
		// which use type/text. This is the observed DingTalk wire shape.
		wire["content"] = map[string]any{"richText": []any{
			map[string]any{"msgType": "text", "content": "Quoted rich text before"},
			map[string]any{"msgType": "picture", "downloadCode": "quoted-rich-picture"},
			map[string]any{"msgType": "text", "content": "Quoted rich text after"},
		}}
		return dingTalkMatrixQuote{
			wire: wire, messageID: "quoted-human-message", sender: "Alice",
			body:      "Quoted rich text before\n" + dingtalkImagePlaceholder + "\nQuoted rich text after",
			mediaRefs: []string{"quoted-rich-picture"},
		}
	default:
		t.Fatalf("unsupported quoted message kind %q", kind)
		return dingTalkMatrixQuote{}
	}
}

func dingTalkMatrixBotQuote(t *testing.T, kind dingTalkMatrixMessageKind) dingTalkMatrixQuote {
	t.Helper()
	textNode := func(value string) map[string]any {
		return map[string]any{
			"elementType": "paragraph",
			"children":    []any{map[string]any{"elementType": "text", "value": value}},
		}
	}
	var (
		body        string
		cardContent []any
	)
	switch kind {
	case matrixText:
		body = "Bot plain text"
		cardContent = []any{textNode(body)}
	case matrixPicture:
		body = "![Bot image](https://example.com/bot-image.png)"
		cardContent = []any{textNode(body)}
	case matrixRichText:
		body = "Bot rich text before\n\n![Bot image](https://example.com/bot-image.png)\n\nBot rich text after"
		cardContent = []any{
			textNode("Bot rich text before"),
			textNode("![Bot image](https://example.com/bot-image.png)"),
			textNode("Bot rich text after"),
		}
	default:
		t.Fatalf("unsupported bot quote shape %q", kind)
	}
	return dingTalkMatrixQuote{
		wire: map[string]any{
			"msgType":    "interactiveCard",
			"msgId":      "quoted-bot-message",
			"senderId":   "bot-user-id",
			"senderNick": "Multica",
			"content":    map[string]any{"cardContent": cardContent},
		},
		messageID: "quoted-bot-message",
		sender:    "Multica",
		body:      body,
	}
}

func attachDingTalkMatrixQuote(
	t *testing.T,
	current dingTalkMatrixCurrent,
	quote dingTalkMatrixQuote,
	location dingTalkMatrixMetadataLocation,
	bot bool,
) {
	t.Helper()
	current.wire["originalMsgId"] = quote.messageID
	metadata := map[string]any{"isReplyMsg": true, "repliedMsg": quote.wire}
	switch location {
	case matrixMetadataInText:
		text, _ := current.wire["text"].(map[string]any)
		if text == nil {
			text = make(map[string]any)
			current.wire["text"] = text
		}
		for key, value := range metadata {
			text[key] = value
		}
	case matrixMetadataInContent:
		content, _ := current.wire["content"].(map[string]any)
		if content == nil {
			content = make(map[string]any)
			current.wire["content"] = content
		}
		for key, value := range metadata {
			content[key] = value
		}
	default:
		t.Fatalf("unsupported quote metadata location %q", location)
	}
	if bot {
		current.wire["chatbotUserId"] = "bot-user-id"
	}
}

func assertDingTalkMatrixMessage(
	t *testing.T,
	current dingTalkMatrixCurrent,
	quote *dingTalkMatrixQuote,
	wantChatType channel.ChatType,
) {
	t.Helper()
	wire, err := json.Marshal(current.wire)
	if err != nil {
		t.Fatalf("marshal matrix callback: %v", err)
	}
	var decoded botCallbackData
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("decode matrix callback: %v", err)
	}
	msg, ok := inboundFromCallback(&decoded, "matrix-app-key")
	if !ok {
		t.Fatal("matrix callback was not ingested")
	}
	if msg.Source.ChatType != wantChatType || !msg.AddressedToBot {
		t.Fatalf("routing = chat type %q addressed %v", msg.Source.ChatType, msg.AddressedToBot)
	}
	if msg.CommandText != current.commandText {
		t.Fatalf("CommandText = %q, want %q", msg.CommandText, current.commandText)
	}

	wantRefs := append([]string(nil), current.mediaRefs...)
	if quote == nil {
		if msg.ReplyTo != nil || strings.Contains(msg.Text, "<quoted_message") || msg.Text != current.body {
			t.Fatalf("unquoted body/reply = %q / %+v, want %q / nil", msg.Text, msg.ReplyTo, current.body)
		}
	} else {
		wantRefs = append(append([]string(nil), quote.mediaRefs...), current.mediaRefs...)
		if msg.ReplyTo == nil || msg.ReplyTo.MessageID != quote.messageID {
			t.Fatalf("ReplyTo = %+v, want %q", msg.ReplyTo, quote.messageID)
		}
		// The formatter's Markdown boundary matrix lives in channel's unit
		// suite; this matrix checks callback extraction and canonical wiring.
		wantBody := channel.FormatQuotedMessage(quote.sender, quote.body) + "\n\n" + current.body
		if msg.Text != wantBody {
			t.Fatalf("canonical quoted/current body = %q, want %q", msg.Text, wantBody)
		}
	}

	raw, err := decodeDingTalkRaw(msg)
	if err != nil {
		t.Fatalf("decode raw matrix event: %v", err)
	}
	if len(raw.Media) != len(wantRefs) {
		t.Fatalf("media count = %d, want %d: %+v", len(raw.Media), len(wantRefs), raw.Media)
	}
	for i, wantRef := range wantRefs {
		if raw.Media[i].Ref != wantRef || raw.Media[i].InlineIndex != i {
			t.Fatalf("media[%d] = %+v, want ref %q at marker %d", i, raw.Media[i], wantRef, i)
		}
	}
	wantType := channel.MsgTypeText
	if len(wantRefs) > 0 {
		wantType = channel.MsgTypeImage
	}
	if msg.Type != wantType {
		t.Fatalf("message type = %q, want %q", msg.Type, wantType)
	}

}

func TestInboundFromCallback_MessageShapeMatrix(t *testing.T) {
	chatTypes := []struct {
		name             string
		conversationType string
		want             channel.ChatType
	}{
		{name: "direct", conversationType: convTypeP2P, want: channel.ChatTypeP2P},
		{name: "group", conversationType: convTypeGroup, want: channel.ChatTypeGroup},
	}
	executed := 0

	// No quote collapses the irrelevant quote-source, quote-type and metadata
	// dimensions: 2 chat types x 3 current message types = 6 cases.
	t.Run("without quote", func(t *testing.T) {
		for _, chatType := range chatTypes {
			for _, currentKind := range dingTalkMatrixMessageKinds {
				executed++
				t.Run(chatType.name+"/current="+string(currentKind), func(t *testing.T) {
					current := dingTalkMatrixCurrentMessage(t, chatType.conversationType, currentKind)
					assertDingTalkMatrixMessage(t, current, nil, chatType.want)
				})
			}
		}
	})

	// Human quote: 2 chat types x 3 quote types x 3 current types x 2
	// metadata locations = 36 cases.
	t.Run("with human quote", func(t *testing.T) {
		for _, chatType := range chatTypes {
			for _, quotedKind := range dingTalkMatrixMessageKinds {
				for _, currentKind := range dingTalkMatrixMessageKinds {
					for _, location := range dingTalkMatrixMetadataLocations {
						executed++
						name := chatType.name + "/quoted=" + string(quotedKind) +
							"/current=" + string(currentKind) + "/metadata=" + string(location)
						t.Run(name, func(t *testing.T) {
							current := dingTalkMatrixCurrentMessage(t, chatType.conversationType, currentKind)
							quote := dingTalkMatrixHumanQuote(t, quotedKind)
							attachDingTalkMatrixQuote(t, current, quote, location, false)
							assertDingTalkMatrixMessage(t, current, &quote, chatType.want)
						})
					}
				}
			}
		}
	})

	// Bot replies are interactive cards in both direct and group chats. Their 3
	// visible shapes x 3 current types x 2 chat types x 2 metadata locations
	// produce 36 cases.
	t.Run("with bot quote", func(t *testing.T) {
		for _, chatType := range chatTypes {
			for _, quotedKind := range dingTalkMatrixMessageKinds {
				for _, currentKind := range dingTalkMatrixMessageKinds {
					for _, location := range dingTalkMatrixMetadataLocations {
						executed++
						name := chatType.name + "/quoted=" + string(quotedKind) +
							"/current=" + string(currentKind) + "/metadata=" + string(location)
						t.Run(name, func(t *testing.T) {
							current := dingTalkMatrixCurrentMessage(t, chatType.conversationType, currentKind)
							quote := dingTalkMatrixBotQuote(t, quotedKind)
							attachDingTalkMatrixQuote(t, current, quote, location, true)
							assertDingTalkMatrixMessage(t, current, &quote, chatType.want)
						})
					}
				}
			}
		}
	})

	const wantCases = 6 + 36 + 36
	if executed != wantCases {
		t.Fatalf("executed %d matrix cases, want %d", executed, wantCases)
	}
}

func TestInboundFromCallback_GroupRichTextQuotePreservesCurrentMediaLayout(t *testing.T) {
	current := dingTalkMatrixCurrentMessage(t, convTypeGroup, matrixRichText)
	current.wire["content"] = map[string]any{"richText": []any{
		map[string]any{"type": "picture", "downloadCode": "current-rich-picture-1"},
		map[string]any{"text": "Current text between images"},
		map[string]any{"type": "picture", "downloadCode": "current-rich-picture-2"},
		map[string]any{"text": "Current text after images"},
	}}
	current.body = dingtalkImagePlaceholder + "\nCurrent text between images\n" +
		dingtalkImagePlaceholder + "\nCurrent text after images"
	current.commandText = "Current text between imagesCurrent text after images"
	current.mediaRefs = []string{"current-rich-picture-1", "current-rich-picture-2"}

	quote := dingTalkMatrixHumanQuote(t, matrixRichText)
	attachDingTalkMatrixQuote(t, current, quote, matrixMetadataInContent, false)
	assertDingTalkMatrixMessage(t, current, &quote, channel.ChatTypeGroup)
}
