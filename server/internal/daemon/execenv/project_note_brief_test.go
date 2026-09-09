package execenv

import (
	"encoding/json"
	"strings"
	"testing"
)

// The design rule for project notes has two halves, and they are easy to
// confuse:
//
//   - The brief DOES carry a fixed-size pointer telling the agent the notepads
//     exist and which commands reach them. Without it, discovery depends on the
//     agent choosing to load a skill, which is not reliable enough for a
//     capability the task may well depend on.
//   - The brief NEVER carries note contents, or even the list of titles. Both
//     grow with the number of notepads, and the brief has no length ceiling, so
//     either one would let this section expand without bound and compete with
//     the task for context.
//
// The tests below pin each half.

// TestProjectNoteInstructionInBrief covers the first half: the pointer must be
// present whenever the task has a project, and must name the commands rather
// than gesturing at them.
func TestProjectNoteInstructionInBrief(t *testing.T) {
	t.Parallel()

	ctx := TaskContextForEnv{
		IssueID:            "11111111-2222-3333-4444-555555555555",
		ProjectID:          "22222222-3333-4444-5555-666666666666",
		ProjectTitle:       "Notes Instruction Project",
		ProjectDescription: "Durable project context lives here.",
	}

	for _, provider := range []string{"claude", "codex"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			brief := buildMetaSkillContent(provider, ctx)

			for _, want := range []string{
				// The commands carry the real project id, not a placeholder, so
				// the agent can run them without first resolving anything.
				"multica project note list 22222222-3333-4444-5555-666666666666",
				"multica project note get 22222222-3333-4444-5555-666666666666 <note-id>",
				"note append",
				"note create",
			} {
				if !strings.Contains(brief, want) {
					t.Errorf("brief is missing the note instruction %q", want)
				}
			}

			// append must be recommended over update: update replaces the whole
			// body, so an agent journalling with it would destroy history.
			if !strings.Contains(brief, "Prefer `append` over `update`") {
				t.Error("brief does not steer the agent from update to append")
			}
		})
	}
}

// TestProjectNoteInstructionOmittedWithoutProject verifies the instruction is
// gated on there actually being a project. Notes are project-scoped, so telling
// an agent about `note list <project-id>` when no project exists would be an
// instruction it cannot act on.
func TestProjectNoteInstructionOmittedWithoutProject(t *testing.T) {
	t.Parallel()

	// A task with resources but no project still enters writeProjectContext.
	ctx := TaskContextForEnv{
		IssueID: "11111111-2222-3333-4444-555555555555",
		ProjectResources: []ProjectResourceForEnv{
			{
				ID:           "33333333-4444-5555-6666-777777777777",
				ResourceType: "github_repo",
				ResourceRef:  json.RawMessage(`{"url":"https://github.com/org/no-project"}`),
			},
		},
	}

	brief := buildMetaSkillContent("claude", ctx)
	if strings.Contains(brief, "project note list") {
		t.Error("brief advertises project notes for a task with no project")
	}
}

// TestProjectNoteContentsAbsentFromBrief covers the second half: the pointer is
// allowed, the payload is not. If someone later adds a notes field to
// TaskContextForEnv and renders titles or bodies here, this fails — which is the
// point.
func TestProjectNoteContentsAbsentFromBrief(t *testing.T) {
	t.Parallel()

	ctx := TaskContextForEnv{
		IssueID:      "11111111-2222-3333-4444-555555555555",
		ProjectID:    "22222222-3333-4444-5555-666666666666",
		ProjectTitle: "Notes Regression Project",
	}

	for _, provider := range []string{"claude", "codex"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			// Deterministic: the brief is a pure function of a context that
			// cannot carry notes, so no note can ever change it.
			if buildMetaSkillContent(provider, ctx) != buildMetaSkillContent(provider, ctx) {
				t.Fatalf("brief is not deterministic for provider %s", provider)
			}

			brief := buildMetaSkillContent(provider, ctx)

			// Strings that only a rendered note list or body could introduce.
			// "note" alone is not bannable any more — the instruction uses it.
			for _, banned := range []string{
				"## Project Notes",
				"body_md",
				"body_size",
				".multica/project/notes",
			} {
				if strings.Contains(brief, banned) {
					t.Errorf("brief leaked note payload marker %q", banned)
				}
			}

			// The instruction is a constant. Anything that scales with the
			// number of notepads would show up as a bullet list here, the way
			// resources do.
			if strings.Contains(brief, "- **Note**") || strings.Contains(brief, "- Note:") {
				t.Error("brief renders per-note rows; only a fixed pointer belongs here")
			}

			if !strings.Contains(brief, "## Project Context") {
				t.Fatalf("brief lost its Project Context section; this test is no longer meaningful")
			}
		})
	}
}

// TestTaskContextForEnvCarriesNoNotes pins the structural half of the guarantee.
// Serializing the context and scanning its keys catches a notes field added to
// the struct even before anything renders it into the brief.
//
// The match is against specific field names rather than the substring "note",
// because unrelated fields legitimately contain it (HandoffNote).
func TestTaskContextForEnvCarriesNoNotes(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(TaskContextForEnv{
		ProjectID:    "22222222-3333-4444-5555-666666666666",
		ProjectTitle: "Structural Check",
	})
	if err != nil {
		t.Fatalf("marshal TaskContextForEnv: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal TaskContextForEnv: %v", err)
	}
	for _, banned := range []string{
		"ProjectNotes",
		"Notes",
		"ProjectNote",
		"NoteBodies",
	} {
		if _, found := fields[banned]; found {
			t.Errorf("TaskContextForEnv gained field %q; note contents must stay out of the brief", banned)
		}
	}
}
