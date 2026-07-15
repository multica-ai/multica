package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func createSkillForAvailabilityTest(
	t *testing.T,
	userID, name, mode string,
	spaceIDs []string,
) SkillWithFilesResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateSkill(w, newRequestAs(userID, http.MethodPost, "/api/skills", map[string]any{
		"name":                   name,
		"content":                "# " + name,
		"availability_mode":      mode,
		"availability_space_ids": spaceIDs,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSkill(%s): expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var skill SkillWithFilesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &skill); err != nil {
		t.Fatalf("decode Skill: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skill.ID)
	})
	return skill
}

func listSkillsAs(t *testing.T, userID string) []SkillSummaryResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListSkills(w, newRequestAs(userID, http.MethodGet, "/api/skills", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var skills []SkillSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &skills); err != nil {
		t.Fatalf("decode Skills: %v", err)
	}
	return skills
}

func containsSkill(skills []SkillSummaryResponse, id string) bool {
	for _, skill := range skills {
		if skill.ID == id {
			return true
		}
	}
	return false
}

func TestSkillAvailabilityControlsDiscoveryWithoutGrantingSpaceAccess(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	openSpace := createSpaceForAccessTest(t, "Skill Open", "SKLOPN", "open")
	privateSpace := createSpaceForAccessTest(t, "Skill Private", "SKLPRV", "private")
	memberID := createPermissionTestMember(t, "skill-availability-member@multica.test")
	skill := createSkillForAvailabilityTest(t, testUserID, "availability-skill", "private", nil)

	if containsSkill(listSkillsAs(t, memberID), skill.ID) {
		t.Fatal("Private Skill leaked into another member's list")
	}

	update := func(mode string, spaceIDs []string) SkillWithFilesResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.UpdateSkill(w, withURLParam(newRequest(http.MethodPut, "/api/skills/"+skill.ID, map[string]any{
			"availability_mode":      mode,
			"availability_space_ids": spaceIDs,
		}), "id", skill.ID))
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateSkill(%s): expected 200, got %d: %s", mode, w.Code, w.Body.String())
		}
		var updated SkillWithFilesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
			t.Fatalf("decode updated Skill: %v", err)
		}
		return updated
	}

	updated := update("selected_spaces", []string{openSpace.ID})
	if updated.AvailabilityMode != "selected_spaces" || len(updated.AvailabilitySpaceIDs) != 1 {
		t.Fatalf("selected Space response = %+v", updated.SkillResponse)
	}
	if !containsSkill(listSkillsAs(t, memberID), skill.ID) {
		t.Fatal("member who can view selected Open Space cannot discover shared Skill")
	}

	// Knowing a Private Space exists does not make its shared Skills visible.
	update("selected_spaces", []string{privateSpace.ID})
	if containsSkill(listSkillsAs(t, memberID), skill.ID) {
		t.Fatal("Skill shared only to inaccessible Private Space leaked into list")
	}
	w := httptest.NewRecorder()
	testHandler.GetSkill(w, withURLParam(newRequestAs(memberID, http.MethodGet, "/api/skills/"+skill.ID, nil), "id", skill.ID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetSkill via inaccessible Private Space: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	update("workspace", nil)
	if !containsSkill(listSkillsAs(t, memberID), skill.ID) {
		t.Fatal("Workspace-shared Skill missing from member list")
	}

	var canCollaborate bool
	canCollaborate, err := testHandler.Queries.CanCollaborateInWorkspaceSpace(
		context.Background(),
		db.CanCollaborateInWorkspaceSpaceParams{
			WorkspaceID: parseUUID(testWorkspaceID),
			ID:          parseUUID(openSpace.ID),
			UserID:      parseUUID(memberID),
		},
	)
	if err != nil {
		t.Fatalf("CanCollaborateInWorkspaceSpace: %v", err)
	}
	if canCollaborate {
		t.Fatal("discovering a shared Skill unexpectedly granted Space collaboration")
	}
}

func TestTaskSkillBundlesRespectSkillAvailability(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	spaceA := createSpaceForAccessTest(t, "Skill Runtime A", "SKLRTA", "open")
	spaceB := createSpaceForAccessTest(t, "Skill Runtime B", "SKLRTB", "open")
	selected := createSkillForAvailabilityTest(t, testUserID, "runtime-selected-skill", "selected_spaces", []string{spaceA.ID})
	workspaceSkill := createSkillForAvailabilityTest(t, testUserID, "runtime-workspace-skill", "workspace", nil)
	privateOwnerSkill := createSkillForAvailabilityTest(t, testUserID, "runtime-private-owner-skill", "private", nil)
	otherMemberID := createPermissionTestMember(t, "skill-runtime-other@multica.test")
	foreignPrivateSkill := createSkillForAvailabilityTest(t, otherMemberID, "runtime-private-foreign-skill", "private", nil)

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent
		WHERE workspace_id = $1 AND owner_id = $2
		ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("load seeded Agent: %v", err)
	}
	for _, skillID := range []string{selected.ID, workspaceSkill.ID, privateOwnerSkill.ID, foreignPrivateSkill.ID} {
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, agentID, skillID); err != nil {
			t.Fatalf("attach Skill %s: %v", skillID, err)
		}
	}

	issueA := createIssueForTest(t, map[string]any{"title": "Skill task A", "space_id": spaceA.ID})
	issueB := createIssueForTest(t, map[string]any{"title": "Skill task B", "space_id": spaceB.ID})

	namesFor := func(task db.AgentTaskQueue) map[string]bool {
		t.Helper()
		loaded := testHandler.TaskService.LoadAgentSkillsForTask(context.Background(), task)
		names := make(map[string]bool, len(loaded))
		for _, skill := range loaded {
			names[skill.Name] = true
		}
		return names
	}

	base := db.AgentTaskQueue{AgentID: parseUUID(agentID)}
	contextless := namesFor(base)
	if contextless[selected.Name] || !contextless[workspaceSkill.Name] || !contextless[privateOwnerSkill.Name] || contextless[foreignPrivateSkill.Name] {
		t.Fatalf("context-free Skill set = %v", contextless)
	}

	inA := base
	inA.IssueID = parseUUID(issueA.ID)
	namesA := namesFor(inA)
	if !namesA[selected.Name] || !namesA[workspaceSkill.Name] || !namesA[privateOwnerSkill.Name] || namesA[foreignPrivateSkill.Name] {
		t.Fatalf("Space A Skill set = %v", namesA)
	}

	inB := base
	inB.IssueID = parseUUID(issueB.ID)
	namesB := namesFor(inB)
	if namesB[selected.Name] || !namesB[workspaceSkill.Name] || !namesB[privateOwnerSkill.Name] || namesB[foreignPrivateSkill.Name] {
		t.Fatalf("Space B Skill set = %v", namesB)
	}
}
