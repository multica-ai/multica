package entitlementtest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/entitlement"
)

func TestStubIsWorkspaceScopedAndDefaultsOff(t *testing.T) {
	stub := New()
	workspaceA, workspaceB := uuid.New(), uuid.New()
	limit := 7
	stub.Set(workspaceA, entitlement.GateIssueCount, entitlement.Decision{
		Gate: entitlement.Gate{Action: entitlement.ActionEnforce, Limit: &limit},
	})

	first := stub.Gate(context.Background(), workspaceA, entitlement.GateIssueCount)
	if first.Gate.Action != entitlement.ActionEnforce || first.Gate.Limit == nil || *first.Gate.Limit != 7 || first.Reason != entitlement.ReasonStub {
		t.Fatalf("configured = %+v", first)
	}
	*first.Gate.Limit = 99
	second := stub.Gate(context.Background(), workspaceA, entitlement.GateIssueCount)
	if second.Gate.Limit == nil || *second.Gate.Limit != 7 {
		t.Fatalf("stub leaked mutable decision: %+v", second)
	}
	missing := stub.Gate(context.Background(), workspaceB, entitlement.GateIssueCount)
	if missing.Gate.Action != entitlement.ActionOff || missing.Reason != entitlement.ReasonStub {
		t.Fatalf("missing = %+v", missing)
	}
	if calls := stub.Calls(); len(calls) != 3 || calls[0].WorkspaceID != workspaceA || calls[2].WorkspaceID != workspaceB {
		t.Fatalf("calls = %+v", calls)
	}
}
