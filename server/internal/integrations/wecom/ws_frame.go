package wecom

// ws_frame.go — the aibot WebSocket wire format. Every frame is JSON with a
// {cmd, headers.req_id, body} envelope. We only parse the frames we act on:
//
//   inbound   — aibot_msg_callback (user message), aibot_event_callback (event)
//   outbound  — aibot_subscribe (auth), ping (heartbeat), aibot_send_msg (push),
//               aibot_respond_msg (in-window reply)
//   response  — the ack the server writes for aibot_subscribe / ping / send_msg
//
// The wire is documented at https://developer.work.weixin.qq.com/document/path/101463 .

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// Frame commands the client sends.
const (
	cmdSubscribe  = "aibot_subscribe"
	cmdPing       = "ping"
	cmdSendMsg    = "aibot_send_msg"
	cmdRespondMsg = "aibot_respond_msg"
)

// Frame commands the server sends. These are what the read loop switches on.
const (
	cmdMsgCallback   = "aibot_msg_callback"
	cmdEventCallback = "aibot_event_callback"
	cmdServerPing    = "ping"
	cmdPong          = "pong"
)

// Event types inside aibot_event_callback.body.event.eventtype.
const (
	eventDisconnected = "disconnected_event"
	eventEnterChat    = "enter_chat"
	eventTemplateCard = "template_card_event"
	eventFeedback     = "feedback_event"
)

// aibot receiver kinds for aibot_send_msg. WeCom uses ints, not strings.
const (
	chatTypeSingleInt = 1
	chatTypeGroupInt  = 2
)

// frameHeaders carries a per-frame correlation id. Server acks reflect the
// req_id back so the client can pair requests with responses.
type frameHeaders struct {
	ReqID string `json:"req_id"`
}

