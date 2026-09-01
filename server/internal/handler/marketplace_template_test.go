package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestMarketplaceTemplateSnapshotWireShapeExcludesRuntimeAndSecrets(t *testing.T) {
	snapshot := MarketplaceTemplateSnapshot{
		Version:    marketplaceTemplateSnapshotVersion,
		SourceType: "agent",
		Agents: []MarketplaceTemplateAgentSnapshot{{
			Key: "agent_1", Name: "Reviewer", Instructions: "Review the change",
			MaxConcurrentTasks: 1, SkillKeys: []string{"skill_1"},
		}},
		Skills: []MarketplaceTemplateSkillSnapshot{{
			Key: "skill_1", Name: "review", Content: "# Review",
			Config: json.RawMessage(`{}`), Files: []MarketplaceTemplateSkillFileSnapshot{},
		}},
	}

	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	for _, forbidden := range []string{
		`"runtime_id"`, `"runtime_config"`, `"custom_env"`, `"mcp_config"`,
		`"composio_toolkit_allowlist"`, `"task"`, `"history"`,
	} {
		if strings.Contains(wire, forbidden) {
			t.Fatalf("snapshot contains forbidden field %q: %s", forbidden, wire)
		}
	}
	for _, required := range []string{"instructions", "skill_keys", "files"} {
		if !strings.Contains(wire, required) {
			t.Fatalf("snapshot is missing required field %q: %s", required, wire)
		}
	}
}

