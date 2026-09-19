package issueworkflow

import (
	"strings"
	"testing"
)

func TestNormalizeEntryPolicyDefaultsAndTypedExecutors(t *testing.T) {
	manual, err := NormalizeEntryPolicy(EntryPolicy{})
	if err != nil {
		t.Fatalf("normalize default policy: %v", err)
	}
	if manual.Executor.Type != ExecutorNone {
		t.Fatalf("default policy = %#v", manual)
	}

	agent, err := NormalizeEntryPolicy(EntryPolicy{
		Executor:     EntryPolicyPrincipal{Type: "agent", ID: "agent-id"},
		Instructions: "Implement and publish the result.",
	})
	if err != nil || agent.Executor.Type != "agent" {
		t.Fatalf("normalize agent policy = %#v, err=%v", agent, err)
	}
}

func TestNormalizeEntryPolicyRejectsAmbiguousOrUnrunnableShapes(t *testing.T) {
	tests := []EntryPolicy{
		{Executor: EntryPolicyPrincipal{Type: "agent", ID: "agent-id"}},
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

func TestEntryPolicyRoundTrip(t *testing.T) {
	policy := DefaultEntryPolicy()
	policy.Executor = EntryPolicyPrincipal{Type: "agent", ID: "reviewer"}
	policy.Instructions = "Review the change and update its status."
	raw, _, err := EncodeEntryPolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEntryPolicy(raw)
	if err != nil || decoded != policy {
		t.Fatalf("round trip: %#v, %v", decoded, err)
	}
}
