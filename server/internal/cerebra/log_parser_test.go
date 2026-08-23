package cerebra

import "testing"

func TestParseFailure(t *testing.T) {
	tests := []struct {
		name        string
		errMsg      string
		wantKind    FailureKind
		wantUnavail bool
	}{
		{
			name:        "quota error",
			errMsg:      "Error: OpenAI returned insufficient_quota. Please check your billing.",
			wantKind:    FailureQuotaExhausted,
			wantUnavail: true,
		},
		{
			name:        "rate limit 429",
			errMsg:      "HTTP 429: rate_limit_exceeded (Too Many Requests)",
			wantKind:    FailureRateLimit,
			wantUnavail: true,
		},
		{
			name:        "context length exceeded",
			errMsg:      "error: context_length_exceeded: maximum context length is 200000 tokens",
			wantKind:    FailureContextLength,
			wantUnavail: false, // Context overflow MUST NOT mark model unavailable
		},
		{
			name:        "generic network error",
			errMsg:      "connection reset by peer",
			wantKind:    FailureUnknown,
			wantUnavail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind := ParseFailure(tt.errMsg)
			if kind != tt.wantKind {
				t.Errorf("ParseFailure(%q) = %v, want %v", tt.errMsg, kind, tt.wantKind)
			}
			shouldUnavail := ShouldMarkUnavailable(kind)
			if shouldUnavail != tt.wantUnavail {
				t.Errorf("ShouldMarkUnavailable(%v) = %v, want %v", kind, shouldUnavail, tt.wantUnavail)
			}
		})
	}
}
