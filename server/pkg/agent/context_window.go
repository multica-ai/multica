package agent

// Context-window reporting, shared by every backend that can see it.
//
// What makes an occupancy figure honest is that it is ONE request's prompt:
// the whole thing, every cache class included, because all of it occupies the
// window. A run-total — the figure most backends accumulate for cost — is a
// different quantity and must never be reported here: summing the prompts of
// ten turns that re-sent the same conversation produces a number ten times
// larger than anything that was ever in the window.
//
// A backend that cannot see a single request's prompt therefore reports
// nothing, and the surfaces render nothing. That is the designed outcome: an
// absent chip means "this runtime does not say", which a reader can act on,
// while a fabricated one cannot be told apart from a measurement.
//
// Reporting today, and what each one can see:
//
//	claude     occupancy, window (from the model's `[1m]`/default), compaction
//	codex      occupancy (tokenUsage.last), window when the app-server sends one
//	zcode      compaction only — its usage path exposes no per-request prompt
//	codebuddy  occupancy (per-response usage, Claude Code stream shape)
//	cursor     occupancy (step_finish)
//	opencode   occupancy (step_finish)
//	codearts   occupancy (step_finish)
//	deveco     occupancy (step_finish)
//	openclaw   occupancy (step_finish)
//	pi         occupancy (turn_end)
//
// Silent, and why — each needs its own protocol check before it can report,
// not a generic fallback:
//
//	the ACP family (dim, devin, grok, hermes, kiro, mcode, qoder, qwenpaw,
//	traecli, zeroclaw) meters through acp_usage.go, whose snapshots are
//	cumulative turn totals; occupancy is not derivable from them.
//
//	opencode_acp and copilot read usage from a session export / event log
//	after the fact, so there is no live request to attribute a figure to.
//
//	amp, antigravity, droid, dsh, kimi, muse, qwen and reasonix report
//	usage in shapes whose per-request meaning has not been verified. Adding one
//	is a few lines at its own accumulation point — the plumbing below, the
//	daemon's throttle and both surfaces are provider-agnostic.

// contextMessage builds the occupancy observation for one request.
//
// limitTokens is the window the figure is measured against, or 0 when the
// runtime did not report one and none could be derived — never a default.
// Returns false when there is nothing to report, so callers can emit
// unconditionally without guarding each call site.
func contextMessage(model string, promptTokens, limitTokens int64) (Message, bool) {
	if promptTokens <= 0 {
		return Message{}, false
	}
	return Message{Type: MessageContext, Context: &ContextInfo{
		UsedTokens:  promptTokens,
		LimitTokens: limitTokens,
		Model:       model,
	}}, true
}

// sendContextUsage reports one request's prompt size on ch, if there is one.
func sendContextUsage(ch chan<- Message, model string, promptTokens, limitTokens int64) {
	if msg, ok := contextMessage(model, promptTokens, limitTokens); ok {
		trySend(ch, msg)
	}
}
