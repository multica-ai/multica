package grants

import (
	"errors"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestValidateAcceptsCanonicalDottedCapability(t *testing.T) {
	svc := New(nil, nil, nil, nil)

	if err := svc.Validate(SubjectTypeGroup, "*", "issue.read"); err != nil {
		t.Fatalf("Validate returned %v, want nil", err)
	}
}

func TestValidateRejectsInvalidCapability(t *testing.T) {
	svc := New(nil, nil, nil, nil)

	err := svc.Validate(SubjectTypeGroup, "*", "Issue Read")
	if !errors.Is(err, ErrInvalidCapability) {
		t.Fatalf("Validate returned %v, want %v", err, ErrInvalidCapability)
	}
}

func TestToResponseWithNameSetsResolvedName(t *testing.T) {
	resp := toResponseWithName(cerebrodb.ListCerebroWorkspaceGrantsWithNamesRow{
		SubjectType:        "agent",
		SubjectDisplayName: "Mia - CEO EA",
		ResourcePattern:    "*",
		Capability:         "issues.read",
		Status:             "active",
	})
	if resp.SubjectDisplayName == nil {
		t.Fatal("SubjectDisplayName is nil, want a resolved name")
	}
	if *resp.SubjectDisplayName != "Mia - CEO EA" {
		t.Fatalf("SubjectDisplayName = %q, want %q", *resp.SubjectDisplayName, "Mia - CEO EA")
	}
}

func TestToResponseWithNameNilsEmptyName(t *testing.T) {
	// A subjectless type (workspace_default) or a deleted subject resolves to
	// "" — the response must send null so the client uses its own label.
	resp := toResponseWithName(cerebrodb.ListCerebroWorkspaceGrantsWithNamesRow{
		SubjectType:        "workspace_default",
		SubjectDisplayName: "",
		ResourcePattern:    "*",
		Capability:         "issues.read",
		Status:             "active",
	})
	if resp.SubjectDisplayName != nil {
		t.Fatalf("SubjectDisplayName = %v, want nil", *resp.SubjectDisplayName)
	}
}
