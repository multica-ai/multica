package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func importURLParams(req *http.Request, targetID string) *http.Request {
	return testutil.WithURLParams(req, "id", targetID)
}

func setupImportSource(t *testing.T) (sourceID, sourceAgentID, sourceSquadID, sourceSkillName string) {
	t.Helper()
	suffix := uuid.NewString()[:8]
	sourceID = dbfx.Workspace(t, "Import Source "+suffix, "import-src-"+suffix)
	dbfx.Member(t, sourceID, testUserID, "owner")
	srcRuntime := dbfx.Runtime(t, "import-src-rt-"+suffix, testutil.Cols{
		"workspace_id": sourceID,
		"visibility":   "public",
	})
	sourceAgentID = dbfx.Agent(t, "Import Lead "+suffix, srcRuntime, testutil.Cols{
		"workspace_id":    sourceID,
		"description":     "Lead agent",
		"instructions":    "Lead the squad. mention://agent/placeholder",
		"custom_env":      testutil.Raw(`'{"SECRET":"never-copy"}'::jsonb`),
		"mcp_config":      testutil.Raw(`'{"mcpServers":{"x":{"url":"https://secret.example"}}}'::jsonb`),
		"custom_args":     testutil.Raw(`'["--verbose"]'::jsonb`),
		"visibility":      "workspace",
		"permission_mode": "public_to",
	})
	dbfx.Exec(t, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, sourceAgentID, sourceID)
	dbfx.Exec(t, `UPDATE agent SET instructions = $1 WHERE id = $2`,
		"Lead the squad. mention://agent/"+sourceAgentID, sourceAgentID)

	sourceSkillName = "import-review-" + suffix
	srcSkill := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": sourceID,
		"name":         sourceSkillName,
		"description":  "Review",
		"content":      "review well",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})
	dbfx.InsertNoID(t, "agent_skill", testutil.Cols{
		"agent_id": sourceAgentID,
		"skill_id": srcSkill,
	}, "agent_id = $1 AND skill_id = $2", sourceAgentID, srcSkill)

	sourceSquadID = dbfx.Squad(t, "Import Delivery "+suffix, sourceAgentID, testutil.Cols{
		"workspace_id": sourceID,
		"description":  "Delivery squad",
		"instructions": "Hand work to mention://agent/" + sourceAgentID,
	})
	dbfx.SquadMember(t, sourceSquadID, "agent", sourceAgentID, testutil.Cols{"role": "leader"})
	return sourceID, sourceAgentID, sourceSquadID, sourceSkillName
}

func TestPreviewWorkspaceImport(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	sourceID, sourceAgentID, sourceSquadID, _ := setupImportSource(t)

	w := httptest.NewRecorder()
	req := importURLParams(
		newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/import-preview?source_workspace_id="+sourceID, nil),
		testWorkspaceID,
	)
	testHandler.PreviewWorkspaceImport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var preview WorkspaceImportPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.SourceWorkspaceID != sourceID {
		t.Fatalf("source workspace id = %s, want %s", preview.SourceWorkspaceID, sourceID)
	}
	foundAgent, foundSquad := false, false
	for _, a := range preview.Agents {
		if a.ID == sourceAgentID {
			foundAgent = true
			if a.Name == "" {
				t.Fatal("preview agent name is empty")
			}
		}
	}
	for _, s := range preview.Squads {
		if s.ID == sourceSquadID {
			foundSquad = true
			if s.LeaderID != sourceAgentID {
				t.Fatalf("squad leader = %s, want %s", s.LeaderID, sourceAgentID)
			}
		}
	}
	if !foundAgent || !foundSquad {
		t.Fatalf("preview missing agent=%v squad=%v: %+v", foundAgent, foundSquad, preview)
	}
}

