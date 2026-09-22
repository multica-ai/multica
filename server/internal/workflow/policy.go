package workflow

import (
	"math"
	"strings"
	"time"
)

type RetryPolicy struct {
	MaxRetries        int
	InitialDelay      time.Duration
	BackoffMultiplier int
	MaxDelay          time.Duration
	ReadOnly          bool
	Idempotent        bool
	EffectState       string
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:        2,
		InitialDelay:      30 * time.Second,
		BackoffMultiplier: 2,
		MaxDelay:          15 * time.Minute,
		EffectState:       "unknown",
	}
}

func (p RetryPolicy) Validate() error {
	if p.MaxRetries < 0 || p.MaxRetries > 10 {
		return Bad("max_retries must be between 0 and 10")
	}
	if p.InitialDelay <= 0 || p.MaxDelay < p.InitialDelay {
		return Bad("retry delays are invalid")
	}
	if p.BackoffMultiplier < 1 || p.BackoffMultiplier > 10 {
		return Bad("backoff_multiplier must be between 1 and 10")
	}
	if p.EffectState != "read_only" && p.EffectState != "idempotent" && p.EffectState != "unknown" {
		return Bad("effect_state must be read_only, idempotent, or unknown")
	}
	if p.ReadOnly && p.EffectState != "read_only" {
		return Bad("read_only requires a read_only effect state")
	}
	return nil
}

// RetryDelay calculates the delay after failedAttempt. failedAttempt is
// one-based: the first failed attempt waits InitialDelay, the second waits
// InitialDelay*BackoffMultiplier, and so on. jitter must be in [-1, 1] and is
// intentionally passed in by the caller so tests and production can use
// different random sources without making time logic nondeterministic.
func RetryDelay(policy RetryPolicy, failedAttempt int, jitter float64) time.Duration {
	if failedAttempt < 1 {
		failedAttempt = 1
	}
	if jitter < -1 {
		jitter = -1
	}
	if jitter > 1 {
		jitter = 1
	}
	power := math.Pow(float64(maxInt(policy.BackoffMultiplier, 1)), float64(failedAttempt-1))
	delay := float64(policy.InitialDelay) * power
	if delay > float64(policy.MaxDelay) {
		delay = float64(policy.MaxDelay)
	}
	// The documented production jitter range is +/-20%. Keeping the argument
	// as a normalized value makes this function easy to test exactly.
	delay *= 1 + 0.2*jitter
	if delay < 0 {
		return 0
	}
	if delay > float64(policy.MaxDelay) {
		return policy.MaxDelay
	}
	return time.Duration(delay)
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

var retryableErrorClasses = map[string]bool{
	"network_temporary":   true,
	"service_unavailable": true,
	"rate_limited":        true,
}

func nodeDefinition(graph Graph, nodeID string) Node {
	for _, node := range graph.Nodes {
		if node.ID == nodeID {
			return node
		}
	}
	return Node{}
}

func retryPolicyForNode(graph Graph, node Node) RetryPolicy {
	policy := DefaultRetryPolicy()
	if graph.Defaults != (WorkflowDefaults{}) {
		policy.MaxRetries = graph.Defaults.MaxRetries
		if graph.Defaults.InitialDelaySeconds > 0 {
			policy.InitialDelay = time.Duration(graph.Defaults.InitialDelaySeconds) * time.Second
		}
		if graph.Defaults.BackoffMultiplier > 0 {
			policy.BackoffMultiplier = graph.Defaults.BackoffMultiplier
		}
		if graph.Defaults.MaxDelaySeconds > 0 {
			policy.MaxDelay = time.Duration(graph.Defaults.MaxDelaySeconds) * time.Second
		}
	}
	if node.MaxRetries > 0 {
		policy.MaxRetries = node.MaxRetries
	}
	if node.Config != nil {
		if effect, ok := node.Config["effect_policy"].(string); ok {
			policy.EffectState = effect
		}
		if raw, ok := node.Config["max_retries"].(float64); ok {
			policy.MaxRetries = int(raw)
		}
	}
	return policy
}

func classifyTaskError(message string) string {
	value := strings.ToLower(message)
	switch {
	case strings.Contains(value, "429"), strings.Contains(value, "rate limit"), strings.Contains(value, "too many requests"):
		return "rate_limited"
	case strings.Contains(value, "temporarily unavailable"), strings.Contains(value, "service unavailable"), strings.Contains(value, "503"):
		return "service_unavailable"
	case strings.Contains(value, "timeout"), strings.Contains(value, "connection reset"), strings.Contains(value, "network"):
		return "network_temporary"
	case strings.Contains(value, "permission"), strings.Contains(value, "forbidden"):
		return "permission_denied"
	case strings.Contains(value, "output"):
		return "output_invalid"
	default:
		return "execution_failed"
	}
}

// ShouldRetry applies the W1 safety rule: an unknown external effect is never
// replayed automatically. Waiting for a resource is not a technical attempt
// and therefore is handled outside this function.
func ShouldRetry(errorClass string, policy RetryPolicy, retriesUsed int) bool {
	if retriesUsed < 0 || retriesUsed >= policy.MaxRetries || !retryableErrorClasses[errorClass] {
		return false
	}
	if policy.EffectState == "unknown" && !policy.ReadOnly && !policy.Idempotent {
		return false
	}
	return true
}
