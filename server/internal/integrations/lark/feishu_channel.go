package lark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// feishuChannel is the Feishu implementation of channel.Channel — the first
// adapter driven by the channel-agnostic engine (MUL-3620). It wraps the
// existing Lark transport: Connect runs the shared WS long-conn connector for
// this installation, translating each decoded event into a normalized
// channel.InboundMessage and handing it to the engine's shared inbound handler
// (the Router, injected via channel.Config.Handler); Send posts a text reply
// through the Lark HTTP API. One instance is built per channel_installation by
// the registered Factory; the connector is shared across instances.
//
// The Channel holds only the credentials it needs for Connect/Send (decoded
// from the per-installation config blob). The installation IDENTITY
// (workspace / agent / installer) is resolved per message by the Router's
// InstallationResolver, so it is deliberately absent here.
type feishuChannel struct {
	inst    Installation
	conn    EventConnector
	handler channel.InboundHandler
	sender  APIClient
	creds   CredentialsResolver
	logger  *slog.Logger
}

var _ channel.Channel = (*feishuChannel)(nil)

func (c *feishuChannel) Type() channel.Type { return channel.TypeFeishu }

// Connect runs the Feishu long connection for this installation, blocking
// until ctx is cancelled or the link drops — the contract engine.Supervisor
// relies on to tie lease renewal to connection liveness. Each decoded event is
// normalized to a channel.InboundMessage and handed to the engine handler. The
// connector discards the (DispatchResult) return and reacts only to the error,
// so the handler's error is what flows back.
func (c *feishuChannel) Connect(ctx context.Context) error {
	return c.conn.Run(ctx, c.inst, func(emitCtx context.Context, lm InboundMessage) (DispatchResult, error) {
		if c.handler == nil {
			return DispatchResult{}, errors.New("lark: inbound handler not configured")
		}
		return DispatchResult{}, c.handler(emitCtx, channelMessageFromLark(lm))
	})
}

// Disconnect is a no-op: the connector's receive loop is torn down by ctx
// cancellation (the Supervisor cancels the run context). Safe to call repeatedly.
func (c *feishuChannel) Disconnect(ctx context.Context) error { return nil }

// Send delivers a minimal text reply via the Lark IM API. Rich cards, media,
// and the streaming card patch stay on the existing Patcher / OutcomeReplier
// paths; this is the cross-platform OutboundMessage path.
func (c *feishuChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	if c.sender == nil {
		return channel.SendResult{}, errors.New("lark: api client not configured")
	}
	creds, err := c.installationCredentials()
	if err != nil {
		return channel.SendResult{}, err
	}
	msgID, err := c.sender.SendTextMessage(ctx, SendTextParams{
		InstallationID: creds,
		ChatID:         ChatID(out.ChatID),
		Text:           out.Text,
		ReplyTarget:    outboundReplyTarget(out),
	})
	if err != nil {
		return channel.SendResult{}, err
	}
	return channel.SendResult{MessageID: msgID}, nil
}

// Capabilities declares what the Feishu adapter supports. Declaration only —
// the engine performs no degradation (channel.Capability docs).
func (c *feishuChannel) Capabilities() channel.Capability {
	return channel.CapText |
		channel.CapRichCard |
		channel.CapThreadReply |
		channel.CapQuoteReply |
		channel.CapAttachment |
		channel.CapTypingIndicator |
		channel.CapMessageEdit
}

func (c *feishuChannel) installationCredentials() (InstallationCredentials, error) {
	if c.creds == nil {
		return InstallationCredentials{}, errors.New("lark: credentials resolver missing")
	}
	secret, err := c.creds.DecryptAppSecret(c.inst)
	if err != nil {
		return InstallationCredentials{}, fmt.Errorf("decrypt app_secret: %w", err)
	}
	creds := InstallationCredentials{
		AppID:     c.inst.AppID,
		AppSecret: secret,
		Region:    RegionOrDefault(c.inst.Region),
	}
	if c.inst.TenantKey.Valid {
		creds.TenantKey = c.inst.TenantKey.String
	}
	return creds, nil
}

// channelMessageFromLark normalizes a decoded Feishu InboundMessage into the
// cross-platform channel.InboundMessage. The original struct is stashed in Raw
// so the Feishu resolvers can read the platform-specific fields (app_id,
// event_type, command body, create time) the envelope does not carry.
func channelMessageFromLark(lm InboundMessage) channel.InboundMessage {
	raw, _ := json.Marshal(lm)
	var reply *channel.ReplyCtx
	if lm.ParentID != "" || lm.RootID != "" {
		reply = &channel.ReplyCtx{MessageID: lm.ParentID, RootID: lm.RootID}
	}
	return channel.InboundMessage{
		EventID:        lm.EventID,
		MessageID:      lm.MessageID,
		Type:           channelMsgType(lm.MessageType),
		Text:           lm.Body,
		ReplyTo:        reply,
		AddressedToBot: lm.AddressedToBot,
		ForceFresh:     lm.ForceFreshSession,
		Source: channel.Source{
			ChannelType: channel.TypeFeishu,
			ChatID:      string(lm.ChatID),
			ChatType:    channel.ChatType(string(lm.ChatType)),
			SenderID:    string(lm.SenderOpenID),
			ThreadID:    lm.ThreadID,
		},
		Raw: raw,
	}
}

