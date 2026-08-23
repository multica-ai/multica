package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// User-facing copy the bot speaks in Telegram. English, matching the Slack
// and DingTalk adapters word for word wherever they say the same thing.
//
// The language is chosen per channel by who is on the other end, and this is
// the decision record so the next person does not "fix" it back (MUL-6509,
// #7349). WeCom and Lark are Chinese-market products — WeCom is China-only
// and their adapters keep Chinese copy on purpose. Telegram is blocked in
// mainland China and reaches everyone else, so a Chinese-first bot there
// speaks the wrong language to almost every user it has, starting with the
// binding prompt they must act on before anything else works.
//
// English rather than a locale lookup because this bot speaks before it knows
// who it is speaking to: the binding prompt is sent to an unbound sender, and
// user.language is only ever written by the settings language switcher, so it
// is NULL for anyone who never opened it. English is the product default
// (DEFAULT_LOCALE in packages/core/i18n/types.ts).
//
// The binding prompt carries the redeem link.
const (
	msgAgentOffline     = "⚠️ The agent is offline right now. Your message was received and will be handled once it's back online."
	msgAgentArchived    = "⚠️ This agent has been archived and can't respond. Please contact your workspace admin."
	msgUnsupportedType  = "Sorry, I can't handle this kind of message yet. Please send text."
	msgBindingGroupHint = "Please message me in a direct chat first, then link your Multica account."
)

// maxMessageUnits caps one outbound sendMessage body. Telegram hard-caps a
// message at 4096 UTF-16 code units after entity parsing; 3500 units leaves
// headroom for the markdown conversion while counting astral characters (such
// as emoji) correctly.
const maxMessageUnits = 3500

// sender posts agent replies back to Telegram via sendMessage. Outbound half
// only; the installation identity is resolved per message by the Router.
type sender struct {
	api    *botAPI
	logger *slog.Logger
}

func newSender(api *botAPI, logger *slog.Logger) *sender {
	if logger == nil {
		logger = slog.Default()
	}
	return &sender{api: api, logger: logger}
}

// Send delivers a text reply, converting Markdown to Telegram HTML and
// chunking under the per-message cap. The returned SendResult carries the id
// of the LAST posted chunk. A rejected HTML payload falls back to plain text
// so a conversion edge case can never eat the reply.
func (s *sender) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	if s.api == nil {
		return channel.SendResult{}, errors.New("telegram: api client not configured")
	}
	chatID, err := strconv.ParseInt(out.ChatID, 10, 64)
	if err != nil {
		return channel.SendResult{}, fmt.Errorf("telegram: bad chat id %q: %w", out.ChatID, err)
	}
	var threadID int64
	if out.ThreadID != "" {
		threadID, _ = strconv.ParseInt(out.ThreadID, 10, 64)
	}
	var replyTo int64
	if out.ReplyTo != "" {
		replyTo = parseMessageRef(out.ReplyTo)
	}

	var lastID string
	for _, chunk := range chunkMessage(out.Text, maxMessageUnits) {
		var reply *replyParameters
		if replyTo != 0 {
			reply = &replyParameters{MessageID: replyTo, AllowSendingWithoutReply: true}
		}
		m, err := sendMessageWithRetryAfter(ctx, s.api, sendMessageParams{
			ChatID:          chatID,
			Text:            formatHTML(chunk),
			ParseMode:       "HTML",
			MessageThreadID: threadID,
			ReplyParameters: reply,
		})
		if err != nil {
			// HTML rejection fallback: send the raw markdown as plain text. Do
			// not retry transport or unrelated API errors: the first request may
			// already have reached Telegram, and retrying could duplicate it.
			if !isHTMLParseError(err) {
				return channel.SendResult{}, fmt.Errorf("telegram: sendMessage: %w", err)
			}
			m, err = sendMessageWithRetryAfter(ctx, s.api, sendMessageParams{
				ChatID:          chatID,
				Text:            chunk,
				MessageThreadID: threadID,
				ReplyParameters: reply,
			})
			if err != nil {
				return channel.SendResult{}, fmt.Errorf("telegram: sendMessage: %w", err)
			}
		}
		lastID = messageKey(chatID, m.MessageID)
		replyTo = 0 // only the first chunk quotes
	}
	return channel.SendResult{MessageID: lastID}, nil
}

// parseMessageRef extracts the numeric message id from either a bare id or
// the composite "chat:message" key this adapter stores.
func parseMessageRef(ref string) int64 {
	if _, after, ok := strings.Cut(ref, ":"); ok {
		ref = after
	}
	id, _ := strconv.ParseInt(ref, 10, 64)
	return id
}

// chunkMessage splits text into <=maxUnits UTF-16 code-unit pieces on rune
// boundaries, preferring newline breaks so code blocks and paragraphs split
// cleanly.
func chunkMessage(text string, maxUnits int) []string {
	runes := []rune(text)
	if maxUnits <= 0 || utf16Units(text) <= maxUnits {
		return []string{text}
	}
	var chunks []string
	for len(runes) > 0 {
		n := 0
		end := 0
		for i, r := range runes {
			units := 1
			if r > 0xFFFF {
				units = 2
			}
			if n+units > maxUnits {
				break
			}
			n += units
			end = i + 1
		}
		if end == 0 {
			end = 1
		}
		// Prefer the last newline in the window, but only when it leaves a
		// substantial first chunk rather than producing tiny fragments.
		if i := lastIndexRune(runes[:end], '\n'); i >= 0 && utf16Units(string(runes[:i])) > maxUnits/2 {
			end = i + 1
		}
		chunks = append(chunks, strings.TrimRight(string(runes[:end]), "\n"))
		runes = runes[end:]
	}
	return chunks
}

func utf16Units(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func isHTMLParseError(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) || ae.Code != http.StatusBadRequest {
		return false
	}
	description := strings.ToLower(ae.Description)
	return strings.Contains(description, "parse entities") ||
		strings.Contains(description, "unsupported start tag") ||
		strings.Contains(description, "can't find end tag")
}

func lastIndexRune(rs []rune, r rune) int {
	for i := len(rs) - 1; i >= 0; i-- {
		if rs[i] == r {
			return i
		}
	}
	return -1
}
