package taskfailure

import "testing"

// TestUnresumableHistory pins the predicate against the real provider wordings
// collected from the field. The positives are verbatim strings from user
// reports; the negatives are the shapes that must keep resuming, because a
// false positive throws away a healthy conversation.
func TestUnresumableHistory(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		errMsg string
		want   bool
	}{
		// --- Positives: an empty message is baked into the transcript. ---
		{
			// GH #6066, verbatim from the reporter's screenshot. Carries
			// neither "invalid_request_error" nor a bare "400", which is
			// exactly why the Anthropic-shaped detector missed it.
			name:   "gh6066 invalid request with position and role",
			errMsg: "Invalid request: the message at position 37 with role 'assistant' must not be empty",
			want:   true,
		},
		{
			// GH #5760, from a manual `kimi -S <session> -p ping` repro.
			name:   "gh5760 kimi provider api_error",
			errMsg: "provider.api_error: 400 the message at position 43 with role 'assistant' must not be empty",
			want:   true,
		},
		{
			name:   "kimi wrapped by the acp sniffer",
			errMsg: "kimi provider error: the message at position 43 with role 'assistant' must not be empty",
			want:   true,
		},
		{
			// Anthropic's own wording for the same defect.
			name:   "anthropic indexed message non-empty content",
			errMsg: `API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"messages.37: all messages must have non-empty content except for the optional final assistant message"}}`,
			want:   true,
		},
		{
			name:   "openai-compatible indexed content field",
			errMsg: "messages[43].content: content must not be empty",
			want:   true,
		},
		{
			name:   "double-quoted role",
			errMsg: `Invalid request: the message with role "assistant" must not be empty`,
			want:   true,
		},
		{
			name:   "unspaced role token",
			errMsg: "invalid_request: role=assistant content cannot be empty",
			want:   true,
		},
		{
			name:   "prose wording",
			errMsg: "The final assistant message must not be empty.",
			want:   true,
		},

		// --- Negatives: emptiness complaints that are NOT about history. ---
		{
			// The single most likely false positive: a tool or validator
			// complaining about a field. No locator into the message history.
			name:   "tool validation error",
			errMsg: "validation error: field must not be empty",
			want:   false,
		},
		{
			name:   "git commit message",
			errMsg: "Aborting commit due to empty commit message: commit message must not be empty",
			want:   false,
		},
		{
			name:   "empty api key",
			errMsg: "config error: api key must not be empty",
			want:   false,
		},

		// --- Negatives: history locator without an emptiness complaint. ---
		{
			name:   "oversized image in history",
			errMsg: "messages[3].content[0].image.source.base64.data: image dimensions exceed max allowed size",
			want:   false,
		},
		{
			name:   "assistant message mentioned in a normal error",
			errMsg: "could not render assistant message: template failure",
			want:   false,
		},

		// --- Negatives: transient failures that MUST keep the session. ---
		{
			name:   "rate limit",
			errMsg: "API Error: 429 rate_limit_error: too many requests",
			want:   false,
		},
		{
			name:   "provider 5xx",
			errMsg: "provider.api_error: 503 upstream temporarily unavailable",
			want:   false,
		},
		{
			name:   "network drop",
			errMsg: "Connection closed mid-response",
			want:   false,
		},
		{
			name:   "empty input",
			errMsg: "",
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := UnresumableHistory(tc.errMsg); got != tc.want {
				t.Fatalf("UnresumableHistory(%q) = %v, want %v", tc.errMsg, got, tc.want)
			}
		})
	}
}

// TestUnresumableHistoryIsStatusAndProviderAgnostic is the regression that
// keeps this from sliding back into a per-provider string match. The same
// defect wearing four different provider costumes must classify identically —
// that is the property GH #6066 and GH #5760 both needed and neither had.
func TestUnresumableHistoryIsStatusAndProviderAgnostic(t *testing.T) {
	t.Parallel()

	sameDefect := []string{
		"Invalid request: the message at position 37 with role 'assistant' must not be empty",
		"provider.api_error: 400 the message at position 43 with role 'assistant' must not be empty",
		"kimi provider error: the message at position 1 with role 'assistant' must not be empty",
		"messages.12: all messages must have non-empty content",
	}
	for _, errMsg := range sameDefect {
		if !UnresumableHistory(errMsg) {
			t.Errorf("provider wording must not change the verdict, missed: %q", errMsg)
		}
	}
}
