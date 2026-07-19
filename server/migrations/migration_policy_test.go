package migrations

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestExternalIdentityMigrationsFollowCurrentPolicy(t *testing.T) {
	inlineUnique := regexp.MustCompile(`(?m)\bunique\s*\(`)
	files, err := filepath.Glob("*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		versionText, _, ok := strings.Cut(filepath.Base(path), "_")
		if !ok {
			continue
		}
		version, err := strconv.Atoi(versionText)
		if err != nil || version < 202 {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(raw))
		for _, forbidden := range []string{"foreign key", "references issue", "on delete cascade", "on update cascade"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s contains forbidden database-owned relationship %q", path, forbidden)
			}
		}
		if strings.Contains(lower, "primary key") || inlineUnique.MatchString(lower) {
			t.Errorf("%s creates an implicit non-concurrent primary/unique index", path)
		}
		if strings.Contains(lower, "create index") || strings.Contains(lower, "create unique index") {
			if !strings.Contains(lower, "create index concurrently") && !strings.Contains(lower, "create unique index concurrently") {
				t.Errorf("%s creates a non-concurrent index", path)
			}
			statements := 0
			for _, statement := range strings.Split(string(raw), ";") {
				lines := strings.Split(statement, "\n")
				var content []string
				for _, line := range lines {
					if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "--") {
						content = append(content, trimmed)
					}
				}
				if len(content) > 0 {
					statements++
				}
			}
			if statements != 1 {
				t.Errorf("%s contains %d SQL statements; concurrent index migrations must contain exactly one", path, statements)
			}
		}
	}
}

func TestWorkspaceDeletionOwnsRaceSafeExternalIdentityCleanupInApplicationTransaction(t *testing.T) {
	raw, err := os.ReadFile("../pkg/db/queries/workspace.sql")
	if err != nil {
		t.Fatal(err)
	}
	query := string(raw)
	deleteWorkspace := strings.Index(query, "deleted_workspace AS (\n    DELETE FROM workspace WHERE workspace.id = $1")
	cleanup := strings.LastIndex(query, "DELETE FROM issue_external_identity")
	dependsOnDelete := strings.LastIndex(query, "WHERE issue_external_identity.workspace_id IN (SELECT id FROM deleted_workspace)")
	if deleteWorkspace < 0 || cleanup < deleteWorkspace || dependsOnDelete < cleanup {
		t.Fatal("DeleteWorkspace must delete the parent and then clean external identities through a dependent statement")
	}
}

func TestIssueDeletionOwnsExternalIdentityCleanupInApplicationTransaction(t *testing.T) {
	queryBytes, err := os.ReadFile("../pkg/db/queries/issue_external_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	queries := string(queryBytes)
	if !strings.Contains(queries, "-- name: DeleteIssueExternalIdentitiesByIssue :exec") ||
		!strings.Contains(queries, "DELETE FROM issue_external_identity") {
		t.Fatal("external identity query set does not expose explicit issue cleanup")
	}

	handlerBytes, err := os.ReadFile("../internal/handler/issue.go")
	if err != nil {
		t.Fatal(err)
	}
	handler := string(handlerBytes)
	start := strings.Index(handler, "func (h *Handler) DeleteIssue")
	if start < 0 {
		t.Fatal("DeleteIssue handler not found")
	}
	handler = handler[start:]
	firstCleanup := strings.Index(handler, "qtx.DeleteIssueExternalIdentitiesByIssue")
	deleteIssue := strings.Index(handler, "qtx.DeleteIssue(")
	lastCleanup := strings.LastIndex(handler[:strings.Index(handler, "func (h *Handler) BatchUpdateIssues")], "qtx.DeleteIssueExternalIdentitiesByIssue")
	commit := strings.Index(handler, "tx.Commit(r.Context())")
	if firstCleanup < 0 || deleteIssue < 0 || lastCleanup < 0 || commit < 0 || !(firstCleanup < deleteIssue && deleteIssue < lastCleanup && lastCleanup < commit) {
		t.Fatal("DeleteIssue must clean aliases, delete the issue, re-clean aliases, and commit in that order in one application transaction")
	}
}
