package loops

import (
	"strings"
	"testing"
)

const validSpec = `
version: 1
goal: Fix the checkout test flake and prove it with a green run
definition_of_done: 20 local repeats pass and a note explains the failure mode
verification:
  - id: tests-green
    type: programmatic
    check: ["pytest", "-x", "tests/checkout"]
    expect: exit_zero
  - id: note-explains
    type: judge
    rubric: The note names the root cause and the verification evidence
caps:
  max_iterations: 12
  max_revisions: 3
  no_progress_stalls: 2
`

func TestParse_Valid(t *testing.T) {
	s, err := Parse([]byte(validSpec))
	if err != nil {
		t.Fatalf("expected valid spec, got error: %v", err)
	}
	if s.Goal == "" || s.DefinitionOfDone == "" {
		t.Fatalf("goal/definition_of_done not parsed: %+v", s)
	}
	if got := len(s.ProgrammaticChecks()); got != 1 {
		t.Fatalf("expected 1 programmatic check, got %d", got)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	if _, err := Parse([]byte("version: : :\n  - bad")); err == nil {
		t.Fatal("expected YAML parse error")
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want string
	}{
		{
			name: "wrong version",
			spec: Spec{Version: 2, Goal: "g", DefinitionOfDone: "d",
				Verification: []Verification{{ID: "a", Type: CheckProgrammatic, Check: []string{"true"}}},
				Caps:         Caps{1, 1, 1}},
			want: "version must be 1",
		},
		{
			name: "missing goal",
			spec: Spec{Version: 1, DefinitionOfDone: "d",
				Verification: []Verification{{ID: "a", Type: CheckProgrammatic, Check: []string{"true"}}},
				Caps:         Caps{1, 1, 1}},
			want: "goal is required",
		},
		{
			name: "no verification",
			spec: Spec{Version: 1, Goal: "g", DefinitionOfDone: "d", Caps: Caps{1, 1, 1}},
			want: "at least one verification",
		},
		{
			name: "only judge, no programmatic",
			spec: Spec{Version: 1, Goal: "g", DefinitionOfDone: "d",
				Verification: []Verification{{ID: "a", Type: CheckJudge, Rubric: "r"}},
				Caps:         Caps{1, 1, 1}},
			want: "at least one programmatic verification",
		},
		{
			name: "programmatic without check argv",
			spec: Spec{Version: 1, Goal: "g", DefinitionOfDone: "d",
				Verification: []Verification{{ID: "a", Type: CheckProgrammatic}},
				Caps:         Caps{1, 1, 1}},
			want: "non-empty check argv",
		},
		{
			name: "duplicate id",
			spec: Spec{Version: 1, Goal: "g", DefinitionOfDone: "d",
				Verification: []Verification{
					{ID: "a", Type: CheckProgrammatic, Check: []string{"true"}},
					{ID: "a", Type: CheckProgrammatic, Check: []string{"true"}},
				},
				Caps: Caps{1, 1, 1}},
			want: `id "a" is duplicated`,
		},
		{
			name: "unknown type",
			spec: Spec{Version: 1, Goal: "g", DefinitionOfDone: "d",
				Verification: []Verification{
					{ID: "a", Type: CheckProgrammatic, Check: []string{"true"}},
					{ID: "b", Type: "vibes"},
				},
				Caps: Caps{1, 1, 1}},
			want: `unknown type "vibes"`,
		},
		{
			name: "missing caps",
			spec: Spec{Version: 1, Goal: "g", DefinitionOfDone: "d",
				Verification: []Verification{{ID: "a", Type: CheckProgrammatic, Check: []string{"true"}}}},
			want: "caps.max_iterations must be > 0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParse_PlanningField(t *testing.T) {
	withPlanning := `
version: 1
goal: Fix checkout flake
definition_of_done: 20 repeats pass
planning: true
verification:
  - id: tests-green
    type: programmatic
    check: ["pytest", "-x", "tests/checkout"]
caps:
  max_iterations: 5
  max_revisions: 2
  no_progress_stalls: 1
`
	s, err := Parse([]byte(withPlanning))
	if err != nil {
		t.Fatalf("expected planning spec to parse: %v", err)
	}
	if !s.Planning {
		t.Fatal("Planning should be true")
	}

	// A spec without the field should default to false (backward compat).
	s2, err := Parse([]byte(validSpec))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s2.Planning {
		t.Fatal("Planning should default to false")
	}
}

func TestValidate_AggregatesMultiple(t *testing.T) {
	// Empty spec should report several problems at once, not just the first.
	err := (&Spec{}).Validate()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"version must be 1", "goal is required", "definition_of_done is required", "at least one verification"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("aggregated error missing %q in: %v", want, err)
		}
	}
}
