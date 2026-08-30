package daemon

import (
	"strings"
	"testing"
)

func TestBuildPromptOperatingModeIdentity(t *testing.T) {
	base := Task{IssueID: "issue-1", Agent: &AgentData{}}
	missing := BuildPrompt(base, "claude")

	coding := base
	coding.Agent = &AgentData{OperatingMode: "coding"}
	if got := BuildPrompt(coding, "claude"); got != missing {
		t.Fatal("explicit coding mode changed the assignment prompt")
	}

	unknown := base
	unknown.Agent = &AgentData{OperatingMode: "unknown"}
	if got := BuildPrompt(unknown, "claude"); got != missing {
		t.Fatal("unknown stored mode did not fall back to the coding prompt")
	}

	for _, mode := range []string{"operational", "hybrid"} {
		t.Run(mode, func(t *testing.T) {
			task := base
			task.Agent = &AgentData{OperatingMode: mode}
			got := BuildPrompt(task, "claude")
			if !strings.Contains(got, "business-task agent for a Multica workspace") {
				t.Fatalf("%s prompt is missing its business-task identity:\n%s", mode, got)
			}
			if strings.Contains(got, "local coding agent") {
				t.Fatalf("%s prompt retained the coding-only identity:\n%s", mode, got)
			}
		})
	}
}

func TestBuildPromptOperatingModeDoesNotRewriteNonAssignmentSurfaces(t *testing.T) {
	for name, task := range map[string]Task{
		"chat":         {ChatSessionID: "chat-1", ChatMessage: "hello"},
		"comment":      {TriggerCommentID: "comment-1", IssueID: "issue-1"},
		"autopilot":    {AutopilotRunID: "run-1", AutopilotTitle: "Daily review"},
		"quick-create": {QuickCreatePrompt: "Create a follow-up issue"},
	} {
		t.Run(name, func(t *testing.T) {
			coding := task
			coding.Agent = &AgentData{OperatingMode: "coding"}
			operational := task
			operational.Agent = &AgentData{OperatingMode: "operational"}
			if got, want := BuildPrompt(operational, "claude"), BuildPrompt(coding, "claude"); got != want {
				t.Fatalf("operating mode changed the %s per-turn prompt", name)
			}
		})
	}
}
