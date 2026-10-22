// Package channel defines platform-independent IM interfaces and envelopes.
// Adapters own transport and platform payloads; the engine consumes normalized
// messages and capabilities. Rich cards and platform-specific delivery remain
// inside their adapters. Callers choose capability degradation policies.
//
// Raw carries adapter-specific fields and must only be read by that adapter.
// Registry maps channel types to factories with last-writer-wins semantics.
// This foundation has no database, network, or integration dependencies.
package channel
