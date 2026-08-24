package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// This file is the minimal Telegram Bot API surface the adapter needs:
// getMe (install-time validation), getUpdates (inbound long polling),
// sendMessage / editMessageText (outbound + streaming), sendChatAction
// (typing indicator). It is a thin hand-rolled client over net/http — the
// Bot API is plain JSON-over-HTTPS, so no SDK dependency is warranted.

// defaultAPIBase is the production Bot API host. Tests point apiBase at an
// httptest server.
const defaultAPIBase = "https://api.telegram.org"

// longPollTimeoutSecs is the getUpdates server-side hold. 50s stays under
// common 60s proxy/LB idle timeouts while keeping the request count low.
const longPollTimeoutSecs = 50

// ErrConflict surfaces a 409 from getUpdates: the same bot token is being
// polled by another consumer (a second Multica instance, another worktree, or
// a foreign process). The polling loop treats it as fatal for the attempt and
// reports a precise message so the operator sees "bot polled elsewhere"
// instead of a silent message tug-of-war.
var ErrConflict = errors.New("telegram: bot is already being polled by another instance (409 conflict)")

// requestError deliberately omits the request URL from Error(). Bot API URLs
// contain the bot token, while net/http transport errors can include that URL.
// Unwrap preserves cancellation and transport classification without making
// the credential loggable through the outer error string.
type requestError struct {
	method string
	cause  error
}

func (e *requestError) Error() string { return fmt.Sprintf("telegram: %s request failed", e.method) }

func (e *requestError) Unwrap() error { return e.cause }

// apiError is a non-OK Bot API response.
type apiError struct {
	Code        int
	Description string
	// RetryAfter is Telegram's mandated backoff (seconds) on a 429.
	RetryAfter int
}

func (e *apiError) Error() string {
	return fmt.Sprintf("telegram api: %d %s", e.Code, e.Description)
}

// isRetryAfter reports the 429 backoff, if this error carries one.
func retryAfter(err error) (time.Duration, bool) {
	var ae *apiError
	if errors.As(err, &ae) && ae.Code == http.StatusTooManyRequests {
		secs := ae.RetryAfter
		if secs <= 0 {
			secs = 1
		}
		return time.Duration(secs) * time.Second, true
	}
	return 0, false
}

// botAPI is one bot's API client.
type botAPI struct {
	base   string // API host, no trailing slash
	token  string
	client *http.Client
}

func newBotAPI(base, token string, client *http.Client) *botAPI {
	if base == "" {
		base = defaultAPIBase
	}
	if client == nil {
		client = &http.Client{Timeout: 65 * time.Second}
	}
	return &botAPI{base: base, token: token, client: client}
}

// envelope is the Bot API response wrapper.
type envelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// call POSTs one Bot API method and decodes the result into out (skipped when
// out is nil). API-level failures return *apiError; a 409 on getUpdates is
// mapped by the caller.
func (a *botAPI) call(ctx context.Context, method string, params any, out any) error {
	var body io.Reader
	if params != nil {
		buf, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("telegram: encode %s params: %w", method, err)
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/bot"+a.token+"/"+method, body)
	if err != nil {
		return fmt.Errorf("telegram: build %s request: %w", method, err)
	}
	if params != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return &requestError{method: method, cause: err}
	}
	defer resp.Body.Close()
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("telegram: decode %s response (http %d): %w", method, resp.StatusCode, err)
	}
	if !env.OK {
		ae := &apiError{Code: env.ErrorCode, Description: env.Description}
		if env.Parameters != nil {
			ae.RetryAfter = env.Parameters.RetryAfter
		}
		return ae
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("telegram: decode %s result: %w", method, err)
		}
	}
	return nil
}

// User is the Bot API User object (getMe result / message sender).
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

// Chat is the Bot API Chat object.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"` // "private", "group", "supergroup", "channel"
}

// Message is the subset of the Bot API Message object the adapter reads.
type Message struct {
	MessageID       int64           `json:"message_id"`
	From            *User           `json:"from"`
	Chat            Chat            `json:"chat"`
	Date            int64           `json:"date"`
	Text            string          `json:"text"`
	Entities        []MessageEntity `json:"entities,omitempty"`
	Caption         string          `json:"caption,omitempty"`
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	ReplyToMessage  *Message        `json:"reply_to_message,omitempty"`
	MessageThreadID int64           `json:"message_thread_id,omitempty"`
	IsTopicMessage  bool            `json:"is_topic_message,omitempty"`
	Photo           []any           `json:"photo,omitempty"`
	Document        *struct {
		FileName string `json:"file_name"`
	} `json:"document,omitempty"`
	Voice   *struct{} `json:"voice,omitempty"`
	Video   *struct{} `json:"video,omitempty"`
	Sticker *struct{} `json:"sticker,omitempty"`
}

