package slack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the Slack side of the brief push (spec 2.7, plan 9.11a): a
// proactive chat.postMessage to a member's direct message with the bot. The
// message is laid out in Block Kit so its decision buttons are real buttons.
// Presses come back over the installation's Socket Mode connection; see
// actions.go.

// Block Kit limits the brief push has to fit.
const (
	// maxSectionTextRunes is the most text one section block carries.
	maxSectionTextRunes = 3000
	// maxButtonValueRunes is the most a button's value may hold. Signed action
	// tokens are 51 characters, so this is only a guard.
	maxButtonValueRunes = 2000
	// maxButtonTextRunes is the most a button's label may hold.
	maxButtonTextRunes = 75
)

// notifierQueries is the slice of generated queries the Notifier reads: the
// installation that holds the bot token.
type notifierQueries interface {
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// permanentSlackErrors are the Web API errors a retry cannot fix: the token or
// its scopes, the channel or the user, and the app's own state. Any other error
// is retryable.
var permanentSlackErrors = map[string]bool{
	"account_inactive":        true,
	"cannot_dm_bot":           true,
	"channel_not_found":       true,
	"invalid_auth":            true,
	"is_archived":             true,
	"missing_scope":           true,
	"not_allowed_token_type":  true,
	"not_authed":              true,
	"not_in_channel":          true,
	"token_expired":           true,
	"token_revoked":           true,
	"user_disabled":           true,
	"user_is_bot":             true,
	"user_not_found":          true,
	"user_not_visible":        true,
	"no_permission":           true,
	"team_access_not_granted": true,
}

// Notifier sends proactive messages to a member's direct message with the bot.
// It reads the bot token from the installation the way Outbound does, and
// addresses the member by the binding's Slack user id: chat.postMessage to a
// user id posts in the app's direct message with that user.
type Notifier struct {
	q       notifierQueries
	decrypt Decrypter
	apiBase string
	client  *http.Client
}

var _ channel.Notifier = (*Notifier)(nil)

// NewNotifier builds the Slack notifier. apiBase and client are the test seams;
// production passes "" and nil.
func NewNotifier(q notifierQueries, decrypt Decrypter, apiBase string, client *http.Client) *Notifier {
	return &Notifier{q: q, decrypt: decrypt, apiBase: apiBase, client: client}
}

// Send posts msg as one chat.postMessage call. Failures come back as
// *channel.DeliveryError: a rate limit carries Slack's Retry-After; a bad
// message, an unusable binding or installation, and the errors in
// permanentSlackErrors are permanent; anything else is retryable.
func (n *Notifier) Send(ctx context.Context, b channel.Binding, msg channel.Message) error {
	if b.ChannelType != TypeSlack {
		return channel.PermanentDeliveryError(fmt.Errorf("slack notifier given a %q binding", b.ChannelType))
	}
	if b.ChannelUserID == "" {
		return channel.PermanentDeliveryError(errors.New("slack: binding has no user id"))
	}
	blocks, err := notifierBlocks(msg)
	if err != nil {
		return channel.PermanentDeliveryError(err)
	}

	inst, err := n.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          b.InstallationID,
		ChannelType: string(TypeSlack),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return channel.PermanentDeliveryError(errors.New("slack: installation not found"))
		}
		return fmt.Errorf("slack: load installation: %w", err)
	}
	if inst.Status != "active" {
		return channel.PermanentDeliveryError(errors.New("slack: installation is not active"))
	}
	creds, err := decodeCredentials(inst.Config, n.decrypt)
	if err != nil {
		return channel.PermanentDeliveryError(fmt.Errorf("slack: installation credentials unreadable: %w", err))
	}
	if creds.BotToken == "" {
		return channel.PermanentDeliveryError(errors.New("slack: installation has no bot token"))
	}

	_, _, err = n.api(creds.BotToken).PostMessageContext(ctx, b.ChannelUserID,
		slack.MsgOptionText(escapeMrkdwn(msg.Text), false),
		slack.MsgOptionBlocks(blocks...),
	)
	return classifyPostError(err)
}

// api is the bot-token Web API client for this notifier's seams.
func (n *Notifier) api(token string) *slack.Client {
	var opts []slack.Option
	if n.apiBase != "" {
		opts = append(opts, slack.OptionAPIURL(strings.TrimRight(n.apiBase, "/")+"/"))
	}
	if n.client != nil {
		opts = append(opts, slack.OptionHTTPClient(n.client))
	}
	return slack.New(token, opts...)
}

// classifyPostError turns a Web API error into the retry decision. A rate limit
// keeps Slack's Retry-After; a permanent Web API error is marked; anything else
// is returned unmarked, so the retry job treats it as retryable.
func classifyPostError(err error) error {
	if err == nil {
		return nil
	}
	var limited *slack.RateLimitedError
	if errors.As(err, &limited) {
		return &channel.DeliveryError{Err: err, RetryAfter: limited.RetryAfter}
	}
	var api slack.SlackErrorResponse
	if errors.As(err, &api) && permanentSlackErrors[api.Err] {
		return channel.PermanentDeliveryError(err)
	}
	return err
}

// notifierBlocks lays the message out in Block Kit: one section with the text,
// then one actions block per row of buttons. Rows are made the way the Telegram
// keyboard makes them: a button starts a row unless it sets SameRow.
func notifierBlocks(msg channel.Message) ([]slack.Block, error) {
	if msg.Text == "" {
		return nil, errors.New("slack: message has no text")
	}
	text := escapeMrkdwn(msg.Text)
	if utf8.RuneCountInString(text) > maxSectionTextRunes {
		return nil, fmt.Errorf("slack: message is %d characters; a section holds %d", utf8.RuneCountInString(text), maxSectionTextRunes)
	}
	blocks := []slack.Block{
		slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil),
	}
	rows := buttonRows(msg.Buttons)
	actionID := 0
	for i, row := range rows {
		elements := make([]slack.BlockElement, 0, len(row))
		for _, b := range row {
			element, err := buttonElement(b, fmt.Sprintf("brief_action_%d", actionID))
			if err != nil {
				return nil, err
			}
			elements = append(elements, element)
			actionID++
		}
		blocks = append(blocks, slack.NewActionBlock(fmt.Sprintf("brief_actions_%d", i), elements...))
	}
	return blocks, nil
}

// buttonRows groups buttons into rows. A button starts a row of its own unless
// it sets SameRow, which puts it beside the one before.
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

// buttonElement renders one button. A button carries exactly one of URL (a link,
// which Slack opens in the browser) or CallbackData (the signed token a press
// sends back over Socket Mode). LinkURL is not read: Slack takes callbacks, so
// a callback button is always a button.
func buttonElement(b channel.Button, actionID string) (slack.BlockElement, error) {
	if (b.URL == "") == (b.CallbackData == "") {
		return nil, fmt.Errorf("slack: button %q must set exactly one of URL or callback data", b.Label)
	}
	if utf8.RuneCountInString(b.CallbackData) > maxButtonValueRunes {
		return nil, fmt.Errorf("slack: button %q value is too long for Slack", b.Label)
	}
	label := cutLabel(b.Label, maxButtonTextRunes)
	element := slack.NewButtonBlockElement(actionID, b.CallbackData, slack.NewTextBlockObject(slack.PlainTextType, label, false, false))
	element.URL = b.URL
	return element, nil
}

// escapeMrkdwn escapes the three characters Slack reads as mrkdwn markup. The
// text is otherwise sent as written.
func escapeMrkdwn(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// cutLabel cuts s to at most n runes, marking the cut with an ellipsis.
func cutLabel(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
