package service

import (
	"log/slog"
	"os"
	"strconv"
)

// Session-resume policy (STOE-29).
//
// A follow-up wakeup on an issue resumes the previous (agent, issue) provider
// session so the agent keeps its working memory across turns. That is the right
// default while the session is small: a resumed turn re-reads a cached prefix
// and pays almost nothing for it. It stops being the right default when the
// session has grown, because a resumed turn then carries the whole prior
// conversation into every request — paying cache-read rates for it, spending
// seconds re-reading it, and eating the context window this turn still needs
// for its own prompt, tool results and answer.
//
// So the question the claim can actually answer before launching anything is
// "how big is the session on offer?". This file answers it as pure arithmetic:
// the caller measures the session (GetSessionResumeMetrics), asks here, and
// records the answer on the run row. Keeping the decision a function of two
// numbers is what makes the threshold testable, tunable per deployment, and
// readable from the run data afterwards.
//
// The other half of the resume decision — whether a session is safe to replay
// at all — is ResumeUnsafeFailure, and it stays there. Size never overrides
// safety: a poisoned session is dropped by that gate before this one is asked.

const (
	// DefaultResumeSessionInputTokenLimit is where a session stops being worth
	// resuming on token size. It is sized against the room a turn needs, not
	// against a model's maximum: a resumed turn arrives with the prior
	// conversation already in context, and still has to fit this turn's own
	// prompt, its tool results and its answer in what is left. At 120k input
	// side tokens a runtime with a 131k window has essentially none of that
	// left — the observed pathology (a 154k-token resume costing 170s per
	// model call, against 1-3s for fresh runs at ~21k) is exactly this state.
	// Below it, continuity is cheap and the resume stands.
	DefaultResumeSessionInputTokenLimit int64 = 120_000

	// DefaultResumeSessionTurnLimit is where a session stops being worth
	// resuming on depth. Token size cannot see it: many small turns still
	// accumulate a transcript the agent has to re-read, still carry stale
	// instructions from turns long finished, and still have to be paid for on
	// every later turn. It is the backstop for sessions that stay individually
	// small and are useless anyway.
	DefaultResumeSessionTurnLimit int64 = 40
)

// The vocabulary written to agent_task_queue.session_resume_decision /
// session_resume_reason. Both columns are free TEXT so a new value needs no
// migration; these are the values this server writes.
const (
	// SessionResumeDecisionResumed: the session was offered and handed over.
	SessionResumeDecisionResumed = "resumed"
	// SessionResumeDecisionFresh: a session was on offer and this run starts
	// without it. The reason column says why.
	SessionResumeDecisionFresh = "fresh"
	// SessionResumeDecisionNone: nothing was on offer — no prior session for
	// this (agent, issue). Not a decision about a session, and recorded so a
	// reader can tell "nothing to resume" apart from "nothing recorded".
	SessionResumeDecisionNone = "none"
)

const (
	// SessionResumeReasonContextLimit: the session's input-side tokens passed
	// the limit.
	SessionResumeReasonContextLimit = "context_limit"
	// SessionResumeReasonTurnLimit: the session's turn count passed the limit.
	SessionResumeReasonTurnLimit = "turn_limit"
	// SessionResumeReasonForcedFresh: the task was dispatched with
	// force_fresh_session set — a retry or rerun that was never going to
	// resume, recorded so it is not mistaken for a policy decision.
	SessionResumeReasonForcedFresh = "forced_fresh"
	// SessionResumeReasonRuntimeMismatch: the session belongs to a run on
	// another runtime, so its rollout is not on this machine to resume from.
	SessionResumeReasonRuntimeMismatch = "runtime_mismatch"
	// SessionResumeReasonNoPriorSession: no resumable session exists for this
	// (agent, issue). Pairs with SessionResumeDecisionNone.
	SessionResumeReasonNoPriorSession = "no_prior_session"
	// SessionResumeReasonMetricsUnavailable: the session could not be
	// measured, so the size gate could not be asked. The resume is still
	// offered — an unreadable metrics query must not silently turn every
	// follow-up into a cold start — but the run says so instead of pretending
	// the gate ran.
	SessionResumeReasonMetricsUnavailable = "metrics_unavailable"
)

// SessionResumeMetrics is what a session's own runs say about its size.
type SessionResumeMetrics struct {
	// InputTokens is the largest input-side total (input + cache read + cache
	// write) any single turn in the session carried — the context a resumed
	// turn has to hold again.
	InputTokens int64
	// Turns is how many runs built the session.
	Turns int64
}

// SessionResumeLimits is the tunable half of the policy. A limit of 0 (or
// negative) disables that axis.
type SessionResumeLimits struct {
	MaxInputTokens int64
	MaxTurns       int64
}

// ResumeSessionLimits reads the deployment's limits from the environment,
// falling back to the defaults. A value that does not parse falls back too,
// with a warning: a typo in a limit should not silently disable the gate.
func ResumeSessionLimits() SessionResumeLimits {
	return SessionResumeLimits{
		MaxInputTokens: resumeSessionLimitEnv("MULTICA_SESSION_RESUME_MAX_INPUT_TOKENS", DefaultResumeSessionInputTokenLimit),
		MaxTurns:       resumeSessionLimitEnv("MULTICA_SESSION_RESUME_MAX_TURNS", DefaultResumeSessionTurnLimit),
	}
}

func resumeSessionLimitEnv(key string, def int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		slog.Warn("session resume policy: ignoring unparseable limit", "env", key, "value", raw, "default", def)
		return def
	}
	if value < 0 {
		return 0
	}
	return value
}

// DecideSessionResume answers the size question: is this session still worth
// handing to a resumed run? It reports the axis that tripped, tokens before
// turns, because the token figure is the one that explains a slow run and a
// context-overflow failure and the turn count alone does not.
//
// A resumed decision carries no reason: the reason column exists to explain an
// unusual outcome, and resuming is the ordinary one.
func DecideSessionResume(m SessionResumeMetrics, limits SessionResumeLimits) (decision, reason string) {
	if limits.MaxInputTokens > 0 && m.InputTokens > limits.MaxInputTokens {
		return SessionResumeDecisionFresh, SessionResumeReasonContextLimit
	}
	if limits.MaxTurns > 0 && m.Turns > limits.MaxTurns {
		return SessionResumeDecisionFresh, SessionResumeReasonTurnLimit
	}
	return SessionResumeDecisionResumed, ""
}
