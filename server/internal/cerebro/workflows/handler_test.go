package workflows

import "testing"

func TestValidateWriteRequest_RequiresName(t *testing.T) {
	err := validateWriteRequest(writeWorkflowRequest{
		TriggerType: TriggerStatusChanged,
		ActionType:  ActionSetStatus,
	})
	if err == nil {
		t.Fatal("empty name must be rejected")
	}
}

func TestValidateWriteRequest_RejectsUnknownEnumValues(t *testing.T) {
	cases := []struct {
		name    string
		req     writeWorkflowRequest
		wantErr bool
	}{
		{
			name: "known trigger and action",
			req: writeWorkflowRequest{
				Name:        "x",
				TriggerType: TriggerStatusChanged,
				ActionType:  ActionCreateSubIssue,
			},
		},
		{
			name: "unknown trigger",
			req: writeWorkflowRequest{
				Name:        "x",
				TriggerType: "issue_starred",
				ActionType:  ActionSetStatus,
			},
			wantErr: true,
		},
		{
			name: "unknown action",
			req: writeWorkflowRequest{
				Name:        "x",
				TriggerType: TriggerStatusChanged,
				ActionType:  "post_to_slack",
			},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateWriteRequest(c.req)
			if c.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

func TestKnownEnums(t *testing.T) {
	for _, tt := range []string{
		TriggerStatusChanged, TriggerDueDateReached, TriggerDueTimeReached,
	} {
		if !knownTrigger(tt) {
			t.Errorf("knownTrigger(%q) = false, want true", tt)
		}
	}
	for _, a := range []string{
		ActionSetStatus, ActionCreateSubIssue, ActionSendReminder,
	} {
		if !knownAction(a) {
			t.Errorf("knownAction(%q) = false, want true", a)
		}
	}
	if knownTrigger("") || knownAction("") {
		t.Fatal("empty string must not be a known trigger or action")
	}
}

func TestDefaultJSON(t *testing.T) {
	if got := string(defaultJSON(nil, "{}")); got != "{}" {
		t.Errorf("nil input must use fallback, got %q", got)
	}
	if got := string(defaultJSON([]byte(`{"a":1}`), "{}")); got != `{"a":1}` {
		t.Errorf("non-empty input must pass through, got %q", got)
	}
}
