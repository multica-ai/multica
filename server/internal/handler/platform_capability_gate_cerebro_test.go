package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// agentPlatformCapabilityRequest builds an agent-actor request WITHOUT issuing
// a task mandate for the capability. These catalog-only keys are not runtime
// tool names, so real mandates never contain them — the gate must decide on
// the tool-policy chain alone (FIR-4220).
func agentPlatformCapabilityRequest(t *testing.T, capability string) *http.Request {
	t.Helper()
	agentID := createHandlerTestAgent(t, "platform-capability-"+capability+"-"+uuid.NewString(), []byte(`{}`))
	taskID := createHandlerTestTaskForAgent(t, agentID)
	req := newRequest(http.MethodPost, "/api/platform-capability-test", nil)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func TestRequirePlatformCapability_PolicyIsTheGateForAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, capability := range []string{
		rerunIssuePlatformAction,
		triggerAutopilotPlatformAction,
		autopilotScopePlatformAction,
		scheduleAgentWakeupPlatformAction,
		manageProjectAccessPlatformAction,
		useOtherRuntimePlatformAction,
		readIssuesPlatformAction,
		readProjectsPlatformAction,
	} {
		t.Run(capability, func(t *testing.T) {
			nextCalls := 0
			guarded := testHandler.RequirePlatformCapability(capability)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalls++
				w.WriteHeader(http.StatusNoContent)
			}))

			setPlatformActionWorkspacePolicy(t, capability, toolpolicy.SettingDeny)
			denied := httptest.NewRecorder()
			guarded.ServeHTTP(denied, agentPlatformCapabilityRequest(t, capability))
			if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "platform_action_denied") {
				t.Fatalf("Deny status=%d body=%s, want 403 platform_action_denied", denied.Code, denied.Body.String())
			}
			if nextCalls != 0 {
				t.Fatalf("Deny reached handler %d time(s), want 0", nextCalls)
			}

			// Allow must pass WITHOUT a task mandate containing the key: the
			// policy chain alone decides these capabilities.
			setPlatformActionWorkspacePolicy(t, capability, toolpolicy.SettingAllow)
			allowed := httptest.NewRecorder()
			guarded.ServeHTTP(allowed, agentPlatformCapabilityRequest(t, capability))
			if allowed.Code != http.StatusNoContent {
				t.Fatalf("Allow status=%d body=%s, want 204", allowed.Code, allowed.Body.String())
			}
			if nextCalls != 1 {
				t.Fatalf("Allow reached handler %d time(s), want 1", nextCalls)
			}
		})
	}
}

// TestRequirePlatformReadCapability_GatesOnlyReadMethods proves the group-wide
// read gate bites on GET for a denied agent but lets non-read methods through
// untouched — mutation routes in the same group carry their own gates.
func TestRequirePlatformReadCapability_GatesOnlyReadMethods(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setPlatformActionWorkspacePolicy(t, readIssuesPlatformAction, toolpolicy.SettingDeny)
	nextCalls := 0
	guarded := testHandler.RequirePlatformReadCapability(readIssuesPlatformAction)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalls++
		w.WriteHeader(http.StatusNoContent)
	}))

	agentGet := agentPlatformCapabilityRequest(t, readIssuesPlatformAction)
	agentGet.Method = http.MethodGet
	deniedRead := httptest.NewRecorder()
	guarded.ServeHTTP(deniedRead, agentGet)
	if deniedRead.Code != http.StatusForbidden || nextCalls != 0 {
		t.Fatalf("GET under Deny: status=%d nextCalls=%d, want 403 and 0", deniedRead.Code, nextCalls)
	}

	agentPost := agentPlatformCapabilityRequest(t, readIssuesPlatformAction)
	passedWrite := httptest.NewRecorder()
	guarded.ServeHTTP(passedWrite, agentPost)
	if passedWrite.Code != http.StatusNoContent || nextCalls != 1 {
		t.Fatalf("POST under Deny: status=%d nextCalls=%d, want 204 and 1 (non-read methods pass)", passedWrite.Code, nextCalls)
	}
}

// TestPlatformIntakeAllowed_WorkspaceDenySwitchesIntakeOff proves the FIR-4220
// slice 2 intake contract: an authored workspace-layer Deny turns the intake
// off, while Allow, Ask, and no row leave it on (Ask has nobody to ask at an
// unattended intake).
func TestPlatformIntakeAllowed_WorkspaceDenySwitchesIntakeOff(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	workspaceUUID, err := util.ParseUUID(testWorkspaceID)
	if err != nil {
		t.Fatalf("parse workspace id: %v", err)
	}
	ctx := context.Background()
	for _, tc := range []struct {
		setting toolpolicy.Setting
		want    bool
	}{
		{toolpolicy.SettingDeny, false},
		{toolpolicy.SettingAllow, true},
		{toolpolicy.SettingAsk, true},
	} {
		setPlatformActionWorkspacePolicy(t, autopilotWebhookPlatformAction, tc.setting)
		if got := testHandler.platformIntakeAllowed(ctx, workspaceUUID, autopilotWebhookPlatformAction); got != tc.want {
			t.Errorf("workspace %s: intake allowed = %v, want %v", tc.setting, got, tc.want)
		}
	}
}

// TestRequirePlatformCapability_MemberBypassesPolicyGate proves the gate is
// agent-scoped: a member request passes even under an authored Deny — the
// wrapped handler's own role/membership checks remain the member gate.
func TestRequirePlatformCapability_MemberBypassesPolicyGate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setPlatformActionWorkspacePolicy(t, rerunIssuePlatformAction, toolpolicy.SettingDeny)
	nextCalls := 0
	guarded := testHandler.RequirePlatformCapability(rerunIssuePlatformAction)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, newRequest(http.MethodPost, "/api/platform-capability-test", nil))
	if rec.Code != http.StatusNoContent || nextCalls != 1 {
		t.Fatalf("member status=%d nextCalls=%d, want 204 and 1", rec.Code, nextCalls)
	}
}