// channelMsgType maps the raw Lark msg_type onto the normalized enum. Text-ish
// Lark types (text / post / merge_forward / interactive) all flatten to text
// (the human-readable content is in Body); media types map across.
func channelMsgType(larkMsgType string) channel.MsgType {
	switch larkMsgType {
	case "image":
		return channel.MsgTypeImage
	case "file":
		return channel.MsgTypeFile
	case "audio":
		return channel.MsgTypeAudio
	case "media", "video":
		return channel.MsgTypeVideo
	case "", "text", "post", "merge_forward", "interactive":
		return channel.MsgTypeText
	default:
		return channel.MsgTypeUnknown
	}
}

// outboundReplyTarget maps the cross-platform OutboundMessage reply hints to a
// Lark ReplyTarget. Only a quote-reply (ReplyTo set) routes through the reply
// endpoint; a bare thread continuation falls back to a chat-level send.
func outboundReplyTarget(out channel.OutboundMessage) ReplyTarget {
	if out.ReplyTo == "" {
		return ReplyTarget{}
	}
	return ReplyTarget{MessageID: out.ReplyTo, InThread: out.ThreadID != ""}
}

// FeishuChannelDeps bundles the shared dependencies the Feishu Factory closes
// over: the WS connector (one shared instance), the outbound HTTP client, and
// the credentials resolver. The inbound handler is supplied per-build by the
// engine via channel.Config.Handler.
type FeishuChannelDeps struct {
	Connector   EventConnector
	APIClient   APIClient
	Credentials CredentialsResolver
	Logger      *slog.Logger
}

// RegisterFeishu registers the Feishu Factory on reg under channel.TypeFeishu
// so the engine.Supervisor can build a feishuChannel per installation. "Adding
// a channel" is this call plus the adapter — no engine edit.
func RegisterFeishu(reg *channel.Registry, deps FeishuChannelDeps) {
	reg.Register(channel.TypeFeishu, newFeishuFactory(deps))
}

func newFeishuFactory(deps FeishuChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return func(cfg channel.Config) (channel.Channel, error) {
		if deps.Connector == nil {
			return nil, errors.New("lark: feishu factory missing connector")
		}
		// cfg.Raw is the per-installation config blob (the channel_installation
		// .config JSONB): app_id, encrypted app_secret, tenant_key, region, ….
		// We build a credentials-only Installation from it; the workspace /
		// agent identity is resolved per message by the Router, not needed here.
		inst, err := installationFromRow(db.ChannelInstallation{
			ChannelType: channelTypeFeishu,
			Config:      cfg.Raw,
		})
		if err != nil {
			return nil, fmt.Errorf("decode feishu installation config: %w", err)
		}
		return &feishuChannel{
			inst:    inst,
			conn:    deps.Connector,
			handler: cfg.Handler,
			sender:  deps.APIClient,
			creds:   deps.Credentials,
			logger:  logger,
		}, nil
	}
}

// channelInstallationStore adapts *db.Queries to engine.InstallationStore. It
// enumerates active installations across ALL channel types (the de-hardcoded
// counterpart of the old per-feishu boot list) and manages the per-installation
// WS lease, translating each row into the engine's Installation: ChannelType
// selects the Factory, Config carries the platform config JSONB (verbatim), and
// Fingerprint is a generic hash over channel_type + config so a credential
// rotation forces a reconnect for any platform.
type channelInstallationStore struct {
	q *db.Queries
}

// NewChannelInstallationStore builds the engine.InstallationStore backed by the
// generalized channel_* tables.
func NewChannelInstallationStore(q *db.Queries) engine.InstallationStore {
	return &channelInstallationStore{q: q}
}

func (s *channelInstallationStore) ListActiveInstallations(ctx context.Context) ([]engine.Installation, error) {
	rows, err := s.q.ListAllActiveChannelInstallations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]engine.Installation, 0, len(rows))
	for _, row := range rows {
		out = append(out, engine.Installation{
			ID:          row.ID,
			ChannelType: channel.Type(row.ChannelType),
			Fingerprint: rowFingerprint(row),
			Config:      row.Config,
		})
	}
	return out, nil
}

func (s *channelInstallationStore) AcquireWSLease(ctx context.Context, arg engine.AcquireLeaseParams) error {
	_, err := s.q.AcquireChannelWSLease(ctx, db.AcquireChannelWSLeaseParams{
		NewToken:     pgtype.Text{String: arg.Token, Valid: true},
		NewExpiresAt: pgtype.Timestamptz{Time: arg.ExpiresAt, Valid: true},
		ID:           arg.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ErrLeaseNotAcquired
		}
		return err
	}
	return nil
}

func (s *channelInstallationStore) ReleaseWSLease(ctx context.Context, arg engine.ReleaseLeaseParams) error {
	return s.q.ReleaseChannelWSLease(ctx, db.ReleaseChannelWSLeaseParams{
		ID:           arg.ID,
		CurrentToken: pgtype.Text{String: arg.Token, Valid: true},
	})
}

// rowFingerprint condenses the credential-bearing config of a
// channel_installation row into an opaque string. Any change to the platform
// config (Feishu rotates app_id / app_secret / region on re-install) flips the
// fingerprint and the Supervisor restarts the connection. The config JSONB
// carries only the secret ciphertext (never plaintext), so hashing it is safe
// and channel-agnostic — no platform field is read directly.
func rowFingerprint(row db.ChannelInstallation) string {
	h := sha256.New()
	_, _ = h.Write([]byte(row.ChannelType))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(row.Config)
	return hex.EncodeToString(h.Sum(nil))
}
