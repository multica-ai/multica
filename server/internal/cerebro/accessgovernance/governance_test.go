package accessgovernance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type fakeGrantSource struct {
	grants []Grant
	calls  int
}

func (f *fakeGrantSource) ListGrants(context.Context) ([]Grant, error) {
	f.calls++
	return f.grants, nil
}

type fakeFindingReporter struct {
	findings []Finding
	calls    int
}

type fakeAccessTightener struct {
	findings []Finding
	calls    int
}

func (f *fakeAccessTightener) Tighten(_ context.Context, findings []Finding, _ time.Time) error {
	f.calls++
	f.findings = append([]Finding(nil), findings...)
	return nil
}

func (f *fakeFindingReporter) Report(_ context.Context, findings []Finding) error {
	f.calls++
	f.findings = append([]Finding(nil), findings...)
	return nil
}

func TestScanOnlyProducesTighteningFindingsOrderedBySeverity(t *testing.T) {
	now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	unused := now.Add(-91 * 24 * time.Hour)
	findings := Scan(now, 90*24*time.Hour, []Grant{
		{ID: "unused", SubjectPresent: true, OwnerID: "owner", LastUsedAt: &unused},
		{ID: "expired", SubjectPresent: true, OwnerID: "owner", ExpiresAt: &expired},
		{ID: "orphan", SubjectPresent: false, OwnerID: ""},
	})
	if len(findings) != 3 {
		t.Fatalf("got %d findings, want 3", len(findings))
	}
	want := []Severity{SeverityCritical, SeverityHigh, SeverityMedium}
	for i, finding := range findings {
		if finding.Severity != want[i] || !finding.TightenOnly {
			t.Fatalf("finding %d = %#v, want severity %s and tighten-only", i, finding, want[i])
		}
	}
}

func TestScanNeverSuggestsOpeningAccess(t *testing.T) {
	findings := Scan(time.Now(), 0, []Grant{{ID: "active", SubjectPresent: true, OwnerID: "owner"}})
	if len(findings) != 0 {
		t.Fatalf("active access produced findings: %#v", findings)
	}
}

func TestSweeperTickLoadsLiveGrantsAndReportsFindings(t *testing.T) {
	now := time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	source := &fakeGrantSource{grants: []Grant{
		{ID: "expired", SubjectPresent: true, OwnerID: "owner", ExpiresAt: &expired},
		{ID: "orphan", SubjectPresent: false, OwnerID: ""},
	}}
	reporter := &fakeFindingReporter{}
	tightener := &fakeAccessTightener{}
	sweeper := NewSweeper(source, tightener, reporter)
	sweeper.Now = func() time.Time { return now }

	findings, err := sweeper.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if source.calls != 1 || tightener.calls != 1 || reporter.calls != 1 {
		t.Fatalf("source calls=%d tightener calls=%d reporter calls=%d, want one live pass each", source.calls, tightener.calls, reporter.calls)
	}
	if len(findings) != 2 || len(tightener.findings) != 2 || len(reporter.findings) != 2 {
		t.Fatalf("findings=%#v tightened=%#v reported=%#v, want two", findings, tightener.findings, reporter.findings)
	}
	if findings[0].Severity != SeverityCritical || findings[1].Severity != SeverityHigh {
		t.Fatalf("findings not severity ordered: %#v", findings)
	}
	for _, finding := range findings {
		if !finding.TightenOnly {
			t.Fatalf("live sweeper reported a widening action: %#v", finding)
		}
	}
}

func TestScanTreatsNeverUsedOldBindingAsUnused(t *testing.T) {
	now := time.Date(2026, 7, 19, 14, 0, 0, 0, time.UTC)
	findings := Scan(now, 90*24*time.Hour, []Grant{{
		ID:             "never-used",
		SubjectPresent: true,
		AddedAt:        now.Add(-91 * 24 * time.Hour),
	}})
	if len(findings) != 1 || findings[0].Severity != SeverityMedium {
		t.Fatalf("findings=%#v, want one medium unused-access finding", findings)
	}
}

func TestScanDoesNotTreatSystemOwnedBindingAsOrphaned(t *testing.T) {
	findings := Scan(time.Now(), 90*24*time.Hour, []Grant{{
		ID:             "system-owned",
		SubjectPresent: true,
		OwnerID:        "",
		AddedAt:        time.Now(),
	}})
	if len(findings) != 0 {
		t.Fatalf("system-owned access was treated as orphaned: %#v", findings)
	}
}

