package agent

import "errors"

// ExecutionPolicy is an admission decision from the authenticated claim endpoint,
// never from task text, metadata, or model output. No backend currently provides
// the isolation required to run a restricted task.
type ExecutionPolicy string

const (
	ExecutionPolicyUnrestricted ExecutionPolicy = "unrestricted"
	ExecutionPolicyRestricted   ExecutionPolicy = "restricted"
)

var ErrExecutionPolicyDenied = errors.New("execution policy requires isolation unavailable on this daemon")

// CheckExecutionPolicy preserves legacy claims when the field is absent or null.
// A present but empty or unrecognized policy fails closed. This compatibility
// behavior is not provenance detection; servers must withhold restricted tasks
// from old daemons and must never omit their policy.
func CheckExecutionPolicy(policy *ExecutionPolicy) error {
	if policy == nil || *policy == ExecutionPolicyUnrestricted {
		return nil
	}
	return ErrExecutionPolicyDenied
}
