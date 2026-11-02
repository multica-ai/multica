package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file is the Feishu/Lark side of button presses (spec 2.7, plan 9.11b). A
// press of a brief push button arrives as a card.action.trigger event on the
// installation's long connection. The connector answers it at once with an empty
// response, which keeps the card as it is, and hands the event here on a
// goroutine. What the press means is decided by channel.ActionHandler on the
// server, the same handler Telegram and Slack use. This file only carries Lark's
// event in and renders what comes back: a direct message to the presser, and the
// message with the buttons of a settled decision removed.

// cardActionEventType is the event a card button press is delivered as.
const cardActionEventType = "card.action.trigger"

// cardActionTimeout bounds one press: the decision and the API calls that answer
// it. The response to the press itself has already gone out.
const cardActionTimeout = 20 * time.Second

// msgCardPressFailed is what the presser hears when the server cannot answer.
const msgCardPressFailed = "That did not go through, so nothing changed. Open the app to decide."

// CardActionHandler receives one card press from an installation's connection.
// The connector calls it on its own goroutine after it has answered the press, so
// the implementation may take the time it needs within its own timeout.
type CardActionHandler interface {
	HandleCardAction(ctx context.Context, inst Installation, payload []byte)
}

// cardActionEnvelope is the part of a card.action.trigger event the press uses.
type cardActionEnvelope struct {
	Header struct {
		AppID string `json:"app_id"`
	} `json:"header"`
	Event struct {
		Operator struct {
			OpenID string `json:"open_id"`
		} `json:"operator"`
		Action struct {
			Value json.RawMessage `json:"value"`
		} `json:"action"`
		Context struct {
			OpenMessageID string `json:"open_message_id"`
		} `json:"context"`
	} `json:"event"`
}

// larkCardActions is the CardActionHandler the connector runs for a press.
type larkCardActions struct {
	api     APIClient
	creds   CredentialsResolver
	actions channel.ActionHandler
	logger  *slog.Logger
}

var _ CardActionHandler = (*larkCardActions)(nil)

// NewCardActionHandler builds the press handler over the API client, the
// credentials resolver and the shared chat action handler. A nil actions leaves
// presses unanswered, which is how a deployment without the buttons runs.
func NewCardActionHandler(api APIClient, creds CredentialsResolver, actions channel.ActionHandler, logger *slog.Logger) CardActionHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &larkCardActions{api: api, creds: creds, actions: actions, logger: logger}
}

// HandleCardAction applies one press. An event from another app, a press without
// a signed token, or a press from a sender without an open id changes nothing and
// gets no reply. Otherwise the presser gets the result as a direct message, and a
// settled decision loses its buttons from the card.
func (h *larkCardActions) HandleCardAction(ctx context.Context, inst Installation, payload []byte) {
	if h.actions == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, cardActionTimeout)
	defer cancel()

	var env cardActionEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		h.logger.WarnContext(ctx, "lark: card press is not a valid event", "app_id", inst.AppID, "error", err)
		return
	}
	if env.Header.AppID != inst.AppID {
		return
	}
	openID := env.Event.Operator.OpenID
	token, ok := cardActionToken(env.Event.Action.Value)
	if openID == "" || !ok {
		return
	}
	creds, err := installationCredentialsFor(inst, h.creds)
	if err != nil {
		h.logger.WarnContext(ctx, "lark: card press cannot read installation credentials", "app_id", inst.AppID, "error", err)
		return
	}

	res, err := h.actions.Press(ctx, channel.Press{
		Sender: channel.Sender{
			ChannelType:    channel.TypeFeishu,
			InstallationID: inst.ID,
			ChannelUserID:  openID,
			// The installation's own bot is an agent's identity; an agent never
			// presses a button.
			IsBot: inst.BotOpenID != "" && openID == inst.BotOpenID,
		},
		Data: token,
	})
	if err != nil {
		h.logger.WarnContext(ctx, "lark: button press failed", "app_id", inst.AppID, "error", err)
		res = channel.PressResult{Reply: msgCardPressFailed}
	}
	if text := pressText(res); text != "" {
		h.reply(ctx, creds, openID, text)
	}
	if res.Settled {
		h.removeSettledButtons(ctx, creds, env.Event.Context.OpenMessageID, token)
	}
}

// pressText is the text the presser sees: the reply, or the toast when there is
// no reply.
func pressText(res channel.PressResult) string {
	if res.Reply != "" {
		return res.Reply
	}
	return res.Toast
}

// cardActionToken reads the signed token out of a callback button's value, which
// is an object keyed by cardActionValueKey. Anything else is not a brief button.
func cardActionToken(raw json.RawMessage) (string, bool) {
	var value map[string]string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	token := value[cardActionValueKey]
	return token, token != ""
}

