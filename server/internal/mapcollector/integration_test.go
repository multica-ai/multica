package mapcollector

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var fixtureKey = []byte("fixture-run-key-32-bytes-exactly!!")

func TestCollectorFixtureDeterminismAndSensitiveBoundary(t *testing.T) {
	adminURL := integrationDatabaseURL(t)
	contract, canonical := loadFixtureContract(t)
	first := collectFixture(t, adminURL, contract, canonical, "source")
	second := collectFixture(t, adminURL, contract, canonical, "restore")

	firstJSON, err := first.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := second.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("equivalent source/restore reports differ\nsource=%s\nrestore=%s", firstJSON, secondJSON)
	}
	if !first.Accepted || len(first.Rejections) != 0 {
		t.Fatalf("baseline fixture rejected: %+v", first.Rejections)
	}
	if first.Tasks == nil || first.Tasks.NonterminalCount != 0 || first.Tasks.AttributionInvalidCount != 0 {
		t.Fatalf("unexpected task report: %+v", first.Tasks)
	}
	if first.Attachments == nil || first.Attachments.RowsChecked != 2 || first.Attachments.TotalBytes != 34 || first.Attachments.MissingCount != 0 || first.Attachments.HashMismatchCount != 0 {
		t.Fatalf("unexpected attachment report: %+v", first.Attachments)
	}
	for _, table := range first.Tables {
		if len(table.Buckets) != 256 {
			t.Fatalf("table %s buckets = %d, want 256", table.Name, len(table.Buckets))
		}
		for _, bucket := range table.Buckets {
			if len(bucket.HMAC256) != 64 {
				t.Fatalf("table %s bucket %d digest length = %d", table.Name, bucket.Bucket, len(bucket.HMAC256))
			}
		}
	}
	if len(first.Domains) == 0 {
		t.Fatal("domain coverage is empty")
	}
	for _, domain := range first.Domains {
		if len(domain.Buckets) != 256 {
			t.Fatalf("domain %s buckets = %d, want 256", domain.Name, len(domain.Buckets))
		}
	}
	workspaceTable := findTableReport(t, first, "workspace")
	anonymousWorkspace := keyedDigestForTest(fixtureKey, []byte("workspace:workspace-a"))
	if workspaceTable.Buckets[int(anonymousWorkspace[0])].Count == 0 {
		t.Fatalf("workspace-a not assigned to anonymous-ID bucket %d", anonymousWorkspace[0])
	}
	output := string(firstJSON)
	for _, forbidden := range []string{"owner-a@example.invalid", "sensitive body", "secret-name.txt", "https://example.invalid", "token-do-not-output", `{"secret":"provider"}`} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("report leaked forbidden content %q", forbidden)
		}
	}
	for _, requiredField := range []string{"mapping_version", "snapshot_id_hash", "enum_coverage", "orphan_count", "nonterminal_count", "storage_type_counts", "unit_fields"} {
		if !strings.Contains(output, `"`+requiredField+`"`) {
			t.Fatalf("report missing redacted field %q", requiredField)
		}
	}
}

func findTableReport(t *testing.T, report *Report, name string) TableReport {
	t.Helper()
	for _, table := range report.Tables {
		if table.Name == name {
			return table
		}
	}
	t.Fatalf("table report %s not found", name)
	return TableReport{}
}

func keyedDigestForTest(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}

