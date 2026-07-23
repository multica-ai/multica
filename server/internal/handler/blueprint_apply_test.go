package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/blueprint"
)

type applyBlueprintTestResourceResult struct {
	Ref    string `json:"ref"`
	Name   string `json:"name"`
	Action string `json:"action"`
	ID     string `json:"id"`
}

type applyBlueprintTestResponse struct {
	Preview blueprint.Preview                  `json:"preview"`
	Squads  []applyBlueprintTestResourceResult `json:"squads"`
	Agents  []applyBlueprintTestResourceResult `json:"agents"`
	Skills  []applyBlueprintTestResourceResult `json:"skills"`
}

func TestApplyBlueprint_CreatesResourcesAndReusesOnSecondApply(t *testing.T) {
	ctx := context.Background()
	manifest := blueprint.Manifest{
		Schema: blueprint.SchemaVersion,
		Name:   "Apply Package",
		Skills: []blueprint.Skill{{
			Ref:         "skill.runbook",
			Name:        "Apply Runbook " + t.Name(),
			Description: "Imported runbook skill",
			Content:     "# Release",
			Config:      json.RawMessage(`{"scope":"release"}`),
			Files: []blueprint.SkillFile{{
				Path:    "steps/checklist.md",
				Content: "- Verify changelog",
			}},
		}},
		Agents: []blueprint.Agent{{
			Ref:                "agent.lead",
			Name:               "Apply Release Lead " + t.Name(),
			Description:        "Imported release lead",
			Instructions:       "Run the release process.",
			Runtime:            blueprint.Runtime{Mode: "cloud", Provider: "codex"},
			Visibility:         "workspace",
			MaxConcurrentTasks: 2,
			CustomEnvSchema: []blueprint.EnvVar{{
				Name:     "GITHUB_TOKEN",
				Required: true,
				Secret:   true,
			}},
			CustomArgs: []string{"--safe-mode"},
			SkillRefs:  []string{"skill.runbook"},
		}},
		Squads: []blueprint.Squad{{
			Ref:          "squad.release",
			Name:         "Apply Release Squad " + t.Name(),
			Description:  "Imported release squad",
			Instructions: "Coordinate the release.",
			LeaderRef:    "agent.lead",
			Members: []blueprint.SquadMember{{
				Ref:  "agent.lead",
				Role: "lead",
			}},
		}},
	}

	reqBody := map[string]any{
		"manifest": manifest,
		"runtime_mappings": []map[string]string{{
			"provider":   "codex",
			"runtime_id": testRuntimeID,
		}},
		"provided_env": []map[string]string{{
			"agent_ref": "agent.lead",
			"name":      "GITHUB_TOKEN",
			"value":     "ghp_imported",
		}, {
			"agent_ref": "agent.lead",
			"name":      "EXTRA_SECRET",
			"value":     "should_not_import",
		}},
	}

	first := applyBlueprintForTest(t, reqBody)
	if first.Preview.Summary.Skills.Create != 1 || first.Preview.Summary.Agents.Create != 1 || first.Preview.Summary.Squads.Create != 1 {
		t.Fatalf("first apply summary = %#v, want create=1 for skill/agent/squad", first.Preview.Summary)
	}
	if len(first.Skills) != 1 || first.Skills[0].Action != blueprint.PreviewActionCreate || first.Skills[0].ID == "" {
		t.Fatalf("first apply skills = %#v, want created skill id", first.Skills)
	}
	if len(first.Agents) != 1 || first.Agents[0].Action != blueprint.PreviewActionCreate || first.Agents[0].ID == "" {
		t.Fatalf("first apply agents = %#v, want created agent id", first.Agents)
	}
	if len(first.Squads) != 1 || first.Squads[0].Action != blueprint.PreviewActionCreate || first.Squads[0].ID == "" {
		t.Fatalf("first apply squads = %#v, want created squad id", first.Squads)
	}

	var agentID string
	var customEnvRaw, customArgsRaw []byte
	if err := testPool.QueryRow(ctx, `
		SELECT id, custom_env, custom_args FROM agent
		WHERE workspace_id = $1 AND name = $2
	`, testWorkspaceID, manifest.Agents[0].Name).Scan(&agentID, &customEnvRaw, &customArgsRaw); err != nil {
		t.Fatalf("load imported agent: %v", err)
	}
	if agentID != first.Agents[0].ID {
		t.Fatalf("response agent id = %q, db agent id = %q", first.Agents[0].ID, agentID)
	}
	var customEnv map[string]string
	if err := json.Unmarshal(customEnvRaw, &customEnv); err != nil {
		t.Fatalf("decode imported custom_env: %v", err)
	}
	if customEnv["GITHUB_TOKEN"] != "ghp_imported" {
		t.Fatalf("custom_env = %#v, want provided secret value", customEnv)
	}
	if _, ok := customEnv["EXTRA_SECRET"]; ok {
		t.Fatalf("custom_env = %#v, want undeclared env values ignored", customEnv)
	}
	var customArgs []string
	if err := json.Unmarshal(customArgsRaw, &customArgs); err != nil {
		t.Fatalf("decode imported custom_args: %v", err)
	}
	if len(customArgs) != 1 || customArgs[0] != "--safe-mode" {
		t.Fatalf("custom_args = %#v, want imported args", customArgs)
	}

	var skillID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM skill
		WHERE workspace_id = $1 AND name = $2
	`, testWorkspaceID, manifest.Skills[0].Name).Scan(&skillID); err != nil {
		t.Fatalf("load imported skill: %v", err)
	}
	if skillID != first.Skills[0].ID {
		t.Fatalf("response skill id = %q, db skill id = %q", first.Skills[0].ID, skillID)
	}
	var hasSkillFile bool
	if err := testPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM skill_file
			WHERE skill_id = $1 AND path = 'steps/checklist.md' AND content = '- Verify changelog'
		)
	`, skillID).Scan(&hasSkillFile); err != nil {
		t.Fatalf("verify imported skill file: %v", err)
	}
	if !hasSkillFile {
		t.Fatal("imported skill file was not stored")
	}
	var hasAgentSkill bool
	if err := testPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM agent_skill
			WHERE agent_id = $1 AND skill_id = $2
		)
	`, agentID, skillID).Scan(&hasAgentSkill); err != nil {
		t.Fatalf("verify imported agent skill link: %v", err)
	}
	if !hasAgentSkill {
		t.Fatal("imported agent was not linked to imported skill")
	}

	var squadID, leaderID, instructions string
	if err := testPool.QueryRow(ctx, `
		SELECT id, leader_id, instructions FROM squad
		WHERE workspace_id = $1 AND name = $2
	`, testWorkspaceID, manifest.Squads[0].Name).Scan(&squadID, &leaderID, &instructions); err != nil {
		t.Fatalf("load imported squad: %v", err)
	}
	if squadID != first.Squads[0].ID {
		t.Fatalf("response squad id = %q, db squad id = %q", first.Squads[0].ID, squadID)
	}
	if leaderID != agentID {
		t.Fatalf("squad leader id = %q, want imported agent %q", leaderID, agentID)
	}
	if instructions != manifest.Squads[0].Instructions {
		t.Fatalf("squad instructions = %q, want %q", instructions, manifest.Squads[0].Instructions)
	}
	var hasSquadMember bool
	if err := testPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM squad_member
			WHERE squad_id = $1 AND member_type = 'agent' AND member_id = $2 AND role = 'lead'
		)
	`, squadID, agentID).Scan(&hasSquadMember); err != nil {
		t.Fatalf("verify imported squad member: %v", err)
	}
	if !hasSquadMember {
		t.Fatal("imported squad member was not stored")
	}

	second := applyBlueprintForTest(t, reqBody)
	if second.Preview.Summary.Skills.Reuse != 1 || second.Preview.Summary.Agents.Reuse != 1 || second.Preview.Summary.Squads.Reuse != 1 {
		t.Fatalf("second apply summary = %#v, want reuse=1 for skill/agent/squad", second.Preview.Summary)
	}
	if second.Skills[0].Action != blueprint.PreviewActionReuse || second.Skills[0].ID != skillID {
		t.Fatalf("second apply skills = %#v, want reused skill %q", second.Skills, skillID)
	}
	if second.Agents[0].Action != blueprint.PreviewActionReuse || second.Agents[0].ID != agentID {
		t.Fatalf("second apply agents = %#v, want reused agent %q", second.Agents, agentID)
	}
	if second.Squads[0].Action != blueprint.PreviewActionReuse || second.Squads[0].ID != squadID {
		t.Fatalf("second apply squads = %#v, want reused squad %q", second.Squads, squadID)
	}
	if got := countBlueprintApplyRowsNamed(t, "skill", manifest.Skills[0].Name); got != 1 {
		t.Fatalf("skill count after second apply = %d, want 1", got)
	}
	if got := countBlueprintApplyRowsNamed(t, "agent", manifest.Agents[0].Name); got != 1 {
		t.Fatalf("agent count after second apply = %d, want 1", got)
	}
	if got := countBlueprintApplyRowsNamed(t, "squad", manifest.Squads[0].Name); got != 1 {
		t.Fatalf("squad count after second apply = %d, want 1", got)
	}
}

