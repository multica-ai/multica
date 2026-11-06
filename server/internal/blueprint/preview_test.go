package blueprint

import "testing"

func TestPreviewManifestClassifiesCreatesReusesAndMissingRequirements(t *testing.T) {
	manifest := Manifest{
		Schema: SchemaVersion,
		Name:   "Release Package",
		Squads: []Squad{{
			Ref:       "squad.release",
			Name:      "Release Squad",
			LeaderRef: "agent.release-lead",
			Members:   []SquadMember{{Ref: "agent.release-lead"}},
		}},
		Agents: []Agent{
			{
				Ref:                "agent.release-lead",
				Name:               "Release Lead",
				Runtime:            Runtime{Mode: "local", Provider: "codex", Model: "gpt-5.4"},
				Visibility:         "workspace",
				MaxConcurrentTasks: 1,
				CustomEnvSchema:    []EnvVar{{Name: "GITHUB_TOKEN", Secret: true}},
				SkillRefs:          []string{"skill.release-runbook"},
			},
			{
				Ref:                "agent.release-reviewer",
				Name:               "Release Reviewer",
				Runtime:            Runtime{Mode: "cloud", Provider: "multica_agent"},
				Visibility:         "workspace",
				MaxConcurrentTasks: 1,
			},
		},
		Skills: []Skill{{
			Ref:    "skill.release-runbook",
			Name:   "Release Runbook",
			Config: []byte(`{}`),
		}},
	}

	preview, err := PreviewManifest(manifest, Inventory{
		Agents:   []ExistingResource{{ID: "agent-existing-id", Name: "Release Reviewer"}},
		Skills:   []ExistingResource{{ID: "skill-existing-id", Name: "Release Runbook"}},
		Runtimes: []ExistingRuntime{{ID: "runtime-codex-id", Provider: "codex"}},
		ProvidedEnv: []ProvidedEnvVar{{
			AgentRef: "agent.release-lead",
			Name:     "GITHUB_TOKEN",
		}},
	})
	if err != nil {
		t.Fatalf("PreviewManifest returned error: %v", err)
	}

	if preview.Summary.Squads.Create != 1 {
		t.Fatalf("squad create count = %d, want 1", preview.Summary.Squads.Create)
	}
	if preview.Summary.Agents.Create != 1 || preview.Summary.Agents.Reuse != 1 {
		t.Fatalf("agent summary = %#v, want create=1 reuse=1", preview.Summary.Agents)
	}
	if preview.Summary.Skills.Reuse != 1 {
		t.Fatalf("skill reuse count = %d, want 1", preview.Summary.Skills.Reuse)
	}
	if preview.Agents[0].Action != PreviewActionCreate {
		t.Fatalf("first agent action = %q, want create", preview.Agents[0].Action)
	}
	if preview.Agents[0].Runtime.Status != RuntimeRequirementMatched || preview.Agents[0].Runtime.RuntimeID != "runtime-codex-id" {
		t.Fatalf("first agent runtime = %#v, want matched codex runtime", preview.Agents[0].Runtime)
	}
	if len(preview.Agents[0].MissingEnv) != 0 {
		t.Fatalf("first agent missing env = %#v, want none", preview.Agents[0].MissingEnv)
	}
	if preview.Agents[1].Action != PreviewActionReuse || preview.Agents[1].ExistingID != "agent-existing-id" {
		t.Fatalf("second agent = %#v, want reuse existing agent", preview.Agents[1])
	}
	if preview.Agents[1].Runtime.Status != RuntimeRequirementMissing {
		t.Fatalf("second agent runtime status = %q, want missing", preview.Agents[1].Runtime.Status)
	}
}

func TestPreviewManifestAppliesRuntimeMappingsAndReportsEnvGaps(t *testing.T) {
	manifest := Manifest{
		Schema: SchemaVersion,
		Name:   "Mapped Package",
		Agents: []Agent{{
			Ref:                "agent.mapped",
			Name:               "Mapped Agent",
			Runtime:            Runtime{Mode: "local", Provider: "codex"},
			Visibility:         "workspace",
			MaxConcurrentTasks: 1,
			CustomEnvSchema:    []EnvVar{{Name: "OPENAI_API_KEY", Secret: true}},
		}},
	}

	preview, err := PreviewManifest(manifest, Inventory{
		Runtimes:        []ExistingRuntime{{ID: "runtime-claude-id", Provider: "claude"}},
		RuntimeMappings: []RuntimeMapping{{Provider: "codex", RuntimeID: "runtime-claude-id"}},
	})
	if err != nil {
		t.Fatalf("PreviewManifest returned error: %v", err)
	}
	if preview.Agents[0].Runtime.Status != RuntimeRequirementMapped {
		t.Fatalf("runtime status = %q, want mapped", preview.Agents[0].Runtime.Status)
	}
	if preview.Agents[0].Runtime.RuntimeID != "runtime-claude-id" {
		t.Fatalf("runtime id = %q, want mapped runtime", preview.Agents[0].Runtime.RuntimeID)
	}
	if len(preview.Agents[0].MissingEnv) != 1 || preview.Agents[0].MissingEnv[0] != "OPENAI_API_KEY" {
		t.Fatalf("missing env = %#v, want OPENAI_API_KEY", preview.Agents[0].MissingEnv)
	}
	if preview.HasBlockingIssues != true {
		t.Fatalf("has_blocking_issues = false, want true for missing env")
	}
}

func TestPreviewManifestReportsDuplicateNamesAsConflicts(t *testing.T) {
	manifest := Manifest{
		Schema: SchemaVersion,
		Name:   "Duplicate Package",
		Agents: []Agent{
			{Ref: "agent.one", Name: "Duplicate Agent", Runtime: Runtime{Mode: "local"}, MaxConcurrentTasks: 1},
			{Ref: "agent.two", Name: "Duplicate Agent", Runtime: Runtime{Mode: "local"}, MaxConcurrentTasks: 1},
		},
	}

	preview, err := PreviewManifest(manifest, Inventory{})
	if err != nil {
		t.Fatalf("PreviewManifest returned error: %v", err)
	}
	if preview.Summary.Agents.Conflict != 2 {
		t.Fatalf("agent conflict count = %d, want 2", preview.Summary.Agents.Conflict)
	}
	if !preview.HasBlockingIssues {
		t.Fatalf("has_blocking_issues = false, want true")
	}
	for _, agent := range preview.Agents {
		if agent.Action != PreviewActionConflict {
			t.Fatalf("agent action = %q, want conflict", agent.Action)
		}
	}
}
