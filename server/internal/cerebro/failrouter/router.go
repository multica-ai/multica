// Package failrouter is the single policy table for task failure handling.
package failrouter

import "github.com/multica-ai/multica/server/pkg/taskfailure"

type Action string

const (
	ActionRetry   Action = "retry"
	ActionPause   Action = "pause"
	ActionAlert   Action = "alert"
	ActionSurface Action = "surface"
)

type Route struct {
	Action       Action
	FreshSession bool
	// RetryLimit is the number of automatic retries allowed for this reason.
	// Zero means the task's configured max_attempts remains authoritative.
	RetryLimit int32
}

var routes = map[string]Route{
	taskfailure.ReasonQueuedExpired.String():                    {Action: ActionSurface},
	taskfailure.ReasonRuntimeOffline.String():                   {Action: ActionRetry},
	taskfailure.ReasonRuntimeRecovery.String():                  {Action: ActionRetry},
	taskfailure.ReasonTimeout.String():                          {Action: ActionRetry},
	taskfailure.ReasonIterationLimit.String():                   {Action: ActionSurface},
	taskfailure.ReasonAgentBlocked.String():                     {Action: ActionSurface},
	taskfailure.ReasonAPIInvalidRequest.String():                {Action: ActionSurface},
	taskfailure.ReasonAgentProviderAuthOrAccess.String():        {Action: ActionPause},
	taskfailure.ReasonAgentProviderQuotaLimit.String():          {Action: ActionPause},
	taskfailure.ReasonAgentProviderCapacityOrRateLimit.String(): {Action: ActionPause},
	taskfailure.ReasonAgentProviderServerError.String():         {Action: ActionRetry},
	taskfailure.ReasonAgentProviderNetwork.String():             {Action: ActionRetry},
	taskfailure.ReasonAgentProcessFailure.String():              {Action: ActionRetry, RetryLimit: 1},
	taskfailure.ReasonAgentEmptyOrUnparseableOutput.String():    {Action: ActionRetry, FreshSession: true, RetryLimit: 1},
	taskfailure.ReasonAgentTimeout.String():                     {Action: ActionSurface},
	taskfailure.ReasonAgentContextOverflow.String():             {Action: ActionRetry, FreshSession: true, RetryLimit: 1},
	taskfailure.ReasonAgentMissingConfig.String():               {Action: ActionSurface},
	taskfailure.ReasonAgentModelNotFoundOrUnavailable.String():  {Action: ActionSurface},
	taskfailure.ReasonAgentRuntimeVersionUnsupported.String():   {Action: ActionAlert},
	taskfailure.ReasonAgentRuntimeMissingExecutable.String():    {Action: ActionAlert},
	taskfailure.ReasonAgentUnknown.String():                     {Action: ActionSurface},
	"codex_semantic_inactivity":                                 {Action: ActionRetry, FreshSession: true},
	// FIR-3651: the daemon's idle watchdog force-stops a run whose agent
	// backend went silent — typically blocked on a tool call against a frozen
	// child process (see daemon.go's "idle_watchdog" case). That is the same
	// shape as codex_semantic_inactivity and deserves the same treatment: the
	// old session is wedged, so retry on a fresh one. It had no entry at all,
	// which means Lookup missed and FailTask fell back to Surface — 31 runs
	// died on a stuck subprocess with no second attempt. Bounded to one retry
	// so a persistently frozen environment cannot loop.
	"idle_watchdog": {Action: ActionRetry, FreshSession: true, RetryLimit: 1},
}

func Lookup(reason string) (Route, bool) {
	route, ok := routes[reason]
	return route, ok
}

func UserMessage(reason string) string {
	switch reason {
	case taskfailure.ReasonAgentRuntimeMissingExecutable.String():
		return "The runtime is missing a required executable. The runtime owner needs to install it, then retry this task."
	case taskfailure.ReasonAgentRuntimeVersionUnsupported.String():
		return "The runtime version is unsupported. The runtime owner needs to upgrade it, then retry this task."
	default:
		return ""
	}
}
