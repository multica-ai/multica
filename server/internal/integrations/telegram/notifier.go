package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// maxNotifierUnits is Telegram's cap on one text message after entity parsing.
// The brief push is far shorter; the check exists so an oversized message fails
// as a clear permanent error instead of a 400 from the API.
const maxNotifierUnits = 4096

// notifierQueries is the slice of generated queries the Notifier reads: the
// installation that holds the bot token.
type notifierQueries interface {
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Notifier sends proactive messages to a member's private chat with the bot.
// It reads the bot token from the installation the way Outbound does, and
// addresses the member by the binding's Telegram user id.
type Notifier struct {
	q       notifierQueries
	decrypt Decrypter
	apiBase string
	client  *http.Client
}

// NewNotifier builds the Telegram notifier. apiBase and client are the test
// seams; production passes "" and nil.
func NewNotifier(q notifierQueries, decrypt Decrypter, apiBase string, client *http.Client) *Notifier {
	return &Notifier{q: q, decrypt: decrypt, apiBase: apiBase, client: client}
}

// Send posts msg as one sendMessage call. Failures come back as
// *channel.DeliveryError: a 403 (the member blocked the bot), any other 4xx
// and an unusable binding or installation are permanent; a 429 carries
// Telegram's retry_after; a 5xx or a transport error is retryable.
func (n *Notifier) Send(ctx context.Context, b channel.Binding, msg channel.Message) error {
	if b.ChannelType != TypeTelegram {
		return channel.PermanentDeliveryError(fmt.Errorf("telegram notifier given a %q binding", b.ChannelType))
	}
	chatID, err := strconv.ParseInt(b.ChannelUserID, 10, 64)
	if err != nil {
		return channel.PermanentDeliveryError(fmt.Errorf("telegram: binding has no chat id: %w", err))
	}
	if units := utf16Units(msg.Text); units > maxNotifierUnits || msg.Text == "" {
		return channel.PermanentDeliveryError(fmt.Errorf("telegram: message is %d units; it must be 1 to %d", units, maxNotifierUnits))
	}
	keyboard, err := inlineKeyboardFor(msg.Buttons)
	if err != nil {
		return channel.PermanentDeliveryError(err)
	}

	inst, err := n.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          b.InstallationID,
		ChannelType: string(TypeTelegram),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return channel.PermanentDeliveryError(errors.New("telegram: installation not found"))
		}
		return fmt.Errorf("telegram: load installation: %w", err)
	}
	if inst.Status != "active" {
		return channel.PermanentDeliveryError(errors.New("telegram: installation is not active"))
	}
	creds, err := decodeCredentials(inst.Config, n.decrypt)
	if err != nil {
		return channel.PermanentDeliveryError(fmt.Errorf("telegram: installation credentials unreadable: %w", err))
	}

	api := newBotAPI(n.apiBase, creds.BotToken, n.client)
	_, err = api.SendMessage(ctx, sendMessageParams{
		ChatID:      chatID,
		Text:        msg.Text,
		ReplyMarkup: keyboard,
	})
	return classifyNotifyError(err)
}

// inlineKeyboardFor renders the buttons, one per row, in the order given. A
// button must carry exactly one of URL or CallbackData.
func inlineKeyboardFor(buttons []channel.Button) (*inlineKeyboard, error) {
	if len(buttons) == 0 {
		return nil, nil
	}
	rows := make([][]inlineKeyboardButton, 0, len(buttons))
	for _, b := range buttons {
		if (b.URL == "") == (b.CallbackData == "") {
			return nil, fmt.Errorf("telegram: button %q must set exactly one of URL or callback data", b.Label)
		}
		rows = append(rows, []inlineKeyboardButton{{Text: b.Label, URL: b.URL, CallbackData: b.CallbackData}})
	}
	return &inlineKeyboard{InlineKeyboard: rows}, nil
}

// classifyNotifyError turns a Bot API error into the retry decision. Only the
// classes the retry job acts on are marked; anything unmarked is retryable.
func classifyNotifyError(err error) error {
	if err == nil {
		return nil
	}
	var ae *apiError
	if errors.As(err, &ae) {
		switch {
		case ae.Code == http.StatusTooManyRequests:
			wait, _ := retryAfter(err)
			return &channel.DeliveryError{Err: err, RetryAfter: wait}
		case ae.Code >= 500:
			return err
		case ae.Code >= 400:
			// 403 is the member blocking the bot. The other 4xx (chat not
			// found, deactivated user, bad token) do not change on a retry.
			return channel.PermanentDeliveryError(err)
		}
	}
	return err
}
