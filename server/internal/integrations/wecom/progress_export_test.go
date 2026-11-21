package wecom

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestDescribeStepWordsAStepForAnotherAdapter — a tool call and an error are
// steps; prose, a tool's output and reasoning are not.
func TestDescribeStepWordsAStepForAnotherAdapter(t *testing.T) {
	step, ok := DescribeStep(toolUse("exec_command", map[string]any{"command": "git status --short"}), LocaleEn, true)
	if !ok || step.Kind != StepCommand || step.Text != "Running git status --short" {
		t.Fatalf("command step = %+v, %v", step, ok)
	}

	for _, msg := range []protocol.TaskMessagePayload{
		{Type: "text", Content: "I will check the repository."},
		{Type: "tool_result", Tool: "exec_command", Output: "M server/main.go"},
		{Type: "thinking", Content: "The user wants a status."},
	} {
		if step, ok := DescribeStep(msg, LocaleEn, true); ok {
			t.Errorf("%s message became the step %+v", msg.Type, step)
		}
	}
}

// TestDescribeStepWithoutArgumentKeepsOnlyTheKindOfWork — a reader the run
// does not belong to sees what sort of work it is and nothing the agent
// composed.
func TestDescribeStepWithoutArgumentKeepsOnlyTheKindOfWork(t *testing.T) {
	cases := []struct {
		name string
		msg  protocol.TaskMessagePayload
		kind StepKind
		want string
		hide string
	}{
		{"a command", toolUse("Bash", map[string]any{"command": "cat /srv/acme/.env"}), StepCommand, "Running a command", "acme"},
		{"a file", toolUse("Read", map[string]any{"file_path": "/srv/acme/handler.go"}), StepRead, "Reading a file", "acme"},
		{"a search", toolUse("Grep", map[string]any{"pattern": "customer_name"}), StepSearch, "Searching the code", "customer_name"},
		{"an error", protocol.TaskMessagePayload{Type: "error", Content: "open /srv/acme/key: denied"}, StepError, "That step errored — carrying on", "acme"},
		{"an MCP call", toolUse("mcp__calendar__list_events", map[string]any{"owner": "ada@acme.test"}), StepService, "Calling calendar · list_events", "ada"},
		{"an unknown tool", toolUse("frobnicate", map[string]any{"target": "acme-prod"}), StepTool, "Using frobnicate", "acme"},
		// Some agent backends pass the call's title as the tool's name, and
		// the agent composed the title.
		{"a tool named by its title", toolUse("Delete /srv/acme/customers.csv", nil), StepTool, "Working", "acme"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step, ok := DescribeStep(tc.msg, LocaleEn, false)
			if !ok || step.Kind != tc.kind || step.Text != tc.want {
				t.Fatalf("step = %+v, %v; want %s %q", step, ok, tc.kind, tc.want)
			}
			// The same call does name its argument for the run's owner, so the
			// absence above is the flag's doing.
			detailed, _ := DescribeStep(tc.msg, LocaleEn, true)
			if !strings.Contains(detailed.Text, tc.hide) {
				t.Fatalf("detailed step %q does not carry %q", detailed.Text, tc.hide)
			}
		})
	}
}
