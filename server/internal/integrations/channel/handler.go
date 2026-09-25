package channel

import "context"

// InboundHandler is the shared, channel-agnostic entry point a Channel
// invokes for every inbound message it receives. The engine supervisor
// injects ONE handler into every Channel it builds (via Config.Handler),
// mirroring the reference design's single set_message_handler wiring: the
// engine's inbound processing is written once and every platform adapter
// funnels into it. The adapter owns its receive loop and calls the handler;
// the core never polls the Channel.
//
// Contract:
//
//   - A non-nil error signals an INFRASTRUCTURE failure (the core could
//     not process the message at all — DB down, dispatcher misconfigured,
//     etc.). The adapter should treat it like a failed delivery: surface
//     it to ops and let the supervisor's reconnect/backoff handle it. It
//     MUST NOT be used for product outcomes.
//   - A nil error means the message was accepted and classified. The
//     message may still have been dropped for a legitimate product reason
//     (dedup hit, unbound sender, group filter) — that is NOT an error.
//     Any outbound reply the verdict implies (binding card, offline
//     notice, typing indicator) is the handler's own responsibility,
//     detached from the adapter's ACK path.
//
// The handler is deliberately fire-and-classify (no return value beyond
// error): the adapter does not branch on the outcome, so coupling it to a
// platform-specific result type would defeat the abstraction.
type InboundHandler func(ctx context.Context, msg InboundMessage) error
