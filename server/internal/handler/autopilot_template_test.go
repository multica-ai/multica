package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutopilotTemplateCRUDAndOwnership(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	creatorID := createPlainMember(t, "autopilot-template-creator@multica.test")
	otherMemberID := createPlainMember(t, "autopilot-template-other@multica.test")

	create := httptest.NewRecorder()
	testHandler.CreateAutopilotTemplate(create, newRequestAs(creatorID, http.MethodPost, "/api/autopilot-templates", map[string]any{
		"name":                 "Template ownership probe",
		"description":          "Review incoming work",
		"execution_mode":       "create_issue",
		"issue_title_template": "Triage {{date}}",
		"trigger_kind":         "schedule",
		"cron_expression":      "0 9 * * 1-5",
		"timezone":             "UTC",
	}))
	if create.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilotTemplate: expected 201, got %d: %s", create.Code, create.Body.String())
	}
	var template autopilotTemplateResponse
	if err := json.Unmarshal(create.Body.Bytes(), &template); err != nil {
		t.Fatalf("decode template: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot_template WHERE id = $1`, template.ID)
	})

	list := httptest.NewRecorder()
	testHandler.ListAutopilotTemplates(list, newRequest(http.MethodGet, "/api/autopilot-templates", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("ListAutopilotTemplates: expected 200, got %d: %s", list.Code, list.Body.String())
	}
	var listed struct {
		Templates []autopilotTemplateResponse `json:"templates"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode template list: %v", err)
	}
	found := false
	for _, item := range listed.Templates {
		found = found || item.ID == template.ID
	}
	if !found {
		t.Fatal("created template missing from Workspace template list")
	}

	forbidden := httptest.NewRecorder()
	testHandler.UpdateAutopilotTemplate(forbidden, withURLParam(newRequestAs(otherMemberID, http.MethodPut, "/api/autopilot-templates/"+template.ID, map[string]any{
		"name":           "Must not update",
		"description":    "",
		"execution_mode": "run_only",
		"trigger_kind":   "webhook",
	}), "id", template.ID))
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("UpdateAutopilotTemplate(other member): expected 403, got %d: %s", forbidden.Code, forbidden.Body.String())
	}

	update := httptest.NewRecorder()
	testHandler.UpdateAutopilotTemplate(update, withURLParam(newRequestAs(creatorID, http.MethodPut, "/api/autopilot-templates/"+template.ID, map[string]any{
		"name":                 "Template ownership probe",
		"description":          "Handle an incoming webhook",
		"execution_mode":       "run_only",
		"issue_title_template": nil,
		"trigger_kind":         "webhook",
	}), "id", template.ID))
	if update.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilotTemplate(creator): expected 200, got %d: %s", update.Code, update.Body.String())
	}
	var updated autopilotTemplateResponse
	if err := json.Unmarshal(update.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated template: %v", err)
	}
	if updated.TriggerKind != "webhook" || updated.CronExpression != nil || updated.Timezone != nil || updated.IssueTitleTemplate != nil {
		t.Fatalf("updated template = %+v, want webhook with schedule and issue title cleared", updated)
	}

	remove := httptest.NewRecorder()
	testHandler.DeleteAutopilotTemplate(remove, withURLParam(newRequestAs(creatorID, http.MethodDelete, "/api/autopilot-templates/"+template.ID, nil), "id", template.ID))
	if remove.Code != http.StatusNoContent {
		t.Fatalf("DeleteAutopilotTemplate: expected 204, got %d: %s", remove.Code, remove.Body.String())
	}
}
