package featureflag

import (
	"context"
	"slices"
	"sync"
)

// Rule describes how a single flag is evaluated by the StaticProvider.
// All fields are optional; an empty Rule evaluates to Default (false) for
// everyone.
//
// Evaluation order (first match wins):
//
//  1. Deny: if any value in the EvalContext matches an entry in Deny on
//     attribute DenyBy (default "user_id"), the flag is OFF.
//  2. Allow: if any value matches an entry in Allow on attribute AllowBy
//     (default "user_id"), the flag is ON.
//  3. Percent: if Percent is non-nil and the bucket for (key, identifier)
//     falls inside Percent.Percent, the flag is ON.
//  4. Default: returned otherwise.
//
// Allow / Deny lists are intentionally separate (rather than a single
// targeting predicate) because operationally they cover different use
// cases — Allow is "internal users only" and Deny is "kill switch for
// these tenants" — and keeping them separate makes the data easy to audit
// in source control.
type Rule struct {
	// Default is the value returned when no targeting rule matches.
	Default bool

	// Variant is the variant identifier returned WHEN the rule evaluates
	// to enabled=true. For multi-arm experiments, set Variant to the
	// experiment-arm identifier (e.g. "experiment-v2"); for plain on/off
	// flags leave it empty.
	//
	// When the rule evaluates to enabled=false (default-off, deny hit,
	// percent miss, ...) the resulting Decision's Variant is always the
	// canonical "off". This is deliberate: a caller that branches on
	// Variant("checkout_algo", "control") would otherwise be routed into
	// the experiment arm even though the user did not roll into the
	// experiment cohort.
	Variant string

	// Allow is the set of identifier values that force the flag ON.
	Allow []string

	// AllowBy is the EvalContext attribute name used for Allow lookups.
	// Defaults to "user_id" when empty.
	AllowBy string

	// Deny is the set of identifier values that force the flag OFF.
	// Deny wins over Allow.
	Deny []string

	// DenyBy is the EvalContext attribute name used for Deny lookups.
	// Defaults to "user_id" when empty.
	DenyBy string

	// Percent enables a deterministic percent rollout. When nil, no
	// percent rollout is applied and Default is used as the fallback.
	Percent *PercentRollout
}

// PercentRollout describes a deterministic percent rollout.
//
// The bucket is computed from (flag key, EvalContext attribute By) using
// FNV-1a, which guarantees that the same identifier always falls into the
// same bucket across processes and across restarts. This is what callers
// need so users do not flip in and out of an experiment between requests.
type PercentRollout struct {
	// Percent is the rollout size in [0, 100]. 0 disables the rollout;
	// 100 enables it for everyone. Out-of-range values are clamped.
	Percent int

	// By selects the EvalContext attribute used as the bucketing
	// identifier. Defaults to "user_id". Use "workspace_id" for
	// workspace-scoped rollouts.
	By string
}

// StaticProvider is a thread-safe in-memory Provider populated either
// programmatically or from a config file. It is the recommended baseline
// provider for production: configuration lives in source control, moves
// through CD alongside the binary, and changes require a deploy — which is
// exactly the Continuous Delivery posture Martin Fowler recommends for
// Release Toggles and most Permissioning Toggles.
//
// For dynamic flags (kill switches, A/B tests changed by product) compose
// a StaticProvider with a DB-backed Provider behind a ChainProvider.
type StaticProvider struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

// NewStaticProvider returns an empty StaticProvider. Use Set or
// LoadRules to populate it.
func NewStaticProvider() *StaticProvider {
	return &StaticProvider{rules: map[string]Rule{}}
}

// Name implements Provider.
func (*StaticProvider) Name() string { return "static" }

// Set installs or replaces the rule for key. Concurrent callers are
// serialized; readers (Lookup) never block writers for long.
func (p *StaticProvider) Set(key string, rule Rule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules[key] = rule
}

// LoadRules atomically replaces every rule in the provider with the supplied
// map. Use this when reloading from a config file: a partial reload could
// otherwise leave the provider in a mixed state where some flags reflect the
// new config and others the old.
func (p *StaticProvider) LoadRules(rules map[string]Rule) {
	clone := make(map[string]Rule, len(rules))
	for k, v := range rules {
		clone[k] = v
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules = clone
}

// Keys returns the sorted set of flag keys this provider knows about. Useful
// for diagnostic endpoints. The returned slice is a copy; mutating it does
// not affect the provider.
func (p *StaticProvider) Keys() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, 0, len(p.rules))
	for k := range p.rules {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Lookup implements Provider.
func (p *StaticProvider) Lookup(ctx context.Context, key string) (Decision, bool) {
	p.mu.RLock()
	rule, ok := p.rules[key]
	p.mu.RUnlock()
	if !ok {
		return Decision{}, false
	}
	ec := EvalContextFrom(ctx)
	return evaluateRule(key, rule, ec), true
}

func evaluateRule(key string, rule Rule, ec EvalContext) Decision {
	// Deny wins over everything else. A kill switch must be reachable
	// even when other targeting matches.
	denyBy := orDefault(rule.DenyBy, "user_id")
	if len(rule.Deny) > 0 {
		if v, ok := ec.Lookup(denyBy); ok && slices.Contains(rule.Deny, v) {
			return decisionFromRule(key, rule, false, ReasonStatic)
		}
	}

	allowBy := orDefault(rule.AllowBy, "user_id")
	if len(rule.Allow) > 0 {
		if v, ok := ec.Lookup(allowBy); ok && slices.Contains(rule.Allow, v) {
			return decisionFromRule(key, rule, true, ReasonStatic)
		}
	}

	if rule.Percent != nil {
		by := orDefault(rule.Percent.By, "user_id")
		identifier, _ := ec.Lookup(by)
		// An empty identifier still produces a deterministic bucket
		// (the empty string hashes to a stable bucket) but in practice
		// that means everyone-without-an-id lands in the same bucket.
		// That's the desired behavior for percent rollouts at the edge:
		// anonymous users get a single shared rollout decision per
		// flag, not a uniformly random one.
		if inPercent(key, identifier, rule.Percent.Percent) {
			return decisionFromRule(key, rule, true, ReasonPercent)
		}
		return decisionFromRule(key, rule, false, ReasonPercent)
	}

	return decisionFromRule(key, rule, rule.Default, ReasonStatic)
}

func decisionFromRule(key string, rule Rule, enabled bool, reason Reason) Decision {
	// Variant policy: rule.Variant is the ON-variant. When the rule
	// evaluates to false we return the canonical "off" so a caller
	// branching on Variant() cannot accidentally enter the experiment
	// arm for a user that did not roll in.
	variant := boolToVariant(enabled)
	if enabled && rule.Variant != "" {
		variant = rule.Variant
	}
	return Decision{
		Key:     key,
		Enabled: enabled,
		Variant: variant,
		Reason:  reason,
		Source:  "static",
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
