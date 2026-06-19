package dettools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// testGateInput lists the smoke/test commands to run. Each runs through the
// system shell in the working directory.
type testGateInput struct {
	Commands []string `json:"commands"`
}

const testOutputTailBytes = 4000

func testGateTool() Tool {
	return Tool{
		Name:        "test_gate",
		Description: "Run configured test suites and normalize outcomes to a stable pass/fail per suite. Returns POLICY_FAILURE if any command exits non-zero. USE instead of raw test runners — output formats differ across test frameworks and the model can misclassify partial failures, timeouts, or flaky output as passing. Each command runs through the system shell; output is captured and tail-trimmed.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "commands": {
      "type": "array",
      "items": {"type": "string"},
      "minItems": 1,
      "description": "Shell command lines to run, e.g. [\"go test ./...\", \"pnpm test\"]."
    }
  },
  "required": ["commands"],
  "additionalProperties": false
}`),
		Handler: testGateHandler,
	}
}

func testGateHandler(ctx context.Context, args json.RawMessage, env ToolEnv) Result {
	var in testGateInput
	if err := strictUnmarshal(args, &in); err != nil {
		return Errf(CodeInvalidInput, "invalid test_gate input: %v", err)
	}
	if len(in.Commands) == 0 {
		return Errf(CodeInvalidInput, "at least one command is required")
	}

	var results []map[string]any
	failed := 0
	for _, command := range in.Commands {
		if ctx.Err() != nil {
			// Deadline hit between commands — stop; the server maps the
			// cancelled context to TIMEOUT.
			break
		}
		start := time.Now()
		out, code, _ := runShell(ctx, env.WorkDir, command)
		passed := code == 0
		if !passed {
			failed++
		}
		results = append(results, map[string]any{
			"command":     command,
			"exit_code":   code,
			"passed":      passed,
			"duration_ms": time.Since(start).Milliseconds(),
			"output_tail": tail(out, testOutputTailBytes),
		})
	}

	data := map[string]any{
		"results":    results,
		"ran":        len(results),
		"failed":     failed,
		"all_passed": failed == 0,
		"work_dir":   env.WorkDir,
	}
	if failed > 0 {
		return Result{
			Status:      StatusError,
			ErrorCode:   CodePolicyFailure,
			Summary:     fmt.Sprintf("%d of %d test command(s) failed", failed, len(results)),
			MachineData: data,
			Retryable:   false,
		}
	}
	return OK(fmt.Sprintf("all %d test command(s) passed", len(results)), data)
}