// MessageEntity is the Bot API's structured annotation for mentions,
// commands, links, and formatting. Offset and Length are UTF-16 code units,
// not UTF-8 byte offsets or Go rune indexes.
type MessageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

// Update is the getUpdates envelope entry. Only new messages are consumed;
// edits, channel posts, and callback queries are ignored in v1.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// GetMe validates the token and returns the bot's own identity.
func (a *botAPI) GetMe(ctx context.Context) (User, error) {
	var u User
	err := a.call(ctx, "getMe", nil, &u)
	return u, err
}

// WebhookInfo is the subset of the Bot API webhook status needed before
// starting a long-polling installation.
type WebhookInfo struct {
	URL                string `json:"url"`
	PendingUpdateCount int    `json:"pending_update_count"`
}

// GetWebhookInfo detects a webhook that would make getUpdates unavailable.
func (a *botAPI) GetWebhookInfo(ctx context.Context) (WebhookInfo, error) {
	var info WebhookInfo
	err := a.call(ctx, "getWebhookInfo", nil, &info)
	return info, err
}

type getUpdatesParams struct {
	Offset         int64    `json:"offset,omitempty"`
	Timeout        int      `json:"timeout"`
	AllowedUpdates []string `json:"allowed_updates"`
}

// GetUpdates long-polls for new updates. A 409 is translated to ErrConflict.
func (a *botAPI) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	var updates []Update
	err := a.call(ctx, "getUpdates", getUpdatesParams{
		Offset:  offset,
		Timeout: longPollTimeoutSecs,
		// Restrict the stream to plain messages: fewer wakeups, and edits /
		// reactions / channel posts never enter the pipeline.
		AllowedUpdates: []string{"message"},
	}, &updates)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Code == http.StatusConflict {
			return nil, ErrConflict
		}
		return nil, err
	}
	return updates, nil
}

type sendMessageParams struct {
	ChatID          int64            `json:"chat_id"`
	Text            string           `json:"text"`
	ParseMode       string           `json:"parse_mode,omitempty"`
	MessageThreadID int64            `json:"message_thread_id,omitempty"`
	ReplyParameters *replyParameters `json:"reply_parameters,omitempty"`
}

// replyParameters is Telegram's current quote-reply shape. The legacy
// reply_to_message_id field is no longer part of sendMessage.
type replyParameters struct {
	MessageID                int64 `json:"message_id"`
	AllowSendingWithoutReply bool  `json:"allow_sending_without_reply,omitempty"`
}

// SendMessage posts a message. HTML parse mode is used by callers that send
// converted markdown; plain calls leave parseMode empty.
func (a *botAPI) SendMessage(ctx context.Context, p sendMessageParams) (Message, error) {
	var m Message
	err := a.call(ctx, "sendMessage", p, &m)
	return m, err
}

// sendMessageWithRetryAfter honors Telegram's explicit 429 backoff once. It
// deliberately does not retry transport or other API errors because a lost
// response can mean Telegram already accepted the message.
func sendMessageWithRetryAfter(ctx context.Context, a *botAPI, p sendMessageParams) (Message, error) {
	m, err := a.SendMessage(ctx, p)
	if wait, ok := retryAfter(err); ok {
		if !sleepCtx(ctx, wait) {
			return Message{}, ctx.Err()
		}
		return a.SendMessage(ctx, p)
	}
	return m, err
}

type editMessageTextParams struct {
	ChatID    int64  `json:"chat_id"`
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// EditMessageText replaces a previously sent message's text — the streaming
// primitive. "message is not modified" errors are swallowed by the caller.
func (a *botAPI) EditMessageText(ctx context.Context, p editMessageTextParams) error {
	return a.call(ctx, "editMessageText", p, nil)
}

// SendChatAction shows the native "typing…" indicator for ~5s in the same
// forum topic as the triggering message, when applicable.
func (a *botAPI) SendChatAction(ctx context.Context, chatID, messageThreadID int64) error {
	return a.call(ctx, "sendChatAction", struct {
		ChatID          int64  `json:"chat_id"`
		Action          string `json:"action"`
		MessageThreadID int64  `json:"message_thread_id,omitempty"`
	}{
		ChatID: chatID, Action: "typing", MessageThreadID: messageThreadID,
	}, nil)
}
