package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func agentWorkflowMutationRequest(t *testing.T) *http.Request {
	t.Helper()
	agentID := createHandlerTestAgent(t, "manage-workflows-"+uuid.NewString(), []byte(`{}`))
	taskID := createHandlerTestTaskForAgent(t, agentID)
	issuePlatformActionMandate(t, taskID, agentID, manageWorkflowsPlatformAction)
	req := newRequest(http.MethodPost, "/api/cerebro/workflows", nil)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func TestRequireManageWorkflows_DenyBlocksBeforeHandlerAndAllowReachesIt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	nextCalls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalls++
		w.WriteHeader(http.StatusNoContent)
	})
	guarded := testHandler.RequireManageWorkflows(next)

	setPlatformActionWorkspacePolicy(t, manageWorkflowsPlatformAction, toolpolicy.SettingDeny)
	denied := httptest.NewRecorder()
	guarded.ServeHTTP(denied, agentWorkflowMutationRequest(t))
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "platform_action_denied") {
		t.Fatalf("Deny status=%d body=%s, want 403 platform_action_denied", denied.Code, denied.Body.String())
	}
	if nextCalls != 0 {
		t.Fatalf("Deny reached workflow handler %d time(s), want 0", nextCalls)
	}

	setPlatformActionWorkspacePolicy(t, manageWorkflowsPlatformAction, toolpolicy.SettingAllow)
	allowed := httptest.NewRecorder()
	guarded.ServeHTTP(allowed, agentWorkflowMutationRequest(t))
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("Allow status=%d body=%s, want 204", allowed.Code, allowed.Body.String())
	}
	if nextCalls != 1 {
		t.Fatalf("Allow reached workflow handler %d time(s), want 1", nextCalls)
	}
}

func TestRequireManageWorkflows_PreservesMemberWritesAndReads(t *testing.T) {
	nextCalls := 0
	guarded := testHandler.RequireManageWorkflows(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalls++
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, newRequest(method, "/api/cerebro/workflows", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s member status=%d body=%s, want 204", method, rec.Code, rec.Body.String())
		}
	}
	if nextCalls != 2 {
		t.Fatalf("member/read requests reached handler %d time(s), want 2", nextCalls)
	}
}

func TestManageWorkflowsCatalogOperationsUseGuardedMutationMethods(t *testing.T) {
	capability, ok := platformcatalog.ByKey(manageWorkflowsPlatformAction)
	if !ok {
		t.Fatal("manage_workflows missing from platformcatalog")
	}
	for _, operation := range capability.Ops {
		method, _, ok := strings.Cut(operation, " ")
		if !ok {
			t.Fatalf("invalid operation %q", operation)
		}
		if !isManageWorkflowsMutation(method) {
			t.Errorf("manage_workflows operation %q bypasses shared mutation-method guard", operation)
		}
	}
	if isManageWorkflowsMutation(http.MethodGet) {
		t.Fatal("GET must remain outside manage_workflows mutation guard")
	}
}
