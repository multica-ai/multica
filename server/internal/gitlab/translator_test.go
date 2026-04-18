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

func TestBuildCreateIssueInput_StatusAndPriorityToLabels(t *testing.T) {
	in := CreateIssueRequest{
		Title:    "hi",
		Status:   "in_progress",
		Priority: "high",
	}
	out := BuildCreateIssueInput(in, nil)
	if out.Title != "hi" {
		t.Errorf("title = %q", out.Title)
	}
	hasStatus := false
	hasPriority := false
	for _, l := range out.Labels {
		if l == "status::in_progress" {
			hasStatus = true
		}
		if l == "priority::high" {
			hasPriority = true
		}
	}
	if !hasStatus {
		t.Errorf("labels missing status::in_progress: %v", out.Labels)
	}
	if !hasPriority {
		t.Errorf("labels missing priority::high: %v", out.Labels)
	}
}

func TestBuildCreateIssueInput_AgentAssigneeToLabel(t *testing.T) {
	in := CreateIssueRequest{
		Title:        "hi",
		Status:       "todo",
		Priority:     "none",
		AssigneeType: "agent",
		AssigneeID:   "agent-uuid-1",
	}
	out := BuildCreateIssueInput(in, map[string]string{"agent-uuid-1": "builder"})
	hasAgentLabel := false
	for _, l := range out.Labels {
		if l == "agent::builder" {
			hasAgentLabel = true
		}
	}
	if !hasAgentLabel {
		t.Errorf("labels missing agent::builder: %v", out.Labels)
	}
	if len(out.AssigneeIDs) != 0 {
		t.Errorf("AssigneeIDs should be empty when assigning to agent, got %v", out.AssigneeIDs)
	}
}

func TestBuildCreateIssueInput_MemberAssigneeIgnoredInPhase3a(t *testing.T) {
	// Phase 3b will resolve member UUID → GitLab user ID. Until then,
	// member assignees are silently dropped.
	in := CreateIssueRequest{
		Title:        "hi",
		Status:       "todo",
		Priority:     "none",
		AssigneeType: "member",
		AssigneeID:   "user-uuid-1",
	}
	out := BuildCreateIssueInput(in, nil)
	if len(out.AssigneeIDs) != 0 {
		t.Errorf("AssigneeIDs should be empty for member assignee in 3a, got %v", out.AssigneeIDs)
	}
}

func TestBuildCreateIssueInput_PriorityNoneOmitted(t *testing.T) {
	// priority::none is the default — emitting the label clutters GitLab UI.
	in := CreateIssueRequest{
		Title:    "hi",
		Status:   "todo",
		Priority: "none",
	}
	out := BuildCreateIssueInput(in, nil)
	for _, l := range out.Labels {
		if l == "priority::none" {
			t.Errorf("priority::none should not be emitted as a label; got %v", out.Labels)
		}
	}
}

