// Package channel is the platform-agnostic foundation for Multica's
// inbound IM integrations (Feishu/Lark, Slack, WeCom, …). It owns the
// contract every integration implements so the core never learns what a
// given platform's event JSON looks like — design tracked in MUL-3506,
// phase-1 foundation in MUL-3515.
//
// The contract has four pieces:
//
//  1. Channel — the per-integration interface
//     (Type / Connect / Disconnect / Send / Capabilities). An adapter
//     translates platform payloads in both directions and owns its own
//     connection mode (outbound WebSocket long-conn, inbound HTTP, …);
//     the core only calls these five methods.
//
//  2. InboundMessage / OutboundMessage — the normalized message
//     envelopes. Every platform's inbound payload is translated by its
//     adapter into one InboundMessage; the core routes, dedups, and
//     persists only this struct. Outbound is the minimal text reply
//     (ChatID + Text + optional thread / reply target) — rich cards,
//     media, and outbound webhooks are deliberately out of scope here
//     and stay inside the adapter that supports them.
//
//  3. Capability — a bitmask each Channel uses to DECLARE what it can
//     do (rich cards, threads, attachments, …). This package only
//     models the declaration; it intentionally contains no degrade
//     logic. Callers that want to degrade (rich card → plain text)
//     read the bitmask and decide for themselves, so adding a platform
//     never forces an if/else into the core.
//
//  4. Registry — a Type→Factory map with last-writer-wins semantics.
//     Adding a platform is "register a factory", not "edit the core".
//
// Boundary rule (MUL-3515 decision §2): the envelope holds ONLY fields
// that are true across every platform — Text, a normalized message-type
// enum, media references, the reply/thread anchors, the routing Source,
// and the event/message ids used for dedup. Anything platform-specific
// (a Lark raw msg_type, parent_id, root_id, …) lives in Raw and is read
// ONLY by the adapter that produced it. The core never reads Raw.
//
// This package is pure: it has no database, network, or platform
// dependencies, and nothing in it imports another integration package.
// The concrete Feishu/Lark adapter, the DB-backed installation/identity/
// session resolvers, and the supervisor that drives Connect/Disconnect
// are wired in the follow-up cutover (see MUL-3515).
package channel