func TestValidateMarketplaceSnapshotRejectsMissingSquadLeader(t *testing.T) {
	err := validateMarketplaceSnapshot(MarketplaceTemplateSnapshot{
		Version:    marketplaceTemplateSnapshotVersion,
		SourceType: "squad",
		Agents:     []MarketplaceTemplateAgentSnapshot{{Key: "agent_1", Name: "Worker"}},
		Squad: &MarketplaceTemplateSquadSnapshot{
			Name: "Delivery", LeaderKey: "agent_2",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "leader") {
		t.Fatalf("validateMarketplaceSnapshot() error = %v, want missing leader", err)
	}
}

func TestNormaliseMarketplaceTemplateTags(t *testing.T) {
	tags, err := normaliseMarketplaceTemplateTags([]string{" Delivery ", "delivery", "Quality"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0] != "Delivery" || tags[1] != "Quality" {
		t.Fatalf("normaliseMarketplaceTemplateTags() = %#v", tags)
	}
}

func TestNextMarketplaceAgentNameAvoidsWorkspaceConflicts(t *testing.T) {
	used := map[string]struct{}{"reviewer": {}, "reviewer (2)": {}}
	if got := nextMarketplaceAgentName("Reviewer", used); got != "Reviewer (3)" {
		t.Fatalf("nextMarketplaceAgentName() = %q, want Reviewer (3)", got)
	}
}

func TestMarketplaceTemplateCreateAndApplyDoesNotCopySecrets(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "marketplace-safe-source", nil)
	skillID := insertHandlerTestSkill(t, "marketplace-safe-skill", "# Safe skill")
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent
		SET custom_env = '{"TOKEN":"secret-env"}'::jsonb,
		    mcp_config = '{"headers":{"Authorization":"secret-mcp"}}'::jsonb,
		    runtime_config = '{"gateway":{"token":"secret-runtime"}}'::jsonb
		WHERE id = $1`, agentID); err != nil {
		t.Fatalf("seed source secrets: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`, agentID, skillID,
	); err != nil {
		t.Fatalf("attach source skill: %v", err)
	}

	createW := httptest.NewRecorder()
	testHandler.CreateMarketplaceTemplate(createW, squadScopeReq("", http.MethodPost, "/api/templates", map[string]any{
		"source_type": "agent",
		"source_id":   agentID,
		"name":        "Safe reviewer",
		"description": "A reusable reviewer with a deliberately secret-bearing source agent.",
		"visibility":  "workspace",
	}, nil))
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateMarketplaceTemplate: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	for _, secret := range []string{"secret-env", "secret-mcp", "secret-runtime"} {
		if strings.Contains(createW.Body.String(), secret) {
			t.Fatalf("template response leaked %q: %s", secret, createW.Body.String())
		}
	}
	var created MarketplaceTemplateResponse
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode template: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM marketplace_template WHERE id = $1`, created.ID)
	})
	listW := httptest.NewRecorder()
	testHandler.ListMarketplaceTemplates(listW, squadScopeReq("", http.MethodGet, "/api/templates?scope=workspace", nil, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("ListMarketplaceTemplates: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	if strings.Contains(listW.Body.String(), `"snapshot"`) {
		t.Fatalf("template list must not include full snapshots: %s", listW.Body.String())
	}
	if !strings.Contains(listW.Body.String(), `"agent_count":1`) || !strings.Contains(listW.Body.String(), `"preview_agents"`) {
		t.Fatalf("template list is missing derived summary fields: %s", listW.Body.String())
	}

	var runtimeID pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load source runtime: %v", err)
	}
	applyW := httptest.NewRecorder()
	testHandler.ApplyMarketplaceTemplate(applyW, squadScopeReq("", http.MethodPost, "/api/templates/apply", map[string]any{
		"runtime_ids": map[string]string{"agent_1": uuidToString(runtimeID)},
	}, map[string]string{"id": created.ID}))
	if applyW.Code != http.StatusCreated {
		t.Fatalf("ApplyMarketplaceTemplate: expected 201, got %d: %s", applyW.Code, applyW.Body.String())
	}
	var applied struct {
		AgentIDs map[string]string `json:"agent_ids"`
	}
	if err := json.Unmarshal(applyW.Body.Bytes(), &applied); err != nil {
		t.Fatalf("decode apply response: %v", err)
	}
	importedAgentID := applied.AgentIDs["agent_1"]
	if importedAgentID == "" {
		t.Fatalf("apply response has no imported agent: %s", applyW.Body.String())
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_skill WHERE agent_id = $1`, importedAgentID)
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, importedAgentID)
	})
	var customEnv, runtimeConfig []byte
	var mcpConfig pgtype.Text
	if err := testPool.QueryRow(context.Background(), `
		SELECT custom_env, runtime_config, mcp_config::text
		FROM agent WHERE id = $1`, importedAgentID).Scan(&customEnv, &runtimeConfig, &mcpConfig); err != nil {
		t.Fatalf("load imported agent: %v", err)
	}
	if string(customEnv) != "{}" || string(runtimeConfig) != "{}" || mcpConfig.Valid {
		t.Fatalf("imported agent copied execution secrets: custom_env=%s runtime_config=%s mcp=%v", customEnv, runtimeConfig, mcpConfig)
	}
}

