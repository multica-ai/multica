package taskfailure

import "testing"

func TestContentFilterDoesNotBecomeNetworkFailure(t *testing.T) {
	for _, text := range []string{
		"stream disconnected before completion: Incomplete response returned, reason: content_filter",
		`stream disconnected: {"reason":"content_filter"}`,
		`stream disconnected: {"reason": "content_filter"}`,
	} {
		if got := Classify(text); got != ReasonAgentContentFilter {
			t.Fatalf("Classify(%q) = %q", text, got)
		}
	}
	if got := Classify("stream disconnected before completion"); got != ReasonAgentProviderNetwork {
		t.Fatalf("ordinary stream failure = %q", got)
	}
	if got := Classify("I'm sorry, but I cannot assist with that request."); got == ReasonAgentContentFilter {
		t.Fatal("refusal wording alone must not classify content filtering")
	}
}
