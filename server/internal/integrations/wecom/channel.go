package wecom

import (
	"log/slog"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TypeWecom is the channel discriminator for the WeCom adapter. It is defined
// here (not in the channel core package) on purpose: registering a new platform
// must not require editing the core, so the Type value lives with its adapter.
const TypeWecom channel.Type = "wecom"

// DefaultDialURL is the WeCom long-connection WebSocket endpoint.
const DefaultDialURL = "wss://openws.work.weixin.qq.com"

// OutboxDeps wires the installation-scoped outbound queue consumer (spec §5.3).
type OutboxDeps struct {
	Queries *db.Queries
	Binding *BindingTokenService
	Rate    *RateGate
	AppURL  string
	Tx      engine.TxStarter
}

// ChannelDeps holds process-wide dependencies shared by every WeCom installation
// factory built through RegisterWecom.
type ChannelDeps struct {
	Decrypt func(sealed string) ([]byte, error)
	Logger  *slog.Logger
	Wake    *OutboundWakeRegistry
	Metrics WecomMetrics
	DialURL string // tests override; default DefaultDialURL
	Outbox  OutboxDeps

	// Retries shares one RetryState per installation across every
	// Factory rebuild (see RetryRegistry doc). Nil is tolerated — each
	// build then gets its own fresh RetryState, which is fine for tests
	// but loses backoff continuity across Supervisor reconnects in
	// production, so RegisterWecom wiring should always set it.
	Retries *RetryRegistry
}

// RegisterWecom registers the WeCom factory on reg under TypeWecom.
func RegisterWecom(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeWecom, NewFactory(deps))
}

// NewFactory builds a channel.Factory for WeCom installations: it decodes
// and validates the per-installation config, decrypts the bot secret, and
// returns a *wecomChannel ready for Connect. See newWecomFactory in
// wecom_channel.go for the implementation.
func NewFactory(deps ChannelDeps) channel.Factory {
	return newWecomFactory(deps)
}
