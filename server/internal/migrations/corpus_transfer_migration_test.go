package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorpusTransferMigrationsPreserveLedgerContract(t *testing.T) {
	dir := realMigrationsDir(t)
	tableFiles := []string{
		"265_corpus_transfer.up.sql",
		"269_corpus_transfer_ack.up.sql",
	}
	for _, name := range tableFiles {
		body := readMigrationForTest(t, dir, name)
		upper := strings.ToUpper(body)
		if strings.Contains(upper, "FOREIGN KEY") || strings.Contains(upper, "REFERENCES") {
			t.Errorf("%s must not add foreign keys", name)
		}
	}

	transfer := readMigrationForTest(t, dir, "265_corpus_transfer.up.sql")
	for _, state := range []string{"created", "uploading", "uploaded", "verifying", "confirmed", "acked", "failed", "expired", "purged"} {
		if !strings.Contains(transfer, "'"+state+"'") {
			t.Errorf("transfer state check is missing %q", state)
		}
	}
	if !strings.Contains(transfer, "2147483648") {
		t.Error("transfer schema must enforce the 2 GiB P0 limit")
	}
	for _, invariant := range []string{
		"(verified_size_bytes IS NULL) = (verified_sha256 IS NULL)",
		"(state IN ('confirmed', 'acked', 'purged')) = (verified_size_bytes IS NOT NULL AND verified_sha256 IS NOT NULL)",
		"(verification_token IS NULL) = (verification_lease_expires_at IS NULL)",
		"(state = 'verifying') = (verification_token IS NOT NULL AND verification_lease_expires_at IS NOT NULL)",
		"(state = 'failed') = (failure_code IS NOT NULL)",
		"cleanup_pending = (cleanup_next_attempt_at IS NOT NULL)",
		"NOT cleanup_pending OR state IN ('failed', 'expired', 'purged')",
	} {
		if !strings.Contains(transfer, invariant) {
			t.Errorf("transfer schema is missing invariant %q", invariant)
		}
	}

	for name, prefix := range map[string]string{
		"266_corpus_transfer_primary_index.up.sql":     "CREATE UNIQUE INDEX CONCURRENTLY",
		"268_corpus_transfer_idempotency_index.up.sql": "CREATE UNIQUE INDEX CONCURRENTLY",
		"270_corpus_transfer_ack_primary_index.up.sql": "CREATE UNIQUE INDEX CONCURRENTLY",
		"272_corpus_transfer_cleanup_due_index.up.sql": "CREATE INDEX CONCURRENTLY",
		"273_corpus_transfer_expiry_due_index.up.sql":  "CREATE INDEX CONCURRENTLY",
	} {
		body := readMigrationForTest(t, dir, name)
		if !strings.Contains(strings.ToUpper(body), prefix) {
			t.Errorf("%s must create its index concurrently", name)
		}
		if strings.Count(body, ";") != 1 {
			t.Errorf("%s must contain exactly one SQL statement", name)
		}
	}

	for _, name := range []string{
		"266_corpus_transfer_primary_index.down.sql",
		"270_corpus_transfer_ack_primary_index.down.sql",
		"272_corpus_transfer_cleanup_due_index.down.sql",
		"273_corpus_transfer_expiry_due_index.down.sql",
	} {
		body := strings.ToUpper(readMigrationForTest(t, dir, name))
		if !strings.Contains(body, "DROP INDEX CONCURRENTLY IF EXISTS") {
			t.Errorf("%s must tolerate the primary-key constraint having dropped its owned index", name)
		}
	}

	queryBody, err := os.ReadFile(filepath.Join(dir, "..", "pkg", "db", "queries", "corpus_transfer.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, predicate := range []string{
		"expected_size_bytes = sqlc.arg('verified_size_bytes')",
		"expected_sha256 = sqlc.arg('verified_sha256')",
		"verification_token = sqlc.arg('verification_token')\n  AND expires_at > now()",
	} {
		if !strings.Contains(string(queryBody), predicate) {
			t.Errorf("confirmation CAS is missing %q", predicate)
		}
	}
	for _, query := range []string{
		"ClaimNextCorpusTransferForCleanup",
		"RetryCorpusTransferCleanup",
		"ScheduleCorpusTransferCleanupPass",
		"CompleteCorpusTransferCleanup",
	} {
		if !strings.Contains(string(queryBody), query) {
			t.Errorf("cleanup ledger query %q is missing", query)
		}
	}
	for _, predicate := range []string{
		"state IN ('created', 'uploading', 'uploaded', 'verifying', 'confirmed', 'acked')",
		"WHEN transfer.state IN ('confirmed', 'acked') THEN 'purged'",
		"WHEN transfer.state IN ('created', 'uploading', 'uploaded', 'verifying') THEN 'expired'",
		"ELSE transfer.state",
	} {
		if !strings.Contains(string(queryBody), predicate) {
			t.Errorf("cleanup must reclaim abandoned uploaded transfers; missing %q", predicate)
		}
	}
}

func readMigrationForTest(t *testing.T, dir, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}
