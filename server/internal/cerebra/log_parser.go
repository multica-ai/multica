package cerebra

import (
	"regexp"
	"strings"
)

// FailureKind classifies an error string returned by the agent runtime.
type FailureKind int

const (
	// FailureUnknown is a generic error that does not trigger unavailability marking.
	FailureUnknown FailureKind = iota

	// FailureQuotaExhausted means the provider rejected the request due to
	// insufficient quota or billing. The model should be marked unavailable
	// for the standard TTL so future tasks are routed elsewhere.
	FailureQuotaExhausted

	// FailureRateLimit means the provider returned a rate-limit response.
	// Treat the same as FailureQuotaExhausted for unavailability purposes.
	FailureRateLimit

	// FailureContextLength means the prompt or combined context exceeded the
	// model's context window. This is tracked SEPARATELY from quota failures:
	// it does not mark the model unavailable (the model itself is fine) and
	// should instead trigger a context-reduction strategy or prompt truncation.
	FailureContextLength
)

// quotaSignals are substrings that indicate a quota, rate-limit, 404 not-found, or upstream provider failure.
var quotaSignals = []string{
	"insufficient_quota",
	"quota exceeded",
	"billing_hard_limit_reached",
	"you've exceeded",
	"you exceeded",
	"exceeded your current quota",
	"exceeded your quota",
	"quota_exceeded",
	"resource_exhausted",
	"resourceexhausted",
	"please retry in",
	"rate_limit_exceeded",
	"rate limit exceeded",
	"rate-limited",
	"rate limited",
	"temporarily rate-limited",
	"rate-limited upstream",
	"rate limited upstream",
	"only supports interactions api",
	"interactions api",
	"too many requests",
	"429",
	"404",
	"provider returned error",
	"upstream request failed",
	"model not found",
	"model_not_found",
	"does not support tools",
	"not support tools",
	"insufficient credits",
	"insufficient credit",
	"purchased credits",
	"purchase more",
	"insufficient balance",
}

var failedModelRegex = regexp.MustCompile(`(?i)\bmodel(?:s)?(?:\s*[:=]\s*|\s+['"]?)([a-zA-Z0-9_.:/-]+)`)

// ExtractFailedModel attempts to extract the specific model identifier named in an error message.
// For example: "model: gemma-4-26b" -> "gemma-4-26b", "models/gemini-1.5-pro" -> "gemini-1.5-pro".
func ExtractFailedModel(errMsg string) string {
	matches := failedModelRegex.FindStringSubmatch(errMsg)
	if len(matches) > 1 {
		clean := strings.Trim(matches[1], `'".,;:`)
		lower := strings.ToLower(clean)
		if clean != "" && lower != "not" && lower != "found" && lower != "is" && lower != "the" && lower != "does" {
			return clean
		}
	}
	return ""
}

// contextLengthSignals indicate that the context window was exceeded.
var contextLengthSignals = []string{
	"context_length_exceeded",
	"maximum context length",
	"context window",
	"token limit",
	"max_tokens",
}

// ParseFailure classifies an agent runtime error message into a FailureKind.
// The classification is case-insensitive substring matching — fast and
// deterministic, no regex overhead.
//
// Callers:
//   - daemon/context_exhausted.go: after task finalization, feed runtime log into here.
//   - If FailureQuotaExhausted or FailureRateLimit → call unavail.MarkUnavailable().
//   - If FailureContextLength → handle separately (do NOT mark unavailable).
func ParseFailure(errMsg string) FailureKind {
	lower := strings.ToLower(errMsg)

	// Context-length check FIRST: it must NOT trigger unavailability marking.
	for _, sig := range contextLengthSignals {
		if strings.Contains(lower, sig) {
			return FailureContextLength
		}
	}

	for _, sig := range quotaSignals {
		if strings.Contains(lower, sig) {
			if strings.Contains(lower, "rate") {
				return FailureRateLimit
			}
			return FailureQuotaExhausted
		}
	}

	return FailureUnknown
}

// ShouldMarkUnavailable returns true when the failure kind should cause the
// responsible model to be temporarily excluded from routing.
func ShouldMarkUnavailable(kind FailureKind) bool {
	return kind == FailureQuotaExhausted || kind == FailureRateLimit
}
