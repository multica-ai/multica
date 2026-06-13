package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// createImportTargetSkill inserts a skill (owned by ownerID) plus the given
// path->content files directly into the DB, returning its id. Used as the
// pre-existing skill that conflict / overwrite imports collide with.
func createImportTargetSkill(t *testing.T, name, ownerID string, files map[string]string) string {
	t.Helper()

	var skillID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, 'original description', '# original', '{}'::jsonb, $3)
		RETURNING id
	`, testWorkspaceID, name, ownerID).Scan(&skillID); err != nil {
		t.Fatalf("create target skill: %v", err)
	}
	for path, content := range files {
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO skill_file (skill_id, path, content) VALUES ($1, $2, $3)
		`, skillID, path, content); err != nil {
			t.Fatalf("create skill file: %v", err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skillID)
	})
	return skillID
}

// bindAgentToSkill creates a workspace agent and binds it to skillID via
// agent_skill, returning the agent id. Lets overwrite tests assert the binding
// survives the re-import.
func bindAgentToSkill(t *testing.T, skillID string) string {
	t.Helper()

	agentName := fmt.Sprintf("overwrite-test-agent-%d", time.Now().UnixNano())
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, agentName, testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)
	`, agentID, skillID); err != nil {
		t.Fatalf("bind agent skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func countAgentSkillBindings(t *testing.T, skillID string) int {
	t.Helper()

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_skill WHERE skill_id = $1
	`, skillID).Scan(&count); err != nil {
		t.Fatalf("count agent_skill: %v", err)
	}
	return count
}

func getSkillRow(t *testing.T, skillID string) (name, description, content, createdBy string) {
	t.Helper()

	if err := testPool.QueryRow(context.Background(), `
		SELECT name, description, content, COALESCE(created_by::text, '')
		FROM skill WHERE id = $1
	`, skillID).Scan(&name, &description, &content, &createdBy); err != nil {
		t.Fatalf("get skill row: %v", err)
	}
	return
}

// reportBundleBody builds the daemon "completed" report body for an import.
func reportBundleBody(name, description, content string, files map[string]string) map[string]any {
	fileList := make([]map[string]any, 0, len(files))
	for p, c := range files {
		fileList = append(fileList, map[string]any{"path": p, "content": c})
	}
	return map[string]any{
		"status": "completed",
		"skill": map[string]any{
			"name":        name,
			"description": description,
			"content":     content,
			"source_path": "~/.claude/skills/review-helper",
			"provider":    "claude",
			"files":       fileList,
		},
	}
}

