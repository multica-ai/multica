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
	// Image / voice / file / video / mixed have their own fields; we do
	// not surface them yet — MsgType=="text" is the only case we route.
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

	// Content is the human-readable text body when MsgType == "text";
	// empty for media / events. The cross-platform envelope's Text field
	// is populated from this.
	Content string `json:"content,omitempty"`

	// ReqID is the frame req_id the server sent this message with. We
	// keep it so a future aibot_respond_msg (5s window) can echo it back;
	// iteration 1 uses aibot_send_msg unconditionally and does not need it.
	ReqID string `json:"req_id,omitempty"`
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
// botDisplayName is the bot's name in a chat, from the installation config. It
// is used for one thing: recognising where the sender's @-mention ends. Empty
// is fine and falls back to a whitespace heuristic; see stripLeadingMentions.
func channelMessageFromCallback(botID, botDisplayName string, mc aibotMsgCallback, reqID string) channel.InboundMessage {
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

	// The command source is the sender's own words. In a group the @-mention IS
	// how you reach the bot, so it arrives glued to whatever was typed after it
	// — "@Andrew /new" is a person asking for a fresh session, not prose that
	// happens to contain a word — and the addressing comes off the front.
	//
	// Groups only. In a 1:1 nobody has to address the bot, so a leading "@" is
	// the sender naming a colleague they are talking ABOUT: "@李雷 /issue 帮我
	// 问问他" is a question, and stripping the name would turn it into a filed
	// issue nobody asked for plus, via SkipAgentRun below, no answer at all.
	// Passing the raw content through keeps p2p exactly where it was before
	// CommandText was set here.
	command := mc.Text.Content
	if chatType == channel.ChatTypeGroup {
		command = stripLeadingMentions(mc.Text.Content, botDisplayName)
	}

	wm := InboundMessage{
		BotID:        botID,
		MsgID:        mc.MsgID,
		MsgType:      mc.MsgType,
		ChatType:     mc.ChatType,
		ChatID:       chatID,
		SenderUserID: senderID,
		Content:      mc.Text.Content,
		ReqID:        reqID,
	}
	raw, _ := json.Marshal(wm)

	return channel.InboundMessage{
		EventID:        mc.MsgID,
		MessageID:      mc.MsgID,
		Type:           channelMsgType(mc.MsgType),
		Text:           mc.Text.Content,
		AddressedToBot: true,
		// The sender's own words, with a group's addressing removed. Command
		// classification is shared (channel/message.go) and falls back to Text
		// when this is empty — and in a group Text starts with the mention, so
		// every slash command read as ordinary prose. Lark sets this from its
		// command body (feishu_channel.go:139) and Slack from its cleaned text
		// (slack/inbound.go:131); WeCom was the one adapter leaving it empty.
		// In a p2p chat this is Text verbatim, which is what the fallback was
		// already producing.
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
		// stay silent, which is the whole reason the strip above is gated.
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

// isIssueCommand mirrors engine.ParseIssueCommand's front-of-body detection
// without materializing the parsed struct — we only need the yes/no. A pure
// /issue command starts at the first non-empty line, "/issue" as a whole
// token, optionally followed by whitespace and the title.
func isIssueCommand(body string) bool {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if line == "/issue" {
			return true
		}
		if strings.HasPrefix(line, "/issue ") || strings.HasPrefix(line, "/issue\t") {
			return true
		}
		return false
	}
	return false
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
	default:
		// Includes "mixed" (text + media): dispatchFrame only routes msgtype
		// == "text", so anything else is answered with the text-only receipt
		// and never reaches this normalization. Kept as Unknown rather than
		// mapping "mixed" → Text, which was dead and implied mixed messages
		// were routed as text when they are not.
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
