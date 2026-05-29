package daemon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAppendGoalInstructionCompilesProviderSpecificPrompt(t *testing.T) {
	base := "Do work.\n"
	got := appendGoalInstruction(base, "codex", "Open PR and checks pass")
	if !strings.Contains(got, "## Completion Goal") {
		t.Fatalf("expected goal section, got %q", got)
	}
	if !strings.Contains(got, "Open PR and checks pass") {
		t.Fatalf("expected goal condition in prompt, got %q", got)
	}
	if !strings.Contains(got, "Codex runtime instruction") {
		t.Fatalf("expected Codex-specific instruction, got %q", got)
	}
	if strings.Contains(got, "/goal") {
		t.Fatalf("prompt should not depend on slash commands: %q", got)
	}
}

func TestAppendGoalInstructionSkipsEmptyGoal(t *testing.T) {
	base := "Do work.\n"
	if got := appendGoalInstruction(base, "codex", "  "); got != base {
		t.Fatalf("expected unchanged prompt, got %q", got)
	}
}

func TestParseGoalStatus(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want goalStatus
	}{
		{name: "marker satisfied", out: "GOAL_SATISFIED\nPR opened", want: goalStatusSatisfied},
		{name: "marker blocked", out: "BLOCKED: missing credentials", want: goalStatusBlocked},
		{name: "marker partial", out: "PARTIAL: tests pass but PR not opened", want: goalStatusPartial},
		{name: "json satisfied", out: `{"goal_status":"satisfied","evidence":["tests passed"]}`, want: goalStatusSatisfied},
		{name: "json fenced", out: "```json\n{\"goal_status\":\"blocked\"}\n```", want: goalStatusBlocked},
		{name: "missing", out: "Done, see summary", want: goalStatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseGoalStatus(tt.out); got != tt.want {
				t.Fatalf("parseGoalStatus() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestBuildPromptIncludesGoalInstruction(t *testing.T) {
	task := Task{IssueID: "issue-1", GoalCondition: "Open PR and checks pass"}
	got := BuildPrompt(task, "codex")
	if !strings.Contains(got, "## Completion Goal") {
		t.Fatalf("expected goal section in assignment prompt, got %q", got)
	}
	if !strings.Contains(got, "Open PR and checks pass") {
		t.Fatalf("expected goal condition in prompt, got %q", got)
	}
	if !strings.Contains(got, "Codex runtime instruction") {
		t.Fatalf("expected provider-specific instruction, got %q", got)
	}
}

func TestBuildPromptWithoutGoalIsUnchanged(t *testing.T) {
	task := Task{IssueID: "issue-1"}
	got := BuildPrompt(task, "codex")
	if strings.Contains(got, "## Completion Goal") {
		t.Fatalf("expected no goal section when GoalCondition is empty, got %q", got)
	}
}

func TestCompileGoalInstructionProviderVariants(t *testing.T) {
	if claude := compileGoalInstruction("claude", "ship it"); !strings.Contains(claude, "Claude runtime instruction") {
		t.Fatalf("expected Claude-specific instruction, got %q", claude)
	}
	// Any unmapped provider still gets a generic instruction (never silence).
	generic := compileGoalInstruction("gemini", "ship it")
	if strings.Contains(generic, "Claude runtime instruction") || strings.Contains(generic, "Codex runtime instruction") {
		t.Fatalf("expected generic instruction for unmapped provider, got %q", generic)
	}
	for _, p := range []string{"claude", "codex", "gemini", "openclaw"} {
		if out := compileGoalInstruction(p, "ship it"); strings.Contains(out, "/goal") {
			t.Fatalf("provider %q instruction must not depend on slash commands: %q", p, out)
		}
	}
}

func TestTaskUnmarshalsGoalCondition(t *testing.T) {
	var task Task
	if err := json.Unmarshal([]byte(`{"id":"t1","issue_id":"i1","goal_condition":"Ship the PR"}`), &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if task.GoalCondition != "Ship the PR" {
		t.Fatalf("GoalCondition = %q, want %q", task.GoalCondition, "Ship the PR")
	}
}
