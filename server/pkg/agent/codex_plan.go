package agent

import "fmt"

// codexPlanToolArgs turns on the update_plan tool, which Codex 0.170 does not
// give the model by default. It goes before the user's args so their own
// `-c tools.update_plan.enabled=...` still wins (last value wins). A Codex
// that does not know the key only logs a warning.
var codexPlanToolArgs = []string{"-c", "tools.update_plan.enabled=true"}

// codexPlanToolName is the transcript tool name for a Codex plan snapshot. It
// matches the model-facing tool Codex uses to publish the plan.
const codexPlanToolName = "update_plan"

// handlePlanUpdated records a turn/plan/updated snapshot so the transcript can
// show the run's progress.
//
// turn/plan/updated carries the whole plan each time, so every notification is
// emitted as one complete tool call: a tool-use holding the snapshot and an
// immediate tool-result. The result must follow right away: the daemon counts
// an unanswered tool-use as in flight and would otherwise judge the rest of
// the turn against the tool-call watchdog budget instead of the idle one.
func (c *codexClient) handlePlanUpdated(params map[string]any) {
	if c.onMessage == nil {
		return
	}
	rawPlan, _ := params["plan"].([]any)
	plan := make([]any, 0, len(rawPlan))
	for _, entry := range rawPlan {
		step, _ := entry.(map[string]any)
		text, _ := step["step"].(string)
		if text == "" {
			continue
		}
		status, _ := step["status"].(string)
		plan = append(plan, map[string]any{"step": text, "status": status})
	}
	input := map[string]any{"plan": plan}
	if explanation, _ := params["explanation"].(string); explanation != "" {
		input["explanation"] = explanation
	}

	c.planUpdates++
	turnID, _ := params["turnId"].(string)
	callID := fmt.Sprintf("plan-%s-%d", turnID, c.planUpdates)
	c.onMessage(Message{Type: MessageToolUse, Tool: codexPlanToolName, CallID: callID, Input: input})
	c.onMessage(Message{Type: MessageToolResult, Tool: codexPlanToolName, CallID: callID, Output: "Plan updated"})
}
