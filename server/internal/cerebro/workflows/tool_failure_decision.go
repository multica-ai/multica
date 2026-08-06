package workflows

import (
	"errors"
	"fmt"
	"strings"
)

// ToolFailureAction is what Workflow decided a failed tool call should do next.
type ToolFailureAction string

const (
	// ToolFailureSurface hands the raw tool error back to the agent, which then
	// decides for itself. It is the answer when no policy matched.
	ToolFailureSurface ToolFailureAction = "surface"
	// ToolFailureRetry re-runs the same tool call, bounded by RetryLimit.
	ToolFailureRetry ToolFailureAction = "retry"
	// ToolFailureStop ends the call with Workflow's own instruction instead of
	// the raw tool error, so the agent stops using this tool.
	ToolFailureStop ToolFailureAction = "stop"
)

// MaxToolFailureRetries caps how often one Workflow decision may re-run a
// failed tool call, so a policy cannot spin a run forever.
const MaxToolFailureRetries = 3

// ToolFailureMutableFields are the modifications a Workflow policy may set on
// an on.tool.failure event.
var ToolFailureMutableFields = []string{"failure_action", "retry_limit", "user_message"}

// ToolFailureDecision is the acted-on outcome of an on.tool.failure event.
type ToolFailureDecision struct {
	Action     ToolFailureAction
	RetryLimit int
	Message    string
	Evaluated  bool
}

// ToolFailureDecisionFrom translates a hook result into the decision the tool
// invoker enforces. A block or require decision always stops the call; the
// failure_action modification chooses between retry, stop, and surface.
//
// "pause" and "alert" are accepted as aliases for stop so a policy written in
// the task-failure vocabulary (failure_action: retry/pause/alert/surface) does
// not silently do nothing at this seam.
func ToolFailureDecisionFrom(result HookResult) ToolFailureDecision {
	decision := ToolFailureDecision{Action: ToolFailureSurface, Evaluated: result.Evaluated}
	if result.Decision == HookBlock || result.Decision == HookRequire {
		decision.Action = ToolFailureStop
	}
	if action, ok := result.Modifications["failure_action"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(action)) {
		case string(ToolFailureRetry):
			decision.Action = ToolFailureRetry
		case string(ToolFailureStop), "pause", "alert":
			decision.Action = ToolFailureStop
		case string(ToolFailureSurface):
			decision.Action = ToolFailureSurface
		}
	}
	if limit, ok := integerModification(result.Modifications["retry_limit"]); ok && limit > 0 {
		decision.RetryLimit = int(limit)
	}
	if decision.Action == ToolFailureRetry {
		if decision.RetryLimit <= 0 {
			decision.RetryLimit = 1
		}
		if decision.RetryLimit > MaxToolFailureRetries {
			decision.RetryLimit = MaxToolFailureRetries
		}
	}
	if message, ok := result.Modifications["user_message"].(string); ok {
		decision.Message = strings.TrimSpace(message)
	}
	if decision.Message == "" && len(result.Requirements) > 0 {
		decision.Message = strings.Join(result.Requirements, "; ")
	}
	return decision
}

// StopError is the error a stop decision returns instead of the raw tool error.
func (d ToolFailureDecision) StopError() error {
	if d.Message != "" {
		return errors.New(d.Message)
	}
	return errors.New("Workflow stopped this tool call after it failed")
}

// BlockingHookError turns a block or require decision into the error the
// calling seam returns. Every seam shares it so one Workflow decision reads
// the same wherever it stopped work.
func BlockingHookError(result HookResult) error {
	if result.Decision != HookBlock && result.Decision != HookRequire {
		return nil
	}
	if len(result.Requirements) > 0 {
		return errors.New(strings.Join(result.Requirements, "; "))
	}
	return fmt.Errorf("Workflow %s decision stopped the operation", result.Decision)
}
