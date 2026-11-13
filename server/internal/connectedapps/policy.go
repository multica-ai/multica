package connectedapps

// Policies a rule can set for an action class.
const (
	PolicyAllow   = "allow"
	PolicyConfirm = "confirm"
	PolicyBlock   = "block"
)

// ValidPolicy reports whether policy is a rule policy.
func ValidPolicy(policy string) bool {
	return policy == PolicyAllow || policy == PolicyConfirm || policy == PolicyBlock
}

func strictness(policy string) int {
	switch policy {
	case PolicyAllow:
		return 0
	case PolicyConfirm:
		return 1
	default:
		return 2
	}
}

// Stricter returns the stricter of two policies.
func Stricter(a, b string) string {
	if strictness(a) >= strictness(b) {
		return a
	}
	return b
}

// DefaultPolicy allows every supported action class unless an account rule
// or the agent's read-only level restricts it.
func DefaultPolicy(_ string, class string) string {
	if ValidClass(class) {
		return PolicyAllow
	}
	return PolicyBlock
}

// levelCap is the loosest policy an agent level permits for class.
func levelCap(level, class string) string {
	if class == ClassRead {
		return PolicyAllow
	}
	switch level {
	case LevelRead:
		return PolicyBlock
	case LevelWrite, LevelConfirmWrites:
		return PolicyAllow
	default:
		return PolicyBlock
	}
}

// Decide returns the policy for one call. rule is the account's rule for the
// class ("" when unset). The account's rule (or default) and the agent's level
// both apply; the stricter one wins, so an agent setting can only tighten what
// the account owner allows and never loosen it. Saved confirmation settings
// allow calls directly now that per-call approval has been removed.
func Decide(toolkitSlug, level, class, rule string) string {
	account := rule
	if !ValidPolicy(account) {
		account = DefaultPolicy(toolkitSlug, class)
	}
	if account == PolicyConfirm {
		account = PolicyAllow
	}
	return Stricter(account, levelCap(level, class))
}
