package service

import "testing"

// The GLM gateway rejection from GH #6143. It matches none of the poisoned-
// session text patterns, so a current daemon still files it as unknown — which
// is exactly the case the breaker exists to bound.
const gh6143GatewayError = "agent_error.unknown"

// TestEvaluateResumeExhaustionThreshold covers the core of GH #6143: an error
// nobody recognises must cost one wasted run, not a permanently dead
// (agent, issue) pair.
//
// The first failure deliberately still resumes. An unrecognised error is not
// yet evidence that the transcript is at fault, and starting fresh on every
// first stumble would throw away conversation context whenever a provider
// hiccupped in a way the classifier has not seen.
func TestEvaluateResumeExhaustionThreshold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		currentReason string
		sessionID     string
		prior         []priorResumeFailure
		wantRetire    bool
		wantSessionID string
	}{
		{
			name:          "first unrecognised failure still resumes",
			currentReason: gh6143GatewayError,
			sessionID:     "sess-a",
			prior:         nil,
			wantRetire:    false,
		},
		{
			name:          "second consecutive failure retires the conversation",
			currentReason: gh6143GatewayError,
			sessionID:     "sess-a",
			prior:         []priorResumeFailure{{SessionID: "sess-a", FailureReason: gh6143GatewayError}},
			wantRetire:    true,
			wantSessionID: "sess-a",
		},
		{
			// The window resets on success, so these two failures are not
			// consecutive — ListResumeFailuresSinceLastSuccess would not
			// return the pre-success row at all.
			name:          "a success in between clears the window",
			currentReason: gh6143GatewayError,
			sessionID:     "sess-b",
			prior:         nil,
			wantRetire:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			retire, sessionID := evaluateResumeExhaustion(tc.currentReason, tc.sessionID, tc.prior)
			if retire != tc.wantRetire {
				t.Fatalf("retire = %v, want %v", retire, tc.wantRetire)
			}
			if retire && sessionID != tc.wantSessionID {
				t.Fatalf("retired session = %q, want %q", sessionID, tc.wantSessionID)
			}
		})
	}
}

