package weixin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The protocol structures mirror Tencent's maintained reference channel:
// https://github.com/Tencent/openclaw-weixin/tree/main/src/api
//
// Keep them intentionally narrow. Unknown response fields are ignored by
// encoding/json, which lets the adapter tolerate additive upstream changes
// while contract tests catch changes to fields Multica actually depends on.

const (
	defaultBaseURL    = "https://ilinkai.weixin.qq.com"
	defaultBotType    = "3"
	defaultILinkAppID = "bot"
	// The wire headers are pinned to the Tencent reference version whose
	// fixtures this first client slice implements. Bump both values together
	// when contract tests are updated against a newer reference release.
	defaultChannelVersion = "2.4.6"
	defaultClientVersion  = "132102" // 2<<16 | 4<<8 | 6
	defaultBotAgent       = "Multica"
	defaultRequestTimeout = 15 * time.Second
	defaultPollTimeout    = 35 * time.Second
	maxPollTimeout        = 60 * time.Second
	minPollTimeout        = 5 * time.Second
	maxResponseBytes      = 2 << 20
)

const (
	messageTypeUser = 1
	messageTypeBot  = 2

	messageItemTypeText  = 1
	messageItemTypeVoice = 3

	messageStateFinish = 2
)

// ErrReauthorizationRequired means iLink rejected the bot session as stale.
// The installation layer should mark the connection degraded and ask the user
// to scan a fresh QR code instead of reconnecting forever.
var ErrReauthorizationRequired = errors.New("weixin: reauthorization required")

// APIError is a successful HTTP response whose iLink ret/errcode reports a
// protocol failure. It never contains credentials or request payloads.
type APIError struct {
	Operation string
	Ret       int
	ErrCode   int
	Message   string
}

func (e *APIError) Error() string {
	code := e.ErrCode
	if code == 0 {
		code = e.Ret
	}
	if e.Message == "" {
		return fmt.Sprintf("weixin: %s failed (code=%d)", e.Operation, code)
	}
	return fmt.Sprintf("weixin: %s failed (code=%d message=%q)", e.Operation, code, e.Message)
}

func (e *APIError) Unwrap() error {
	if e.Ret == -14 || e.ErrCode == -14 {
		return ErrReauthorizationRequired
	}
	return nil
}

type baseInfo struct {
	ChannelVersion string `json:"channel_version,omitempty"`
	BotAgent       string `json:"bot_agent,omitempty"`
}

// QRCode is the opaque polling token plus the content that the frontend should
// render as a QR code. The opaque Code itself must never be rendered or logged.
type QRCode struct {
	Code         string `json:"qrcode"`
	ImageContent string `json:"qrcode_img_content"`
}

// QRStatus is the complete state returned by get_qrcode_status. The caller
// decides whether to request a verification code, follow an allowlisted
// redirect, finish installation, or expire the local login session.
type QRStatus struct {
	Status       string `json:"status"`
	BotToken     string `json:"bot_token,omitempty"`
	BotID        string `json:"ilink_bot_id,omitempty"`
	BaseURL      string `json:"baseurl,omitempty"`
	ScannerID    string `json:"ilink_user_id,omitempty"`
	RedirectHost string `json:"redirect_host,omitempty"`
}

func (s QRStatus) confirmed() bool {
	return s.Status == "confirmed"
}

// MessageItem is one content item inside an iLink message. Media fields are
// intentionally omitted from this first text slice; Voice.Text is retained so
// a server-provided voice transcript can be treated as text without downloading
// or decoding SILK audio.
type MessageItem struct {
	Type  int        `json:"type,omitempty"`
	MsgID string     `json:"msg_id,omitempty"`
	Text  *TextItem  `json:"text_item,omitempty"`
	Voice *VoiceItem `json:"voice_item,omitempty"`
}

type TextItem struct {
	Text string `json:"text,omitempty"`
}

type VoiceItem struct {
	Text string `json:"text,omitempty"`
}

// Message mirrors the stable subset of WeixinMessage used for text routing.
// MessageID stays json.RawMessage because upstream has emitted numeric ids and
// fixtures from older bridges have used strings. stableMessageID accepts both.
type Message struct {
	Seq          int64           `json:"seq,omitempty"`
	MessageID    json.RawMessage `json:"message_id,omitempty"`
	FromUserID   string          `json:"from_user_id,omitempty"`
	ToUserID     string          `json:"to_user_id,omitempty"`
	ClientID     string          `json:"client_id,omitempty"`
	CreateTimeMS int64           `json:"create_time_ms,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	GroupID      string          `json:"group_id,omitempty"`
	MessageType  int             `json:"message_type,omitempty"`
	MessageState int             `json:"message_state,omitempty"`
	Items        []MessageItem   `json:"item_list,omitempty"`
	ContextToken string          `json:"context_token,omitempty"`
	RunID        string          `json:"run_id,omitempty"`
}

type getUpdatesRequest struct {
	Cursor   string   `json:"get_updates_buf"`
	BaseInfo baseInfo `json:"base_info"`
}

// Updates is one long-poll response. Cursor is opaque and must be persisted
// only after all Messages in the response are durably handled.
type Updates struct {
	Ret            int       `json:"ret,omitempty"`
	ErrCode        int       `json:"errcode,omitempty"`
	ErrorMessage   string    `json:"errmsg,omitempty"`
	Messages       []Message `json:"msgs,omitempty"`
	Cursor         string    `json:"get_updates_buf,omitempty"`
	LongPollMillis int64     `json:"longpolling_timeout_ms,omitempty"`
}

func (u Updates) pollTimeout(fallback time.Duration) time.Duration {
	if fallback <= 0 {
		fallback = defaultPollTimeout
	}
	if u.LongPollMillis <= 0 {
		return fallback
	}
	d := time.Duration(u.LongPollMillis) * time.Millisecond
	if d < minPollTimeout {
		return minPollTimeout
	}
	if d > maxPollTimeout {
		return maxPollTimeout
	}
	return d
}

type sendMessageRequest struct {
	Message  Message  `json:"msg"`
	BaseInfo baseInfo `json:"base_info"`
}

type apiResponse struct {
	Ret          int    `json:"ret,omitempty"`
	ErrCode      int    `json:"errcode,omitempty"`
	ErrorMessage string `json:"errmsg,omitempty"`
}

func validateQRStatus(status QRStatus) error {
	if !status.confirmed() {
		return nil
	}
	missing := make([]string, 0, 3)
	if strings.TrimSpace(status.BotToken) == "" {
		missing = append(missing, "bot_token")
	}
	if strings.TrimSpace(status.BotID) == "" {
		missing = append(missing, "ilink_bot_id")
	}
	if strings.TrimSpace(status.BaseURL) == "" {
		missing = append(missing, "baseurl")
	}
	if len(missing) > 0 {
		return fmt.Errorf("weixin: confirmed QR response missing %s", strings.Join(missing, ", "))
	}
	return nil
}
