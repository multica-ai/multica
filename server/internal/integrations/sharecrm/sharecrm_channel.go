package sharecrm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// sharecrmChannel is ONE installation's ShareCRM SSE connection.
type sharecrmChannel struct {
	appID     string
	appSecret string
	baseURL   string
	client    *Client
	handler   channel.InboundHandler
	logger    *slog.Logger
	dispatch  *dispatcher
	slot      *dispatchSlot
	botFullID atomic.Value // string

	stopDispatch atomic.Bool
}

func (c *sharecrmChannel) Type() channel.Type { return TypeShareCRM }

func (c *sharecrmChannel) Capabilities() channel.Capability {
	return channel.CapText
}

func (c *sharecrmChannel) Disconnect(ctx context.Context) error {
	if !c.stopDispatch.Load() || c.slot == nil || c.dispatch == nil {
		return nil
	}
	c.slot.mu.Lock()
	if c.slot.current.Load() != c {
		c.slot.mu.Unlock()
		return nil
	}
	c.dispatch.startClose()
	c.slot.mu.Unlock()
	defer c.slot.current.CompareAndSwap(c, nil)
	if !c.dispatch.waitClosed(ctx) {
		return fmt.Errorf("sharecrm: dispatcher drain: %w", ctx.Err())
	}
	return nil
}

func (c *sharecrmChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	text := stripMarkdown(out.Text)
	msgID, err := c.client.SendMessage(ctx, c.baseURL, c.appID, c.appSecret, out.ChatID, text, nil)
	if err != nil {
		return channel.SendResult{}, err
	}
	return channel.SendResult{MessageID: msgID}, nil
}

func (c *sharecrmChannel) Connect(ctx context.Context) (err error) {
	defer func() {
		c.stopDispatch.Store(ctx.Err() != nil)
	}()
	if c.handler == nil {
		return errors.New("sharecrm: inbound handler not configured")
	}
	if c.appSecret == "" {
		return errors.New("sharecrm: app secret not configured")
	}
	conn := &sseConnector{
		httpClient: c.client.httpClient,
		baseURL:    c.baseURL,
		appID:      c.appID,
		appSecret:  c.appSecret,
		client:     c.client,
		onMessage:  c.onMessage,
		logger:     c.logger,
	}
	// One supervised attempt: return when stream ends so Supervisor can
	// reconnect under backoff. The connector's internal loop would fight the
	// Supervisor; use a single openAndRead cycle by wrapping run once.
	return conn.openAndRead(ctx)
}

func (c *sharecrmChannel) onMessage(ctx context.Context, data *botMessageData) error {
	botFullID, _ := c.botFullID.Load().(string)
	if data != nil && data.BotFullID != "" {
		c.botFullID.Store(data.BotFullID)
		botFullID = data.BotFullID
	}
	msg, ok := inboundFromEvent(data, c.appID, botFullID)
	if !ok {
		if data != nil {
			c.logger.InfoContext(ctx, "sharecrm: dropped unsupported inbound",
				"app_id", c.appID, "msg_id", data.MessageID, "has_sender", data.From.ID != "")
		}
		return nil
	}
	c.dispatch.enqueue(msg.Source.ChatID, msg)
	return nil
}

func (c *sharecrmChannel) runInbound(ctx context.Context, msg channel.InboundMessage) {
	if err := c.handler(ctx, msg); err != nil {
		c.logger.WarnContext(ctx, "sharecrm: inbound handler error", "error", err, "app_id", c.appID)
		c.notifyIssueDispatchError(msg)
	}
}

const issueErrorReplyTimeout = 5 * time.Second
const issueDispatchFailedText = "⚠️ I couldn't create that issue because an internal error occurred. Please try again."

func (c *sharecrmChannel) notifyIssueDispatchError(msg channel.InboundMessage) {
	if !isAddressedIssueCommand(msg) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), issueErrorReplyTimeout)
		defer cancel()
		if _, err := c.client.SendMessage(ctx, c.baseURL, c.appID, c.appSecret, msg.Source.ChatID, issueDispatchFailedText, nil); err != nil {
			c.logger.WarnContext(ctx, "sharecrm: issue dispatch-error reply failed",
				"error", err, "app_id", c.appID)
		}
	}()
}

// ChannelDeps are shared dependencies the Factory closes over.
type ChannelDeps struct {
	Decrypt Decrypter
	Client  *Client
	Logger  *slog.Logger
}

type dispatchSlot struct {
	mu      sync.Mutex
	current atomic.Pointer[sharecrmChannel]
	queue   *dispatcher
}

// RegisterShareCRM registers the per-installation Factory.
func RegisterShareCRM(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeShareCRM, newShareCRMFactory(deps))
}

func newShareCRMFactory(deps ChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	scClient := deps.Client
	if scClient == nil {
		scClient = NewClient(nil)
	}
	var dispatchMu sync.Mutex
	dispatchByAppID := make(map[string]*dispatchSlot)
	return func(cfg channel.Config) (channel.Channel, error) {
		var ic installConfig
		if err := json.Unmarshal(cfg.Raw, &ic); err != nil {
			return nil, fmt.Errorf("sharecrm: decode installation config: %w", err)
		}
		appSecret, err := decryptToken(ic.AppSecretEncrypted, deps.Decrypt)
		if err != nil {
			return nil, fmt.Errorf("sharecrm: decrypt app secret: %w", err)
		}
		if appSecret == "" {
			return nil, errors.New("sharecrm: installation has no app secret")
		}
		ch := &sharecrmChannel{
			appID:     ic.AppID,
			appSecret: appSecret,
			baseURL:   ic.gatewayBase(),
			client:    scClient,
			handler:   cfg.Handler,
			logger:    logger,
		}

		dispatchMu.Lock()
		slot := dispatchByAppID[ch.appID]
		if slot != nil {
			slot.mu.Lock()
		}
		if slot == nil || slot.queue.isClosed() {
			if slot != nil {
				slot.mu.Unlock()
			}
			slot = &dispatchSlot{}
			slot.mu.Lock()
			slot.queue = newDispatcher(func(ctx context.Context, msg channel.InboundMessage) {
				if current := slot.current.Load(); current != nil {
					current.runInbound(ctx, msg)
				}
			}, logger)
			dispatchByAppID[ch.appID] = slot
		}
		slot.current.Store(ch)
		ch.dispatch = slot.queue
		ch.slot = slot
		slot.mu.Unlock()
		dispatchMu.Unlock()
		return ch, nil
	}
}