func TestBindingTargetsForTighteningRejectsAnyWideningAction(t *testing.T) {
	_, err := bindingTargetsForTightening([]Finding{{
		GrantID: "unsafe", RoleID: "role", SubjectType: "agent", SubjectID: "subject",
		BindingAddedAt: time.Now(), TightenOnly: false,
	}})
	if err == nil {
		t.Fatal("non-tighten finding must be rejected before a database transaction starts")
	}
}

func TestBindingTargetsForTighteningDeduplicatesRoleAssignment(t *testing.T) {
	findings := []Finding{
		{GrantID: "one", RoleID: "role", SubjectType: "agent", SubjectID: "subject", BindingAddedAt: time.Now(), TightenOnly: true},
		{GrantID: "two", RoleID: "role", SubjectType: "agent", SubjectID: "subject", BindingAddedAt: time.Now(), TightenOnly: true},
	}
	targets, err := bindingTargetsForTightening(findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets=%#v, want one role-assignment expiry", targets)
	}
}

func TestDatabaseGrantSourceReadsMigratedSchema(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not configured; skipping governance integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("DATABASE_URL not reachable: %v", err)
	}

	if _, err := (&databaseGrantSource{pool: pool}).ListGrants(ctx); err != nil {
		t.Fatalf("list live grants from migrated schema: %v", err)
	}
}

// TestDatabaseTightenerNeverExtendsExpiry proves the FIR-3403 acceptance
// criterion "governance can only tighten" against the REAL SQL, not the
// hard-coded TightenOnly constant. The tightener's monotonic guarantee lives in
// `SET expires_at = LEAST(COALESCE(expires_at,$5),$5)` (sweeper.go): a later
// sweep may never push an expiry further out or reopen access; an earlier sweep
// may tighten it further. This drives Tighten three times against a seeded role
// assignment and asserts each transition directly on the stored value.
func TestDatabaseTightenerNeverExtendsExpiry(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not configured; skipping governance integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("DATABASE_URL not reachable: %v", err)
	}

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("rand: %v", err)
	}
	slug := "gov-least-" + hex.EncodeToString(suffix)

	var workspaceID, roleID, subjectID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ('gov-least-test', $1) RETURNING id`, slug,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	// ON DELETE CASCADE removes the role and its assignment with the workspace.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	if err := pool.QueryRow(ctx,
		`INSERT INTO cerebro_role (workspace_id, name) VALUES ($1, 'least-role') RETURNING id`, workspaceID,
	).Scan(&roleID); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&subjectID); err != nil {
		t.Fatalf("gen subject id: %v", err)
	}

	addedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx,
		`INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_at, expires_at)
		 VALUES ($1, 'agent', $2, $3, NULL)`, roleID, subjectID, addedAt,
	); err != nil {
		t.Fatalf("seed role assignment: %v", err)
	}

	tightener := &databaseAccessTightener{pool: pool}
	finding := Finding{
		GrantID: "g", RoleID: roleID, SubjectType: "agent", SubjectID: subjectID,
		BindingAddedAt: addedAt, TightenOnly: true,
	}
	readExpiry := func() time.Time {
		t.Helper()
		var got *time.Time
		if err := pool.QueryRow(ctx,
			`SELECT expires_at FROM cerebro_role_assignment WHERE role_id = $1 AND subject_id = $2`,
			roleID, subjectID,
		).Scan(&got); err != nil {
			t.Fatalf("read expiry: %v", err)
		}
		if got == nil {
			t.Fatalf("expiry unexpectedly NULL")
		}
		return *got
	}

	// First sweep sets the NULL expiry to the sweep time.
	t1 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := tightener.Tighten(ctx, []Finding{finding}, t1); err != nil {
		t.Fatalf("first tighten: %v", err)
	}
	if got := readExpiry(); !got.Equal(t1) {
		t.Fatalf("after first sweep expiry = %s, want %s", got, t1)
	}

	// A LATER sweep must NOT extend the expiry — the core monotonic guarantee.
	t2 := t1.Add(48 * time.Hour)
	if err := tightener.Tighten(ctx, []Finding{finding}, t2); err != nil {
		t.Fatalf("later tighten: %v", err)
	}
	if got := readExpiry(); !got.Equal(t1) {
		t.Fatalf("a later sweep extended the expiry to %s; LEAST must keep %s", got, t1)
	}

	// An EARLIER sweep may tighten further (closes access sooner).
	t0 := t1.Add(-24 * time.Hour)
	if err := tightener.Tighten(ctx, []Finding{finding}, t0); err != nil {
		t.Fatalf("earlier tighten: %v", err)
	}
	if got := readExpiry(); !got.Equal(t0) {
		t.Fatalf("an earlier sweep must tighten to %s, got %s", t0, got)
	}
}
