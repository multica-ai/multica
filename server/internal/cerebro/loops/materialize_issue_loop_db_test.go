package loops

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/util"
)

func seedIssueLoopRecipe(t *testing.T, workspaceID pgtype.UUID, name string, loopSpecJSON []byte) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := loopTestPool.QueryRow(context.Background(),
		`INSERT INTO cerebro_workflow (
			workspace_id, name, trigger_type, action_type, created_by_id, created_by_type,
			workflow_type, loop_spec
		) VALUES (
			$1, $2, 'status_changed', 'run_skill', gen_random_uuid(), 'member',
			'issue_loop', $3
		) RETURNING id`,
		workspaceID, name, loopSpecJSON,
	).Scan(&id); err != nil {
		t.Fatalf("seed issue-loop recipe: %v", err)
	}
	return id
}

func TestIssueLoopBridge_ActivateOnIssueReplacesPriorRecipeForSameIssue(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)

	var workspaceID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id = $1`, issueID).Scan(&workspaceID); err != nil {
		t.Fatalf("load issue workspace: %v", err)
	}

	loopSpecJSON, err := json.Marshal(issueLoopSpecWire{
		Spec:       *goodSpec(t),
		BuildSkill: "build",
	})
	if err != nil {
		t.Fatalf("marshal loop spec: %v", err)
	}

	firstRecipe := seedIssueLoopRecipe(t, workspaceID, "first issue loop recipe", loopSpecJSON)
	secondRecipe := seedIssueLoopRecipe(t, workspaceID, "second issue loop recipe", loopSpecJSON)

	bridge := NewIssueLoopBridge(cerebrodb.New(loopTestPool), workflows.NewIssueLoopColumnStore(loopTestPool))
	if err := bridge.ActivateOnIssue(ctx, workspaceID, firstRecipe, pgtype.UUID{}, firstRecipe, issueID, "member", loopSpecJSON); err != nil {
		t.Fatalf("activate first recipe: %v", err)
	}
	if err := bridge.ActivateOnIssue(ctx, workspaceID, secondRecipe, pgtype.UUID{}, secondRecipe, issueID, "member", loopSpecJSON); err != nil {
		t.Fatalf("activate second recipe: %v", err)
	}

	rows, err := loopTestPool.Query(ctx,
		`SELECT DISTINCT generated_from_workflow_id
		 FROM cerebro_workflow
		 WHERE generated_for_issue_id = $1
		 ORDER BY generated_from_workflow_id`,
		issueID,
	)
	if err != nil {
		t.Fatalf("list active recipes: %v", err)
	}
	defer rows.Close()

	var active []string
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan active recipe: %v", err)
		}
		active = append(active, util.UUIDToString(id))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate active recipes: %v", err)
	}

	if len(active) != 1 || active[0] != util.UUIDToString(secondRecipe) {
		t.Fatalf("issue should have only the latest active recipe, got %v; want %s", active, util.UUIDToString(secondRecipe))
	}
}
