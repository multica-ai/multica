package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetGovernancePolicyReturnsDecisionMatrix(t *testing.T) {
	req := newRequest(http.MethodGet, "/api/governance/policy", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.GetGovernancePolicy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetGovernancePolicy: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp GovernancePolicyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id = %q, want %q", resp.WorkspaceID, testWorkspaceID)
	}
	if resp.WorkspaceRole != "owner" {
		t.Fatalf("workspace_role = %q, want owner", resp.WorkspaceRole)
	}
	if len(resp.Roles) < 5 {
		t.Fatalf("expected role templates, got %d", len(resp.Roles))
	}
	if len(resp.Decisions) == 0 {
		t.Fatal("expected decisions")
	}

	var foundApprovalRequired bool
	for _, decision := range resp.Decisions {
		if decision.ActionID == "agent.create" {
			foundApprovalRequired = true
			if decision.Allowed {
				t.Fatalf("agent.create should not be allowed without approval: %+v", decision)
			}
			if !decision.RequiresApproval || decision.Reason != "approval_required" {
				t.Fatalf("agent.create decision = %+v", decision)
			}
		}
	}
	if !foundApprovalRequired {
		t.Fatal("missing agent.create decision")
	}
}

func TestGetGovernancePolicyIncludesPersistedApprovalSources(t *testing.T) {
	agentID := createWebhookTestAgent(t, "governance policy approval agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")

	createGovernanceApprovalForTarget(t, "autopilot.delete", "autopilot", autopilotID)

	req := newRequest(http.MethodGet, "/api/governance/policy", nil)

	w := httptest.NewRecorder()
	testHandler.GetGovernancePolicy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetGovernancePolicy: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp GovernancePolicyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.ApprovalSources) == 0 {
		t.Fatal("expected persisted approval sources")
	}
	for _, decision := range resp.Decisions {
		if decision.ActionID == "autopilot.delete" && (!decision.RequiresApproval || decision.Allowed) {
			t.Fatalf("policy matrix should still require target-specific execution approval: %+v", decision)
		}
	}
}