func TestImportFromWorkspace_CopiesAgentAndSquad(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	sourceID, sourceAgentID, sourceSquadID, skillName := setupImportSource(t)
	targetSkill := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         skillName,
		"description":  "Review",
		"content":      "target copy",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})

	runtimeID := handlerTestRuntimeID(t)
	w := httptest.NewRecorder()
	req := importURLParams(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/import-from-workspace", map[string]any{
		"source_workspace_id": sourceID,
		"runtime_id":          runtimeID,
		"import_all":          true,
	}), testWorkspaceID)
	testHandler.ImportFromWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("import: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var result WorkspaceImportResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode import: %v", err)
	}
	if len(result.Agents) == 0 || result.Agents[0].Status != "created" {
		t.Fatalf("expected created agent, got %+v", result.Agents)
	}
	if len(result.Squads) == 0 || result.Squads[0].Status != "created" {
		t.Fatalf("expected created squad, got %+v", result.Squads)
	}
	newAgentID := result.Agents[0].ID
	if newAgentID == sourceAgentID {
		t.Fatal("imported agent reused source id")
	}

	var env, mcp []byte
	var runtimeIDCopied, instructions string
	if err := testPool.QueryRow(context.Background(),
		`SELECT custom_env, mcp_config, runtime_id::text, instructions FROM agent WHERE id = $1`,
		newAgentID,
	).Scan(&env, &mcp, &runtimeIDCopied, &instructions); err != nil {
		t.Fatalf("load imported agent: %v", err)
	}
	if strings.Contains(string(env), "SECRET") {
		t.Fatalf("custom_env leaked onto import: %s", env)
	}
	if strings.Contains(string(mcp), "secret.example") {
		t.Fatalf("mcp_config leaked onto import: %s", mcp)
	}
	if runtimeIDCopied != runtimeID {
		t.Fatalf("runtime_id = %s, want %s", runtimeIDCopied, runtimeID)
	}
	if !strings.Contains(instructions, newAgentID) {
		t.Fatalf("instructions were not remapped: %s", instructions)
	}
	if strings.Contains(instructions, sourceAgentID) {
		t.Fatalf("instructions still mention source agent: %s", instructions)
	}

	var attached int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`,
		newAgentID, targetSkill,
	).Scan(&attached); err != nil {
		t.Fatalf("count attached skills: %v", err)
	}
	if attached != 1 {
		t.Fatalf("attached skills = %d, want 1", attached)
	}

	var squadLeader, squadInstructions string
	if err := testPool.QueryRow(context.Background(),
		`SELECT leader_id::text, instructions FROM squad WHERE id = $1`,
		result.Squads[0].ID,
	).Scan(&squadLeader, &squadInstructions); err != nil {
		t.Fatalf("load imported squad: %v", err)
	}
	if squadLeader != newAgentID {
		t.Fatalf("squad leader = %s, want remapped %s", squadLeader, newAgentID)
	}
	if strings.Contains(squadInstructions, sourceAgentID) {
		t.Fatalf("squad instructions still mention source agent: %s", squadInstructions)
	}
	if sourceSquadID == result.Squads[0].ID {
		t.Fatal("imported squad reused source id")
	}
}

func TestImportFromWorkspace_SkipNameConflict(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	sourceID, sourceAgentID, _, _ := setupImportSource(t)
	var sourceName string
	if err := testPool.QueryRow(context.Background(), `SELECT name FROM agent WHERE id = $1`, sourceAgentID).Scan(&sourceName); err != nil {
		t.Fatalf("source name: %v", err)
	}
	existing := createHandlerTestAgent(t, sourceName, nil)

	w := httptest.NewRecorder()
	req := importURLParams(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/import-from-workspace", map[string]any{
		"source_workspace_id": sourceID,
		"runtime_id":          handlerTestRuntimeID(t),
		"agent_ids":           []string{sourceAgentID},
	}), testWorkspaceID)
	testHandler.ImportFromWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("import: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var result WorkspaceImportResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Agents) != 1 || result.Agents[0].Status != "skipped" || result.Agents[0].ID != existing {
		t.Fatalf("want skipped reuse of %s, got %+v", existing, result.Agents)
	}
}

func TestImportFromWorkspace_SkipDoesNotWireUnwitablePrivateAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	sourceID, sourceAgentID, _, _ := setupImportSource(t)
	var sourceName string
	if err := testPool.QueryRow(ctx, `SELECT name FROM agent WHERE id = $1`, sourceAgentID).Scan(&sourceName); err != nil {
		t.Fatalf("source name: %v", err)
	}

	memberID := createPlainMember(t, "import-wire-"+uuid.NewString()[:8]+"@multica.test")
	dbfx.Member(t, sourceID, memberID, "member")

	privateID := dbfx.Agent(t, sourceName, handlerTestRuntimeID(t), testutil.Cols{
		"visibility":      "private",
		"permission_mode": "private",
		"owner_id":        testUserID,
	})
	publicRT := dbfx.Runtime(t, "import-public-rt-"+uuid.NewString()[:8], testutil.Cols{
		"workspace_id": testWorkspaceID,
		"visibility":   "public",
	})

	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(memberID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	privateAgent, err := testHandler.Queries.GetAgent(ctx, parseUUID(privateID))
	if err != nil {
		t.Fatalf("load private agent: %v", err)
	}
	if testHandler.memberCanWireAgent(ctx, memberRow, privateAgent, testWorkspaceID) {
		t.Fatal("fixture broken: memberCanWireAgent must be false for the others' private agent")
	}

	w := httptest.NewRecorder()
	req := importURLParams(newRequestAs(memberID, http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/import-from-workspace", map[string]any{
		"source_workspace_id": sourceID,
		"runtime_id":          publicRT,
		"import_all":          true,
	}), testWorkspaceID)
	testHandler.ImportFromWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("import: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var result WorkspaceImportResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Agents) != 1 || result.Agents[0].Status != "skipped" {
		t.Fatalf("want skipped agent, got %+v", result.Agents)
	}
	for _, squad := range result.Squads {
		if squad.Status == "created" {
			t.Fatalf("must not create a squad wired through an unwitable agent: %+v", result.Squads)
		}
	}
	var wired int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM squad_member WHERE member_type = 'agent' AND member_id = $1`, privateID).Scan(&wired); err != nil {
		t.Fatalf("count wired: %v", err)
	}
	if wired != 0 {
		t.Fatalf("private agent wired into %d squad(s)", wired)
	}
	var led int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM squad WHERE workspace_id = $1 AND leader_id = $2`, testWorkspaceID, privateID).Scan(&led); err != nil {
		t.Fatalf("count led: %v", err)
	}
	if led != 0 {
		t.Fatalf("private agent became leader of %d squad(s)", led)
	}
}

func TestImportFromWorkspace_RenameNameConflict(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	sourceID, sourceAgentID, _, _ := setupImportSource(t)
	var sourceName string
	if err := testPool.QueryRow(context.Background(), `SELECT name FROM agent WHERE id = $1`, sourceAgentID).Scan(&sourceName); err != nil {
		t.Fatalf("source name: %v", err)
	}
	_ = createHandlerTestAgent(t, sourceName, nil)

	w := httptest.NewRecorder()
	req := importURLParams(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/import-from-workspace", map[string]any{
		"source_workspace_id": sourceID,
		"runtime_id":          handlerTestRuntimeID(t),
		"agent_ids":           []string{sourceAgentID},
		"on_conflict":         "rename",
	}), testWorkspaceID)
	testHandler.ImportFromWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("import: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var result WorkspaceImportResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Agents) != 1 || result.Agents[0].Status != "created" {
		t.Fatalf("want created rename, got %+v", result.Agents)
	}
	if result.Agents[0].Name != sourceName+" (imported)" {
		t.Fatalf("renamed name = %q, want %q", result.Agents[0].Name, sourceName+" (imported)")
	}
}

func TestImportFromWorkspace_SourceNonMemberForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	suffix := uuid.NewString()[:8]
	foreign := dbfx.Workspace(t, "Foreign "+suffix, "import-foreign-"+suffix)

	w := httptest.NewRecorder()
	req := importURLParams(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/import-from-workspace", map[string]any{
		"source_workspace_id": foreign,
		"runtime_id":          handlerTestRuntimeID(t),
		"import_all":          true,
	}), testWorkspaceID)
	testHandler.ImportFromWorkspace(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportFromWorkspace_SameWorkspaceRejected(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := importURLParams(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/import-from-workspace", map[string]any{
		"source_workspace_id": testWorkspaceID,
		"runtime_id":          handlerTestRuntimeID(t),
		"import_all":          true,
	}), testWorkspaceID)
	testHandler.ImportFromWorkspace(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportFromWorkspace_AgentActorDenied(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	sourceID, _, _, _ := setupImportSource(t)
	agentID := createHandlerTestAgent(t, "import-actor-"+uuid.NewString()[:8], nil)
	taskID := createHandlerTestTaskForAgent(t, agentID)

	w := httptest.NewRecorder()
	req := importURLParams(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/import-from-workspace", map[string]any{
		"source_workspace_id": sourceID,
		"runtime_id":          handlerTestRuntimeID(t),
		"import_all":          true,
	}), testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.ImportFromWorkspace(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPreviewWorkspaceImport_RequiresSource(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := importURLParams(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/import-preview", nil), testWorkspaceID)
	testHandler.PreviewWorkspaceImport(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportFromWorkspace_MissingRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	sourceID, _, _, _ := setupImportSource(t)
	w := httptest.NewRecorder()
	req := importURLParams(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/import-from-workspace", map[string]any{
		"source_workspace_id": sourceID,
		"import_all":          true,
	}), testWorkspaceID)
	testHandler.ImportFromWorkspace(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAllocateImportedName(t *testing.T) {
	existing := map[string]importNameEntry{
		"Lead": {id: "a1"},
	}
	name, reused, skip := allocateImportedName(existing, "Lead", "skip")
	if !skip || reused != "a1" || name != "Lead" {
		t.Fatalf("skip = (%s, %s, %v)", name, reused, skip)
	}
	name, reused, skip = allocateImportedName(existing, "Lead", "rename")
	if skip || reused != "" || name != "Lead (imported)" {
		t.Fatalf("rename = (%s, %s, %v)", name, reused, skip)
	}
	name, _, skip = allocateImportedName(existing, "Fresh", "skip")
	if skip || name != "Fresh" {
		t.Fatalf("fresh = (%s, %v)", name, skip)
	}
}

func TestRewriteImportedMentions(t *testing.T) {
	got := rewriteImportedMentions("see mention://agent/old-id please", map[string]string{"old-id": "new-id"})
	want := "see mention://agent/new-id please"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if rewriteImportedMentions("", map[string]string{"a": "b"}) != "" {
		t.Fatal("empty text should stay empty")
	}
	self := rewriteImportedMentions("mention://agent/src-id", map[string]string{"src-id": "dst-id"})
	if self != "mention://agent/dst-id" {
		t.Fatalf("self mention = %q", self)
	}
}

func TestSelectImportIDs_RejectsUnknownAgent(t *testing.T) {
	w := httptest.NewRecorder()
	_, _, ok := selectImportIDs(w, workspaceImportRequest{
		AgentIDs: []string{"missing"},
	}, nil, nil)
	if ok {
		t.Fatal("expected rejection")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestSelectImportIDs_SquadPullsAgentMembers(t *testing.T) {
	w := httptest.NewRecorder()
	agents := []workspaceImportPreviewAgent{{ID: "a1", Name: "Lead"}}
	squads := []workspaceImportPreviewSquad{{
		ID:             "s1",
		Name:           "Delivery",
		AgentMemberIDs: []string{"a1"},
	}}
	agentIDs, squadIDs, ok := selectImportIDs(w, workspaceImportRequest{
		SquadIDs: []string{"s1"},
	}, agents, squads)
	if !ok {
		t.Fatalf("unexpected reject: %s", w.Body.String())
	}
	if fmt.Sprint(agentIDs) != "[a1]" || fmt.Sprint(squadIDs) != "[s1]" {
		t.Fatalf("agentIDs=%v squadIDs=%v", agentIDs, squadIDs)
	}
}
