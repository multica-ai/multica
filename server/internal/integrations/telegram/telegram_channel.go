package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// telegramChannel is ONE installation's getUpdates long-polling loop. Telegram
// has no WebSocket transport; long polling is the persistent-connection
// equivalent — one blocking loop per bot token, supervised per-installation by
// engine.Supervisor exactly like Feishu's WS long-conn and Slack's Socket
// Mode. The single-consumer constraint (Telegram 409s a second getUpdates
// consumer) maps 1:1 onto the Supervisor's WS-lease guarantee of at most one
// active loop per installation across replicas.
type telegramChannel struct {
	botID       int64
	botUsername string
	api         *botAPI
	handler     channel.InboundHandler
	logger      *slog.Logger
}

// pollRetryDelay spaces retries after a transient getUpdates failure inside
// one Connect attempt before giving the error to the Supervisor's backoff.
const pollRetryDelay = 2 * time.Second

func (c *telegramChannel) Type() channel.Type { return TypeTelegram }

func (c *telegramChannel) Capabilities() channel.Capability {
	return channel.CapText | channel.CapThreadReply | channel.CapQuoteReply |
		channel.CapTypingIndicator | channel.CapMessageEdit
}

// Disconnect is a no-op: the polling loop's whole lifetime is scoped to
// Connect (it returns when the run context is cancelled). Mirrors
// slackChannel.Disconnect.
func (c *telegramChannel) Disconnect(ctx context.Context) error { return nil }

// Send posts an outbound reply with this installation's bot token, reusing
// the shared sender (Markdown→Telegram HTML, chunking, threading).
func (c *telegramChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	return newSender(c.api, c.logger).Send(ctx, out)
}

// Connect runs the getUpdates long-polling receive loop until ctx is
// cancelled (returns nil — graceful stop) or the link fails in a way the loop
// cannot absorb (returns the error — the Supervisor reconnects under
// backoff). A 409 Conflict is surfaced as ErrConflict with a precise message:
// it means another consumer is polling this bot token, a state backoff cannot
// fix but the operator can.
func (c *telegramChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("telegram: inbound handler not configured")
	}
	// offset starts at 0: Telegram then delivers all pending updates, and the
	// engine's (installation, message_id) dedup absorbs any the previous run
	// already processed — the same replay tolerance the Feishu WS reconnect
	// relies on.
	var offset int64
	for {
		updates, err := c.api.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, ErrConflict) {
				c.logger.WarnContext(ctx, "telegram: getUpdates conflict — this bot token is polled by another instance; stop the other consumer or use a distinct bot per environment",
					"bot_id", c.botID)
				return err
			}
			if wait, ok := retryAfter(err); ok {
				c.logger.WarnContext(ctx, "telegram: getUpdates rate limited", "bot_id", c.botID, "retry_after", wait)
				if !sleepCtx(ctx, wait) {
					return nil
				}
				continue
			}
			// Transient network/API failure: one spaced retry loop inside the
			// attempt keeps a momentary blip from churning the Supervisor's
			// backoff; persistent failure still escalates via repeated errors.
			c.logger.WarnContext(ctx, "telegram: getUpdates failed", "bot_id", c.botID, "error", err)
			if !sleepCtx(ctx, pollRetryDelay) {
				return nil
			}
			return fmt.Errorf("telegram: getUpdates: %w", err)
		}
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if err := c.dispatch(ctx, u); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

// dispatch translates one update and hands it to the engine. A non-nil
// handler error is an infrastructure failure and propagates (Supervisor
// reconnects; the un-advanced updates re-deliver and dedup absorbs
// duplicates). Product drops return nil. Unsupported media in a private chat,
// or explicitly addressed to the bot in a group, gets a courteous notice.
func (c *telegramChannel) dispatch(ctx context.Context, u Update) error {
	msg, ok := inboundFromUpdate(u, c.botID, c.botUsername)
	if !ok {
		return nil
	}
	if msg.Type != channel.MsgTypeText {
		if msg.Source.ChatType == channel.ChatTypeP2P || msg.AddressedToBot {
			c.notifyUnsupported(ctx, u)
		}
		return nil
	}
	if msg.Text == "" {
		return nil
	}
	if err := c.handler(ctx, msg); err != nil {
		c.notifyIssueDispatchError(msg)
		return err
	}
	return nil
}

