package wecom

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file is the WeCom side of button presses (spec 2.7). A press of a brief
// card's button arrives as a template_card_event on the installation's socket,
// carrying the button's key: the signed token. What the press means is decided by
// channel.ActionHandler on the server, the same handler Telegram and Slack use.
// This file only carries WeCom's event in and sends what comes back to the chat.

// wecomPressTimeout bounds one press: the decision and the reply.
const wecomPressTimeout = 20 * time.Second

// msgWecomPressFailed is what the presser sees when the decision could not be read
// or written. Nothing changed in that case.
const msgWecomPressFailed = "That did not go through, so nothing changed. Open the app to decide."

// pressCard applies one card press. It runs on its own goroutine: the decision and
// the reply touch the database and the socket, and the callbacks worker, which
// also carries inbound messages, should not wait for them.
func (c *wecomChannel) pressCard(ec aibotEventCallback, sender *wsSender, log *slog.Logger) {
	if c.actions == nil || ec.Event.EventKey == "" {
		return
	}
	chatID, chatType := ec.From.UserID, chatTypeSingleInt
	if ec.ChatType == "group" {
		chatID, chatType = ec.ChatID, chatTypeGroupInt
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), wecomPressTimeout)
		defer cancel()

		res, err := c.actions.Press(ctx, channel.Press{
			Sender: channel.Sender{
				ChannelType:    TypeWecom,
				InstallationID: c.installationID,
				ChannelUserID:  ec.From.UserID,
			},
			Data: ec.Event.EventKey,
		})
		if err != nil {
			log.WarnContext(ctx, "wecom: card press failed", "error", err)
			res = channel.PressResult{Reply: msgWecomPressFailed}
		}
		text := res.Reply
		if text == "" {
			text = res.Toast
		}
		if text == "" {
			return
		}
		if err := sender.sendTextCtx(ctx, chatID, chatType, text); err != nil {
			log.WarnContext(ctx, "wecom: card press reply failed", "error", err)
		}
	}()
}
