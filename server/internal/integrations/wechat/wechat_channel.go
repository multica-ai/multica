package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// wechatChannel is ONE installation's iLink long-poll connection. Each WeChat
// installation carries its own bot_token + base_url (returned by the QR-login
// status poll when the installer scans), so it gets its own connection, exactly
// like the per-installation Feishu and Slack models. The engine.Supervisor
// builds one wechatChannel per active WeChat installation (via the registered
// Factory) and owns the lease / reconnect lifecycle; Connect blocks on the
// long-poll receive loop.
//
// Inbound messages are translated by inbound.go and handed to the engine router,
// which resolves the installation by the message's to_user_id (the bot id) —
// equal to this installation's app_id, the per-installation routing key.
// Outbound replies flow through the EventChatDone subscriber (outbound.go);
// Send satisfies the Channel contract for the engine's detached OutboundReplier.
type wechatChannel struct {
	appID    string
	botToken string // decrypted iLink bot_token
	baseURL  string // per-account API host
	client   *iLinkClient
	handler  channel.InboundHandler
	logger   *slog.Logger
}

func (c *wechatChannel) Type() channel.Type { return TypeWechat }

func (c *wechatChannel) Capabilities() channel.Capability {
	// MVP: plain text only. WeChat iLink can carry media/voice, but the AES-ECB
	// CDN pipeline is deferred to a later phase. No thread-reply / typing
	// capabilities are declared.
	return channel.CapText
}

// Disconnect is a no-op: the long-poll loop's whole lifetime is scoped to
// Connect (it returns when the run context is cancelled), so there is no
// long-lived resource to release here. Mirrors slackChannel.Disconnect and
// feishuChannel.Disconnect.
func (c *wechatChannel) Disconnect(ctx context.Context) error { return nil }

// Send posts an outbound text reply via iLink sendmessage. The context_token
// required to associate the reply with the conversation is taken from
// out.ReplyTo (the inbound message id) when the engine's OutboundReplier sets
// it; the primary outbound path is the EventChatDone subscriber in outbound.go,
// which recovers the token from the session binding. Send is the fallback the
// engine contract requires.
func (c *wechatChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	if c.client == nil {
		return channel.SendResult{}, errors.New("wechat: client not configured")
	}
	// OutboundMessage.ChatID for WeChat carries "<toUserID>\t<contextToken>" so a
	// decoupled Send can recover both without a DB read. The outbound subscriber
	// and replier build this encoding; see encodeSendTarget.
	toUserID, contextToken := decodeSendTarget(out.ChatID)
	if contextToken == "" {
		return channel.SendResult{}, errors.New("wechat: outbound reply missing context_token")
	}
	// Chunk long bodies conservatively; each chunk is a separate sendmessage
	// sharing the same context_token.
	var lastID string
	for _, chunk := range chunkMessage(out.Text, maxMessageRunes) {
		id, err := c.client.sendMessage(ctx, c.botToken, c.baseURL, contextToken, toUserID, chunk)
		if err != nil {
			if ctx.Err() != nil {
				return channel.SendResult{}, nil
			}
			return channel.SendResult{}, fmt.Errorf("wechat: sendmessage: %w", err)
		}
		lastID = id
	}
	return channel.SendResult{MessageID: lastID}, nil
}