func TestMarketplaceSquadTemplatePreservesLeaderRolesAndInstructions(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "marketplace-squad-leader", nil)
	workerID := createHandlerTestAgent(t, "marketplace-squad-worker", nil)
	sourceSquad := createSquadAs(t, "", "Marketplace source squad", leaderID)

	addW := httptest.NewRecorder()
	testHandler.AddSquadMember(addW, squadScopeReq("", http.MethodPost, "/api/squads/members", map[string]any{
		"member_type": "agent",
		"member_id":   workerID,
		"role":        "implementation",
	}, map[string]string{"id": sourceSquad.ID}))
	if addW.Code != http.StatusCreated {
		t.Fatalf("AddSquadMember: expected 201, got %d: %s", addW.Code, addW.Body.String())
	}
	updateW := httptest.NewRecorder()
	testHandler.UpdateSquad(updateW, squadScopeReq("", http.MethodPut, "/api/squads", map[string]any{
		"instructions": "The leader delegates implementation work to the worker.",
	}, map[string]string{"id": sourceSquad.ID}))
	if updateW.Code != http.StatusOK {
		t.Fatalf("UpdateSquad: expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}

	createW := httptest.NewRecorder()
	testHandler.CreateMarketplaceTemplate(createW, squadScopeReq("", http.MethodPost, "/api/templates", map[string]any{
		"source_type": "squad",
		"source_id":   sourceSquad.ID,
		"name":        "Delivery squad",
		"description": "A two-agent squad used to verify leader, role, and instruction preservation.",
		"visibility":  "private",
	}, nil))
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateMarketplaceTemplate(squad): expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var template MarketplaceTemplateResponse
	if err := json.Unmarshal(createW.Body.Bytes(), &template); err != nil {
		t.Fatalf("decode squad template: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM marketplace_template WHERE id = $1`, template.ID)
	})
	if template.Snapshot == nil || template.Snapshot.Squad == nil || len(template.Snapshot.Agents) != 2 {
		t.Fatalf("squad template snapshot is incomplete: %#v", template.Snapshot)
	}

	var runtimeID pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `SELECT runtime_id FROM agent WHERE id = $1`, leaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	applyW := httptest.NewRecorder()
	testHandler.ApplyMarketplaceTemplate(applyW, squadScopeReq("", http.MethodPost, "/api/templates/apply", map[string]any{
		"name": "Imported delivery squad",
		"runtime_ids": map[string]string{
			"agent_1": uuidToString(runtimeID),
			"agent_2": uuidToString(runtimeID),
		},
	}, map[string]string{"id": template.ID}))
	if applyW.Code != http.StatusCreated {
		t.Fatalf("ApplyMarketplaceTemplate(squad): expected 201, got %d: %s", applyW.Code, applyW.Body.String())
	}
	var applied struct {
		AgentIDs map[string]string `json:"agent_ids"`
		SquadID  string            `json:"squad_id"`
	}
	if err := json.Unmarshal(applyW.Body.Bytes(), &applied); err != nil {
		t.Fatalf("decode squad apply response: %v", err)
	}
	if applied.SquadID == "" || len(applied.AgentIDs) != 2 {
		t.Fatalf("squad apply response is incomplete: %s", applyW.Body.String())
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, applied.SquadID)
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, applied.SquadID)
		for _, id := range applied.AgentIDs {
			testPool.Exec(context.Background(), `DELETE FROM agent_skill WHERE agent_id = $1`, id)
			testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, id)
		}
	})

	var name, instructions, leader string
	if err := testPool.QueryRow(context.Background(), `
		SELECT name, instructions, leader_id::text FROM squad WHERE id = $1`, applied.SquadID,
	).Scan(&name, &instructions, &leader); err != nil {
		t.Fatalf("load imported squad: %v", err)
	}
	if name != "Imported delivery squad" || instructions != "The leader delegates implementation work to the worker." {
		t.Fatalf("imported squad metadata = (%q, %q)", name, instructions)
	}
	if leader != applied.AgentIDs["agent_1"] {
		t.Fatalf("imported leader = %s, want %s", leader, applied.AgentIDs["agent_1"])
	}
	var workerRole string
	if err := testPool.QueryRow(context.Background(), `
		SELECT role FROM squad_member
		WHERE squad_id = $1 AND member_id = $2`, applied.SquadID, applied.AgentIDs["agent_2"],
	).Scan(&workerRole); err != nil {
		t.Fatalf("load imported worker role: %v", err)
	}
	if workerRole != "implementation" {
		t.Fatalf("imported worker role = %q, want implementation", workerRole)
	}
}

