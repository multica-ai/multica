package providerfailover

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// baseEligible returns an active-mode Input that would proceed: a GPT run that
// hit a rate limit, no side effects, no exclusions, Claude available. Individual
// tests mutate one field to assert the corresponding gate.
func baseEligible() Input {
	return Input{
		FailureReason:       taskfailure.ReasonAgentProviderCapacityOrRateLimit,
		SourceProvider:      "codex",
		Mode:                ModeActive,
		SideEffectsComplete: true,
		ClaudeAvailable:     true,
	}
}

func TestIsFailoverTrigger(t *testing.T) {
	t.Parallel()
	triggers := []taskfailure.Reason{
		taskfailure.ReasonAgentProviderCapacityOrRateLimit,
		taskfailure.ReasonAgentProviderQuotaLimit,
		// td-836aa9 liveness watchdog: a silent hang is now a trigger too.
		taskfailure.ReasonProviderLivenessTimeout,
	}
	for _, r := range triggers {
		if !IsFailoverTrigger(r) {
			t.Errorf("IsFailoverTrigger(%q) = false, want true", r)
		}
	}
	// Every other canonical reason must be excluded — this pins the exact
	// classification and catches a taxonomy change silently widening triggers.
	// In particular plain ReasonTimeout / ReasonAgentTimeout stay EXCLUDED: a
	// busy run that hit a hard cap must not fail over, only a wedged one.
	for _, r := range taskfailure.AllReasons() {
		want := r == taskfailure.ReasonAgentProviderCapacityOrRateLimit ||
			r == taskfailure.ReasonAgentProviderQuotaLimit ||
			r == taskfailure.ReasonProviderLivenessTimeout
		if got := IsFailoverTrigger(r); got != want {
			t.Errorf("IsFailoverTrigger(%q) = %v, want %v", r, got, want)
		}
	}
	if IsFailoverTrigger(taskfailure.ReasonTimeout) {
		t.Error("plain timeout (busy run hit a cap) must NOT be a trigger")
	}
	if IsFailoverTrigger(taskfailure.Reason("")) {
		t.Error("empty reason must not be a trigger (fail-closed)")
	}
}

// AC1: 429 / rate-limit with no side effects elects failover.
func TestDecide_RateLimit_Proceeds(t *testing.T) {
	t.Parallel()
	d := Decide(baseEligible())
	if d.Outcome != OutcomeProceed || d.State != StatePending || !d.WouldFailOver {
		t.Fatalf("rate-limit should proceed to HANDOFF_PENDING, got %+v", d)
	}
}

// AC2: usage/quota limit elects failover.
func TestDecide_QuotaLimit_Proceeds(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.FailureReason = taskfailure.ReasonAgentProviderQuotaLimit
	d := Decide(in)
	if d.Outcome != OutcomeProceed || !d.WouldFailOver {
		t.Fatalf("quota limit should proceed, got %+v", d)
	}
}

// AC3/AC4: timeout and auth are not failover triggers.
func TestDecide_NonTriggerReasons_Decline(t *testing.T) {
	t.Parallel()
	nonTriggers := []taskfailure.Reason{
		taskfailure.ReasonAgentTimeout,
		taskfailure.ReasonTimeout,
		taskfailure.ReasonAgentProviderAuthOrAccess,
		taskfailure.ReasonAgentProviderNetwork,
		taskfailure.ReasonAgentProviderServerError,
		taskfailure.ReasonAgentContextOverflow,
		taskfailure.ReasonAgentProcessFailure,
		taskfailure.ReasonAgentUnknown,
	}
	for _, r := range nonTriggers {
		in := baseEligible()
		in.FailureReason = r
		d := Decide(in)
		if d.Outcome != OutcomeDeclined || d.WouldFailOver {
			t.Errorf("reason %q should decline, got %+v", r, d)
		}
		if d.Reason != ReasonNotTrigger {
			t.Errorf("reason %q decline code = %q, want %q", r, d.Reason, ReasonNotTrigger)
		}
	}
}

