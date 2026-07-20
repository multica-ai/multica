package taskfailure

import "testing"

// TestClassifyEmptyAndWhitespace pins the empty/whitespace contract.
// Daemon callers should never hand us empty error text — but if they
// do, returning the catchall is safer than panicking.
func TestClassifyEmptyAndWhitespace(t *testing.T) {
	t.Parallel()

	cases := []string{"", "   ", "\n\t  \n"}
	for _, in := range cases {
		if got := Classify(in); got != ReasonAgentUnknown {
			t.Errorf("Classify(%q) = %q, want %q", in, got, ReasonAgentUnknown)
		}
	}
}

// TestClassifyRules walks every classifier rule with a real-world
// sample taken from MUL-1949's db-boy production analysis (top error
// prefixes from `agent_task_queue.error` over a 7-day window). When
// MUL-1949's SQL grows a new rule, add a fixture here so the in-flight
// classifier and the offline backfill stay in lock-step.
//
// One test case per rule is the minimum bar; rules with notable
// boundary conditions (e.g. the 5xx regex) get a dedicated subtest
// further down.
func TestClassifyRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want Reason
	}{
		// 1. Context overflow.
		{"context length exceeded", "Error: context length exceeded for model gpt-4", ReasonAgentContextOverflow},
		{"context_length_exceeded code", `{"error":{"code":"context_length_exceeded"}}`, ReasonAgentContextOverflow},
		{"maximum context", "Maximum context window of 200000 tokens has been exceeded", ReasonAgentContextOverflow},
		{"prompt is too long", "API Error: prompt is too long: 250000 tokens > 200000 maximum", ReasonAgentContextOverflow},
		{"context size has been exceeded", "context size has been exceeded; consider /compact", ReasonAgentContextOverflow},
		{"token limit", "Hit the token limit for this conversation", ReasonAgentContextOverflow},

		// 2. Missing config.
		{"missing env var", "Missing environment variable: `MIFY_API_KEY`.", ReasonAgentMissingConfig},
		{"missing api_key", "Failed to authenticate: missing api_key in config", ReasonAgentMissingConfig},
		{"api key required", "An api key is required to use this provider", ReasonAgentMissingConfig},
		{"no llm provider configured", "no llm provider configured; set OPENAI_API_KEY", ReasonAgentMissingConfig},
		{"no provider configured", "no provider configured for runtime", ReasonAgentMissingConfig},

		// 3. Provider auth / access.
		{"401", "API Error: 401 Unauthorized", ReasonAgentProviderAuthOrAccess},
		{"403", "API Error: 403 Forbidden", ReasonAgentProviderAuthOrAccess},
		{"unauthorized text", "Request unauthorized for this organization", ReasonAgentProviderAuthOrAccess},
		{"login required", "login required: please run /login", ReasonAgentProviderAuthOrAccess},
		{"not logged in", "Not logged in · Please run /login", ReasonAgentProviderAuthOrAccess},
		{"please login again", "Session expired, please login again", ReasonAgentProviderAuthOrAccess},
		{"refresh token", "refresh token has expired", ReasonAgentProviderAuthOrAccess},
		{"invalid api key", "Invalid API key provided", ReasonAgentProviderAuthOrAccess},
		{"access token", "access token has been revoked", ReasonAgentProviderAuthOrAccess},
		{"subscription access", "Your organization has disabled Claude subscription access for Claude Code", ReasonAgentProviderAuthOrAccess},
		{"does not have access", "Your account does not have access to this model", ReasonAgentProviderAuthOrAccess},
		{"may not have access", "you may not have access to claude-3-opus", ReasonAgentProviderAuthOrAccess},

		// 4. Provider quota / billing.
		{"402", "API Error: 402 Payment Required", ReasonAgentProviderQuotaLimit},
		{"insufficient_balance", `{"error":{"code":"insufficient_balance"}}`, ReasonAgentProviderQuotaLimit},
		{"balance is too low", "balance is too low to make this request", ReasonAgentProviderQuotaLimit},
		{"monthly usage limit", "You've hit your org's monthly usage limit", ReasonAgentProviderQuotaLimit},
		{"usage limit", "Account exceeded the daily usage limit", ReasonAgentProviderQuotaLimit},
		{"hit your limit ascii", "you've hit your limit; upgrade to continue", ReasonAgentProviderQuotaLimit},
		{"hit your limit curly", "you\u2019ve hit your limit", ReasonAgentProviderQuotaLimit},
		{"credits", "Your account has 0 credits remaining", ReasonAgentProviderQuotaLimit},
		{"quota", "quota exceeded for project foo", ReasonAgentProviderQuotaLimit},

		// 5. Capacity / rate limit.
		{"429", "API Error: 429 Too Many Requests", ReasonAgentProviderCapacityOrRateLimit},
		{"529", "Server overloaded: HTTP 529", ReasonAgentProviderCapacityOrRateLimit},
		{"rate limit", "rate limit exceeded for tier 3", ReasonAgentProviderCapacityOrRateLimit},
		{"overloaded", "overloaded_error: please retry", ReasonAgentProviderCapacityOrRateLimit},
		{"no capacity available", "no capacity available; try again later", ReasonAgentProviderCapacityOrRateLimit},

		// 6. Provider 5xx / server error.
		{"server had an error", "the server had an error processing your request", ReasonAgentProviderServerError},
		{"provider returned error", "provider returned error: malformed response", ReasonAgentProviderServerError},
		{"internal error", "An internal error occurred while serving the request", ReasonAgentProviderServerError},
		{"500 with delimiter", "API Error: 500 Internal Server Error", ReasonAgentProviderServerError},
		{"503 anywhere", "got HTTP 503 from provider", ReasonAgentProviderServerError},
		{"503 at start", "503 service degraded", ReasonAgentProviderServerError},
		{"504 at end", "upstream returned 504", ReasonAgentProviderServerError},
		{"service unavailable", "service unavailable, retry later", ReasonAgentProviderServerError},
		{"bad gateway", "Bad Gateway: upstream rejected", ReasonAgentProviderServerError},

		// 7. Provider network.
		{"stream disconnected", "stream disconnected before completion", ReasonAgentProviderNetwork},
		{"connection closed mid-response", "API Error: Connection closed mid-response. The response above may be incomplete.", ReasonAgentProviderNetwork},
		{"connection closed with exit status wins over process failure", "claude exited with error: exit status 1\nAPI Error: Connection closed mid-response.", ReasonAgentProviderNetwork},
		{"error sending request", "error sending request for url (https://api.example.com/v1)", ReasonAgentProviderNetwork},
		{"unable to connect", "unable to connect to provider", ReasonAgentProviderNetwork},
		{"dial tcp", "dial tcp 1.2.3.4:443: connect: connection refused", ReasonAgentProviderNetwork},
		{"connection refused alone", "connection refused", ReasonAgentProviderNetwork},
		{"connectionrefused single", "ConnectionRefused", ReasonAgentProviderNetwork},
		{"dns", "dns lookup failed", ReasonAgentProviderNetwork},
		{"i/o timeout", "read tcp 1.2.3.4:443: i/o timeout", ReasonAgentProviderNetwork},

		// 8. Model not found / unavailable.
		{"model not found", "Error: model claude-3-opus-99 not found", ReasonAgentModelNotFoundOrUnavailable},
		{"model not found phrase", "the model was not found in this account", ReasonAgentModelNotFoundOrUnavailable},
		{"unknown model", "unknown model 'foo-1.0'", ReasonAgentModelNotFoundOrUnavailable},
		{"selected model", "the selected model is no longer supported", ReasonAgentModelNotFoundOrUnavailable},
		{"http 404", "HTTP 404: model endpoint not registered", ReasonAgentModelNotFoundOrUnavailable},
		{"404 page not found", "404 page not found", ReasonAgentModelNotFoundOrUnavailable},

		// 9. Empty / unparseable output.
		{"returned empty output", "openclaw returned empty output", ReasonAgentEmptyOrUnparseableOutput},
		{"returned no parseable output", "kimi returned no parseable output", ReasonAgentEmptyOrUnparseableOutput},

		// 10. Agent timeout.
		{"timed out after", "claude timed out after 2h0m0s", ReasonAgentTimeout},

		// 11. Runtime missing executable.
		{"executable not found", "executable not found in $PATH", ReasonAgentRuntimeMissingExecutable},

		// 12. Runtime version unsupported.
		{"below the minimum supported version", "claude CLI 0.1.0 is below the minimum supported version 0.5.0", ReasonAgentRuntimeVersionUnsupported},
		{"requires a newer version", "this protocol requires a newer version of the runtime", ReasonAgentRuntimeVersionUnsupported},

		// 13. Process failure.
		{"exit status", "agent exit status 137", ReasonAgentProcessFailure},
		{"signal", "agent terminated by signal: killed", ReasonAgentProcessFailure},
		{"panic", "panic: runtime error: invalid memory address", ReasonAgentProcessFailure},
		{"sigsegv", "fatal error: SIGSEGV", ReasonAgentProcessFailure},
		{"process exited", "process exited with status 1", ReasonAgentProcessFailure},
		{"pipe has been ended", "the pipe has been ended", ReasonAgentProcessFailure},
		{"file already closed", "write |1: file already closed", ReasonAgentProcessFailure},
		{"initialize failed", "initialize failed: backend not ready", ReasonAgentProcessFailure},

		// 14. Catchall.
		{"unrecognized", "the agent gave up for reasons unknown", ReasonAgentUnknown},
		{"sentence with no marker", "Hello world.", ReasonAgentUnknown},

		// 15. Digit-boundary regression: 3-digit HTTP status codes must NOT
		//     match when embedded in a longer number. Before the fix these
		//     landed in provider auth/quota/capacity buckets, masking hard
		//     process failures under a provider reason and polluting failure
		//     observability.
		{"402 embedded not quota", "agent consumed 402913 tokens before crashing", ReasonAgentUnknown},
		{"529 embedded not capacity", "request latency was 15290ms; then it panicked: signal killed", ReasonAgentProcessFailure},
		{"403 embedded not auth", "processed 4030 items, then exit status 1", ReasonAgentProcessFailure},
		{"401 embedded not auth", "job 24019 finished, process exited with status 2", ReasonAgentProcessFailure},
		{"429 embedded not capacity", "seq 14290 unknown outcome", ReasonAgentUnknown},
		// Genuine status codes with a boundary still classify correctly.
		{"402 boundary still quota", "API Error: 402 Payment Required", ReasonAgentProviderQuotaLimit},
		{"403 boundary still auth", "HTTP 403 Forbidden", ReasonAgentProviderAuthOrAccess},
		{"429 boundary still capacity", "got 429 from provider", ReasonAgentProviderCapacityOrRateLimit},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.in); got != c.want {
				t.Fatalf("Classify(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestClassifyOrderingPriorities pins the rule precedence between
// overlapping rules. These cases caught regressions during MUL-2946 PR1
// review: the SQL CASE ordering matters and a naive Go switch could
// silently route them differently.
func TestClassifyOrderingPriorities(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want Reason
	}{
		// "token limit" mentions both "context-ish" tokens AND
		// "limit". The context_overflow rule must win because the
		// quota-limit rule's "limit" trigger would otherwise swallow
		// it.
		{"token limit beats quota", "you exceeded the token limit", ReasonAgentContextOverflow},

		// 401 + missing api_key: the missing_config rule runs before
		// auth precisely so we don't classify a config error as an
		// auth rejection.
		{"missing api key beats 401", "missing api_key for openai (401 returned downstream)", ReasonAgentMissingConfig},

		// Both "429" and "rate limit" present — should still land in
		// the capacity bucket, not the quota bucket.
		{"429 rate limit", "API Error: 429 rate limit reached", ReasonAgentProviderCapacityOrRateLimit},

		// "exit status" co-occurring with a stronger upstream marker
		// — the upstream classification should win because the
		// process_failure rule is checked last.
		{"exit status with 401 upstream", "exit status 1: API Error: 401 Unauthorized", ReasonAgentProviderAuthOrAccess},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.in); got != c.want {
				t.Errorf("Classify(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestClassify5xxRegex pins the boundary behavior of the 5xx HTTP
// status detector. The SQL classifier uses an anchored regex
// `(^|[^0-9])5[0-9][0-9]([^0-9]|$)`; this Go classifier mirrors it via
// providerHTTP5xxRe. Without the anchors, "1500ms" and "1.5.0" would
// be misclassified as a server error.
func TestClassify5xxRegex(t *testing.T) {
	t.Parallel()

	hits := []string{
		"503",
		" 504 ",
		"got 502 from upstream",
		"upstream returned 599\n",
	}
	for _, in := range hits {
		if got := Classify(in); got != ReasonAgentProviderServerError {
			t.Errorf("Classify(%q) = %q, want %q", in, got, ReasonAgentProviderServerError)
		}
	}

	misses := []string{
		"1500ms latency observed",
		"version 1.5.0 unsupported",
		"5000 tokens generated",
		"agent slept for 1500 seconds",
	}
	for _, in := range misses {
		if got := Classify(in); got == ReasonAgentProviderServerError {
			t.Errorf("Classify(%q) = %q, want NOT provider_server_error", in, got)
		}
	}
}

// TestClassifyAlwaysReturnsAgentSide guarantees Classify never returns
// a platform-side reason. Platform-side reasons originate from
// sweepers / scheduler / poisoned classifier paths that don't pass
// through Classify; the in-flight classifier's job is exclusively to
// pick among the 14 agent_error.* sub-reasons (or fall back to
// ReasonAgentUnknown). A future change that accidentally returned,
// say, ReasonRuntimeOffline from Classify would break Prometheus
// label semantics — pin it here.
func TestClassifyAlwaysReturnsAgentSide(t *testing.T) {
	t.Parallel()

	samples := []string{
		"",
		"random text",
		"401 Unauthorized",
		"context length exceeded",
		"503 internal server error",
		"timed out after 2h0m0s",
		"exit status 1",
	}
	for _, s := range samples {
		got := Classify(s)
		if !got.IsAgentError() {
			t.Errorf("Classify(%q) = %q, must be agent_error.* (in-flight classifier never returns platform-side reasons)", s, got)
		}
	}
}
