package main

import "testing"

func TestValidateOptionsRequiresSeparateSourceAndApprovalForApply(t *testing.T) {
	base := options{sourceURL: "postgres://source", targetURL: "postgres://target", workspaceID: "workspace"}
	if err := validateOptions(base); err != nil {
		t.Fatalf("dry-run options: %v", err)
	}
	same := base
	same.targetURL = same.sourceURL
	if err := validateOptions(same); err == nil {
		t.Fatal("source and target must be separate databases")
	}
	apply := base
	apply.apply = true
	if err := validateOptions(apply); err == nil {
		t.Fatal("apply without recorded approval must fail")
	}
	apply.approvedBy = "member"
	apply.approvalReference = "FIR-3388 comment"
	if err := validateOptions(apply); err != nil {
		t.Fatalf("approved apply options: %v", err)
	}
}