func TestBuildUpdateIssueInput(t *testing.T) {
	agentSlugByUUID := map[string]string{
		"11111111-1111-1111-1111-111111111111": "builder",
		"22222222-2222-2222-2222-222222222222": "reviewer",
	}

	statusClosed := "done"
	statusOpen := "in_progress"
	statusCancelled := "cancelled"
	prioHigh := "high"
	prioNone := "none"
	titleNew := "new title"
	descNew := "new desc"
	due := "2026-05-01"

	type oldSnap struct {
		status       string
		priority     string
		assigneeType string
		assigneeUUID string
	}
	cases := []struct {
		name          string
		old           oldSnap
		req           UpdateIssueRequest
		wantAddLabels []string
		wantRemove    []string
		wantTitle     *string
		wantDesc      *string
		wantDue       *string
		wantState     *string
	}{
		{
			name:          "title-only",
			old:           oldSnap{status: "todo", priority: "none"},
			req:           UpdateIssueRequest{Title: &titleNew},
			wantTitle:     &titleNew,
			wantAddLabels: nil,
			wantRemove:    nil,
		},
		{
			name:          "status transition in_progress → done closes",
			old:           oldSnap{status: "in_progress", priority: "none"},
			req:           UpdateIssueRequest{Status: &statusClosed},
			wantAddLabels: []string{"status::done"},
			wantRemove:    []string{"status::in_progress"},
			wantState:     strPtr("close"),
		},
		{
			name:          "status transition done → in_progress reopens",
			old:           oldSnap{status: "done", priority: "none"},
			req:           UpdateIssueRequest{Status: &statusOpen},
			wantAddLabels: []string{"status::in_progress"},
			wantRemove:    []string{"status::done"},
			wantState:     strPtr("reopen"),
		},
		{
			name:          "status cancelled closes",
			old:           oldSnap{status: "todo", priority: "none"},
			req:           UpdateIssueRequest{Status: &statusCancelled},
			wantAddLabels: []string{"status::cancelled"},
			wantRemove:    []string{"status::todo"},
			wantState:     strPtr("close"),
		},
		{
			name:          "priority none → high",
			old:           oldSnap{status: "todo", priority: "none"},
			req:           UpdateIssueRequest{Priority: &prioHigh},
			wantAddLabels: []string{"priority::high"},
			wantRemove:    nil,
		},
		{
			name:          "priority high → none removes without adding",
			old:           oldSnap{status: "todo", priority: "high"},
			req:           UpdateIssueRequest{Priority: &prioNone},
			wantAddLabels: nil,
			wantRemove:    []string{"priority::high"},
		},
		{
			name:          "agent assignee change",
			old:           oldSnap{status: "todo", priority: "none", assigneeType: "agent", assigneeUUID: "11111111-1111-1111-1111-111111111111"},
			req:           UpdateIssueRequest{AssigneeType: strPtr("agent"), AssigneeID: strPtr("22222222-2222-2222-2222-222222222222")},
			wantAddLabels: []string{"agent::reviewer"},
			wantRemove:    []string{"agent::builder"},
		},
		{
			name:          "clear agent assignee",
			old:           oldSnap{status: "todo", priority: "none", assigneeType: "agent", assigneeUUID: "11111111-1111-1111-1111-111111111111"},
			req:           UpdateIssueRequest{AssigneeType: strPtr(""), AssigneeID: strPtr("")},
			wantAddLabels: nil,
			wantRemove:    []string{"agent::builder"},
		},
		{
			name:          "switch from agent to member removes agent label",
			old:           oldSnap{status: "todo", priority: "none", assigneeType: "agent", assigneeUUID: "11111111-1111-1111-1111-111111111111"},
			req:           UpdateIssueRequest{AssigneeType: strPtr("member"), AssigneeID: strPtr("99999999-9999-9999-9999-999999999999")},
			wantAddLabels: nil,
			wantRemove:    []string{"agent::builder"},
		},
		{
			name:     "description + due date pass through",
			old:      oldSnap{status: "todo", priority: "none"},
			req:      UpdateIssueRequest{Description: &descNew, DueDate: &due},
			wantDesc: &descNew,
			wantDue:  &due,
		},
		{
			name:          "status unchanged emits no labels or state event",
			old:           oldSnap{status: "in_progress", priority: "none"},
			req:           UpdateIssueRequest{Status: strPtr("in_progress")},
			wantAddLabels: nil,
			wantRemove:    nil,
			// wantState defaults to nil — no state_event for a no-op status
		},
		{
			name:          "priority none → none emits nothing",
			old:           oldSnap{status: "todo", priority: "none"},
			req:           UpdateIssueRequest{Priority: strPtr("none")},
			wantAddLabels: nil,
			wantRemove:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildUpdateIssueInput(
				OldIssueSnapshot{
					Status:       tc.old.status,
					Priority:     tc.old.priority,
					AssigneeType: tc.old.assigneeType,
					AssigneeUUID: tc.old.assigneeUUID,
				},
				tc.req,
				agentSlugByUUID,
			)
			if !strSliceEq(got.AddLabels, tc.wantAddLabels) {
				t.Errorf("AddLabels = %v, want %v", got.AddLabels, tc.wantAddLabels)
			}
			if !strSliceEq(got.RemoveLabels, tc.wantRemove) {
				t.Errorf("RemoveLabels = %v, want %v", got.RemoveLabels, tc.wantRemove)
			}
			if !strPtrEq(got.Title, tc.wantTitle) {
				t.Errorf("Title = %v, want %v", got.Title, tc.wantTitle)
			}
			if !strPtrEq(got.Description, tc.wantDesc) {
				t.Errorf("Description = %v, want %v", got.Description, tc.wantDesc)
			}
			if !strPtrEq(got.DueDate, tc.wantDue) {
				t.Errorf("DueDate = %v, want %v", got.DueDate, tc.wantDue)
			}
			if !strPtrEq(got.StateEvent, tc.wantState) {
				t.Errorf("StateEvent = %v, want %v", got.StateEvent, tc.wantState)
			}
		})
	}
}

func strPtr(v string) *string { return &v }

func strSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func strPtrEq(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
