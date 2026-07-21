package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestMigration204ContainsOnlyConcurrentAuthorityNonceExpiryIndex(t *testing.T) {
	raw, err := os.ReadFile("204_authority_nonce_expires_index.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ToLower(strings.TrimSpace(string(raw)))
	if normalized != "create index concurrently if not exists idx_authority_nonce_expires_at on authority_nonce(expires_at);" {
		t.Fatalf("unexpected migration 204 SQL: %s", raw)
	}
}
