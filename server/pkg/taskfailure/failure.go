// Package taskfailure is the canonical, refined taxonomy of values written
// into agent_task_queue.failure_reason and chat_message.failure_reason.
//
// History: until MUL-1949, server/daemon code wrote one of a small handful
// of coarse failure_reason values ("agent_error", "timeout",
// "runtime_offline", …). The "agent_error" bucket grew to ~30% of all
// failures and hid the real cause (provider 401, quota exceeded, context
// overflow, runner crash, etc.) inside the free-form `error` text column.
// MUL-1949's offline backfill SQL re-classified those rows into 14
// agent_error.* sub-reasons via a CASE expression on the error text.
//
// This package lifts that classifier into the in-flight write path so the
// stored failure_reason is already refined when the row is first
// persisted, and so server / daemon / cloud share a single source of
// truth for the canonical 21 values. PR1 of the Grafana board plan
// ([MUL-2946](https://multica/issues/MUL-2946)). Subsequent PRs use
// AllReasons() to pre-warm the Prometheus failure_reason label set.
//
// The 21 canonical values fall into two groups:
//
//   - 7 platform-side values (no `agent_error.` prefix) emitted by the
//     server-side sweepers and daemon classifiers when the failure is
//     attributable to the platform/scheduler/runtime layer rather than
//     anything the agent process did:
//
//     queued_expired, runtime_offline, runtime_recovery, timeout,
//     iteration_limit, agent_blocked, api_invalid_request
//
//   - 14 agent-side values (with `agent_error.` prefix) produced by
//     Classify(rawError) when the agent process surfaced an error string.
//     IsAgentError reports membership in this set.
//
// Wire stability: the string forms of these constants are persisted into
// the database and surfaced as Prometheus labels. Renaming a value is a
// breaking change. New values may be added — but only after the SQL
// classifier in MUL-1949 grows a matching rule and a backfill migration
// re-classifies historical rows.
package taskfailure

import "strings"

// Reason is a string-backed enum of the canonical failure_reason values
// stored in agent_task_queue.failure_reason. Use the Reason* constants
// rather than string literals so the compiler catches typos and a future
// taxonomy change can be made package-wide.
type Reason string

// agentErrorPrefix marks the 14 sub-reasons that originate inside the
// agent process (provider error, runner crash, context overflow, etc.)
// as opposed to the 7 platform-side reasons (queue expiry, runtime
// offline, sweeper timeout, etc.). IsAgentError uses this prefix so
// callers don't have to enumerate the agent-side reasons by hand.
const agentErrorPrefix = "agent_error."

const (
	// Platform / scheduler side: failure attributable to Multica
	// infrastructure rather than anything the agent process did. These
	// are emitted by server-side sweepers (ExpireStaleQueuedTasks,
	// FailStaleTasks, FailTasksForOfflineRuntimes,
	// RecoverOrphanedTasksForRuntime) and the daemon's poisoned-session
	// classifier (api_invalid_request, iteration_limit, agent_blocked).
	// IsAgentError returns false for all of these.

	// ReasonQueuedExpired: task sat in 'queued' past the TTL without
	// being claimed (typically autopilot backlog while the assignee's
	// runtime is offline). Written by ExpireStaleQueuedTasks.
	ReasonQueuedExpired Reason = "queued_expired"

	// ReasonRuntimeOffline: the runtime owning a dispatched/running
	// task went offline. Written by FailTasksForOfflineRuntimes.
	ReasonRuntimeOffline Reason = "runtime_offline"

	// ReasonRuntimeRecovery: the daemon restarted while the task was
	// in flight; the prior session is unrecoverable. Written by
	// RecoverOrphanedTasksForRuntime at daemon startup.
	ReasonRuntimeRecovery Reason = "runtime_recovery"

	// ReasonTimeout: server-side or runtime-side hard timeout.
	// Written by FailStaleTasks (server) and the daemon's per-task
	// agent timeout path.
	ReasonTimeout Reason = "timeout"

	// ReasonIterationLimit: the agent reached its per-run iteration
	// cap and emitted a fallback "I reached the iteration limit"
	// message. Treated as platform-side because it is a Multica-imposed
	// budget rather than an external API rejection.
	ReasonIterationLimit Reason = "iteration_limit"

	// ReasonAgentBlocked: the agent intentionally entered the
	// 'blocked' workflow state (e.g. requesting human input). Not a
	// system error.
	ReasonAgentBlocked Reason = "agent_blocked"

	// ReasonAPIInvalidRequest: the upstream LLM API rejected the
	// request body with a 400 invalid_request_error (oversized image,
	// malformed payload, etc.). The conversation history itself is
	// poisoned, so the next task on the same session would replay the
	// same 400 — GetLastTaskSession excludes this reason from the
	// resume lookup. Written by classifyPoisonedError in daemon/poisoned.go.
	ReasonAPIInvalidRequest Reason = "api_invalid_request"

	// Agent process side: failure surfaced by the agent CLI / SDK as
	// an error string. Classify(rawError) is responsible for picking
	// the right sub-reason from the string. IsAgentError returns true
	// for all of these.

	// ReasonAgentProviderAuthOrAccess: 401 / 403, "Not logged in",
	// invalid API key, no access to the model. Not retryable; user
	// must re-auth.
	ReasonAgentProviderAuthOrAccess Reason = "agent_error.provider_auth_or_access"

	// ReasonAgentProviderQuotaLimit: 402, insufficient_balance,
	// monthly usage limit, credits exhausted. Not retryable until the
	// account is topped up.
	ReasonAgentProviderQuotaLimit Reason = "agent_error.provider_quota_limit"

	// ReasonAgentProviderCapacityOrRateLimit: 429 / 529, rate-limited,
	// overloaded, no capacity available. Transient — backoff +
	// retry is appropriate.
	ReasonAgentProviderCapacityOrRateLimit Reason = "agent_error.provider_capacity_or_rate_limit"

	// ReasonAgentProviderServerError: provider 5xx, internal error,
	// service unavailable, bad gateway. Transient — short backoff.
	ReasonAgentProviderServerError Reason = "agent_error.provider_server_error"

	// ReasonAgentProviderNetwork: stream disconnected, dial tcp
	// failures, DNS failures, i/o timeout. Transient.
	ReasonAgentProviderNetwork Reason = "agent_error.provider_network"

	// ReasonAgentProcessFailure: agent subprocess exited non-zero,
	// crashed, or returned an unexpected signal. Runner / backend
	// quality issue.
	ReasonAgentProcessFailure Reason = "agent_error.process_failure"

	// ReasonAgentEmptyOrUnparseableOutput: the agent CLI returned
	// empty output or output we couldn't parse against its known
	// protocol. Wrapper / protocol robustness issue.
	ReasonAgentEmptyOrUnparseableOutput Reason = "agent_error.empty_or_unparseable_output"

	// ReasonAgentTimeout: the agent subprocess hit its hard timeout
	// (e.g. 2h) — distinct from ReasonTimeout, which is a
	// platform-side sweeper timeout.
	ReasonAgentTimeout Reason = "agent_error.agent_timeout"

	// ReasonAgentContextOverflow: prompt or context window exceeded
	// the model's limit. Not retryable on the same session; needs
	// compaction or a fresh session.
	ReasonAgentContextOverflow Reason = "agent_error.context_overflow"

	// ReasonAgentMissingConfig: the agent / runtime is missing a
	// required environment variable or API key, or no LLM provider
	// is configured.
	ReasonAgentMissingConfig Reason = "agent_error.missing_config"

	// ReasonAgentModelNotFoundOrUnavailable: the chosen model id
	// doesn't exist, isn't accessible to this account, or returned
	// HTTP 404 from the provider.
	ReasonAgentModelNotFoundOrUnavailable Reason = "agent_error.model_not_found_or_unavailable"

	// ReasonAgentRuntimeVersionUnsupported: the local runner CLI is
	// below the minimum supported version or the protocol isn't
	// compatible.
	ReasonAgentRuntimeVersionUnsupported Reason = "agent_error.runtime_version_unsupported"

	// ReasonAgentRuntimeMissingExecutable: the runner CLI binary
	// isn't installed / not on PATH.
	ReasonAgentRuntimeMissingExecutable Reason = "agent_error.runtime_missing_executable"

	// ReasonAgentUnknown: the classifier couldn't match any rule.
	// This bucket is expected to be small (<5% of failed tasks) — a
	// rising share is a signal that the classifier needs new rules.
	// Returned by Classify when no other rule fires (including for
	// empty input).
	ReasonAgentUnknown Reason = "agent_error.unknown"
)

