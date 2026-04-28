package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/multica-ai/multica-connect/channel"
)

// Adapter implements channel.Channel for Telegram.
type Adapter struct {
	token   string
	apiBase string

	// threads maps bot reply message_id → original thread_id
	// so we can track reply chains.
	mu      sync.RWMutex
	threads map[string]string // telegram_msg_id → thread_id
}

func New(botToken string) *Adapter {
	return &Adapter{
		token:   botToken,
		apiBase: "https://api.telegram.org/bot" + botToken,
		threads: make(map[string]string),
	}
}

func (a *Adapter) Name() string { return "telegram" }

// WebhookHandler returns an HTTP handler for Telegram webhook updates.
func (a *Adapter) WebhookHandler(onMessage func(ctx context.Context, msg channel.Message)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var update Update
		if err := json.Unmarshal(body, &update); err != nil {
			slog.Error("failed to parse telegram update", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if update.Message == nil {
			// Not a message update (could be callback, edit, etc.)
			w.WriteHeader(http.StatusOK)
			return
		}

		msg := a.toChannelMessage(update.Message)
		onMessage(r.Context(), msg)

		w.WriteHeader(http.StatusOK)
	})
}

// Reply sends a message back to Telegram.
func (a *Adapter) Reply(ctx context.Context, reply channel.Reply) error {
	chatID, _, err := parseThreadID(reply.ThreadID)
	if err != nil {
		return fmt.Errorf("parse thread_id: %w", err)
	}

	payload := map[string]any{
		"chat_id": chatID,
		"text":    reply.Text,
	}
	if reply.ParseMode != "" {
		payload["parse_mode"] = reply.ParseMode
	}

	// Reply to the original message for threading.
	_, msgID, _ := parseThreadID(reply.ThreadID)
	if msgID != "" {
		if id, err := strconv.Atoi(msgID); err == nil {
			payload["reply_to_message_id"] = id
		}
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(a.apiBase+"/sendMessage", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram sendMessage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Track the bot's reply message_id for thread continuation.
	var result struct {
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	if json.Unmarshal(respBody, &result) == nil && result.Result.MessageID != 0 {
		botMsgID := strconv.Itoa(result.Result.MessageID)
		a.mu.Lock()
		a.threads[botMsgID] = reply.ThreadID
		a.mu.Unlock()
	}

	return nil
}

// toChannelMessage converts a Telegram message to a normalized channel.Message.
func (a *Adapter) toChannelMessage(m *TelegramMessage) channel.Message {
	chatID := strconv.FormatInt(m.Chat.ID, 10)
	msgID := strconv.Itoa(m.MessageID)
	threadID := chatID + ":" + msgID

	msg := channel.Message{
		Source:   "telegram",
		ThreadID: threadID,
		UserID:   strconv.FormatInt(m.From.ID, 10),
		UserName: m.From.FirstName,
		Text:     m.Text,
		URLs:     extractURLs(m.Text),
	}

	// Check if this is a reply to a bot message.
	if m.ReplyToMessage != nil {
		replyMsgID := strconv.Itoa(m.ReplyToMessage.MessageID)
		a.mu.RLock()
		originalThread, exists := a.threads[replyMsgID]
		a.mu.RUnlock()

		if exists {
			msg.IsReply = true
			msg.ReplyToThreadID = originalThread
		}
	}

	return msg
}

// Telegram API types (minimal subset).

type Update struct {
	UpdateID int              `json:"update_id"`
	Message  *TelegramMessage `json:"message"`
}

type TelegramMessage struct {
	MessageID      int              `json:"message_id"`
	From           *TelegramUser    `json:"from"`
	Chat           *TelegramChat    `json:"chat"`
	Text           string           `json:"text"`
	ReplyToMessage *TelegramMessage `json:"reply_to_message"`
}

type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type TelegramChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// parseThreadID splits "chatID:msgID" into parts.
func parseThreadID(threadID string) (chatID, msgID string, err error) {
	parts := strings.SplitN(threadID, ":", 2)
	if len(parts) == 0 {
		return "", "", fmt.Errorf("invalid thread_id: %s", threadID)
	}
	chatID = parts[0]
	if len(parts) > 1 {
		msgID = parts[1]
	}
	return chatID, msgID, nil
}

var urlRegex = regexp.MustCompile(`https?://[^\s<>"']+`)

func extractURLs(text string) []string {
	return urlRegex.FindAllString(text, -1)
}
