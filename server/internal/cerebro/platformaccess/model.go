package platformaccess

// Enforcement identifies the call-time access contract for one platform
// capability. It is deliberately separate from tool discovery: a registered
// tool can still require opt-in, a human actor, or workspace ownership.
type Enforcement string

const (
	EnforcementPolicy            Enforcement = "policy"
	EnforcementAuthenticatedRead Enforcement = "authenticated_read"
	EnforcementActorOptIn        Enforcement = "actor_opt_in"
	EnforcementHumanOptIn        Enforcement = "human_opt_in"
	EnforcementOwnerOnly         Enforcement = "owner_only"
)

// Actor is the minimum trusted identity context needed by platform capability
// enforcement. Authentication, actor kind, and ownership come from the caller;
// they are never inferred from a visible tool or an authored policy row.
type Actor struct {
	Authenticated bool
	Agent         bool
	Owner         bool
}
