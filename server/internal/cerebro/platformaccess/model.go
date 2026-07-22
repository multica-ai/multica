package platformaccess

// Enforcement identifies the call-time access contract for one platform
// capability. It is deliberately separate from tool discovery: a registered
// tool can still require opt-in, a human actor, or workspace ownership.
type Enforcement string

const (
	EnforcementPolicy            Enforcement = "policy"
	EnforcementAuthenticatedRead Enforcement = "authenticated_read"
	EnforcementActorOptIn        Enforcement = "actor_opt_in"
	EnforcementAgentOptIn        Enforcement = "agent_opt_in"
	EnforcementHumanOptIn        Enforcement = "human_opt_in"
	EnforcementHumanOptInOrAdmin Enforcement = "human_opt_in_or_admin"
	EnforcementOwnerOnly         Enforcement = "owner_only"
)

// Contract is the single declared resolver contract for a permission key whose
// call-time semantics differ from the ordinary policy chain. Every read surface
// and enforcement point asks this registry instead of selecting a resolver on
// its own.
type Contract struct {
	Key         string
	Enforcement Enforcement
}

var contracts = map[string]Contract{
	"hooks:read":                 {Key: "hooks:read", Enforcement: EnforcementAuthenticatedRead},
	"hooks:write":                {Key: "hooks:write", Enforcement: EnforcementActorOptIn},
	"hooks:enforce":              {Key: "hooks:enforce", Enforcement: EnforcementHumanOptIn},
	"hooks:manage_managed":       {Key: "hooks:manage_managed", Enforcement: EnforcementOwnerOnly},
	"tools:personal-browser":     {Key: "tools:personal-browser", Enforcement: EnforcementAgentOptIn},
	"tools:test-as-user":         {Key: "tools:test-as-user", Enforcement: EnforcementHumanOptIn},
	"manage_workspace_overrides": {Key: "manage_workspace_overrides", Enforcement: EnforcementHumanOptInOrAdmin},
	"manage_group_overrides":     {Key: "manage_group_overrides", Enforcement: EnforcementHumanOptInOrAdmin},
}

// ForKey returns the declared resolver contract for key. Keys absent from this
// registry use the ordinary policy chain.
func ForKey(key string) (Contract, bool) {
	contract, ok := contracts[key]
	return contract, ok
}

// All returns a stable copy of every special permission contract.
func All() []Contract {
	keys := []string{
		"hooks:read",
		"hooks:write",
		"hooks:enforce",
		"hooks:manage_managed",
		"tools:personal-browser",
		"tools:test-as-user",
		"manage_workspace_overrides",
		"manage_group_overrides",
	}
	out := make([]Contract, 0, len(keys))
	for _, key := range keys {
		out = append(out, contracts[key])
	}
	return out
}

// Actor is the minimum trusted identity context needed by platform capability
// enforcement. Authentication, actor kind, and ownership come from the caller;
// they are never inferred from a visible tool or an authored policy row.
type Actor struct {
	Authenticated bool
	Agent         bool
	Owner         bool
	Admin         bool
}
