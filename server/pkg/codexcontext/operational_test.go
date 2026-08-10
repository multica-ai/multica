package codexcontext

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

func TestDecodeMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    Mode
		wantErr bool
	}{
		{name: "empty config", raw: "", want: ModeInherited},
		{name: "missing config", raw: "{}", want: ModeInherited},
		{name: "missing field among future fields", raw: "{\"codex\":{\"future\":true}}", want: ModeInherited},
		{name: "inherited", raw: "{\"codex\":{\"context_mode\":\"inherited\"}}", want: ModeInherited},
		{name: "operational", raw: "{\"codex\":{\"context_mode\":\"operational\"}}", want: ModeOperational},
		{name: "unknown value", raw: "{\"codex\":{\"context_mode\":\"small\"}}", wantErr: true},
		{name: "wrong type", raw: "{\"codex\":{\"context_mode\":true}}", wantErr: true},
		{name: "malformed codex object", raw: "{\"codex\":true}", wantErr: true},
		{name: "malformed runtime config", raw: "{", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DecodeMode(json.RawMessage(tt.raw))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DecodeMode() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeMode() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("DecodeMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildOperationalContext(t *testing.T) {
	t.Parallel()

	input := BuildInput{
		AgentInstructions: "Investigate operational anomalies and report evidence.",
		TaskPrompt:        "Issue issue-123: inspect the runner and return a Multica task result.",
		AssignedSkills: []skillbundle.Skill{
			{
				ID:          "skill-1",
				Source:      skillbundle.SourceWorkspace,
				Name:        "Runner Health",
				Description: "Read runner health.",
				Content:     "Use the runner-health command.",
				Files: []skillbundle.File{
					{Path: "references/contract.md", Content: "runner contract"},
				},
			},
		},
	}

	got, err := BuildOperationalContext(input)
	if err != nil {
		t.Fatalf("BuildOperationalContext() error = %v", err)
	}
	if !strings.Contains(got.DeveloperInstructions, input.AgentInstructions) {
		t.Fatalf("developer instructions do not contain agent instructions: %q", got.DeveloperInstructions)
	}
	if got.Prompt != input.TaskPrompt {
		t.Fatalf("prompt = %q, want %q", got.Prompt, input.TaskPrompt)
	}
	if len(got.Skills) != 1 || len(got.Skills[0].Files) != 1 {
		t.Fatalf("supporting skill files were not preserved: %#v", got.Skills)
	}

	input.AssignedSkills[0].Content = "mutated"
	input.AssignedSkills[0].Files[0].Content = "mutated"
	if got.Skills[0].Content == "mutated" || got.Skills[0].Files[0].Content == "mutated" {
		t.Fatal("returned skills alias the input")
	}

	combined := strings.Join([]string{
		got.BaseInstructions,
		got.DeveloperInstructions,
		got.Prompt,
	}, "\n")
	for _, excluded := range []string{"AGENTS.md", ".agent_context", "runtime brief"} {
		if strings.Contains(combined, excluded) {
			t.Fatalf("operational context refers to excluded source %q", excluded)
		}
	}
}

func TestBuildOperationalContextRejectsAssignedSkillNameCollision(t *testing.T) {
	t.Parallel()

	_, err := BuildOperationalContext(BuildInput{
		AssignedSkills: []skillbundle.Skill{
			{Name: "Runner Health"},
			{Name: "runner-health"},
		},
	})
	if err == nil {
		t.Fatal("BuildOperationalContext() error = nil, want normalized-name collision")
	}
	if strings.Contains(err.Error(), "Runner Health") || strings.Contains(err.Error(), "runner-health") {
		t.Fatalf("error leaks skill names: %v", err)
	}
}
