package issueworkflow

import (
	"strings"
	"testing"
)

func TestBriefTaskBindingMismatch(t *testing.T) {
	for _, key := range []string{"implement", "removed_key", "old\nkey\t\x1b\u0085"} {
		for _, observer := range []bool{false, true} {
			for _, legacy := range []bool{false, true} {
				def := devDefinition()
				def.Steps = append(def.Steps, Step{StatusKey: "handlerless", Instructions: "Do the new work."})
				for _, current := range []string{"code_review", "handlerless"} {
					in := BriefInput{Workflow: def, StatusKey: current, TaskWorkflowStep: &key, IssueIdentifier: "MUL-7", Observer: observer, LegacyRuntimeRules: legacy,
						StatusName: func(k string) string {
							if k == "implement" {
								return "Renamed\nimplementation\x1b"
							}
							return ""
						},
					}
					got := Brief(in)
					for _, want := range []string{"issue current step:", current, "this task's bound step:", "Do not change the issue's status in this run", "Statuses in this workflow:", "Agent-created wakeups and automatic retries", "does not identify what created this task"} {
						if !strings.Contains(got, want) {
							t.Errorf("missing %q:\n%s", want, got)
						}
					}
					for _, forbidden := range []string{"Your moves", "Step done", "Needs changes", "Can't proceed", "Starting work", "multica issue status", "Step instructions", "Do the new work.", "\x1b", "\u0085", "old\nkey", "Renamed\nimplementation"} {
						if strings.Contains(got, forbidden) {
							t.Errorf("mismatch leaks %q:\n%s", forbidden, got)
						}
					}
					if key == "implement" && !strings.Contains(got, `"Renamed implementation" (implement)`) {
						t.Errorf("missing renamed key: %s", got)
					}
					if key == "removed_key" && !strings.Contains(got, `"removed_key"`) {
						t.Errorf("missing deleted raw key: %s", got)
					}
					if strings.HasPrefix(key, "old") && !strings.Contains(got, `"old key"`) {
						t.Errorf("unsafe raw fallback: %s", got)
					}
					if observer != strings.Contains(got, "You are not its assignee") {
						t.Errorf("observer changed: %s", got)
					}
					if legacy != strings.Contains(got, "replaces the status rules in your runtime instructions") {
						t.Errorf("legacy override changed: %s", got)
					}
				}
			}
		}
	}
}

func TestBriefKnownSameBindingPreservesPreviewAndTimeline(t *testing.T) {
	for _, observer := range []bool{false, true} {
		for _, legacy := range []bool{false, true} {
			in := BriefInput{Workflow: devDefinition(), StatusKey: "code_review", IssueIdentifier: "MUL-7", Observer: observer, LegacyRuntimeRules: legacy}
			withoutTask := Brief(in)
			in.TaskWorkflowStep = &in.StatusKey
			if got := Brief(in); got != withoutTask {
				t.Fatalf("same binding changed preview/timeline rendering:\n%s\n%s", got, withoutTask)
			}
		}
	}
}
