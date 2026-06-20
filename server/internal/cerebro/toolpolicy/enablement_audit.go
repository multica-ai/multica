// Enablement-readiness audit (FIR-1609 Phase 6). The unified permission engine
// is built but its gates are still flag-gated OFF (repo_grants_enabled,
// cerebro_policy_cel). Before flipping enforcement on for a workspace we must
// answer ONE go/no-go question: if the chain decided every request instead of
// the legacy enforcing paths, would any agent that can act TODAY be locked out?
//
// The "fejl-luk hvis ikke 1:1" rule from the milestone: anything a legacy path
// allows today that the chain would NOT Allow is a lock-out risk and must become
// an explicit Allow row before enablement. This file computes that delta.
//
// Why the delta is computable from the chain's shape, not guesswork: every
// enforcing gate resolves through the chain with Base=Allow (the daemon repo
// gate in CheckDaemonRepoCapability and the credential caps gate in resolveCap
// both set Query.Base = SettingAllow). Resolve is a monotonic max over
// restrictiveness, so with Base=Allow a resource with no explicit row resolves
// to Allow. A currently-allowed grant can therefore only flip to Ask/Deny if an
// explicit Ask/Deny row — or an unsatisfied Condition that downgrades to Deny —
// targets it. The audit makes that the literal definition of a lock-out risk.
package toolpolicy

import "context"

// LegacyPath names one of the legacy enforcing surfaces an agent's access flows
// through TODAY, before enablement. Each enumerated grant carries the path it
// came from so the audit report can group lock-out risks by the system that
// must gain an explicit Allow row before that path is switched to the chain.
type LegacyPath string

const (
	// LegacyPathRepoAllowlist is the daemon's flat per-workspace repo allowlist
	// (workspaceRepoAllowed: allowedRepoURLs + taskRepoURLs). Switched to the
	// chain by the repo_grants_enabled workspace setting.
	LegacyPathRepoAllowlist LegacyPath = "repo_allowlist"
	// LegacyPathWebFetchHost is the web_fetch host policy (webfetchpolicy), still
	// the enforcing source for host access; folded into web_fetch rows as a
	// host Condition on the read side only so far.
	LegacyPathWebFetchHost LegacyPath = "web_fetch_host"
	// LegacyPathConnectionPerTool is the per-tool connection access surface
	// (a tool reachable because its connection is attached to the agent).
	LegacyPathConnectionPerTool LegacyPath = "connection_per_tool"
	// LegacyPathCredentialGrant is the credential grant floor (deny-by-default
	// grants). The chain caps layer (adminCap/resolveCap) tightens on top of it.
	LegacyPathCredentialGrant LegacyPath = "credential_grant"
)

// LegacyGrant is one thing an agent (or user) can do TODAY via a legacy path: a
// (toolKey, resource) the legacy surface currently permits for a subject. The
// audit asks the chain what it would decide for the same tuple.
type LegacyGrant struct {
	Path     LegacyPath
	ToolKey  string
	Resource string
	// Subject ids. Leave the zero value for layers that do not apply (e.g. an
	// autonomous daemon checkout carries only Agent + Workspace).
	Query Query
}

// Resolver is the chain decision surface the audit runs each grant through. It
// is satisfied by *Store; the narrow interface keeps the audit unit-testable
// without a database.
type Resolver interface {
	Resolve(ctx context.Context, in Query) (Effective, error)
}

// LockoutRisk is one currently-allowed grant the chain would NOT Allow — i.e. a
// subject that would be locked out the moment the gate flips to the chain. Each
// risk must be cleared (by writing an explicit Allow row) before enablement.
type LockoutRisk struct {
	Grant   LegacyGrant
	Verdict Effective
	// Reason is the human-readable chain reason, or a fail-closed note when the
	// chain could not be resolved (an unresolvable grant is treated as a risk —
	// we never assume "safe" on an error).
	Reason string
}

// AuditEnablement returns the lock-out delta: every legacy grant the chain would
// not resolve to Allow. An empty result means enabling the corresponding gates
// is 1:1 with today's access — the go-signal. A non-empty result lists exactly
// what must first become an explicit Allow row (fail-closed: the no-go set).
//
// Each grant is resolved exactly as the live gates resolve it: Base=Allow and
// RequestContext.Action derived from the tool key, so an action-scoped Condition
// is evaluated here the same way it bites in production. A resolve error is
// fail-closed — recorded as a risk, never silently dropped.
func AuditEnablement(ctx context.Context, resolver Resolver, grants []LegacyGrant) ([]LockoutRisk, error) {
	if resolver == nil {
		return nil, nil
	}
	var risks []LockoutRisk
	for _, g := range grants {
		q := g.Query
		q.ToolKey = g.ToolKey
		q.ResourcePattern = g.Resource
		q.Base = SettingAllow
		// Mirror the live gates: derive the action verb so an action-scoped
		// Condition is evaluated, not silently skipped.
		if q.RequestContext.Action == "" {
			q.RequestContext.Action = ActionOf(g.ToolKey)
		}
		eff, err := resolver.Resolve(ctx, q)
		if err != nil {
			risks = append(risks, LockoutRisk{
				Grant:   g,
				Verdict: Effective{Setting: SettingDeny},
				Reason:  "fail-closed: chain resolve error — cannot prove access is preserved",
			})
			continue
		}
		if eff.Setting != SettingAllow {
			risks = append(risks, LockoutRisk{Grant: g, Verdict: eff, Reason: eff.Reason})
		}
	}
	return risks, nil
}

// ReadyForEnablement reports whether the lock-out delta is empty — the 1:1 check.
// True means every legacy grant audited still resolves to Allow under the chain,
// so flipping the gate on does not lock anyone out.
func ReadyForEnablement(risks []LockoutRisk) bool { return len(risks) == 0 }