func TestSquadTemplateFileRoundTripExcludesSecrets(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "template-file-leader", nil)
	sourceSquad := createSquadAs(t, "", "Template file source squad", leaderID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent
		SET custom_env = '{"TOKEN":"file-secret-env"}'::jsonb,
		    mcp_config = '{"token":"file-secret-mcp"}'::jsonb,
		    runtime_config = '{"gateway":{"token":"file-secret-runtime"}}'::jsonb
		WHERE id = $1`, leaderID); err != nil {
		t.Fatalf("seed file export secrets: %v", err)
	}
	updateW := httptest.NewRecorder()
	testHandler.UpdateSquad(updateW, squadScopeReq("", http.MethodPut, "/api/squads", map[string]any{
		"instructions": "Delegate every implementation task and wait for evidence.",
	}, map[string]string{"id": sourceSquad.ID}))
	if updateW.Code != http.StatusOK {
		t.Fatalf("UpdateSquad: expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}

	exportW := httptest.NewRecorder()
	testHandler.ExportSquadTemplateFile(exportW, squadScopeReq("", http.MethodGet, "/api/squads/template-file", nil, map[string]string{"id": sourceSquad.ID}))
	if exportW.Code != http.StatusOK {
		t.Fatalf("ExportSquadTemplateFile: expected 200, got %d: %s", exportW.Code, exportW.Body.String())
	}
	if !strings.Contains(exportW.Header().Get("Content-Disposition"), ".multica-template.json") {
		t.Fatalf("missing template file content disposition: %q", exportW.Header().Get("Content-Disposition"))
	}
	for _, secret := range []string{"file-secret-env", "file-secret-mcp", "file-secret-runtime"} {
		if strings.Contains(exportW.Body.String(), secret) {
			t.Fatalf("exported template leaked %q", secret)
		}
	}
	var exportedManifest MarketplaceTemplateFileV2
	if err := json.Unmarshal(exportW.Body.Bytes(), &exportedManifest); err != nil {
		t.Fatalf("decode exported template: %v", err)
	}
	if exportedManifest.Format != marketplaceTemplateV2FileFormat || exportedManifest.SchemaVersion != marketplaceTemplateV2FileVersion {
		t.Fatalf("exported template is not v2: %#v", exportedManifest)
	}
	if _, err := parseMarketplaceTemplateFile(exportW.Body.Bytes()); err != nil {
		t.Fatalf("exported template does not validate: %v", err)
	}

	var runtimeID pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `SELECT runtime_id FROM agent WHERE id = $1`, leaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load source runtime: %v", err)
	}
	applyW := httptest.NewRecorder()
	testHandler.ApplyMarketplaceTemplateFile(applyW, squadScopeReq("", http.MethodPost, "/api/templates/apply-file", map[string]any{
		"manifest": exportedManifest,
		"name":     "Imported from file",
		"runtime_ids": map[string]string{
			"agent_1": uuidToString(runtimeID),
		},
	}, nil))
	if applyW.Code != http.StatusCreated {
		t.Fatalf("ApplyMarketplaceTemplateFile: expected 201, got %d: %s", applyW.Code, applyW.Body.String())
	}
	var applied struct {
		TemplateID string            `json:"template_id"`
		AgentIDs   map[string]string `json:"agent_ids"`
		SquadID    string            `json:"squad_id"`
	}
	if err := json.Unmarshal(applyW.Body.Bytes(), &applied); err != nil {
		t.Fatalf("decode file import response: %v", err)
	}
	if applied.TemplateID != "" || applied.SquadID == "" || applied.AgentIDs["agent_1"] == "" {
		t.Fatalf("file import response is incomplete: %s", applyW.Body.String())
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, applied.SquadID)
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, applied.SquadID)
		for _, id := range applied.AgentIDs {
			testPool.Exec(context.Background(), `DELETE FROM agent_skill WHERE agent_id = $1`, id)
			testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, id)
		}
	})
	var instructions string
	if err := testPool.QueryRow(context.Background(), `SELECT instructions FROM squad WHERE id = $1`, applied.SquadID).Scan(&instructions); err != nil {
		t.Fatalf("load file-imported squad: %v", err)
	}
	if instructions != "Delegate every implementation task and wait for evidence." {
		t.Fatalf("file-imported instructions = %q", instructions)
	}
}

func TestMarketplaceTemplateFileRejectsUnsafeSkillPath(t *testing.T) {
	manifest := MarketplaceTemplateFile{
		Format: marketplaceTemplateFileFormat, Version: marketplaceTemplateFileVersion,
		Name: "Unsafe", SourceType: "agent", SnapshotVersion: marketplaceTemplateSnapshotVersion,
		Snapshot: MarketplaceTemplateSnapshot{
			Version: marketplaceTemplateSnapshotVersion, SourceType: "agent",
			Agents: []MarketplaceTemplateAgentSnapshot{{
				Key: "agent_1", Name: "Unsafe", MaxConcurrentTasks: 1,
				SkillKeys: []string{"skill_1"},
			}},
			Skills: []MarketplaceTemplateSkillSnapshot{{
				Key: "skill_1", Name: "unsafe", Config: json.RawMessage(`{}`),
				Files: []MarketplaceTemplateSkillFileSnapshot{{Path: "../escape.sh", Content: "echo no"}},
			}},
		},
	}
	err := validateMarketplaceTemplateFile(manifest)
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("validateMarketplaceTemplateFile() error = %v, want unsafe path", err)
	}
}

func TestParseMarketplaceTemplateFileV2(t *testing.T) {
	raw := json.RawMessage(`{
      "format":"multica.template",
      "schema_version":2,
      "type":"squad",
      "metadata":{"name":"Delivery","description":"Portable squad","tags":[],"use_cases":"","usage_notes":""},
      "resources":{
        "agents":[{"key":"lead","name":"Lead","description":"","instructions":"Delegate","max_concurrent_tasks":1,"skill_refs":["review"]}],
        "skills":[{"key":"review","name":"review","description":"","content":"# Review","source_type":"file","files":[]}]
      },
      "spec":{"name":"Delivery","description":"Portable squad","leader_ref":"lead","members":[{"agent_ref":"lead","role":"leader"}]}
    }`)
	manifest, err := parseMarketplaceTemplateFile(raw)
	if err != nil {
		t.Fatalf("parseMarketplaceTemplateFile(v2): %v", err)
	}
	if manifest.SourceType != "squad" || manifest.Snapshot.Squad == nil {
		t.Fatalf("normalized v2 squad is incomplete: %#v", manifest)
	}
	if len(manifest.Snapshot.Agents) != 1 || len(manifest.Snapshot.Skills) != 1 {
		t.Fatalf("normalized v2 resources = %d agents, %d skills", len(manifest.Snapshot.Agents), len(manifest.Snapshot.Skills))
	}
	if manifest.Snapshot.Agents[0].SkillKeys[0] != "review" || manifest.Snapshot.Squad.LeaderKey != "lead" {
		t.Fatalf("normalized v2 references were not mapped: %#v", manifest.Snapshot)
	}
}

func TestParseMarketplaceTemplateFileV1BackwardCompatibility(t *testing.T) {
	raw := json.RawMessage(`{
      "format":"multica-template",
      "version":1,
      "name":"Legacy agent",
      "description":"Exported before schema v2",
      "tags":[],
      "source_type":"agent",
      "snapshot_version":1,
      "snapshot":{
        "version":1,
        "source_type":"agent",
        "agents":[{"key":"agent_1","name":"Legacy agent","instructions":"Help","skill_keys":[]}],
        "skills":[]
      }
    }`)
	manifest, err := parseMarketplaceTemplateFile(raw)
	if err != nil {
		t.Fatalf("parseMarketplaceTemplateFile(v1): %v", err)
	}
	if manifest.SourceType != "agent" || len(manifest.Snapshot.Agents) != 1 {
		t.Fatalf("normalized v1 agent is incomplete: %#v", manifest)
	}
}
