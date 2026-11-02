package lark

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the Feishu/Lark side of the brief push (spec 2.7, plan 9.11b): a
// proactive message to a member's direct message with the bot, addressed by the
// binding's open_id. A message with no buttons is plain text; one with buttons
// is an interactive card. Presses come back as card.action.trigger events over
// the installation's long connection; see card_action.go.

// cardActionValueKey is the key the signed token sits under in a callback
// button's value. The press reads the same key back.
const cardActionValueKey = "token"

// codeRateLimited is Lark's "request trigger frequency limit" business code. It
// can come with HTTP 429 or inside a 400 body, so both shapes are read.
const codeRateLimited = 99991400

// notifierQueries is the slice of generated queries the notifier reads: the
// installation that holds the app credentials.
type notifierQueries interface {
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Notifier sends brief pushes to a member's direct message with the bot. It
// reads the installation the binding names, decrypts its app secret through the
// credentials resolver, and sends with the shared API client, so the tenant
// token is the one the rest of the integration already caches.
type Notifier struct {
	q     notifierQueries
	creds CredentialsResolver
	api   APIClient
}

var _ channel.Notifier = (*Notifier)(nil)

// NewNotifier builds the Lark notifier over the generated queries, the
// credentials resolver (the installation service) and the API client.
func NewNotifier(q notifierQueries, creds CredentialsResolver, api APIClient) *Notifier {
	return &Notifier{q: q, creds: creds, api: api}
}

// Send posts msg to the binding's open_id. Failures come back as
// *channel.DeliveryError: a rate limit carries Lark's Retry-After when it sent
// one; a binding or installation that cannot be used, a message that cannot be
// laid out, and "bot has no availability to this user" (230013) are permanent;
// anything else is retryable.
func (n *Notifier) Send(ctx context.Context, b channel.Binding, msg channel.Message) error {
	if b.ChannelType != channel.TypeFeishu {
		return channel.PermanentDeliveryError(fmt.Errorf("lark notifier given a %q binding", b.ChannelType))
	}
	if b.ChannelUserID == "" {
		return channel.PermanentDeliveryError(errors.New("lark: binding has no open id"))
	}
	msgType, content, err := notifierContent(msg)
	if err != nil {
		return channel.PermanentDeliveryError(err)
	}

	row, err := n.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          b.InstallationID,
		ChannelType: string(channel.TypeFeishu),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return channel.PermanentDeliveryError(errors.New("lark: installation not found"))
		}
		return fmt.Errorf("lark: load installation: %w", err)
	}
	if row.Status != string(InstallationActive) {
		return channel.PermanentDeliveryError(errors.New("lark: installation is not active"))
	}
	inst, err := installationFromRow(row)
	if err != nil {
		return channel.PermanentDeliveryError(fmt.Errorf("lark: installation unreadable: %w", err))
	}
	creds, err := installationCredentialsFor(inst, n.creds)
	if err != nil {
		return channel.PermanentDeliveryError(fmt.Errorf("lark: installation credentials unreadable: %w", err))
	}

	_, err = n.api.SendUserMessage(ctx, creds, SendUserMessageParams{
		OpenID:  b.ChannelUserID,
		MsgType: msgType,
		Content: content,
	})
	return classifySendError(err)
}

// classifySendError turns a send failure into the retry decision. The rate limit
// is checked first, so a 429 that also names a permanent code still retries.
func classifySendError(err error) error {
	if err == nil {
		return nil
	}
	code, _, hasCode := larkErrorCodeMsg(err)
	limited := code == codeRateLimited
	var retryAfter time.Duration
	var status *larkAPIStatusError
	if errors.As(err, &status) {
		if status.StatusCode == http.StatusTooManyRequests {
			limited = true
		}
		retryAfter = status.RetryAfter
	}
	switch {
	case limited:
		return &channel.DeliveryError{Err: err, RetryAfter: retryAfter}
	case hasCode && code == codeNoAvailability:
		return channel.PermanentDeliveryError(err)
	}
	return err
}

// notifierContent picks the message type for msg. Without buttons the message
// is text, which the member reads as a normal bubble. With buttons it is a card.
func notifierContent(msg channel.Message) (string, string, error) {
	if msg.Text == "" {
		return "", "", errors.New("lark: message has no text")
	}
	if len(msg.Buttons) == 0 {
		content, err := jsonBytes(map[string]string{"text": msg.Text})
		return "text", content, err
	}
	content, err := briefCard(msg)
	return "interactive", content, err
}

// briefCard lays the message out as a schema 2.0 card: the text as one markdown
// element, then one column_set per row of buttons. Rows follow the Telegram
// keyboard rule: a button starts a row unless it sets SameRow. A callback button
// carries its signed token in behaviors; a link button opens its URL.
func briefCard(msg channel.Message) (string, error) {
	elements := []any{
		map[string]any{"tag": "markdown", "content": msg.Text},
	}
	for _, row := range buttonRows(msg.Buttons) {
		columns := make([]any, 0, len(row))
		for _, b := range row {
			button, err := cardButton(b)
			if err != nil {
				return "", err
			}
			columns = append(columns, map[string]any{
				"tag":      "column",
				"width":    "auto",
				"elements": []any{button},
			})
		}
		elements = append(elements, map[string]any{
			"tag":       "column_set",
			"flex_mode": "flow",
			"columns":   columns,
		})
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{"update_multi": true, "wide_screen_mode": true},
		"body":   map[string]any{"elements": elements},
	}
	return jsonBytes(card)
}

// buttonRows groups buttons into rows, the way the Telegram and Slack layouts do.
func buttonRows(buttons []channel.Button) [][]channel.Button {
	var rows [][]channel.Button
	for i, b := range buttons {
		if i > 0 && b.SameRow {
			rows[len(rows)-1] = append(rows[len(rows)-1], b)
			continue
		}
		rows = append(rows, []channel.Button{b})
	}
	return rows
}

// cardButton renders one button. A button carries exactly one of URL (open_url)
// or CallbackData (callback with the token as its value). LinkURL is not read:
// Lark takes callbacks over the long connection, so a callback button is always
// a button.
func cardButton(b channel.Button) (map[string]any, error) {
	if (b.URL == "") == (b.CallbackData == "") {
		return nil, fmt.Errorf("lark: button %q must set exactly one of URL or callback data", b.Label)
	}
	button := map[string]any{
		"tag":  "button",
		"text": map[string]any{"tag": "plain_text", "content": b.Label},
		"type": "default",
	}
	if b.CallbackData != "" {
		button["behaviors"] = []any{map[string]any{
			"type":  "callback",
			"value": map[string]string{cardActionValueKey: b.CallbackData},
		}}
	} else {
		button["behaviors"] = []any{map[string]any{
			"type":        "open_url",
			"default_url": b.URL,
		}}
	}
	return button, nil
}
