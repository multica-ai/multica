package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestExistingSkillIdentityByNameReturnsIDAndName(t *testing.T) {
	namePrefix := "duplicate-import-identity"
	name := namePrefix + "-" + t.Name()
	skillID := insertHandlerTestSkill(t, namePrefix, "# Duplicate import identity")

	existing, ok, err := testHandler.existingSkillIdentityByName(context.Background(), parseUUID(testWorkspaceID), name)
	if err != nil {
		t.Fatalf("existingSkillIdentityByName: %v", err)
	}
	if !ok {
		t.Fatal("expected existing skill identity to be found")
	}
	if existing.ID != skillID || existing.Name != name {
		t.Fatalf("existing skill = %#v, want id %s name %s", existing, skillID, name)
	}
}

func TestWriteSkillImportDuplicateConflictIncludesExistingSkill(t *testing.T) {
	w := httptest.NewRecorder()
	writeSkillImportDuplicateConflict(w, ExistingSkillIdentity{ID: "skill-123", Name: "review-helper"})

	if w.Code != 409 {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "a skill with this name already exists" {
		t.Fatalf("error = %v", body["error"])
	}
	existing, ok := body["existing_skill"].(map[string]any)
	if !ok {
		t.Fatalf("existing_skill missing or wrong type: %#v", body["existing_skill"])
	}
	if existing["id"] != "skill-123" || existing["name"] != "review-helper" {
		t.Fatalf("existing_skill = %#v", existing)
	}
}
