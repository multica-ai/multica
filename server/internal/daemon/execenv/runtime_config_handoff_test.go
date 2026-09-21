package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectRuntimeConfigAssignmentHandoffGuidance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		provider string
		file     string
	}{
		{"claude", "CLAUDE.md"},
		{"codex", "AGENTS.md"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if _, err := InjectRuntimeConfig(dir, tc.provider, TaskContextForEnv{IssueID: "issue-1"}); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			brief := string(content)
			for _, want := range []string{
				"use it when the target assignee is already handling this issue from another run",
				"To hand the issue to another agent, reassign without `--no-start`; this starts a new run for the new owner",
				"when assignment or status only records ownership/progress for an assignee already handling this issue",
			} {
				if !strings.Contains(brief, want) {
					t.Errorf("%s missing handoff guidance %q", tc.file, want)
				}
			}
			// The outgoing agent's work is always underway at handoff time (#8659).
			for _, ambiguous := range []string{
				"use it when the work is already underway",
				"ownership/progress for work already underway",
			} {
				if strings.Contains(brief, ambiguous) {
					t.Errorf("%s contains ambiguous no-start guidance %q", tc.file, ambiguous)
				}
			}
		})
	}
}
