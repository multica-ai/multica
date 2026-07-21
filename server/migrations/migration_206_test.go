package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestMigration206ContainsOnlyConcurrentExternalIdentityUniqueIndex(t *testing.T) {
	raw, err := os.ReadFile("206_issue_external_identity_key_unique_index.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ToLower(strings.TrimSpace(string(raw)))
	if normalized != "create unique index concurrently uq_issue_external_identity_workspace_namespace_external_id on issue_external_identity(workspace_id, namespace, external_id);" {
		t.Fatalf("unexpected migration 206 SQL: %s", raw)
	}
}
