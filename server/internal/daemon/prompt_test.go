package daemon

import (
	"strings"
	"testing"
)

func TestBuildQuickCreatePrompt_AllExplicitFields(t *testing.T) {
	task := Task{
		QuickCreatePrompt:    "Fix the login button",
		QuickCreatePriority:  "high",
		QuickCreateDueDate:   "2025-06-01T00:00:00Z",
		QuickCreateProjectID: "123e4567-e89b-12d3-a456-426614174000",
	}
	got := buildQuickCreatePrompt(task)

	for _, want := range []string{
		"`--priority high`",
		"`--due-date 2025-06-01T00:00:00Z`",
		"`--project 123e4567-e89b-12d3-a456-426614174000`",
		"`--priority high`.\n\n",
		"`--due-date 2025-06-01T00:00:00Z`.\n\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, `\n`) {
		t.Fatalf("prompt should contain real newlines, got literal \\n:\n%s", got)
	}
	if strings.Contains(got, "Map P0/P1") {
		t.Fatalf("prompt should not include fallback priority guidance when explicit priority is set:\n%s", got)
	}
}

func TestBuildQuickCreatePrompt_PriorityOnly(t *testing.T) {
	task := Task{
		QuickCreatePrompt:   "Urgent: server is down",
		QuickCreatePriority: "urgent",
	}
	got := buildQuickCreatePrompt(task)

	if !strings.Contains(got, "`--priority urgent`") {
		t.Fatalf("prompt missing explicit priority flag:\n%s", got)
	}
	if strings.Contains(got, "Map P0/P1") {
		t.Fatalf("prompt should not include fallback priority guidance when explicit priority is set:\n%s", got)
	}
	if strings.Contains(got, "`--due-date") || strings.Contains(got, "`--project") {
		t.Fatalf("prompt should not inject unset quick-create flags:\n%s", got)
	}
}

func TestBuildQuickCreatePrompt_NoneSet(t *testing.T) {
	task := Task{QuickCreatePrompt: "Something came up"}
	got := buildQuickCreatePrompt(task)

	if !strings.Contains(got, "Map P0/P1") {
		t.Fatalf("prompt should include fallback priority guidance when no explicit priority is set:\n%s", got)
	}
	if strings.Contains(got, `\n`) {
		t.Fatalf("prompt should contain real newlines, got literal \\n:\n%s", got)
	}
}
