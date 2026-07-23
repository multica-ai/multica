package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/cerebro/grantrecovery"
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
	values     []string
	err        error
}

func (row fakeRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(row.values) > 0 {
		for index, value := range row.values {
			*(dest[index].(*string)) = value
		}
		return nil
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

type identityQueryer struct {
	row pgx.Row
}

func (queryer identityQueryer) QueryRow(context.Context, string, ...any) pgx.Row {
	return queryer.row
}

type nullableStringRow struct {
	value *string
}

func (row nullableStringRow) Scan(dest ...any) error {
	*(dest[0].(**string)) = row.value
	return nil
}

type canonicalQueryer struct {
	values map[string]*string
}

func (queryer canonicalQueryer) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	return nullableStringRow{value: queryer.values[args[0].(string)]}
}

func TestCanonicalizeLegacyGrantsMapsAliasesAndQuarantinesUnknownKeys(t *testing.T) {
	canonical := "web_fetch"
	grants, err := canonicalizeLegacyGrants(context.Background(), canonicalQueryer{values: map[string]*string{
		"tools:WebFetch": &canonical,
	}}, []grantrecovery.LegacyGrant{
		{WorkspaceID: "workspace", AgentID: "agent", ToolName: "tools:WebFetch", Enabled: true},
		{WorkspaceID: "workspace", AgentID: "agent", ToolName: "unknown", Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if grants[0].ToolName != "web_fetch" {
		t.Fatalf("alias mapped to %q, want web_fetch", grants[0].ToolName)
	}
	if grants[1].ToolName != "" {
		t.Fatalf("unknown key mapped to %q, want quarantined empty identity", grants[1].ToolName)
	}
}

func TestRequireSeparateDatabasesRejectsEquivalentConnections(t *testing.T) {
	source := identityQueryer{row: fakeRow{values: []string{"10.0.0.1", "5432", "database"}}}
	sameTarget := identityQueryer{row: fakeRow{values: []string{"10.0.0.1", "5432", "database"}}}
	if err := requireSeparateDatabases(context.Background(), source, sameTarget); err == nil {
		t.Fatal("equivalent URL variants resolving to the same physical database must be rejected")
	}

	separateTarget := identityQueryer{row: fakeRow{values: []string{"10.0.0.2", "5432", "database"}}}
	if err := requireSeparateDatabases(context.Background(), source, separateTarget); err != nil {
		t.Fatalf("separate databases rejected: %v", err)
	}
}
