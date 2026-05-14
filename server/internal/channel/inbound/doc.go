// Package inbound implements the channel-layer inbound event pipeline.
//
// The pipeline is the chokepoint every InboundEvent flows through after
// an adapter (e.g. Feishu, T5) emits it on its Events() channel. It is
// organised as an ordered list of Steps:
//
//	normalize -> dedup -> identity-bind -> slash_expand -> dispatch
//
// Each Step decides whether to Continue (advance to the next Step) or
// Skip (terminate the pipeline cleanly). Errors abort the pipeline and
// propagate to the caller.
//
// Intent resolution is handled by the inbound runtime between the pre and
// post pipelines.
package inbound