func TestCollectorFailClosedFindings(t *testing.T) {
	adminURL := integrationDatabaseURL(t)
	contract, canonical := loadFixtureContract(t)
	report := collectMutatedFixture(t, adminURL, contract, canonical, `
		INSERT INTO map_fixture.issue VALUES
		  ('issue-unknown', 'workspace-a', 'missing-parent', 'future_status', 99, 'do not output title', 'do not output body');
		INSERT INTO map_fixture.task VALUES
		  ('task-queued', 'workspace-a', 'missing-agent', 'queued', 'member-owner-a', 'member-admin-a', '', ''),
		  ('task-dispatched', 'workspace-a', 'agent-private', 'dispatched', NULL, NULL, '', ''),
		  ('task-running', 'workspace-a', 'agent-private', 'running', NULL, NULL, '', ''),
		  ('task-waiting', 'workspace-a', 'agent-private', 'waiting', NULL, NULL, '', '');
		INSERT INTO map_fixture.agent_target VALUES
		  ('target-private', 'agent-private', 'workspace', 'workspace-a', 'invocation', 'invoke', 'none'),
		  ('target-unknown', 'agent-public', 'future_scope', 'workspace-a', 'future_permission_scope', 'future_action', 'future_inheritance'),
		  ('target-cross-workspace', 'agent-public', 'member', 'member-user-b', 'invocation', 'invoke', 'none');
		INSERT INTO map_fixture.attachment VALUES
		  ('attachment-missing', 'workspace-a', 'issue-root', 'member-owner-a', 'local', 'objects/missing.bin', 1, repeat('0', 64), 'do-not-output.bin', 'https://example.invalid/missing'),
		  ('attachment-corrupt', 'workspace-b', 'issue-root', 'member-user-b', 'local', 'objects/object-a.bin', 999, repeat('f', 64), 'do-not-output-corrupt.bin', 'https://example.invalid/corrupt');
		INSERT INTO map_fixture.usage VALUES
		  ('usage-invalid', '', -1, 0, -2, 'do not output provider payload');
		DELETE FROM map_fixture.member WHERE id = 'member-owner-b';
	`)
	if report.Accepted {
		t.Fatal("mutated fixture was accepted")
	}
	reasons := make(map[string]int)
	for _, rejection := range report.Rejections {
		reasons[rejection.ReasonCode]++
	}
	for _, reason := range []string{
		"ENUM_UNKNOWN", "REFERENCE_ORPHAN", "CROSS_WORKSPACE_REF", "NONTERMINAL_TASK",
		"ATTRIBUTION_INVALID", "PERMISSION_UNPROVEN", "MISSING_OWNER", "ATTACHMENT_OBJECT_MISSING",
		"ATTACHMENT_HASH_MISMATCH", "ATTACHMENT_SIZE_MISMATCH", "USAGE_SEMANTICS_UNKNOWN",
	} {
		if reasons[reason] == 0 {
			t.Fatalf("missing rejection reason %s: %#v", reason, reasons)
		}
	}
	if report.Tasks == nil || report.Tasks.NonterminalCount != 4 {
		t.Fatalf("nonterminal task count = %+v, want 4", report.Tasks)
	}
	if report.Attachments == nil || report.Attachments.MissingCount != 1 || report.Attachments.HashMismatchCount != 1 || report.Attachments.SizeMismatchCount != 1 {
		t.Fatalf("attachment failures = %+v", report.Attachments)
	}
	assertAllContractEnumsObserved(t, contract, report)
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"future_status", "future_scope", "do not output", "missing.bin", "https://example.invalid"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("rejection report leaked %q", forbidden)
		}
	}
}

func assertAllContractEnumsObserved(t *testing.T, contract *Contract, report *Report) {
	t.Helper()
	for _, tableContract := range contract.Tables {
		tableReport := findTableReport(t, report, tableContract.Name)
		for _, field := range tableContract.Fields {
			for _, allowed := range field.Enum {
				if tableReport.EnumCoverage[field.Name][allowed] == 0 {
					t.Fatalf("allowed enum %s.%s=%s was not covered", tableContract.Name, field.Name, allowed)
				}
			}
		}
	}
}