func initiateLocalSkillImport(t *testing.T, runtimeID string, body map[string]any) string {
	t.Helper()

	w := httptest.NewRecorder()
	req := withURLParams(
		newRequestAsUser(testUserID, http.MethodPost, "/api/runtimes/"+runtimeID+"/local-skills/import", body),
		"runtimeId", runtimeID,
	)
	testHandler.InitiateImportLocalSkill(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("InitiateImportLocalSkill: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var importReq RuntimeLocalSkillImportRequest
	if err := json.NewDecoder(w.Body).Decode(&importReq); err != nil {
		t.Fatalf("decode import request: %v", err)
	}
	return importReq.ID
}

func reportLocalSkillImport(t *testing.T, runtimeID, requestID string, body map[string]any) {
	t.Helper()

	w := httptest.NewRecorder()
	req := withURLParams(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/local-skills/import/"+requestID+"/result", body, testWorkspaceID, "overwrite-test-daemon"),
		"runtimeId", runtimeID,
		"requestId", requestID,
	)
	testHandler.ReportLocalSkillImportResult(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ReportLocalSkillImportResult: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func pollLocalSkillImport(t *testing.T, runtimeID, requestID string) RuntimeLocalSkillImportRequest {
	t.Helper()

	w := httptest.NewRecorder()
	req := withURLParams(
		newRequestAsUser(testUserID, http.MethodGet, "/api/runtimes/"+runtimeID+"/local-skills/import/"+requestID, nil),
		"runtimeId", runtimeID,
		"requestId", requestID,
	)
	testHandler.GetLocalSkillImportRequest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetLocalSkillImportRequest: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got RuntimeLocalSkillImportRequest
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode poll response: %v", err)
	}
	return got
}

// runLocalSkillImport drives initiate -> report -> poll and returns the
// terminal request.
func runLocalSkillImport(t *testing.T, runtimeID string, initBody, reportBody map[string]any) RuntimeLocalSkillImportRequest {
	t.Helper()
	requestID := initiateLocalSkillImport(t, runtimeID, initBody)
	reportLocalSkillImport(t, runtimeID, requestID, reportBody)
	return pollLocalSkillImport(t, runtimeID, requestID)
}

func TestRuntimeLocalSkillImport_ConflictCreatorCanOverwrite(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	name := fmt.Sprintf("conflict-creator-%d", time.Now().UnixNano())
	existingID := createImportTargetSkill(t, name, testUserID, nil)

	got := runLocalSkillImport(t, runtimeID,
		map[string]any{"skill_key": "review-helper", "supports_conflict": true},
		reportBundleBody(name, "incoming description", "# incoming", map[string]string{"a.md": "A"}),
	)

	if got.Status != RuntimeLocalSkillConflict {
		t.Fatalf("status = %s, want conflict", got.Status)
	}
	if got.Conflict == nil {
		t.Fatal("expected conflict metadata")
	}
	if got.Conflict.ExistingSkillID != existingID {
		t.Fatalf("existing_skill_id = %q, want %q", got.Conflict.ExistingSkillID, existingID)
	}
	if got.Conflict.ExistingCreatedBy != testUserID {
		t.Fatalf("existing_created_by = %q, want %q", got.Conflict.ExistingCreatedBy, testUserID)
	}
	if !got.Conflict.CanOverwrite {
		t.Fatal("creator should be allowed to overwrite")
	}
	// A conflict must neither create a second skill nor mutate the original.
	if n := countSkillsByName(t, name); n != 1 {
		t.Fatalf("expected exactly 1 skill named %q, got %d", name, n)
	}
	if _, desc, _, _ := getSkillRow(t, existingID); desc != "original description" {
		t.Fatalf("conflict must not modify the existing skill, description = %q", desc)
	}
}

func TestRuntimeLocalSkillImport_ConflictNonCreatorCannotOverwrite(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	otherUserID := createRuntimeLocalSkillTestMember(t, "member")
	name := fmt.Sprintf("conflict-noncreator-%d", time.Now().UnixNano())
	existingID := createImportTargetSkill(t, name, otherUserID, nil)

	got := runLocalSkillImport(t, runtimeID,
		map[string]any{"skill_key": "review-helper", "supports_conflict": true},
		reportBundleBody(name, "incoming description", "# incoming", nil),
	)

	if got.Status != RuntimeLocalSkillConflict {
		t.Fatalf("status = %s, want conflict", got.Status)
	}
	if got.Conflict == nil {
		t.Fatal("expected conflict metadata")
	}
	if got.Conflict.ExistingSkillID != existingID {
		t.Fatalf("existing_skill_id = %q, want %q", got.Conflict.ExistingSkillID, existingID)
	}
	if got.Conflict.ExistingCreatedBy != otherUserID {
		t.Fatalf("existing_created_by = %q, want %q", got.Conflict.ExistingCreatedBy, otherUserID)
	}
	if got.Conflict.CanOverwrite {
		t.Fatal("a non-creator must not be allowed to overwrite")
	}
}

func TestRuntimeLocalSkillImport_OverwritePreservesIdentityAndBindings(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	name := fmt.Sprintf("overwrite-keep-%d", time.Now().UnixNano())
	existingID := createImportTargetSkill(t, name, testUserID, map[string]string{
		"keep.md":  "old keep",
		"prune.md": "should be removed",
	})
	bindAgentToSkill(t, existingID)

	got := runLocalSkillImport(t, runtimeID,
		map[string]any{"skill_key": "review-helper", "action": "overwrite", "target_skill_id": existingID},
		reportBundleBody(name, "overwritten description", "# overwritten", map[string]string{"keep.md": "new keep"}),
	)

	if got.Status != RuntimeLocalSkillCompleted {
		t.Fatalf("status = %s, want completed (error=%q)", got.Status, got.Error)
	}
	if got.Skill == nil {
		t.Fatal("expected overwritten skill in response")
	}
	// Same row: UUID and creator preserved.
	if got.Skill.ID != existingID {
		t.Fatalf("overwrite must preserve UUID: got %q, want %q", got.Skill.ID, existingID)
	}
	if got.Skill.CreatedBy == nil || *got.Skill.CreatedBy != testUserID {
		t.Fatalf("created_by not preserved: %v", got.Skill.CreatedBy)
	}
	if got.Skill.Description != "overwritten description" {
		t.Fatalf("description not replaced: %q", got.Skill.Description)
	}
	// Files fully replaced: prune.md (absent from the new bundle) is gone.
	if n := countSkillFiles(t, existingID); n != 1 {
		t.Fatalf("expected 1 file after overwrite, got %d", n)
	}
	// Agent binding preserved — the agent must NOT need to re-add the skill.
	if n := countAgentSkillBindings(t, existingID); n != 1 {
		t.Fatalf("expected agent binding to survive overwrite, got %d", n)
	}
}

func TestRuntimeLocalSkillImport_OverwriteNonCreatorFails(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	otherUserID := createRuntimeLocalSkillTestMember(t, "member")
	name := fmt.Sprintf("overwrite-forbidden-%d", time.Now().UnixNano())
	existingID := createImportTargetSkill(t, name, otherUserID, nil)

	got := runLocalSkillImport(t, runtimeID,
		map[string]any{"skill_key": "review-helper", "action": "overwrite", "target_skill_id": existingID},
		reportBundleBody(name, "incoming description", "# incoming", nil),
	)

	if got.Status != RuntimeLocalSkillFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	// Original skill (owned by someone else) must be untouched.
	if _, desc, _, _ := getSkillRow(t, existingID); desc != "original description" {
		t.Fatalf("forbidden overwrite must not mutate the skill, description = %q", desc)
	}
}

func TestRuntimeLocalSkillImport_OverwriteTargetDeletedFails(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	name := fmt.Sprintf("overwrite-deleted-%d", time.Now().UnixNano())
	deletedID := createImportTargetSkill(t, name, testUserID, nil)
	// Simulate the target being deleted between the user's confirm and the
	// daemon report.
	if _, err := testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, deletedID); err != nil {
		t.Fatalf("delete target skill: %v", err)
	}

	got := runLocalSkillImport(t, runtimeID,
		map[string]any{"skill_key": "review-helper", "action": "overwrite", "target_skill_id": deletedID},
		reportBundleBody(name, "incoming description", "# incoming", map[string]string{"a.md": "A"}),
	)

	if got.Status != RuntimeLocalSkillFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	// Must NOT fall back to creating a new skill by name.
	if n := countSkillsByName(t, name); n != 0 {
		t.Fatalf("deleted-target overwrite must not create a skill, got %d", n)
	}
}

func TestRuntimeLocalSkillImport_OverwriteRetryIsIdempotent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	name := fmt.Sprintf("overwrite-idempotent-%d", time.Now().UnixNano())
	existingID := createImportTargetSkill(t, name, testUserID, map[string]string{"old.md": "old"})

	requestID := initiateLocalSkillImport(t, runtimeID, map[string]any{
		"skill_key":       "review-helper",
		"action":          "overwrite",
		"target_skill_id": existingID,
	})

	// First report wins and overwrites the skill.
	reportLocalSkillImport(t, runtimeID, requestID,
		reportBundleBody(name, "first overwrite", "# first", map[string]string{"first.md": "1"}))

	// A retry of the SAME request id with a different bundle must be ignored
	// (the request is already terminal) — no second write.
	reportLocalSkillImport(t, runtimeID, requestID,
		reportBundleBody(name, "second overwrite", "# second", map[string]string{"second.md": "2", "extra.md": "3"}))

	got := pollLocalSkillImport(t, runtimeID, requestID)
	if got.Status != RuntimeLocalSkillCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
	if _, desc, _, _ := getSkillRow(t, existingID); desc != "first overwrite" {
		t.Fatalf("retry must not re-apply, description = %q", desc)
	}
	if n := countSkillFiles(t, existingID); n != 1 {
		t.Fatalf("retry must not re-write files, got %d files", n)
	}
}

// TestRuntimeLocalSkillImport_LegacyClientGetsFailedOnConflict verifies the
// installed-app compatibility gate: a client that does NOT opt into the
// structured-conflict contract keeps the legacy `failed` + "already exists"
// behavior on a same-name collision, instead of the new `conflict` status its
// older poll loop wouldn't understand.
func TestRuntimeLocalSkillImport_LegacyClientGetsFailedOnConflict(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	name := fmt.Sprintf("legacy-conflict-%d", time.Now().UnixNano())
	createImportTargetSkill(t, name, testUserID, nil)

	got := runLocalSkillImport(t, runtimeID,
		// No supports_conflict (and no action) — an old client.
		map[string]any{"skill_key": "review-helper"},
		reportBundleBody(name, "incoming description", "# incoming", nil),
	)

	if got.Status != RuntimeLocalSkillFailed {
		t.Fatalf("status = %s, want failed (legacy contract)", got.Status)
	}
	if got.Conflict != nil {
		t.Fatalf("legacy client must not receive structured conflict metadata: %+v", got.Conflict)
	}
	if got.Error != "a skill with this name already exists" {
		t.Fatalf("error = %q, want legacy already-exists message", got.Error)
	}
}

// TestRuntimeLocalSkillImport_OverwriteNameMismatchFails verifies the guard
// against a stale / wrong target_skill_id: if the target's name no longer
// matches the imported skill, the overwrite fails instead of writing one
// skill's content onto another.
func TestRuntimeLocalSkillImport_OverwriteNameMismatchFails(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	targetName := fmt.Sprintf("overwrite-target-%d", time.Now().UnixNano())
	otherName := fmt.Sprintf("overwrite-other-%d", time.Now().UnixNano())
	targetID := createImportTargetSkill(t, targetName, testUserID, nil)

	// Overwrite targets targetID but the imported bundle is named otherName.
	got := runLocalSkillImport(t, runtimeID,
		map[string]any{"skill_key": "review-helper", "action": "overwrite", "target_skill_id": targetID},
		reportBundleBody(otherName, "incoming description", "# incoming", map[string]string{"a.md": "A"}),
	)

	if got.Status != RuntimeLocalSkillFailed {
		t.Fatalf("status = %s, want failed (name mismatch)", got.Status)
	}
	if _, desc, _, _ := getSkillRow(t, targetID); desc != "original description" {
		t.Fatalf("name-mismatch overwrite must not mutate the target, description = %q", desc)
	}
}
