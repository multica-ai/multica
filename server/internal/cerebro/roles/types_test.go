package roles

import (
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestRoleAssignmentResponseFromNamedRowSetsName(t *testing.T) {
	resp := roleAssignmentResponseFromNamedRow(cerebrodb.ListCerebroRoleAssignmentsWithNamesRow{
		SubjectType:        "agent",
		SubjectDisplayName: "Mia - CEO EA",
	})
	if resp.SubjectDisplayName == nil {
		t.Fatal("SubjectDisplayName is nil, want a resolved name")
	}
	if *resp.SubjectDisplayName != "Mia - CEO EA" {
		t.Fatalf("SubjectDisplayName = %q, want %q", *resp.SubjectDisplayName, "Mia - CEO EA")
	}
}

func TestRoleAssignmentResponseFromNamedRowNilsEmptyName(t *testing.T) {
	resp := roleAssignmentResponseFromNamedRow(cerebrodb.ListCerebroRoleAssignmentsWithNamesRow{
		SubjectType:        "agent",
		SubjectDisplayName: "",
	})
	if resp.SubjectDisplayName != nil {
		t.Fatalf("SubjectDisplayName = %v, want nil", *resp.SubjectDisplayName)
	}
}
