package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestMigration203ContainsOnlyConcurrentAuthorityNonceUniqueIndex(t *testing.T) {
	raw, err := os.ReadFile("203_authority_nonce_hash_unique_index.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ToLower(strings.TrimSpace(string(raw)))
	if normalized != "create unique index concurrently if not exists uq_authority_nonce_nonce_hash on authority_nonce(nonce_hash);" {
		t.Fatalf("unexpected migration 203 SQL: %s", raw)
	}
}