// frameEnvelope is the outer shape of every frame the server pushes. Body
// is left raw so downstream code can unmarshal the specific shape without
// re-parsing the outer wrapper.
type frameEnvelope struct {
	Cmd     string          `json:"cmd"`
	Headers frameHeaders    `json:"headers"`
	Body    json.RawMessage `json:"body"`

	// Response fields (present when the server acks one of our writes).
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// aibotMsgCallback is the body of an aibot_msg_callback frame — a user
// message pushed from a chat to the bot.
type aibotMsgCallback struct {
	MsgID    string `json:"msgid"`
	AIBotID  string `json:"aibotid"`
	ChatID   string `json:"chatid"`
	ChatType string `json:"chattype"` // "single" | "group"
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
	MsgType string `json:"msgtype"` // "text" | "image" | "voice" | "file" | "video" | "mixed"
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	// Voice carries the TRANSCRIPT, not audio. WeCom runs the speech
	// recognition on its side and delivers only the result, so a voice note
	// needs no download, no media key and no storage — it is a sentence that
	// happened to be spoken. Not gated on chat type: whatever chat a voice
	// note arrives from, the transcript is read the same way.
	Voice struct {
		Content string `json:"content"`
	} `json:"voice"`
	// Image / File / Video are the downloadable kinds. Each carries only a
	// pre-signed COS url and the key its bytes are encrypted with — no name,
	// no size, no MIME type (see media_download.go for where those come from
	// instead).
	Image mediaBody `json:"image"`
	File  mediaBody `json:"file"`
	Video mediaBody `json:"video"`
	// Mixed carries 图文混排 — a message the user composed with text runs
	// and attachments interleaved. Each item is itself typed and carries the
	// same bodies a standalone message of that type would.
	Mixed struct {
		MsgItem []mixedItem `json:"msg_item"`
	} `json:"mixed"`
}

// mediaBody is the {url, aeskey} pair every downloadable kind carries. In
// long-connection mode the key is minted per url, so it lives on the message
// rather than in configuration.
type mediaBody struct {
	URL    string `json:"url"`
	AESKey string `json:"aeskey"`
}

// mixedItem is one run of a 图文混排 message: a sentence, a spoken sentence,
// or an attachment, in the order the user composed them.
type mixedItem struct {
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Voice struct {
		Content string `json:"content"`
	} `json:"voice"`
	Image mediaBody `json:"image"`
	File  mediaBody `json:"file"`
	Video mediaBody `json:"video"`
}

// words is the part of one 图文混排 run the SENDER typed or said. An
// attachment contributes nothing, which is the difference between this and
// render below.
func (item mixedItem) words() string {
	switch strings.ToLower(item.MsgType) {
	case "text":
		return strings.TrimSpace(item.Text.Content)
	case "voice":
		// WeCom runs the speech recognition on its side and delivers only
		// the result, so a voice run is a sentence that happened to be
		// spoken — no download, no key. It is the sender's own words, so a
		// spoken "/issue 登录坏了" is a command like the typed one.
		return strings.TrimSpace(item.Voice.Content)
	default:
		return ""
	}
}

// render turns one 图文混排 run into the line it contributes to the message
// body. An item of a kind this adapter does not know contributes nothing
// rather than a stray placeholder.
func (item mixedItem) render() string {
	if s := item.words(); s != "" {
		return s
	}
	body, kind, ok := mediaFor(item.MsgType, item.Image, item.File, item.Video)
	if !ok || strings.TrimSpace(body.URL) == "" {
		return ""
	}
	return mediaPlaceholder(kind)
}

// mediaPlaceholder is the marker that stands in for an attachment in the
// stored message body, so the agent can see that something was attached
// before (or instead of) the bytes arriving on the detached media path.
//
// The exact strings are Lark's and DingTalk's, byte for byte:
// lark/content_flatten.go flattenContent returns "[Image]" / "[File]" /
// "[Video]", and dingtalk/inbound.go:95 pins dingtalkImagePlaceholder =
// "[Image]" with "[File]" at inbound.go:205. An agent reads every channel
// through the same prompt; a wecom-only spelling would be one more thing
// for it to learn for no reason.
func mediaPlaceholder(kind channel.MsgType) string {
	switch kind {
	case channel.MsgTypeImage:
		return "[Image]"
	case channel.MsgTypeVideo:
		return "[Video]"
	default:
		return "[File]"
	}
}

// mediaFor returns the body and normalized kind for a raw wecom msgtype, and
// whether that type is one we download at all.
func mediaFor(msgType string, image, file, video mediaBody) (mediaBody, channel.MsgType, bool) {
	switch strings.ToLower(msgType) {
	case "image":
		return image, channel.MsgTypeImage, true
	case "file":
		return file, channel.MsgTypeFile, true
	case "video":
		return video, channel.MsgTypeVideo, true
	default:
		return mediaBody{}, channel.MsgTypeUnknown, false
	}
}

// attachments lists the downloadable media on this callback, in the order the
// user sent it. A body with no url is skipped: there is nothing to fetch, and
// carrying it forward would only produce an intent-ledger row for an object
// that can never exist.
func (mc aibotMsgCallback) attachments() []InboundMedia {
	var out []InboundMedia
	add := func(body mediaBody, kind channel.MsgType) {
		if strings.TrimSpace(body.URL) == "" {
			return
		}
		out = append(out, InboundMedia{Kind: kind, URL: body.URL, AESKey: body.AESKey})
	}
	if body, kind, ok := mediaFor(mc.MsgType, mc.Image, mc.File, mc.Video); ok {
		add(body, kind)
		return out
	}
	if !strings.EqualFold(mc.MsgType, "mixed") {
		return nil
	}
	for _, item := range mc.Mixed.MsgItem {
		if body, kind, ok := mediaFor(item.MsgType, item.Image, item.File, item.Video); ok {
			add(body, kind)
		}
	}
	return out
}

// ownText is the agent-readable body of this callback, and whether there is
// one at all.
//
// Plain text answers with its body; a voice note answers with the transcript
// WeCom recognised, which is the sender's own words and needs no download; a
// photo, file or video answers with a bracketed placeholder, because the bytes
// arrive later on a detached path and the message has to say something in the
// meantime (the placeholder is also what survives if the download never
// succeeds); 图文混排 answers with its runs rendered in the order they were
// composed, so "look at this" still reads above the picture it was written
// about.
//
// Recognition comes back empty on background noise or a half-second press, and
// an empty body would reach the agent as a turn with nothing in it — so an
// empty transcript answers false and takes the receipt path, exactly like a
// location card or a kind WeCom adds next year.
func (mc aibotMsgCallback) ownText() (string, bool) {
	switch strings.ToLower(mc.MsgType) {
	case "text":
		return mc.Text.Content, true
	case "voice":
		transcript := strings.TrimSpace(mc.Voice.Content)
		return transcript, transcript != ""
	case "image", "file", "video":
		body, kind, _ := mediaFor(mc.MsgType, mc.Image, mc.File, mc.Video)
		if strings.TrimSpace(body.URL) == "" {
			return "", false
		}
		return mediaPlaceholder(kind), true
	case "mixed":
		var runs []string
		for _, item := range mc.Mixed.MsgItem {
			if s := item.render(); s != "" {
				runs = append(runs, s)
			}
		}
		if len(runs) == 0 {
			return "", false
		}
		return strings.Join(runs, "\n"), true
	default:
		return "", false
	}
}

// ownCommandSource is what the slash-command parsers read: the sender's own
// words, and nothing this adapter wrote.
//
// It is not ownText. ownText inserts "[Image]" / "[File]" / "[Video]" where
// the attachments were, because the stored body has to show that something
// was attached and where. engine.ParseIssueCommand only ever looks at the
// FIRST non-empty line, so a person who attaches a screenshot and then types
// "/issue 登录坏了" — the natural order, and the one WeCom's composer
// encourages — produces a body opening with "[Image]", the parser sees a
// placeholder instead of the command, no issue is filed, and nothing anywhere
// tells them why. Typing the same two things in the other order works. That
// is not a distinction a user can be expected to know about.
//
// So the command source drops the placeholders and keeps the runs the sender
// authored, in order. The stored body still carries them: cutting them there
// would lose the position the detached media binder materializes into
// (engine/issue_command.go issueDescriptionFromCommandBody says the same
// thing from the other end).
//
// A spoken message answers with its transcript. Those are the sender's own
// words as much as a typed line is, so "/issue 登录坏了" said out loud files
// the same issue it would have typed — and sourcing the command from the typed
// field instead would leave it empty, at which point the engine falls back to
// Text, files the issue anyway and, with SkipAgentRun false, runs the agent
// over it as well.
//
// A standalone photo, file or video answers with nothing. Its whole body is a
// placeholder, so there are no words in it to parse, and handing the parser a
// string the sender never typed is the defect this exists to remove.
func (mc aibotMsgCallback) ownCommandSource() string {
	switch strings.ToLower(mc.MsgType) {
	case "text":
		return mc.Text.Content
	case "voice":
		return strings.TrimSpace(mc.Voice.Content)
	case "mixed":
		var runs []string
		for _, item := range mc.Mixed.MsgItem {
			if s := item.words(); s != "" {
				runs = append(runs, s)
			}
		}
		return strings.Join(runs, "\n")
	default:
		return ""
	}
}

// aibotEventCallback is the body of an aibot_event_callback frame. We only
// look at the event type; specific event fields (template-card selection,
// feedback vote) are not surfaced yet.
type aibotEventCallback struct {
	Event struct {
		EventType string `json:"eventtype"`
	} `json:"event"`
}

// ---- normalization ----

// InboundMessage is the wecom-side flattened envelope the WS read loop
// builds from a decoded aibot_msg_callback. It is stashed into
// channel.InboundMessage.Raw as JSON so wecom_resolvers.go can reach the
// platform-specific fields (BotID, ReqID) the cross-platform envelope does
// not carry.
type InboundMessage struct {
	// BotID is the smart-bot identifier this event was delivered to. It
	// is the routing key the installation resolver uses.
	BotID string `json:"bot_id"`

	// MsgID is the WeCom per-message identifier used for two-phase dedup.
	MsgID string `json:"msg_id,omitempty"`

	// MsgType is the raw wecom type ("text", "image", "event", ...). Media
	// / unknown types round-trip via the cross-platform channel.MsgType enum
	// (see channelMsgType); the raw string stays here for auditing.
	MsgType string `json:"msg_type,omitempty"`

	// ChatType is the tencent-internal conversation discriminator
	// ("single" for 1:1, "group" for a group chat).
	ChatType string `json:"chat_type,omitempty"`

	// ChatID is the userid (single) or chatid (group) that the message
	// originated in — the routing identity for outbound + session binding.
	ChatID string `json:"chat_id,omitempty"`

	// SenderUserID is the userid of the person who typed the message.
	SenderUserID string `json:"sender_user_id,omitempty"`

	// Content is the human-readable body: the user's words — typed, or as
	// WeCom's recognition returned them for a voice note — and the
	// placeholders standing in for their attachments. The cross-platform
	// envelope's Text field is populated from this.
	Content string `json:"content,omitempty"`

	// ReqID is the frame req_id the server sent this message with. We
	// keep it so a future aibot_respond_msg (5s window) can echo it back;
	// iteration 1 uses aibot_send_msg unconditionally and does not need it.
	ReqID string `json:"req_id,omitempty"`

	// Media lists the attachments to fetch, in the order the user sent them.
	// It is the MediaResolver's input and travels only in
	// channel.InboundMessage.Raw, which the engine passes along in memory and
	// never persists — the urls lapse after five minutes and the keys are
	// single-use, so neither belongs in a table or a log line.
	Media []InboundMedia `json:"media,omitempty"`
}

// InboundMedia is one downloadable attachment on a callback.
type InboundMedia struct {
	// Kind is the normalized media type the attachment row is labelled with.
	Kind channel.MsgType `json:"kind"`
	// URL is the pre-signed COS address, good for five minutes, needing no
	// access token.
	URL string `json:"url"`
	// AESKey unlocks what comes back from URL. Long-connection mode mints one
	// per url; see media_crypt.go.
	AESKey string `json:"aeskey"`
}

// channelMessageFromCallback converts a wecom-side aibot_msg_callback into
// the cross-platform channel.InboundMessage the engine.Router consumes.
// The wecom-side InboundMessage is stashed in Raw so wecom_resolvers.go can
// access platform-specific fields.
//
// Routing identity:
//   - single → ChatType=p2p,  ChatID=userid,  SenderID=userid
//   - group  → ChatType=group, ChatID=chatid,  SenderID=from.userid
//
// A user @-mentioning the bot in a group is not distinguishable from a raw
// group message on the wire — WeCom only forwards to the bot when it was
// addressed, so any received group message counts as addressed.
//
// text is the agent-readable body the caller already resolved via ownText.
// It is passed in rather than recomputed because the caller has to know
// whether the message is routable at all before it gets here. The command
// source is a different string and is derived here from mc — see
// ownCommandSource for why they must not be the same one.
//
// botDisplayName is the bot's name in a chat, from the installation config. It
// is used for one thing: recognising where the sender's @-mention ends. Empty
// is fine and falls back to a whitespace heuristic; see stripLeadingMentions.
func channelMessageFromCallback(botID, botDisplayName string, mc aibotMsgCallback, text, reqID string) channel.InboundMessage {
	chatType := channel.ChatTypeP2P
	if strings.EqualFold(mc.ChatType, "group") {
		chatType = channel.ChatTypeGroup
	}
	senderID := mc.From.UserID
	chatID := mc.ChatID
	if chatType == channel.ChatTypeP2P && chatID == "" {
		// Some flavors set ChatID only for groups; fall back to the sender.
		chatID = senderID
	}

	// The command source is the sender's own words — ownCommandSource, not the
	// resolved body, so a 图文混排 whose first run is a screenshot still has
	// its "/issue …" on the first line the parser reads.
	//
	// In a group the @-mention IS how you reach the bot, so it arrives glued to
	// whatever was typed after it — "@Andrew /new" is a person asking for a
	// fresh session, not prose that happens to contain a word — and the
	// addressing comes off the front.
	//
	// Groups only. In a 1:1 nobody has to address the bot, so a leading "@" is
	// the sender naming a colleague they are talking ABOUT: "@李雷 /issue 帮我
	// 问问他" is a question, and stripping the name would turn it into a filed
	// issue nobody asked for plus, via SkipAgentRun below, no answer at all.
	//
	// For a plain text message this is mc.Text.Content, which is what p2p was
	// passing through before CommandText was set here; for a voice note it is
	// the transcript, so a spoken command is a command.
	command := mc.ownCommandSource()
	if chatType == channel.ChatTypeGroup {
		command = stripLeadingMentions(command, botDisplayName)
	}

	wm := InboundMessage{
		BotID:        botID,
		MsgID:        mc.MsgID,
		MsgType:      mc.MsgType,
		ChatType:     mc.ChatType,
		ChatID:       chatID,
		SenderUserID: senderID,
		Content:      text,
		ReqID:        reqID,
		Media:        mc.attachments(),
	}
	raw, _ := json.Marshal(wm)

	return channel.InboundMessage{
		EventID:        mc.MsgID,
		MessageID:      mc.MsgID,
		Type:           channelMsgType(mc.MsgType),
		Text:           text,
		AddressedToBot: true,
		// The sender's own words, with a group's addressing removed and the
		// media placeholders left out. Command classification is shared
		// (channel/message.go) and falls back to Text when this is empty — and
		// Text starts with the mention in a group, and with "[Image]" whenever
		// a screenshot came first, so on that fallback every such slash command
		// read as ordinary prose. Lark sets this from its command body
		// (feishu_channel.go:139) and Slack from its cleaned text
		// (slack/inbound.go:131); WeCom was the one adapter leaving it empty.
		CommandText: command,
		// A pure /issue command in WeCom should NOT trigger the
		// agent — the engine already creates the issue and the
		// OutboundReplier already sends "✅ 已创建 #N". Letting the agent
		// see "/issue foo" then produces a "I don't recognize this slash
		// command" reply that just clutters the conversation. wecom is
		// alone on this — Slack/Lark keep the historical "let the agent
		// see /issue and respond too" behaviour.
		//
		// Read off the same source the engine will parse, so a group /issue
		// behaves like the p2p one instead of filing the issue and then also
		// asking the agent about it. It has to be the same source: read off the
		// raw text instead and a p2p "@李雷 /issue …" would file an issue and
		// stay silent, which is the whole reason the strip above is gated; read
		// off the resolved body instead and a screenshot-then-"/issue" message
		// would skip the agent while the parser declined the placeholder line,
		// leaving the sender with neither an issue nor an answer.
		SkipAgentRun: isIssueCommand(command),
		Source: channel.Source{
			ChannelType: TypeWecom,
			ChatID:      chatID,
			ChatType:    chatType,
			SenderID:    senderID,
		},
		Raw: raw,
	}
}

// stripLeadingMentions removes the @-mentions a message opens with, which in a
// group chat is how the sender addresses the bot. WeCom puts them in the text
// and sends no mention list alongside it, so there is nothing to match against
// but the shape: an "@" at the very front, up to the next space.
//
// Group messages only — the caller gates it on chatType. Nobody addresses the
// bot in a 1:1, so the same "@" at the front there is a colleague's name in the
// sender's own sentence, and removing it would rewrite what they said.
//
// Only the front. A name further into the sentence is the sender talking ABOUT
// somebody — "@Andrew ask @李雷 about yesterday" is one instruction naming one
// colleague — and stripping that would quietly rewrite what they said.
//
// This feeds command classification only. The stored message keeps the text
// exactly as it arrived, so the transcript still shows who was addressed.
//
// Slack does the same thing with a regex over its mention token
// (slack/inbound.go cleanText); Feishu is handed an already-clean command body
// by the platform. WeCom was the one adapter passing the raw text through.
func stripLeadingMentions(s, botName string) string {
	for {
		trimmed := strings.TrimLeftFunc(s, unicode.IsSpace)
		if !strings.HasPrefix(trimmed, "@") {
			return trimmed
		}
		// Our own name first, matched whole. A display name may contain
		// spaces — "Multica Bot" is the obvious one — and cutting at the
		// first space would leave "Bot /new 重新分析", which is not a command,
		// so every slash command in that group would still be dropped.
		//
		// The name is not guessed. It comes from the installation config, set
		// when the bot was connected, because the callback carries no
		// structured mention list to read it from. Absent, the heuristic below
		// is what runs — correct for a one-word name, and what every
		// installation has until somebody fills the field in.
		if botName != "" && strings.HasPrefix(trimmed[1:], botName) {
			s = trimmed[1+len(botName):]
			continue
		}
		i := strings.IndexFunc(trimmed, unicode.IsSpace)
		if i < 0 {
			// The whole message is one mention and nothing else. There is no
			// command and no words — leave it, so an empty body is decided by
			// the caller rather than manufactured here.
			return trimmed
		}
		s = trimmed[i:]
	}
}

// isIssueCommand asks the engine's own parser instead of mirroring it. The
// mirror had drifted: it trimmed with strings.TrimSpace, which strips every
// Unicode space including U+3000 — the ideographic space a Chinese IME emits
// in full-width mode — while engine.ParseIssueCommand trims only " \t".
//
// So a p2p line opening with U+3000 read as a command here and as prose there:
// SkipAgentRun was set so no agent ran, and the parser declined so no issue was
// filed. The sender got nothing back and no error anywhere said why. Group
// messages reach this helper after their leading mentions are normalized, and
// must use the same parser too.
//
// A mirror of a parser is a parser. Delegating costs one allocation on a path
// that already does I/O, and removes the whole class.
func isIssueCommand(body string) bool {
	_, ok := engine.ParseIssueCommand(body)
	return ok
}

// channelMsgType maps the raw aibot msg_type onto the normalized enum.
func channelMsgType(wecomType string) channel.MsgType {
	switch strings.ToLower(wecomType) {
	case "text":
		return channel.MsgTypeText
	case "image":
		return channel.MsgTypeImage
	case "file":
		return channel.MsgTypeFile
	case "voice", "audio":
		return channel.MsgTypeAudio
	case "video":
		return channel.MsgTypeVideo
	case "mixed":
		// 图文混排: text runs and attachments interleaved. It maps to Text
		// because the message IS text once ownText has rendered it — runs in
		// composition order, each attachment standing in as its placeholder —
		// and the attachments travel separately as MediaRefs, exactly as they
		// do for Lark's `post`, the same shape under another name:
		// lark/feishu_channel.go:167 maps post → MsgTypeText while
		// lark/media_ingest.go:272 pulls that same post's img/media spans.
		//
		// This line previously read Unknown, and the comment there was right
		// for the code that existed: dispatchFrame routed only the kinds
		// that arrived as words, so a mixed message never reached
		// normalization and calling it Text would have claimed a routing that
		// did not happen. That claim is what this change makes true. The two
		// must land together — mapping to Text without the routing is the
		// dead, misleading mapping the old comment warned about.
		return channel.MsgTypeText
	default:
		// A kind the adapter cannot read at all. dispatchFrame answers it
		// with the unsupported-kind receipt and stops, so this normalization
		// is never reached for one.
		return channel.MsgTypeUnknown
	}
}

// ---- outbound helpers ----

// subscribeBody builds an aibot_subscribe body. The server responds with an
// echoed req_id and errcode 0 on success.
func subscribeBody(botID, secret string) map[string]any {
	return map[string]any{"bot_id": botID, "secret": secret}
}

// sendMsgTextBody builds an aibot_send_msg body carrying plain-text
// content. aibot_send_msg's supported msgtypes are markdown and
// template_card only — text is NOT accepted on this cmd (contrast
// aibot_respond_msg, which does accept text). We therefore ship as
// markdown; the WeCom client renders plain text through the markdown
// path without any special escaping. chatType is 1 for single, 2 for
// group.
func sendMsgTextBody(chatID string, chatType int, content string) (map[string]any, error) {
	if chatID == "" {
		return nil, errors.New("wecom: send_msg requires chat_id")
	}
	if chatType != chatTypeSingleInt && chatType != chatTypeGroupInt {
		return nil, errors.New("wecom: send_msg chat_type must be 1 (single) or 2 (group)")
	}
	return map[string]any{
		"chatid":    chatID,
		"chat_type": chatType,
		"msgtype":   "markdown",
		"markdown":  map[string]string{"content": content},
	}, nil
}

// aibotChatTypeFromChannel maps the engine's ChatType enum to the int the
// aibot_send_msg body wants.
func aibotChatTypeFromChannel(t channel.ChatType) int {
	if t == channel.ChatTypeGroup {
		return chatTypeGroupInt
	}
	return chatTypeSingleInt
}
