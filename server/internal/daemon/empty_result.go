package daemon

import "github.com/multica-ai/multica/server/pkg/agent"

// emptyAgentResultError is deliberately phrased to match the canonical
// taskfailure empty-output classifier. A provider can exit successfully while
// having produced no answer at all; once output, usage, and tool activity are
// all absent, treating that process exit as a completed task hides the failure.
const emptyAgentResultError = "agent returned empty output with no token usage or tool activity"

// promoteEmptyAgentResultFailure converts the one silent-success shape the
// daemon can prove is not useful into a failed agent result. An empty answer is
// valid when the agent did work through tools, and a missing usage map is valid
// for runtimes that do not expose billing, so all three independent signals
// must be absent before the fallback applies.
func promoteEmptyAgentResultFailure(result agent.Result, tools int32) (agent.Result, bool) {
	if result.Status != "completed" || result.Output != "" || len(result.Usage) != 0 || tools != 0 {
		return result, false
	}

	result.Status = "failed"
	if result.Error == "" {
		result.Error = emptyAgentResultError
	}
	return result, true
}
