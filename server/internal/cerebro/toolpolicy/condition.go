// Condition is the optional WHEN layer on a Rule (FIR-1609). It refines whether
// a rule applies to a request; it NEVER decides Allow/Ask/Deny — that stays the
// Effect's job. Borrowing only the idea of a condition language (not Cedar): the
// common cases are plain structured fields, and a CEL expression is the escape
// hatch for genuine dynamics, evaluated through an injected evaluator so this
// pure core carries no expression-engine dependency and stays unit-testable.
package toolpolicy

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// RequestContext carries the request attributes a Condition is matched against
// at the gate. New fields are added here as new condition kinds need them.
type RequestContext struct {
	// Host is the target host of the request, for host-allowlist conditions
	// (e.g. folding the web_fetch host policy into a rule as a Condition).
	Host string
	// Action is the verb being attempted (read/checkout/push, reveal, rotate …),
	// for action-scoped conditions on repos and credentials.
	Action string
}

// ExprEvaluator evaluates a (precompiled) CEL expression against the request
// context, returning whether it holds. It is injected so the pure core carries
// no CEL dependency. A nil evaluator paired with a non-empty Expr is reported as
// an error, never silently treated as "matches".
type ExprEvaluator func(expr string, ctx RequestContext) (bool, error)

// Condition is the optional WHEN layer on a Rule. The zero value constrains
// nothing and always matches.
type Condition struct {
	// HostAllowlist, when non-empty, requires the request Host to match one of
	// these patterns. A pattern is an exact host or a "*.suffix" wildcard.
	HostAllowlist []string `json:"host_allowlist,omitempty"`
	// Actions, when non-empty, restricts the rule to these action verbs.
	Actions []string `json:"actions,omitempty"`
	// Expr, when non-empty, is a CEL expression evaluated against the request
	// context via the injected ExprEvaluator. Compiled at write-time elsewhere;
	// carried opaquely here.
	Expr string `json:"expr,omitempty"`
}

// IsZero reports whether the condition imposes no constraint.
func (c Condition) IsZero() bool {
	return len(c.HostAllowlist) == 0 && len(c.Actions) == 0 && c.Expr == ""
}

// Matches reports whether the request satisfies the condition's terms. It is a
// pure factual predicate and takes NO safety stance: when it cannot decide (a
// non-empty Expr with no evaluator, or an evaluator error) it returns a non-nil
// error and the caller chooses the safety stance from the rule's Effect.
// What an unmet/undecidable condition MEANS per Effect (a whitelist Allow closes
// to Deny, a Deny/Ask restriction drops) lives in ConditionedSetting, not here.
func (c Condition) Matches(ctx RequestContext, eval ExprEvaluator) (bool, error) {
	if len(c.HostAllowlist) > 0 && !hostMatches(c.HostAllowlist, ctx.Host) {
		return false, nil
	}
	if len(c.Actions) > 0 && !containsFold(c.Actions, ctx.Action) {
		return false, nil
	}
	if c.Expr != "" {
		if eval == nil {
			return false, fmt.Errorf("toolpolicy: condition has expression but no evaluator is wired")
		}
		return eval(c.Expr, ctx)
	}
	return true, nil
}

// ConditionedSetting reports the Setting a single stored row contributes to the
// chain at its layer, and whether it contributes at all. It is the seam that
// gives the Terms axis (the WHEN layer) teeth at the gate. The Effect direction
// (grant vs. restriction) decides what an UNMET condition means — this is the
// only place the rule's Effect is consulted, per Matches' contract that the
// predicate itself takes no safety stance.
//
// An Allow row is a WHITELIST grant ("allow ONLY when the condition holds"); a
// Deny/Ask row is a RESTRICTION ("apply the restriction when the condition holds").
// That asymmetry is what makes a "conditional yes" real: on a Base=Allow resource
// (every gate runs Base=Allow), an unmet conditional Allow that merely dropped
// would be inert — the resource stays allowed by default — so `Allow WHEN
// action=="get_schema"` would still let `execute` through (FIR-1609, found in QA).
// An unmet conditional Allow therefore DENIES instead of dropping.
//
//   - No condition (nil): the setting applies unchanged — the common case, and
//     the only case for every row written before conditions existed, so wiring
//     this into the resolver is behaviour-preserving for unconditioned policy.
//   - Condition met: the setting applies unchanged.
//   - Condition not met (clean false):
//   - Allow (whitelist grant) → DENY applies — access only exists when the
//     condition holds, so a miss closes the gate rather than falling back to
//     the base default.
//   - Deny/Ask (restriction) → the row drops — its layer inherits, exactly as
//     if the row were absent (no restriction outside its condition).
//   - Condition undecidable (a CEL Expr with no evaluator, or an evaluator error):
//     fail closed by effect — an Allow DENIES (never grant on uncertainty), a
//     Deny/Ask is kept (stay restrictive on uncertainty).
func ConditionedSetting(setting Setting, cond *Condition, ctx RequestContext, eval ExprEvaluator) (Setting, bool) {
	if cond == nil {
		return setting, true
	}
	matched, err := cond.Matches(ctx, eval)
	if err != nil {
		// Undecidable: a conditional grant fails closed to Deny; a restriction is kept.
		if setting == SettingAllow {
			return SettingDeny, true
		}
		return setting, true
	}
	if matched {
		return setting, true
	}
	// Cleanly not met: a whitelist Allow closes (Deny); a restriction drops (inherit).
	if setting == SettingAllow {
		return SettingDeny, true
	}
	return "", false
}

