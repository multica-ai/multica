package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type menuSession struct {
	owner   int64
	page    string
	expires time.Time
	used    bool
	seen    map[string]bool
}

// Menu UI failures must not restart polling: a rebuilt channel loses its
// menu state and Telegram can keep redelivering the same unacknowledged update.
func (c *telegramChannel) menuAPIFailed(ctx context.Context, method string, err error) bool {
	if err == nil {
		return false
	}
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, "telegram: menu API call failed", "bot_id", c.botID, "method", method, "error", err)
	return true
}

func (c *telegramChannel) answerMenuCallback(ctx context.Context, id, text string) bool {
	return !c.menuAPIFailed(ctx, "answerCallbackQuery", c.api.AnswerCallbackQuery(ctx, id, text))
}

func (c *telegramChannel) menuView(page string) (string, *inlineKeyboardMarkup) {
	markup := &inlineKeyboardMarkup{}
	title := c.menu.RootText
	if p, ok := c.menu.page(page); ok {
		title = p.Title
		for _, a := range p.Actions {
			markup.InlineKeyboard = append(markup.InlineKeyboard, []inlineKeyboardButton{{a.Label, "menu:v1:" + page + ":" + a.ID}})
		}
	} else {
		for _, p := range c.menu.Pages {
			markup.InlineKeyboard = append(markup.InlineKeyboard, []inlineKeyboardButton{{p.Title, "menu:v1:" + p.Command}})
		}
	}
	markup.InlineKeyboard = append(markup.InlineKeyboard, []inlineKeyboardButton{{c.menu.BackText, "menu:v1:root"}})
	return title, markup
}

func (c *telegramChannel) menuPrivate(m *Message) bool {
	if c.menu == nil || c.menuAuthorize == nil || m == nil || m.From == nil || m.From.IsBot || m.Chat.Type != "private" || m.From.ID != m.Chat.ID {
		return false
	}
	for _, id := range c.menu.PrivateChatIDs {
		if m.Chat.ID == id {
			return true
		}
	}
	return false
}

func (c *telegramChannel) authorizeMenu(ctx context.Context, msg channel.InboundMessage) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return c.menuAuthorize(ctx, msg)
}

func (c *telegramChannel) dispatchMenuCommand(ctx context.Context, u Update, msg channel.InboundMessage) (bool, error) {
	if msg.Type != channel.MsgTypeText || !c.menuPrivate(u.Message) {
		return false, nil
	}
	command := strings.TrimSpace(msg.CommandText)
	page := strings.TrimPrefix(command, "/")
	if strings.HasSuffix(page, "@"+c.botUsername) {
		page = strings.TrimSuffix(page, "@"+c.botUsername)
	}
	if _, ok := c.menu.page(page); !ok || !strings.HasPrefix(command, "/") {
		return false, nil
	}
	if err := c.authorizeMenu(ctx, msg); err != nil {
		_, sendErr := c.api.SendMessage(ctx, sendMessageParams{ChatID: u.Message.Chat.ID, Text: "Menu unavailable. Check account binding, workspace membership, and agent access."})
		c.menuAPIFailed(ctx, "sendMessage", sendErr)
		return true, nil
	}
	if c.menus == nil {
		c.menus = make(map[string]*menuSession)
	}
	for id, s := range c.menus {
		if time.Now().After(s.expires) {
			delete(c.menus, id)
		}
	}
	if len(c.menus) >= 128 {
		_, err := c.api.SendMessage(ctx, sendMessageParams{ChatID: u.Message.Chat.ID, Text: "Too many open menus. Try again later."})
		c.menuAPIFailed(ctx, "sendMessage", err)
		return true, nil
	}
	title, markup := c.menuView(page)
	m, err := c.api.SendMessage(ctx, sendMessageParams{ChatID: u.Message.Chat.ID, Text: title, ReplyMarkup: markup})
	if !c.menuAPIFailed(ctx, "sendMessage", err) {
		c.menus[messageKey(u.Message.Chat.ID, m.MessageID)] = &menuSession{owner: u.Message.From.ID, page: page, expires: time.Now().Add(15 * time.Minute), seen: make(map[string]bool)}
	}
	return true, nil
}