func TestCollectorRejectsAttachmentSymlinkEscape(t *testing.T) {
	adminURL := integrationDatabaseURL(t)
	contract, canonical := loadFixtureContract(t)
	_, pool := fixtureDatabase(t, adminURL, "symlink_escape")
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(outside, []byte("fixture-object-a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.bin")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE map_fixture.attachment
		   SET storage_key = 'escape.bin'
		 WHERE id = 'attachment-a'
	`); err != nil {
		t.Fatal(err)
	}
	report, err := Collect(context.Background(), pool, contract, canonical, fixtureKey, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Accepted {
		t.Fatal("symlink escape fixture was accepted")
	}
	found := false
	for _, rejection := range report.Rejections {
		if rejection.ReasonCode == "ATTACHMENT_PATH_ESCAPE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing ATTACHMENT_PATH_ESCAPE rejection: %+v", report.Rejections)
	}
}

func TestCollectorRejectsSchemaDrift(t *testing.T) {
	adminURL := integrationDatabaseURL(t)
	contract, canonical := loadFixtureContract(t)
	dbName, pool := fixtureDatabase(t, adminURL, "schema_drift")
	_ = dbName
	if _, err := pool.Exec(context.Background(), `ALTER TABLE map_fixture.workspace ADD COLUMN unclassified_secret text`); err != nil {
		t.Fatal(err)
	}
	_, err := Collect(context.Background(), pool, contract, canonical, fixtureKey, "testdata")
	if err == nil || !strings.Contains(err.Error(), "unclassified columns") {
		t.Fatalf("expected schema drift rejection, got %v", err)
	}
}

func integrationDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("MAPCOLLECTOR_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("MAPCOLLECTOR_TEST_DATABASE_URL is required for PostgreSQL 17.10 integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Skipf("PostgreSQL fixture is unavailable: %v", err)
	}
	defer conn.Close(ctx)
	var versionNumber string
	if err := conn.QueryRow(ctx, `SELECT current_setting('server_version_num')`).Scan(&versionNumber); err != nil || versionNumber != "170010" {
		t.Skipf("PostgreSQL 17.10 required, got server_version_num %q (%v)", versionNumber, err)
	}
	return url
}

func loadFixtureContract(t *testing.T) (*Contract, []byte) {
	t.Helper()
	contract, canonical, err := LoadContract(filepath.Join("testdata", "contract.json"))
	if err != nil {
		t.Fatalf("load fixture contract: %v", err)
	}
	return contract, canonical
}

func collectFixture(t *testing.T, adminURL string, contract *Contract, canonical []byte, suffix string) *Report {
	t.Helper()
	_, pool := fixtureDatabase(t, adminURL, suffix)
	report, err := Collect(context.Background(), pool, contract, canonical, fixtureKey, "testdata")
	if err != nil {
		t.Fatalf("collect fixture: %v", err)
	}
	return report
}

func collectMutatedFixture(t *testing.T, adminURL string, contract *Contract, canonical []byte, mutation string) *Report {
	t.Helper()
	_, pool := fixtureDatabase(t, adminURL, "mutated")
	if _, err := pool.Exec(context.Background(), mutation); err != nil {
		t.Fatalf("mutate fixture: %v", err)
	}
	report, err := Collect(context.Background(), pool, contract, canonical, fixtureKey, "testdata")
	if err != nil {
		t.Fatalf("collect mutated fixture: %v", err)
	}
	return report
}

func fixtureDatabase(t *testing.T, adminURL, suffix string) (string, *pgxpool.Pool) {
	t.Helper()
	name := fmt.Sprintf("new31_map_%s_%d", suffix, time.Now().UnixNano())
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
		admin.Close(ctx)
		t.Fatalf("create fixture database: %v", err)
	}
	admin.Close(ctx)
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), adminURL)
		if err != nil {
			t.Logf("connect for cleanup: %v", err)
			return
		}
		defer conn.Close(context.Background())
		if _, err := conn.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name)); err != nil {
			t.Logf("drop fixture database: %v", err)
		}
	})
	pool, err := pgxpool.New(ctx, replaceDatabase(adminURL, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fixture, err := os.ReadFile(filepath.Join("testdata", "fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(fixture)); err != nil {
		t.Fatalf("apply fixture: %v", err)
	}
	return name, pool
}

func replaceDatabase(url, name string) string {
	index := strings.LastIndex(url, "/")
	if index < 0 {
		return url
	}
	rest := url[index+1:]
	if query := strings.Index(rest, "?"); query >= 0 {
		return url[:index+1] + name + rest[query:]
	}
	return url[:index+1] + name
}
