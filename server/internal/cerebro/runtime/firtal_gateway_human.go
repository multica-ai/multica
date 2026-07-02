package runtime

// firtal_gateway_human.go is the ingress half of the loop human-check
// transport for an agent assignee: it lets an agent report an explicit
// approve/reject decision back to the delivery gate via a first-class tool
// call, mirroring firtal_gateway_judge.go's report_loop_judge. Unlike a judge
// check (score a rubric), a human check stands in for a person's sign-off —
// the agent is expected to review the actual delivered work and decide, not
// grade against a rubric. A person (member) assignee never calls this tool;
// they approve via the approve-human-check HTTP endpoint instead (see
// workflows/handler.go).
//
// This closes the agent-assignee half of the human check loop:
//
//	gate enqueues human check → dispatch.go sends task → agent reads the
//	delivered work and decides → agent calls
//	report_loop_human(approved, note) → store records the decision →
//	gate re-evaluates → advance or revise

import (
	"context"
	"encoding/json"
	"fmt"

	cerebroloops "github.com/multica-ai/multica/server/internal/cerebro/loops"
	"github.com/multica-ai/multica/server/internal/util"
)

// FirtalReportLoopHumanTool lets an agent assignee report an explicit
// approve/reject decision for a loop human-type verification check back to
// the delivery gate, standing in for a person's sign-off.
type FirtalReportLoopHumanTool struct {
	store *cerebroloops.Store
	tctx  ToolContext
}

func (t *FirtalReportLoopHumanTool) Name() string { return "report_loop_human" }
func (t *FirtalReportLoopHumanTool) Description() string {
	return "Report an explicit approve/reject decision for a loop human-type verification check " +
		"back to the delivery gate. Call this after reviewing the actual delivered work from the " +
		"task you were dispatched with (not just the builder's own summary of it) — you are " +
		"standing in for a person's sign-off, not scoring a rubric. approved must be true only if " +
		"you would sign off on this yourself; note must explain your decision either way."
}
func (t *FirtalReportLoopHumanTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id", "gate", "round", "check_id", "approved"},
		"properties": map[string]any{
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Issue UUID from the task you were dispatched with.",
			},
			"gate": map[string]any{
				"type":        "string",
				"description": "Gate key from the task (identifies which delivery gate owns this decision).",
			},
			"round": map[string]any{
				"type":        "integer",
				"description": "Loop round from the task.",
			},
			"check_id": map[string]any{
				"type":        "string",
				"description": "The check id from the task — must match exactly.",
			},
			"approved": map[string]any{
				"type":        "boolean",
				"description": "true only if you sign off; false otherwise.",
			},
			"note": map[string]any{
				"type":        "string",
				"description": "Why you approved or rejected. Required whenever approved is false.",
			},
		},
	}
}

func (t *FirtalReportLoopHumanTool) Call(ctx context.Context, args map[string]any) (string, error) {
	issueStr, err := toolRequireString(args, "issue_id")
	if err != nil {
		return "", err
	}
	gate, err := toolRequireString(args, "gate")
	if err != nil {
		return "", err
	}

	round, err := toolRequireInt32(args, "round")
	if err != nil {
		return "", err
	}

	checkID, err := toolRequireString(args, "check_id")
	if err != nil {
		return "", err
	}

	approvedRaw, ok := args["approved"]
	if !ok {
		return "", fmt.Errorf("report_loop_human: approved is required")
	}
	approved, ok := approvedRaw.(bool)
	if !ok {
		return "", fmt.Errorf("report_loop_human: approved must be a boolean")
	}

	note, _ := args["note"].(string)
	if !approved && note == "" {
		return "", fmt.Errorf("report_loop_human: note is required when approved is false")
	}

	issueID, err := util.ParseUUID(issueStr)
	if err != nil {
		return "", fmt.Errorf("report_loop_human: invalid issue_id: %w", err)
	}

	agentID := t.tctx.AgentID
	if err := t.store.ReportHuman(ctx, issueID, gate, round, checkID, approved, note, agentID, "agent"); err != nil {
		return "", fmt.Errorf("report_loop_human: %w", err)
	}

	result := map[string]any{"recorded": true, "approved": approved}
	if approved {
		result["status"] = "passed"
	} else {
		result["status"] = "revise"
	}
	raw, _ := json.Marshal(result)
	return string(raw), nil
}
