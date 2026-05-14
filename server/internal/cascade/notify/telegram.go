package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TelegramChannel posts cascade events as a Telegram message via the
// Bot API. Bot token in env (MULTICA_CASCADE_TELEGRAM_BOT_TOKEN); chat
// id in env (MULTICA_CASCADE_TELEGRAM_CHAT_ID) is the destination —
// typically the user's private chat with the bot or a small alert
// channel. Multi-user routing is a follow-up; PR6 ships single-target.
type TelegramChannel struct {
	apiBaseURL string // override for tests; defaults to https://api.telegram.org
	botToken   string
	chatID     string
	httpClient *http.Client
}

// NewTelegramChannel constructs a Telegram channel. apiBaseURL may be
// empty in production — defaults to the public Bot API endpoint.
// Tests pass a httptest.NewServer().URL to inspect the outbound
// requests without hitting telegram.org.
func NewTelegramChannel(apiBaseURL, botToken, chatID string, client *http.Client) *TelegramChannel {
	if apiBaseURL == "" {
		apiBaseURL = "https://api.telegram.org"
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &TelegramChannel{
		apiBaseURL: strings.TrimRight(apiBaseURL, "/"),
		botToken:   botToken,
		chatID:     chatID,
		httpClient: client,
	}
}

// Name implements Channel.
func (t *TelegramChannel) Name() string { return "telegram" }

// telegramRequest mirrors sendMessage's JSON body. parse_mode is set
// to "MarkdownV2" so the formatting we emit (escaped) renders.
// MarkdownV2 has strict escaping rules — we keep messages narrow to
// avoid escape hell (no inline links, no nested markup).
type telegramRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

type telegramResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

// Send posts the event as a Telegram message. Returns wrapped
// ErrChannelTransient on 5xx so the Bridge retry path differentiates
// them from permanent errors (4xx, bad token).
func (t *TelegramChannel) Send(ctx context.Context, e Event) error {
	if t.botToken == "" || t.chatID == "" {
		return fmt.Errorf("telegram: missing bot token or chat id")
	}

	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBaseURL, url.PathEscape(t.botToken))
	text := buildTelegramText(e)
	raw, err := json.Marshal(telegramRequest{
		ChatID:    t.chatID,
		Text:      text,
		ParseMode: "MarkdownV2",
	})
	if err != nil {
		return fmt.Errorf("telegram: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: post: %w (%w)", err, ErrChannelTransient)
	}
	defer resp.Body.Close()

	var body telegramResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)

	switch {
	case body.OK && resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 500:
		return fmt.Errorf("telegram: status %d %q (%w)", resp.StatusCode, body.Description, ErrChannelTransient)
	default:
		return fmt.Errorf("telegram: status %d %q", resp.StatusCode, body.Description)
	}
}

// buildTelegramText formats the event with MarkdownV2 escaping
// applied. We use a narrow message shape (one line of bold headline +
// one optional paragraph) so MarkdownV2's surprising escaping rules
// don't bite us. Inline URLs are EmittedAsPlainText — Telegram auto-
// links them and the bot's preview render handles the rest.
func buildTelegramText(e Event) string {
	var b strings.Builder
	b.WriteString("*")
	b.WriteString(escapeMD(slackVerbFor(e.Type))) // reuse the verb table
	b.WriteString("*")
	id := e.IssuePUL
	if id == "" {
		id = e.IssueID
	}
	if id != "" {
		b.WriteString(" — ")
		b.WriteString(escapeMD(id))
	}
	if e.PRNumber > 0 {
		b.WriteString(escapeMD(fmt.Sprintf(" PR #%d", e.PRNumber)))
	}
	if e.IssueTitle != "" {
		b.WriteString("\n")
		b.WriteString(escapeMD(e.IssueTitle))
	}
	if e.HumanContext != "" {
		b.WriteString("\n\n")
		b.WriteString(escapeMD(e.HumanContext))
	}
	if e.PRURL != "" {
		b.WriteString("\n\n")
		b.WriteString(escapeMD(e.PRURL))
	}
	return b.String()
}

// escapeMD escapes the characters MarkdownV2 reserves. Per Telegram
// docs: _ * [ ] ( ) ~ ` > # + - = | { } . !
// Backslash-prefix is the only acceptable escape.
func escapeMD(s string) string {
	const reserved = "_*[]()~`>#+-=|{}.!"
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if strings.ContainsRune(reserved, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
