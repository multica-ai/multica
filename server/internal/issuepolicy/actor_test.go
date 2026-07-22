package issuepolicy

import "testing"

func TestValidateCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		actorType  string
		originType string
		hasParent  bool
		wantErr    bool
	}{
		{name: "member creates issue", actorType: "member"},
		{name: "quick create creates requested top level issue", actorType: "agent", originType: "quick_create"},
		{name: "quick create cannot create child", actorType: "agent", originType: "quick_create", hasParent: true, wantErr: true},
		{name: "issue bound agent cannot create issue", actorType: "agent", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCreate(test.actorType, test.originType, test.hasParent)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateCreate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"backlog", "todo", "in_progress", "in_review"} {
		if err := ValidateStatus("agent", status); err != nil {
			t.Errorf("agent status %q rejected: %v", status, err)
		}
	}
	for _, status := range []string{"blocked", "done", "cancelled"} {
		if err := ValidateStatus("agent", status); err == nil {
			t.Errorf("agent status %q accepted", status)
		}
		if err := ValidateStatus("member", status); err != nil {
			t.Errorf("member status %q rejected: %v", status, err)
		}
	}
}

func TestValidateHierarchyChange(t *testing.T) {
	t.Parallel()

	if err := ValidateHierarchyChange("agent", true); err == nil {
		t.Fatal("agent hierarchy mutation accepted")
	}
	if err := ValidateHierarchyChange("agent", false); err != nil {
		t.Fatalf("agent unrelated mutation rejected: %v", err)
	}
	if err := ValidateHierarchyChange("member", true); err != nil {
		t.Fatalf("member hierarchy mutation rejected: %v", err)
	}
}
