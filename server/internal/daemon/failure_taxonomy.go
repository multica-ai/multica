package daemon

import "strings"

// Taxonomy values — the unified, user-visible failure classification.
// Internal detail is preserved in the error text field; these values
// are what task records, APIs, and the UI should consume.
const (
	TaxonomyCancelled       = "cancelled"
	TaxonomyTimeout         = "timeout"
	TaxonomyRateLimited     = "rate_limited"
	TaxonomyParseError      = "parse_error"
	TaxonomyUpstreamFailure = "upstream_failure"
	TaxonomyRuntimeOffline  = "runtime_offline"
	TaxonomyQueuedExpired   = "queued_expired"
	TaxonomyUnknown         = "unknown"
)

// MapToTaxonomy normalizes a raw failure_reason value (or daemon-reported
// reason) into one of the standard taxonomy categories. Unknown or empty
// values map to TaxonomyUnknown. The original value is preserved in the
// error/detail field by the caller.
func MapToTaxonomy(raw string) string {
	switch raw {
	case "cancelled", "user_cancelled", "deny", "decline", "reject":
		return TaxonomyCancelled
	case "timeout", "timed_out", "idle_watchdog":
		return TaxonomyTimeout
	case "rate_limited":
		return TaxonomyRateLimited
	case "iteration_limit", "agent_fallback_message":
		return TaxonomyParseError
	case "api_invalid_request":
		return classifyAPIError(raw)
	case "upstream_failure":
		return TaxonomyUpstreamFailure
	case "runtime_offline", "runtime_recovery":
		return TaxonomyRuntimeOffline
	case "queued_expired":
		return TaxonomyQueuedExpired
	case "agent_error":
		return TaxonomyUnknown
	default:
		if raw == "" {
			return TaxonomyUnknown
		}
		return TaxonomyUnknown
	}
}

// classifyAPIError distinguishes rate-limiting from other upstream errors.
// When we only have the coarse "api_invalid_request" tag, we default to
// upstream_failure. Callers that have the raw error text should use
// ClassifyErrorText for finer-grained classification.
func classifyAPIError(_ string) string {
	return TaxonomyUpstreamFailure
}

// ClassifyErrorText examines a raw error message and returns an appropriate
// taxonomy value. This is used when the daemon has the full error text and
// can distinguish rate-limiting (429) from other API failures.
func ClassifyErrorText(errMsg string) string {
	if errMsg == "" {
		return TaxonomyUnknown
	}
	lowered := strings.ToLower(errMsg)
	if strings.Contains(lowered, "429") && (strings.Contains(lowered, "rate") || strings.Contains(lowered, "too many")) {
		return TaxonomyRateLimited
	}
	if strings.Contains(lowered, "invalid_request_error") && strings.Contains(lowered, "400") {
		return TaxonomyUpstreamFailure
	}
	if strings.Contains(lowered, "overloaded") || strings.Contains(lowered, "503") || strings.Contains(lowered, "500") {
		return TaxonomyUpstreamFailure
	}
	return ""
}
