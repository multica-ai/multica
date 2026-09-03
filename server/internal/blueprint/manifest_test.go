package blueprint

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildManifestExportsPortableRefsAndRelationships(t *testing.T) {
	exportedAt := time.Date(2026, 5, 24, 10, 30, 0, 0, time.UTC)
	manifest, err := BuildManifest(Source{
		Name:       "Release Squad",
		ExportedAt: exportedAt,
		Squads: []SourceSquad{{
			ID:           "squad-db-id",
			Name:         "Release Squad",
			Description:  "Ships weekly releases",
			Instructions: "Coordinate release tasks.",
			LeaderID:     "agent-lead-id",
		}},
		SquadMembers: map[string][]SourceSquadMember{
			"squad-db-id": {
				{MemberType: "agent", MemberID: "agent-lead-id", Role: "lead"},
				{MemberType: "agent", MemberID: "agent-reviewer-id", Role: "review"},
				{MemberType: "member", MemberID: "human-member-id", Role: "stakeholder"},
			},
		},
		Agents: []SourceAgent{
			{
				ID:                 "agent-lead-id",
				Name:               "Release Lead",
				Description:        "Plans release work",
				Instructions:       "Own the release checklist.",
				RuntimeMode:        "local",
				RuntimeProvider:    "codex",
				Visibility:         "workspace",
				MaxConcurrentTasks: 2,
				Model:              "gpt-5.4",
				ThinkingLevel:      "medium",
			},
			{
				ID:                 "agent-reviewer-id",
				Name:               "Release Reviewer",
				Description:        "Reviews release notes",
				Instructions:       "Review release notes.",
				RuntimeMode:        "cloud",
				RuntimeProvider:    "multica_agent",
				Visibility:         "private",
				MaxConcurrentTasks: 1,
			},
		},
		AgentSkillIDs: map[string][]string{
			"agent-lead-id": {"skill-runbook-id"},
		},
		Skills: []SourceSkill{{
			ID:          "skill-runbook-id",
			Name:        "Release Runbook",
			Description: "Release process",
			Content:     "# Release",
			Config:      json.RawMessage(`{"scope":"release"}`),
		}},
		SkillFiles: map[string][]SourceSkillFile{
			"skill-runbook-id": {
				{Path: "steps/checklist.md", Content: "- Verify changelog"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}

	if manifest.Schema != SchemaVersion {
		t.Fatalf("schema = %q, want %q", manifest.Schema, SchemaVersion)
	}
	if manifest.ExportedAt != exportedAt.Format(time.RFC3339) {
		t.Fatalf("exported_at = %q, want %q", manifest.ExportedAt, exportedAt.Format(time.RFC3339))
	}
	if len(manifest.Squads) != 1 {
		t.Fatalf("squads len = %d, want 1", len(manifest.Squads))
	}
	squad := manifest.Squads[0]
	if squad.Ref != "squad.release-squad" {
		t.Fatalf("squad ref = %q, want portable slug ref", squad.Ref)
	}
	if squad.LeaderRef != "agent.release-lead" {
		t.Fatalf("leader_ref = %q, want agent.release-lead", squad.LeaderRef)
	}
	if len(squad.Members) != 2 {
		t.Fatalf("agent squad members len = %d, want 2", len(squad.Members))
	}
	if squad.Members[1].Ref != "agent.release-reviewer" {
		t.Fatalf("second member ref = %q, want agent.release-reviewer", squad.Members[1].Ref)
	}

	if len(manifest.Agents) != 2 {
		t.Fatalf("agents len = %d, want 2", len(manifest.Agents))
	}
	lead := manifest.Agents[0]
	if lead.Ref != "agent.release-lead" {
		t.Fatalf("agent ref = %q, want agent.release-lead", lead.Ref)
	}
	if len(lead.SkillRefs) != 1 || lead.SkillRefs[0] != "skill.release-runbook" {
		t.Fatalf("agent skill_refs = %#v, want [skill.release-runbook]", lead.SkillRefs)
	}
	if lead.Runtime.Provider != "codex" {
		t.Fatalf("runtime provider = %q, want codex", lead.Runtime.Provider)
	}

	if len(manifest.Skills) != 1 {
		t.Fatalf("skills len = %d, want 1", len(manifest.Skills))
	}
	skill := manifest.Skills[0]
	if skill.Ref != "skill.release-runbook" {
		t.Fatalf("skill ref = %q, want skill.release-runbook", skill.Ref)
	}
	if len(skill.Files) != 1 || skill.Files[0].Path != "steps/checklist.md" {
		t.Fatalf("skill files = %#v, want steps/checklist.md", skill.Files)
	}
}

func TestBuildManifestRedactsSensitiveAgentFields(t *testing.T) {
	manifest, err := BuildManifest(Source{
		Name: "Secret Squad",
		Agents: []SourceAgent{{
			ID:                 "agent-secret-id",
			Name:               "Secret Agent",
			RuntimeID:          "runtime-db-id",
			RuntimeMode:        "local",
			RuntimeProvider:    "codex",
			RuntimeConfig:      json.RawMessage(`{"workdir":"/private/tmp/project","provider":"codex"}`),
			Visibility:         "private",
			MaxConcurrentTasks: 1,
			CustomEnv: map[string]string{
				"GITHUB_TOKEN": "ghp_secret",
				"OPENAI_KEY":   "sk-secret",
			},
			CustomArgs: []string{"--safe-mode"},
			MCPConfig:  json.RawMessage(`{"servers":{"github":{"env":{"GITHUB_TOKEN":"ghp_secret"}}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}
	if len(manifest.Agents) != 1 {
		t.Fatalf("agents len = %d, want 1", len(manifest.Agents))
	}
	agent := manifest.Agents[0]

	encoded, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("marshal agent: %v", err)
	}
	for _, forbidden := range []string{"ghp_secret", "sk-secret", "runtime-db-id", "/private/tmp/project"} {
		if containsJSON(encoded, forbidden) {
			t.Fatalf("agent manifest leaked %q: %s", forbidden, encoded)
		}
	}
	if len(agent.CustomEnvSchema) != 2 {
		t.Fatalf("custom_env_schema len = %d, want 2", len(agent.CustomEnvSchema))
	}
	if agent.CustomEnvSchema[0].Name != "GITHUB_TOKEN" || !agent.CustomEnvSchema[0].Secret {
		t.Fatalf("first env schema = %#v, want secret GITHUB_TOKEN", agent.CustomEnvSchema[0])
	}
	if !agent.MCPConfigRedacted {
		t.Fatalf("mcp_config_redacted = false, want true")
	}
	if len(agent.CustomArgs) != 1 || agent.CustomArgs[0] != "--safe-mode" {
		t.Fatalf("custom_args = %#v, want preserved non-secret args", agent.CustomArgs)
	}
}

func TestValidateManifestRejectsDuplicateRefsAndUnsafeSkillFiles(t *testing.T) {
	manifest := Manifest{
		Schema: SchemaVersion,
		Name:   "Invalid",
		Agents: []Agent{
			{Ref: "agent.same", Name: "A", Runtime: Runtime{Mode: "local"}},
			{Ref: "agent.same", Name: "B", Runtime: Runtime{Mode: "cloud"}},
		},
		Skills: []Skill{{
			Ref:   "skill.bad",
			Name:  "Bad Skill",
			Files: []SkillFile{{Path: "../secret.txt", Content: "nope"}},
		}},
	}

	err := ValidateManifest(manifest)
	if err == nil {
		t.Fatal("ValidateManifest returned nil, want duplicate ref/path error")
	}
	if !containsError(err, "duplicate ref") {
		t.Fatalf("ValidateManifest error = %v, want duplicate ref", err)
	}
	if !containsError(err, "unsafe skill file path") {
		t.Fatalf("ValidateManifest error = %v, want unsafe skill file path", err)
	}
}

func containsJSON(raw []byte, needle string) bool {
	return json.Valid(raw) && strings.Contains(string(raw), needle)
}

func containsError(err error, needle string) bool {
	return err != nil && strings.Contains(err.Error(), needle)
}
