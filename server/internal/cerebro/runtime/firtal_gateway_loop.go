package runtime

// firtal_gateway_loop.go is the ingress half of the loop check transport: it
// lets a worker agent report the exit code of a programmatic check back to the
// delivery gate via a first-class tool call. The gate reads reported exit codes
// from the store (loops.Store) and decides advance / revise / wait without ever
// trusting the agent's own judgement — only the exit code matters.
//
// This closes the loop:
//
//	gate enqueues check → dispatch.go sends task → worker runs check →
//	worker calls report_loop_check(exit_code) → store records result →
//	gate re-evaluates → advance or revise

import (
	"context"
	"encoding/json"
	"fmt"

	cerebroloops "github.com/multica-ai/multica/server/internal/cerebro/loops"
	"github.com/multica-ai/multica/server/internal/util"
)

// FirtalReportLoopCheckTool lets a worker agent report the exit code of a loop
// verification check back to the delivery gate. The gate uses the raw exit code
// (never the agent's prose summary) to decide whether the check passed.
type FirtalReportLoopCheckTool struct {
	store *cerebroloops.Store
	tctx  ToolContext
}

func (t *FirtalReportLoopCheckTool) Name() string { return "report_loop_check" }
func (t *FirtalReportLoopCheckTool) Description() string {
	return "Report the exit code of a loop verification check back to the delivery gate. " +
		"Call this immediately after running the check command the loop dispatched to you, " +
		"with the exact argv and the process exit code. The gate decides pass/fail from the " +
		"exit code alone — not from your description of the output."
}
func (t *FirtalReportLoopCheckTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id", "gate", "round", "argv", "exit_code"},
		"properties": map[string]any{
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Issue UUID from the check task you were dispatched with.",
			},
			"gate": map[string]any{
				"type":        "string",
				"description": "Gate key from the check task (identifies which delivery gate owns this result).",
			},
			"round": map[string]any{
				"type":        "integer",
				"description": "Loop round from the check task.",
			},
			"argv": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
				"description": "The exact argv array from the check task — must match the dispatched command character for character.",
			},
			"exit_code": map[string]any{
				"type":        "integer",
				"description": "Process exit code: 0 = check passed, any other value = check failed.",
			},
		},
	}
}

func (t *FirtalReportLoopCheckTool) Call(ctx context.Context, args map[string]any) (string, error) {
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

	argvRaw, ok := args["argv"]
	if !ok {
		return "", fmt.Errorf("report_loop_check: argv is required")
	}
	argvSlice, ok := argvRaw.([]any)
	if !ok {
		return "", fmt.Errorf("report_loop_check: argv must be an array")
	}
	argv := make([]string, len(argvSlice))
	for i, a := range argvSlice {
		s, ok := a.(string)
		if !ok {
			return "", fmt.Errorf("report_loop_check: argv[%d] must be a string", i)
		}
		argv[i] = s
	}

	exitCode, err := toolRequireInt32(args, "exit_code")
	if err != nil {
		return "", err
	}

	issueID, err := util.ParseUUID(issueStr)
	if err != nil {
		return "", fmt.Errorf("report_loop_check: invalid issue_id: %w", err)
	}

	if err := t.store.Report(ctx, issueID, gate, round, argv, exitCode); err != nil {
		return "", fmt.Errorf("report_loop_check: %w", err)
	}

	result := map[string]any{"recorded": true, "exit_code": exitCode}
	if exitCode == 0 {
		result["status"] = "passed"
	} else {
		result["status"] = "failed"
	}
	raw, _ := json.Marshal(result)
	return string(raw), nil
}

// toolRequireInt32 extracts a required integer argument, accepting the JSON
// float64 encoding that all gateway args arrive as. Mirrors toolRequireString.
func toolRequireInt32(args map[string]any, key string) (int32, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("%s is required", key)
	}
	switch n := v.(type) {
	case float64:
		return int32(n), nil
	case int:
		return int32(n), nil
	case int32:
		return n, nil
	case int64:
		return int32(n), nil
	default:
		return 0, fmt.Errorf("%s must be an integer, got %T", key, v)
	}
}
