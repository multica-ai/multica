package autopilotmodel

import "fmt"

// Provider-independent "cheap model" tier (FIR-4492).
//
// The problem this solves: a model ID only means something to the provider that
// runs it, but the agent scheduling a wakeup has to name one, and nothing in its
// prompt tells it what its own runtime accepts. Agents therefore write the ID
// they know — `claude-haiku-4-5-20251001` — onto whatever runtime they happen to
// sit on. On a Codex runtime that ID killed 28 consecutive woken runs (100%
// failed, provider `codex`, week of 2026-07-13) before the create-time check in
// FIR-3287 started rejecting it, and rejecting it is only half a fix: the agent
// still cannot name a model that works, so the cheap run it wanted never happens.
//
// TierCheap is the answer: the caller states the INTENT ("run this on the cheap
// model") and the provider that will actually run it decides the ID. There is
// nothing to tune and nothing for an agent to guess.

// TierCheap is the value a caller passes instead of a model ID to mean "whatever
// the cheap model is on the runtime that ends up running this".
const TierCheap = "cheap"

// cheapModels maps a catalogued runtime provider to its cheapest model. Curated
// on purpose rather than derived from the price registry: "cheapest" from prices
// alone drifts to whichever model happens to be listed lowest, which can be a
// model too weak to finish the check the wakeup exists to do. Every value is
// asserted to be in that provider's agent.StaticCatalog by
// TestCheapModelsAreInProviderCatalog.
//
// A provider absent here (every provider with no authoritative catalog, plus any
// catalogued one we have not curated) resolves TierCheap to no override at all —
// the run then uses the agent's own model, which always works. Guessing an ID for
// a provider we cannot vouch for is the failure mode this whole file exists to
// remove.
var cheapModels = map[string]string{
	"claude":       ModelHaiku,
	"codex":        "gpt-5.4-mini",
	"openai-eu":    "gpt-4o-mini",
	"firtal-local": "gemma4:12b-it-qat",
}

// CheapForProvider returns the cheap model for provider. ok=false means we have
// no curated cheap model for it, which callers must treat as "no override".
func CheapForProvider(provider string) (model string, ok bool) {
	model, ok = cheapModels[provider]
	return model, ok
}

// ResolveForProvider turns whatever a caller supplied into a model ID the given
// runtime provider can actually run. It replaces a bare ValidateForProvider call
// wherever the caller would otherwise have to drop or reject the value:
//
//   - "" stays "" — no override, use the agent's own model.
//   - TierCheap becomes this provider's cheap model, or "" when we have none.
//   - An ID this provider supports passes through unchanged. A provider with no
//     authoritative catalog supports everything, same permissive rule as
//     SupportedByProvider — we cannot prove its IDs wrong.
//   - Another provider's CHEAP model becomes this provider's cheap model. This is
//     the haiku-on-a-Codex-runtime case: the caller wanted a cheap run and used
//     the only cheap ID it knew, so honour the intent instead of failing the run.
//   - Anything else is an error. A caller that named a specific non-cheap model
//     for the wrong provider asked for a capability this runtime does not have,
//     and silently swapping it would hide that.
func ResolveForProvider(provider, model string) (string, error) {
	if model == "" {
		return "", nil
	}
	cheap, hasCheap := CheapForProvider(provider)
	if model == TierCheap {
		if !hasCheap {
			return "", nil
		}
		return cheap, nil
	}
	if SupportedByProvider(provider, model) {
		return model, nil
	}
	if hasCheap && isCheapModel(model) {
		return cheap, nil
	}
	allowed, _ := AllowedForProvider(provider)
	return "", fmt.Errorf("%w: %q is not a %s model (allowed: %v, or %q for the cheap model)",
		ErrModelNotOnProvider, model, provider, allowed, TierCheap)
}

// isCheapModel reports whether model is some provider's cheap model.
func isCheapModel(model string) bool {
	for _, m := range cheapModels {
		if m == model {
			return true
		}
	}
	return false
}
