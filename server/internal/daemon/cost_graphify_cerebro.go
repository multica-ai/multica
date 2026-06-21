package daemon

// CEREBRO-PATCH(daemon-graphify-nudge): the "graphify" adoption nudge (FIR-1311).
// When a workspace turns the graphify saving on, the server ships a standing
// "use the code graph" instruction in Task.GraphifyNudge at claim time. This
// folds it into the agent's start prompt. Unlike the snapshot saving it does NOT
// claim the issue was pre-fetched and is independent of snapshot_prompt /
// bundled_read — it only reaches ALL local runtime types (every CLI consumes the
// daemon-built start prompt) and tells them to prefer the graphify CLI for
// navigation. No measurement (deferred per FIR-1312).

import "strings"

// graphifyNudgeBlock returns the graphify instruction to inline into the start
// prompt, and true, when the server shipped a nudge (graphify saving "on").
// Returns "", false when no nudge was shipped, leaving the prompt unchanged.
func graphifyNudgeBlock(task Task) (string, bool) {
	nudge := strings.TrimSpace(task.GraphifyNudge)
	if nudge == "" {
		return "", false
	}
	return nudge + "\n\n", true
}