const (
	issueErrorReplyTimeout  = 5 * time.Second
	issueDispatchFailedText = "⚠️ 创建任务时发生内部错误，请稍后重试。"
)

// notifyIssueDispatchError prevents an addressed /issue command from failing
// silently when the engine returns an infrastructure error before it can
// produce a normal outcome. It is detached from the polling loop so the
// Supervisor can reconnect without waiting on the best-effort notice.
func (c *telegramChannel) notifyIssueDispatchError(msg channel.InboundMessage) {
	if !isAddressedIssueCommand(msg) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), issueErrorReplyTimeout)
		defer cancel()
		chatID, err := strconv.ParseInt(msg.Source.ChatID, 10, 64)
		if err != nil {
			c.logger.Warn("telegram: issue dispatch-error reply has invalid chat id", "error", err)
			return
		}
		var threadID int64
		if msg.Source.ThreadID != "" {
			threadID, _ = strconv.ParseInt(msg.Source.ThreadID, 10, 64)
		}
		if _, err := c.api.SendMessage(ctx, sendMessageParams{
			ChatID:          chatID,
			Text:            issueDispatchFailedText,
			MessageThreadID: threadID,
			ReplyParameters: optionalReplyParameters(parseMessageRef(msg.MessageID)),
		}); err != nil {
			c.logger.WarnContext(ctx, "telegram: issue dispatch-error reply failed", "error", err)
		}
	}()
}

// notifyUnsupported tells an interacting sender their non-text message type is
// not handled yet. Preserve topic routing and quote the triggering message so
// the notice remains unambiguous in a busy group. Best-effort; failures are
// logged only.
func (c *telegramChannel) notifyUnsupported(ctx context.Context, u Update) {
	if u.Message == nil {
		return
	}
	if _, err := c.api.SendMessage(ctx, sendMessageParams{
		ChatID:          u.Message.Chat.ID,
		Text:            msgUnsupportedType,
		MessageThreadID: u.Message.MessageThreadID,
		ReplyParameters: &replyParameters{
			MessageID:                u.Message.MessageID,
			AllowSendingWithoutReply: true,
		},
	}); err != nil {
		c.logger.WarnContext(ctx, "telegram: unsupported-type notice failed", "error", err)
	}
}

// sleepCtx sleeps for d or until ctx is done; false means ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ChannelDeps are the shared dependencies the Telegram Factory closes over.
// The engine inbound handler is supplied per-build via channel.Config.Handler.
type ChannelDeps struct {
	Decrypt Decrypter
	Logger  *slog.Logger
	// APIBase overrides the Bot API host (tests). Empty uses production.
	APIBase string
	// HTTPClient overrides the polling client (tests). Nil uses a default with
	// a timeout sized for long polling.
	HTTPClient *http.Client
}

// RegisterTelegram registers the per-installation Telegram Factory so the
// engine.Supervisor builds + supervises one polling loop per active Telegram
// installation. Same contract as lark.RegisterFeishu / slack.RegisterSlack —
// no engine edit.
func RegisterTelegram(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeTelegram, newTelegramFactory(deps))
}

func newTelegramFactory(deps ChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return func(cfg channel.Config) (channel.Channel, error) {
		var ic installConfig
		if err := json.Unmarshal(cfg.Raw, &ic); err != nil {
			return nil, fmt.Errorf("telegram: decode installation config: %w", err)
		}
		token, err := decryptToken(ic.BotTokenEncrypted, deps.Decrypt)
		if err != nil {
			return nil, fmt.Errorf("telegram: decrypt bot token: %w", err)
		}
		if token == "" {
			return nil, errors.New("telegram: installation has no bot token")
		}
		botID, err := strconv.ParseInt(ic.AppID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("telegram: installation app_id is not a bot id: %w", err)
		}
		return &telegramChannel{
			botID:       botID,
			botUsername: ic.BotUsername,
			api:         newBotAPI(deps.APIBase, token, deps.HTTPClient),
			handler:     cfg.Handler,
			logger:      logger,
		}, nil
	}
}