// Connect runs the iLink long-poll receive loop until ctx is cancelled (graceful
// shutdown / lease loss → returns nil) or a non-ctx error occurs (the supervisor
// reconnects under exponential backoff). It deliberately does NOT implement its
// own outer retry/backoff loop — the supervisor owns reconnection. Every HTTP
// call uses ctx as the request context so lease-loss cancelRun() interrupts an
// in-flight long poll in bounded time.
func (c *wechatChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("wechat: inbound handler not configured")
	}
	if c.botToken == "" {
		return errors.New("wechat: bot token not configured")
	}
	if c.baseURL == "" {
		return errors.New("wechat: base url not configured")
	}
	// The cursor is held in memory only. On cold restart the first getupdates
	// uses an empty cursor and may re-receive recent messages; the engine's
	// two-phase dedup (channel_inbound_message_dedup) makes that idempotent.
	cursor := ""
	for {
		if ctx.Err() != nil {
			return nil
		}
		result, err := c.client.getUpdates(ctx, c.botToken, c.baseURL, cursor)
		if err != nil {
			// ctx cancellation is graceful: return nil so the supervisor does
			// not treat a shutdown as a reconnectable failure.
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wechat: getupdates: %w", err)
		}
		cursor = result.NextCursor
		for _, m := range result.Messages {
			// Skip messages with no stable id — they cannot be deduped and would
			// risk infinite reprocessing on reconnect.
			if m.MsgID == "" {
				c.logger.WarnContext(ctx, "wechat: inbound message missing msg_id; skipping", "from", m.FromUserID)
				continue
			}
			inbound := inboundFromIlink(m)
			// A non-nil handler error is an infrastructure failure; propagate so
			// the supervisor reconnects. The router returns nil for legitimate
			// product drops (unbound user, duplicate, non-member).
			if err := c.handler(ctx, inbound); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wechat: inbound handler: %w", err)
			}
		}
	}
}

// encodeSendTarget packs the WeChat destination user id and the inbound
// context_token into the OutboundMessage.ChatID slot, so a decoupled Send (no
// DB access) can recover both. Tab is the separator because WeChat ids are
// "xxx@im.wechat" / "xxx@im.bot" and never contain a tab.
func encodeSendTarget(toUserID, contextToken string) string {
	return toUserID + "\t" + contextToken
}

// decodeSendTarget is the inverse of encodeSendTarget. A value without a tab is
// treated as a bare user id with no context token.
func decodeSendTarget(chatID string) (toUserID, contextToken string) {
	if idx := strings.IndexByte(chatID, '\t'); idx >= 0 {
		return chatID[:idx], chatID[idx+1:]
	}
	return chatID, ""
}

// chunkMessage splits text into <=maxRunes-rune pieces on rune boundaries so a
// long agent reply does not exceed the conservative per-message cap. An empty
// body yields a single empty chunk (the caller guards against truly empty text
// upstream). Mirrors slack.chunkMessage.
func chunkMessage(text string, maxRunes int) []string {
	if maxRunes <= 0 || len([]rune(text)) <= maxRunes {
		return []string{text}
	}
	runes := []rune(text)
	var chunks []string
	for len(runes) > 0 {
		n := maxRunes
		if n > len(runes) {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}

// ChannelDeps are the shared dependencies the WeChat Factory closes over. The
// engine inbound handler is supplied per-build via channel.Config.Handler; the
// Decrypter turns the installation's stored ciphertext bot token into plaintext.
type ChannelDeps struct {
	Decrypt Decrypter
	Logger  *slog.Logger
	// BaseURL overrides the default iLink host for the QR-login flow (proxy /
	// mock / single-cloud staging). Per-account base_url from the QR-login
	// response always wins for getupdates/sendmessage.
	BaseURL string
}

// RegisterWeChat registers the per-installation WeChat Factory so the
// engine.Supervisor builds + supervises one wechatChannel per active WeChat
// installation. "Adding WeChat inbound" is this call plus the adapter — no
// engine edit (the same contract as lark.RegisterFeishu and slack.RegisterSlack).
func RegisterWeChat(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeWechat, newWechatFactory(deps))
}

func newWechatFactory(deps ChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	client := newILinkClient(deps.BaseURL, logger)
	return func(cfg channel.Config) (channel.Channel, error) {
		var ic installConfig
		if err := json.Unmarshal(cfg.Raw, &ic); err != nil {
			return nil, fmt.Errorf("wechat: decode installation config: %w", err)
		}
		botToken, err := decryptToken(ic.BotTokenEncrypted, deps.Decrypt)
		if err != nil {
			return nil, fmt.Errorf("wechat: decrypt bot token: %w", err)
		}
		if botToken == "" {
			return nil, errors.New("wechat: installation has no bot token")
		}
		if ic.BaseURL == "" {
			return nil, errors.New("wechat: installation has no base url")
		}
		return &wechatChannel{
			appID:    ic.AppID,
			botToken: botToken,
			baseURL:  ic.BaseURL,
			client:   client,
			handler:  cfg.Handler,
			logger:   logger,
		}, nil
	}
}