func TestApplyBlueprint_ReturnsPreviewErrorsWithoutMutatingWorkspace(t *testing.T) {
	manifest := blueprint.Manifest{
		Schema: blueprint.SchemaVersion,
		Name:   "Blocked Apply Package",
		Skills: []blueprint.Skill{{
			Ref:    "skill.blocked",
			Name:   "Blocked Apply Skill " + t.Name(),
			Config: json.RawMessage(`{}`),
		}},
		Agents: []blueprint.Agent{{
			Ref:                "agent.blocked",
			Name:               "Blocked Apply Agent " + t.Name(),
			Runtime:            blueprint.Runtime{Mode: "cloud", Provider: "missing_provider"},
			Visibility:         "workspace",
			MaxConcurrentTasks: 1,
			CustomEnvSchema:    []blueprint.EnvVar{{Name: "MISSING_ENV", Required: true, Secret: true}},
			SkillRefs:          []string{"skill.blocked"},
		}},
	}
	beforeSkillCount := countBlueprintApplyRowsNamed(t, "skill", manifest.Skills[0].Name)
	beforeAgentCount := countBlueprintApplyRowsNamed(t, "agent", manifest.Agents[0].Name)

	req := newRequest("POST", "/api/blueprints/apply?workspace_id="+testWorkspaceID, map[string]any{
		"manifest": manifest,
	})
	w := httptest.NewRecorder()

	testHandler.ApplyBlueprint(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ApplyBlueprint: expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp applyBlueprintTestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode apply response: %v", err)
	}
	if !resp.Preview.HasBlockingIssues || len(resp.Preview.Errors) == 0 {
		t.Fatalf("preview = %#v, want blocking issues", resp.Preview)
	}
	if got := countBlueprintApplyRowsNamed(t, "skill", manifest.Skills[0].Name); got != beforeSkillCount {
		t.Fatalf("skill count after blocked apply = %d, want %d", got, beforeSkillCount)
	}
	if got := countBlueprintApplyRowsNamed(t, "agent", manifest.Agents[0].Name); got != beforeAgentCount {
		t.Fatalf("agent count after blocked apply = %d, want %d", got, beforeAgentCount)
	}
}

func applyBlueprintForTest(t *testing.T, body any) applyBlueprintTestResponse {
	t.Helper()
	req := newRequest("POST", "/api/blueprints/apply?workspace_id="+testWorkspaceID, body)
	w := httptest.NewRecorder()

	testHandler.ApplyBlueprint(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("ApplyBlueprint: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp applyBlueprintTestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode apply response: %v", err)
	}
	return resp
}

func countBlueprintApplyRowsNamed(t *testing.T, table, name string) int {
	t.Helper()
	var query string
	switch table {
	case "skill":
		query = `SELECT count(*) FROM skill WHERE workspace_id = $1 AND name = $2`
	case "agent":
		query = `SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = $2`
	case "squad":
		query = `SELECT count(*) FROM squad WHERE workspace_id = $1 AND name = $2`
	default:
		t.Fatalf("unsupported table %q", table)
	}
	var count int
	if err := testPool.QueryRow(context.Background(), query, testWorkspaceID, name).Scan(&count); err != nil {
		t.Fatalf("count %s rows named %q: %v", table, name, err)
	}
	return count
}
