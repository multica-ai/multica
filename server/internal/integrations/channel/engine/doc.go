// Package engine is the channel-agnostic runtime that DRIVES the
// channel.Channel adapters defined in the parent channel package. Stage-1
// (MUL-3515) shipped the abstraction (Channel / InboundMessage /
// OutboundMessage / Registry) but no engine consumed it; this package is
// that engine — the "通用引擎" of MUL-3620, generalized out of the
// Feishu-specific lark.Hub / lark.Dispatcher.
//
// It provides:
//
//  1. Supervisor — the per-installation connection supervisor generalized
//     from lark.Hub. It enumerates active installations across ALL
//     channel types (no hard-coded platform), fences each behind the WS
//     lease CAS so at most one replica connects per installation, builds
//     the platform Channel via the channel.Registry, drives its
//     Connect/Disconnect lifecycle with exponential backoff + jitter, and
//     restarts a connection whose credentials rotated.
//
//  2. Router — the inbound pipeline generalized from lark.Dispatcher. It is
//     the single shared channel.InboundHandler the Supervisor injects into
//     every Channel: it routes by ChannelType to that platform's registered
//     ResolverSet and runs the same ordered pipeline for every platform
//     (installation route → two-phase dedup → group @bot filter → identity +
//     membership → ensure session → append+mark → /issue → debounced run),
//     then drives the detached OutboundReplier + typing indicator.
//
// Everything platform-specific lives behind the resolver interfaces in
// resolvers.go; adding a platform is "register a Factory + ResolverSet",
// never "edit the engine". The engine depends only on the channel package,
// the platform-agnostic service layer, and small interfaces; the DB-backed
// store/resolvers and the concrete platform adapters are wired at boot.
package engine
