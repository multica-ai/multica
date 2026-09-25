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

// TestAuthMethodUnresolved pins the GH #6777 predicate. The positives are the
// provider phrase as it reaches us through each ACP lifecycle step; the
// negatives are every other authentication-shaped failure, which a fresh
// session cannot cure and which must therefore keep its session pointer.
func TestAuthMethodUnresolved(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		errMsg string
		want   bool
	}{
		{
			// Verbatim from the reporter, as the ACP provider-error sniffer
			// wraps it (GH #6777).
			name:   "gh6777 sniffer-wrapped provider error",
			errMsg: `hermes provider error: "Could not resolve authentication method. Expected either api_key or auth_token to be set. Or for one of the X-Api-Key or Authorization headers to be explicitly omitted"`,
			want:   true,
		},
		{
			// Same failure one step earlier: the adapter wraps the runtime
			// message with %v instead of replacing it, so the phrase survives.
			name:   "set_model wrapper keeps the phrase",
			errMsg: `hermes could not switch to model "custom:deepseek-v4-pro": session/set_model: Could not resolve authentication method. Expected either api_key or auth_token to be set.`,
			want:   true,
		},
		{
			name:   "case differences do not matter",
			errMsg: "COULD NOT RESOLVE AUTHENTICATION METHOD",
			want:   true,
		},
		{
			name:   "empty error",
			errMsg: "",
			want:   false,
		},

		// --- Negatives: authentication-shaped, but not session-shaped. A new
		// session replays these verbatim, so matching them would cost a wasted
		// run AND the conversation pointer. ---
		{
			name:   "rejected api key",
			errMsg: `hermes provider error: "401 authentication_error: invalid x-api-key"`,
			want:   false,
		},
		{
			name:   "revoked oauth token",
			errMsg: "OAuth access token has been revoked",
			want:   false,
		},
		{
			name:   "missing credential env var",
			errMsg: "missing environment variable: ANTHROPIC_API_KEY",
			want:   false,
		},
		{
			// Talks about resolving and about auth, but is a DNS failure.
			name:   "unresolved host is not unresolved auth",
			errMsg: "dial tcp: lookup auth.example.com: no such host",
			want:   false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AuthMethodUnresolved(tt.errMsg); got != tt.want {
				t.Errorf("AuthMethodUnresolved(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
		})
	}
}

// TestAuthMethodUnresolvedMatchesResumeQueryGuard asserts the Go predicate and
// the SQL guard in GetLastTaskSession / GetLastChatTaskSession agree on the
// phrase. They are two independent implementations of the same rule — the
// daemon's in-turn retry reads this one, and rows written by a daemon too old
// to have it are caught by the SQL one — so a drift would leave one layer
// resuming a session the other considers dead.
func TestAuthMethodUnresolvedMatchesResumeQueryGuard(t *testing.T) {
	t.Parallel()

	// The ILIKE pattern the queries apply, minus the wildcards.
	const sqlGuardPhrase = "could not resolve authentication method"

	if !AuthMethodUnresolved("prefix " + sqlGuardPhrase + " suffix") {
		t.Fatalf("predicate does not match the SQL guard phrase %q — pkg/db/queries and pkg/taskfailure have drifted", sqlGuardPhrase)
	}
}
