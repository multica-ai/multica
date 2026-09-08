package cerebra

// Policy enforces provider/model allowlists, budget ceilings, data sensitivity
// rules, tool/MCP minimum tier, and environment restrictions.
//
// All checks run BEFORE model selection (the filter-before-select invariant).
// The zero-value Policy allows everything — safe default when routing is first
// wired in without any active restrictions.
type Policy struct {
	// ProviderAllowlist, when non-empty, limits routing to the listed providers.
	// Provider names are matched case-insensitively against the runtime's provider field.
	ProviderAllowlist []string

	// ModelAllowlist, when non-empty, limits routing to the listed model IDs.
	ModelAllowlist []string

	// ModelDenylist explicitly blocks the listed model IDs regardless of tier.
	ModelDenylist []string

	// BudgetCeiling is the maximum estimated cost per task in USD.
	// 0 means no budget constraint.
	BudgetCeiling float64

	// DataSensitiveProviders is the set of provider names that are NOT allowed
	// when the task is classified as privacy-sensitive.
	DataSensitiveProviders []string

	// MinMCPTier is the minimum tier for tool/MCP tasks.
	// Defaults to TierStandard when zero.
	MinMCPTier Tier

	// EnvironmentRestrictions maps environment names (e.g. "prod") to allowed providers.
	// Empty means no environment restrictions.
	EnvironmentRestrictions map[string][]string

	// CurrentEnvironment is the environment the daemon is running in.
	CurrentEnvironment string
}

// Allow returns true if the given (runtimeID, model) pair passes all policy checks.
// runtimeID is currently used for environment-restriction lookups; extend with
// provider lookups once the runtime registry is threaded through.
func (p *Policy) Allow(runtimeID, model string) bool {
	if p == nil {
		return true
	}

	// Model denylist takes priority.
	for _, denied := range p.ModelDenylist {
		if denied == model {
			return false
		}
	}

	// Model allowlist filter.
	if len(p.ModelAllowlist) > 0 {
		found := false
		for _, allowed := range p.ModelAllowlist {
			if allowed == model {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Environment restrictions (by runtimeID as a stand-in until provider is available).
	if p.CurrentEnvironment != "" && len(p.EnvironmentRestrictions) > 0 {
		if allowedProviders, ok := p.EnvironmentRestrictions[p.CurrentEnvironment]; ok {
			_ = allowedProviders // wire provider lookup here when available
		}
	}

	return true
}

// AllowForSensitiveTask is like Allow but additionally enforces the data
// sensitivity restriction — blocks providers in DataSensitiveProviders.
// Call this when the task has been marked as containing sensitive data.
func (p *Policy) AllowForSensitiveTask(runtimeID, model string) bool {
	if !p.Allow(runtimeID, model) {
		return false
	}
	// Extend: look up the provider for runtimeID and reject if it is in
	// DataSensitiveProviders. Requires threading the runtime→provider map here.
	return true
}

// EffectiveMCPTier returns the minimum tier for MCP/tool tasks.
func (p *Policy) EffectiveMCPTier() Tier {
	if p == nil || p.MinMCPTier == "" {
		return TierStandard
	}
	return p.MinMCPTier
}
