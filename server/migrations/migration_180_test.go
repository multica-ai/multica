package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestMigration180OwnsOnlyItsObjectsAndDoesNotMutateIssueConstraints(t *testing.T) {
	upBytes, err := os.ReadFile("180_issue_external_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := os.ReadFile("180_issue_external_identity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down := strings.ToLower(string(upBytes)), strings.ToLower(string(downBytes))
	for _, forbidden := range []string{"uq_issue_workspace_id", "alter table issue", "unique (workspace_id, id)"} {
		if strings.Contains(up, forbidden) || strings.Contains(down, forbidden) {
			t.Fatalf("migration 180 contains destructive/pre-existing issue constraint operation %q", forbidden)
		}
	}
	if !strings.Contains(up, "references issue(id)") || !strings.Contains(up, "issue_external_identity_workspace_180") {
		t.Fatal("migration 180 must use the existing issue primary key and its owned same-workspace trigger")
	}
	for _, required := range []string{"drop trigger if exists issue_external_identity_workspace_180", "drop table if exists issue_external_identity", "drop function if exists issue_external_identity_enforce_workspace_180"} {
		if !strings.Contains(down, required) {
			t.Fatalf("down migration missing owned-object cleanup %q", required)
		}
	}
}
