package taskfailure

import "testing"

func TestTraePromptResourceAccess(t *testing.T) {
	t.Parallel()

	const prompt = "traecli session/prompt failed: session/prompt: Internal error (code=-32603"
	const denial = "You do not have permission to access this resource."
	const deniedPrompt = prompt + `, data={"message":"` + denial + `"})`
	for _, tc := range []struct {
		name string
		raw  string
		want Reason
	}{
		{"explicit denial", deniedPrompt, ReasonAgentProviderAuthOrAccess},
		{"generic failure", prompt + `, data={"message":"An internal failure occurred."})`, ReasonAgentProviderServerError},
		{"missing data", prompt + ")", ReasonAgentProviderServerError},
		{"malformed data", prompt + ", data=" + denial + ")", ReasonAgentProviderServerError},
		{"unrelated field", prompt + `, data={"input":"` + denial + `"})`, ReasonAgentProviderServerError},
		{"other method", `traecli session/load failed: session/load: Internal error (code=-32603, data={"message":"` + denial + `"})`, ReasonAgentProviderServerError},
		{"tool output", "Tool output: " + deniedPrompt, ReasonAgentProviderServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.raw); got != tc.want {
				t.Errorf("Classify = %q, want %q", got, tc.want)
			}
			if got := NormalizeDaemonReason(ReasonAgentProviderServerError.String(), tc.raw); got != tc.want {
				t.Errorf("NormalizeDaemonReason = %q, want %q", got, tc.want)
			}
		})
	}
	if got := NormalizeDaemonReason(ReasonRuntimeAccessDenied.String(), deniedPrompt); got != ReasonRuntimeAccessDenied {
		t.Errorf("NormalizeDaemonReason changed the structured reason to %q", got)
	}
}