// AC5: any observable side effect blocks failover.
func TestDecide_SideEffects_Decline(t *testing.T) {
	t.Parallel()
	cases := map[string]SideEffects{
		"tool_calls":        {ObservedToolCalls: 1},
		"delivered_comment": {DeliveredCommentIDs: 1},
		"agent_commented":   {AgentCommented: true},
		"head_advanced":     {HeadSHAAdvanced: true},
		"partial_output":    {PartialOutput: true},
	}
	for name, se := range cases {
		in := baseEligible()
		in.SideEffects = se
		d := Decide(in)
		if d.Outcome != OutcomeDeclined || d.Reason != ReasonSideEffects {
			t.Errorf("%s: expected side-effect decline, got %+v", name, d)
		}
	}
	// Zero side effects must NOT block.
	if !Decide(baseEligible()).WouldFailOver {
		t.Error("empty side effects must not block failover")
	}
}

// AC6: cancellation declines.
func TestDecide_Cancelled_Decline(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.Cancelled = true
	d := Decide(in)
	if d.Outcome != OutcomeDeclined || d.Reason != ReasonCancelled {
		t.Fatalf("cancelled run should decline, got %+v", d)
	}
}

// AC8: at-most-one handoff per chain + loop prevention.
func TestDecide_MaxOnePerChain(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.ChainHasOwningHandoff = true
	d := Decide(in)
	if d.Outcome != OutcomeDeclined || d.Reason != ReasonMaxOnePerChain {
		t.Fatalf("second handoff on a chain should decline, got %+v", d)
	}
}

func TestDecide_LoopPrevention_FallbackNeverFailsOverAgain(t *testing.T) {
	t.Parallel()
	// A Claude fallback that itself hits a limit must not spawn another handoff.
	in := baseEligible()
	in.IsAlreadyFallback = true
	d := Decide(in)
	if d.Outcome != OutcomeDeclined || d.Reason != ReasonAlreadyFallback {
		t.Fatalf("fallback task should not fail over again, got %+v", d)
	}
}

// #1 (bidirectional, td-836aa9): the failover source set is now a closed,
// BIDIRECTIONAL whitelist of the two in-scope coding runtimes — codex AND
// claude are both valid sources — while every other CLI (grok/gemini/kimi/…)
// and the billing strings stay ineligible. The policy is still a positive
// whitelist, not "anything that isn't claude".
func TestDecide_SourceProviderWhitelist(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"codex", "claude"} {
		if !IsFailoverSource(src) {
			t.Errorf("%q must be a failover source (bidirectional)", src)
		}
	}
	ineligibleSources := []string{
		"grok",
		"gemini",
		"kimi",
		"cursor",
		"codebuddy",
		"openai", // billing provider string, never a runtime provider
		"gpt",
		"anthropic",
		"",
	}
	for _, src := range ineligibleSources {
		if IsFailoverSource(src) {
			t.Errorf("IsFailoverSource(%q) = true, want false", src)
		}
		in := baseEligible()
		in.SourceProvider = src
		d := Decide(in)
		if d.Outcome != OutcomeDeclined || d.Reason != ReasonSourceIneligible {
			t.Errorf("source %q should decline as ineligible, got %+v", src, d)
		}
	}
}

// #1b (bidirectional): a Claude run that hits a limit hands off to codex — the
// direction that matters when the Claude Max window ends. Loop-back is still
// prevented (a fallback never fails over again, at-most-one-per-chain), which
// the loop-prevention test covers independent of direction.
func TestDecide_Bidirectional_ClaudeToCodex(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.SourceProvider = "claude"
	d := Decide(in)
	if d.Outcome != OutcomeProceed || !d.WouldFailOver {
		t.Fatalf("claude source should proceed to a handoff, got %+v", d)
	}
	if d.TargetProvider != "codex" {
		t.Fatalf("claude should hand off to codex, got target %q", d.TargetProvider)
	}
}

// The direction relation is exactly the bidirectional pair; targets are always a
// different, in-scope family and never self.
func TestFailoverTargets(t *testing.T) {
	t.Parallel()
	if got := PrimaryTargetFor("codex"); got != "claude" {
		t.Errorf("PrimaryTargetFor(codex) = %q, want claude", got)
	}
	if got := PrimaryTargetFor("claude"); got != "codex" {
		t.Errorf("PrimaryTargetFor(claude) = %q, want codex", got)
	}
	if got := PrimaryTargetFor("grok"); got != "" {
		t.Errorf("PrimaryTargetFor(grok) = %q, want empty", got)
	}
	for _, src := range []string{"codex", "claude"} {
		for _, tgt := range FailoverTargets(src) {
			if tgt == src {
				t.Errorf("FailoverTargets(%q) contains self", src)
			}
			if !IsFailoverSource(tgt) {
				t.Errorf("FailoverTargets(%q) target %q must itself be in-scope", src, tgt)
			}
		}
	}
}