func TestDeleteAutopilotRequiresGovernanceApproval(t *testing.T) {
	agentID := createWebhookTestAgent(t, "governance denied agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")

	req := newRequest(http.MethodDelete, "/api/autopilots/"+autopilotID, nil)
	req = withURLParam(req, "id", autopilotID)
	w := httptest.NewRecorder()
	testHandler.DeleteAutopilot(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteAutopilot without approval: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	if _, err := testHandler.Queries.GetAutopilot(req.Context(), parseUUID(autopilotID)); err != nil {
		t.Fatalf("autopilot should still exist after denied delete: %v", err)
	}
}

func TestDeleteAutopilotConsumesApprovalAndWritesAudit(t *testing.T) {
	agentID := createWebhookTestAgent(t, "governance approved agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")

	approval := createGovernanceApprovalForTarget(t, "autopilot.delete", "autopilot", autopilotID)

	req := newRequest(http.MethodDelete, "/api/autopilots/"+autopilotID, nil)
	req = withURLParam(req, "id", autopilotID)
	w := httptest.NewRecorder()
	testHandler.DeleteAutopilot(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteAutopilot with approval: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/governance/audits", nil)
	testHandler.ListGovernanceAudits(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListGovernanceAudits: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var auditsResp struct {
		Audits []GovernanceAuditResponse `json:"audits"`
	}
	if err := json.NewDecoder(w.Body).Decode(&auditsResp); err != nil {
		t.Fatalf("decode audits: %v", err)
	}
	if len(auditsResp.Audits) == 0 {
		t.Fatal("expected audit row")
	}
	found := false
	for _, audit := range auditsResp.Audits {
		if audit.ApprovalID != nil && *audit.ApprovalID == approval.ID && audit.ActionID == "autopilot.delete" {
			found = true
			if audit.ActorID != testUserID {
				t.Fatalf("audit actor_id = %q, want %q", audit.ActorID, testUserID)
			}
			if audit.BeforeSummary["title"] == "" || audit.AfterSummary["deleted"] != true {
				t.Fatalf("unexpected audit summaries: before=%v after=%v", audit.BeforeSummary, audit.AfterSummary)
			}
		}
	}
	if !found {
		t.Fatalf("missing audit for approval %s: %+v", approval.ID, auditsResp.Audits)
	}

	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/governance/approvals", nil)
	testHandler.ListGovernanceApprovals(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListGovernanceApprovals: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var approvalsResp struct {
		Approvals []GovernanceApprovalResponse `json:"approvals"`
	}
	if err := json.NewDecoder(w.Body).Decode(&approvalsResp); err != nil {
		t.Fatalf("decode approvals: %v", err)
	}
	for _, item := range approvalsResp.Approvals {
		if item.ID == approval.ID && item.ConsumedAt == nil {
			t.Fatalf("approval %s should be consumed after approved delete", approval.ID)
		}
	}
}

func TestRotateWebhookTokenRequiresGovernanceApproval(t *testing.T) {
	agentID := createWebhookTestAgent(t, "governance rotate denied agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trigger := createWebhookTriggerViaHandler(t, autopilotID)
	oldToken := *trigger.WebhookToken

	req := newRequest(http.MethodPost, "/api/autopilots/"+autopilotID+"/triggers/"+trigger.ID+"/rotate-webhook-token", nil)
	req = withURLParams(req, "id", autopilotID, "triggerId", trigger.ID)
	w := httptest.NewRecorder()
	testHandler.RotateAutopilotTriggerWebhookToken(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("RotateAutopilotTriggerWebhookToken without approval: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	row, err := testHandler.Queries.GetAutopilotTrigger(req.Context(), parseUUID(trigger.ID))
	if err != nil {
		t.Fatalf("reload trigger: %v", err)
	}
	if row.WebhookToken.String != oldToken {
		t.Fatalf("denied rotate changed token: old=%q new=%q", oldToken, row.WebhookToken.String)
	}
}

func TestRotateWebhookTokenConsumesApprovalAndWritesAudit(t *testing.T) {
	agentID := createWebhookTestAgent(t, "governance rotate approved agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trigger := createWebhookTriggerViaHandler(t, autopilotID)
	oldToken := *trigger.WebhookToken

	approval := createGovernanceApprovalForTarget(t, "autopilot.webhook.rotate", "autopilot_trigger", trigger.ID)

	req := newRequest(http.MethodPost, "/api/autopilots/"+autopilotID+"/triggers/"+trigger.ID+"/rotate-webhook-token", nil)
	req = withURLParams(req, "id", autopilotID, "triggerId", trigger.ID)
	w := httptest.NewRecorder()
	testHandler.RotateAutopilotTriggerWebhookToken(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("RotateAutopilotTriggerWebhookToken with approval: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rotated AutopilotTriggerResponse
	if err := json.NewDecoder(w.Body).Decode(&rotated); err != nil {
		t.Fatalf("decode rotated trigger: %v", err)
	}
	if rotated.WebhookToken == nil || *rotated.WebhookToken == oldToken {
		t.Fatalf("token was not rotated: old=%q new=%v", oldToken, rotated.WebhookToken)
	}

	audit := findGovernanceAuditForApproval(t, approval.ID, "autopilot.webhook.rotate")
	if audit.TargetType != "autopilot_trigger" || audit.TargetID != trigger.ID {
		t.Fatalf("unexpected audit target: %+v", audit)
	}
	if audit.BeforeSummary["has_webhook_token"] != true || audit.AfterSummary["rotated"] != true {
		t.Fatalf("unexpected rotate audit summaries: before=%v after=%v", audit.BeforeSummary, audit.AfterSummary)
	}
	assertGovernanceApprovalConsumed(t, approval.ID)
}

func TestGovernanceApprovalCannotAuthorizeTwoWebhookMutations(t *testing.T) {
	agentID := createWebhookTestAgent(t, "governance one shot approval agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trigger := createWebhookTriggerViaHandler(t, autopilotID)
	oldToken := *trigger.WebhookToken

	approval := createGovernanceApprovalForTarget(t, "autopilot.webhook.rotate", "autopilot_trigger", trigger.ID)

	req := newRequest(http.MethodPost, "/api/autopilots/"+autopilotID+"/triggers/"+trigger.ID+"/rotate-webhook-token", nil)
	req = withURLParams(req, "id", autopilotID, "triggerId", trigger.ID)
	w := httptest.NewRecorder()
	testHandler.RotateAutopilotTriggerWebhookToken(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first rotate with approval: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rotated AutopilotTriggerResponse
	if err := json.NewDecoder(w.Body).Decode(&rotated); err != nil {
		t.Fatalf("decode rotated trigger: %v", err)
	}
	if rotated.WebhookToken == nil || *rotated.WebhookToken == oldToken {
		t.Fatalf("first rotate did not change token: old=%q new=%v", oldToken, rotated.WebhookToken)
	}

	req = newRequest(http.MethodPost, "/api/autopilots/"+autopilotID+"/triggers/"+trigger.ID+"/rotate-webhook-token", nil)
	req = withURLParams(req, "id", autopilotID, "triggerId", trigger.ID)
	w = httptest.NewRecorder()
	testHandler.RotateAutopilotTriggerWebhookToken(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("second rotate reused consumed approval: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	row, err := testHandler.Queries.GetAutopilotTrigger(req.Context(), parseUUID(trigger.ID))
	if err != nil {
		t.Fatalf("reload trigger: %v", err)
	}
	if row.WebhookToken.String != *rotated.WebhookToken {
		t.Fatalf("denied second rotate changed token: previous=%q new=%q", *rotated.WebhookToken, row.WebhookToken.String)
	}
	assertGovernanceApprovalConsumed(t, approval.ID)
	audit := findGovernanceAuditForApproval(t, approval.ID, "autopilot.webhook.rotate")
	if audit.TargetID != trigger.ID {
		t.Fatalf("unexpected audit after one-shot approval: %+v", audit)
	}
}

func TestSetSigningSecretRequiresGovernanceApproval(t *testing.T) {
	agentID := createWebhookTestAgent(t, "governance secret denied agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trigger := createWebhookTriggerViaHandler(t, autopilotID)

	req := newRequest(http.MethodPut, "/api/autopilots/"+autopilotID+"/triggers/"+trigger.ID+"/signing-secret", map[string]any{
		"signing_secret": testSigningSecret,
	})
	req = withURLParams(req, "id", autopilotID, "triggerId", trigger.ID)
	w := httptest.NewRecorder()
	testHandler.SetAutopilotTriggerSigningSecret(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("SetAutopilotTriggerSigningSecret without approval: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	row, err := testHandler.Queries.GetAutopilotTrigger(req.Context(), parseUUID(trigger.ID))
	if err != nil {
		t.Fatalf("reload trigger: %v", err)
	}
	if row.SigningSecret.Valid {
		t.Fatalf("denied signing-secret update persisted a secret")
	}
}

func TestSetSigningSecretConsumesApprovalAndWritesAudit(t *testing.T) {
	agentID := createWebhookTestAgent(t, "governance secret approved agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trigger := createWebhookTriggerViaHandler(t, autopilotID)

	approval := createGovernanceApprovalForTarget(t, "autopilot.webhook.rotate", "autopilot_trigger", trigger.ID)

	req := newRequest(http.MethodPut, "/api/autopilots/"+autopilotID+"/triggers/"+trigger.ID+"/signing-secret", map[string]any{
		"signing_secret": testSigningSecret,
	})
	req = withURLParams(req, "id", autopilotID, "triggerId", trigger.ID)
	w := httptest.NewRecorder()
	testHandler.SetAutopilotTriggerSigningSecret(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SetAutopilotTriggerSigningSecret with approval: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated AutopilotTriggerResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated trigger: %v", err)
	}
	if !updated.HasSigningSecret || updated.SigningSecretHint == nil {
		t.Fatalf("expected signing secret metadata in response: %+v", updated)
	}

	audit := findGovernanceAuditForApproval(t, approval.ID, "autopilot.webhook.rotate")
	if audit.TargetType != "autopilot_trigger" || audit.TargetID != trigger.ID {
		t.Fatalf("unexpected audit target: %+v", audit)
	}
	if audit.BeforeSummary["has_signing_secret"] != false || audit.AfterSummary["has_signing_secret"] != true {
		t.Fatalf("unexpected signing-secret audit summaries: before=%v after=%v", audit.BeforeSummary, audit.AfterSummary)
	}
	if audit.AfterSummary["signing_secret_hint"] == testSigningSecret {
		t.Fatalf("audit leaked raw signing secret: %v", audit.AfterSummary)
	}
	assertGovernanceApprovalConsumed(t, approval.ID)
}

func TestGovernanceDoesNotRegressIssueMetadataFlow(t *testing.T) {
	issueID := createMetadataTestIssue(t, "Governance metadata regression")
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID+"/metadata/pipeline_status", json.RawMessage(`{"value":"waiting_review"}`))
	req = withURLParams(req, "id", issueID, "key", "pipeline_status")
	testHandler.SetIssueMetadataKey(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SetIssueMetadataKey: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/metadata", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueMetadata(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueMetadata: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if resp.Metadata["pipeline_status"] != "waiting_review" {
		t.Fatalf("metadata pipeline_status = %v", resp.Metadata["pipeline_status"])
	}
}

func createGovernanceApprovalForTarget(t *testing.T, actionID, targetType, targetID string) GovernanceApprovalResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/governance/approvals", map[string]any{
		"action_id":            actionID,
		"target_type":          targetType,
		"target_id":            targetID,
		"approval_source_type": "manual",
		"reason":               "test approval",
	})
	testHandler.CreateGovernanceApproval(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateGovernanceApproval: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp GovernanceApprovalResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode approval: %v", err)
	}
	return resp
}

func findGovernanceAuditForApproval(t *testing.T, approvalID, actionID string) GovernanceAuditResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/governance/audits", nil)
	testHandler.ListGovernanceAudits(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListGovernanceAudits: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var auditsResp struct {
		Audits []GovernanceAuditResponse `json:"audits"`
	}
	if err := json.NewDecoder(w.Body).Decode(&auditsResp); err != nil {
		t.Fatalf("decode audits: %v", err)
	}
	for _, audit := range auditsResp.Audits {
		if audit.ApprovalID != nil && *audit.ApprovalID == approvalID && audit.ActionID == actionID {
			return audit
		}
	}
	t.Fatalf("missing audit for approval %s action %s: %+v", approvalID, actionID, auditsResp.Audits)
	return GovernanceAuditResponse{}
}

func assertGovernanceApprovalConsumed(t *testing.T, approvalID string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/governance/approvals", nil)
	testHandler.ListGovernanceApprovals(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListGovernanceApprovals: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var approvalsResp struct {
		Approvals []GovernanceApprovalResponse `json:"approvals"`
	}
	if err := json.NewDecoder(w.Body).Decode(&approvalsResp); err != nil {
		t.Fatalf("decode approvals: %v", err)
	}
	for _, item := range approvalsResp.Approvals {
		if item.ID == approvalID {
			if item.ConsumedAt == nil {
				t.Fatalf("approval %s should be consumed", approvalID)
			}
			return
		}
	}
	t.Fatalf("approval %s not found in approval list", approvalID)
}
