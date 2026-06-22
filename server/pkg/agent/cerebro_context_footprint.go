package agent

// FIR-1870 — generalised last-turn context-window footprint capture.
//
// This file is in the cerebro zone (the `cerebro_` filename prefix carves it out
// of the upstream-zone guard, see scripts/cerebro-zones.txt), so it carries no
// per-line CEREBRO-PATCH markers.
//
// Background. The context-window indicator must answer "how full was the model's
// context window on its LAST turn?" — i.e. the size of the prompt the model last
// read. The cumulative token columns on task_usage are a SUM across every turn of
// a run, and with prompt caching warm each turn re-reads almost the whole prompt
// from cache, so the cumulative cache_read alone is several times the window. Read
// as occupancy it pins the gauge at 100% (the screenshot Jesper flagged on
// FIR-1870 read 3350k / 1000k). FIR-1856 fixed this for Codex by recording the
// last-turn footprint in a side table; every other runtime was wrongly assumed to
// already report a per-turn footprint and was left on the cumulative formula. It
// does not — Claude (and the other streaming runtimes) accumulate per-turn usage
// the same way, so they over-count identically. This helper lets every runtime
// record the last turn's footprint with one observe() call at its per-turn usage
// site and one apply at the end.
//
// Accounting note (the part that silently breaks if you get it wrong): wholePrompt
// is the ENTIRE prompt the model read on that turn, INCLUDING the cached portion.
// Each runtime computes it in its own accounting and passes the result here:
//   - Anthropic-style runtimes report input_tokens EXCLUSIVE of cache (cache_read
//     and cache_creation are separate), so wholePrompt = input + cacheRead +
//     cacheWrite.
//   - OpenAI/Gemini-style runtimes report input/prompt tokens INCLUSIVE of the
//     cached subset, so wholePrompt = input alone (adding cacheRead would
//     double-count — the same double-count FIR-1856 removed for Codex).
// The helper stays neutral on this; the caller owns the runtime's semantics.

// lastTurnFootprint records the most recent turn's full prompt size for one model.
type lastTurnFootprint struct {
	model       string
	wholePrompt int64
	cacheRead   int64
	set         bool
}

// observe records a turn's footprint, overwriting any earlier turn so the final
// observed turn wins. wholePrompt must already include the cached portion (see the
// accounting note above). Zero/negative prompts are ignored so a trailing
// usage-less event cannot blank a real footprint.
func (l *lastTurnFootprint) observe(model string, wholePrompt, cacheRead int64) {
	if wholePrompt <= 0 {
		return
	}
	l.model = model
	l.wholePrompt = wholePrompt
	l.cacheRead = cacheRead
	l.set = true
}

// apply writes the captured footprint onto a single TokenUsage. No-op when nothing
// was observed, leaving the cumulative fallback in place.
func (l *lastTurnFootprint) apply(u *TokenUsage) {
	if !l.set {
		return
	}
	u.ContextInputTokens = l.wholePrompt
	u.ContextCacheReadTokens = l.cacheRead
}

// applyToMap writes the captured footprint onto the matching model entry of a
// per-model usage map. No-op when nothing was observed or the model is unknown.
func (l *lastTurnFootprint) applyToMap(usage map[string]TokenUsage) {
	if !l.set || l.model == "" {
		return
	}
	u := usage[l.model]
	l.apply(&u)
	usage[l.model] = u
}
