package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// dingtalkChannel is ONE installation's DingTalk Stream connection. Every
// installation carries its own robot — its own AppKey/AppSecret (stored
// encrypted in the installation config) — so it gets its own connection, exactly
// like the per-installation Slack and Feishu adapters. The engine.Supervisor
// builds one dingtalkChannel per active installation (via the registered
// Factory) and owns the lease / reconnect lifecycle; Connect blocks until the
// run context is cancelled.
//
// Inbound events are translated by inbound.go, parameterized by THIS
// installation's AppKey so the engine router can resolve the installation (the
// DingTalk callback carries no robot code). Outbound replies flow through the
// EventChatDone subscriber (outbound.go) and the OutboundReplier; Send satisfies
// the Channel contract for a group reply.
type dingtalkChannel struct {
	appID     string // AppKey — routing key stamped into each inbound envelope
	robotCode string
	appKey    string
	appSecret string // decrypted — opens the Stream connection + mints tokens
	client    *Client
	handler   channel.InboundHandler
	logger    *slog.Logger
	// dispatch runs inbound jobs off the socket read loop on per-conversation
	// serial queues (see dispatch.go). Built once per channel in the factory;
	// it survives redials, and in-flight jobs deliberately outlive the socket.
	dispatch *dispatcher
}

func (c *dingtalkChannel) Type() channel.Type { return TypeDingTalk }

func (c *dingtalkChannel) Capabilities() channel.Capability {
	return channel.CapText | channel.CapAttachment
}

// Disconnect is a no-op: the Stream connection's whole lifetime is scoped to
// Connect (it returns when the run context is cancelled), so there is no
// long-lived resource to release here. Mirrors feishuChannel.Disconnect.
func (c *dingtalkChannel) Disconnect(ctx context.Context) error { return nil }

// Send posts a group reply into out.ChatID with this installation's robot. It
// satisfies the Channel contract; the primary reply paths (EventChatDone
// subscriber and OutboundReplier) build their own sender with a full target.
func (c *dingtalkChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	s := &sender{client: c.client, robotCode: c.robotCode, appKey: c.appKey, appSecret: c.appSecret}
	key, err := s.send(ctx, sendTarget{ConversationType: convTypeGroup, ConversationID: out.ChatID}, out.Text)
	if err != nil {
		return channel.SendResult{}, err
	}
	return channel.SendResult{MessageID: key}, nil
}

// Connect opens this installation's Stream connection (authenticated with its
// own AppKey/AppSecret) and blocks until ctx is cancelled. The connector owns a
// single socket session and returns on ctx cancel, a gateway disconnect, or a
// broken socket; the engine.Supervisor owns reconnect/backoff and the
// per-installation lease, so an error here just triggers a supervised redial.
func (c *dingtalkChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("dingtalk: inbound handler not configured")
	}
	if c.appSecret == "" {
		return errors.New("dingtalk: app secret not configured")
	}
	conn := &wsConnector{
		httpClient: c.client.httpClient,
		apiBase:    c.client.apiBase,
		appKey:     c.appKey,
		appSecret:  c.appSecret,
		onMessage:  c.onMessage,
		logger:     c.logger,
	}
	return conn.run(ctx)
}

// onMessage is the connector's bot-message callback. It translates the event
// with THIS installation's AppKey and enqueues it on the per-conversation
// dispatcher, so the socket read loop ACKs immediately and is never blocked
// by pipeline work (media downloads can take tens of seconds). It always
// returns nil: DingTalk never redelivers robot messages and the engine's
// (installation, msgId) dedup guards any duplicate anyway.
func (c *dingtalkChannel) onMessage(ctx context.Context, data *botCallbackData) error {
	msg, ok := inboundFromCallback(data, c.appID)
	if !ok {
		// The message never reaches the engine (no sender staff id — a system
		// or bot-authored event), so no channel_inbound_audit row is written
		// for it. Log the drop so the report is diagnosable instead of
		// vanishing silently. (Malformed/over-quota media now DOES reach the
		// engine as MediaUnreadable, for an identity-gated refusal.)
		if data != nil {
			c.logger.InfoContext(ctx, "dingtalk: dropped unsupported inbound message",
				"app_id", c.appID, "msg_type", data.Msgtype, "msg_id", data.MsgId,
				"has_sender", data.SenderStaffId != "")
		}
		return nil
	}
	c.dispatch.enqueue(msg.Source.ChatID, msg)
	return nil
}

// runInbound is the dispatcher's job body: hand the message to the engine and
// surface pipeline errors the way the old inline path did.
func (c *dingtalkChannel) runInbound(ctx context.Context, msg channel.InboundMessage) {
	if err := c.handler(ctx, msg); err != nil {
		c.logger.WarnContext(ctx, "dingtalk: inbound handler error", "error", err, "app_id", c.appID)
		c.notifyIssueDispatchError(msg)
	}
}

// issueErrorReplyTimeout bounds the detached dispatch-error reply send.
const issueErrorReplyTimeout = 5 * time.Second

// notifyIssueDispatchError posts an internal-error notice when an addressed
// /issue command failed inside the engine pipeline (a transient resolver / DB
// error, before the quick-create branch could report through Result flags).
// The frame is already ACKed and never redelivered, so without this the
// command would vanish silently. Detached so onMessage returns promptly.
func (c *dingtalkChannel) notifyIssueDispatchError(msg channel.InboundMessage) {
	if !isAddressedIssueCommand(msg) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), issueErrorReplyTimeout)
		defer cancel()
		s := &sender{client: c.client, robotCode: c.robotCode, appKey: c.appKey, appSecret: c.appSecret}
		if _, err := s.send(ctx, targetFromMessage(msg), engine.IssueQueueFailedText); err != nil {
			c.logger.WarnContext(ctx, "dingtalk: issue dispatch-error reply failed",
				"error", err, "app_id", c.appID)
		}
	}()
}

// ChannelDeps are the shared dependencies the DingTalk Factory closes over. The
// engine inbound handler is supplied per-build via channel.Config.Handler; the
// Decrypter turns the installation's stored ciphertext AppSecret into plaintext;
// the Client owns the outbound token cache + transport.
type ChannelDeps struct {
	Decrypt Decrypter
	Client  *Client
	Logger  *slog.Logger
}

// RegisterDingTalk registers the per-installation DingTalk Factory so the
// engine.Supervisor builds + supervises one dingtalkChannel per active
// installation.
func RegisterDingTalk(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeDingTalk, newDingTalkFactory(deps))
}

func newDingTalkFactory(deps ChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	dtClient := deps.Client
	if dtClient == nil {
		dtClient = NewClient(nil, "")
	}
	return func(cfg channel.Config) (channel.Channel, error) {
		var ic installConfig
		if err := json.Unmarshal(cfg.Raw, &ic); err != nil {
			return nil, fmt.Errorf("dingtalk: decode installation config: %w", err)
		}
		appSecret, err := decryptToken(ic.AppSecretEncrypted, deps.Decrypt)
		if err != nil {
			return nil, fmt.Errorf("dingtalk: decrypt app secret: %w", err)
		}
		if appSecret == "" {
			return nil, errors.New("dingtalk: installation has no app secret")
		}
		ch := &dingtalkChannel{
			appID:     ic.AppID,
			robotCode: ic.robotCodeOrAppID(),
			appKey:    ic.AppID,
			appSecret: appSecret,
			client:    dtClient,
			handler:   cfg.Handler,
			logger:    logger,
		}
		ch.dispatch = newDispatcher(ch.runInbound, logger)
		return ch, nil
	}
}
