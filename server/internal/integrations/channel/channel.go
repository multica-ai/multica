package channel

import (
	"context"
	"encoding/json"
)

// Type identifies an inbound channel platform — the discriminator the
// Registry keys on and the value persisted in the channel_type column of
// the generalized channel_* tables. Use the lower-case platform slug
// ("feishu", "slack", "wecom", …); keep it stable, it is durable data.
type Type string

const (
	// TypeFeishu is the Feishu / Lark adapter — the only implementation
	// in phase 1. It serves both the mainland Feishu cloud and the Lark
	// international cloud; the cloud (region) is per-installation config,
	// not a separate Type.
	TypeFeishu Type = "feishu"
)

// Channel is the platform-agnostic contract every IM integration
// implements. An adapter keeps ALL platform specifics behind these five
// methods: the core supervisor calls Connect/Disconnect to manage the
// link, Send to deliver an outbound reply, and reads Capabilities to
// decide how to render; it never touches platform SDKs or wire formats.
//
// Inbound is intentionally NOT on this interface. A Channel pushes
// normalized InboundMessage values into the core router via the wiring
// established at construction (the adapter owns its receive loop); the
// core does not poll the Channel for messages.
type Channel interface {
	// Type reports the platform discriminator. It MUST equal the Type
	// the Channel was registered under, and is stable for the lifetime
	// of the instance.
	Type() Type

	// Connect establishes the platform link (e.g. dials the outbound
	// WebSocket long-conn, or starts the inbound HTTP listener) and then
	// BLOCKS, running the receive loop, until the link ends. The
	// connection mode is the implementation's choice and invisible to the
	// core. It returns:
	//
	//   - nil when ctx is cancelled (graceful shutdown / lease loss);
	//   - a non-nil error when the link drops and cannot be recovered
	//     locally — the supervisor treats this as "this attempt failed"
	//     and reconnects under exponential backoff.
	//
	// While Connect runs, the adapter delivers each inbound message by
	// invoking the InboundHandler it captured at construction
	// (Config.Handler). Send may be called concurrently from another
	// goroutine for the lifetime of the connection. Implementations MUST
	// tolerate repeated Connect calls on different contexts: the
	// supervisor may Connect, return, and Connect again after backoff.
	Connect(ctx context.Context) error

	// Disconnect tears the platform link down and releases its
	// resources. It is safe to call after a failed Connect and safe to
	// call more than once; a Channel that is already disconnected
	// returns nil.
	Disconnect(ctx context.Context) error

	// Send delivers a single outbound message and returns the platform's
	// identifier for the delivered message. A non-nil error is reserved
	// for real delivery failures (network, auth, rate limit) that the
	// caller may retry.
	Send(ctx context.Context, out OutboundMessage) (SendResult, error)

	// Capabilities declares what this Channel supports. It is a pure
	// declaration with no side effects and a stable result; callers read
	// it to choose a rendering and degrade on their own (this package
	// performs no degradation — see the Capability docs).
	Capabilities() Capability
}

// Config is the normalized per-installation configuration a Factory
// consumes. Type is the platform discriminator; Raw is the platform's
// own credential/config blob (Feishu's app_id / encrypted app_secret /
// tenant_key / region, Slack's bot/app tokens, …), carried opaquely so
// the foundation never grows a per-platform field. It maps directly onto
// the channel_type column + JSONB config of a channel_installation row
// (MUL-3515 decision §3).
type Config struct {
	Type Type
	Raw  json.RawMessage

	// Handler is the shared inbound entry point the engine injects so the
	// built Channel can deliver normalized InboundMessage values into the
	// core (see InboundHandler). A Factory captures it and invokes it from
	// the Channel's receive loop. It may be nil when a Channel is built
	// purely for its outbound Send path (no inbound delivery needed).
	Handler InboundHandler
}

// Factory builds a Channel from its per-installation Config. Each adapter
// registers exactly one Factory under its Type; the Registry calls it to
// instantiate a per-installation Channel. A Factory should validate Raw
// and return an error rather than a half-built Channel.
type Factory func(cfg Config) (Channel, error)
