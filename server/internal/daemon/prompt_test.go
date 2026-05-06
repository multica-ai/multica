package daemon

import (
	"strings"
	"testing"
)

func TestBuildQuickCreatePromptUsesSelectedProject(t *testing.T) {
	prompt := BuildPrompt(Task{
		QuickCreatePrompt: "file the follow-up",
		ProjectID:         "22222222-3333-4444-5555-666666666666",
		ProjectTitle:      "Project Alpha",
	})

	for _, want := range []string{
		`selected project is "Project Alpha"`,
		`--project "22222222-3333-4444-5555-666666666666"`,
		"do not substitute the project title",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("quick-create prompt missing %q\n---\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "project**: omit") {
		t.Fatalf("quick-create prompt should not omit project when selected\n---\n%s", prompt)
	}
}

func TestBuildQuickCreatePromptOmitsProjectWhenUnselected(t *testing.T) {
	prompt := BuildPrompt(Task{QuickCreatePrompt: "file the follow-up"})

	if !strings.Contains(prompt, "No project was selected") {
		t.Fatalf("quick-create prompt should explain no project was selected\n---\n%s", prompt)
	}
	if strings.Contains(prompt, "--project") {
		t.Fatalf("quick-create prompt should not include --project without a selected project\n---\n%s", prompt)
	}
}
