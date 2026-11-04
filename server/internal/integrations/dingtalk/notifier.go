package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the DingTalk side of the brief push (spec 2.7): a proactive robot
// message to a member's direct message, addressed by the binding's staff id. A
// message with buttons is an action card whose buttons open links. DingTalk
// action cards cannot call the server back, so no button here carries a signed
// token: a press would need an interactive card with a Stream callback, which
// this adapter does not build. The decision links in the text lead to the Company,
// where the member decides in the app.

const (
	// msgKeyActionCard is DingTalk's action card template with a list of buttons,
	// each opening a URL.
	msgKeyActionCard = "sampleActionCard6"
	// maxActionCardButtons bounds the buttons one card carries; the brief's own
	// link comes first, so truncation drops the decision links, which the text
	// already names.
	maxActionCardButtons = 5
	// maxActionCardButtonRunes keeps a button's title short.
	maxActionCardButtonRunes = 24
)

// notifierQueries is the slice of generated queries the notifier reads: the
// installation that holds the robot credentials.
type notifierQueries interface {
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Notifier sends brief pushes to a member's direct message with the robot. It
// reads the installation the binding names and decodes its credentials the way
// Outbound does, so the access-token cache is the one the rest of the adapter uses.
type Notifier struct {
	q       notifierQueries
	decrypt Decrypter
	client  *Client
}

var _ channel.Notifier = (*Notifier)(nil)

// NewNotifier builds the DingTalk notifier. client is the shared token-caching
// client; production passes the one the router builds for the adapter.
func NewNotifier(q notifierQueries, decrypt Decrypter, client *Client) *Notifier {
	if client == nil {
		client = NewClient(nil, "")
	}
	return &Notifier{q: q, decrypt: decrypt, client: client}
}

// Send posts msg to the binding's staff id. Failures come back as
// *channel.DeliveryError: a 4xx from DingTalk (other than 408 and 429), an
// unusable binding or installation, and a message that cannot be laid out are
// permanent; a 429, a 5xx and a transport error are retryable.
func (n *Notifier) Send(ctx context.Context, b channel.Binding, msg channel.Message) error {
	if b.ChannelType != TypeDingTalk {
		return channel.PermanentDeliveryError(fmt.Errorf("dingtalk notifier given a %q binding", b.ChannelType))
	}
	if b.ChannelUserID == "" {
		return channel.PermanentDeliveryError(errors.New("dingtalk: binding has no staff id"))
	}
	msgKey, msgParam, err := notifierMessage(msg)
	if err != nil {
		return channel.PermanentDeliveryError(err)
	}

	inst, err := n.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          b.InstallationID,
		ChannelType: string(TypeDingTalk),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return channel.PermanentDeliveryError(errors.New("dingtalk: installation not found"))
		}
		return fmt.Errorf("dingtalk: load installation: %w", err)
	}
	if inst.Status != "active" {
		return channel.PermanentDeliveryError(errors.New("dingtalk: installation is not active"))
	}
	creds, err := decodeCredentials(inst.Config, n.decrypt)
	if err != nil {
		return channel.PermanentDeliveryError(fmt.Errorf("dingtalk: installation credentials unreadable: %w", err))
	}

	s := &sender{client: n.client, robotCode: creds.RobotCode, appKey: creds.AppKey, appSecret: creds.AppSecret}
	_, err = s.sendOne(ctx, sendTarget{ConversationType: convTypeP2P, StaffID: b.ChannelUserID}, msgKey, msgParam)
	return classifyNotifyError(err)
}

// classifyNotifyError turns a DingTalk API failure into the retry decision. A
// client error the request itself caused does not change on a retry; a rate
// limit, a server error and anything unclassified do.
func classifyNotifyError(err error) error {
	if err == nil {
		return nil
	}
	var api *apiRequestError
	if errors.As(err, &api) {
		switch {
		case api.StatusCode == http.StatusRequestTimeout, api.StatusCode == http.StatusTooManyRequests:
			return err
		case api.StatusCode >= 400 && api.StatusCode < 500:
			return channel.PermanentDeliveryError(err)
		}
	}
	return err
}

// actionCardParam is the msgParam of sampleActionCard6.
type actionCardParam struct {
	Title          string          `json:"title"`
	Text           string          `json:"text"`
	BtnOrientation string          `json:"btnOrientation"`
	Btns           []actionCardBtn `json:"btns"`
}

type actionCardBtn struct {
	Title     string `json:"title"`
	ActionURL string `json:"actionURL"`
}

// notifierMessage picks the template for msg. Without a link button the message
// is a Markdown message; with one it is an action card. A callback button has no
// place on DingTalk, so it is shown as a link to its LinkURL, and skipped when it
// has none.
func notifierMessage(msg channel.Message) (string, string, error) {
	if msg.Text == "" {
		return "", "", errors.New("dingtalk: message has no text")
	}
	buttons := make([]actionCardBtn, 0, len(msg.Buttons))
	for _, b := range msg.Buttons {
		if (b.URL == "") == (b.CallbackData == "") {
			return "", "", fmt.Errorf("dingtalk: button %q must set exactly one of URL or callback data", b.Label)
		}
		link := b.URL
		if link == "" {
			link = b.LinkURL
		}
		if link == "" {
			continue
		}
		buttons = append(buttons, actionCardBtn{Title: cutRunes(b.Label, maxActionCardButtonRunes), ActionURL: link})
	}
	if len(buttons) == 0 {
		param, err := json.Marshal(markdownParam{Title: markdownTitle(msg.Text), Text: msg.Text})
		if err != nil {
			return "", "", fmt.Errorf("dingtalk: marshal message: %w", err)
		}
		if len(param) > markdownPayloadByteBudget {
			return "", "", fmt.Errorf("dingtalk: message is %d bytes; one DingTalk message holds %d", len(param), markdownPayloadByteBudget)
		}
		return msgKeyMarkdown, string(param), nil
	}
	if len(buttons) > maxActionCardButtons {
		buttons = buttons[:maxActionCardButtons]
	}
	param, err := json.Marshal(actionCardParam{
		Title:          markdownTitle(msg.Text),
		Text:           msg.Text,
		BtnOrientation: "0",
		Btns:           buttons,
	})
	if err != nil {
		return "", "", fmt.Errorf("dingtalk: marshal action card: %w", err)
	}
	if len(param) > markdownPayloadByteBudget {
		return "", "", fmt.Errorf("dingtalk: message is %d bytes; one DingTalk card holds %d", len(param), markdownPayloadByteBudget)
	}
	return msgKeyActionCard, string(param), nil
}

// cutRunes cuts s to at most n runes, marking the cut with an ellipsis.
func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