// allReasons is the canonical ordered list of the 21 reasons. Order is
// stable so callers (e.g. Prometheus collectors that pre-warm series via
// AllReasons) can build deterministic label sets across restarts.
//
// Ordering:
//  1. Platform-side reasons in the same order they tend to fire in a
//     task lifecycle (queue → dispatch → run → post-run).
//  2. Agent-side reasons grouped by responsibility area (provider /
//     agent process / config / runtime), then unknown last.
var allReasons = []Reason{
	// Platform / scheduler side.
	ReasonQueuedExpired,
	ReasonRuntimeOffline,
	ReasonRuntimeRecovery,
	ReasonTimeout,
	ReasonIterationLimit,
	ReasonAgentBlocked,
	ReasonAPIInvalidRequest,

	// Agent process side: provider errors.
	ReasonAgentProviderAuthOrAccess,
	ReasonAgentProviderQuotaLimit,
	ReasonAgentProviderCapacityOrRateLimit,
	ReasonAgentProviderServerError,
	ReasonAgentProviderNetwork,

	// Agent process side: agent / runner errors.
	ReasonAgentProcessFailure,
	ReasonAgentEmptyOrUnparseableOutput,
	ReasonAgentTimeout,
	ReasonAgentContextOverflow,
	ReasonAgentMissingConfig,
	ReasonAgentModelNotFoundOrUnavailable,
	ReasonAgentRuntimeVersionUnsupported,
	ReasonAgentRuntimeMissingExecutable,

	// Catchall.
	ReasonAgentUnknown,
}

// String returns the wire form of the reason — what gets written to the
// failure_reason column and exposed as a Prometheus label value.
func (r Reason) String() string { return string(r) }

// IsAgentError reports whether the reason originates inside the agent
// process (provider error, runner crash, context overflow, etc.) as
// opposed to the platform/scheduler/runtime layer (queue expiry, runtime
// offline, sweeper timeout, etc.).
//
// The classification is intentionally based on a string prefix rather
// than an enum membership test: any future agent_error.* value
// automatically inherits the correct grouping without needing to update
// this method.
func (r Reason) IsAgentError() bool {
	return strings.HasPrefix(string(r), agentErrorPrefix)
}

// AllReasons returns the canonical 21 reasons in a stable order. The
// caller MUST NOT mutate the returned slice; a copy is returned so
// concurrent callers can append to their local copy without corrupting
// the package-level fixture.
//
// Primary use: Prometheus failure_reason label pre-warming, so a label
// the production process has not seen yet is still observable as a
// zero-valued series instead of appearing only after the first failure
// of that kind.
func AllReasons() []Reason {
	out := make([]Reason, len(allReasons))
	copy(out, allReasons)
	return out
}
