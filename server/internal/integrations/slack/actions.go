package slack

import (
	"context"
	"net/url"
	"time"

	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file is the Slack side of button presses (spec 2.7, plan 9.11a). A press
// of a brief push button arrives as a block_actions interaction over the
// installation's Socket Mode connection, so no request URL or signature check
// is needed. What the press means is decided by channel.ActionHandler on the
// server, the same handler Telegram uses. This file only carries Slack's payload
// in and renders what comes back: an ephemeral reply to the presser, and the
// message with the buttons of a settled decision removed.

// User-facing copy the adapter speaks itself, when the server's answer cannot be
// had (an infrastructure failure).
const msgSlackPressFailed = "That did not go through, so nothing changed. Open the app to decide."

// slackPressTimeout bounds one press: the decision and the Web API calls that
// answer it. Slack expects the press to be answered within a few seconds of the
// ACK, and the reply goes out on the interaction's response_url.
const slackPressTimeout = 20 * time.Second

// dispatchInteraction handles one interactive payload on a detached goroutine
// with its own bounded context, so the decision and the Web API calls never
// block the socket receive loop. The envelope has already been ACKed. A nil
// handler leaves presses unanswered.
func (c *slackChannel) dispatchInteraction(cb slack.InteractionCallback) {
	if c.actions == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), slackPressTimeout)
		defer cancel()
		c.press(ctx, cb)
	}()
}

// press applies one button press. A link button (Open the brief, Revise) carries
// no value and is opened by the browser, so it is not a press. The reply goes to
// the presser; a settled decision loses its buttons from the message.
func (c *slackChannel) press(ctx context.Context, cb slack.InteractionCallback) {
	if cb.Type != slack.InteractionTypeBlockActions {
		return
	}
	var action *slack.BlockAction
	for _, a := range cb.ActionCallback.BlockActions {
		if a != nil && a.Value != "" {
			action = a
			break
		}
	}
	if action == nil {
		return
	}

	res, err := c.actions.Press(ctx, channel.Press{
		Sender: channel.Sender{
			ChannelType:    TypeSlack,
			InstallationID: c.installationID,
			ChannelUserID:  cb.User.ID,
			// The installation's own bot is an agent's Slack identity; an agent
			// never presses a button.
			IsBot: c.botUserID != "" && cb.User.ID == c.botUserID,
		},
		Data: action.Value,
	})
	if err != nil {
		c.logger.WarnContext(ctx, "slack: button press failed", "app_id", c.appID, "error", err)
		res = channel.PressResult{Reply: msgSlackPressFailed}
	}
	if text := pressReply(res); text != "" {
		c.replyToPress(ctx, cb.ResponseURL, text)
	}
	if res.Settled {
		c.removeSettledButtons(ctx, cb, action.Value)
	}
}

// pressReply is the text the presser sees: the reply, or the toast when there is
// no reply.
func pressReply(res channel.PressResult) string {
	if res.Reply != "" {
		return res.Reply
	}
	return res.Toast
}

// replyToPress posts an ephemeral reply to the interaction's response_url. Only
// the presser sees it, which suits refusals and the decided result alike. A
// failure is logged: the press is decided already.
func (c *slackChannel) replyToPress(ctx context.Context, responseURL, text string) {
	if responseURL == "" {
		return
	}
	if err := slack.PostWebhookContext(ctx, responseURL, &slack.WebhookMessage{
		ResponseType: slack.ResponseTypeEphemeral,
		Text:         escapeMrkdwn(text),
	}); err != nil {
		c.logger.WarnContext(ctx, "slack: press reply failed", "app_id", c.appID, "error", err)
	}
}

// removeSettledButtons takes out every button of the decision the pressed button
// acted on: its callback buttons, and its Revise link, which names the proposal
// in its decision parameter. The other decisions' buttons stay, and the message
// text is kept as it was posted.
func (c *slackChannel) removeSettledButtons(ctx context.Context, cb slack.InteractionCallback, pressedValue string) {
	key := c.actions.ActionKey(pressedValue)
	if key == "" {
		return
	}
	channelID := cb.Container.ChannelID
	if channelID == "" {
		channelID = cb.Channel.ID
	}
	ts := cb.Container.MessageTs
	if ts == "" {
		ts = cb.Message.Timestamp
	}
	if channelID == "" || ts == "" {
		return
	}
	blocks, removed := withoutDecision(cb.Message.Blocks.BlockSet, c.actions, key)
	if !removed {
		return
	}
	if _, _, _, err := c.botAPI.UpdateMessageContext(ctx, channelID, ts,
		slack.MsgOptionText(cb.Message.Text, false),
		slack.MsgOptionBlocks(blocks...),
	); err != nil {
		c.logger.WarnContext(ctx, "slack: remove settled buttons failed", "app_id", c.appID, "error", err)
	}
}

// withoutDecision returns the blocks with every button of the decision key
// removed. An actions block left with no button goes; the other blocks, and an
// actions block that lost nothing, are kept as they were. removed reports
// whether any button went.
func withoutDecision(blocks []slack.Block, actions channel.ActionHandler, key string) ([]slack.Block, bool) {
	removed := false
	out := make([]slack.Block, 0, len(blocks))
	for _, block := range blocks {
		row, ok := block.(*slack.ActionBlock)
		if !ok || row.Elements == nil {
			out = append(out, block)
			continue
		}
		var kept []slack.BlockElement
		dropped := false
		for _, element := range row.Elements.ElementSet {
			if button, ok := element.(*slack.ButtonBlockElement); ok && buttonBelongsTo(button, actions, key) {
				dropped = true
				removed = true
				continue
			}
			kept = append(kept, element)
		}
		switch {
		case len(kept) == 0:
			continue
		case dropped:
			out = append(out, slack.NewActionBlock(row.BlockID, kept...))
		default:
			out = append(out, block)
		}
	}
	return out, removed
}

// buttonBelongsTo reports whether a button acts on the decision key. A callback
// button names its proposal in its signed token; a link names it in its decision
// parameter.
func buttonBelongsTo(b *slack.ButtonBlockElement, actions channel.ActionHandler, key string) bool {
	if b.Value != "" {
		return actions.ActionKey(b.Value) == key
	}
	u, err := url.Parse(b.URL)
	return err == nil && u.Query().Get("decision") == key
}
