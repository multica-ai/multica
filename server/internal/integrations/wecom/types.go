package wecom

import "encoding/json"

// Wire command names for the WeCom long-connection JSON protocol.
const (
	CmdSubscribe      = "aibot_subscribe"
	CmdMsgCallback    = "aibot_msg_callback"
	CmdEventCallback  = "aibot_event_callback"
	CmdSendMsg        = "aibot_send_msg"
	CmdRespondWelcome = "aibot_respond_welcome_msg"
	CmdPing           = "ping"
)

// Event type discriminators carried in aibot_event_callback bodies.
const (
	EventTypeEnterChat    = "enter_chat"
	EventTypeTemplateCard = "template_card_event"
	EventTypeFeedback     = "feedback_event"
	EventTypeDisconnected = "disconnected_event"
)

// ChatType values on inbound callbacks.
const (
	ChatTypeSingle = "single"
	ChatTypeGroup  = "group"
)

// Frame is the top-level WeCom long-connection JSON envelope. Requests carry
// cmd + headers + body; responses echo headers.req_id and set errcode/errmsg.
type Frame struct {
	Cmd     string          `json:"cmd,omitempty"`
	Headers FrameHeaders    `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
	ErrCode int             `json:"errcode,omitempty"`
	ErrMsg  string          `json:"errmsg,omitempty"`
}

// FrameHeaders holds per-frame metadata. req_id correlates requests with
// responses and must be preserved when replying to a callback.
type FrameHeaders struct {
	ReqID string `json:"req_id"`
}

// Response is the generic success/error envelope returned for subscribe,
// ping, send, and welcome commands.
type Response struct {
	Headers FrameHeaders `json:"headers"`
	ErrCode int          `json:"errcode"`
	ErrMsg  string       `json:"errmsg"`
}

// SubscribeBody is the body for CmdSubscribe (bot_id + secret).
type SubscribeBody struct {
	BotID  string `json:"bot_id"`
	Secret string `json:"secret"`
}

// MsgCallbackBody is the body for CmdMsgCallback inbound message pushes.
type MsgCallbackBody struct {
	MsgID    string     `json:"msgid"`
	AIBotID  string     `json:"aibotid"`
	ChatID   string     `json:"chatid,omitempty"`
	ChatType string     `json:"chattype"`
	From     MsgFrom    `json:"from"`
	MsgType  string     `json:"msgtype"`
	Text     *TextBody  `json:"text,omitempty"`
	Mixed    *MixedBody `json:"mixed,omitempty"`
	Voice    *VoiceBody `json:"voice,omitempty"`
	Image    *ImageBody `json:"image,omitempty"`
	File     *FileBody  `json:"file,omitempty"`
	Video    *VideoBody `json:"video,omitempty"`
	Quote    *QuoteBody `json:"quote,omitempty"`
}

// MixedBody carries a mixed message's ordered parts (spec §5.5).
type MixedBody struct {
	MsgItem []MixedItem `json:"msg_item"`
}

// MixedItem is one element inside a mixed message.
type MixedItem struct {
	MsgType string     `json:"msgtype"`
	Text    *TextBody  `json:"text,omitempty"`
	Image   *ImageBody `json:"image,omitempty"`
}

// VoiceBody carries voice-to-text content (spec §5.5: already transcribed).
type VoiceBody struct {
	Content string `json:"content"`
}

// ImageBody is a reference to an inbound image (media path is Task 12).
type ImageBody struct {
	URL    string `json:"url,omitempty"`
	AESKey string `json:"aeskey,omitempty"`
}

// FileBody is a reference to an inbound file attachment.
type FileBody struct {
	URL      string `json:"url,omitempty"`
	AESKey   string `json:"aeskey,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// VideoBody is a reference to an inbound video attachment.
type VideoBody struct {
	URL    string `json:"url,omitempty"`
	AESKey string `json:"aeskey,omitempty"`
}

// QuoteBody is optional reply context on text/mixed callbacks (spec §5.5).
// WeCom does not supply a quoted message id — only the quoted content.
type QuoteBody struct {
	MsgType string     `json:"msgtype"`
	Text    *TextBody  `json:"text,omitempty"`
	Voice   *VoiceBody `json:"voice,omitempty"`
	Image   *ImageBody `json:"image,omitempty"`
	File    *FileBody  `json:"file,omitempty"`
	Video   *VideoBody `json:"video,omitempty"`
}

// MsgFrom identifies the sender on an inbound callback.
type MsgFrom struct {
	UserID string `json:"userid"`
}

// TextBody is the text payload on a text or mixed message callback.
type TextBody struct {
	Content string `json:"content"`
}

// EventCallbackBody is the body for CmdEventCallback inbound event pushes,
// including enter_chat and disconnected_event.
type EventCallbackBody struct {
	MsgID      string    `json:"msgid"`
	CreateTime int64     `json:"create_time"`
	AIBotID    string    `json:"aibotid"`
	ChatID     string    `json:"chatid,omitempty"`
	ChatType   string    `json:"chattype,omitempty"`
	From       *MsgFrom  `json:"from,omitempty"`
	MsgType    string    `json:"msgtype"`
	Event      EventBody `json:"event"`
}

// EventBody carries the event discriminator on CmdEventCallback frames.
type EventBody struct {
	EventType string `json:"eventtype"`
}

// SendMsgBody is the body for CmdSendMsg proactive outbound pushes.
type SendMsgBody struct {
	ChatID   string        `json:"chatid"`
	ChatType int           `json:"chat_type,omitempty"`
	MsgType  string        `json:"msgtype"`
	Markdown *MarkdownBody `json:"markdown,omitempty"`
}

// MarkdownBody is markdown content on outbound send/welcome frames.
type MarkdownBody struct {
	Content string `json:"content"`
}

// WelcomeMsgBody is the body for CmdRespondWelcome replies to enter_chat.
type WelcomeMsgBody struct {
	MsgType  string        `json:"msgtype"`
	Markdown *MarkdownBody `json:"markdown,omitempty"`
}
