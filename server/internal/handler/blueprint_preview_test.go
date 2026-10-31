package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/blueprint"
)

func TestPreviewBlueprint_ReturnsImportPlanWithoutMutatingWorkspace(t *testing.T) {
	ctx := context.Background()
	existingSkillID := insertBlueprintExportSkill(t, "Preview Existing Skill", "# Existing")
	existingAgentID := insertBlueprintExportAgent(t, "Preview Existing Agent", map[string]string{}, []byte(`{}`))

	manifest := blueprint.Manifest{
		Schema: blueprint.SchemaVersion,
		Name:   "Preview Package",
		Agents: []blueprint.Agent{
			{
				Ref:                "agent.new",
				Name:               "Preview New Agent",
				Runtime:            blueprint.Runtime{Mode: "local", Provider: "codex"},
				Visibility:         "workspace",
				MaxConcurrentTasks: 1,
				CustomEnvSchema:    []blueprint.EnvVar{{Name: "GITHUB_TOKEN", Secret: true}},
				SkillRefs:          []string{"skill.existing"},
			},
			{
				Ref:                "agent.existing",
				Name:               "Preview Existing Agent",
				Runtime:            blueprint.Runtime{Mode: "cloud", Provider: "missing_provider"},
				Visibility:         "workspace",
				MaxConcurrentTasks: 1,
			},
		},
		Skills: []blueprint.Skill{{
			Ref:    "skill.existing",
			Name:   "Preview Existing Skill-" + t.Name(),
			Config: json.RawMessage(`{}`),
		}},
	}

	beforeAgents := countBlueprintPreviewAgentsNamed(t, "Preview New Agent")

	req := newRequest("POST", "/api/blueprints/preview?workspace_id="+testWorkspaceID, map[string]any{
		"manifest": manifest,
		"runtime_mappings": []map[string]string{{
			"provider":   "codex",
			"runtime_id": testRuntimeID,
		}},
		"provided_env": []map[string]string{{
			"agent_ref": "agent.new",
			"name":      "GITHUB_TOKEN",
		}},
	})
	w := httptest.NewRecorder()

	testHandler.PreviewBlueprint(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PreviewBlueprint: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var preview blueprint.Preview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.Summary.Agents.Create != 1 || preview.Summary.Agents.Reuse != 1 {
		t.Fatalf("agent summary = %#v, want create=1 reuse=1", preview.Summary.Agents)
	}
	if preview.Agents[0].Runtime.Status != blueprint.RuntimeRequirementMapped || preview.Agents[0].Runtime.RuntimeID != testRuntimeID {
		t.Fatalf("new agent runtime = %#v, want mapped test runtime", preview.Agents[0].Runtime)
	}
	if len(preview.Agents[0].MissingEnv) != 0 {
		t.Fatalf("new agent missing env = %#v, want none", preview.Agents[0].MissingEnv)
	}
	if preview.Agents[1].ExistingID != existingAgentID {
		t.Fatalf("existing agent id = %q, want %q", preview.Agents[1].ExistingID, existingAgentID)
	}
	if preview.Agents[1].Runtime.Status != blueprint.RuntimeRequirementMissing {
		t.Fatalf("existing agent runtime = %#v, want missing", preview.Agents[1].Runtime)
	}
	if preview.Skills[0].ExistingID != existingSkillID {
		t.Fatalf("existing skill id = %q, want %q", preview.Skills[0].ExistingID, existingSkillID)
	}

	afterAgents := countBlueprintPreviewAgentsNamed(t, "Preview New Agent")
	if afterAgents != beforeAgents {
		t.Fatalf("preview mutated agent table: before=%d after=%d", beforeAgents, afterAgents)
	}

	var sourceSkillStillExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM skill WHERE id = $1)`, existingSkillID).Scan(&sourceSkillStillExists); err != nil {
		t.Fatalf("verify source skill exists: %v", err)
	}
	if !sourceSkillStillExists {
		t.Fatal("preview should not mutate source skill")
	}
}

func countBlueprintPreviewAgentsNamed(t *testing.T, name string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent
		WHERE workspace_id = $1 AND name = $2
	`, testWorkspaceID, name).Scan(&count); err != nil {
		t.Fatalf("count blueprint preview agents: %v", err)
	}
	return count
}
