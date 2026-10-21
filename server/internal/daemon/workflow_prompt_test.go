package daemon

import (
	"strings"
	"testing"
)

const deliveryBrief = "This project's workflow — Delivery\nYou are handling the \"Code review\" step of MUL-1.\n"

// Project workflow block of the per-turn prompt (MUL-7420). The brief has no
// workflow input at all; the block is appended after everything else, so the
// prompt ahead of it is the same whatever the workflow says.
func TestBuildPromptAppendsProjectWorkflowBlock(t *testing.T) {
	base := Task{IssueID: "issue-1"}
	plain := BuildPrompt(base, "claude")

	task := base
	task.ProjectWorkflow = &ProjectWorkflowData{Brief: deliveryBrief}
	out := BuildPrompt(task, "claude")
	if !strings.HasPrefix(out, plain) {
		t.Fatalf("the workflow block must be appended, not change the prompt ahead of it:\n%s", out)
	}
	if !strings.Contains(out, "[Project workflow]\n"+deliveryBrief) {
		t.Fatalf("prompt is missing the workflow brief:\n%s", out)
	}
}

func TestBuildPromptSkipsEmptyWorkflowBrief(t *testing.T) {
	task := Task{IssueID: "issue-1", ProjectWorkflow: &ProjectWorkflowData{Brief: "  \n"}}
	if out := BuildPrompt(task, "claude"); strings.Contains(out, "[Project workflow]") {
		t.Fatalf("an empty brief rendered a block:\n%s", out)
	}
}

// A handoff note is the caller's own instruction now; the workflow brief
// arrives through the claim, so a handoff run gets both.
func TestBuildPromptKeepsWorkflowBlockBesideHandoffNote(t *testing.T) {
	task := Task{IssueID: "issue-1", HandoffNote: "Only the login flow.", ProjectWorkflow: &ProjectWorkflowData{Brief: deliveryBrief}}
	out := BuildPrompt(task, "claude")
	if !strings.Contains(out, "> Only the login flow.") || !strings.Contains(out, "[Project workflow]") {
		t.Fatalf("handoff run must carry the note and the workflow brief:\n%s", out)
	}
}

func TestBuildPromptKeepsWorkflowBlockForWakeup(t *testing.T) {
	task := Task{IssueID: "issue-1", WakeupID: "w-1", HandoffNote: "check the PR", ProjectWorkflow: &ProjectWorkflowData{Brief: deliveryBrief}}
	if out := BuildPrompt(task, "claude"); !strings.Contains(out, "[Project workflow]") {
		t.Fatalf("wakeup run lost the workflow block:\n%s", out)
	}
}

func TestBuildPromptQuotesEveryHandoffNoteLine(t *testing.T) {
	out := BuildPrompt(Task{IssueID: "issue-1", HandoffNote: "First line.\nSecond line."}, "claude")
	if !strings.Contains(out, "> First line.\n> Second line.\n") {
		t.Fatalf("every handoff note line must be quoted:\n%s", out)
	}
}