// hostMatches reports whether host matches any pattern. A pattern is either an
// exact host ("api.dixa.io") or a "*.suffix" wildcard ("*.coolrunner.dk") that
// matches the bare suffix and any subdomain of it. Case- and space-insensitive.
func hostMatches(patterns []string, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "*.") {
			bare := p[2:]   // "coolrunner.dk"
			dotted := p[1:] // ".coolrunner.dk"
			if host == bare || strings.HasSuffix(host, dotted) {
				return true
			}
			continue
		}
		if host == p {
			return true
		}
	}
	return false
}

// HostOf extracts the host a tool call targets, for building a RequestContext
// against a host-allowlist Condition. It is the gate-side adapter from a tool
// call's resource string (a web_fetch URL, a connection endpoint) to the Host
// the WHEN layer matches on, kept next to hostMatches so parse and match share
// one notion of a host. A full URL ("https://api.dixa.io/v1") yields its host;
// a scheme-less "host/path" or "host:port" yields the bare host; a value with
// no recognizable host yields "". The result is lower-cased to match hostMatches.
func HostOf(resource string) string {
	s := strings.TrimSpace(resource)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil {
			return strings.ToLower(u.Hostname())
		}
		return ""
	}
	// Scheme-less: keep the authority up to the first path separator, then drop
	// any ":port" so "api.dixa.io:443/x" and "api.dixa.io" both yield the host.
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(s)
}

// verbedNamespaces are the capability namespaces whose key encodes the action
// verb as its trailing dotted segment, so an action-scoped Condition can bite.
// Repos already resolve under these dotted keys (grants.CapabilityRepo{Read,
// Checkout,Push} = "repo.read"/"repo.checkout"/"repo.push"); credentials use the
// same shape (credential.reveal/rotate/use) as their gated tool keys land. Any
// key outside this set — a bare Claude tool ("tools:Bash"), an MCP/connection key
// ("mcp__…", "connection:…"), or an unverbed namespace ("issue.read") — has no
// action a Condition.Actions list is meant to match, so ActionOf yields "".
var verbedNamespaces = map[string]struct{}{
	"repo":       {},
	"credential": {},
}

// ActionOf derives the canonical action verb a tool key attempts, for matching an
// action-scoped Condition (Condition.Actions) at the gate. It is the gate-side
// adapter from a tool/capability key to the Action the WHEN layer matches on,
// kept next to containsFold so derivation and match share one notion of a verb.
// The verb is the trailing segment of a dotted key whose head namespace is one we
// have verbed (repo, credential): "repo.checkout" → "checkout", "credential.reveal"
// → "reveal". A key with no recognized verbed namespace yields "" — no action
// constraint can bind, so an action-scoped Condition simply does not match it
// (and a rule without an Actions term is unaffected). Pure and case-folding to
// match containsFold; result is lower-cased.
func ActionOf(toolKey string) string {
	s := strings.ToLower(strings.TrimSpace(toolKey))
	head, verb, ok := strings.Cut(s, ".")
	if !ok || verb == "" {
		return ""
	}
	// A colon-namespaced key that contains a dot ("connection:repo.x") yields a
	// head of "connection:repo", which is not in verbedNamespaces — so this lookup
	// also rejects those, leaving only a clean "<ns>.<verb>" key with a known head.
	if _, known := verbedNamespaces[head]; !known {
		return ""
	}
	return verb
}

// containsFold reports whether list contains v, ignoring case and surrounding
// space on both sides.
func containsFold(list []string, v string) bool {
	v = strings.TrimSpace(v)
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), v) {
			return true
		}
	}
	return false
}
