package wecom

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file is the WeCom side of the brief push (spec 2.7). WeCom has no REST
// send for a smart bot: every push is an aibot_send_msg over the installation's
// WebSocket, so the notifier sends over the connection the registry holds for
// the binding's installation. A brief is the text as a markdown message, then one
// button_interaction template card per decision. A card's buttons carry the signed
// tokens; a press comes back as a template_card_event (card_press.go).

const (
	// wecomCardMaxButtons is the most buttons one button_interaction card holds.
	wecomCardMaxButtons = 6
	// wecomCardTitleRunes keeps a card's title short.
	wecomCardTitleRunes = 64
	// wecomCardTaskPrefix starts each card's task_id, which WeCom requires to
	// identify the card a press belongs to.
	wecomCardTaskPrefix = "brief-"
	// wecomDecisionTitle titles a card whose decision has no title to show.
	wecomDecisionTitle = "Decision"
)

// Notifier sends brief pushes over a live aibot connection. It holds no
// credentials of its own: the connection the binding's installation has on this
// replica is the sender. With no connection here the push is retried, since the
// retry may land on the replica that holds it.
type Notifier struct {
	senders *sendersRegistry
}

var _ channel.Notifier = (*Notifier)(nil)

// NewNotifier builds the WeCom notifier over the registry the connections register in.
func NewNotifier(senders *SendersRegistry) *Notifier {
	return &Notifier{senders: senders}
}

// Send pushes the text and then the decision cards to the member's single chat.
// A failure before anything is sent (no connection on this replica, the quota,
// a refusal of the text) is retryable. Once the text is out, a card that fails
// is permanent: a retry would send the text again, and the brief's links are in
// the text anyway.
func (n *Notifier) Send(ctx context.Context, b channel.Binding, msg channel.Message) error {
	if b.ChannelType != TypeWecom {
		return channel.PermanentDeliveryError(fmt.Errorf("wecom notifier given a %q binding", b.ChannelType))
	}
	if b.ChannelUserID == "" {
		return channel.PermanentDeliveryError(errors.New("wecom: binding has no user id"))
	}
	if msg.Text == "" {
		return channel.PermanentDeliveryError(errors.New("wecom: message has no text"))
	}
	cards, err := briefCards(msg)
	if err != nil {
		return channel.PermanentDeliveryError(err)
	}
	if n.senders == nil {
		return errNoLiveConnection
	}
	sender := n.senders.get(b.InstallationID)
	if sender == nil {
		return errNoLiveConnection
	}

	chatID := b.ChannelUserID
	if err := sender.sendPiecesCtx(ctx, chatID, chatTypeSingleInt, splitForWire(msg.Text)); err != nil {
		if errors.Is(err, errPartiallySent) {
			return channel.PermanentDeliveryError(err)
		}
		return err
	}
	for _, card := range cards {
		if err := sender.sendCardCtx(ctx, chatID, chatTypeSingleInt, card); err != nil {
			return channel.PermanentDeliveryError(fmt.Errorf("wecom: the brief text was sent but its decision card was not: %w", err))
		}
	}
	return nil
}

// wecomCard is one button_interaction card: one decision, with its callback
// buttons, opened by a link to the decision.
type wecomCard struct {
	title   string
	link    string
	buttons []wecomCardButton
}

type wecomCardButton struct {
	text string
	key  string
}

// briefCards groups the callback buttons into one card per link, in the order the
// links first appear. A link button is left out: its target is already in the
// text. A callback button with no link is left out too, because WeCom needs the
// card to open something.
func briefCards(msg channel.Message) ([]wecomCard, error) {
	var cards []wecomCard
	byLink := map[string]int{}
	for _, b := range msg.Buttons {
		if (b.URL == "") == (b.CallbackData == "") {
			return nil, fmt.Errorf("wecom: button %q must set exactly one of URL or callback data", b.Label)
		}
		if b.CallbackData == "" || b.LinkURL == "" {
			continue
		}
		i, ok := byLink[b.LinkURL]
		if !ok {
			cards = append(cards, wecomCard{title: decisionTitle(b.Label), link: b.LinkURL})
			i = len(cards) - 1
			byLink[b.LinkURL] = i
		}
		cards[i].buttons = append(cards[i].buttons, wecomCardButton{text: actionText(b.Label), key: b.CallbackData})
	}
	for _, c := range cards {
		if len(c.buttons) > wecomCardMaxButtons {
			return nil, fmt.Errorf("wecom: a decision card holds %d buttons; WeCom allows %d", len(c.buttons), wecomCardMaxButtons)
		}
	}
	return cards, nil
}

// decisionTitle is the part of a decision button's label after its action, the
// decision's title (renderBriefPush labels a button "Approve: <title>").
func decisionTitle(label string) string {
	_, title, _ := strings.Cut(label, ": ")
	title = strings.TrimSpace(title)
	if title == "" {
		return wecomDecisionTitle
	}
	return truncateRunes(title, wecomCardTitleRunes)
}

// actionText is the part of a button's label before its title: "Approve", "Reject",
// "Snooze 7 days".
func actionText(label string) string {
	action, _, _ := strings.Cut(label, ": ")
	return strings.TrimSpace(action)
}

// body is the aibot_send_msg body for the card, addressed to the chat.
func (c wecomCard) body(chatID string, chatTypeInt int) map[string]any {
	buttons := make([]map[string]any, 0, len(c.buttons))
	for _, b := range c.buttons {
		buttons = append(buttons, map[string]any{"text": b.text, "key": b.key})
	}
	return map[string]any{
		"chatid":    chatID,
		"chat_type": chatTypeInt,
		"msgtype":   "template_card",
		"template_card": map[string]any{
			"card_type":   "button_interaction",
			"main_title":  map[string]any{"title": c.title},
			"button_list": buttons,
			"card_action": map[string]any{"type": 1, "url": c.link},
			"task_id":     wecomCardTaskPrefix + newReqID(),
		},
	}
}

// sendCardCtx writes one template card and reads the server's verdict. It holds the
// chat's lock, so the card does not land inside another answer.
func (s *wsSender) sendCardCtx(ctx context.Context, chatID string, chatTypeInt int, card wecomCard) error {
	release, err := s.chats.acquire(ctx, chatID)
	if err != nil {
		return err
	}
	defer release()
	return s.sendMsgFrame(ctx, chatID, card.body(chatID, chatTypeInt))
}