// #2: active mode must not hand off unless the run's side-effect surface is
// PROVEN empty. An otherwise-eligible run with unproven completeness declines in
// active mode but is still recorded as would-fail-over in shadow.
func TestDecide_SideEffectsUnproven_ActiveHoldsClosed(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.SideEffectsComplete = false
	d := Decide(in)
	if d.Outcome != OutcomeDeclined || d.State != StateDeclined {
		t.Fatalf("unproven completeness should decline in active, got %+v", d)
	}
	if d.WouldFailOver || d.Reason != ReasonSideEffectsUnproven {
		t.Fatalf("active decline should be side_effect_completeness_unproven, got %+v", d)
	}

	// Shadow evaluates the observable subset regardless of completeness.
	in.Mode = ModeShadow
	ds := Decide(in)
	if ds.Outcome != OutcomeShadow || !ds.WouldFailOver || ds.Reason != ReasonEligible {
		t.Fatalf("shadow should still record would-fail-over on observable eligibility, got %+v", ds)
	}
}

// AC11: structural exclusion for authority-sensitive agents.
func TestDecide_AuthoritySensitive_Decline(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.AuthoritySensitive = true
	d := Decide(in)
	if d.Outcome != OutcomeDeclined || d.Reason != ReasonAuthoritySensitive {
		t.Fatalf("authority-sensitive agent should decline, got %+v", d)
	}
}

// AC9: Claude unavailable → explicit FAILED in active mode.
func TestDecide_ClaudeUnavailable_ActiveFails(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.ClaudeAvailable = false
	d := Decide(in)
	if d.Outcome != OutcomeUnavailable || d.State != StateFailed || !d.WouldFailOver {
		t.Fatalf("unavailable Claude should yield HANDOFF_FAILED, got %+v", d)
	}
	if d.Reason != ReasonClaudeUnavailable {
		t.Fatalf("reason = %q, want %q", d.Reason, ReasonClaudeUnavailable)
	}
}

// AC10: shadow mode never acts, always records the full verdict.
func TestDecide_ShadowMode_RecordsButNeverActs(t *testing.T) {
	t.Parallel()

	// Eligible run in shadow: records "would fail over", state SHADOW.
	in := baseEligible()
	in.Mode = ModeShadow
	d := Decide(in)
	if d.Outcome != OutcomeShadow || d.State != StateShadow || !d.WouldFailOver {
		t.Fatalf("shadow eligible: got %+v", d)
	}
	if d.Reason != ReasonEligible {
		t.Fatalf("shadow eligible reason = %q, want %q", d.Reason, ReasonEligible)
	}

	// Ineligible run in shadow: records the decline reason, still SHADOW/no-act.
	in2 := baseEligible()
	in2.Mode = ModeShadow
	in2.SideEffects = SideEffects{ObservedToolCalls: 3}
	d2 := Decide(in2)
	if d2.Outcome != OutcomeShadow || d2.State != StateShadow {
		t.Fatalf("shadow ineligible outcome/state: got %+v", d2)
	}
	if d2.WouldFailOver || d2.Reason != ReasonSideEffects {
		t.Fatalf("shadow ineligible should record WouldFailOver=false + reason, got %+v", d2)
	}

	// Shadow records "would fail over" even when Claude is down — availability
	// is an active-mode operational concern, not an eligibility one.
	in3 := baseEligible()
	in3.Mode = ModeShadow
	in3.ClaudeAvailable = false
	d3 := Decide(in3)
	if !d3.WouldFailOver || d3.Outcome != OutcomeShadow {
		t.Fatalf("shadow with Claude down should still record would-fail-over, got %+v", d3)
	}
}

// Ordering: a non-trigger reason wins over a side-effect gate (both would
// decline, but the reason must reflect the earliest gate for auditability).
func TestDecide_DeclineOrdering(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.FailureReason = taskfailure.ReasonAgentProviderAuthOrAccess
	in.SideEffects = SideEffects{ObservedToolCalls: 5}
	in.AuthoritySensitive = true
	d := Decide(in)
	if d.Reason != ReasonNotTrigger {
		t.Fatalf("earliest gate (not-trigger) should win, got %q", d.Reason)
	}
}
