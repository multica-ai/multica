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
	issueCmd  *IssueCommandProcessor // nil leaves /issue on the engine's direct-create path
	logger    *slog.Logger
}

// issueCommandTimeout bounds the detached processing of one `/issue` command
// (resolve + enqueue + reply). Mirrors slashCommandTimeout on Slack.
const issueCommandTimeout = 10 * time.Second

func (c *dingtalkChannel) Type() channel.Type { return TypeDingTalk }

func (c *dingtalkChannel) Capabilities() channel.Capability {
	return channel.CapText
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
// with THIS installation's AppKey and hands it to the engine. It always returns
// nil (the connector ACKs regardless): DingTalk expires un-ACKed frames quickly
// and the engine's (installation, msgId) dedup guards any redelivery, so a
// handler error is logged, not surfaced to DingTalk.
func (c *dingtalkChannel) onMessage(ctx context.Context, data *botCallbackData) error {
	msg, ok := inboundFromCallback(data, c.appID)
	if !ok {
		return nil
	}
	// `/issue` is a quick-create entry point, not a chat turn: divert an addressed
	// /issue command to the processor instead of the engine. AddressedToBot gates
	// group messages to @-mentions, matching the engine's own filter.
	if c.issueCmd != nil && msg.AddressedToBot {
		if _, isIssue := engine.ParseIssueCommand(msg.Text); isIssue {
			c.dispatchIssueCommand(msg)
			return nil
		}
	}
	if err := c.handler(ctx, msg); err != nil {
		c.logger.WarnContext(ctx, "dingtalk: inbound handler error", "error", err, "app_id", c.appID)
	}
	return nil
}

// dispatchIssueCommand processes an /issue command on a detached goroutine so
// onMessage returns promptly and the Stream frame is ACKed without waiting on
// the outbound reply. ACK-first means the frame is not redelivered, so no dedup
// is needed here — the same reason the Slack slash command does not dedup.
func (c *dingtalkChannel) dispatchIssueCommand(msg channel.InboundMessage) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), issueCommandTimeout)
		defer cancel()
		c.issueCmd.Handle(ctx, msg)
	}()
}

// ChannelDeps are the shared dependencies the DingTalk Factory closes over. The
// engine inbound handler is supplied per-build via channel.Config.Handler; the
// Decrypter turns the installation's stored ciphertext AppSecret into plaintext;
// the Client owns the outbound token cache + transport.
type ChannelDeps struct {
	Decrypt Decrypter
	Client  *Client
	// IssueCommand handles the `/issue` quick-create command diverted from the
	// Stream message flow. Nil leaves /issue on the engine's direct-create path.
	IssueCommand *IssueCommandProcessor
	Logger       *slog.Logger
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
		return &dingtalkChannel{
			appID:     ic.AppID,
			robotCode: ic.robotCodeOrAppID(),
			appKey:    ic.AppID,
			appSecret: appSecret,
			client:    dtClient,
			handler:   cfg.Handler,
			issueCmd:  deps.IssueCommand,
			logger:    logger,
		}, nil
	}
}
