package daemon

import "testing"

func TestMapToTaxonomy(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"cancelled", TaxonomyCancelled},
		{"user_cancelled", TaxonomyCancelled},
		{"deny", TaxonomyCancelled},
		{"decline", TaxonomyCancelled},
		{"reject", TaxonomyCancelled},
		{"timeout", TaxonomyTimeout},
		{"timed_out", TaxonomyTimeout},
		{"idle_watchdog", TaxonomyTimeout},
		{"rate_limited", TaxonomyRateLimited},
		{"iteration_limit", TaxonomyParseError},
		{"agent_fallback_message", TaxonomyParseError},
		{"api_invalid_request", TaxonomyUpstreamFailure},
		{"upstream_failure", TaxonomyUpstreamFailure},
		{"runtime_offline", TaxonomyRuntimeOffline},
		{"runtime_recovery", TaxonomyRuntimeOffline},
		{"queued_expired", TaxonomyQueuedExpired},
		{"agent_error", TaxonomyUnknown},
		{"", TaxonomyUnknown},
		{"something_new", TaxonomyUnknown},
	}
	for _, tt := range tests {
		got := MapToTaxonomy(tt.raw)
		if got != tt.want {
			t.Errorf("MapToTaxonomy(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestClassifyErrorText(t *testing.T) {
	tests := []struct {
		errMsg string
		want   string
	}{
		{"API Error: 429 rate limit exceeded", TaxonomyRateLimited},
		{"too many requests (429)", TaxonomyRateLimited},
		{"API Error: 400 {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\"}}", TaxonomyUpstreamFailure},
		{"503 Service Temporarily Unavailable", TaxonomyUpstreamFailure},
		{"The server is overloaded", TaxonomyUpstreamFailure},
		{"normal error message", ""},
		{"", TaxonomyUnknown},
	}
	for _, tt := range tests {
		got := ClassifyErrorText(tt.errMsg)
		if got != tt.want {
			t.Errorf("ClassifyErrorText(%q) = %q, want %q", tt.errMsg, got, tt.want)
		}
	}
}
