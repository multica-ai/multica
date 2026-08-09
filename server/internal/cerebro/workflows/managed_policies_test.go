package workflows

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestManagedPoliciesExposePlainLanguageContracts(t *testing.T) {
	const workspaceID = "11111111-1111-1111-1111-111111111111"
	for _, definition := range managedHookPolicies(workspaceID) {
		raw, err := json.Marshal(definition.Policy)
		if err != nil {
			t.Fatal(err)
		}
		var policy map[string]any
		if err := json.Unmarshal(raw, &policy); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(stringValue(policy["contract_rule"])) == "" {
			t.Errorf("managed hook %q has no contract_rule", definition.Key)
		}
		if strings.TrimSpace(stringValue(policy["contract_satisfy"])) == "" {
			t.Errorf("managed hook %q has no contract_satisfy", definition.Key)
		}
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func TestManagedMessagePoliciesOwnEveryMigratedCommentDecision(t *testing.T) {
	const workspaceID = "11111111-1111-1111-1111-111111111111"
	definitions := managedHookPolicies(workspaceID)
	policies := make([]HookPolicy, 0, len(definitions))
	for _, definition := range definitions {
		policies = append(policies, definition.Policy)
	}
	engine := NewHookEngine(true, NewMemoryHookStore(policies))

	tests := []struct {
		name         string
		message      map[string]any
		wantDecision HookDecision
		wantParent   string
	}{
		{
			name: "missing recipient requires correction",
			message: map[string]any{
				"agent_authored": true, "has_recipient": false, "has_active_wakeup": false,
				"thread_required": false, "correct_thread": true, "no_action": false, "is_sub_issue": false,
			},
			wantDecision: HookRequire,
		},
		{
			name: "active wakeup is a valid continuation",
			message: map[string]any{
				"agent_authored": true, "has_recipient": false, "has_active_wakeup": true,
				"thread_required": false, "correct_thread": true, "no_action": false, "is_sub_issue": false,
			},
			wantDecision: HookAllow,
		},
		{
			name: "unbacked continuation promise requires correction",
			message: map[string]any{
				"agent_authored": true, "has_recipient": true, "has_active_wakeup": false,
				"promises_continuation": true,
				"thread_required":       false, "correct_thread": true, "no_action": false, "is_sub_issue": false,
			},
			wantDecision: HookRequire,
		},
		{
			name: "wakeup backs continuation promise",
			message: map[string]any{
				"agent_authored": true, "has_recipient": true, "has_active_wakeup": true,
				"promises_continuation": true,
				"thread_required":       false, "correct_thread": true, "no_action": false, "is_sub_issue": false,
			},
			wantDecision: HookAllow,
		},
		{
			name: "wrong thread is corrected",
			message: map[string]any{
				"agent_authored": true, "has_recipient": true, "has_active_wakeup": false,
				"thread_required": true, "correct_thread": false, "required_parent_id": "comment-1",
				"no_action": false, "is_sub_issue": false,
			},
			wantDecision: HookModify,
			wantParent:   "comment-1",
		},
		{
			name: "no_action blocks",
			message: map[string]any{
				"agent_authored": true, "has_recipient": true, "has_active_wakeup": false,
				"thread_required": false, "correct_thread": true, "no_action": true, "is_sub_issue": false,
			},
			wantDecision: HookBlock,
		},
		{
			name: "sub-issue must keep parent agent in loop",
			message: map[string]any{
				"agent_authored": true, "has_recipient": true, "has_active_wakeup": false,
				"thread_required": false, "correct_thread": true, "no_action": false, "is_sub_issue": true,
				"mentions_initiator": false, "mentions_agent": false, "posted_on_parent": false,
			},
			wantDecision: HookRequire,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := engine.Evaluate(context.Background(), HookEvent{
				EventID: "event-" + test.name, Type: HookBeforeMessageSend, WorkspaceID: workspaceID,
				MutableFields: []string{"parent_id"}, Context: map[string]any{"message": test.message},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Decision != test.wantDecision {
				t.Fatalf("decision = %q, want %q; result=%#v", result.Decision, test.wantDecision, result)
			}
			if test.wantParent != "" && result.Modifications["parent_id"] != test.wantParent {
				t.Fatalf("parent_id = %#v, want %q", result.Modifications["parent_id"], test.wantParent)
			}
		})
	}
}

func TestManagedCompletionPolicyRequiresPersistedContinuation(t *testing.T) {
	const workspaceID = "11111111-1111-1111-1111-111111111111"
	definitions := managedHookPolicies(workspaceID)
	policies := make([]HookPolicy, 0, len(definitions))
	for _, definition := range definitions {
		policies = append(policies, definition.Policy)
	}
	engine := NewHookEngine(true, NewMemoryHookStore(policies))

	for attempt := 1; attempt <= 4; attempt++ {
		result, err := engine.Evaluate(context.Background(), HookEvent{
			EventID: "task-stop-" + string(rune('0'+attempt)),
			Type:    HookBeforeTaskComplete, WorkspaceID: workspaceID, Attempt: attempt,
			Context: map[string]any{
				"issue":        map[string]any{"terminal": false},
				"continuation": map[string]any{"present": false},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Decision != HookRequire {
			t.Fatalf("attempt %d decision = %q, want require", attempt, result.Decision)
		}
	}

	result, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "task-stop-with-wakeup", Type: HookBeforeTaskComplete, WorkspaceID: workspaceID,
		Context: map[string]any{
			"issue":        map[string]any{"terminal": false},
			"continuation": map[string]any{"present": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookAllow {
		t.Fatalf("continuation decision = %q, want allow", result.Decision)
	}
}

func TestManagedChainPoliciesRequireFinalApprovalAndRecordLifecycle(t *testing.T) {
	const workspaceID = "11111111-1111-1111-1111-111111111111"
	definitions := managedHookPolicies(workspaceID)
	policies := make([]HookPolicy, 0, len(definitions))
	for _, definition := range definitions {
		policies = append(policies, definition.Policy)
	}
	engine := NewHookEngine(true, NewMemoryHookStore(policies))

	blocked, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "chain-done-without-approval", Type: HookBeforeIssueStatus, WorkspaceID: workspaceID,
		Context: map[string]any{
			"status": map[string]any{"from": "in_review", "to": "done"},
			"chain":  map[string]any{"active": true, "approved_for_done": false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Decision != HookRequire {
		t.Fatalf("unapproved Done decision = %q, want require", blocked.Decision)
	}

	approved, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "chain-done-approved", Type: HookBeforeIssueStatus, WorkspaceID: workspaceID,
		Context: map[string]any{
			"status": map[string]any{"from": "in_review", "to": "done"},
			"chain":  map[string]any{"active": true, "approved_for_done": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Decision != HookAllow {
		t.Fatalf("approved Done decision = %q, want allow", approved.Decision)
	}

	step, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "chain-step-completed", Type: HookAfterWorkflowStep, WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if step.Decision != HookAllow || !step.Evaluated {
		t.Fatalf("completed step result = %#v, want evaluated allow", step)
	}
}

func TestManagedFailurePoliciesMirrorEveryLegacyRoute(t *testing.T) {
	const workspaceID = "11111111-1111-1111-1111-111111111111"
	definitions := managedHookPolicies(workspaceID)
	policies := make([]HookPolicy, 0, len(definitions))
	for _, definition := range definitions {
		policies = append(policies, definition.Policy)
	}
	engine := NewHookEngine(true, NewMemoryHookStore(policies))

	for reason, route := range managedTaskFailureRoutes() {
		t.Run(reason, func(t *testing.T) {
			result, err := engine.Evaluate(context.Background(), HookEvent{
				EventID: "failure-" + reason, Type: HookOnTaskFailure, WorkspaceID: workspaceID,
				MutableFields: []string{"failure_action", "fresh_session", "retry_limit", "user_message"},
				Context:       map[string]any{"failure": map[string]any{"reason": reason}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Decision != HookModify {
				t.Fatalf("decision = %q, want modify", result.Decision)
			}
			if result.Modifications["failure_action"] != route.Action {
				t.Fatalf("failure_action = %#v, want %q", result.Modifications["failure_action"], route.Action)
			}
			if result.Modifications["fresh_session"] != route.FreshSession {
				t.Fatalf("fresh_session = %#v, want %t", result.Modifications["fresh_session"], route.FreshSession)
			}
			if result.Modifications["retry_limit"] != int(route.RetryLimit) {
				t.Fatalf("retry_limit = %#v, want %d", result.Modifications["retry_limit"], route.RetryLimit)
			}
			if result.Modifications["user_message"] != route.UserMessage {
				t.Fatalf("user_message = %#v, want %q", result.Modifications["user_message"], route.UserMessage)
			}
		})
	}
	for _, reason := range taskfailure.AllReasons() {
		if _, ok := managedTaskFailureRoutes()[reason.String()]; !ok {
			t.Errorf("missing managed Workflow route for %q", reason)
		}
	}
}

func TestManagedWakeupPoliciesOwnCreateAndFireDecisions(t *testing.T) {
	const workspaceID = "11111111-1111-1111-1111-111111111111"
	definitions := managedHookPolicies(workspaceID)
	policies := make([]HookPolicy, 0, len(definitions))
	for _, definition := range definitions {
		policies = append(policies, definition.Policy)
	}
	engine := NewHookEngine(true, NewMemoryHookStore(policies))

	create, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "wakeup-create-limit", Type: HookBeforeWakeupCreate, WorkspaceID: workspaceID,
		Context: map[string]any{"wakeup": map[string]any{
			"trigger_type": "time", "trigger_enabled": true,
			"active_count": int64(8), "max_active": 8,
			"seconds_until_fire": int64(3600), "min_interval_seconds": int64(300),
			"has_last_fire": false, "loop_guard_enabled": true,
			"loop_limit_enabled": true, "consecutive_without_progress": int64(0), "max_without_progress": 2,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if create.Decision != HookRequire {
		t.Fatalf("create decision = %q, want require", create.Decision)
	}

	progressContext := map[string]any{"wakeup": map[string]any{
		"trigger_type": "time", "trigger_enabled": true,
		"active_count": int64(0), "max_active": 8,
		"seconds_until_fire": int64(3600), "min_interval_seconds": int64(300),
		"has_last_fire": false, "loop_guard_enabled": true, "loop_limit_enabled": true,
		"since_member_reply": int64(2), "since_status_change": int64(2),
		"since_progress_update": int64(2), "since_pull_request_update": int64(0),
		"max_without_progress": 2,
	}}
	progress, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "wakeup-create-no-progress", Type: HookBeforeWakeupCreate, WorkspaceID: workspaceID,
		Context: progressContext,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.Decision != HookRequire {
		t.Fatalf("no-progress decision = %q, want require", progress.Decision)
	}
	progressContext["wakeup"].(map[string]any)["since_member_reply"] = int64(0)
	resumed, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "wakeup-create-member-progress", Type: HookBeforeWakeupCreate, WorkspaceID: workspaceID,
		Context: progressContext,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Decision != HookAllow {
		t.Fatalf("member-progress decision = %q, want allow", resumed.Decision)
	}

	fire, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "wakeup-fire-offline", Type: HookOnWakeupFireFailure, WorkspaceID: workspaceID,
		MutableFields: []string{"failure_action", "postpone_seconds", "notify_after"},
		Context:       map[string]any{"failure": map[string]any{"reason": "offline"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fire.Decision != HookModify || fire.Modifications["failure_action"] != "postpone" {
		t.Fatalf("fire result = %+v", fire)
	}
	if fire.Modifications["postpone_seconds"] != 300 || fire.Modifications["notify_after"] != 3 {
		t.Fatalf("fire modifications = %+v", fire.Modifications)
	}
}
