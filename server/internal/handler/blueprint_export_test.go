package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/blueprint"
)

func TestExportBlueprint_ExportsSelectedSquadGraphAndRedactsSecrets(t *testing.T) {
	ctx := context.Background()
	leaderID := insertBlueprintExportAgent(t, "Blueprint Release Lead", map[string]string{
		"GITHUB_TOKEN": "ghp_secret",
	}, []byte(`{"servers":{"github":{"env":{"GITHUB_TOKEN":"ghp_secret"}}}}`))
	skillID := insertBlueprintExportSkill(t, "Blueprint Release Runbook", "# Release")
	insertBlueprintExportSkillFile(t, skillID, "steps/checklist.md", "- Verify changelog")
	linkBlueprintExportAgentSkill(t, leaderID, skillID)
	squadID := insertBlueprintExportSquad(t, "Blueprint Release Squad", leaderID)
	addBlueprintExportSquadMember(t, squadID, "agent", leaderID, "lead")
	addBlueprintExportSquadMember(t, squadID, "member", testUserID, "stakeholder")

	req := newRequest("POST", "/api/blueprints/export?workspace_id="+testWorkspaceID, map[string]any{
		"name":      "Release Package",
		"squad_ids": []string{squadID},
	})
	w := httptest.NewRecorder()

	testHandler.ExportBlueprint(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("ExportBlueprint: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()

	var manifest blueprint.Manifest
	if err := json.Unmarshal([]byte(body), &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Schema != blueprint.SchemaVersion {
		t.Fatalf("schema = %q, want %q", manifest.Schema, blueprint.SchemaVersion)
	}
	if len(manifest.Squads) != 1 || manifest.Squads[0].LeaderRef == "" {
		t.Fatalf("squads = %#v, want selected squad with leader_ref", manifest.Squads)
	}
	if len(manifest.Squads[0].Members) != 1 {
		t.Fatalf("agent members len = %d, want 1 human members excluded", len(manifest.Squads[0].Members))
	}
	if len(manifest.Agents) != 1 {
		t.Fatalf("agents len = %d, want 1", len(manifest.Agents))
	}
	if len(manifest.Agents[0].SkillRefs) != 1 {
		t.Fatalf("agent skill_refs = %#v, want one runbook skill", manifest.Agents[0].SkillRefs)
	}
	if manifest.Agents[0].Runtime.Provider != "handler_test_runtime" {
		t.Fatalf("runtime provider = %q, want handler_test_runtime", manifest.Agents[0].Runtime.Provider)
	}
	if len(manifest.Agents[0].CustomEnvSchema) != 1 || manifest.Agents[0].CustomEnvSchema[0].Name != "GITHUB_TOKEN" {
		t.Fatalf("custom_env_schema = %#v, want redacted GITHUB_TOKEN schema", manifest.Agents[0].CustomEnvSchema)
	}
	if !manifest.Agents[0].MCPConfigRedacted {
		t.Fatalf("mcp_config_redacted = false, want true")
	}
	if len(manifest.Skills) != 1 || len(manifest.Skills[0].Files) != 1 {
		t.Fatalf("skills = %#v, want runbook with one file", manifest.Skills)
	}

	for _, forbidden := range []string{leaderID, skillID, squadID, testWorkspaceID, testRuntimeID, "ghp_secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("manifest leaked local id or secret %q: %s", forbidden, body)
		}
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM squad WHERE id = $1)`, squadID).Scan(&exists); err != nil {
		t.Fatalf("verify squad still exists: %v", err)
	}
	if !exists {
		t.Fatal("export should not mutate source squad")
	}
}

func insertBlueprintExportAgent(t *testing.T, name string, customEnv map[string]string, mcpConfig []byte) string {
	t.Helper()
	envRaw, err := json.Marshal(customEnv)
	if err != nil {
		t.Fatalf("marshal custom env: %v", err)
	}
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config, model, thinking_level
		)
		VALUES ($1, $2, 'Blueprint export fixture', 'cloud', '{"workdir":"/private/tmp/should-not-export"}'::jsonb,
			$3, 'private', 2, $4, 'Run the release process.', $5::jsonb, '["--safe-mode"]'::jsonb, $6, 'gpt-5.4', 'medium')
		RETURNING id
	`, testWorkspaceID, name, testRuntimeID, testUserID, envRaw, mcpConfig).Scan(&agentID); err != nil {
		t.Fatalf("insert blueprint export agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func insertBlueprintExportSkill(t *testing.T, name, content string) string {
	t.Helper()
	name = name + "-" + t.Name()
	var skillID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, 'Blueprint export fixture', $3, '{"scope":"release"}'::jsonb, $4)
		RETURNING id
	`, testWorkspaceID, name, content, testUserID).Scan(&skillID); err != nil {
		t.Fatalf("insert blueprint export skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skillID)
	})
	return skillID
}

func insertBlueprintExportSkillFile(t *testing.T, skillID, filePath, content string) {
	t.Helper()
	var fileID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill_file (skill_id, path, content)
		VALUES ($1, $2, $3)
		RETURNING id
	`, skillID, filePath, content).Scan(&fileID); err != nil {
		t.Fatalf("insert blueprint export skill file: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill_file WHERE id = $1`, fileID)
	})
}

func linkBlueprintExportAgentSkill(t *testing.T, agentID, skillID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_skill (agent_id, skill_id)
		VALUES ($1, $2)
	`, agentID, skillID); err != nil {
		t.Fatalf("link blueprint export agent skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, agentID, skillID)
	})
}

func insertBlueprintExportSquad(t *testing.T, name, leaderID string) string {
	t.Helper()
	name = name + "-" + t.Name()
	var squadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id, instructions)
		VALUES ($1, $2, 'Blueprint export fixture', $3, $4, 'Coordinate the release squad.')
		RETURNING id
	`, testWorkspaceID, name, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("insert blueprint export squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})
	return squadID
}

func addBlueprintExportSquadMember(t *testing.T, squadID, memberType, memberID, role string) {
	t.Helper()
	var rowID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad_member (squad_id, member_type, member_id, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, squadID, memberType, memberID, role).Scan(&rowID); err != nil {
		t.Fatalf("insert blueprint export squad member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE id = $1`, rowID)
	})
}