// TestEvaluateResumeExhaustionTransient is the false-positive guard. Discarding
// a healthy session costs real conversation context, which is why the GH #6143
// review rejected the reporter's "any 400 is poison" patch — under it a single
// rate-limit whose body mentions 400ms would have burned the session.
func TestEvaluateResumeExhaustionTransient(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		currentReason string
		prior         []priorResumeFailure
		wantRetire    bool
	}{
		{
			name:          "repeated daemon outages never retire",
			currentReason: "runtime_offline",
			prior: []priorResumeFailure{
				{SessionID: "sess-a", FailureReason: "runtime_offline"},
				{SessionID: "sess-a", FailureReason: "runtime_offline"},
			},
			wantRetire: false,
		},
		{
			// Not auto-retried, but unambiguously transient: two rate-limited
			// runs say nothing about whether the transcript can be replayed.
			name:          "repeated rate limits never retire",
			currentReason: "agent_error.provider_capacity_or_rate_limit",
			prior: []priorResumeFailure{
				{SessionID: "sess-a", FailureReason: "agent_error.provider_capacity_or_rate_limit"},
			},
			wantRetire: false,
		},
		{
			name:          "provider 5xx never retires",
			currentReason: "agent_error.provider_server_error",
			prior: []priorResumeFailure{
				{SessionID: "sess-a", FailureReason: "agent_error.provider_server_error"},
			},
			wantRetire: false,
		},
		{
			// The interleaving case. A transient blip between two real
			// failures is skipped, NOT treated as a reset: letting a flaky
			// daemon clear the count would hide a genuinely dead session
			// forever.
			name:          "a transient blip between two real failures does not reset the count",
			currentReason: gh6143GatewayError,
			prior: []priorResumeFailure{
				{SessionID: "sess-a", FailureReason: gh6143GatewayError},
				{SessionID: "sess-a", FailureReason: "runtime_offline"},
			},
			wantRetire: true,
		},
		{
			// One real failure plus noise is still only one real failure.
			name:          "transient noise alone cannot reach the threshold",
			currentReason: gh6143GatewayError,
			prior: []priorResumeFailure{
				{SessionID: "sess-a", FailureReason: "runtime_offline"},
				{SessionID: "sess-a", FailureReason: "timeout"},
				{SessionID: "sess-a", FailureReason: "queued_expired"},
			},
			wantRetire: false,
		},
		{
			// The regression the PR review caught. An expired key fails every
			// run until the user re-auths, so without this the second failure
			// would retire a healthy session AND relabel the failure — hiding
			// "Auth failed", the one thing the user could act on.
			name:          "expired credentials never retire",
			currentReason: "agent_error.provider_auth_or_access",
			prior: []priorResumeFailure{
				{SessionID: "sess-a", FailureReason: "agent_error.provider_auth_or_access"},
			},
			wantRetire: false,
		},
		{
			name:          "exhausted billing never retires",
			currentReason: "agent_error.provider_quota_limit",
			prior: []priorResumeFailure{
				{SessionID: "sess-a", FailureReason: "agent_error.provider_quota_limit"},
			},
			wantRetire: false,
		},
		{
			name:          "misconfigured model never retires",
			currentReason: "agent_error.model_not_found_or_unavailable",
			prior: []priorResumeFailure{
				{SessionID: "sess-a", FailureReason: "agent_error.model_not_found_or_unavailable"},
			},
			wantRetire: false,
		},
		{
			name:          "missing runner binary never retires",
			currentReason: "agent_error.runtime_missing_executable",
			prior: []priorResumeFailure{
				{SessionID: "sess-a", FailureReason: "agent_error.missing_config"},
				{SessionID: "sess-a", FailureReason: "agent_error.runtime_version_unsupported"},
			},
			wantRetire: false,
		},
		{
			// Counting these IS intended: replaying an oversized or malformed
			// history is a plausible cause, so the fail-safe direction is to
			// treat them as evidence about the transcript.
			name:          "agent process crashes still count",
			currentReason: "agent_error.process_failure",
			prior: []priorResumeFailure{
				{SessionID: "sess-a", FailureReason: "agent_error.process_failure"},
			},
			wantRetire: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			retire, _ := evaluateResumeExhaustion(tc.currentReason, "sess-a", tc.prior)
			if retire != tc.wantRetire {
				t.Fatalf("retire = %v, want %v", retire, tc.wantRetire)
			}
		})
	}
}

// TestEvaluateResumeExhaustionSessionDrift covers the ACP backends, whose
// resolveResumedSessionID may answer a resume with a DIFFERENT session id when
// the provider's own state is gone.
//
// The upgraded failure_reason only excludes the row it is written on, so under
// drift the EARLIER id would stay eligible and be resumed on the next turn —
// the loop would survive the fix. Retiring the earlier id is what closes that.
func TestEvaluateResumeExhaustionSessionDrift(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		sessionID     string
		prior         []priorResumeFailure
		wantSessionID string
	}{
		{
			name:          "stable id retires itself",
			sessionID:     "sess-a",
			prior:         []priorResumeFailure{{SessionID: "sess-a", FailureReason: gh6143GatewayError}},
			wantSessionID: "sess-a",
		},
		{
			// The current id is already covered by the reason written on this
			// row; the drifted predecessor is the one that needs retiring.
			name:          "drifted id retires the predecessor",
			sessionID:     "sess-b",
			prior:         []priorResumeFailure{{SessionID: "sess-a", FailureReason: gh6143GatewayError}},
			wantSessionID: "sess-a",
		},
		{
			// A run that died before establishing a session records no id.
			name:          "empty prior id falls back to the current session",
			sessionID:     "sess-b",
			prior:         []priorResumeFailure{{SessionID: "", FailureReason: gh6143GatewayError}},
			wantSessionID: "sess-b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			retire, sessionID := evaluateResumeExhaustion(gh6143GatewayError, tc.sessionID, tc.prior)
			if !retire {
				t.Fatalf("retire = false, want true")
			}
			if sessionID != tc.wantSessionID {
				t.Fatalf("retired session = %q, want %q", sessionID, tc.wantSessionID)
			}
		})
	}
}

