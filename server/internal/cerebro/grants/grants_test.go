package grants

import (
	"errors"
	"testing"
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
