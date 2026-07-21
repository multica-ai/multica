package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestMigration207ContainsOnlyConcurrentExternalIdentityLookupIndex(t *testing.T) {
	raw, err := os.ReadFile("207_issue_external_identity_workspace_issue_index.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ToLower(strings.TrimSpace(string(raw)))
	if normalized != "create index concurrently idx_issue_external_identity_workspace_issue on issue_external_identity(workspace_id, issue_id);" {
		t.Fatalf("unexpected migration 207 SQL: %s", raw)
	}
}
