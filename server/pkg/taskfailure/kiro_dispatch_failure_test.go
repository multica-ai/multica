package taskfailure

import "testing"

// kiroDispatchFailureErr is verbatim from failed Kiro runs (MUL-8314): the AWS
// SDK's DispatchFailure, wrapped in ACP's -32603 "Internal error".
const kiroDispatchFailureErr = `kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Encountered an error in the response stream: An unknown error occurred: dispatch failure)`

func TestClassifyKiroDispatchFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want Reason
	}{
		// Transport failure: retryable and resume-safe, not a provider 5xx.
		{"dispatch failure", kiroDispatchFailureErr, ReasonAgentProviderNetwork},
		// A deterministic rejection under the same stream wrapper must not be
		// retried as network flakiness.
		{
			"oversized image under the same wrapper",
			`kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Encountered an error in the response stream: messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)`,
			ReasonAgentProviderServerError,
		},
		// "dispatch failure" without Kiro's stream witness is not provider evidence.
		{"bare dispatch failure", "mcp tool returned: dispatch failure", ReasonAgentUnknown},
		// Real provider evidence still wins over the transport witness.
		{
			"rate limit co-occurring",
			`kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Encountered an error in the response stream: 429 rate limit: dispatch failure)`,
			ReasonAgentProviderCapacityOrRateLimit,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tc.in); got != tc.want {
				t.Errorf("Classify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// An installed daemon predating the Classify rule reports
// provider_server_error; the server must upgrade it so the retry applies
// without waiting on the daemon fleet.
func TestNormalizeDaemonReasonUpgradesKiroDispatchFailure(t *testing.T) {
	t.Parallel()

	for _, legacy := range []string{
		string(ReasonAgentProviderServerError),
		string(ReasonAgentUnknown),
		"agent_error",
	} {
		if got := NormalizeDaemonReason(legacy, kiroDispatchFailureErr); got != ReasonAgentProviderNetwork {
			t.Errorf("NormalizeDaemonReason(%q, dispatch failure) = %q, want %q", legacy, got, ReasonAgentProviderNetwork)
		}
	}

	// A more specific reason the daemon chose is left alone.
	if got := NormalizeDaemonReason(string(ReasonAgentProviderAuthOrAccess), kiroDispatchFailureErr); got != ReasonAgentProviderAuthOrAccess {
		t.Errorf("auth reason was overwritten: got %q", got)
	}
	// A genuine server error without the dispatch witness is untouched.
	const serverErr = `kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Encountered an error in the response stream: 503 service unavailable)`
	if got := NormalizeDaemonReason(string(ReasonAgentProviderServerError), serverErr); got != ReasonAgentProviderServerError {
		t.Errorf("plain server error was upgraded: got %q", got)
	}
}
