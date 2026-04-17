package gitlab

import (
	"testing"

	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

func TestTranslateIssue_StatusFromScopedLabel(t *testing.T) {
	in := gitlabapi.Issue{
		IID:    42,
		Title:  "Hi",
		State:  "opened",
		Labels: []string{"status::in_progress", "bug"},
	}
	out := TranslateIssue(in, &TranslateContext{AgentBySlug: nil})
	if out.Status != "in_progress" {
		t.Errorf("Status = %q, want in_progress", out.Status)
	}
}

func TestTranslateIssue_StatusFallsBackToTodoForOpened(t *testing.T) {
	in := gitlabapi.Issue{IID: 42, Labels: []string{"bug"}, State: "opened"}
	out := TranslateIssue(in, &TranslateContext{})
	if out.Status != "todo" {
		t.Errorf("Status = %q, want todo (default for opened)", out.Status)
	}
}

func TestTranslateIssue_StatusFallsBackToDoneForClosed(t *testing.T) {
	in := gitlabapi.Issue{IID: 42, Labels: []string{"bug"}, State: "closed"}
	out := TranslateIssue(in, &TranslateContext{})
	if out.Status != "done" {
		t.Errorf("Status = %q, want done (default for closed)", out.Status)
	}
}

func TestTranslateIssue_PriorityFromScopedLabel(t *testing.T) {
	in := gitlabapi.Issue{Labels: []string{"priority::high"}, State: "opened"}
	out := TranslateIssue(in, &TranslateContext{})
	if out.Priority != "high" {
		t.Errorf("Priority = %q, want high", out.Priority)
	}
}

func TestTranslateIssue_PriorityDefaultsToNone(t *testing.T) {
	in := gitlabapi.Issue{Labels: []string{"bug"}, State: "opened"}
	out := TranslateIssue(in, &TranslateContext{})
	if out.Priority != "none" {
		t.Errorf("Priority = %q, want none", out.Priority)
	}
}

func TestTranslateIssue_AgentAssigneeFromScopedLabel(t *testing.T) {
	in := gitlabapi.Issue{Labels: []string{"agent::builder"}, State: "opened"}
	out := TranslateIssue(in, &TranslateContext{
		AgentBySlug: map[string]string{"builder": "agent-uuid-123"},
	})
	if out.AssigneeType != "agent" || out.AssigneeID != "agent-uuid-123" {
		t.Errorf("Assignee = (%q, %q), want (agent, agent-uuid-123)", out.AssigneeType, out.AssigneeID)
	}
}

func TestTranslateIssue_AgentLabelWithUnknownSlugLeavesUnassigned(t *testing.T) {
	in := gitlabapi.Issue{Labels: []string{"agent::ghost"}, State: "opened"}
	out := TranslateIssue(in, &TranslateContext{
		AgentBySlug: map[string]string{"builder": "uuid-builder"},
	})
	if out.AssigneeType != "" || out.AssigneeID != "" {
		t.Errorf("Assignee should be empty for unknown agent slug, got (%q, %q)", out.AssigneeType, out.AssigneeID)
	}
}

func TestTranslateIssue_NativeAssigneeIgnoredInPhase2a(t *testing.T) {
	in := gitlabapi.Issue{
		Labels:    []string{},
		State:     "opened",
		Assignees: []gitlabapi.User{{ID: 100, Username: "alice"}},
	}
	out := TranslateIssue(in, &TranslateContext{})
	if out.AssigneeType != "" || out.AssigneeID != "" {
		t.Errorf("Native assignee should be ignored in 2a, got (%q, %q)", out.AssigneeType, out.AssigneeID)
	}
}

func TestTranslateIssue_MultipleAgentLabelsPicksFirstAlphabetically(t *testing.T) {
	in := gitlabapi.Issue{
		Labels: []string{"agent::zebra", "agent::alpha"},
		State:  "opened",
	}
	out := TranslateIssue(in, &TranslateContext{
		AgentBySlug: map[string]string{"alpha": "uuid-a", "zebra": "uuid-z"},
	})
	if out.AssigneeID != "uuid-a" {
		t.Errorf("AssigneeID = %q, want uuid-a (first alphabetical)", out.AssigneeID)
	}
}

func TestTranslateNote_StripsAgentPrefix(t *testing.T) {
	in := gitlabapi.Note{
		Body:   "**[agent:builder]** I'm working on it.",
		System: false,
	}
	out := TranslateNote(in)
	if out.AuthorType != "agent" || out.AuthorSlug != "builder" {
		t.Errorf("Author = (%q, %q), want (agent, builder)", out.AuthorType, out.AuthorSlug)
	}
	if out.Body != "I'm working on it." {
		t.Errorf("Body = %q, want stripped", out.Body)
	}
	if out.Type != "comment" {
		t.Errorf("Type = %q, want comment", out.Type)
	}
}

func TestTranslateNote_SystemNote(t *testing.T) {
	in := gitlabapi.Note{Body: "added status::todo", System: true}
	out := TranslateNote(in)
	if out.Type != "system" {
		t.Errorf("Type = %q, want system", out.Type)
	}
	if out.AuthorType != "" {
		t.Errorf("Author should be empty for system note, got %q", out.AuthorType)
	}
}

func TestTranslateAward_PassesEmoji(t *testing.T) {
	in := gitlabapi.AwardEmoji{Name: "thumbsup", User: gitlabapi.User{ID: 100}}
	out := TranslateAward(in)
	if out.Emoji != "thumbsup" {
		t.Errorf("Emoji = %q, want thumbsup", out.Emoji)
	}
	if out.GitlabUserID != 100 {
		t.Errorf("GitlabUserID = %d, want 100", out.GitlabUserID)
	}
}
