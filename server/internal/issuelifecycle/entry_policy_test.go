package issuelifecycle

import (
	"strings"
	"testing"
)

func TestNormalizeEntryPolicyDefaultsAndTypedExecutors(t *testing.T) {
	manual, err := NormalizeEntryPolicy(EntryPolicy{})
	if err != nil {
		t.Fatalf("normalize default policy: %v", err)
	}
	if manual.Assignee.Type != AssigneeKeep || manual.Executor.Type != ExecutorNone || manual.Advance != AdvanceHumanConfirms {
		t.Fatalf("default policy = %#v", manual)
	}

	agent, err := NormalizeEntryPolicy(EntryPolicy{
		Assignee:     EntryPolicyPrincipal{Type: "agent", ID: "agent-id"},
		Executor:     EntryPolicyPrincipal{Type: "agent", ID: "agent-id"},
		Instructions: "Implement and publish the result.",
		Advance:      AdvanceExecutorMayTransition,
	})
	if err != nil || agent.Executor.Type != "agent" {
		t.Fatalf("normalize agent policy = %#v, err=%v", agent, err)
	}
}

func TestNormalizeEntryPolicyRejectsAmbiguousOrUnrunnableShapes(t *testing.T) {
	tests := []EntryPolicy{
		{Assignee: EntryPolicyPrincipal{Type: AssigneeKeep, ID: "unexpected"}},
		{Assignee: EntryPolicyPrincipal{Type: AssigneeHuman}},
		{Executor: EntryPolicyPrincipal{Type: "agent", ID: "agent-id"}},
		{Executor: EntryPolicyPrincipal{Type: ExecutorNone}, Advance: AdvanceExecutorMayTransition},
		{Instructions: strings.Repeat("x", MaxEntryInstructionsRunes+1)},
	}
	for _, policy := range tests {
		if _, err := NormalizeEntryPolicy(policy); err == nil {
			t.Fatalf("policy should be rejected: %#v", policy)
		}
	}
}

func TestDecodeEntryPolicyExpandsLegacyEmptyObject(t *testing.T) {
	policy, err := DecodeEntryPolicy([]byte(`{}`))
	if err != nil {
		t.Fatalf("decode legacy empty policy: %v", err)
	}
	if policy != DefaultEntryPolicy() {
		t.Fatalf("decoded policy = %#v", policy)
	}
}
