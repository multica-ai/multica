package runtime

// firtal_gateway_judge.go is the ingress half of the loop judge-check
// transport: it lets a judge agent report a structured pass/revise verdict
// back to the delivery gate via a first-class tool call, mirroring
// firtal_gateway_loop.go's report_loop_check for programmatic checks. The
// gate reads the reported verdict from the store (loops.Store) and decides
// advance / revise / wait without ever trusting the worker's own summary of
// its work — only the judge's structured verdict matters, and the judge
// itself is expected to have read the actual delivered work (see
// dispatch.go's buildJudgePrompt), not the builder's account of it.
//
// This closes the judge half of the loop:
//
//	gate enqueues judge check → dispatch.go sends task → judge reads the
//	delivered work and scores the rubric → judge calls
//	report_loop_judge(pass, blocking_issues) → store records the verdict →
//	gate re-evaluates → advance or revise

import (
	"context"
	"encoding/json"
	"fmt"

	cerebroloops "github.com/multica-ai/multica/server/internal/cerebro/loops"
	"github.com/multica-ai/multica/server/internal/util"
)

// FirtalReportLoopJudgeTool lets a judge agent report a structured verdict for
// a loop judge-type verification check back to the delivery gate. The gate
// uses the reported pass/fail alone (never the judge's free-form reasoning)
// to decide whether the check passed.
type FirtalReportLoopJudgeTool struct {
	store *cerebroloops.Store
	tctx  ToolContext
}

func (t *FirtalReportLoopJudgeTool) Name() string { return "report_loop_judge" }
func (t *FirtalReportLoopJudgeTool) Description() string {
	return "Report the structured verdict of a loop judge-type verification check back to the " +
		"delivery gate. Call this after scoring the rubric from the judge task you were " +
		"dispatched with, having read the actual delivered work (not just the builder's own " +
		"summary of it). The gate decides pass/fail from this verdict alone — pass must be true " +
		"only if the rubric is fully met, and blocking_issues must name the specific, concrete " +
		"reasons whenever pass is false, so the builder does not have to rediscover them."
}
func (t *FirtalReportLoopJudgeTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id", "gate", "round", "check_id", "pass"},
		"properties": map[string]any{
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Issue UUID from the judge task you were dispatched with.",
			},
			"gate": map[string]any{
				"type":        "string",
				"description": "Gate key from the judge task (identifies which delivery gate owns this verdict).",
			},
			"round": map[string]any{
				"type":        "integer",
				"description": "Loop round from the judge task.",
			},
			"check_id": map[string]any{
				"type":        "string",
				"description": "The check id from the judge task — must match exactly.",
			},
			"pass": map[string]any{
				"type":        "boolean",
				"description": "true only if the rubric is fully met; false otherwise.",
			},
			"blocking_issues": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "The specific, concrete reasons the rubric is not met. Required whenever pass is false; omit or leave empty when pass is true.",
			},
		},
	}
}

func (t *FirtalReportLoopJudgeTool) Call(ctx context.Context, args map[string]any) (string, error) {
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

	passRaw, ok := args["pass"]
	if !ok {
		return "", fmt.Errorf("report_loop_judge: pass is required")
	}
	pass, ok := passRaw.(bool)
	if !ok {
		return "", fmt.Errorf("report_loop_judge: pass must be a boolean")
	}

	var blocking []string
	if raw, ok := args["blocking_issues"]; ok && raw != nil {
		items, ok := raw.([]any)
		if !ok {
			return "", fmt.Errorf("report_loop_judge: blocking_issues must be an array")
		}
		blocking = make([]string, len(items))
		for i, item := range items {
			s, ok := item.(string)
			if !ok {
				return "", fmt.Errorf("report_loop_judge: blocking_issues[%d] must be a string", i)
			}
			blocking[i] = s
		}
	}
	if !pass && len(blocking) == 0 {
		return "", fmt.Errorf("report_loop_judge: blocking_issues is required when pass is false")
	}

	issueID, err := util.ParseUUID(issueStr)
	if err != nil {
		return "", fmt.Errorf("report_loop_judge: invalid issue_id: %w", err)
	}

	if err := t.store.ReportJudge(ctx, issueID, gate, round, checkID, pass, blocking); err != nil {
		return "", fmt.Errorf("report_loop_judge: %w", err)
	}

	result := map[string]any{"recorded": true, "pass": pass}
	if pass {
		result["status"] = "passed"
	} else {
		result["status"] = "revise"
	}
	raw, _ := json.Marshal(result)
	return string(raw), nil
}
