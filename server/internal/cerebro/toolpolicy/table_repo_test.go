package toolpolicy

// Integration tests for the per-repo half of the table read model
// (table_repo.go). They prove that the workspace's repos (workspace.repos ∪ each
// project's github_repo resource) turn into one row per repo capability, each
// carrying its (tool, repo) cell's per-layer settings and resolved Effective —
// the shape the collapsible repo group renders. They share the store_test.go
// fixture (TestMain) and skip cleanly when no test database is reachable.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// setWorkspaceRepos writes the workspace-level repo list (the JSON array the
// daemon reads as []RepoData).
func setWorkspaceRepos(t *testing.T, s *Store, reposJSON string) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(),
		`UPDATE workspace SET repos = $2 WHERE id = $1`, tpTestWorkspaceID, []byte(reposJSON)); err != nil {
		t.Fatalf("set workspace repos: %v", err)
	}
}

// addProjectRepo creates a project with one github_repo resource pointing at
// repoURL and returns nothing — discoverRepoURLs reads them back by workspace.
func addProjectRepo(t *testing.T, s *Store, repoURL string) {
	t.Helper()
	var projectID pgtype.UUID
	if err := s.pool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Repo Test Project')
		RETURNING id
	`, tpTestWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, position)
		VALUES ($1, $2, 'github_repo', $3, 0)
	`, projectID, tpTestWorkspaceID, []byte(`{"url":"`+repoURL+`"}`)); err != nil {
		t.Fatalf("insert project resource: %v", err)
	}
}

// clearRepos resets the repo universe so each subtest controls it exactly:
// empties workspace.repos and drops every project (project_resource cascades).
func clearRepos(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM project_resource WHERE workspace_id = $1`, tpTestWorkspaceID); err != nil {
		t.Fatalf("clear project resources: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM project WHERE workspace_id = $1`, tpTestWorkspaceID); err != nil {
		t.Fatalf("clear projects: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE workspace SET repos = '[]'::jsonb WHERE id = $1`, tpTestWorkspaceID); err != nil {
		t.Fatalf("reset workspace repos: %v", err)
	}
}

func findRepoRow(rows []TableRow, key, pattern string) (TableRow, bool) {
	for _, r := range rows {
		if r.ToolKey == key && r.ResourcePattern == pattern {
			return r, true
		}
	}
	return TableRow{}, false
}

// TestTable_RepoRowsPerCapability is the core slice-2 check: every repo in the
// workspace becomes three rows (read / checkout / push), the workspace and
// project sources are unioned and deduplicated, and per-(tool, repo) settings
// resolve independently — all alongside the capability-wide rows.
func TestTable_RepoRowsPerCapability(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	clearRepos(t, s)
	defer clearRepos(t, s)
	ctx := context.Background()

	const (
		repoA = "github.com/firtal-group/repo-a"
		repoB = "github.com/firtal-group/repo-b"
		repoC = "github.com/firtal-group/repo-c"
	)
	// repoB appears in BOTH sources to prove the union deduplicates it.
	setWorkspaceRepos(t, s, `[{"url":"`+repoA+`"},{"url":"`+repoB+`"}]`)
	addProjectRepo(t, s, repoB)
	addProjectRepo(t, s, repoC)

	// One ordinary capability so we can assert repo rows coexist with the
	// capability-wide listing rather than replacing it.
	addCap(t, s, "add_comment", "Add comment", "Issues", "builtin")

	agent, user := uuidByte(2), tpTestUserID
	// repo.checkout on repoA: agent Allow → effective Allow on that one cell.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: capRepoCheckout, Layer: LayerAgent, SubjectID: agent, ResourcePattern: repoA, Setting: SettingAllow}); err != nil {
		t.Fatalf("set agent checkout repoA: %v", err)
	}
	// repo.push on repoC: user Deny → effective Deny capped by user on that cell.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: capRepoPush, Layer: LayerUser, SubjectID: user, ResourcePattern: repoC, Setting: SettingDeny}); err != nil {
		t.Fatalf("set user push repoC: %v", err)
	}

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, AgentID: agent, UserID: user})
	if err != nil {
		t.Fatalf("table: %v", err)
	}

	// 3 repos × 3 capabilities = 9 repo rows, none of them duplicated for repoB.
	repoRowCount := 0
	for _, r := range rows {
		if r.Category == repoCategory {
			repoRowCount++
		}
	}
	if repoRowCount != 9 {
		t.Fatalf("got %d repo rows, want 9 (3 repos × 3 caps, repoB deduped)", repoRowCount)
	}

	// The capability-wide row is still there.
	if _, ok := findRow(rows, "add_comment"); !ok {
		t.Fatal("capability-wide row add_comment missing — repo rows must not replace the listing")
	}

	// Authored cell: agent Allow surfaces and resolves to Allow.
	checkoutA, ok := findRepoRow(rows, capRepoCheckout, repoA)
	if !ok {
		t.Fatalf("repo.checkout row for %s missing", repoA)
	}
	if checkoutA.Layers[LayerAgent] != SettingAllow || checkoutA.Effective.Setting != SettingAllow {
		t.Fatalf("checkout repoA = layer %q / effective %q, want agent allow / allow", checkoutA.Layers[LayerAgent], checkoutA.Effective.Setting)
	}
	if checkoutA.Title != "Check out" || checkoutA.Source != repoSource {
		t.Fatalf("checkout repoA labels = %q/%q, want human title + repo source", checkoutA.Title, checkoutA.Source)
	}

	// Capped cell: user Deny on push repoC wins.
	pushC, ok := findRepoRow(rows, capRepoPush, repoC)
	if !ok {
		t.Fatalf("repo.push row for %s missing", repoC)
	}
	if pushC.Effective.Setting != SettingDeny || pushC.Effective.CappedBy != LayerUser {
		t.Fatalf("push repoC effective = %q capped %q, want deny/user", pushC.Effective.Setting, pushC.Effective.CappedBy)
	}

	// Unauthored cell resolves to the Base default (Allow) with no explicit layers,
	// and a setting on one repo must not leak onto another.
	readB, ok := findRepoRow(rows, capRepoRead, repoB)
	if !ok {
		t.Fatalf("repo.read row for %s missing", repoB)
	}
	if len(readB.Layers) != 0 {
		t.Fatalf("unauthored repo cell should have no explicit layers, got %v", readB.Layers)
	}
	if readB.Effective.Setting != SettingAllow {
		t.Fatalf("unauthored repo cell effective = %q, want allow (base default)", readB.Effective.Setting)
	}
	if _, leaked := findRepoRow(rows, capRepoCheckout, repoB); !leaked {
		t.Fatal("repo.checkout row for repoB missing")
	}
	if cb, _ := findRepoRow(rows, capRepoCheckout, repoB); len(cb.Layers) != 0 {
		t.Fatalf("agent allow on repoA leaked onto repoB checkout: %v", cb.Layers)
	}
}

// TestTable_RepoRowsAbsentWithoutRepos proves the table emits no repo rows when
// the workspace has neither workspace repos nor project github_repo resources —
// the per-repo block simply doesn't appear.
func TestTable_RepoRowsAbsentWithoutRepos(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	clearRepos(t, s)
	defer clearRepos(t, s)
	ctx := context.Background()

	addCap(t, s, "add_comment", "Add comment", "Issues", "builtin")

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	for _, r := range rows {
		if r.Category == repoCategory {
			t.Fatalf("unexpected repo row with no workspace repos: %+v", r)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (only the capability-wide row)", len(rows))
	}
}
