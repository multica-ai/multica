package handler

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestPRMergeStatusMigration runs migration 551 against workspaces with each
// kind of PR history (MUL-7726). Workspaces whose merges never all completed
// their issues are pinned to "none"; the rest keep the Done default. The
// migration runs inside a rolled-back transaction, twice, to prove a replay is
// harmless.
func TestPRMergeStatusMigration(t *testing.T) {
	if testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	type link struct {
		state  string
		closes bool
	}
	cases := []struct {
		name     string
		settings string
		links    []link
		want     string // "" = key absent (Done default)
	}{
		{"no links", `{}`, nil, ""},
		{"only keyword merges", `{}`, []link{{"merged", true}, {"open", false}}, ""},
		{"plain merge", `{}`, []link{{"merged", false}}, "none"},
		{"mixed merges", `{}`, []link{{"merged", true}, {"merged", false}}, "none"},
		{"nothing merged yet", `{}`, []link{{"open", true}}, "none"},
		{"switched off", `{"pr_auto_complete_enabled": false}`, nil, "none"},
		{"already chosen", `{"pr_merge_status": "in_review"}`, []link{{"merged", false}}, "in_review"},
	}
	workspaces := make([]string, len(cases))
	for i, tc := range cases {
		ws := dbfx.Workspace(t, "PR merge status migration", fmt.Sprintf("pr-merge-migration-%d", i),
			testutil.Cols{"issue_prefix": "PMM", "settings": testutil.Raw(fmt.Sprintf("'%s'::jsonb", tc.settings))})
		workspaces[i] = ws
		fixture := testutil.New(testPool, ws, testUserID)
		for j, l := range tc.links {
			issue := fixture.Issue(t, "Migration fixture")
			pr := fixture.Insert(t, "github_pull_request", testutil.Cols{
				"workspace_id": ws, "installation_id": 551, "repo_owner": "acme", "repo_name": "migration",
				"pr_number": j + 1, "title": "Fixture", "state": l.state,
				"html_url":      fmt.Sprintf("https://github.com/acme/migration/pull/%d", j+1),
				"pr_created_at": testutil.Raw("now()"), "pr_updated_at": testutil.Raw("now()"),
			})
			fixture.InsertNoID(t, "issue_pull_request",
				testutil.Cols{"issue_id": issue, "pull_request_id": pr, "close_intent": l.closes},
				"issue_id = $1 AND pull_request_id = $2", issue, pr)
		}
	}

	migration, err := os.ReadFile("../../migrations/551_pr_merge_status.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for range 2 {
		if _, err := tx.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("migration: %v", err)
		}
	}
	for i, tc := range cases {
		var got, legacy *string
		if err := tx.QueryRow(ctx, `SELECT settings->>'pr_merge_status', settings->>'pr_auto_complete_enabled' FROM workspace WHERE id = $1`, workspaces[i]).Scan(&got, &legacy); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		value := ""
		if got != nil {
			value = *got
		}
		if value != tc.want {
			t.Errorf("%s: pr_merge_status = %q, want %q", tc.name, value, tc.want)
		}
		// Desktop clients from before the change show the retired switch; a
		// pinned workspace must show it off there.
		if tc.want == "none" && (legacy == nil || *legacy != "false") {
			t.Errorf("%s: pr_auto_complete_enabled = %v, want false", tc.name, legacy)
		}
	}
}