// TestSessionResumeExhaustedIsResumeUnsafe pins the wiring that makes the
// breaker reach the manual-rerun path. That path reads the exact source task
// through ResumeUnsafeFailure and never consults the retired_sessions CTE, so
// without this the Rerun button would keep resuming a retired conversation —
// and Rerun is the first thing a user reaches for.
func TestSessionResumeExhaustedIsResumeUnsafe(t *testing.T) {
	t.Parallel()

	if !ResumeUnsafeFailure("session_resume_exhausted", "") {
		t.Fatal("session_resume_exhausted must be resume-unsafe so manual rerun starts fresh")
	}
	if !resumeUnsafeFailureReason("session_resume_exhausted") {
		t.Fatal("session_resume_exhausted missing from the resume-unsafe reason set")
	}
}

// TestTranscriptNeutralSupersetOfRetryable pins the superset relationship. If a
// reason is safe enough to auto-retry it is safe enough not to burn the
// session, and a future addition to retryableReasons must not silently start
// tripping the breaker.
func TestTranscriptNeutralSupersetOfRetryable(t *testing.T) {
	t.Parallel()

	for reason := range retryableReasons {
		if !transcriptNeutralReasons[reason] {
			t.Errorf("retryable reason %q is not in transcriptNeutralReasons", reason)
		}
	}
	// The members that are NOT auto-retryable are the whole reason this set
	// exists separately; losing any of them reintroduces a false positive that
	// costs the user their conversation context.
	for _, reason := range []string{
		// Transient, but with no safe idempotent replay.
		"agent_error.provider_capacity_or_rate_limit",
		"agent_error.provider_server_error",
		"queued_expired",
		// Not transient at all — they persist until the user fixes the
		// environment — but equally silent about whether the transcript
		// replays, which is the actual membership test for this set.
		"agent_error.provider_auth_or_access",
		"agent_error.provider_quota_limit",
		"agent_error.model_not_found_or_unavailable",
		"agent_error.missing_config",
		"agent_error.runtime_version_unsupported",
		"agent_error.runtime_missing_executable",
	} {
		if !transcriptNeutralReasons[reason] {
			t.Errorf("transcript-neutral reason %q must not count toward the breaker", reason)
		}
	}
	// The counting side of the contract. These can plausibly be CAUSED by
	// replaying a bad history, so excluding them would blind the breaker.
	for _, reason := range []string{
		"agent_error.unknown",
		"agent_error.process_failure",
		"agent_error.empty_or_unparseable_output",
		"agent_error.agent_timeout",
	} {
		if transcriptNeutralReasons[reason] {
			t.Errorf("reason %q must keep counting toward the breaker", reason)
		}
	}
}

// TestEvaluateResumeExhaustionPrefersMostRecentDriftedID pins the ordering
// contract with ListResumeFailuresSinceLastSuccess, which returns newest-first.
// Only one id fits in retired_session_id, so when several drifted predecessors
// exist the one worth retiring is the session this run was actually told to
// resume — the most recent.
func TestEvaluateResumeExhaustionPrefersMostRecentDriftedID(t *testing.T) {
	t.Parallel()

	retire, sessionID := evaluateResumeExhaustion(gh6143GatewayError, "sess-c", []priorResumeFailure{
		{SessionID: "sess-b", FailureReason: gh6143GatewayError},
		{SessionID: "sess-a", FailureReason: gh6143GatewayError},
	})
	if !retire {
		t.Fatal("retire = false, want true")
	}
	if sessionID != "sess-b" {
		t.Fatalf("retired session = %q, want %q (the most recent drifted id)", sessionID, "sess-b")
	}
}
