package agent

// Context-window reporting, shared by every backend that can see it.
//
// An observation is one request's prompt — the whole thing, every cache class
// included, because all of it occupies the window — measured against the
// window the runtime itself reported. Both halves are required: a figure with
// no window is an absolute token count a reader cannot act on ("is 917.9K a
// lot?"), and a window we derived rather than read would be presented as a
// measurement. So a backend that cannot see both reports nothing, and the
// surfaces render nothing.
//
// A run-total — the figure most backends accumulate for cost — is a different
// quantity and must never be reported here: summing the prompts of ten turns
// that re-sent the same conversation produces a number ten times larger than
// anything that was ever in the window.
//
// Reporting today:
//
//	codex   `tokenUsage.last` for the prompt, `modelContextWindow` for the
//	        window (verified against codex-cli 0.171.0, which answers
//	        `"modelContextWindow":475000` beside the usage).
//	claude  per-response `usage` for the prompt; the window from the CLI's own
//	        `get_usage` answer, else the configured model's `[1m]`-style
//	        modifier, else the family default for the model its init names.
//
// Silent, and why. These runtimes meter their prompts but report no window,
// so every figure they produce is windowless — the reason they show nothing
// is the missing window, not missing usage:
//
//	codebuddy  per-response usage (Claude Code stream shape)
//	cursor     step_finish
//	opencode   step_finish
//	codearts   step_finish
//	deveco     step_finish
//	openclaw   step_finish
//	pi         turn_end
//
// Silent for want of a usable prompt figure at all:
//
//	the ACP family (dim, devin, grok, hermes, kiro, mcode, qoder, qoderclicn,
//	qwenpaw, traecli, zeroclaw) meters through acp_usage.go, whose snapshots
//	are cumulative turn totals; occupancy is not derivable from them. zcode
//	is silent for the same reason.
//
//	opencode_acp and copilot read usage from a session export / event log
//	after the fact, so there is no live request to attribute a figure to.
//
//	museapp carries no token counters at all: its wire protocol is text,
//	thinking and tool events plus a final result, and nothing else.
//
//	amp, antigravity, droid, dsh, kimi, muse, qwen and reasonix report usage
//	in shapes whose per-request meaning has not been verified.
//
// Adding a runtime is a sendContextUsage call at its own accumulation point,
// once its protocol is known to carry both halves — the daemon's throttle, the
// stored task figure and both surfaces are provider-agnostic.

// contextMessage builds the occupancy observation for one request.
//
// Returns false when there is nothing reportable, so callers can emit
// unconditionally without guarding each call site. That includes a prompt
// with no window to measure it against: see the file header.
func contextMessage(model string, promptTokens, limitTokens int64) (Message, bool) {
	if promptTokens <= 0 || limitTokens <= 0 {
		return Message{}, false
	}
	// A prompt cannot exceed the window that accepted it, so a window this
	// measurement is larger than is not this run's window — most likely a
	// family default standing in for a model whose request carried a context
	// modifier the CLI did not report back. With nothing true left to measure
	// against, the observation is dropped rather than pinned at 100%, which
	// would read as "out of room" when the run may be nowhere near it.
	if promptTokens > limitTokens {
		return Message{}, false
	}
	return Message{Type: MessageContext, Context: &ContextInfo{
		UsedTokens:  promptTokens,
		LimitTokens: limitTokens,
		Model:       model,
	}}, true
}

// sendContextUsage reports one request's occupancy on ch, if it is reportable.
func sendContextUsage(ch chan<- Message, model string, promptTokens, limitTokens int64) {
	if msg, ok := contextMessage(model, promptTokens, limitTokens); ok {
		trySend(ch, msg)
	}
}