// reply sends text to the presser's direct message. A failure is logged: the
// press is decided already, and a lost reply must not undo it.
func (h *larkCardActions) reply(ctx context.Context, creds InstallationCredentials, openID, text string) {
	content, err := jsonBytes(map[string]string{"text": text})
	if err != nil {
		h.logger.WarnContext(ctx, "lark: press reply encode failed", "app_id", creds.AppID, "error", err)
		return
	}
	if _, err := h.api.SendUserMessage(ctx, creds, SendUserMessageParams{
		OpenID:  openID,
		MsgType: "text",
		Content: content,
	}); err != nil {
		h.logger.WarnContext(ctx, "lark: press reply failed", "app_id", creds.AppID, "error", err)
	}
}

// removeSettledButtons takes out every button of the decision the pressed token
// acted on, and leaves the rest of the card as it was. It reads the card back
// from Lark, so a card whose content cannot be read keeps its buttons, and a
// second press on it still gets "already decided" from the server.
func (h *larkCardActions) removeSettledButtons(ctx context.Context, creds InstallationCredentials, messageID, pressedToken string) {
	key := h.actions.ActionKey(pressedToken)
	if key == "" || messageID == "" {
		return
	}
	items, err := h.api.GetMessage(ctx, creds, messageID)
	if err != nil || len(items) == 0 {
		h.logger.WarnContext(ctx, "lark: read card for settled buttons failed", "app_id", creds.AppID, "message_id", messageID, "error", err)
		return
	}
	card, removed, err := withoutDecisionCard(items[0].Content, h.actions, key)
	if err != nil {
		h.logger.WarnContext(ctx, "lark: card content unreadable for settled buttons", "app_id", creds.AppID, "message_id", messageID, "error", err)
		return
	}
	if !removed {
		return
	}
	if err := h.api.PatchInteractiveCard(ctx, PatchCardParams{InstallationID: creds, LarkCardMessageID: messageID, CardJSON: card}); err != nil {
		h.logger.WarnContext(ctx, "lark: remove settled buttons failed", "app_id", creds.AppID, "message_id", messageID, "error", err)
	}
}

// withoutDecisionCard returns the card JSON with every button of the decision key
// removed. A column left with no element goes, and so does a column set left with
// no column. The rest of the card is kept as it was. removed reports whether any
// button went.
func withoutDecisionCard(content string, actions channel.ActionHandler, key string) (string, bool, error) {
	var doc any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return "", false, fmt.Errorf("card content: %w", err)
	}
	removed := false
	cleaned, _ := dropDecisionButtons(doc, actions, key, &removed)
	if !removed {
		return "", false, nil
	}
	out, err := json.Marshal(cleaned)
	if err != nil {
		return "", false, fmt.Errorf("encode card: %w", err)
	}
	return string(out), true, nil
}

// dropDecisionButtons walks a decoded card. It returns the node without the
// decision's buttons, and false when the node itself goes.
func dropDecisionButtons(node any, actions channel.ActionHandler, key string, removed *bool) (any, bool) {
	switch v := node.(type) {
	case []any:
		kept := make([]any, 0, len(v))
		for _, item := range v {
			if out, keep := dropDecisionButtons(item, actions, key, removed); keep {
				kept = append(kept, out)
			}
		}
		return kept, true
	case map[string]any:
		if v["tag"] == "button" && buttonBelongsToDecision(v, actions, key) {
			*removed = true
			return nil, false
		}
		for name, child := range v {
			if out, keep := dropDecisionButtons(child, actions, key, removed); keep {
				v[name] = out
			} else {
				delete(v, name)
			}
		}
		if v["tag"] == "column" && emptyList(v["elements"]) {
			return nil, false
		}
		if v["tag"] == "column_set" && emptyList(v["columns"]) {
			return nil, false
		}
		return v, true
	}
	return node, true
}

// emptyList reports whether a decoded field is an array with nothing in it.
func emptyList(v any) bool {
	list, ok := v.([]any)
	return ok && len(list) == 0
}

// buttonBelongsToDecision reports whether a button acts on the decision key. A
// callback behavior names its proposal in its signed token; an open_url behavior
// names it in the decision parameter of its link.
func buttonBelongsToDecision(button map[string]any, actions channel.ActionHandler, key string) bool {
	behaviors, _ := button["behaviors"].([]any)
	for _, raw := range behaviors {
		behavior, _ := raw.(map[string]any)
		switch behavior["type"] {
		case "callback":
			value, _ := behavior["value"].(map[string]any)
			token, _ := value[cardActionValueKey].(string)
			if token != "" && actions.ActionKey(token) == key {
				return true
			}
		case "open_url":
			link, _ := behavior["default_url"].(string)
			if u, err := url.Parse(link); err == nil && u.Query().Get("decision") == key {
				return true
			}
		}
	}
	return false
}
