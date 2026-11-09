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

var defaultPolicies = map[string]string{
	ClassRead:   PolicyAllow,
	ClassWrite:  PolicyAllow,
	ClassSend:   PolicyConfirm,
	ClassDelete: PolicyConfirm,
	ClassShare:  PolicyBlock,
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

// DefaultPolicy is the policy an account uses for class when it has no rule:
// the curated app's default, else the workspace default.
func DefaultPolicy(toolkitSlug, class string) string {
	if app, ok := Curated(toolkitSlug); ok {
		if p, ok := app.Defaults[class]; ok {
			return p
		}
	}
	if p, ok := defaultPolicies[class]; ok {
		return p
	}
	return PolicyConfirm
}

// levelCap is the loosest policy an agent level permits for class.
func levelCap(level, class string) string {
	if class == ClassRead {
		return PolicyAllow
	}
	switch level {
	case LevelRead:
		return PolicyBlock
	case LevelWrite:
		return PolicyAllow
	default:
		return PolicyConfirm
	}
}

// Decide returns the policy for one call. rule is the account's rule for the
// class ("" when unset). The account's rule (or default) and the agent's level
// both apply; the stricter one wins, so an agent setting can only tighten what
// the account owner allows and never loosen it.
func Decide(toolkitSlug, level, class, rule string) string {
	account := rule
	if !ValidPolicy(account) {
		account = DefaultPolicy(toolkitSlug, class)
	}
	return Stricter(account, levelCap(level, class))
}