func (c *telegramChannel) dispatchMenuCallback(ctx context.Context, q *CallbackQuery) error {
	if q.ID == "" {
		return nil
	}
	reject := func() error {
		c.answerMenuCallback(ctx, q.ID, "Menu expired or access denied. Send the menu command again.")
		return nil
	}
	if q.From == nil || q.From.IsBot || q.Message == nil || q.Message.Date == 0 || q.Message.From == nil ||
		q.Message.From.ID != c.botID || !q.Message.From.IsBot {
		return reject()
	}
	// The callback sender, not the bot-authored menu message, owns the request.
	m := *q.Message
	m.From = q.From
	m.Text = "Menu action"
	m.Entities = nil
	m.ReplyToMessage = nil
	if !c.menuPrivate(&m) {
		return reject()
	}
	s := c.menus[messageKey(q.Message.Chat.ID, q.Message.MessageID)]
	if s == nil || s.owner != q.From.ID || time.Now().After(s.expires) {
		return reject()
	}
	msg, ok := inboundFromUpdate(Update{Message: &m}, c.botID, c.botUsername)
	if !ok {
		return reject()
	}
	if err := c.authorizeMenu(ctx, msg); err != nil {
		return reject()
	}
	if len(q.Data) > 64 {
		return reject()
	}
	parts := strings.Split(q.Data, ":")
	if len(parts) < 3 || len(parts) > 4 || parts[0] != "menu" || parts[1] != "v1" {
		return reject()
	}
	page := parts[2]
	if len(parts) == 3 {
		if page != "root" {
			if _, ok := c.menu.page(page); !ok || s.page != "root" {
				return reject()
			}
		}
		if s.seen[q.ID] {
			c.answerMenuCallback(ctx, q.ID, "Already handled.")
			return nil
		}
		if len(s.seen) >= 256 {
			return reject()
		}
		if !c.answerMenuCallback(ctx, q.ID, "") {
			return nil
		}
		title, markup := c.menuView(page)
		if err := c.api.EditMessageText(ctx, editMessageTextParams{ChatID: q.Message.Chat.ID, MessageID: q.Message.MessageID, Text: title, ReplyMarkup: markup}); err != nil {
			var ae *apiError
			if !errors.As(err, &ae) || !strings.Contains(ae.Description, "message is not modified") {
				c.menuAPIFailed(ctx, "editMessageText", err)
				return nil
			}
		}
		s.page = page
		s.used = false
		if s.seen == nil {
			s.seen = make(map[string]bool)
		}
		s.seen[q.ID] = true
		return nil
	}
	p, ok := c.menu.page(page)
	if !ok || page != s.page {
		return reject()
	}
	var request string
	for _, a := range p.Actions {
		if a.ID == parts[3] {
			request = a.Request
			break
		}
	}
	if request == "" {
		return reject()
	}
	if s.used || s.seen[q.ID] {
		c.answerMenuCallback(ctx, q.ID, "Already submitted. Wait for a reply or return to the menu.")
		return nil
	}
	if len(s.seen) >= 256 {
		return reject()
	}
	if !c.answerMenuCallback(ctx, q.ID, "Submitting.") {
		return nil
	}
	msg.EventID = "callback:" + q.ID
	msg.MessageID = msg.EventID
	if raw, err := decodeTelegramRaw(msg); err == nil {
		raw.EventType = "callback_query"
		msg.Raw, _ = json.Marshal(raw)
	}
	msg.Text = request
	msg.CommandText = msg.Text
	if err := c.handler(ctx, msg); err != nil {
		_, sendErr := c.api.SendMessage(ctx, sendMessageParams{ChatID: q.Message.Chat.ID, Text: "Could not confirm request submission. Check task status, then open the menu again if needed."})
		c.menuAPIFailed(ctx, "sendMessage", sendErr)
		return err
	}
	s.used = true
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	s.seen[q.ID] = true
	return nil
}

// NewMenuAuthorizer reuses the router's installation and identity resolvers;
// even navigation must re-check membership and the agent invocation gate.
func NewMenuAuthorizer(q *db.Queries, mayInvoke func(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)) func(context.Context, channel.InboundMessage) error {
	return func(ctx context.Context, msg channel.InboundMessage) error {
		inst, err := (&installationResolver{q: q}).ResolveInstallation(ctx, msg)
		if err != nil {
			return err
		}
		if !inst.Active {
			return errors.New("telegram: installation inactive")
		}
		identity, err := (&identityResolver{q: q}).ResolveSender(ctx, inst, msg)
		if err != nil {
			return err
		}
		allowed, err := mayInvoke(ctx, inst.AgentID, identity.UserID)
		if err != nil {
			return err
		}
		if !allowed {
			return engine.ErrSenderNotMember
		}
		return nil
	}
}
