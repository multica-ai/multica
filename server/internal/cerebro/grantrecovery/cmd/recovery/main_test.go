package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

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
	apply.approvalID = "approval"
	if err := validateOptions(apply); err != nil {
		t.Fatalf("approved apply options: %v", err)
	}
}

type fakeRow struct {
	approvedBy string
	err        error
}

func (row fakeRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*(dest[0].(*string)) = row.approvedBy
	return nil
}

type fakeQuerier struct {
	args []any
	row  pgx.Row
}

func (queryer *fakeQuerier) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if !strings.Contains(query, "approver.role IN ('owner', 'admin')") || !strings.Contains(query, "approval.single_use = TRUE") {
		queryer.row = fakeRow{err: errors.New("approval query omitted security constraints")}
	}
	queryer.args = args
	return queryer.row
}

func TestConsumeRecoveryApprovalBindsApprovalToExactOperation(t *testing.T) {
	queryer := &fakeQuerier{row: fakeRow{approvedBy: "owner"}}
	opts := options{workspaceID: "workspace", approvalID: "approval"}
	approvedBy, err := consumeRecoveryApproval(context.Background(), queryer, opts, "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	if approvedBy != "owner" {
		t.Fatalf("approved by %q", approvedBy)
	}
	want := []any{"approval", "workspace", "permission_recovery:apply", "workspace:workspace:source:fingerprint", "72b52abc-6d80-4611-8fd8-5a164602d788", "production_permission_recovery_import"}
	if len(queryer.args) != len(want) {
		t.Fatalf("approval query args: %#v", queryer.args)
	}
	for index := range want {
		if queryer.args[index] != want[index] {
			t.Fatalf("approval query arg %d = %#v, want %#v", index, queryer.args[index], want[index])
		}
	}
}

func TestConsumeRecoveryApprovalRejectsMissingOrConsumedApproval(t *testing.T) {
	queryer := &fakeQuerier{row: fakeRow{err: pgx.ErrNoRows}}
	_, err := consumeRecoveryApproval(context.Background(), queryer, options{workspaceID: "workspace", approvalID: "approval"}, "fingerprint")
	if err == nil || !strings.Contains(err.Error(), "single-use recovery approval") {
		t.Fatalf("error = %v", err)
	}
}
