package timing

import "testing"

func TestClassifyDoesNotReadStatusCodesInsideAddressesOrIDs(t *testing.T) {
	t.Parallel()

	for message, want := range map[string]string{
		"request failed: 429 Too Many Requests": "rate_limit",
		"http_429":                              "rate_limit",
		"http_401: refused":                     "authentication",
		"status 401":                            "authentication",
		"forbidden: 403":                        "authentication",
		"failed to dial 12D3KooWa429b: no good addresses":        "other",
		"dial /ip4/10.0.0.1/tcp/54290: connection refused":       "other",
		"dial /ip4/10.0.0.1/tcp/4031: context deadline exceeded": "transport",
		"dial 12D3KooW401x: timeout":                             "transport",
	} {
		if got := Classify(message); got != want {
			t.Errorf("Classify(%q) = %q, want %q", message, got, want)
		}
	}
}
