// Package providerfailover holds the explicit, auditable, fail-closed policy
// that hands a task off from one AI provider runtime to another when — and only
// when — the source run terminated on a usage/rate-limit condition OR went
// silently unresponsive past a wall-clock liveness deadline.
//
// This package is deliberately pure: no database, no network, no clock. It is
// the single source of truth for "should this failure become a cross-provider
// handoff, and in which direction?", so the decision can be unit-tested
// exhaustively and reproduced from a persisted ledger row. All side effects
// (persistence, dispatch, comments) live in server/internal/service and consume
// Decide's output.
//
// Direction (td-836aa9 hardening): failover began as single-directional
// (GPT/codex → Claude, a closed source whitelist). It is now a BIDIRECTIONAL,
// role/capacity policy: any provider in failoverProviders may be a source when
// its run hits a limit or hangs, and hands off to a DIFFERENT eligible provider
// that has capacity. So Claude → GPT works too, which matters when the Claude
// Max window ends and GPT is the one with headroom. Loop/ping-pong is prevented
// structurally by the at-most-one-per-chain guard and the already-a-fallback
// guard (see policy.go), not by direction.
package providerfailover

import "github.com/multica-ai/multica/server/pkg/taskfailure"

// TargetProvider is the default/legacy target provider token. Retained for
// backward compatibility (the ledger's target_provider column default and
// call sites that predate the bidirectional policy). New code should resolve
// the target per-direction with PrimaryTargetFor / FailoverTargets rather than
// assume Claude.
//
// "claude" is the exact runtime provider token (agent_runtime.provider /
// runtime_profile.protocol_family) for Anthropic's Claude CLI — not the LLM
// billing provider "anthropic". See pkg/agent/agent.go SupportedTypes.
const TargetProvider = "claude"

// failoverProviders is the exact, closed set of runtime providers that
// participate in capacity-based failover, in EITHER direction. Membership means
// a provider may act as a failover SOURCE (its run hit a usage/rate-limit or
// went silently unresponsive) AND/OR as a failover TARGET (it has capacity to
// take over). It is deliberately a positive whitelist, not "any provider": the
// repo has ~17 runtime providers (grok, kimi, cursor, gemini, …) and only the
// two first-class, mutually-substitutable coding runtimes are in scope.
//
// The OpenAI/GPT runtime provider is exactly "codex" (the Codex CLI) and the
// Anthropic one is exactly "claude" (the Claude CLI); "openai"/"gpt"/"anthropic"
// are LLM *billing* tokens, never runtime providers. Widening this set is a
// deliberate, audited change (add the token here + a failoverTargets entry, and
// extend the policy tests).
var failoverProviders = map[string]bool{
	"codex":  true,
	"claude": true,
}

// failoverTargets maps a source provider to the ordered set of target providers
// a failover may hand off to, most-preferred first. Invariants enforced by
// eligibleTargets and asserted in tests:
//   - every target is in failoverProviders,
//   - a provider never targets itself (you never fail a runtime over to its own
//     family — that would re-hit the same capacity wall),
//   - the relation is bidirectional for the two in-scope providers.
var failoverTargets = map[string][]string{
	"codex":  {"claude"},
	"claude": {"codex"},
}

// IsFailoverSource reports whether a source run's runtime provider is one the
// failover policy may act on — i.e. it participates in failover and has at least
// one eligible target. Fail-closed: an empty or unknown provider (every non-GPT,
// non-Claude CLI) returns false.
func IsFailoverSource(provider string) bool {
	return failoverProviders[provider] && len(eligibleTargets(provider)) > 0
}

// FailoverTargets returns the ordered eligible target providers for a source,
// most-preferred first. Empty when the source is not a failover participant or
// has no distinct eligible target. The returned slice is a fresh copy; callers
// may mutate it freely.
func FailoverTargets(source string) []string {
	targets := eligibleTargets(source)
	out := make([]string, len(targets))
	copy(out, targets)
	return out
}

// PrimaryTargetFor returns the single most-preferred target provider for a
// source, or "" when the source has no eligible target. Used to record the
// intended handoff direction on a decision (including shadow rows, so operators
// can see which way a handoff would have gone) and as the default target the
// service resolves a concrete agent for.
func PrimaryTargetFor(source string) string {
	if t := eligibleTargets(source); len(t) > 0 {
		return t[0]
	}
	return ""
}

// eligibleTargets returns the configured targets for a source, filtered to
// providers that are themselves failover participants and are a DIFFERENT family
// from the source. Central chokepoint so IsFailoverSource / FailoverTargets /
// PrimaryTargetFor all agree, and a malformed failoverTargets entry (self-target
// or non-participant target) can never widen the policy.
func eligibleTargets(source string) []string {
	if source == "" || !failoverProviders[source] {
		return nil
	}
	var out []string
	for _, t := range failoverTargets[source] {
		if t == source || !failoverProviders[t] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// IsFailoverTrigger reports whether a failure reason is one a cross-provider
// failover is allowed to act on.
//
// Three reasons qualify:
//
//   - ReasonAgentProviderCapacityOrRateLimit — HTTP 429/529, "rate limit",
//     "overloaded", "no capacity available".
//   - ReasonAgentProviderQuotaLimit — HTTP 402, "usage limit", "quota",
//     "credits", "insufficient balance".
//   - ReasonProviderLivenessTimeout — a SILENT HANG: the run went unresponsive
//     past its provider-specific wall-clock liveness deadline (GPT ~60min,
//     Claude ~180s) while the owning runtime stopped proving liveness. This is
//     the td-836aa9 watchdog trigger — before it, failover fired ONLY on
//     rate-limit/usage errors, so a wedged run that never returned an error
//     silently stalled the whole task instead of handing off.
//
// Everything else is structurally excluded, and the exclusions matter:
//
//   - Auth (ReasonAgentProviderAuthOrAccess, 401/403): a re-auth problem, not a
//     capacity problem. Handing off would mask a misconfiguration and burn a
//     second provider's quota on a request that will keep failing.
//   - Plain timeout (ReasonAgentTimeout / ReasonTimeout): the model was actively
//     WORKING and hit a hard cap; re-running elsewhere risks duplicating what it
//     had started. Contrast ReasonProviderLivenessTimeout above, which is a
//     wedged/silent process, not a busy one.
//   - Network / server 5xx: transient and resume-safe on the SAME provider; the
//     platform's own retry path already covers these.
//   - Context overflow, process failure, config, model-not-found, unknown: none
//     is cured by swapping providers.
//
// Fail-closed: an unrecognized/empty reason returns false.
func IsFailoverTrigger(reason taskfailure.Reason) bool {
	switch reason {
	case taskfailure.ReasonAgentProviderCapacityOrRateLimit,
		taskfailure.ReasonAgentProviderQuotaLimit,
		taskfailure.ReasonProviderLivenessTimeout:
		return true
	default:
		return false
	}
}
