package telegram

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file is the Telegram side of chat actions (spec 2.7, plan 9.11): a press
// of a decision button, and the /brief and /decisions commands. What they mean
// is decided by channel.ActionHandler on the server. This file only carries the
// Bot API's shapes in and out, and keeps the keyboard in step with the decision.

// User-facing copy the adapter speaks itself, when the server's answer cannot be
// had (an infrastructure failure).
const (
	msgPressFailedToast = "Something went wrong. Try again."
	msgPressFailed      = "That did not go through, so nothing changed. Open the app to decide."
	msgCommandFailed    = "I could not load that right now. Try again in a moment."
)

const (
	// pressTimeout bounds one button press: the decision and the Bot API calls
	// that answer it. Telegram expects an answer to a press within seconds.
	pressTimeout = 20 * time.Second
	// commandTimeout bounds one /brief or /decisions reply.
	commandTimeout = 15 * time.Second
)

// sender is the Telegram user the server resolves through the binding.
func (c *telegramChannel) sender(userID string, isBot bool) channel.Sender {
	return channel.Sender{
		ChannelType:    TypeTelegram,
		InstallationID: c.installationID,
		ChannelUserID:  userID,
		IsBot:          isBot,
	}
}

// press handles a button press. The callback is answered in every case, so the
// button stops spinning. A reply goes to the chat, a settled decision loses its
// buttons, and nothing else changes.
func (c *telegramChannel) press(ctx context.Context, cq CallbackQuery) {
	ctx, cancel := context.WithTimeout(ctx, pressTimeout)
	defer cancel()
	if c.actions == nil {
		c.answerPress(ctx, cq.ID, "")
		return
	}
	res, err := c.actions.Press(ctx, channel.Press{
		Sender: c.sender(strconv.FormatInt(cq.From.ID, 10), cq.From.IsBot),
		Data:   cq.Data,
	})
	if err != nil {
		c.logger.WarnContext(ctx, "telegram: button press failed", "bot_id", c.botID, "error", err)
		res = channel.PressResult{Toast: msgPressFailedToast, Reply: msgPressFailed}
	}
	c.answerPress(ctx, cq.ID, res.Toast)
	if cq.Message == nil {
		// Telegram no longer has the message, so there is no chat to answer in
		// and no keyboard to change.
		return
	}
	if res.Reply != "" {
		c.reply(ctx, cq.Message.Chat.ID, cq.Message.MessageThreadID, cq.Message.MessageID, channel.Message{Text: res.Reply})
	}
	if res.Settled {
		c.removeSettledButtons(ctx, cq.Message, cq.Data)
	}
}

func (c *telegramChannel) answerPress(ctx context.Context, callbackQueryID, toast string) {
	if err := c.api.AnswerCallbackQuery(ctx, callbackQueryID, toast); err != nil {
		c.logger.WarnContext(ctx, "telegram: answer callback query failed", "bot_id", c.botID, "error", err)
	}
}

// removeSettledButtons takes out every button of the decision the pressed
// button acted on: its callback buttons, and its Revise link, which names the
// proposal in its decision parameter. The other decisions' buttons stay. When
// nothing is left, the keyboard goes.
func (c *telegramChannel) removeSettledButtons(ctx context.Context, m *Message, pressedData string) {
	key := c.actions.ActionKey(pressedData)
	if key == "" || m.ReplyMarkup == nil {
		return
	}
	removed := false
	rows := [][]inlineKeyboardButton{}
	for _, row := range m.ReplyMarkup.InlineKeyboard {
		var kept []inlineKeyboardButton
		for _, b := range row {
			if c.belongsTo(b, key) {
				removed = true
				continue
			}
			kept = append(kept, b)
		}
		if len(kept) > 0 {
			rows = append(rows, kept)
		}
	}
	if !removed {
		return
	}
	if err := c.api.EditMessageReplyMarkup(ctx, m.Chat.ID, m.MessageID, inlineKeyboard{InlineKeyboard: rows}); err != nil {
		c.logger.WarnContext(ctx, "telegram: remove settled buttons failed", "bot_id", c.botID, "error", err)
	}
}

// belongsTo reports whether a button on the message acts on the decision key.
// A callback button names its proposal in its signed token; a link names it in
// its decision parameter.
func (c *telegramChannel) belongsTo(b inlineKeyboardButton, key string) bool {
	if b.CallbackData != "" {
		return c.actions.ActionKey(b.CallbackData) == key
	}
	u, err := url.Parse(b.URL)
	return err == nil && u.Query().Get("decision") == key
}

// command answers /brief and /decisions for a linked member and reports whether
// it did, so the message does not reach the agent. An unlinked sender is not
// answered here: the ordinary path gives the binding prompt.
func (c *telegramChannel) command(ctx context.Context, msg channel.InboundMessage) bool {
	if c.actions == nil || msg.Type != channel.MsgTypeText || !msg.AddressedToBot {
		return false
	}
	name, ok := channel.ParseChatCommand(msg.CommandText)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	reply, handled, err := c.actions.Command(ctx, channel.Command{
		Sender: c.sender(msg.Source.SenderID, false),
		Name:   name,
	})
	if err != nil {
		c.logger.WarnContext(ctx, "telegram: chat command failed", "bot_id", c.botID, "command", name, "error", err)
		reply, handled = channel.Message{Text: msgCommandFailed}, true
	}
	if !handled {
		return false
	}
	chatID, err := strconv.ParseInt(msg.Source.ChatID, 10, 64)
	if err != nil {
		c.logger.WarnContext(ctx, "telegram: command reply has invalid chat id", "bot_id", c.botID, "error", err)
		return true
	}
	var threadID int64
	if msg.Source.ThreadID != "" {
		threadID, _ = strconv.ParseInt(msg.Source.ThreadID, 10, 64)
	}
	c.reply(ctx, chatID, threadID, parseMessageRef(msg.MessageID), reply)
	return true
}

// reply posts a message in a chat, quoting the message it answers when there is
// one. Its buttons are links. A failure is logged: the press or command has
// already been decided, and a lost reply must not stop the polling loop.
func (c *telegramChannel) reply(ctx context.Context, chatID, threadID, quoteID int64, msg channel.Message) {
	keyboard, err := inlineKeyboardFor(msg.Buttons)
	if err != nil {
		c.logger.WarnContext(ctx, "telegram: reply buttons invalid", "bot_id", c.botID, "error", err)
		keyboard = nil
	}
	if _, err := c.api.SendMessage(ctx, sendMessageParams{
		ChatID:          chatID,
		Text:            msg.Text,
		MessageThreadID: threadID,
		ReplyParameters: optionalReplyParameters(quoteID),
		ReplyMarkup:     keyboard,
	}); err != nil {
		c.logger.WarnContext(ctx, "telegram: chat reply failed", "bot_id", c.botID, "error", err)
	}
}
